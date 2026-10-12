package operations

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestComputeFilterHash_Deterministic(t *testing.T) {
	pid := uuid.New()
	t1 := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	filter1 := WorklistFilter{
		State:          "PROCESSING,CREATED",
		RecoveryStatus: "needs_attention",
		BankCode:       "BANK-A",
		PaymentID:      &pid,
		CreatedFrom:    &t1,
	}
	filter2 := WorklistFilter{
		State:          "PROCESSING,CREATED",
		RecoveryStatus: "needs_attention",
		BankCode:       "BANK-A",
		PaymentID:      &pid,
		CreatedFrom:    &t1,
	}
	hash1 := ComputeFilterHash(filter1)
	hash2 := ComputeFilterHash(filter2)
	if hash1 != hash2 {
		t.Fatalf("expected identical filter hashes, got %s vs %s", hash1, hash2)
	}

	filter3 := filter1
	filter3.State = "COMPLETED"
	hash3 := ComputeFilterHash(filter3)
	if hash1 == hash3 {
		t.Fatalf("expected different filter hashes for different state filter")
	}
}

func TestCursorEncodingDecoding(t *testing.T) {
	pid := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	hash := "testhash123"

	payload := WorklistCursorPayload{
		UpdatedAt:  now,
		PaymentID:  pid,
		FilterHash: hash,
	}

	cursorStr, err := EncodeWorklistCursor(payload)
	if err != nil {
		t.Fatalf("failed to encode cursor: %v", err)
	}

	decoded, err := DecodeWorklistCursor(cursorStr, hash)
	if err != nil {
		t.Fatalf("failed to decode cursor: %v", err)
	}

	if decoded.PaymentID != pid || !decoded.UpdatedAt.Equal(now) {
		t.Fatalf("mismatched decoded cursor values: %+v vs %+v", decoded, payload)
	}

	// Tampered or mismatched filter hash
	_, err = DecodeWorklistCursor(cursorStr, "differenthash")
	if err == nil {
		t.Fatalf("expected error when decoding with wrong filter hash")
	}
}

func TestActorRateLimiter_EnforcesLimit(t *testing.T) {
	actorID := uuid.New()
	limiter := NewActorRateLimiter(5, 1*time.Minute)

	for i := 0; i < 5; i++ {
		if !limiter.Allow(actorID) {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}

	// 6th request within window must be rejected
	if limiter.Allow(actorID) {
		t.Fatalf("expected 6th request to be rejected by rate limiter")
	}

	// Different actor should be allowed
	otherActor := uuid.New()
	if !limiter.Allow(otherActor) {
		t.Fatalf("expected different actor to be allowed")
	}
}

func TestWorklistJsonSerialization_MatchesContract(t *testing.T) {
	pid := uuid.New()
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	age := int64(1500)

	srcBank := "BANK-A"
	dstBank := "BANK-B"
	reason := "PARTICIPANT_TIMEOUT"

	item := WorklistItem{
		PaymentID:           pid,
		State:               "PROCESSING",
		AmountPaise:         "25000",
		Currency:            "INR",
		SourceBankCode:      &srcBank,
		DestinationBankCode: &dstBank,
		CreatedAt:           now,
		UpdatedAt:           now,
		AgeMs:               1500,
		RecoveryStatus:      "needs_attention",
		AttemptCount:        3,
		NextAttemptAt:       &now,
		ReasonCode:          &reason,
		ProjectionVersion:   "4",
	}

	worklist := Worklist{
		Items:      []WorklistItem{item},
		NextCursor: nil,
		AsOf:       now,
		Projection: WorklistProjectionStatus{
			Status:                  "ready",
			UpdatedAt:               &now,
			OldestPendingEventAgeMs: &age,
		},
	}

	data, err := json.Marshal(worklist)
	if err != nil {
		t.Fatalf("marshal worklist: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}

	if _, ok := parsed["items"]; !ok {
		t.Fatalf("missing items in json")
	}
	if _, ok := parsed["projection"]; !ok {
		t.Fatalf("missing projection in json")
	}
}

func TestEvidenceBundleJsonSerialization_MatchesContract(t *testing.T) {
	pid := uuid.New()
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

	bundle := EvidenceBundle{
		SchemaVersion:        "tx.evidence.v1",
		PaymentID:            pid,
		SnapshotID:           "sha256:abc123mock",
		CollectedAt:          now,
		Payment: PaymentSnapshot{
			State:        "COMMITTED",
			AmountPaise:  "5000",
			Currency:     "INR",
			CreatedAt:    now,
			UpdatedAt:    now,
			CompletedAt:  nil,
			EventVersion: "2",
			EvidenceID:   "payment:" + pid.String() + ":mock",
		},
		Route: RouteSnapshot{
			SourceBankCode:      nil,
			DestinationBankCode: nil,
			ExecutionTargetID:   nil,
			SelectedAt:          nil,
			ReasonCode:          nil,
			Status:              "not_applicable",
		},
		Operations:           []OperationSnapshot{},
		Transitions:          []TransitionSnapshot{},
		TransitionsTruncated: false,
		Recovery: RecoverySnapshot{
			Status:        "none",
			AttemptCount:  0,
			NextAttemptAt: nil,
			LastAttemptID: nil,
			ReasonCode:    nil,
			SourceStatus:  "available",
		},
		Reconciliation: ReconciliationSnapshot{
			SourceStatus:     "not_collected",
			RunIDs:           []uuid.UUID{},
			DiscrepancyCount: nil,
			ScopeMismatch:    false,
		},
		Integrity: IntegritySnapshot{
			SourceStatus:   "not_collected",
			RunID:          nil,
			OccurredAt:     nil,
			ViolationCount: nil,
		},
		Traces: TracesSnapshot{
			SourceStatus: "not_collected",
			TraceIDs:     []string{},
			Truncated:    false,
		},
		Sources:  []SourceSnapshot{},
		Warnings: []WarningSnapshot{},
	}

	bytes, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("failed to marshal evidence bundle: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(bytes, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Verify required array keys serialize as empty array [] instead of null
	for _, key := range []string{"operations", "transitions", "sources", "warnings"} {
		val, ok := m[key]
		if !ok || val == nil {
			t.Fatalf("expected key %s to be non-nil empty array", key)
		}
	}
}
