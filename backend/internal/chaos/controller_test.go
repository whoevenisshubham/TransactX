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

// mockRepo implements in-memory chaos.Repository with controllable failure injection
type mockRepo struct {
	mu             sync.Mutex
	scenarios      map[string]chaos.ChaosScenario
	events         []chaos.ChaosEvent
	failCreate     bool
	failUpdate     bool
	failExpire     bool
	failReset      bool
	failGetActive  bool
	failList       bool
	failListActive bool
	expiredCalls   int
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		scenarios: make(map[string]chaos.ChaosScenario),
	}
}

func (m *mockRepo) CreateScenarioWithEvent(ctx context.Context, s chaos.ChaosScenario, event chaos.ChaosEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCreate {
		return errors.New("db error: insert failed")
	}
	m.scenarios[s.ScenarioID] = s
	m.events = append(m.events, event)
	return nil
}

func (m *mockRepo) UpdateScenarioWithEvent(ctx context.Context, s chaos.ChaosScenario, event chaos.ChaosEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failUpdate {
		return errors.New("db error: update failed")
	}
	if _, ok := m.scenarios[s.ScenarioID]; !ok {
		return chaos.ErrScenarioNotFound
	}
	m.scenarios[s.ScenarioID] = s
	m.events = append(m.events, event)
	return nil
}

func (m *mockRepo) GetScenario(ctx context.Context, scenarioID string) (*chaos.ChaosScenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.scenarios[scenarioID]
	if !ok {
		return nil, chaos.ErrScenarioNotFound
	}
	copyScenario := s
	return &copyScenario, nil
}

func (m *mockRepo) GetActiveScenarioByTarget(ctx context.Context, targetID string, now time.Time) (*chaos.ChaosScenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGetActive {
		return nil, chaos.ErrRepoUnavailable
	}
	for _, s := range m.scenarios {
		if s.TargetID == targetID && s.Active && s.ExpiresAt.After(now) {
			copyScenario := s
			return &copyScenario, nil
		}
	}
	return nil, chaos.ErrScenarioNotFound
}

func (m *mockRepo) ListActiveScenarios(ctx context.Context, now time.Time) ([]chaos.ChaosScenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failListActive {
		return nil, chaos.ErrRepoUnavailable
	}
	var list []chaos.ChaosScenario
	for _, s := range m.scenarios {
		if s.Active && s.ExpiresAt.After(now) {
			list = append(list, s)
		}
	}
	return list, nil
}

func (m *mockRepo) ListScenarios(ctx context.Context, activeOnly bool, now time.Time, limit int) ([]chaos.ChaosScenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failList {
		return nil, chaos.ErrRepoUnavailable
	}
	var list []chaos.ChaosScenario
	for _, s := range m.scenarios {
		if activeOnly {
			if s.Active && s.ExpiresAt.After(now) {
				list = append(list, s)
			}
		} else {
			list = append(list, s)
		}
	}
	return list, nil
}

func (m *mockRepo) ExpireScenario(ctx context.Context, scenarioID string, now time.Time, event chaos.ChaosEvent) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failExpire {
		return false, errors.New("db error: expire failed")
	}
	s, ok := m.scenarios[scenarioID]
	if !ok || !s.Active || s.ExpiresAt.After(now) {
		return false, nil
	}
	s.Active = false
	exp := s.ExpiresAt
	s.StoppedAt = &exp
	systemActor := "SYSTEM_AUTO_EXPIRY"
	s.StoppedBy = &systemActor
	s.UpdatedAt = now
	m.scenarios[scenarioID] = s

	m.events = append(m.events, event)
	m.expiredCalls++
	return true, nil
}

func (m *mockRepo) ResetScenarios(ctx context.Context, targetID string, stoppedAt time.Time, stoppedBy string, actorID, actorRole string) ([]chaos.ChaosScenario, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failReset {
		return nil, errors.New("db error: reset failed")
	}
	var resetList []chaos.ChaosScenario
	for id, s := range m.scenarios {
		if s.Active && (targetID == "" || s.TargetID == targetID) {
			s.Active = false
			s.StoppedAt = &stoppedAt
			s.StoppedBy = &stoppedBy
			s.UpdatedAt = stoppedAt
			m.scenarios[id] = s
			resetList = append(resetList, s)

			m.events = append(m.events, chaos.ChaosEvent{
				ScenarioID: s.ScenarioID,
				EventType:  chaos.EventTypeChaosReset,
				TargetID:   s.TargetID,
				FaultType:  string(s.Type),
				ActorID:    actorID,
				ActorRole:  actorRole,
				OccurredAt: stoppedAt,
			})
		}
	}
	return resetList, nil
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

// 1. Authorization
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

	// CUSTOMER denied
	_, err := ctrl.Start(ctx, req, "cust-1", "CUSTOMER")
	if !errors.Is(err, chaos.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for CUSTOMER, got %v", err)
	}

	// MERCHANT denied
	_, err = ctrl.Start(ctx, req, "merch-1", "MERCHANT")
	if !errors.Is(err, chaos.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for MERCHANT, got %v", err)
	}

	// OPS_ADMIN allowed
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

// 2. Target validation
func TestTargetValidation(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	// Build validator from genuine configured execution targets and health targets
	healthTargets := []string{"HEALTH-RAIL-A", "HEALTH-RAIL-B"}
	executionTargets := []string{"RAIL-A", "RAIL-B"}
	ctrl.SetTargetValidator(chaos.BuildTargetValidator(healthTargets, executionTargets))

	// 1. Configured executionTargetID accepted
	reqA := chaos.StartRequest{
		ScenarioID: "sc-val-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	sc, err := ctrl.Start(ctx, reqA, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("configured execution target must be accepted: %v", err)
	}
	if sc.TargetID != "RAIL-A" {
		t.Fatalf("expected target RAIL-A, got %s", sc.TargetID)
	}

	// 2. Configured health target accepted
	reqHealth := chaos.StartRequest{
		ScenarioID: "sc-val-health",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "HEALTH-RAIL-B",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	scHealth, err := ctrl.Start(ctx, reqHealth, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("configured health target must be accepted: %v", err)
	}
	if scHealth.TargetID != "HEALTH-RAIL-B" {
		t.Fatalf("expected target HEALTH-RAIL-B, got %s", scHealth.TargetID)
	}

	// 3. Bank code rejected unless explicitly registered as execution/health target
	bankCodeReq := chaos.StartRequest{
		ScenarioID: "sc-bank-code",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "BANK_A", // logical bank code, NOT execution target
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	_, err = ctrl.Start(ctx, bankCodeReq, "ops-1", "OPS_ADMIN")
	if !errors.Is(err, chaos.ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound for bank code target, got %v", err)
	}

	// 4. Arbitrary unknown target rejected
	unknownReq := chaos.StartRequest{
		ScenarioID: "sc-unk-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "UNKNOWN-TARGET",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	_, err = ctrl.Start(ctx, unknownReq, "ops-1", "OPS_ADMIN")
	if !errors.Is(err, chaos.ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound for unknown target, got %v", err)
	}

	// 5. Target A isolation from Target B: fault on RAIL-A does not affect RAIL-B
	faultA, activeA, errA := ctrl.GetActiveFault(ctx, "RAIL-A")
	if errA != nil || !activeA || faultA == nil {
		t.Fatalf("expected active fault on RAIL-A")
	}
	faultB, activeB, errB := ctrl.GetActiveFault(ctx, "RAIL-B")
	if errB != nil || activeB || faultB != nil {
		t.Fatalf("target A fault must not affect target B: activeB=%v", activeB)
	}
}

// 3. Durable persistence failure handling
func TestDurablePersistenceFailures(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	req := chaos.StartRequest{
		ScenarioID: "sc-fail-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}

	// 1. Simulate DB failure on Start
	repo.failCreate = true
	_, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err == nil {
		t.Fatalf("expected Start to fail when DB persistence fails")
	}

	// Verify in-memory fault was NOT activated
	fault, active, getErr := ctrl.GetActiveFault(ctx, "RAIL-A")
	if getErr != nil || active || fault != nil {
		t.Fatalf("fault must not be activated in memory if DB persistence failed: err=%v", getErr)
	}

	// 2. Allow Start to succeed
	repo.failCreate = false
	_, err = ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("start should succeed: %v", err)
	}

	// 3. Simulate DB failure on Stop
	repo.failUpdate = true
	_, err = ctrl.Stop(ctx, "sc-fail-1", "ops-1", "OPS_ADMIN")
	if err == nil {
		t.Fatalf("expected Stop to fail when DB persistence fails")
	}

	// In-memory state must NOT be mutated to inactive if DB update failed
	fault, active, getErr = ctrl.GetActiveFault(ctx, "RAIL-A")
	if getErr != nil || !active || fault == nil {
		t.Fatalf("scenario must remain active in memory if DB stop failed: err=%v", getErr)
	}

	// 4. Simulate DB failure on Reset
	repo.failReset = true
	err = ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN")
	if err == nil {
		t.Fatalf("expected Reset to fail when DB persistence fails")
	}
}

// 4. Active chaos surviving restart
func TestRestartAndHydration(t *testing.T) {
	repo := newMockRepo()
	ctrl1 := chaos.NewController(repo)
	ctx := context.Background()

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	ctrl1.SetNowFunc(func() time.Time { return baseTime })

	// Start scenario on controller 1
	req := chaos.StartRequest{
		ScenarioID: "sc-restart-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 60000},
	}
	_, err := ctrl1.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Simulate API restart: create a new controller instance with the same repo
	ctrl2 := chaos.NewController(repo)
	ctrl2.SetNowFunc(func() time.Time { return baseTime })

	// 1. Lazy load: GetActiveFault on cache miss queries persistent storage
	fault, active, getErr := ctrl2.GetActiveFault(ctx, "RAIL-A")
	if getErr != nil || !active || fault == nil || fault.ScenarioID != "sc-restart-1" {
		t.Fatalf("active chaos must survive restart via lazy load: active=%v, fault=%+v, err=%v", active, fault, getErr)
	}

	// 2. Simulate second restart and test eager Hydrate
	ctrl3 := chaos.NewController(repo)
	ctrl3.SetNowFunc(func() time.Time { return baseTime })
	if err := ctrl3.Hydrate(ctx); err != nil {
		t.Fatalf("hydration failed: %v", err)
	}
	fault, active, getErr = ctrl3.GetActiveFault(ctx, "RAIL-A")
	if getErr != nil || !active || fault == nil || fault.ScenarioID != "sc-restart-1" {
		t.Fatalf("active chaos must be present after eager Hydrate: active=%v, fault=%+v, err=%v", active, fault, getErr)
	}

	// 3. Stop after restart
	_, err = ctrl3.Stop(ctx, "sc-restart-1", "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("stop after restart failed: %v", err)
	}
	fault, active, getErr = ctrl3.GetActiveFault(ctx, "RAIL-A")
	if getErr != nil || active || fault != nil {
		t.Fatalf("fault should be inactive after stop: err=%v", getErr)
	}

	// 4. Reset after restart clears persisted scenario
	ctrl4 := chaos.NewController(repo)
	ctrl4.SetNowFunc(func() time.Time { return baseTime })
	_, err = ctrl4.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to restart scenario: %v", err)
	}
	// Fresh controller simulating restart before reset
	ctrl5 := chaos.NewController(repo)
	ctrl5.SetNowFunc(func() time.Time { return baseTime })
	if err := ctrl5.Reset(ctx, "", "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("reset all after restart failed: %v", err)
	}
	fault, active, getErr = ctrl5.GetActiveFault(ctx, "RAIL-A")
	if getErr != nil || active || fault != nil {
		t.Fatalf("fault should be inactive after reset: err=%v", getErr)
	}
}

// 5. Durable and exact expiry
func TestDurableAndExactExpiry(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	currentTime := baseTime
	ctrl.SetNowFunc(func() time.Time { return currentTime })

	req := chaos.StartRequest{
		ScenarioID: "sc-exp-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000},
	}
	_, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Active before expiry
	list, err := ctrl.ListScenarios(ctx, true)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 active scenario before expiry, got %d", len(list))
	}

	// Advance time past expiry
	currentTime = baseTime.Add(15 * time.Second)

	// 1. GetActiveFault returns false
	fault, active, getErr := ctrl.GetActiveFault(ctx, "RAIL-A")
	if getErr != nil || active || fault != nil {
		t.Fatalf("expected inactive after expiry: err=%v", getErr)
	}

	// 2. GetScenario returns active = false
	s, err := ctrl.GetScenario(ctx, "sc-exp-1")
	if err != nil || s.Active {
		t.Fatalf("expected active=false from GetScenario, got active=%v, err=%v", s.Active, err)
	}

	// 3. Active listing excludes expired scenarios
	list, err = ctrl.ListScenarios(ctx, true)
	if err != nil || len(list) != 0 {
		t.Fatalf("active listing must exclude expired scenarios, got %d", len(list))
	}

	// 4. Exactly-once expiry event
	events, err := repo.ListEvents(ctx, "sc-exp-1", 50)
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	expiredCount := 0
	for _, e := range events {
		if e.EventType == chaos.EventTypeChaosExpired {
			expiredCount++
		}
	}
	if expiredCount != 1 {
		t.Fatalf("expected exactly 1 CHAOS_EXPIRED event, got %d", expiredCount)
	}

	// 5. Repeated stop does NOT resurrect or emit new stop event
	stopped, err := ctrl.Stop(ctx, "sc-exp-1", "ops-1", "OPS_ADMIN")
	if err != nil || stopped.Active {
		t.Fatalf("stop should not resurrect expired scenario")
	}
}

// 6. Message-drop semantics: UNKNOWN != FAILURE
func TestMessageDropSemantics(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	stub := newStubBank("RAIL-A")
	adapter := chaos.NewChaosAdapter("RAIL-A", stub, ctrl)

	// Start transient drop scenario with DropCount = 1
	req := chaos.StartRequest{
		ScenarioID: "sc-drop-1",
		Type:       chaos.ScenarioTypeTransientDrop,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000, DropCount: 1},
	}
	if _, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN"); err != nil {
		t.Fatalf("failed to start transient drop: %v", err)
	}

	// Invocations:
	// Call 1 is a pre-call transient failure
	holdRes, err := adapter.HoldFunds(ctx, bank.HoldFundsRequest{})
	if err == nil {
		t.Fatalf("call 1 should fail transiently")
	}
	var adapterErr *bank.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected *bank.AdapterError, got %T", err)
	}
	// Definite transient transport rejection:
	if adapterErr.Code != bank.ErrCodeTransientFailure {
		t.Fatalf("expected ErrCodeTransientFailure, got %s", adapterErr.Code)
	}
	// Under pre-call drop, operation status is FAILED (not PENDING)
	if holdRes.Status != bank.OperationFailed {
		t.Fatalf("expected OperationFailed, got %s", holdRes.Status)
	}

	// Crucial distinction: UNKNOWN != FAILURE
	// When bank outcome is truly unknown (e.g. timeout during bank processing), status is OperationPending
	unknownRes := bank.OperationResult{
		Status: bank.OperationPending,
	}
	if unknownRes.Status == holdRes.Status {
		t.Fatalf("UNKNOWN must not equal definite FAILURE: %s == %s", unknownRes.Status, holdRes.Status)
	}

	// Call 2 passes through cleanly
	holdRes2, err2 := adapter.HoldFunds(ctx, bank.HoldFundsRequest{})
	if err2 != nil || holdRes2.Status != bank.OperationSucceeded {
		t.Fatalf("call 2 should succeed, got status=%s, err=%v", holdRes2.Status, err2)
	}
}

// 7. Outage, latency, partition scenarios
func TestScenariosOutageLatencyPartition(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	stubA := newStubBank("RAIL-A")
	stubB := newStubBank("RAIL-B")
	adapterA := chaos.NewChaosAdapter("RAIL-A", stubA, ctrl)
	adapterB := chaos.NewChaosAdapter("RAIL-B", stubB, ctrl)

	// BANK_OUTAGE on RAIL-A
	_, err := ctrl.Start(ctx, chaos.StartRequest{
		ScenarioID: "sc-outage",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000},
	}, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to start outage: %v", err)
	}

	healthA, errA := adapterA.GetHealth(ctx)
	if healthA.Available || errA == nil {
		t.Fatalf("RAIL-A must observe outage")
	}
	// Target B unaffected
	healthB, errB := adapterB.GetHealth(ctx)
	if !healthB.Available || errB != nil {
		t.Fatalf("RAIL-B must be unaffected")
	}

	_ = ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN")

	// LATENCY on RAIL-A
	var sleepRecorded time.Duration
	ctrl.SetSleepFunc(func(ctx context.Context, d time.Duration) error {
		sleepRecorded = d
		return nil
	})
	_, err = ctrl.Start(ctx, chaos.StartRequest{
		ScenarioID: "sc-latency",
		Type:       chaos.ScenarioTypeLatency,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000, LatencyMs: 150},
	}, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to start latency: %v", err)
	}
	_, _ = adapterA.GetHealth(ctx)
	if sleepRecorded != 150*time.Millisecond {
		t.Fatalf("expected 150ms sleep, got %v", sleepRecorded)
	}

	_ = ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN")

	// TEMPORARY_PARTITION on RAIL-A
	_, err = ctrl.Start(ctx, chaos.StartRequest{
		ScenarioID: "sc-partition",
		Type:       chaos.ScenarioTypePartition,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000},
	}, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to start partition: %v", err)
	}
	_, err = adapterA.GetHealth(ctx)
	if !errors.Is(err, chaos.ErrNetworkPartition) {
		t.Fatalf("expected ErrNetworkPartition, got %v", err)
	}
}

// 8. Health -> Circuit -> Routing integration chain
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

func TestChaosHealthCircuitRoutingChain(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	stubA := newStubBank("RAIL-A")
	chaosAdapterA := chaos.NewChaosAdapter("RAIL-A", stubA, ctrl)

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

	hRepo := &memHealthRepo{}
	hConfig := health.DefaultConfig()
	hConfig.TimeoutThreshold = 500 * time.Millisecond
	hService := health.NewService(hRepo, hConfig)
	hService.SetProbeGate(cb)
	hService.SetSampleObserver(cb.RecordHealthSample)

	// Step 1: Initial state is CLOSED
	if state := cb.GetState("RAIL-A", now); state != circuit.StateClosed {
		t.Fatalf("expected initial state CLOSED, got %s", state)
	}

	// Step 2: Inject BANK_OUTAGE into RAIL-A
	_, err = ctrl.Start(ctx, chaos.StartRequest{
		ScenarioID: "sc-chain-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 30000},
	}, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to start chaos: %v", err)
	}

	// Step 3: Health sample 1 observes failure
	s1, err := hService.Sample(ctx, "RAIL-A", chaosAdapterA, now)
	if err != nil || s1.Outcome != health.OutcomeFailure {
		t.Fatalf("sample 1 should fail, got: %+v, err: %v", s1, err)
	}

	// Step 4: Health sample 2 observes failure -> trips circuit to OPEN
	s2, err := hService.Sample(ctx, "RAIL-A", chaosAdapterA, now)
	if err != nil || s2.Outcome != health.OutcomeFailure {
		t.Fatalf("sample 2 should fail, got: %+v, err: %v", s2, err)
	}
	if state := cb.GetState("RAIL-A", now); state != circuit.StateOpen {
		t.Fatalf("circuit must trip to OPEN after 2 failures, got %s", state)
	}

	// Step 5: Routing eligibility excludes OPEN target
	allowed, reason := cb.Allow("RAIL-A", now)
	if allowed {
		t.Fatalf("payment routing Allow() must be false while OPEN, got reason=%s", reason)
	}

	// Step 6: Advance cooldown -> HALF_OPEN (recovery probe only)
	now = now.Add(6 * time.Second)
	if state := cb.GetState("RAIL-A", now); state != circuit.StateHalfOpen {
		t.Fatalf("expected state HALF_OPEN after cooldown, got %s", state)
	}

	// Payment routing MUST remain ineligible during HALF_OPEN
	allowed, _ = cb.Allow("RAIL-A", now)
	if allowed {
		t.Fatalf("payment routing must be ineligible in HALF_OPEN")
	}

	// Step 7: Stop chaos -> recovery probe succeeds
	_, err = ctrl.Stop(ctx, "sc-chain-1", "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	probe1, err := hService.Sample(ctx, "RAIL-A", chaosAdapterA, now)
	if err != nil || probe1.Outcome != health.OutcomeSuccess {
		t.Fatalf("probe 1 should succeed: %+v, err: %v", probe1, err)
	}

	probe2, err := hService.Sample(ctx, "RAIL-A", chaosAdapterA, now)
	if err != nil || probe2.Outcome != health.OutcomeSuccess {
		t.Fatalf("probe 2 should succeed: %+v, err: %v", probe2, err)
	}

	// Transitioned back to CLOSED
	if state := cb.GetState("RAIL-A", now); state != circuit.StateClosed {
		t.Fatalf("expected state CLOSED after successful probes, got %s", state)
	}

	// Payment routing eligible again
	allowed, _ = cb.Allow("RAIL-A", now)
	if !allowed {
		t.Fatalf("payment routing must be eligible once CLOSED")
	}
}

// 9. Runtime target-validator wiring test
func TestRuntimeTargetValidatorWiring(t *testing.T) {
	healthTargetsMap := map[string]bool{"HEALTH-PROBE-1": true}
	executionTargetsList := []string{"EXEC-RAIL-1", "EXEC-RAIL-2"}

	var healthIDs []string
	for id := range healthTargetsMap {
		healthIDs = append(healthIDs, id)
	}
	validator := chaos.BuildTargetValidator(healthIDs, executionTargetsList)

	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctrl.SetTargetValidator(validator)
	ctx := context.Background()

	// Valid wired runtime target
	req := chaos.StartRequest{
		ScenarioID: "sc-wire-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "EXEC-RAIL-1",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	sc, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("expected Start to succeed for wired runtime target, got %v", err)
	}
	if sc.TargetID != "EXEC-RAIL-1" {
		t.Fatalf("expected EXEC-RAIL-1, got %s", sc.TargetID)
	}

	// Unknown target rejected
	badReq := chaos.StartRequest{
		ScenarioID: "sc-wire-2",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "EXEC-RAIL-UNKNOWN",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	_, err = ctrl.Start(ctx, badReq, "ops-1", "OPS_ADMIN")
	if !errors.Is(err, chaos.ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound for unknown wired target, got %v", err)
	}
}

// 10. Expiry persistence error handling and memory/DB consistency
func TestExpiryPersistenceErrorPropagation(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	currentTime := baseTime
	ctrl.SetNowFunc(func() time.Time { return currentTime })

	req := chaos.StartRequest{
		ScenarioID: "sc-exp-err-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 10000},
	}
	_, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Advance time past expiry
	currentTime = baseTime.Add(15 * time.Second)

	// Inject persistence failure on expiry
	repo.failExpire = true

	// 1. GetScenario must surface error
	_, err = ctrl.GetScenario(ctx, "sc-exp-err-1")
	if err == nil {
		t.Fatalf("expected GetScenario to surface expiry persistence error")
	}

	// 2. GetActiveFault must surface error
	_, _, err = ctrl.GetActiveFault(ctx, "RAIL-A")
	if err == nil {
		t.Fatalf("expected GetActiveFault to surface expiry persistence error")
	}

	// 3. ListScenarios must surface error
	_, err = ctrl.ListScenarios(ctx, true)
	if err == nil {
		t.Fatalf("expected ListScenarios to surface expiry persistence error")
	}

	// 4. Restore persistence and verify expiry completes exactly once
	repo.failExpire = false
	s, err := ctrl.GetScenario(ctx, "sc-exp-err-1")
	if err != nil {
		t.Fatalf("GetScenario should succeed after repo recovery: %v", err)
	}
	if s.Active {
		t.Fatalf("scenario must now be inactive")
	}

	// Verify exactly 1 CHAOS_EXPIRED event was recorded
	expiredCount := 0
	for _, e := range repo.events {
		if e.EventType == chaos.EventTypeChaosExpired {
			expiredCount++
		}
	}
	if expiredCount != 1 {
		t.Fatalf("expected exactly 1 CHAOS_EXPIRED event, got %d", expiredCount)
	}
}

// 11. GetActiveFault database error vs scenario not found
func TestGetActiveFaultDatabaseErrorHandling(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	// 1. Cache miss with ErrScenarioNotFound -> returns nil, false, nil
	fault, active, err := ctrl.GetActiveFault(ctx, "NONEXISTENT")
	if err != nil || active || fault != nil {
		t.Fatalf("expected nil, false, nil for not found: active=%v, err=%v", active, err)
	}

	// 2. Cache miss with DB failure -> returns error, does NOT swallow DB error!
	repo.failGetActive = true
	fault, active, err = ctrl.GetActiveFault(ctx, "RAIL-A")
	if err == nil {
		t.Fatalf("expected error from GetActiveFault when DB query fails")
	}
	if active || fault != nil {
		t.Fatalf("expected inactive on error")
	}

	// 3. Healthy DB -> Start scenario and verify cache hit avoids DB query
	repo.failGetActive = false
	req := chaos.StartRequest{
		ScenarioID: "sc-cache-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 60000},
	}
	_, err = ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Break DB again: cache hit should still return the active scenario without querying DB!
	repo.failGetActive = true
	fault, active, err = ctrl.GetActiveFault(ctx, "RAIL-A")
	if err != nil {
		t.Fatalf("cache hit should not fail even if repo is failing: %v", err)
	}
	if !active || fault == nil || fault.ScenarioID != "sc-cache-1" {
		t.Fatalf("expected cached fault on cache hit")
	}
}

// 12. Start fails when active-state lookup is untrustworthy
func TestStartFailsOnUntrustworthyDurableLookup(t *testing.T) {
	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	// Simulate untrustworthy DB lookup
	repo.failGetActive = true

	req := chaos.StartRequest{
		ScenarioID: "sc-trust-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 60000},
	}

	// Start MUST fail because durable state cannot be verified
	_, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
	if err == nil {
		t.Fatalf("expected Start to fail when durable lookup is untrustworthy")
	}

	// Verify nothing was persisted
	if len(repo.scenarios) != 0 {
		t.Fatalf("no scenario should be persisted in repo: %d", len(repo.scenarios))
	}

	// Verify nothing active in memory
	repo.failGetActive = false
	fault, active, err := ctrl.GetActiveFault(ctx, "RAIL-A")
	if err != nil || active || fault != nil {
		t.Fatalf("no in-memory fault should be activated")
	}
}

// 13. Hydration failure handling
func TestHydrationFailureHandling(t *testing.T) {
	repo := newMockRepo()
	repo.failListActive = true
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	err := ctrl.Hydrate(ctx)
	if err == nil {
		t.Fatalf("expected Hydrate to return error when repo fails")
	}
}

// 14. Exactly-once atomic expiry tests
func TestExactlyOnceExpiryAtomic(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	s := chaos.ChaosScenario{
		ScenarioID: "sc-atomic-exp",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Active:     true,
		StartedAt:  now.Add(-20 * time.Second),
		ExpiresAt:  now.Add(-10 * time.Second),
	}
	repo.scenarios[s.ScenarioID] = s

	event := chaos.ChaosEvent{
		ScenarioID: s.ScenarioID,
		EventType:  chaos.EventTypeChaosExpired,
		TargetID:   s.TargetID,
		FaultType:  string(s.Type),
		ActorID:    "SYSTEM",
		ActorRole:  "SYSTEM",
		OccurredAt: s.ExpiresAt,
	}

	// A) First expiry: 1 row changed, 1 event inserted
	first, err := repo.ExpireScenario(ctx, s.ScenarioID, now, event)
	if err != nil || !first {
		t.Fatalf("expected first expiry to return true: %v, err=%v", first, err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected exactly 1 event after first expiry, got %d", len(repo.events))
	}

	// B) Second expiry: 0 rows changed, 0 second event
	second, err := repo.ExpireScenario(ctx, s.ScenarioID, now, event)
	if err != nil || second {
		t.Fatalf("expected second expiry to return false: %v, err=%v", second, err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected no additional event after second expiry, got %d", len(repo.events))
	}

	// C) Concurrent expiry: exactly one winner
	scenarioIDConcurrent := "sc-atomic-conc"
	sConc := chaos.ChaosScenario{
		ScenarioID: scenarioIDConcurrent,
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Active:     true,
		StartedAt:  now.Add(-20 * time.Second),
		ExpiresAt:  now.Add(-10 * time.Second),
	}
	repo.scenarios[scenarioIDConcurrent] = sConc

	eventConc := chaos.ChaosEvent{
		ScenarioID: scenarioIDConcurrent,
		EventType:  chaos.EventTypeChaosExpired,
		TargetID:   sConc.TargetID,
		FaultType:  string(sConc.Type),
		ActorID:    "SYSTEM",
		ActorRole:  "SYSTEM",
		OccurredAt: sConc.ExpiresAt,
	}

	var wg sync.WaitGroup
	var successCount int
	var countMu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, expErr := repo.ExpireScenario(ctx, scenarioIDConcurrent, now, eventConc)
			if expErr == nil && won {
				countMu.Lock()
				successCount++
				countMu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 concurrent expiry to win, got %d", successCount)
	}
	concurrentEvents := 0
	for _, e := range repo.events {
		if e.ScenarioID == scenarioIDConcurrent && e.EventType == chaos.EventTypeChaosExpired {
			concurrentEvents++
		}
	}
	if concurrentEvents != 1 {
		t.Fatalf("expected exactly 1 concurrent event recorded, got %d", concurrentEvents)
	}
}

// 15. Chaos adapter fails closed when repository is unavailable
func TestChaosAdapterFailClosedOnRepositoryUnavailable(t *testing.T) {
	repo := newMockRepo()
	repo.failGetActive = true
	ctrl := chaos.NewController(repo)
	ctx := context.Background()

	stub := newStubBank("RAIL-A")
	adapter := chaos.NewChaosAdapter("RAIL-A", stub, ctrl)

	// Health check fails closed with ErrCodeBankUnavailable
	healthRes, err := adapter.GetHealth(ctx)
	if err == nil {
		t.Fatalf("expected error from GetHealth when chaos repo is unavailable")
	}
	if healthRes.Available {
		t.Fatalf("health must be unavailable when chaos repo is unavailable")
	}
	var adapterErr *bank.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Code != bank.ErrCodeBankUnavailable {
		t.Fatalf("expected ErrCodeBankUnavailable, got %v", err)
	}

	// HoldFunds fails closed with ErrCodeBankUnavailable and OperationFailed
	holdRes, err := adapter.HoldFunds(ctx, bank.HoldFundsRequest{})
	if err == nil {
		t.Fatalf("expected error from HoldFunds when chaos repo is unavailable")
	}
	if holdRes.Status != bank.OperationFailed {
		t.Fatalf("expected OperationFailed, got %s", holdRes.Status)
	}

	// ProvisionalCredit fails closed
	creditRes, err := adapter.ProvisionalCredit(ctx, bank.ProvisionalCreditRequest{})
	if err == nil || creditRes.Status != bank.OperationFailed {
		t.Fatalf("expected OperationFailed from ProvisionalCredit: %v", err)
	}

	// ResolveAccount fails closed with AccountInactive
	accRes, err := adapter.ResolveAccount(ctx, bank.ResolveAccountRequest{})
	if err == nil || accRes.Status != bank.AccountInactive {
		t.Fatalf("expected AccountInactive from ResolveAccount: %v", err)
	}
}
