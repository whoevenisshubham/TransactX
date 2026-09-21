package health

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/common"
)

type memoryRepository struct {
	mu      sync.Mutex
	samples []HealthSample
}

func (repository *memoryRepository) Record(_ context.Context, sample HealthSample) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.samples = append(repository.samples, sample)
	return nil
}

func (repository *memoryRepository) ListRecent(_ context.Context, targetID string, from, to time.Time, limit int) ([]HealthSample, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var result []HealthSample
	for _, sample := range repository.samples {
		if sample.TargetID == targetID && !sample.SampledAt.Before(from) && !sample.SampledAt.After(to) {
			result = append(result, sample)
		}
	}
	return result, nil
}

type healthChecker func(context.Context) (bank.HealthResult, error)

func (checker healthChecker) GetHealth(ctx context.Context) (bank.HealthResult, error) {
	return checker(ctx)
}

func TestSampleRecordsHealthOutcomesAndLatency(t *testing.T) {
	cases := []struct {
		name      string
		checker   healthChecker
		outcome   Outcome
		available bool
	}{
		{"success", func(context.Context) (bank.HealthResult, error) { return bank.HealthResult{Available: true}, nil }, OutcomeSuccess, true},
		{"unavailable", func(context.Context) (bank.HealthResult, error) { return bank.HealthResult{Available: false}, nil }, OutcomeFailure, false},
		{"fast error", func(context.Context) (bank.HealthResult, error) {
			return bank.HealthResult{}, errors.New("unavailable")
		}, OutcomeFailure, false},
		{"timeout", func(ctx context.Context) (bank.HealthResult, error) {
			<-ctx.Done()
			return bank.HealthResult{}, ctx.Err()
		}, OutcomeTimeout, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &memoryRepository{}
			service := NewService(repository, DefaultConfig())
			ctx := common.ContextWithRequestID(context.Background(), "sample-correlation")
			sample, err := service.Sample(ctx, "BANK-A", testCase.checker)
			if err != nil {
				t.Fatal(err)
			}
			if sample.Outcome != testCase.outcome || sample.Available != testCase.available {
				t.Fatalf("sample = %+v", sample)
			}
			if sample.Latency < 0 || sample.SampledAt.IsZero() {
				t.Fatalf("invalid timing: %+v", sample)
			}
			if len(repository.samples) != 1 || repository.samples[0] != sample {
				t.Fatalf("sample was not persisted: %+v", repository.samples)
			}
			if sample.CorrelationID != "sample-correlation" {
				t.Fatalf("correlation ID = %q", sample.CorrelationID)
			}
		})
	}
}

func TestSampleRecordsDeadlineAsTimeout(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository, DefaultConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	sample, err := service.Sample(ctx, "BANK-A", healthChecker(func(ctx context.Context) (bank.HealthResult, error) {
		<-ctx.Done()
		return bank.HealthResult{}, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	if sample.Outcome != OutcomeTimeout || sample.Available {
		t.Fatalf("sample = %+v, want timeout", sample)
	}
}

func TestSampleKeepsTargetsIsolated(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository, DefaultConfig())
	checker := healthChecker(func(context.Context) (bank.HealthResult, error) { return bank.HealthResult{Available: true}, nil })
	if _, err := service.Sample(context.Background(), "BANK-A", checker); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sample(context.Background(), "BANK-B", checker); err != nil {
		t.Fatal(err)
	}
	if len(repository.samples) != 2 || repository.samples[0].TargetID != "BANK-A" || repository.samples[1].TargetID != "BANK-B" {
		t.Fatalf("targets were not isolated: %+v", repository.samples)
	}
}

func TestSampleMeasuresElapsedLatency(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository, DefaultConfig())
	sample, err := service.Sample(context.Background(), "BANK-A", healthChecker(func(context.Context) (bank.HealthResult, error) {
		time.Sleep(15 * time.Millisecond)
		return bank.HealthResult{Available: true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if sample.Latency < 15*time.Millisecond {
		t.Fatalf("latency = %s, want measured elapsed latency", sample.Latency)
	}
}

type mockProbeGate struct {
	mu            sync.Mutex
	allowed       bool
	isProbe       bool
	releaseCalls  int
	beforeCalls   int
	lastTargetID  string
}

func (m *mockProbeGate) BeforeHealthSample(targetID string, now ...time.Time) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beforeCalls++
	m.lastTargetID = targetID
	return m.allowed, m.isProbe
}

func (m *mockProbeGate) ReleaseProbe(targetID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCalls++
	m.lastTargetID = targetID
}

func TestSampleProbeGateRejection(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository, DefaultConfig())
	gate := &mockProbeGate{allowed: false, isProbe: false}
	service.SetProbeGate(gate)

	checkerCalled := false
	checker := healthChecker(func(context.Context) (bank.HealthResult, error) {
		checkerCalled = true
		return bank.HealthResult{Available: true}, nil
	})

	_, err := service.Sample(context.Background(), "RAIL-A", checker)
	if !errors.Is(err, ErrProbeRejected) {
		t.Fatalf("expected ErrProbeRejected, got %v", err)
	}
	if checkerCalled {
		t.Fatalf("checker must not be called when probe gate rejects")
	}
	if gate.beforeCalls != 1 || gate.lastTargetID != "RAIL-A" {
		t.Fatalf("gate before calls = %d, target = %s", gate.beforeCalls, gate.lastTargetID)
	}
}

func TestSampleProbeGateReleaseOnPanic(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository, DefaultConfig())
	gate := &mockProbeGate{allowed: true, isProbe: true}
	service.SetProbeGate(gate)

	checker := healthChecker(func(context.Context) (bank.HealthResult, error) {
		panic("boom")
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		if gate.releaseCalls != 1 {
			t.Fatalf("expected ReleaseProbe to be called once on panic, got %d", gate.releaseCalls)
		}
	}()

	_, _ = service.Sample(context.Background(), "RAIL-A", checker)
}
