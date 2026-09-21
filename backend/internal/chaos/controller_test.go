package chaos_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/circuit"
	"github.com/transactx/backend/internal/health"
)

// mockRepo implements in-memory chaos.Repository for testing
type mockRepo struct {
	mu        sync.Mutex
	scenarios map[string]chaos.ChaosScenario
	events    []chaos.ChaosEvent
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		scenarios: make(map[string]chaos.ChaosScenario),
	}
}

func (m *mockRepo) CreateScenario(ctx context.Context, s chaos.ChaosScenario) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scenarios[s.ScenarioID] = s
	return nil
}

func (m *mockRepo) UpdateScenario(ctx context.Context, s chaos.ChaosScenario) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scenarios[s.ScenarioID] = s
	return nil
}

func (m *mockRepo) GetScenario(ctx context.Context, scenarioID string) (*chaos.ChaosScenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.scenarios[scenarioID]
	if !ok {
		return nil, chaos.ErrScenarioNotFound
	}
	return &s, nil
}

func (m *mockRepo) ListScenarios(ctx context.Context, activeOnly bool, limit int) ([]chaos.ChaosScenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []chaos.ChaosScenario
	for _, s := range m.scenarios {
		if activeOnly && !s.Active {
			continue
		}
		list = append(list, s)
	}
	return list, nil
}

func (m *mockRepo) RecordEvent(ctx context.Context, event chaos.ChaosEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockRepo) ListEvents(ctx context.Context, scenarioID string, limit int) ([]chaos.ChaosEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []chaos.ChaosEvent
	for _, e := range m.events {
		if scenarioID == "" || e.ScenarioID == scenarioID {
			list = append(list, e)
		}
	}
	return list, nil
}

// stubBankAdapter implements bank.BankAdapter
type stubBankAdapter struct {
	bankID          string
	healthCalls     int
	holdCalls       int
	creditCalls     int
	availableHealth bool
}

func newStubBank(bankID string) *stubBankAdapter {
	return &stubBankAdapter{
		bankID:          bankID,
		availableHealth: true,
	}
}

func (s *stubBankAdapter) GetHealth(ctx context.Context) (bank.HealthResult, error) {
	s.healthCalls++
	return bank.HealthResult{Available: s.availableHealth}, nil
}

func (s *stubBankAdapter) ResolveAccount(ctx context.Context, req bank.ResolveAccountRequest) (bank.AccountResult, error) {
	return bank.AccountResult{AccountID: req.AccountID, Status: bank.AccountActive}, nil
}

func (s *stubBankAdapter) HoldFunds(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	s.holdCalls++
	return bank.HoldResult{
		OperationResult: bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationSucceeded,
		},
		HoldID: uuid.New(),
	}, nil
}

func (s *stubBankAdapter) ProvisionalCredit(ctx context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	s.creditCalls++
	return bank.OperationResult{
		PaymentID:   req.PaymentID,
		OperationID: req.OperationID,
		Status:      bank.OperationSucceeded,
	}, nil
}

func (s *stubBankAdapter) ConfirmHold(ctx context.Context, req bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationSucceeded}, nil
}

func (s *stubBankAdapter) ReleaseHold(ctx context.Context, req bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationSucceeded}, nil
}

func (s *stubBankAdapter) ReverseProvisionalCredit(ctx context.Context, req bank.ReverseCreditRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationSucceeded}, nil
}

func (s *stubBankAdapter) GetOperationStatus(ctx context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationSucceeded}, nil
}

func (s *stubBankAdapter) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{BankID: s.bankID, SnapshotID: uuid.New(), CapturedAt: time.Now()}, nil
}

func TestChaosAuthorization(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	req := chaos.StartRequest{
		ScenarioID: "sc-auth-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}

	// 1. CUSTOMER denied
	_, err := ctrl.Start(ctx, req, "cust-1", "CUSTOMER")
	if !errors.Is(err, chaos.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for CUSTOMER, got %v", err)
	}

	// 2. MERCHANT denied
	_, err = ctrl.Start(ctx, req, "merch-1", "MERCHANT")
	if !errors.Is(err, chaos.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for MERCHANT, got %v", err)
	}

	// 3. OPS_ADMIN allowed
	sc, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("unexpected error for OPS_ADMIN: %v", err)
	}
	if sc.ScenarioID != "sc-auth-1" || !sc.Active {
		t.Fatalf("expected active scenario sc-auth-1, got %+v", sc)
	}

	// Stop authorization
	if _, err := ctrl.Stop(ctx, "sc-auth-1", "cust-1", "CUSTOMER"); !errors.Is(err, chaos.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on Stop for CUSTOMER, got %v", err)
	}
	if _, err := ctrl.Stop(ctx, "sc-auth-1", "merch-1", "MERCHANT"); !errors.Is(err, chaos.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on Stop for MERCHANT, got %v", err)
	}
	if _, err := ctrl.Stop(ctx, "sc-auth-1", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("unexpected error on Stop for OPS_ADMIN: %v", err)
	}

	// Reset authorization
	if err := ctrl.Reset(ctx, "", "cust-1", "CUSTOMER"); !errors.Is(err, chaos.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on Reset for CUSTOMER, got %v", err)
	}
	if err := ctrl.Reset(ctx, "", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("unexpected error on Reset for OPS_ADMIN: %v", err)
	}
}

func TestChaosLifecycleAndExpiry(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	currentTime := baseTime
	ctrl.SetNowFunc(func() time.Time { return currentTime })

	// 1. Create/Start
	req := chaos.StartRequest{
		ScenarioID: "sc-life-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000},
	}
	sc, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to start scenario: %v", err)
	}
	if sc.Mode != chaos.ExecutionModeSimulation {
		t.Fatalf("expected mode SIMULATION, got %s", sc.Mode)
	}
	if !sc.Active {
		t.Fatalf("expected scenario to be active")
	}
	expectedExpiry := baseTime.Add(10 * time.Second)
	if !sc.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected expiry %v, got %v", expectedExpiry, sc.ExpiresAt)
	}

	// Inspect
	fetched, err := ctrl.GetScenario(ctx, "sc-life-1")
	if err != nil {
		t.Fatalf("failed to get scenario: %v", err)
	}
	if !fetched.Active {
		t.Fatalf("expected fetched scenario to be active")
	}

	// Target conflict when active
	if _, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN"); !errors.Is(err, chaos.ErrTargetConflict) {
		t.Fatalf("expected ErrTargetConflict, got %v", err)
	}

	// Active fault present
	fault, active := ctrl.GetActiveFault("RAIL-A")
	if !active || fault.ScenarioID != "sc-life-1" {
		t.Fatalf("expected active fault sc-life-1, got active=%v, fault=%+v", active, fault)
	}

	// Advance time past expiry
	currentTime = baseTime.Add(11 * time.Second)

	// 2. Auto-expiry check
	fault, active = ctrl.GetActiveFault("RAIL-A")
	if active || fault != nil {
		t.Fatalf("expected fault to be inactive after expiry, got active=%v, fault=%+v", active, fault)
	}

	// Scenario inspected shows inactive
	fetched, err = ctrl.GetScenario(ctx, "sc-life-1")
	if err != nil {
		t.Fatalf("failed to get expired scenario: %v", err)
	}
	if fetched.Active {
		t.Fatalf("expected expired scenario to be inactive")
	}

	// 3. Repeated stop is safe and idempotent
	if _, err := ctrl.Stop(ctx, "sc-life-1", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("repeated stop should succeed cleanly, got %v", err)
	}

	// 4. Repeated reset is safe and idempotent
	if err := ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("reset target should succeed, got %v", err)
	}
	if err := ctrl.Reset(ctx, "", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("reset all should succeed, got %v", err)
	}
}

func TestTargetIsolation(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	stubA := newStubBank("RAIL-A")
	stubB := newStubBank("RAIL-B")

	adapterA := chaos.NewChaosAdapter("RAIL-A", stubA, ctrl)
	adapterB := chaos.NewChaosAdapter("RAIL-B", stubB, ctrl)

	// Start fault only on RAIL-A
	req := chaos.StartRequest{
		ScenarioID: "sc-iso-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000},
	}
	if _, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("failed to start scenario: %v", err)
	}

	// Target A is affected
	healthA, errA := adapterA.GetHealth(ctx)
	if healthA.Available || errA == nil {
		t.Fatalf("expected target A to be unavailable with error, got avail=%v, err=%v", healthA.Available, errA)
	}
	var adapterErr *bank.AdapterError
	if !errors.As(errA, &adapterErr) || adapterErr.Code != bank.ErrCodeBankUnavailable {
		t.Fatalf("expected ErrCodeBankUnavailable on target A, got %v", errA)
	}

	holdA, errHoldA := adapterA.HoldFunds(ctx, bank.HoldFundsRequest{})
	if holdA.Status != bank.OperationFailed || errHoldA == nil {
		t.Fatalf("expected hold to fail on target A, got status=%s, err=%v", holdA.Status, errHoldA)
	}

	// Target B is completely UNAFFECTED
	healthB, errB := adapterB.GetHealth(ctx)
	if errB != nil || !healthB.Available {
		t.Fatalf("target B should be unaffected, got avail=%v, err=%v", healthB.Available, errB)
	}

	holdB, errHoldB := adapterB.HoldFunds(ctx, bank.HoldFundsRequest{})
	if errHoldB != nil || holdB.Status != bank.OperationSucceeded {
		t.Fatalf("target B hold should succeed, got status=%s, err=%v", holdB.Status, errHoldB)
	}

	// Stop scenario on A -> Target A immediately restored
	if _, err := ctrl.Stop(ctx, "sc-iso-1", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("stop should succeed, got %v", err)
	}

	healthA2, errA2 := adapterA.GetHealth(ctx)
	if errA2 != nil || !healthA2.Available {
		t.Fatalf("target A should be restored after stop, got avail=%v, err=%v", healthA2.Available, errA2)
	}
}

func TestScenarioLatency(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	var recordedSleep time.Duration
	ctrl.SetSleepFunc(func(ctx context.Context, d time.Duration) error {
		recordedSleep = d
		return nil
	})

	stub := newStubBank("RAIL-A")
	adapter := chaos.NewChaosAdapter("RAIL-A", stub, ctrl)

	req := chaos.StartRequest{
		ScenarioID: "sc-lat-1",
		Type:       chaos.ScenarioTypeLatency,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000, LatencyMs: 250},
	}
	if _, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("failed to start latency scenario: %v", err)
	}

	res, err := adapter.GetHealth(ctx)
	if err != nil || !res.Available {
		t.Fatalf("expected available health with latency, got avail=%v, err=%v", res.Available, err)
	}
	if recordedSleep != 250*time.Millisecond {
		t.Fatalf("expected 250ms sleep, got %v", recordedSleep)
	}
}

func TestScenarioTransientDrop(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	stub := newStubBank("RAIL-A")
	adapter := chaos.NewChaosAdapter("RAIL-A", stub, ctrl)

	// Drop exactly 2 requests
	req := chaos.StartRequest{
		ScenarioID: "sc-drop-1",
		Type:       chaos.ScenarioTypeTransientDrop,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000, DropCount: 2},
	}
	if _, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("failed to start transient drop scenario: %v", err)
	}

	// Call 1: Dropped
	if _, err1 := adapter.GetHealth(ctx); !errors.Is(err1, chaos.ErrTransientDrop) {
		t.Fatalf("call 1 should be dropped with ErrTransientDrop, got %v", err1)
	}

	// Call 2: Dropped
	if _, err2 := adapter.HoldFunds(ctx, bank.HoldFundsRequest{}); err2 == nil {
		t.Fatalf("call 2 should be dropped with error, got nil")
	}

	// Call 3: Allowed through
	res3, err3 := adapter.GetHealth(ctx)
	if err3 != nil || !res3.Available {
		t.Fatalf("call 3 should succeed, got avail=%v, err=%v", res3.Available, err3)
	}

	// Call 4: Allowed through
	res4, err4 := adapter.HoldFunds(ctx, bank.HoldFundsRequest{})
	if err4 != nil || res4.Status != bank.OperationSucceeded {
		t.Fatalf("call 4 should succeed, got status=%s, err=%v", res4.Status, err4)
	}
}

func TestScenarioTemporaryPartition(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	stub := newStubBank("RAIL-A")
	adapter := chaos.NewChaosAdapter("RAIL-A", stub, ctrl)

	req := chaos.StartRequest{
		ScenarioID: "sc-part-1",
		Type:       chaos.ScenarioTypePartition,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	if _, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("failed to start partition scenario: %v", err)
	}

	// Health check fails with network partition error
	if _, err := adapter.GetHealth(ctx); !errors.Is(err, chaos.ErrNetworkPartition) {
		t.Fatalf("expected ErrNetworkPartition, got %v", err)
	}

	// Reset removes partition
	if err := ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("reset should succeed, got %v", err)
	}

	res, err := adapter.GetHealth(ctx)
	if err != nil || !res.Available {
		t.Fatalf("target should be restored after reset, got avail=%v, err=%v", res.Available, err)
	}
}

// In-memory health repo for integration tests
type memHealthRepo struct {
	mu      sync.Mutex
	samples []health.HealthSample
}

func (m *memHealthRepo) Record(ctx context.Context, sample health.HealthSample) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, sample)
	return nil
}

func (m *memHealthRepo) ListRecent(ctx context.Context, targetID string, from, to time.Time, limit int) ([]health.HealthSample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []health.HealthSample
	for _, s := range m.samples {
		if s.TargetID == targetID && !s.SampledAt.Before(from) && !s.SampledAt.After(to) {
			res = append(res, s)
		}
	}
	return res, nil
}

func TestChaosHealthAndCircuitIntegration(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	stub := newStubBank("RAIL-A")
	chaosAdapter := chaos.NewChaosAdapter("RAIL-A", stub, ctrl)

	// Create CircuitBreaker configured to trip on 2 failures
	cbConfig := circuit.Config{
		FailureThreshold:     2,
		TimeoutThreshold:     2,
		RollingWindow:        10 * time.Second,
		OpenCooldown:         5 * time.Second,
		HalfOpenProbeLimit:   1,
		SuccessThreshold:     2,
		RestorationSteps:     1,
		SuccessPolicy:        circuit.SuccessPolicyDecrement,
		StepSuccessThreshold: 2,
	}
	cb, err := circuit.NewBreaker(cbConfig)
	if err != nil {
		t.Fatalf("failed to create breaker: %v", err)
	}

	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// Create HealthService with ProbeGate and observer wired to circuit breaker
	hRepo := &memHealthRepo{}
	hConfig := health.DefaultConfig()
	hConfig.TimeoutThreshold = 500 * time.Millisecond
	hService := health.NewService(hRepo, hConfig)
	hService.SetProbeGate(cb)
	hService.SetSampleObserver(cb.RecordHealthSample)

	// Initial state is CLOSED
	if state := cb.GetState("RAIL-A", now); state != circuit.StateClosed {
		t.Fatalf("expected initial state CLOSED, got %s", state)
	}

	// Start Chaos: BANK_OUTAGE on RAIL-A
	if _, err := ctrl.Start(ctx, chaos.StartRequest{
		ScenarioID: "sc-cb-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 30000},
	}, "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("failed to start chaos: %v", err)
	}

	// Health check 1 fails
	sample1, err := hService.Sample(ctx, "RAIL-A", chaosAdapter, now)
	if err != nil || sample1.Outcome != health.OutcomeFailure {
		t.Fatalf("expected sample 1 to fail, got sample=%+v, err=%v", sample1, err)
	}
	if state := cb.GetState("RAIL-A", now); state != circuit.StateClosed {
		t.Fatalf("expected state CLOSED after 1 failure, got %s", state)
	}

	// Health check 2 fails -> trips circuit to OPEN!
	sample2, err := hService.Sample(ctx, "RAIL-A", chaosAdapter, now)
	if err != nil || sample2.Outcome != health.OutcomeFailure {
		t.Fatalf("expected sample 2 to fail, got sample=%+v, err=%v", sample2, err)
	}
	if state := cb.GetState("RAIL-A", now); state != circuit.StateOpen {
		t.Fatalf("expected state OPEN after 2 failures, got %s", state)
	}

	// While OPEN, payment routing is ineligible (Allow returns false)
	allowed, reason := cb.Allow("RAIL-A", now)
	if allowed {
		t.Fatalf("expected payment routing Allow() to be false while OPEN, got reason=%s", reason)
	}

	// Advance time past OpenCooldown to transition to HALF_OPEN
	now = now.Add(6 * time.Second)
	if state := cb.GetState("RAIL-A", now); state != circuit.StateHalfOpen {
		t.Fatalf("expected state HALF_OPEN after cooldown, got %s", state)
	}

	// HALF_OPEN is recovery-health-probe-only:
	// Normal payment routing Allow() MUST still return false!
	allowed, reason = cb.Allow("RAIL-A", now)
	if allowed {
		t.Fatalf("payment routing must remain ineligible during HALF_OPEN, got allowed=true, reason=%s", reason)
	}

	// Stop chaos
	if _, err := ctrl.Stop(ctx, "sc-cb-1", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("stop should succeed, got %v", err)
	}

	// Recovery health probe executes and succeeds
	probeSample1, err := hService.Sample(ctx, "RAIL-A", chaosAdapter, now)
	if err != nil || probeSample1.Outcome != health.OutcomeSuccess {
		t.Fatalf("expected recovery probe 1 to succeed, got sample=%+v, err=%v", probeSample1, err)
	}

	// 2nd successful probe transitions circuit back to CLOSED
	probeSample2, err := hService.Sample(ctx, "RAIL-A", chaosAdapter, now)
	if err != nil || probeSample2.Outcome != health.OutcomeSuccess {
		t.Fatalf("expected recovery probe 2 to succeed, got sample=%+v, err=%v", probeSample2, err)
	}
	if state := cb.GetState("RAIL-A", now); state != circuit.StateClosed {
		t.Fatalf("expected state CLOSED after successful probes, got %s", state)
	}

	// Once CLOSED, normal payment routing is eligible again
	allowed, _ = cb.Allow("RAIL-A", now)
	if !allowed {
		t.Fatalf("expected payment routing Allow() to be true once CLOSED")
	}
}
