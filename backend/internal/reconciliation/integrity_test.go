package reconciliation_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/reconciliation"
)

func createIntegrityRecords(base time.Time, count int, totalDuration time.Duration) []reconciliation.CanonicalRecord {
	records := make([]reconciliation.CanonicalRecord, count)
	for i := 0; i < count; i++ {
		opID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("op-integrity-%d", i)))
		payID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("pay-integrity-%d", i)))
		accID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("acc-integrity-%d", i%5)))

		entryType := "DEBIT"
		if i%2 == 1 {
			entryType = "CREDIT"
		}

		offsetNs := (int64(i) * int64(totalDuration)) / int64(count)
		occurredAt := base.Add(time.Duration(offsetNs) + 10*time.Second).UTC()

		records[i] = reconciliation.CanonicalRecord{
			OperationID: opID,
			PaymentID:   payID,
			AccountID:   accID,
			EntryType:   entryType,
			AmountPaise: int64((i+1)*2500 + 45),
			Currency:    "INR",
			OccurredAt:  occurredAt,
		}
	}
	return records
}

func cloneTestRecords(records []reconciliation.CanonicalRecord) []reconciliation.CanonicalRecord {
	cloned := make([]reconciliation.CanonicalRecord, len(records))
	copy(cloned, records)
	return cloned
}

func buildTestParticipant(t *testing.T, participantID string, bucketWidth time.Duration, records []reconciliation.CanonicalRecord) *reconciliation.MemoryParticipant {
	t.Helper()
	p, err := reconciliation.NewMemoryParticipant(participantID, participantID, bucketWidth, records)
	if err != nil {
		t.Fatalf("build test participant: %v", err)
	}
	return p
}

// buildTrustedContext constructs a ProofVerificationContext from independent trusted sources:
// participant.GetRoot (providing ParticipantID, actual commitment Generation, and expected Root),
// independently known Scope, independently computed BucketID via BucketForRecord, and known
// supported canonical and algorithm versions.
//
// CRITICAL SECURITY INVARIANT: No fields are ever copied or derived from an IntegrityProof.
func buildTrustedContext(t *testing.T, ctx context.Context, participant reconciliation.ReconciliationParticipant, scope reconciliation.Scope, bucketWidth time.Duration, target reconciliation.CanonicalRecord) reconciliation.ProofVerificationContext {
	t.Helper()
	rootRes, err := participant.GetRoot(ctx, scope)
	if err != nil {
		t.Fatalf("failed to get root for trusted context: %v", err)
	}
	participantID := rootRes.Ref.ParticipantID
	expectedBucket, err := reconciliation.BucketForRecord(target, participantID, bucketWidth)
	if err != nil {
		t.Fatalf("failed to calculate bucket for trusted context: %v", err)
	}
	return reconciliation.ProofVerificationContext{
		ParticipantID:    participantID,
		Scope:            scope.Normalize(),
		BucketID:         expectedBucket,
		Generation:       rootRes.Ref.Generation,
		CanonicalVersion: reconciliation.CanonicalVersion,
		AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		ExpectedRoot:     rootRes.Root,
	}
}

// TestGenerateProof verifies proof generation produces a valid, complete typed proof structure with mandatory Generation.
func TestGenerateProof(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	records := createIntegrityRecords(base, 24, 24*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 6*time.Hour, records)

	target := records[7]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof failed: %v", err)
	}

	if proof.Record.OperationID != target.OperationID {
		t.Errorf("record operation_id = %s, want %s", proof.Record.OperationID, target.OperationID)
	}
	if len(proof.LeafHash) != 32 {
		t.Errorf("leaf hash len = %d, want 32", len(proof.LeafHash))
	}
	if proof.ParticipantID != "BANK-A" {
		t.Errorf("participant = %q, want BANK-A", proof.ParticipantID)
	}
	if proof.Generation == "" {
		t.Errorf("proof generation is empty, want non-empty mandatory generation")
	}
	if proof.CanonicalVersion != reconciliation.CanonicalVersion {
		t.Errorf("canonical version = %q, want %q", proof.CanonicalVersion, reconciliation.CanonicalVersion)
	}
	if proof.AlgorithmVersion != reconciliation.MerkleAlgorithmVersion {
		t.Errorf("algorithm version = %q, want %q", proof.AlgorithmVersion, reconciliation.MerkleAlgorithmVersion)
	}
	if len(proof.BucketRoot) != 32 {
		t.Errorf("bucket root len = %d, want 32", len(proof.BucketRoot))
	}
	if len(proof.ExpectedRoot) != 32 {
		t.Errorf("expected root len = %d, want 32", len(proof.ExpectedRoot))
	}
	if proof.GenerationMetrics.Duration < 0 {
		t.Errorf("invalid generation duration: %v", proof.GenerationMetrics.Duration)
	}
	if proof.GenerationMetrics.TotalSiblingCount != len(proof.BucketPath)+len(proof.GlobalPath) {
		t.Errorf("total sibling count mismatch: %d != %d", proof.GenerationMetrics.TotalSiblingCount, len(proof.BucketPath)+len(proof.GlobalPath))
	}
	if proof.GenerationMetrics.SerializedSizeBytes <= 0 {
		t.Errorf("serialized size bytes = %d, want > 0", proof.GenerationMetrics.SerializedSizeBytes)
	}

	// Also verify GenerateProofByOperationID produces equivalent proof
	byOpProof, err := reconciliation.NewIntegrityEngine().GenerateProofByOperationID(ctx, participant, scope, target.OperationID)
	if err != nil {
		t.Fatalf("GenerateProofByOperationID failed: %v", err)
	}
	if !bytes.Equal(proof.LeafHash, byOpProof.LeafHash) || !bytes.Equal(proof.ExpectedRoot, byOpProof.ExpectedRoot) {
		t.Fatalf("GenerateProofByOperationID did not match GenerateProof")
	}
}

// TestVerifyProof verifies positive proof verification against trusted verification context.
func TestVerifyProof(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	records := createIntegrityRecords(base, 24, 24*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 6*time.Hour, records)

	target := records[10]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	// 1. Direct verification using ProofVerificationContext from independent trusted sources
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 6*time.Hour, target)
	res, err := reconciliation.VerifyProof(ctx, proof, trustedCtx)
	if err != nil {
		t.Fatalf("VerifyProof failed: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid proof")
	}
	if !bytes.Equal(res.ReconstructedBucketRoot, proof.BucketRoot) {
		t.Errorf("reconstructed bucket root does not match proof bucket root")
	}
	if !bytes.Equal(res.ReconstructedRoot, proof.ExpectedRoot) {
		t.Errorf("reconstructed global root does not match proof expected root")
	}
	if res.Metrics.Duration < 0 {
		t.Errorf("invalid verification duration: %v", res.Metrics.Duration)
	}
	if res.Metrics.StepsEvaluated != len(proof.BucketPath)+len(proof.GlobalPath) {
		t.Errorf("steps evaluated = %d, want %d", res.Metrics.StepsEvaluated, len(proof.BucketPath)+len(proof.GlobalPath))
	}
	if !res.Metrics.RootVerified {
		t.Errorf("rootVerified = false, want true")
	}

	// 2. Verification using full typed options from independent trusted sources
	resOpt, err := reconciliation.VerifyProofWithOptions(
		ctx,
		proof,
		reconciliation.WithExpectedParticipant(trustedCtx.ParticipantID),
		reconciliation.WithExpectedScope(scope),
		reconciliation.WithExpectedBucket(trustedCtx.BucketID),
		reconciliation.WithExpectedGeneration(trustedCtx.Generation),
		reconciliation.WithExpectedCanonicalVersion(reconciliation.CanonicalVersion),
		reconciliation.WithExpectedAlgorithmVersion(reconciliation.MerkleAlgorithmVersion),
		reconciliation.WithExpectedRoot(trustedCtx.ExpectedRoot),
	)
	if err != nil {
		t.Fatalf("VerifyProofWithOptions failed: %v", err)
	}
	if !resOpt.Valid {
		t.Errorf("expected valid proof with options")
	}
}

// TestVerifyModifiedRecordFails verifies tampering with record fields fails verification.
func TestVerifyModifiedRecordFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[3])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[3])

	// Mutate AmountPaise
	tampered := proof
	tampered.Record.AmountPaise += 1000
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofRecordMismatch) {
		t.Errorf("expected ErrProofRecordMismatch, got %v", err)
	}

	// Mutate Currency
	tampered = proof
	tampered.Record.Currency = "USD"
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofRecordMismatch) {
		t.Errorf("expected ErrProofRecordMismatch for currency change, got %v", err)
	}

	// Mutate EntryType
	tampered = proof
	if tampered.Record.EntryType == "DEBIT" {
		tampered.Record.EntryType = "CREDIT"
	} else {
		tampered.Record.EntryType = "DEBIT"
	}
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofRecordMismatch) {
		t.Errorf("expected ErrProofRecordMismatch for entry_type change, got %v", err)
	}
}

// TestVerifyModifiedLeafFails verifies tampering with the leaf hash fails verification.
func TestVerifyModifiedLeafFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[2])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[2])

	tampered := proof
	tampered.LeafHash = append([]byte(nil), proof.LeafHash...)
	tampered.LeafHash[0] ^= 0xFF

	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofRecordMismatch) {
		t.Errorf("expected ErrProofRecordMismatch, got %v", err)
	}
}

// TestVerifyModifiedSiblingFails verifies tampering with any sibling hash fails verification.
func TestVerifyModifiedSiblingFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	records := createIntegrityRecords(base, 24, 24*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 6*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[5])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 6*time.Hour, records[5])

	// 1. Mutate BucketPath sibling
	if len(proof.BucketPath) > 0 {
		tampered := proof
		tampered.BucketPath = make([]reconciliation.ProofStep, len(proof.BucketPath))
		copy(tampered.BucketPath, proof.BucketPath)
		for i := range tampered.BucketPath {
			if len(tampered.BucketPath[i].Hash) > 0 {
				tampered.BucketPath[i].Hash = append([]byte(nil), tampered.BucketPath[i].Hash...)
				tampered.BucketPath[i].Hash[0] ^= 0xFF
				break
			}
		}
		if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofBucketRootMismatch) {
			t.Errorf("expected ErrProofBucketRootMismatch, got %v", err)
		}
	}

	// 2. Mutate GlobalPath sibling
	if len(proof.GlobalPath) > 0 {
		tampered := proof
		tampered.GlobalPath = make([]reconciliation.ProofStep, len(proof.GlobalPath))
		copy(tampered.GlobalPath, proof.GlobalPath)
		for i := range tampered.GlobalPath {
			if len(tampered.GlobalPath[i].Hash) > 0 {
				tampered.GlobalPath[i].Hash = append([]byte(nil), tampered.GlobalPath[i].Hash...)
				tampered.GlobalPath[i].Hash[0] ^= 0xFF
				break
			}
		}
		if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofRootMismatch) {
			t.Errorf("expected ErrProofRootMismatch, got %v", err)
		}
	}
}

// TestVerifyWrongRootFails verifies proof verification rejects an incorrect expected root.
func TestVerifyWrongRootFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[1])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[1])

	// Case A: proof.ExpectedRoot is modified
	tampered := proof
	tampered.ExpectedRoot = append([]byte(nil), proof.ExpectedRoot...)
	tampered.ExpectedRoot[0] ^= 0xFF
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofRootMismatch) {
		t.Errorf("expected ErrProofRootMismatch when proof.ExpectedRoot is wrong, got %v", err)
	}

	// Case B: Trusted context has wrong expected root
	wrongRootCtx := trustedCtx
	wrongRootCtx.ExpectedRoot = append([]byte(nil), trustedCtx.ExpectedRoot...)
	wrongRootCtx.ExpectedRoot[0] ^= 0xEE
	if _, err := reconciliation.VerifyProof(ctx, proof, wrongRootCtx); err == nil || !errors.Is(err, reconciliation.ErrProofRootMismatch) {
		t.Errorf("expected ErrProofRootMismatch when context ExpectedRoot differs, got %v", err)
	}
}

// TestVerifyWrongBucketFails verifies proof verification rejects mismatched bucket identities.
func TestVerifyWrongBucketFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	// Case A: Mutate BucketID start time
	tampered := proof
	tampered.BucketID.Start = tampered.BucketID.Start.Add(4 * time.Hour)
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
		t.Errorf("expected ErrProofBucketMismatch when bucket start time is wrong, got %v", err)
	}

	// Case B: Mutate BucketID partition
	tampered = proof
	tampered.BucketID.Partition = "BANK-OTHER"
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
		t.Errorf("expected ErrProofBucketMismatch when bucket partition is wrong, got %v", err)
	}

	// Case C: Mutate BucketID width so record maps to a different bucket start
	tampered = proof
	tampered.BucketID.Width = 5 * time.Second
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
		t.Errorf("expected ErrProofBucketMismatch when bucket width is wrong, got %v", err)
	}
}

// TestVerifyWrongScopeFails verifies verification fails when verified against the wrong scope.
func TestVerifyWrongScopeFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	// Case A: proof.Scope modified so record is outside scope
	tampered := proof
	tampered.Scope.From = proof.Record.OccurredAt.Add(time.Minute)
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofScopeMismatch) {
		t.Errorf("expected ErrProofScopeMismatch when record is outside scope, got %v", err)
	}

	// Case B: Trusted context specifies different scope
	wrongScopeCtx := trustedCtx
	wrongScopeCtx.Scope = reconciliation.Scope{From: base.Add(24 * time.Hour), To: base.Add(48 * time.Hour)}
	if _, err := reconciliation.VerifyProof(ctx, proof, wrongScopeCtx); err == nil || !errors.Is(err, reconciliation.ErrProofScopeMismatch) {
		t.Errorf("expected ErrProofScopeMismatch when context scope differs, got %v", err)
	}
}

// TestVerifyWrongParticipantFails verifies cross-participant verification fails.
func TestVerifyWrongParticipantFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	// Case A: proof.ParticipantID changed to BANK-B while bucket partition is BANK-A
	tampered := proof
	tampered.ParticipantID = "BANK-B"
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofParticipantMismatch) {
		t.Errorf("expected ErrProofParticipantMismatch when proof participant is altered, got %v", err)
	}

	// Case B: Trusted context expects BANK-B
	wrongPartCtx := trustedCtx
	wrongPartCtx.ParticipantID = "BANK-B"
	wrongPartCtx.BucketID.Partition = "BANK-B"
	if _, err := reconciliation.VerifyProof(ctx, proof, wrongPartCtx); err == nil || !errors.Is(err, reconciliation.ErrProofParticipantMismatch) {
		t.Errorf("expected ErrProofParticipantMismatch when expected participant is BANK-B, got %v", err)
	}
}

// TestVerifyWrongVersionFails verifies incompatible canonical version is rejected.
func TestVerifyWrongVersionFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	tampered := proof
	tampered.CanonicalVersion = "v2"
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofIncompatibleVersion) {
		t.Errorf("expected ErrProofIncompatibleVersion for CanonicalVersion=v2, got %v", err)
	}
}

// TestVerifyWrongAlgorithmFails verifies incompatible Merkle algorithm version is rejected.
func TestVerifyWrongAlgorithmFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	tampered := proof
	tampered.AlgorithmVersion = "merkle-v2"
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofIncompatibleVersion) {
		t.Errorf("expected ErrProofIncompatibleVersion for AlgorithmVersion=merkle-v2, got %v", err)
	}
}

// TestVerifyTruncatedProofFails verifies that missing path elements cannot verify.
func TestVerifyTruncatedProofFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	records := createIntegrityRecords(base, 24, 24*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 6*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[4])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 6*time.Hour, records[4])

	// Truncate BucketPath
	if len(proof.BucketPath) > 0 {
		tampered := proof
		tampered.BucketPath = proof.BucketPath[:len(proof.BucketPath)-1]
		if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil {
			t.Errorf("expected error when BucketPath is truncated, got nil")
		}
	}

	// Truncate GlobalPath
	if len(proof.GlobalPath) > 0 {
		tampered := proof
		tampered.GlobalPath = proof.GlobalPath[:len(proof.GlobalPath)-1]
		if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil {
			t.Errorf("expected error when GlobalPath is truncated, got nil")
		}
	}
}

// TestVerifySiblingOrderFails verifies that swapping or falsifying sibling order fails verification.
func TestVerifySiblingOrderFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	records := createIntegrityRecords(base, 24, 24*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 6*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[2])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 6*time.Hour, records[2])

	// Case A: Swap LEFT and RIGHT order
	swapped := proof
	swapped.BucketPath = make([]reconciliation.ProofStep, len(proof.BucketPath))
	copy(swapped.BucketPath, proof.BucketPath)
	foundSwap := false
	for i := range swapped.BucketPath {
		if swapped.BucketPath[i].Order == reconciliation.SiblingLeft {
			swapped.BucketPath[i].Order = reconciliation.SiblingRight
			foundSwap = true
			break
		} else if swapped.BucketPath[i].Order == reconciliation.SiblingRight {
			swapped.BucketPath[i].Order = reconciliation.SiblingLeft
			foundSwap = true
			break
		}
	}
	if foundSwap {
		if _, err := reconciliation.VerifyProof(ctx, swapped, trustedCtx); err == nil {
			t.Errorf("expected error when sibling order was swapped, got nil")
		}
	}

	// Case B: Invalid order string
	tampered := proof
	tampered.BucketPath = make([]reconciliation.ProofStep, len(proof.BucketPath))
	copy(tampered.BucketPath, proof.BucketPath)
	if len(tampered.BucketPath) > 0 {
		tampered.BucketPath[0].Order = "TOP"
		if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofMalformedPath) {
			t.Errorf("expected ErrProofMalformedPath for invalid order, got %v", err)
		}
	}
}

// TestVerifyOddTreeProof verifies proofs for odd-element Merkle trees where nodes are promoted without hashing.
func TestVerifyOddTreeProof(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// 3 buckets across 3 hours (1 bucket per hour)
	scope := reconciliation.Scope{From: base, To: base.Add(3 * time.Hour)}
	records := createIntegrityRecords(base, 9, 3*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", time.Hour, records)

	target := records[8]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof on odd tree: %v", err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, time.Hour, target)

	// Verify that proof contains at least one SiblingPromoted step
	hasPromoted := false
	for _, step := range proof.BucketPath {
		if step.Order == reconciliation.SiblingPromoted {
			hasPromoted = true
		}
	}
	for _, step := range proof.GlobalPath {
		if step.Order == reconciliation.SiblingPromoted {
			hasPromoted = true
		}
	}
	if !hasPromoted {
		t.Errorf("expected odd tree proof to contain a SiblingPromoted step")
	}

	// Positive verification
	res, err := reconciliation.VerifyProof(ctx, proof, trustedCtx)
	if err != nil {
		t.Fatalf("VerifyProof on odd tree failed: %v", err)
	}
	if !res.Valid {
		t.Errorf("expected valid odd tree proof")
	}

	// Negative case: SiblingPromoted step tampered with non-empty hash
	for i, step := range proof.GlobalPath {
		if step.Order == reconciliation.SiblingPromoted {
			tampered := proof
			tampered.GlobalPath = make([]reconciliation.ProofStep, len(proof.GlobalPath))
			copy(tampered.GlobalPath, proof.GlobalPath)
			tampered.GlobalPath[i].Hash = make([]byte, 32)
			if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofMalformedPath) {
				t.Errorf("expected ErrProofMalformedPath when promoted node has non-empty hash, got %v", err)
			}
			break
		}
	}
}

// TestProofForFirstBucket verifies proof generation and verification for the first bucket in a multi-bucket tree.
func TestProofForFirstBucket(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(5 * time.Hour)}
	records := createIntegrityRecords(base, 15, 5*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", time.Hour, records)

	target := records[0]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	if !proof.BucketID.Start.Equal(base) {
		t.Errorf("bucket start = %v, want %v", proof.BucketID.Start, base)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, time.Hour, target)
	res, err := reconciliation.VerifyProof(ctx, proof, trustedCtx)
	if err != nil || !res.Valid {
		t.Fatalf("VerifyProof for first bucket failed: %v", err)
	}
}

// TestProofForMiddleBucket verifies proof generation and verification for a middle bucket.
func TestProofForMiddleBucket(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(5 * time.Hour)}
	records := createIntegrityRecords(base, 15, 5*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", time.Hour, records)

	target := records[7]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	expectedStart := base.Add(2 * time.Hour)
	if !proof.BucketID.Start.Equal(expectedStart) {
		t.Errorf("bucket start = %v, want %v", proof.BucketID.Start, expectedStart)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, time.Hour, target)
	res, err := reconciliation.VerifyProof(ctx, proof, trustedCtx)
	if err != nil || !res.Valid {
		t.Fatalf("VerifyProof for middle bucket failed: %v", err)
	}
}

// TestProofForLastBucket verifies proof generation and verification for the last bucket.
func TestProofForLastBucket(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(5 * time.Hour)}
	records := createIntegrityRecords(base, 15, 5*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", time.Hour, records)

	target := records[14]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	expectedStart := base.Add(4 * time.Hour)
	if !proof.BucketID.Start.Equal(expectedStart) {
		t.Errorf("bucket start = %v, want %v", proof.BucketID.Start, expectedStart)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, time.Hour, target)
	res, err := reconciliation.VerifyProof(ctx, proof, trustedCtx)
	if err != nil || !res.Valid {
		t.Fatalf("VerifyProof for last bucket failed: %v", err)
	}
}

// TestProofGenerationDeterministic verifies that repeating proof generation produces byte-for-byte identical output.
func TestProofGenerationDeterministic(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	records := createIntegrityRecords(base, 48, 24*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	target := records[15]

	firstProof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof run 0: %v", err)
	}
	firstBytes, err := firstProof.SerializedBytes()
	if err != nil {
		t.Fatalf("marshal first proof: %v", err)
	}

	for run := 1; run < 10; run++ {
		proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
		if err != nil {
			t.Fatalf("GenerateProof run %d: %v", run, err)
		}

		if len(proof.BucketPath) != len(firstProof.BucketPath) {
			t.Fatalf("run %d bucket path len mismatch: %d vs %d", run, len(proof.BucketPath), len(firstProof.BucketPath))
		}
		if len(proof.GlobalPath) != len(firstProof.GlobalPath) {
			t.Fatalf("run %d global path len mismatch: %d vs %d", run, len(proof.GlobalPath), len(firstProof.GlobalPath))
		}
		for i := range proof.BucketPath {
			if proof.BucketPath[i].Order != firstProof.BucketPath[i].Order || !bytes.Equal(proof.BucketPath[i].Hash, firstProof.BucketPath[i].Hash) {
				t.Fatalf("run %d bucket path step %d differs", run, i)
			}
		}
		for i := range proof.GlobalPath {
			if proof.GlobalPath[i].Order != firstProof.GlobalPath[i].Order || !bytes.Equal(proof.GlobalPath[i].Hash, firstProof.GlobalPath[i].Hash) {
				t.Fatalf("run %d global path step %d differs", run, i)
			}
		}

		// Serialized byte-for-byte equivalence (ignoring volatile wall-clock measurements in metrics)
		proof.GenerationMetrics = firstProof.GenerationMetrics
		proofBytes, err := proof.SerializedBytes()
		if err != nil {
			t.Fatalf("marshal run %d proof: %v", run, err)
		}
		if !bytes.Equal(firstBytes, proofBytes) {
			t.Fatalf("run %d serialized proof differs from initial run", run)
		}
	}
}

// TestProofGenerationDoesNotMutateFinancialState proves that proof generation and verification
// are strictly read-only and do not mutate any ledger records, amounts, currencies, or timestamps.
func TestProofGenerationDoesNotMutateFinancialState(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	records := createIntegrityRecords(base, 30, 24*time.Hour)

	snapshotBefore := cloneTestRecords(records)

	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	for _, rec := range records[:5] {
		proof, err := reconciliation.GenerateProof(ctx, participant, scope, rec)
		if err != nil {
			t.Fatalf("GenerateProof: %v", err)
		}
		trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, rec)
		res, err := reconciliation.VerifyProof(ctx, proof, trustedCtx)
		if err != nil || !res.Valid {
			t.Fatalf("VerifyProof: %v", err)
		}
	}

	if len(records) != len(snapshotBefore) {
		t.Fatalf("record slice length changed: %d vs %d", len(records), len(snapshotBefore))
	}

	for i := range records {
		orig := snapshotBefore[i]
		curr := records[i]

		if curr.OperationID != orig.OperationID {
			t.Errorf("record %d OperationID mutated: %s vs %s", i, curr.OperationID, orig.OperationID)
		}
		if curr.PaymentID != orig.PaymentID {
			t.Errorf("record %d PaymentID mutated: %s vs %s", i, curr.PaymentID, orig.PaymentID)
		}
		if curr.AccountID != orig.AccountID {
			t.Errorf("record %d AccountID mutated: %s vs %s", i, curr.AccountID, orig.AccountID)
		}
		if curr.EntryType != orig.EntryType {
			t.Errorf("record %d EntryType mutated: %s vs %s", i, curr.EntryType, orig.EntryType)
		}
		if curr.AmountPaise != orig.AmountPaise {
			t.Errorf("record %d AmountPaise mutated: %d vs %d", i, curr.AmountPaise, orig.AmountPaise)
		}
		if curr.Currency != orig.Currency {
			t.Errorf("record %d Currency mutated: %s vs %s", i, curr.Currency, orig.Currency)
		}
		if !curr.OccurredAt.Equal(orig.OccurredAt) {
			t.Errorf("record %d OccurredAt mutated: %v vs %v", i, curr.OccurredAt, orig.OccurredAt)
		}
	}
}

// TestProofOneRecordBucket verifies proof generation and verification for a 1-record bucket.
func TestProofOneRecordBucket(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
	records := createIntegrityRecords(base, 1, 2*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 2*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	if len(proof.BucketPath) != 0 {
		t.Errorf("1-record bucket path should be empty, got len %d", len(proof.BucketPath))
	}
	if len(proof.GlobalPath) != 0 {
		t.Errorf("1-bucket tree global path should be empty, got len %d", len(proof.GlobalPath))
	}
	if !bytes.Equal(proof.LeafHash, proof.BucketRoot) {
		t.Errorf("leaf hash must equal bucket root in 1-record bucket")
	}
	if !bytes.Equal(proof.BucketRoot, proof.ExpectedRoot) {
		t.Errorf("bucket root must equal global root in 1-bucket tree")
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 2*time.Hour, records[0])
	res, err := reconciliation.VerifyProof(ctx, proof, trustedCtx)
	if err != nil || !res.Valid {
		t.Fatalf("VerifyProof on 1-record bucket failed: %v", err)
	}
}

// TestProofLeftAndRightChild verifies both left-child and right-child proofs in a 2-record bucket.
func TestProofLeftAndRightChild(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(1 * time.Hour)}
	records := createIntegrityRecords(base, 2, 1*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 1*time.Hour, records)

	// Record 0 is left child (its sibling is on the right)
	proof0, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatalf("GenerateProof 0: %v", err)
	}
	if len(proof0.BucketPath) != 1 || proof0.BucketPath[0].Order != reconciliation.SiblingRight {
		t.Errorf("proof0 step 0 order = %q, want %q", proof0.BucketPath[0].Order, reconciliation.SiblingRight)
	}
	trustedCtx0 := buildTrustedContext(t, ctx, participant, scope, 1*time.Hour, records[0])
	if res, err := reconciliation.VerifyProof(ctx, proof0, trustedCtx0); err != nil || !res.Valid {
		t.Fatalf("VerifyProof proof0: %v", err)
	}

	// Record 1 is right child (its sibling is on the left)
	proof1, err := reconciliation.GenerateProof(ctx, participant, scope, records[1])
	if err != nil {
		t.Fatalf("GenerateProof 1: %v", err)
	}
	if len(proof1.BucketPath) != 1 || proof1.BucketPath[0].Order != reconciliation.SiblingLeft {
		t.Errorf("proof1 step 0 order = %q, want %q", proof1.BucketPath[0].Order, reconciliation.SiblingLeft)
	}
	trustedCtx1 := buildTrustedContext(t, ctx, participant, scope, 1*time.Hour, records[1])
	if res, err := reconciliation.VerifyProof(ctx, proof1, trustedCtx1); err != nil || !res.Valid {
		t.Fatalf("VerifyProof proof1: %v", err)
	}
}

// TestVerifyExtraProofElementsFails verifies that inserting unexpected proof elements causes failure.
func TestVerifyExtraProofElementsFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	records := createIntegrityRecords(base, 8, 4*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 2*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 2*time.Hour, records[0])

	tampered := proof
	extraStep := reconciliation.ProofStep{
		Hash:  make([]byte, 32),
		Order: reconciliation.SiblingRight,
	}
	tampered.BucketPath = append(tampered.BucketPath, extraStep)
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil {
		t.Errorf("expected error when extra step is added to BucketPath, got nil")
	}
}

// TestProofSerializeRoundTrip verifies JSON marshaling and unmarshaling roundtrip of an IntegrityProof.
func TestProofSerializeRoundTrip(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(6 * time.Hour)}
	records := createIntegrityRecords(base, 12, 6*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 2*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[3])
	if err != nil {
		t.Fatal(err)
	}

	data, err := proof.SerializedBytes()
	if err != nil {
		t.Fatalf("SerializedBytes: %v", err)
	}

	restored, err := reconciliation.DeserializeProof(data)
	if err != nil {
		t.Fatalf("DeserializeProof: %v", err)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 2*time.Hour, records[3])
	res, err := reconciliation.VerifyProof(ctx, restored, trustedCtx)
	if err != nil || !res.Valid {
		t.Fatalf("VerifyProof on restored proof failed: %v", err)
	}
}

// TestGenerateProofRecordNotFound verifies requesting a proof for an absent record fails.
func TestGenerateProofRecordNotFound(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	records := createIntegrityRecords(base, 8, 4*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 2*time.Hour, records)

	absentRecord := reconciliation.CanonicalRecord{
		OperationID: uuid.New(),
		PaymentID:   uuid.New(),
		AccountID:   uuid.New(),
		EntryType:   "DEBIT",
		AmountPaise: 99999,
		Currency:    "INR",
		OccurredAt:  base.Add(30 * time.Minute),
	}

	if _, err := reconciliation.GenerateProof(ctx, participant, scope, absentRecord); err == nil || !errors.Is(err, reconciliation.ErrRecordNotFound) {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}
}

// --- M3-7-C1 HARDENED CONTEXT BINDING TESTS ---

// TestVerifyRelabeledParticipantAndBucketFails proves that changing BOTH ParticipantID
// and BucketID.Partition fails verification against the trusted context.
func TestVerifyRelabeledParticipantAndBucketFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}

	// Trusted context strictly expects BANK-A from independent source
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])
	if trustedCtx.ParticipantID != "BANK-A" {
		t.Fatalf("expected trustedCtx.ParticipantID to be BANK-A, got %q", trustedCtx.ParticipantID)
	}

	// Attacker relabels both proof ParticipantID and BucketID.Partition to BANK-B
	tampered := proof
	tampered.ParticipantID = "BANK-B"
	tampered.BucketID.Partition = "BANK-B"
	// Keep record, paths, roots unchanged

	// Verification against the original trusted context must fail
	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofParticipantMismatch) {
		t.Errorf("expected ErrProofParticipantMismatch when proof is relabeled to BANK-B, got %v", err)
	}
}

// TestVerifyWrongBroaderScopeFails proves that expanding the proof scope to a broader scope
// that still contains the record fails verification against the expected scope.
func TestVerifyWrongBroaderScopeFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// Original scope: 12 hours
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	// Attacker broadens scope to 24 hours (record at base + 10s is still within this broader scope)
	tampered := proof
	tampered.Scope = reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}

	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofScopeMismatch) {
		t.Errorf("expected ErrProofScopeMismatch when scope is broadened, got %v", err)
	}
}

// TestVerifyWrongContainingScopeFails proves that changing proof scope to another valid scope
// that still contains the record and bucket fails verification against expected scope.
func TestVerifyWrongContainingScopeFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	// Attacker shifts scope endpoints by 1 hour backwards/forwards: [base - 1h, base + 13h)
	// Record at base + 10s is still contained, and bucket [base, base + 4h) is still contained
	tampered := proof
	tampered.Scope = reconciliation.Scope{From: base.Add(-1 * time.Hour), To: base.Add(13 * time.Hour)}

	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofScopeMismatch) {
		t.Errorf("expected ErrProofScopeMismatch when scope is shifted, got %v", err)
	}
}

// TestVerifyWrongGenerationFails proves that changing Generation while all Merkle fields
// remain valid fails verification against the expected generation.
func TestVerifyWrongGenerationFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])
	if trustedCtx.Generation == "" {
		t.Fatalf("expected non-empty generation in trusted context")
	}

	// Change Generation to different generation string while all Merkle paths and roots remain valid
	tampered := proof
	tampered.Generation = "different-generation"

	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofGenerationMismatch) {
		t.Errorf("expected ErrProofGenerationMismatch, got %v", err)
	}
}

// TestVerifyWrongBucketIdentityFails proves that changing BucketID while keeping record/root/path
// valid where possible fails verification against the expected BucketID.
func TestVerifyWrongBucketIdentityFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	// Trusted context expects bucket 0: [base, base + 4h)
	// Attacker provides proof claiming bucket 1: [base + 4h, base + 8h)
	tampered := proof
	tampered.BucketID.Start = base.Add(4 * time.Hour)

	if _, err := reconciliation.VerifyProof(ctx, tampered, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
		t.Errorf("expected ErrProofBucketMismatch when BucketID differs from expected context, got %v", err)
	}
}

// TestVerifyWrongVersionContextFails proves that verifying with mismatched expected
// canonical or algorithm version fails.
func TestVerifyWrongVersionContextFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}

	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])

	// Mismatched CanonicalVersion in trusted context
	wrongCanonCtx := trustedCtx
	wrongCanonCtx.CanonicalVersion = "v2"
	if _, err := reconciliation.VerifyProof(ctx, proof, wrongCanonCtx); err == nil || !errors.Is(err, reconciliation.ErrProofIncompatibleVersion) {
		t.Errorf("expected ErrProofIncompatibleVersion for mismatched canonical version in context, got %v", err)
	}

	// Mismatched AlgorithmVersion in trusted context
	wrongAlgoCtx := trustedCtx
	wrongAlgoCtx.AlgorithmVersion = "merkle-v2"
	if _, err := reconciliation.VerifyProof(ctx, proof, wrongAlgoCtx); err == nil || !errors.Is(err, reconciliation.ErrProofIncompatibleVersion) {
		t.Errorf("expected ErrProofIncompatibleVersion for mismatched algorithm version in context, got %v", err)
	}
}

// TestVerifyMissingTrustedContextFails proves that verification rejects empty or incomplete
// trusted context rather than silently assuming contextual validity.
func TestVerifyMissingTrustedContextFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	proof, err := reconciliation.GenerateProof(ctx, participant, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}

	// 1. Completely empty context
	emptyCtx := reconciliation.ProofVerificationContext{}
	if _, err := reconciliation.VerifyProof(ctx, proof, emptyCtx); err == nil || !errors.Is(err, reconciliation.ErrMissingVerificationContext) {
		t.Errorf("expected ErrMissingVerificationContext for empty context, got %v", err)
	}

	// 2. Context missing generation
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, records[0])
	partialCtx := trustedCtx
	partialCtx.Generation = ""
	if _, err := reconciliation.VerifyProof(ctx, proof, partialCtx); err == nil || !errors.Is(err, reconciliation.ErrMissingVerificationContext) {
		t.Errorf("expected ErrMissingVerificationContext when generation is missing, got %v", err)
	}

	// 3. Context missing expected root
	partialCtx = trustedCtx
	partialCtx.ExpectedRoot = nil
	if _, err := reconciliation.VerifyProof(ctx, proof, partialCtx); err == nil || !errors.Is(err, reconciliation.ErrMissingVerificationContext) {
		t.Errorf("expected ErrMissingVerificationContext when expected root is nil, got %v", err)
	}

	// 4. Context missing participant
	partialCtx = trustedCtx
	partialCtx.ParticipantID = ""
	if _, err := reconciliation.VerifyProof(ctx, proof, partialCtx); err == nil || !errors.Is(err, reconciliation.ErrMissingVerificationContext) {
		t.Errorf("expected ErrMissingVerificationContext when participant ID is missing, got %v", err)
	}

	// 5. VerifyProofWithOptions with zero options
	if _, err := reconciliation.VerifyProofWithOptions(ctx, proof); err == nil || !errors.Is(err, reconciliation.ErrMissingVerificationContext) {
		t.Errorf("expected ErrMissingVerificationContext when no options provided, got %v", err)
	}
}

// TestVulnerabilityProofRelabelingRejected proves that the silent relabeling vulnerability
// (where an attacker takes a valid BANK-A proof, relabels ParticipantID and BucketID to BANK-B,
// and preserves the valid Merkle path) is completely blocked by the hardened verifier.
func TestVulnerabilityProofRelabelingRejected(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)

	// Participant BANK-A
	partA := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	// Participant BANK-B (with different records so roots differ)
	recordsB := cloneTestRecords(records)
	recordsB[0].AmountPaise = 9999999
	partB := buildTestParticipant(t, "BANK-B", 4*time.Hour, recordsB)

	// 1. Build BANK-A trusted context from BANK-A root/reference
	trustedCtxA := buildTrustedContext(t, ctx, partA, scope, 4*time.Hour, records[0])

	// 2. Build BANK-B trusted context from BANK-B root/reference
	trustedCtxB := buildTrustedContext(t, ctx, partB, scope, 4*time.Hour, records[0])

	// Generate authentic proof from BANK-A
	proofA, err := reconciliation.GenerateProof(ctx, partA, scope, records[0])
	if err != nil {
		t.Fatal(err)
	}

	// Exploit attempt: relabel proof to BANK-B
	relabelledProof := proofA
	relabelledProof.ParticipantID = "BANK-B"
	relabelledProof.BucketID.Partition = "BANK-B"
	relabelledProof.Generation = trustedCtxB.Generation
	// Sibling hashes, record, leaf hash, bucket root, expected root all left unchanged!

	// Verifier expecting BANK-A rejects the relabelled proof
	if _, err := reconciliation.VerifyProof(ctx, relabelledProof, trustedCtxA); err == nil || !errors.Is(err, reconciliation.ErrProofParticipantMismatch) {
		t.Fatalf("expected ErrProofParticipantMismatch when relabelled proof is verified against BANK-A context, got %v", err)
	}

	// Verifier expecting BANK-B rejects the relabelled proof because the Merkle root belongs to BANK-A
	if _, err := reconciliation.VerifyProof(ctx, relabelledProof, trustedCtxB); err == nil || !errors.Is(err, reconciliation.ErrProofRootMismatch) {
		t.Fatalf("expected ErrProofRootMismatch when relabelled proof is verified against BANK-B context, got %v", err)
	}
}

// TestTrustedContextIsIndependentFromProof proves that the trusted verification context
// is constructed independently from the proof and that tampering with any proof context
// field (ParticipantID, BucketID, Scope, Generation, ExpectedRoot) is rejected.
func TestTrustedContextIsIndependentFromProof(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(12 * time.Hour)}
	records := createIntegrityRecords(base, 12, 12*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	target := records[0]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	// Construct trusted context independently from participant and test parameters (never copying from proof)
	trustedCtx := buildTrustedContext(t, ctx, participant, scope, 4*time.Hour, target)

	// Baseline: unmodified proof against independent trusted context verifies successfully
	res, err := reconciliation.VerifyProof(ctx, proof, trustedCtx)
	if err != nil || !res.Valid {
		t.Fatalf("VerifyProof with independent trusted context failed: %v", err)
	}

	// 1. Mutate proof ParticipantID
	tamperedPart := proof
	tamperedPart.ParticipantID = "BANK-B"
	if _, err := reconciliation.VerifyProof(ctx, tamperedPart, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofParticipantMismatch) {
		t.Errorf("expected ErrProofParticipantMismatch on mutated ParticipantID, got %v", err)
	}

	// 2. Mutate proof BucketID
	tamperedBucket := proof
	tamperedBucket.BucketID.Start = tamperedBucket.BucketID.Start.Add(4 * time.Hour)
	if _, err := reconciliation.VerifyProof(ctx, tamperedBucket, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
		t.Errorf("expected ErrProofBucketMismatch on mutated BucketID, got %v", err)
	}

	// 3. Mutate proof Scope
	tamperedScope := proof
	tamperedScope.Scope = reconciliation.Scope{From: base.Add(time.Hour), To: base.Add(13 * time.Hour)}
	if _, err := reconciliation.VerifyProof(ctx, tamperedScope, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofScopeMismatch) {
		t.Errorf("expected ErrProofScopeMismatch on mutated Scope, got %v", err)
	}

	// 4. Mutate proof Generation
	tamperedGen := proof
	tamperedGen.Generation = "mutated-generation"
	if _, err := reconciliation.VerifyProof(ctx, tamperedGen, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofGenerationMismatch) {
		t.Errorf("expected ErrProofGenerationMismatch on mutated Generation, got %v", err)
	}

	// 5. Mutate proof ExpectedRoot
	tamperedRoot := proof
	tamperedRoot.ExpectedRoot = append([]byte(nil), proof.ExpectedRoot...)
	tamperedRoot.ExpectedRoot[0] ^= 0xFF
	if _, err := reconciliation.VerifyProof(ctx, tamperedRoot, trustedCtx); err == nil || !errors.Is(err, reconciliation.ErrProofRootMismatch) {
		t.Errorf("expected ErrProofRootMismatch on mutated ExpectedRoot, got %v", err)
	}
}

// TestGenerateProofMissingGenerationFails asserts that GenerateProofFromState with empty generation fails.
func TestGenerateProofMissingGenerationFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	records := createIntegrityRecords(base, 4, 4*time.Hour)

	ledger, err := reconciliation.NewIncrementalMerkleLedger("BANK-A", 2*time.Hour, scope)
	if err != nil {
		t.Fatalf("NewIncrementalMerkleLedger: %v", err)
	}
	if _, err := ledger.Bootstrap(ctx, records); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	state := ledger.Snapshot()

	// Call GenerateProofFromState with empty generation string
	_, err = reconciliation.GenerateProofFromState(state, "BANK-A", "", records[0])
	if err == nil || !errors.Is(err, reconciliation.ErrMissingProofGeneration) {
		t.Fatalf("expected ErrMissingProofGeneration when generation is empty, got %v", err)
	}
}
