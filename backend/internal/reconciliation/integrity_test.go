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

// TestGenerateProof verifies proof generation produces a valid, complete typed proof structure.
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

// TestVerifyProof verifies positive proof verification.
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

	// 1. Default verification
	res, err := reconciliation.VerifyProof(ctx, proof)
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

	// 2. Verification with explicit options matching the proof
	resOpt, err := reconciliation.VerifyProof(
		ctx,
		proof,
		reconciliation.WithExpectedParticipant("BANK-A"),
		reconciliation.WithExpectedScope(scope),
		reconciliation.WithExpectedRoot(proof.ExpectedRoot),
	)
	if err != nil {
		t.Fatalf("VerifyProof with options failed: %v", err)
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

	// Mutate AmountPaise
	tampered := proof
	tampered.Record.AmountPaise += 1000
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofRecordMismatch) {
		t.Errorf("expected ErrProofRecordMismatch, got %v", err)
	}

	// Mutate Currency
	tampered = proof
	tampered.Record.Currency = "USD"
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofRecordMismatch) {
		t.Errorf("expected ErrProofRecordMismatch for currency change, got %v", err)
	}

	// Mutate EntryType
	tampered = proof
	if tampered.Record.EntryType == "DEBIT" {
		tampered.Record.EntryType = "CREDIT"
	} else {
		tampered.Record.EntryType = "DEBIT"
	}
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofRecordMismatch) {
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

	tampered := proof
	tampered.LeafHash = append([]byte(nil), proof.LeafHash...)
	tampered.LeafHash[0] ^= 0xFF

	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofRecordMismatch) {
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
		if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofBucketRootMismatch) {
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
		if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofRootMismatch) {
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

	// Case A: proof.ExpectedRoot is modified
	tampered := proof
	tampered.ExpectedRoot = append([]byte(nil), proof.ExpectedRoot...)
	tampered.ExpectedRoot[0] ^= 0xFF
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofRootMismatch) {
		t.Errorf("expected ErrProofRootMismatch when proof.ExpectedRoot is wrong, got %v", err)
	}

	// Case B: WithExpectedRoot option provides different root
	wrongRoot := append([]byte(nil), proof.ExpectedRoot...)
	wrongRoot[0] ^= 0xEE
	if _, err := reconciliation.VerifyProof(ctx, proof, reconciliation.WithExpectedRoot(wrongRoot)); err == nil || !errors.Is(err, reconciliation.ErrProofRootMismatch) {
		t.Errorf("expected ErrProofRootMismatch when WithExpectedRoot is wrong, got %v", err)
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

	// Case A: Mutate BucketID start time so record no longer falls in this bucket
	tampered := proof
	tampered.BucketID.Start = tampered.BucketID.Start.Add(4 * time.Hour)
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
		t.Errorf("expected ErrProofBucketMismatch when bucket start time is wrong, got %v", err)
	}

	// Case B: Mutate BucketID partition
	tampered = proof
	tampered.BucketID.Partition = "BANK-OTHER"
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
		t.Errorf("expected ErrProofBucketMismatch when bucket partition is wrong, got %v", err)
	}

	// Case C: Mutate BucketID width so record maps to a different bucket start
	tampered = proof
	tampered.BucketID.Width = 5 * time.Second
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
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

	// Case A: proof.Scope modified so record is outside scope
	tampered := proof
	tampered.Scope.From = proof.Record.OccurredAt.Add(time.Minute)
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofScopeMismatch) {
		t.Errorf("expected ErrProofScopeMismatch when record is outside scope, got %v", err)
	}

	// Case B: WithExpectedScope option provides a different scope
	wrongScope := reconciliation.Scope{From: base.Add(24 * time.Hour), To: base.Add(48 * time.Hour)}
	if _, err := reconciliation.VerifyProof(ctx, proof, reconciliation.WithExpectedScope(wrongScope)); err == nil || !errors.Is(err, reconciliation.ErrProofScopeMismatch) {
		t.Errorf("expected ErrProofScopeMismatch when WithExpectedScope differs, got %v", err)
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

	// Case A: proof.ParticipantID changed to BANK-B while bucket partition is BANK-A
	tampered := proof
	tampered.ParticipantID = "BANK-B"
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofBucketMismatch) {
		t.Errorf("expected ErrProofBucketMismatch when participant diverges from bucket partition, got %v", err)
	}

	// Case B: WithExpectedParticipant("BANK-B")
	if _, err := reconciliation.VerifyProof(ctx, proof, reconciliation.WithExpectedParticipant("BANK-B")); err == nil || !errors.Is(err, reconciliation.ErrProofParticipantMismatch) {
		t.Errorf("expected ErrProofParticipantMismatch when WithExpectedParticipant is BANK-B, got %v", err)
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

	tampered := proof
	tampered.CanonicalVersion = "v2"
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofIncompatibleVersion) {
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

	tampered := proof
	tampered.AlgorithmVersion = "merkle-v2"
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofIncompatibleVersion) {
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

	// Truncate BucketPath
	if len(proof.BucketPath) > 0 {
		tampered := proof
		tampered.BucketPath = proof.BucketPath[:len(proof.BucketPath)-1]
		if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil {
			t.Errorf("expected error when BucketPath is truncated, got nil")
		}
	}

	// Truncate GlobalPath
	if len(proof.GlobalPath) > 0 {
		tampered := proof
		tampered.GlobalPath = proof.GlobalPath[:len(proof.GlobalPath)-1]
		if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil {
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
		if _, err := reconciliation.VerifyProof(ctx, swapped); err == nil {
			t.Errorf("expected error when sibling order was swapped, got nil")
		}
	}

	// Case B: Invalid order string
	tampered := proof
	tampered.BucketPath = make([]reconciliation.ProofStep, len(proof.BucketPath))
	copy(tampered.BucketPath, proof.BucketPath)
	if len(tampered.BucketPath) > 0 {
		tampered.BucketPath[0].Order = "TOP"
		if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofMalformedPath) {
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
	// 3 records in bucket 0 (hour 0), 3 records in bucket 1 (hour 1), 3 records in bucket 2 (hour 2)
	records := createIntegrityRecords(base, 9, 3*time.Hour)
	participant := buildTestParticipant(t, "BANK-A", time.Hour, records)

	// Target record 8 is in bucket 2 (the odd third bucket in a 3-bucket tree)
	// Within bucket 2, record 8 is the third record (odd element promoted at bucket level!)
	target := records[8]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof on odd tree: %v", err)
	}

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
	res, err := reconciliation.VerifyProof(ctx, proof)
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
			if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil || !errors.Is(err, reconciliation.ErrProofMalformedPath) {
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
	records := createIntegrityRecords(base, 15, 5*time.Hour) // 3 records per hour across 5 buckets
	participant := buildTestParticipant(t, "BANK-A", time.Hour, records)

	// Record 0 is in the first bucket (Hour 0)
	target := records[0]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	if !proof.BucketID.Start.Equal(base) {
		t.Errorf("bucket start = %v, want %v", proof.BucketID.Start, base)
	}

	res, err := reconciliation.VerifyProof(ctx, proof)
	if err != nil || !res.Valid {
		t.Fatalf("VerifyProof for first bucket failed: %v", err)
	}
}

// TestProofForMiddleBucket verifies proof generation and verification for a middle bucket.
func TestProofForMiddleBucket(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(5 * time.Hour)}
	records := createIntegrityRecords(base, 15, 5*time.Hour) // 3 records per hour across 5 buckets
	participant := buildTestParticipant(t, "BANK-A", time.Hour, records)

	// Records 6, 7, 8 are in the middle bucket (Hour 2, index 2)
	target := records[7]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	expectedStart := base.Add(2 * time.Hour)
	if !proof.BucketID.Start.Equal(expectedStart) {
		t.Errorf("bucket start = %v, want %v", proof.BucketID.Start, expectedStart)
	}

	res, err := reconciliation.VerifyProof(ctx, proof)
	if err != nil || !res.Valid {
		t.Fatalf("VerifyProof for middle bucket failed: %v", err)
	}
}

// TestProofForLastBucket verifies proof generation and verification for the last bucket.
func TestProofForLastBucket(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(5 * time.Hour)}
	records := createIntegrityRecords(base, 15, 5*time.Hour) // 3 records per hour across 5 buckets
	participant := buildTestParticipant(t, "BANK-A", time.Hour, records)

	// Records 12, 13, 14 are in the final bucket (Hour 4, index 4)
	target := records[14]
	proof, err := reconciliation.GenerateProof(ctx, participant, scope, target)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	expectedStart := base.Add(4 * time.Hour)
	if !proof.BucketID.Start.Equal(expectedStart) {
		t.Errorf("bucket start = %v, want %v", proof.BucketID.Start, expectedStart)
	}

	res, err := reconciliation.VerifyProof(ctx, proof)
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

	// Take deep snapshot before running proof operations
	snapshotBefore := cloneTestRecords(records)

	participant := buildTestParticipant(t, "BANK-A", 4*time.Hour, records)

	for _, rec := range records[:5] {
		proof, err := reconciliation.GenerateProof(ctx, participant, scope, rec)
		if err != nil {
			t.Fatalf("GenerateProof: %v", err)
		}
		res, err := reconciliation.VerifyProof(ctx, proof)
		if err != nil || !res.Valid {
			t.Fatalf("VerifyProof: %v", err)
		}
	}

	// Verify deep snapshot matches exactly after all operations
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

	// With 1 record in 1 bucket, BucketPath is empty and GlobalPath is empty
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

	res, err := reconciliation.VerifyProof(ctx, proof)
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
	if res, err := reconciliation.VerifyProof(ctx, proof0); err != nil || !res.Valid {
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
	if res, err := reconciliation.VerifyProof(ctx, proof1); err != nil || !res.Valid {
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

	tampered := proof
	extraStep := reconciliation.ProofStep{
		Hash:  make([]byte, 32),
		Order: reconciliation.SiblingRight,
	}
	tampered.BucketPath = append(tampered.BucketPath, extraStep)
	if _, err := reconciliation.VerifyProof(ctx, tampered); err == nil {
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

	res, err := reconciliation.VerifyProof(ctx, restored)
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
