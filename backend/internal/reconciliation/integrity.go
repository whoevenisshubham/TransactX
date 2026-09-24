package reconciliation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidProof              = errors.New("invalid integrity proof")
	ErrRecordNotFound            = errors.New("record not found in commitment")
	ErrProofRecordMismatch       = errors.New("proof record does not match leaf hash")
	ErrProofBucketMismatch       = errors.New("proof record does not belong to bucket")
	ErrProofScopeMismatch        = errors.New("proof record or bucket is outside scope")
	ErrProofParticipantMismatch  = errors.New("proof participant mismatch")
	ErrProofRootMismatch         = errors.New("reconstructed root does not match expected root")
	ErrProofBucketRootMismatch   = errors.New("reconstructed bucket root does not match expected bucket root")
	ErrProofMalformedPath        = errors.New("malformed proof sibling path")
	ErrProofIncompatibleVersion  = errors.New("incompatible proof version")
)

// SiblingOrder specifies the position of a sibling relative to the current node
// during bottom-up Merkle path reconstruction.
type SiblingOrder string

const (
	// SiblingLeft indicates the sibling is to the left of the current node:
	// parent = InternalNodeHash(sibling, current)
	SiblingLeft SiblingOrder = "LEFT"

	// SiblingRight indicates the sibling is to the right of the current node:
	// parent = InternalNodeHash(current, sibling)
	SiblingRight SiblingOrder = "RIGHT"

	// SiblingPromoted indicates the current node was an odd final child at this
	// tree level and was promoted unchanged without hashing:
	// parent = current
	SiblingPromoted SiblingOrder = "PROMOTED"
)

// ProofStep represents one level in a Merkle authentication path.
type ProofStep struct {
	Hash  []byte       `json:"hash,omitempty"`
	Order SiblingOrder `json:"order"`
}

// Validate ensures the proof step has valid structure.
func (s ProofStep) Validate() error {
	switch s.Order {
	case SiblingLeft, SiblingRight:
		if len(s.Hash) != sha256.Size {
			return fmt.Errorf("%w: sibling hash must be %d bytes, got %d", ErrProofMalformedPath, sha256.Size, len(s.Hash))
		}
	case SiblingPromoted:
		if len(s.Hash) != 0 {
			return fmt.Errorf("%w: promoted sibling must have empty hash", ErrProofMalformedPath)
		}
	default:
		return fmt.Errorf("%w: invalid sibling order %q", ErrProofMalformedPath, s.Order)
	}
	return nil
}

// ProofGenerationMetrics records timing and structural metadata for proof generation.
type ProofGenerationMetrics struct {
	Duration            time.Duration `json:"durationNs"`
	BucketSiblingCount  int           `json:"bucketSiblingCount"`
	GlobalSiblingCount  int           `json:"globalSiblingCount"`
	TotalSiblingCount   int           `json:"totalSiblingCount"`
	SerializedSizeBytes int           `json:"serializedSizeBytes,omitempty"`
}

// ProofVerificationMetrics records timing and verification metadata.
type ProofVerificationMetrics struct {
	Duration       time.Duration `json:"durationNs"`
	StepsEvaluated int           `json:"stepsEvaluated"`
	RootVerified   bool          `json:"rootVerified"`
}

// VerificationResult contains the outcome of proof verification.
type VerificationResult struct {
	Valid                   bool                     `json:"valid"`
	ReconstructedBucketRoot []byte                   `json:"reconstructedBucketRoot"`
	ReconstructedRoot       []byte                   `json:"reconstructedRoot"`
	Metrics                 ProofVerificationMetrics `json:"metrics"`
}

// IntegrityProof is the typed cryptographic proof that a canonical ledger record
// belongs to a committed Merkle root.
//
// Architectural Boundary:
// An integrity proof proves ONLY that the specified record was committed into the
// specific Merkle tree path represented by that proof under the specified scope.
// It does NOT claim or prove that the entire financial system or external ledgers
// are globally correct.
type IntegrityProof struct {
	// Record is the canonical ledger record being proven.
	Record CanonicalRecord `json:"record"`

	// LeafHash is SHA-256("TXLEAF|v1|" || CanonicalBytes(record)).
	LeafHash []byte `json:"leafHash"`

	// BucketID identifies the logical bucket window containing the record.
	BucketID BucketID `json:"bucketId"`

	// ParticipantID identifies the participant claiming the commitment.
	ParticipantID string `json:"participantId"`

	// Scope identifies the ledger time window [From, To).
	Scope Scope `json:"scope"`

	// Generation is the participant commitment generation (e.g. "g1").
	Generation string `json:"generation,omitempty"`

	// CanonicalVersion is the frozen canonical record version ("v1").
	CanonicalVersion string `json:"canonicalVersion"`

	// AlgorithmVersion is the frozen Merkle algorithm version ("merkle-v1").
	AlgorithmVersion string `json:"algorithmVersion"`

	// BucketPath contains sibling steps from leaf hash to bucket root.
	BucketPath []ProofStep `json:"bucketPath"`

	// BucketRoot is the expected Merkle root of the bucket.
	BucketRoot []byte `json:"bucketRoot"`

	// GlobalPath contains sibling steps from bucket root to global root.
	GlobalPath []ProofStep `json:"globalPath"`

	// ExpectedRoot is the expected global Merkle root.
	ExpectedRoot []byte `json:"expectedRoot"`

	// GenerationMetrics contains non-cryptographic performance telemetry.
	GenerationMetrics ProofGenerationMetrics `json:"generationMetrics"`
}

// SerializedBytes returns the deterministic JSON representation of the proof.
func (p IntegrityProof) SerializedBytes() ([]byte, error) {
	return json.Marshal(p)
}

// DeserializeProof unmarshals and validates an IntegrityProof.
func DeserializeProof(data []byte) (IntegrityProof, error) {
	var p IntegrityProof
	if err := json.Unmarshal(data, &p); err != nil {
		return IntegrityProof{}, fmt.Errorf("%w: unmarshal proof: %v", ErrInvalidProof, err)
	}
	if err := p.Validate(); err != nil {
		return IntegrityProof{}, err
	}
	return p, nil
}

// Validate checks the structural validity of the proof fields.
func (p IntegrityProof) Validate() error {
	if err := ValidateCommitmentVersions(p.CanonicalVersion, p.AlgorithmVersion); err != nil {
		return fmt.Errorf("%w: %v", ErrProofIncompatibleVersion, err)
	}
	if p.ParticipantID == "" {
		return fmt.Errorf("%w: participant ID is required", ErrInvalidProof)
	}
	if err := p.Scope.Validate(); err != nil {
		return fmt.Errorf("%w: invalid scope: %v", ErrInvalidProof, err)
	}
	if err := p.BucketID.Validate(); err != nil {
		return fmt.Errorf("%w: invalid bucket ID: %v", ErrInvalidProof, err)
	}
	if p.BucketID.Partition != p.ParticipantID {
		return fmt.Errorf("%w: bucket partition %q does not match participant %q", ErrProofBucketMismatch, p.BucketID.Partition, p.ParticipantID)
	}
	if len(p.LeafHash) != sha256.Size {
		return fmt.Errorf("%w: leaf hash must be %d bytes, got %d", ErrInvalidProof, sha256.Size, len(p.LeafHash))
	}
	if len(p.BucketRoot) != sha256.Size {
		return fmt.Errorf("%w: bucket root must be %d bytes, got %d", ErrInvalidProof, sha256.Size, len(p.BucketRoot))
	}
	if len(p.ExpectedRoot) != sha256.Size {
		return fmt.Errorf("%w: expected root must be %d bytes, got %d", ErrInvalidProof, sha256.Size, len(p.ExpectedRoot))
	}
	for i, step := range p.BucketPath {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("%w: bucket path step %d: %v", ErrProofMalformedPath, i, err)
		}
	}
	for i, step := range p.GlobalPath {
		if err := step.Validate(); err != nil {
			return fmt.Errorf("%w: global path step %d: %v", ErrProofMalformedPath, i, err)
		}
	}
	return nil
}

// IntegrityEngine provides methods for generating and verifying cryptographic proofs
// of record inclusion in Merkle reconciliation roots.
type IntegrityEngine struct{}

// NewIntegrityEngine constructs an IntegrityEngine.
func NewIntegrityEngine() *IntegrityEngine {
	return &IntegrityEngine{}
}

// GenerateProof produces an IntegrityProof for the target record using the participant's
// committed state for the given scope.
func (e *IntegrityEngine) GenerateProof(ctx context.Context, participant ReconciliationParticipant, scope Scope, target CanonicalRecord) (IntegrityProof, error) {
	if participant == nil {
		return IntegrityProof{}, errors.New("reconciliation participant is required")
	}
	if err := ctx.Err(); err != nil {
		return IntegrityProof{}, err
	}
	scope = scope.Normalize()
	if err := scope.Validate(); err != nil {
		return IntegrityProof{}, err
	}

	// Fast-path: access internal snapshot state directly where available
	if mp, ok := participant.(*MemoryParticipant); ok {
		state, generation, _, err := mp.state(ctx, scope)
		if err != nil {
			return IntegrityProof{}, err
		}
		return GenerateProofFromState(state, mp.participantID, generation, target)
	}
	if rp, ok := participant.(*RepositoryParticipant); ok {
		state, generation, _, err := rp.state(ctx, scope)
		if err != nil {
			return IntegrityProof{}, err
		}
		return GenerateProofFromState(state, rp.participantID, generation, target)
	}

	// Fallback: general participant traversal using ReconciliationParticipant API
	return e.generateProofByTraversal(ctx, participant, scope, target)
}

// GenerateProofByOperationID locates the target record by OperationID and generates its proof.
func (e *IntegrityEngine) GenerateProofByOperationID(ctx context.Context, participant ReconciliationParticipant, scope Scope, opID uuid.UUID) (IntegrityProof, error) {
	if participant == nil {
		return IntegrityProof{}, errors.New("reconciliation participant is required")
	}
	if err := ctx.Err(); err != nil {
		return IntegrityProof{}, err
	}
	scope = scope.Normalize()
	if err := scope.Validate(); err != nil {
		return IntegrityProof{}, err
	}

	if mp, ok := participant.(*MemoryParticipant); ok {
		mp.mu.RLock()
		records := cloneCanonicalRecords(mp.records)
		mp.mu.RUnlock()
		for _, r := range records {
			if r.OperationID == opID && scopeContains(scope, r.OccurredAt) {
				return e.GenerateProof(ctx, participant, scope, r)
			}
		}
		return IntegrityProof{}, fmt.Errorf("%w: operation_id %s in scope", ErrRecordNotFound, opID)
	}

	rootRes, err := participant.GetRoot(ctx, scope)
	if err != nil {
		return IntegrityProof{}, err
	}
	if rootRes.Region.IsZero() {
		return IntegrityProof{}, fmt.Errorf("%w: operation_id %s (empty commitment)", ErrRecordNotFound, opID)
	}

	// Search via BFS or leaf buckets
	var foundRecord *CanonicalRecord
	queue := []NodeRef{rootRes.Ref}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		children, err := participant.GetChildren(ctx, curr)
		if err != nil {
			return IntegrityProof{}, err
		}
		if len(children) == 0 {
			bucketID, err := participant.GetBucketID(ctx, curr)
			if err != nil {
				return IntegrityProof{}, err
			}
			records, err := participant.GetRecords(ctx, BucketRef{
				ParticipantID: curr.ParticipantID,
				ScopeID:       curr.ScopeID,
				Generation:    curr.Generation,
				Key:           bucketID.String(),
			})
			if err != nil {
				return IntegrityProof{}, err
			}
			for _, r := range records {
				if r.OperationID == opID {
					target := r
					foundRecord = &target
					break
				}
			}
			if foundRecord != nil {
				break
			}
		} else {
			for _, c := range children {
				queue = append(queue, c.Ref)
			}
		}
	}

	if foundRecord == nil {
		return IntegrityProof{}, fmt.Errorf("%w: operation_id %s", ErrRecordNotFound, opID)
	}
	return e.GenerateProof(ctx, participant, scope, *foundRecord)
}

// GenerateProofFromState constructs an IntegrityProof directly from an IncrementalCommitmentState.
// It avoids recomputing tree hashes by utilizing pre-maintained tree levels.
func GenerateProofFromState(state IncrementalCommitmentState, participantID string, generation string, target CanonicalRecord) (IntegrityProof, error) {
	startTime := time.Now()

	if err := validateIncrementalState(state); err != nil {
		return IntegrityProof{}, err
	}
	if participantID == "" {
		participantID = state.Partition
	}

	target = target.Normalize()
	if !scopeContains(state.Scope, target.OccurredAt) {
		return IntegrityProof{}, fmt.Errorf("%w: record timestamp %v outside scope", ErrRecordOutsideScope, target.OccurredAt)
	}

	targetBucketID, err := BucketForRecord(target, state.Partition, state.BucketWidth)
	if err != nil {
		return IntegrityProof{}, err
	}

	targetBucketIndex := -1
	for i, b := range state.Buckets {
		if b.ID.Equal(targetBucketID) {
			targetBucketIndex = i
			break
		}
	}
	if targetBucketIndex == -1 {
		return IntegrityProof{}, fmt.Errorf("%w: bucket %s not found in state", ErrBucketNotFound, targetBucketID)
	}

	bucketRecords := state.BucketRecords[targetBucketIndex]
	orderedRecords := SortRecords(bucketRecords)

	recordIndex := -1
	for i, r := range orderedRecords {
		if r.OperationID == target.OperationID {
			if !bytesEqualCanonical(r, target) {
				return IntegrityProof{}, fmt.Errorf("%w: record with operation_id %s differs from target record", ErrProofRecordMismatch, target.OperationID)
			}
			recordIndex = i
			break
		}
	}
	if recordIndex == -1 {
		return IntegrityProof{}, fmt.Errorf("%w: record %s not found in bucket %s", ErrRecordNotFound, target.OperationID, targetBucketID)
	}

	leafHash, err := LeafHash(target)
	if err != nil {
		return IntegrityProof{}, err
	}

	// 1. Build BucketPath: leaf to bucket root
	leaves, err := LeafHashes(orderedRecords)
	if err != nil {
		return IntegrityProof{}, err
	}

	bucketPath := make([]ProofStep, 0)
	currentLevel := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		currentLevel[i] = hashCopy(leaf)
	}

	currIdx := recordIndex
	for len(currentLevel) > 1 {
		if currIdx%2 == 0 {
			if currIdx+1 < len(currentLevel) {
				bucketPath = append(bucketPath, ProofStep{
					Order: SiblingRight,
					Hash:  hashCopy(currentLevel[currIdx+1]),
				})
			} else {
				bucketPath = append(bucketPath, ProofStep{
					Order: SiblingPromoted,
					Hash:  nil,
				})
			}
		} else {
			bucketPath = append(bucketPath, ProofStep{
				Order: SiblingLeft,
				Hash:  hashCopy(currentLevel[currIdx-1]),
			})
		}

		nextLevel := make([][]byte, 0, (len(currentLevel)+1)/2)
		for i := 0; i < len(currentLevel); i += 2 {
			if i+1 == len(currentLevel) {
				nextLevel = append(nextLevel, hashCopy(currentLevel[i]))
			} else {
				nextLevel = append(nextLevel, InternalNodeHash(currentLevel[i], currentLevel[i+1]))
			}
		}
		currentLevel = nextLevel
		currIdx = currIdx / 2
	}

	bucketRoot := currentLevel[0]

	// 2. Build GlobalPath: bucket root to global root using state.Levels
	globalPath := make([]ProofStep, 0)
	currBucketIdx := targetBucketIndex

	for level := 0; level < len(state.Levels)-1; level++ {
		levelNodes := state.Levels[level]
		if len(levelNodes) <= 1 {
			break
		}

		if currBucketIdx%2 == 0 {
			if currBucketIdx+1 < len(levelNodes) {
				globalPath = append(globalPath, ProofStep{
					Order: SiblingRight,
					Hash:  hashCopy(levelNodes[currBucketIdx+1]),
				})
			} else {
				globalPath = append(globalPath, ProofStep{
					Order: SiblingPromoted,
					Hash:  nil,
				})
			}
		} else {
			globalPath = append(globalPath, ProofStep{
				Order: SiblingLeft,
				Hash:  hashCopy(levelNodes[currBucketIdx-1]),
			})
		}
		currBucketIdx = currBucketIdx / 2
	}

	duration := time.Since(startTime)

	proof := IntegrityProof{
		Record:           target,
		LeafHash:         leafHash,
		BucketID:         targetBucketID,
		ParticipantID:    participantID,
		Scope:            state.Scope,
		Generation:       generation,
		CanonicalVersion: CanonicalVersion,
		AlgorithmVersion: MerkleAlgorithmVersion,
		BucketPath:       bucketPath,
		BucketRoot:       hashCopy(bucketRoot),
		GlobalPath:       globalPath,
		ExpectedRoot:     hashCopy(state.Root),
		GenerationMetrics: ProofGenerationMetrics{
			Duration:           duration,
			BucketSiblingCount: len(bucketPath),
			GlobalSiblingCount: len(globalPath),
			TotalSiblingCount:  len(bucketPath) + len(globalPath),
		},
	}

	if b, err := json.Marshal(proof); err == nil {
		proof.GenerationMetrics.SerializedSizeBytes = len(b)
	}

	return proof, nil
}

func (e *IntegrityEngine) generateProofByTraversal(ctx context.Context, participant ReconciliationParticipant, scope Scope, target CanonicalRecord) (IntegrityProof, error) {
	startTime := time.Now()

	rootRes, err := participant.GetRoot(ctx, scope)
	if err != nil {
		return IntegrityProof{}, err
	}
	target = target.Normalize()
	if !scopeContains(scope, target.OccurredAt) {
		return IntegrityProof{}, fmt.Errorf("%w: record %v outside scope %v", ErrRecordOutsideScope, target.OccurredAt, scope)
	}

	curr := rootRes.Ref
	var globalStepsDown []ProofStep

	for {
		children, err := participant.GetChildren(ctx, curr)
		if err != nil {
			return IntegrityProof{}, err
		}
		if len(children) == 0 {
			break
		}
		if len(children) == 1 {
			globalStepsDown = append(globalStepsDown, ProofStep{
				Order: SiblingPromoted,
				Hash:  nil,
			})
			curr = children[0].Ref
			continue
		}

		child0Contains := scopeContains(Scope{From: children[0].Region.Start, To: children[0].Region.End}, target.OccurredAt)
		child1Contains := scopeContains(Scope{From: children[1].Region.Start, To: children[1].Region.End}, target.OccurredAt)

		if child0Contains && !child1Contains {
			globalStepsDown = append(globalStepsDown, ProofStep{
				Order: SiblingRight,
				Hash:  hashCopy(children[1].Hash),
			})
			curr = children[0].Ref
		} else if child1Contains && !child0Contains {
			globalStepsDown = append(globalStepsDown, ProofStep{
				Order: SiblingLeft,
				Hash:  hashCopy(children[0].Hash),
			})
			curr = children[1].Ref
		} else {
			// Fallback: pick child0 if instant is strictly before children[1].Region.Start
			if target.OccurredAt.Before(children[1].Region.Start) {
				globalStepsDown = append(globalStepsDown, ProofStep{
					Order: SiblingRight,
					Hash:  hashCopy(children[1].Hash),
				})
				curr = children[0].Ref
			} else {
				globalStepsDown = append(globalStepsDown, ProofStep{
					Order: SiblingLeft,
					Hash:  hashCopy(children[0].Hash),
				})
				curr = children[1].Ref
			}
		}
	}

	bucketID, err := participant.GetBucketID(ctx, curr)
	if err != nil {
		return IntegrityProof{}, err
	}

	records, err := participant.GetRecords(ctx, BucketRef{
		ParticipantID: curr.ParticipantID,
		ScopeID:       curr.ScopeID,
		Generation:    curr.Generation,
		Key:           bucketID.String(),
	})
	if err != nil {
		return IntegrityProof{}, err
	}

	orderedRecords := SortRecords(records)
	recordIndex := -1
	for i, r := range orderedRecords {
		if r.OperationID == target.OperationID {
			if !bytesEqualCanonical(r, target) {
				return IntegrityProof{}, fmt.Errorf("%w: record with operation_id %s differs from target record", ErrProofRecordMismatch, target.OperationID)
			}
			recordIndex = i
			break
		}
	}
	if recordIndex == -1 {
		return IntegrityProof{}, fmt.Errorf("%w: record %s not found in bucket %s", ErrRecordNotFound, target.OperationID, bucketID)
	}

	leafHash, err := LeafHash(target)
	if err != nil {
		return IntegrityProof{}, err
	}

	leaves, err := LeafHashes(orderedRecords)
	if err != nil {
		return IntegrityProof{}, err
	}

	bucketPath := make([]ProofStep, 0)
	currentLevel := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		currentLevel[i] = hashCopy(leaf)
	}

	currIdx := recordIndex
	for len(currentLevel) > 1 {
		if currIdx%2 == 0 {
			if currIdx+1 < len(currentLevel) {
				bucketPath = append(bucketPath, ProofStep{
					Order: SiblingRight,
					Hash:  hashCopy(currentLevel[currIdx+1]),
				})
			} else {
				bucketPath = append(bucketPath, ProofStep{
					Order: SiblingPromoted,
					Hash:  nil,
				})
			}
		} else {
			bucketPath = append(bucketPath, ProofStep{
				Order: SiblingLeft,
				Hash:  hashCopy(currentLevel[currIdx-1]),
			})
		}

		nextLevel := make([][]byte, 0, (len(currentLevel)+1)/2)
		for i := 0; i < len(currentLevel); i += 2 {
			if i+1 == len(currentLevel) {
				nextLevel = append(nextLevel, hashCopy(currentLevel[i]))
			} else {
				nextLevel = append(nextLevel, InternalNodeHash(currentLevel[i], currentLevel[i+1]))
			}
		}
		currentLevel = nextLevel
		currIdx = currIdx / 2
	}
	bucketRoot := currentLevel[0]

	// Reverse globalStepsDown to obtain bottom-up GlobalPath
	globalPath := make([]ProofStep, len(globalStepsDown))
	for i := range globalStepsDown {
		globalPath[len(globalStepsDown)-1-i] = globalStepsDown[i]
	}

	duration := time.Since(startTime)

	proof := IntegrityProof{
		Record:           target,
		LeafHash:         leafHash,
		BucketID:         bucketID,
		ParticipantID:    rootRes.Ref.ParticipantID,
		Scope:            scope,
		Generation:       rootRes.Ref.Generation,
		CanonicalVersion: CanonicalVersion,
		AlgorithmVersion: MerkleAlgorithmVersion,
		BucketPath:       bucketPath,
		BucketRoot:       hashCopy(bucketRoot),
		GlobalPath:       globalPath,
		ExpectedRoot:     hashCopy(rootRes.Root),
		GenerationMetrics: ProofGenerationMetrics{
			Duration:           duration,
			BucketSiblingCount: len(bucketPath),
			GlobalSiblingCount: len(globalPath),
			TotalSiblingCount:  len(bucketPath) + len(globalPath),
		},
	}

	if b, err := json.Marshal(proof); err == nil {
		proof.GenerationMetrics.SerializedSizeBytes = len(b)
	}

	return proof, nil
}

type verifyConfig struct {
	expectedRoot          []byte
	expectedParticipantID string
	expectedScope         *Scope
}

// VerifyOption customizes the verification criteria for an IntegrityProof.
type VerifyOption func(*verifyConfig)

// WithExpectedRoot configures an expected global root to verify against.
func WithExpectedRoot(root []byte) VerifyOption {
	return func(c *verifyConfig) {
		c.expectedRoot = hashCopy(root)
	}
}

// WithExpectedParticipant configures an expected participant identity to verify against.
func WithExpectedParticipant(participantID string) VerifyOption {
	return func(c *verifyConfig) {
		c.expectedParticipantID = participantID
	}
}

// WithExpectedScope configures an expected scope to verify against.
func WithExpectedScope(scope Scope) VerifyOption {
	return func(c *verifyConfig) {
		s := scope.Normalize()
		c.expectedScope = &s
	}
}

// VerifyProof verifies that an IntegrityProof is cryptographically and logically valid.
func (e *IntegrityEngine) VerifyProof(ctx context.Context, proof IntegrityProof, opts ...VerifyOption) (VerificationResult, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return VerificationResult{}, err
	}

	var cfg verifyConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	// 1. Basic structural validation
	if err := proof.Validate(); err != nil {
		return VerificationResult{}, err
	}

	// 2. Validate external options if specified
	if cfg.expectedParticipantID != "" && proof.ParticipantID != cfg.expectedParticipantID {
		return VerificationResult{}, fmt.Errorf("%w: expected participant %q, proof has %q", ErrProofParticipantMismatch, cfg.expectedParticipantID, proof.ParticipantID)
	}
	if cfg.expectedScope != nil && !scopeEqual(proof.Scope, *cfg.expectedScope) {
		return VerificationResult{}, fmt.Errorf("%w: expected scope %v, proof has %v", ErrProofScopeMismatch, *cfg.expectedScope, proof.Scope)
	}
	if len(cfg.expectedRoot) > 0 && !bytes.Equal(proof.ExpectedRoot, cfg.expectedRoot) {
		return VerificationResult{}, fmt.Errorf("%w: expected root %x, proof has %x", ErrProofRootMismatch, cfg.expectedRoot, proof.ExpectedRoot)
	}

	// 3. Validate Scope contains record and bucket
	if !scopeContains(proof.Scope, proof.Record.OccurredAt) {
		return VerificationResult{}, fmt.Errorf("%w: record occurred_at %v is outside scope %v", ErrProofScopeMismatch, proof.Record.OccurredAt, proof.Scope)
	}
	if !scopeContains(proof.Scope, proof.BucketID.Start) {
		return VerificationResult{}, fmt.Errorf("%w: bucket start %v is outside scope %v", ErrProofScopeMismatch, proof.BucketID.Start, proof.Scope)
	}

	// 4. Validate record maps to claimed bucket
	mappedBucket, err := BucketForRecord(proof.Record, proof.BucketID.Partition, proof.BucketID.Width)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("%w: mapping record to bucket: %v", ErrInvalidProof, err)
	}
	if !mappedBucket.Equal(proof.BucketID) {
		return VerificationResult{}, fmt.Errorf("%w: record maps to %s, proof claims %s", ErrProofBucketMismatch, mappedBucket, proof.BucketID)
	}

	// 5. Validate LeafHash against Record
	computedLeafHash, err := LeafHash(proof.Record)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("%w: compute leaf hash: %v", ErrInvalidProof, err)
	}
	if !bytes.Equal(computedLeafHash, proof.LeafHash) {
		return VerificationResult{}, fmt.Errorf("%w: computed leaf hash %x != proof leaf hash %x", ErrProofRecordMismatch, computedLeafHash, proof.LeafHash)
	}

	stepsEvaluated := 0

	// 6. Reconstruct Bucket Root
	currHash := hashCopy(proof.LeafHash)
	for i, step := range proof.BucketPath {
		stepsEvaluated++
		switch step.Order {
		case SiblingLeft:
			currHash = InternalNodeHash(step.Hash, currHash)
		case SiblingRight:
			currHash = InternalNodeHash(currHash, step.Hash)
		case SiblingPromoted:
			// currHash remains unchanged
		default:
			return VerificationResult{}, fmt.Errorf("%w: invalid bucket step %d order %q", ErrProofMalformedPath, i, step.Order)
		}
	}
	reconstructedBucketRoot := currHash
	if !bytes.Equal(reconstructedBucketRoot, proof.BucketRoot) {
		return VerificationResult{}, fmt.Errorf("%w: reconstructed %x != expected bucket root %x", ErrProofBucketRootMismatch, reconstructedBucketRoot, proof.BucketRoot)
	}

	// 7. Reconstruct Global Root
	for i, step := range proof.GlobalPath {
		stepsEvaluated++
		switch step.Order {
		case SiblingLeft:
			currHash = InternalNodeHash(step.Hash, currHash)
		case SiblingRight:
			currHash = InternalNodeHash(currHash, step.Hash)
		case SiblingPromoted:
			// currHash remains unchanged
		default:
			return VerificationResult{}, fmt.Errorf("%w: invalid global step %d order %q", ErrProofMalformedPath, i, step.Order)
		}
	}
	reconstructedRoot := currHash
	if !bytes.Equal(reconstructedRoot, proof.ExpectedRoot) {
		return VerificationResult{}, fmt.Errorf("%w: reconstructed %x != expected root %x", ErrProofRootMismatch, reconstructedRoot, proof.ExpectedRoot)
	}

	duration := time.Since(start)

	return VerificationResult{
		Valid:                   true,
		ReconstructedBucketRoot: reconstructedBucketRoot,
		ReconstructedRoot:       reconstructedRoot,
		Metrics: ProofVerificationMetrics{
			Duration:       duration,
			StepsEvaluated: stepsEvaluated,
			RootVerified:   true,
		},
	}, nil
}

// GenerateProof is a top-level package function providing the default engine proof generation.
func GenerateProof(ctx context.Context, participant ReconciliationParticipant, scope Scope, target CanonicalRecord) (IntegrityProof, error) {
	return NewIntegrityEngine().GenerateProof(ctx, participant, scope, target)
}

// VerifyProof is a top-level package function providing default engine proof verification.
func VerifyProof(ctx context.Context, proof IntegrityProof, opts ...VerifyOption) (VerificationResult, error) {
	return NewIntegrityEngine().VerifyProof(ctx, proof, opts...)
}
