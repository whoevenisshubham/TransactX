package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		filepath.Join("..", "..", ".."),
		filepath.Join(".."),
		".",
	} {
		manifest := filepath.Join(candidate, "docs", "runbooks", "corpus-manifest.json")
		if _, err := os.Stat(manifest); err == nil {
			root, err := filepath.Abs(candidate)
			if err == nil {
				return root
			}
		}
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func approvedCorpus(t *testing.T) CorpusBuild {
	t.Helper()
	root := repositoryRoot(t)
	build, err := BuildCorpus(root, filepath.Join(root, "docs", "runbooks", "corpus-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return build
}

func TestBuildApprovedCorpusStableChunks(t *testing.T) {
	first := approvedCorpus(t)
	second := approvedCorpus(t)
	if first.Version != "tx.corpus.c1.v1" || first.ManifestSHA256 == "" || len(first.Chunks) < 20 {
		t.Fatalf("unexpected corpus summary: version=%s hash=%s chunks=%d", first.Version, first.ManifestSHA256, len(first.Chunks))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical corpus build was not deterministic")
	}

	groups := make(map[string][]RunbookChunk)
	for _, chunk := range first.Chunks {
		if chunk.StartLine < 1 || chunk.EndLine < chunk.StartLine || chunk.Version != first.Version {
			t.Fatalf("invalid chunk location/version: %+v", chunk)
		}
		words := strings.Fields(chunk.Content)
		if len(words) == 0 || len(words) > maxChunkWords {
			t.Fatalf("chunk %s has %d words", chunk.ID, len(words))
		}
		if sha256Hex([]byte(chunk.Content)) != chunk.SHA256 {
			t.Fatalf("chunk %s content hash mismatch", chunk.ID)
		}
		groups[chunk.Path+"\x00"+chunk.Heading] = append(groups[chunk.Path+"\x00"+chunk.Heading], chunk)
	}
	for key, chunks := range groups {
		sort.Slice(chunks, func(left, right int) bool { return chunks[left].StartLine < chunks[right].StartLine })
		for index := 1; index < len(chunks); index++ {
			priorWords := strings.Fields(chunks[index-1].Content)
			currentWords := strings.Fields(chunks[index].Content)
			if len(priorWords) < chunkOverlapWords || len(currentWords) < chunkOverlapWords {
				t.Fatalf("split chunk too small in %s", key)
			}
			if !reflect.DeepEqual(priorWords[len(priorWords)-chunkOverlapWords:], currentWords[:chunkOverlapWords]) {
				t.Fatalf("split chunks do not preserve exact %d-word overlap in %s", chunkOverlapWords, key)
			}
		}
	}
}

func TestBuildCorpusRejectsPathEscapeAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("# Outside\n\n## Secret\n\nnot approved\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	manifest := CorpusManifest{
		Version: "tx.test.v1",
		Documents: []CorpusManifestDocument{{
			Path: "../outside.md", SHA256: strings.Repeat("0", 64), ImplementationStatus: "existing",
			AllowedRoles: []string{"OPS_ADMIN"}, Tags: []string{"test"}, Sections: []string{"Secret"},
			HashEncoding: "UTF-8 with LF line endings",
		}},
	}
	manifestPath := writeManifest(t, manifest)
	if _, err := BuildCorpus(root, manifestPath); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("path escape error = %v", err)
	}

	link := filepath.Join(root, "linked.md")
	if err := os.Symlink(outside, link); err != nil {
		// Windows hosts without Developer Mode cannot create a symlink. The
		// resolved-boundary helper still receives a mandatory escape test.
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			t.Fatal(rootErr)
		}
		resolvedOutside, outsideErr := filepath.EvalSymlinks(outside)
		if outsideErr != nil {
			t.Fatal(outsideErr)
		}
		if boundaryErr := requireWithinRoot(resolvedRoot, resolvedOutside); boundaryErr == nil {
			t.Fatal("resolved outside path passed repository boundary")
		}
		return
	}
	outsideBytes, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Documents[0].Path = "linked.md"
	manifest.Documents[0].SHA256 = sha256Hex(outsideBytes)
	manifestPath = writeManifest(t, manifest)
	if _, err := BuildCorpus(root, manifestPath); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escaping symlink error = %v", err)
	}
}

func TestMemoryCorpusDuplicateRemovedDocumentRoleAndCitation(t *testing.T) {
	build := approvedCorpus(t)
	corpus := NewMemoryCorpus()
	if err := corpus.Stage(build); err != nil {
		t.Fatal(err)
	}
	if err := corpus.Stage(build); err != nil {
		t.Fatalf("duplicate exact ingestion: %v", err)
	}
	conflict := build
	conflict.ManifestSHA256 = strings.Repeat("f", 64)
	if err := corpus.Stage(conflict); !errors.Is(err, ErrCorpusVersionConflict) {
		t.Fatalf("version conflict = %v", err)
	}
	if err := corpus.Activate(build.Version, build.ManifestSHA256); err != nil {
		t.Fatal(err)
	}

	scope := TrustedScope{ActorID: "operator", Role: "OPS_VIEWER", SelectedPaymentID: "selected"}
	initial, err := corpus.Retrieve(context.Background(), scope, "IsolationForest held-out baseline", []string{"baseline", "model"}, 5)
	if err != nil || len(initial) == 0 {
		t.Fatalf("initial retrieval = %v, %v", initial, err)
	}
	var saved RunbookChunk
	for _, chunk := range initial {
		if chunk.Path == "docs/runbooks/projections-and-anomaly-analytics.md" {
			saved = chunk
			break
		}
	}
	if saved.ID == "" {
		t.Fatal("expected analytics citation not retrieved")
	}

	next := CorpusBuild{Version: "tx.corpus.c1.v2", ManifestSHA256: strings.Repeat("a", 64)}
	for index, chunk := range build.Chunks {
		if chunk.Path == "docs/runbooks/projections-and-anomaly-analytics.md" {
			continue
		}
		chunk.Version = next.Version
		chunk.ID = fmt.Sprintf("runbook:v2:%03d", index)
		next.Chunks = append(next.Chunks, chunk)
	}
	if err := corpus.Stage(next); err != nil {
		t.Fatal(err)
	}
	if err := corpus.Activate(next.Version, next.ManifestSHA256); err != nil {
		t.Fatal(err)
	}
	after, err := corpus.Retrieve(context.Background(), scope, "IsolationForest held-out baseline", []string{"baseline", "model"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range after {
		if chunk.Path == saved.Path {
			t.Fatal("removed document remained in active corpus")
		}
	}
	resolved, ok := corpus.Resolve(build.Version, saved.ID)
	if !ok || resolved.SHA256 != saved.SHA256 || resolved.Content != saved.Content {
		t.Fatal("saved citation did not resolve after active corpus switch")
	}

	unauthorized := scope
	unauthorized.Role = "CUSTOMER"
	if _, err := corpus.Retrieve(context.Background(), unauthorized, "payment", nil, 5); err == nil {
		t.Fatal("customer role retrieved operational runbooks")
	}
}

func TestPostgresRetrievalKeepsQueryParameterized(t *testing.T) {
	injected := `pending'); DROP TABLE runbook_chunks; --`
	scope := TrustedScope{Role: "OPS_ADMIN"}
	statement, arguments := postgresRetrievalQuery(scope, injected, []string{"recovery"}, 5)
	if strings.Contains(statement, injected) || !strings.Contains(statement, "websearch_to_tsquery('simple', $1)") {
		t.Fatal("retrieval query interpolated untrusted input")
	}
	if len(arguments) != 4 || arguments[0] != injected || arguments[1] != scope.Role || arguments[3] != 5 {
		t.Fatalf("unexpected retrieval arguments: %#v", arguments)
	}
}

func TestReviewedRetrievalRecallAtFive(t *testing.T) {
	var dataset struct {
		Version string `json:"version"`
		Cases   []struct {
			ID, Query, ExpectedPath, ExpectedHeading string
			Tags                                     []string
		} `json:"cases"`
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "retrieval_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &dataset); err != nil {
		t.Fatal(err)
	}
	if len(dataset.Cases) < 8 {
		t.Fatalf("reviewed retrieval cases = %d, want at least 8", len(dataset.Cases))
	}
	datasetHash := sha256.Sum256(raw)

	build := approvedCorpus(t)
	corpus := NewMemoryCorpus()
	if err := corpus.Stage(build); err != nil {
		t.Fatal(err)
	}
	if err := corpus.Activate(build.Version, build.ManifestSHA256); err != nil {
		t.Fatal(err)
	}
	scope := TrustedScope{ActorID: "reviewer", Role: "OPS_ADMIN", SelectedPaymentID: "selected"}
	started := time.Now()
	hits := 0
	for _, testCase := range dataset.Cases {
		chunks, err := corpus.Retrieve(context.Background(), scope, testCase.Query, testCase.Tags, 5)
		if err != nil {
			t.Fatalf("%s: %v", testCase.ID, err)
		}
		found := false
		for _, chunk := range chunks {
			if chunk.Path == testCase.ExpectedPath && chunk.Heading == testCase.ExpectedHeading {
				found = true
				break
			}
		}
		if found {
			hits++
		} else {
			t.Errorf("%s: expected %s / %s in top five; got %v", testCase.ID, testCase.ExpectedPath, testCase.ExpectedHeading, summarizeChunks(chunks))
		}
	}
	recall := float64(hits) / float64(len(dataset.Cases))
	if recall < 0.85 {
		t.Fatalf("reviewed recall@5 %.3f is below 0.85", recall)
	}
	t.Logf("dataset=%s sha256=%s cases=%d chunks=%d recall@5=%.3f elapsed=%s", dataset.Version, hex.EncodeToString(datasetHash[:]), len(dataset.Cases), len(build.Chunks), recall, time.Since(started))
}

func writeManifest(t *testing.T, manifest CorpusManifest) string {
	t.Helper()
	contents, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func summarizeChunks(chunks []RunbookChunk) []string {
	result := make([]string, len(chunks))
	for index, chunk := range chunks {
		result[index] = chunk.Path + " / " + chunk.Heading
	}
	return result
}
