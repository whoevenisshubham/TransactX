package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	maxCorpusDocumentBytes = 1 << 20
	maxChunkWords          = 500
	minSplitChunkWords     = 250
	chunkOverlapWords      = 40
	maxRetrievalQuery      = 500
	maxRetrievalTags       = 8
	maxRetrievalTopK       = 5
)

var (
	corpusVersionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	corpusHashPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	corpusTagPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	headingPattern       = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)
)

var ErrCorpusVersionConflict = errors.New("corpus version already has different contents")

type CorpusManifest struct {
	Version   string                   `json:"version"`
	Active    bool                     `json:"active"`
	Documents []CorpusManifestDocument `json:"documents"`
	Note      string                   `json:"note,omitempty"`
}

type CorpusManifestDocument struct {
	Path                 string   `json:"path"`
	SHA256               string   `json:"sha256"`
	ImplementationStatus string   `json:"implementationStatus"`
	AllowedRoles         []string `json:"allowedRoles"`
	Tags                 []string `json:"tags"`
	Sections             []string `json:"sections"`
	HashEncoding         string   `json:"hashEncoding"`
}

type CorpusBuild struct {
	Version        string
	ManifestSHA256 string
	Chunks         []RunbookChunk
}

// BuildCorpus reads only Markdown files explicitly named by the manifest. File
// hashes are calculated over UTF-8 bytes with LF line endings so checkout line
// ending conversion cannot silently change an approved source.
func BuildCorpus(repositoryRoot, manifestPath string) (CorpusBuild, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return CorpusBuild{}, fmt.Errorf("resolve repository root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return CorpusBuild{}, fmt.Errorf("resolve repository root links: %w", err)
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return CorpusBuild{}, fmt.Errorf("read corpus manifest: %w", err)
	}
	manifestBytes = canonicalLF(manifestBytes)
	var manifest CorpusManifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return CorpusBuild{}, fmt.Errorf("decode corpus manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CorpusBuild{}, errors.New("corpus manifest contains trailing JSON values")
	}
	if err := validateManifest(manifest); err != nil {
		return CorpusBuild{}, err
	}

	build := CorpusBuild{
		Version:        manifest.Version,
		ManifestSHA256: sha256Hex(manifestBytes),
	}
	seenPaths := make(map[string]struct{}, len(manifest.Documents))
	seenChunkIDs := make(map[string]struct{})
	for _, document := range manifest.Documents {
		if _, exists := seenPaths[document.Path]; exists {
			return CorpusBuild{}, fmt.Errorf("duplicate corpus document path %q", document.Path)
		}
		seenPaths[document.Path] = struct{}{}

		contents, err := readApprovedMarkdown(root, document)
		if err != nil {
			return CorpusBuild{}, err
		}
		chunks, err := chunkMarkdown(manifest.Version, document, contents)
		if err != nil {
			return CorpusBuild{}, fmt.Errorf("chunk %s: %w", document.Path, err)
		}
		for _, chunk := range chunks {
			if _, exists := seenChunkIDs[chunk.ID]; exists {
				return CorpusBuild{}, fmt.Errorf("duplicate chunk ID %q", chunk.ID)
			}
			seenChunkIDs[chunk.ID] = struct{}{}
			build.Chunks = append(build.Chunks, chunk)
		}
	}
	if len(build.Chunks) == 0 {
		return CorpusBuild{}, errors.New("approved corpus produced no chunks")
	}
	if err := validateCorpusBuild(build); err != nil {
		return CorpusBuild{}, err
	}
	return build, nil
}

func validateCorpusBuild(build CorpusBuild) error {
	if !corpusVersionPattern.MatchString(build.Version) || !corpusHashPattern.MatchString(build.ManifestSHA256) || len(build.Chunks) == 0 {
		return errors.New("invalid corpus build")
	}
	seen := make(map[string]struct{}, len(build.Chunks))
	for _, chunk := range build.Chunks {
		if chunk.ID == "" || chunk.Version != build.Version || chunk.Path == "" || chunk.Heading == "" || chunk.Content == "" {
			return fmt.Errorf("invalid corpus chunk identity %q", chunk.ID)
		}
		if _, duplicate := seen[chunk.ID]; duplicate {
			return fmt.Errorf("duplicate corpus chunk identity %q", chunk.ID)
		}
		seen[chunk.ID] = struct{}{}
		if chunk.StartLine < 1 || chunk.EndLine < chunk.StartLine || len(strings.Fields(chunk.Content)) > maxChunkWords {
			return fmt.Errorf("invalid corpus chunk bounds %q", chunk.ID)
		}
		if !corpusHashPattern.MatchString(chunk.SHA256) || sha256Hex([]byte(chunk.Content)) != chunk.SHA256 || !corpusHashPattern.MatchString(chunk.SourceSHA256) {
			return fmt.Errorf("invalid corpus chunk hash %q", chunk.ID)
		}
		if chunk.ImplementationStatus != "existing" && chunk.ImplementationStatus != "planned" {
			return fmt.Errorf("invalid corpus chunk status %q", chunk.ID)
		}
		if len(chunk.AllowedRoles) == 0 || len(chunk.Tags) == 0 || len(chunk.Tags) > maxRetrievalTags {
			return fmt.Errorf("invalid corpus chunk authorization or tags %q", chunk.ID)
		}
		for _, role := range chunk.AllowedRoles {
			if role != "OPS_ADMIN" && role != "OPS_VIEWER" {
				return fmt.Errorf("invalid corpus chunk role %q", chunk.ID)
			}
		}
		for _, tag := range chunk.Tags {
			if !corpusTagPattern.MatchString(tag) {
				return fmt.Errorf("invalid corpus chunk tag %q", chunk.ID)
			}
		}
	}
	return nil
}

func validateManifest(manifest CorpusManifest) error {
	if !corpusVersionPattern.MatchString(manifest.Version) {
		return fmt.Errorf("invalid corpus version %q", manifest.Version)
	}
	if len(manifest.Documents) == 0 {
		return errors.New("corpus manifest has no documents")
	}
	for _, document := range manifest.Documents {
		if document.Path == "" || strings.Contains(document.Path, `\`) || filepath.IsAbs(document.Path) {
			return fmt.Errorf("invalid corpus path %q", document.Path)
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(document.Path)))
		if clean != document.Path || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("corpus path escapes or is not canonical: %q", document.Path)
		}
		if strings.ToLower(filepath.Ext(clean)) != ".md" {
			return fmt.Errorf("corpus document is not Markdown: %q", document.Path)
		}
		if !corpusHashPattern.MatchString(document.SHA256) {
			return fmt.Errorf("invalid SHA-256 for %q", document.Path)
		}
		if document.HashEncoding != "UTF-8 with LF line endings" {
			return fmt.Errorf("unsupported hash encoding for %q", document.Path)
		}
		if document.ImplementationStatus != "existing" && document.ImplementationStatus != "planned" {
			return fmt.Errorf("invalid implementation status for %q", document.Path)
		}
		if len(document.AllowedRoles) == 0 {
			return fmt.Errorf("document %q has no allowed roles", document.Path)
		}
		roles := make(map[string]struct{}, len(document.AllowedRoles))
		for _, role := range document.AllowedRoles {
			if role != "OPS_ADMIN" && role != "OPS_VIEWER" {
				return fmt.Errorf("document %q has unsupported role %q", document.Path, role)
			}
			if _, duplicate := roles[role]; duplicate {
				return fmt.Errorf("document %q repeats role %q", document.Path, role)
			}
			roles[role] = struct{}{}
		}
		if len(document.Sections) == 0 {
			return fmt.Errorf("document %q has no approved sections", document.Path)
		}
		if len(document.Tags) == 0 || len(document.Tags) > maxRetrievalTags {
			return fmt.Errorf("document %q has invalid tag count", document.Path)
		}
		tags := make(map[string]struct{}, len(document.Tags))
		for _, tag := range document.Tags {
			if !corpusTagPattern.MatchString(tag) {
				return fmt.Errorf("document %q has invalid tag %q", document.Path, tag)
			}
			if _, duplicate := tags[tag]; duplicate {
				return fmt.Errorf("document %q repeats tag %q", document.Path, tag)
			}
			tags[tag] = struct{}{}
		}
	}
	return nil
}

func readApprovedMarkdown(root string, document CorpusManifestDocument) ([]byte, error) {
	candidate := filepath.Join(root, filepath.FromSlash(document.Path))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, fmt.Errorf("resolve corpus document %q: %w", document.Path, err)
	}
	if err := requireWithinRoot(root, resolved); err != nil {
		return nil, fmt.Errorf("corpus document %q: %w", document.Path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat corpus document %q: %w", document.Path, err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxCorpusDocumentBytes {
		return nil, fmt.Errorf("corpus document %q is not a bounded regular file", document.Path)
	}
	contents, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read corpus document %q: %w", document.Path, err)
	}
	contents = canonicalLF(contents)
	if !utf8.Valid(contents) {
		return nil, fmt.Errorf("corpus document %q is not valid UTF-8 text", document.Path)
	}
	actualHash := sha256Hex(contents)
	if actualHash != document.SHA256 {
		return nil, fmt.Errorf("corpus document %q hash mismatch: got %s", document.Path, actualHash)
	}
	return contents, nil
}

func requireWithinRoot(root, candidate string) error {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("resolved path escapes repository root")
	}
	return nil
}

type markdownSection struct {
	heading           string
	level             int
	headingLine       int
	contentStart, end int
}

type chunkWord struct {
	text string
	line int
}

func chunkMarkdown(version string, document CorpusManifestDocument, contents []byte) ([]RunbookChunk, error) {
	lines := strings.Split(string(contents), "\n")
	sections := parseMarkdownSections(lines)
	byHeading := make(map[string][]markdownSection)
	for _, section := range sections {
		byHeading[section.heading] = append(byHeading[section.heading], section)
	}

	var chunks []RunbookChunk
	for _, approvedHeading := range document.Sections {
		matches := byHeading[approvedHeading]
		if len(matches) != 1 {
			return nil, fmt.Errorf("approved heading %q matched %d sections", approvedHeading, len(matches))
		}
		section := matches[0]
		words := sectionWords(lines, section)
		if len(words) == 0 {
			return nil, fmt.Errorf("approved heading %q is empty", approvedHeading)
		}
		for _, span := range chunkSpans(len(words)) {
			selected := words[span[0]:span[1]]
			parts := make([]string, len(selected))
			for index, word := range selected {
				parts[index] = word.text
			}
			content := strings.Join(parts, " ")
			contentHash := sha256Hex([]byte(content))
			identity := strings.Join([]string{
				version,
				document.Path,
				approvedHeading,
				fmt.Sprintf("%d", selected[0].line),
				fmt.Sprintf("%d", selected[len(selected)-1].line),
				contentHash,
			}, "\n")
			identityHash := sha256.Sum256([]byte(identity))
			chunks = append(chunks, RunbookChunk{
				ID:                   "runbook:" + hex.EncodeToString(identityHash[:16]),
				Path:                 document.Path,
				Heading:              approvedHeading,
				SHA256:               contentHash,
				Version:              version,
				Content:              content,
				StartLine:            selected[0].line,
				EndLine:              selected[len(selected)-1].line,
				AllowedRoles:         append([]string(nil), document.AllowedRoles...),
				SourceSHA256:         document.SHA256,
				ImplementationStatus: document.ImplementationStatus,
				Tags:                 append([]string(nil), document.Tags...),
			})
		}
	}
	return chunks, nil
}

func parseMarkdownSections(lines []string) []markdownSection {
	type heading struct {
		text  string
		level int
		line  int
	}
	var headings []heading
	inFence := false
	fenceMarker := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if !inFence {
				inFence, fenceMarker = true, marker
			} else if marker == fenceMarker {
				inFence, fenceMarker = false, ""
			}
			continue
		}
		if inFence {
			continue
		}
		match := headingPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		headings = append(headings, heading{text: strings.TrimSpace(match[2]), level: len(match[1]), line: index + 1})
	}

	sections := make([]markdownSection, 0, len(headings))
	for index, current := range headings {
		end := len(lines)
		for next := index + 1; next < len(headings); next++ {
			if headings[next].level <= current.level {
				end = headings[next].line - 1
				break
			}
		}
		sections = append(sections, markdownSection{
			heading: current.text, level: current.level, headingLine: current.line,
			contentStart: current.line + 1, end: end,
		})
	}
	return sections
}

func sectionWords(lines []string, section markdownSection) []chunkWord {
	var words []chunkWord
	for lineNumber := section.contentStart; lineNumber <= section.end && lineNumber <= len(lines); lineNumber++ {
		for _, word := range strings.Fields(lines[lineNumber-1]) {
			words = append(words, chunkWord{text: word, line: lineNumber})
		}
	}
	return words
}

func chunkSpans(wordCount int) [][2]int {
	if wordCount <= maxChunkWords {
		return [][2]int{{0, wordCount}}
	}
	var spans [][2]int
	start := 0
	for {
		remaining := wordCount - start
		if remaining <= maxChunkWords {
			spans = append(spans, [2]int{start, wordCount})
			return spans
		}
		end := start + maxChunkWords
		if wordCount-(end-chunkOverlapWords) < minSplitChunkWords {
			end = wordCount - minSplitChunkWords + chunkOverlapWords
		}
		spans = append(spans, [2]int{start, end})
		start = end - chunkOverlapWords
	}
}

func canonicalLF(contents []byte) []byte {
	value := strings.ReplaceAll(string(contents), "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return []byte(value)
}

func sha256Hex(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

type memoryCorpusVersion struct {
	manifestSHA string
	chunks      []RunbookChunk
}

// MemoryCorpus is a deterministic test/evaluation implementation. Production
// composition must use a durable M2-owned store and must never silently fall
// back to this implementation.
type MemoryCorpus struct {
	mu       sync.RWMutex
	versions map[string]memoryCorpusVersion
	active   string
}

func NewMemoryCorpus() *MemoryCorpus {
	return &MemoryCorpus{versions: make(map[string]memoryCorpusVersion)}
}

func (corpus *MemoryCorpus) Stage(build CorpusBuild) error {
	if err := validateCorpusBuild(build); err != nil {
		return err
	}
	corpus.mu.Lock()
	defer corpus.mu.Unlock()
	if current, exists := corpus.versions[build.Version]; exists {
		if current.manifestSHA != build.ManifestSHA256 {
			return ErrCorpusVersionConflict
		}
		return nil
	}
	corpus.versions[build.Version] = memoryCorpusVersion{
		manifestSHA: build.ManifestSHA256,
		chunks:      cloneChunks(build.Chunks),
	}
	return nil
}

func (corpus *MemoryCorpus) Activate(version, manifestSHA string) error {
	corpus.mu.Lock()
	defer corpus.mu.Unlock()
	staged, exists := corpus.versions[version]
	if !exists || staged.manifestSHA != manifestSHA {
		return errors.New("staged corpus version/hash not found")
	}
	corpus.active = version
	return nil
}

func (corpus *MemoryCorpus) Retrieve(ctx context.Context, scope TrustedScope, query string, tags []string, topK int) ([]RunbookChunk, error) {
	if err := validateRetrieval(scope, query, tags, topK); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	corpus.mu.RLock()
	active := corpus.versions[corpus.active]
	corpus.mu.RUnlock()
	if corpus.active == "" {
		return nil, errors.New("no active corpus")
	}
	type scoredChunk struct {
		chunk RunbookChunk
		score int
	}
	terms := searchTerms(query)
	requestedTags := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		requestedTags[tag] = struct{}{}
	}
	var matches []scoredChunk
	for _, chunk := range active.chunks {
		if !containsString(chunk.AllowedRoles, scope.Role) {
			continue
		}
		heading := strings.ToLower(chunk.Heading)
		body := strings.ToLower(chunk.Content)
		score := 0
		for _, term := range terms {
			score += 4 * strings.Count(heading, term)
			score += strings.Count(body, term)
		}
		for _, tag := range chunk.Tags {
			if _, wanted := requestedTags[tag]; wanted {
				score += 8
			}
		}
		if score > 0 {
			matches = append(matches, scoredChunk{chunk: chunk, score: score})
		}
	}
	sort.Slice(matches, func(left, right int) bool {
		if matches[left].score != matches[right].score {
			return matches[left].score > matches[right].score
		}
		return matches[left].chunk.ID < matches[right].chunk.ID
	})
	if len(matches) > topK {
		matches = matches[:topK]
	}
	result := make([]RunbookChunk, len(matches))
	for index := range matches {
		result[index] = cloneChunk(matches[index].chunk)
	}
	return result, nil
}

func (corpus *MemoryCorpus) Resolve(version, chunkID string) (RunbookChunk, bool) {
	corpus.mu.RLock()
	defer corpus.mu.RUnlock()
	for _, chunk := range corpus.versions[version].chunks {
		if chunk.ID == chunkID {
			return cloneChunk(chunk), true
		}
	}
	return RunbookChunk{}, false
}

func validateRetrieval(scope TrustedScope, query string, tags []string, topK int) error {
	if scope.Role != "OPS_ADMIN" && scope.Role != "OPS_VIEWER" {
		return errors.New("role is not authorized for runbook retrieval")
	}
	if strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > maxRetrievalQuery {
		return errors.New("retrieval query must contain 1 to 500 characters")
	}
	if topK < 1 || topK > maxRetrievalTopK {
		return errors.New("retrieval topK must be between 1 and 5")
	}
	if len(tags) > maxRetrievalTags {
		return errors.New("too many retrieval tags")
	}
	for _, tag := range tags {
		if !corpusTagPattern.MatchString(tag) {
			return fmt.Errorf("invalid retrieval tag %q", tag)
		}
	}
	return nil
}

func searchTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	stop := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "are": {}, "for": {}, "from": {}, "how": {}, "is": {},
		"of": {}, "or": {}, "the": {}, "this": {}, "to": {}, "was": {}, "what": {}, "when": {}, "why": {},
	}
	seen := make(map[string]struct{}, len(fields))
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 2 {
			continue
		}
		if _, ignored := stop[field]; ignored {
			continue
		}
		if _, duplicate := seen[field]; duplicate {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
	}
	return terms
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func cloneChunks(chunks []RunbookChunk) []RunbookChunk {
	result := make([]RunbookChunk, len(chunks))
	for index := range chunks {
		result[index] = cloneChunk(chunks[index])
	}
	return result
}

func cloneChunk(chunk RunbookChunk) RunbookChunk {
	chunk.AllowedRoles = append([]string(nil), chunk.AllowedRoles...)
	chunk.Tags = append([]string(nil), chunk.Tags...)
	return chunk
}
