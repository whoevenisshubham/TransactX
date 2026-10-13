package investigator

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDecodeStoredJobFailsClosed(t *testing.T) {
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	valid := Job{
		ID:             "00000000-0000-4000-8000-000000000001",
		ActorID:        "00000000-0000-4000-8000-000000000002",
		Role:           "OPS_VIEWER",
		PaymentID:      "00000000-0000-4000-8000-000000000003",
		Question:       "What happened?",
		IdempotencyKey: "stored-job-test",
		RequestHash:    "5ad90574d42bc6aa1f9bf0f8d45ed36163007106eef6e9fda73bf7b4d860b811",
		Status:         StatusQueued,
		Mode:           ModeEvidenceOnly,
		CreatedAt:      now,
		DeadlineAt:     now.Add(time.Minute),
	}
	payload, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStoredJob(payload)
	if err != nil || decoded.ID != valid.ID {
		t.Fatalf("valid stored job rejected: job=%+v err=%v", decoded, err)
	}

	for name, payload := range map[string][]byte{
		"empty object": []byte(`{}`),
		"malformed":    []byte(`{"ID":`),
		"missing hash": mustStoredJobJSON(t, func(job *Job) { job.RequestHash = "" }, valid),
		"bad role":     mustStoredJobJSON(t, func(job *Job) { job.Role = "CUSTOMER" }, valid),
		"bad status":   mustStoredJobJSON(t, func(job *Job) { job.Status = "unknown" }, valid),
		"bad mode":     mustStoredJobJSON(t, func(job *Job) { job.Mode = "automatic" }, valid),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeStoredJob(payload); !errors.Is(err, errStoredJobPayload) {
				t.Fatalf("decode error = %v", err)
			}
		})
	}
}

func mustStoredJobJSON(t *testing.T, mutate func(*Job), original Job) []byte {
	t.Helper()
	mutate(&original)
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
