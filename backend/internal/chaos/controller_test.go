package chaos_test

import (
	"context"
	"errors"
	"fmt"
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
	var latest *chaos.ChaosScenario
	for _, s := range m.scenarios {
		if s.TargetID == targetID && s.Active {
			sc := s
			if latest == nil || sc.StartedAt.After(latest.StartedAt) {
				latest = &sc
			}
		}
	}
	if latest != nil {
		copyScenario := *latest
		return &copyScenario, nil
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
		if s.Active {
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
			if s.Active && !chaos.IsExpired(now, s.ExpiresAt) {
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
	if !ok || !s.Active || !chaos.IsExpired(now, s.ExpiresAt) {
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
			isExpired := chaos.IsExpired(stoppedAt, s.ExpiresAt)
			if isExpired {
				exp := s.ExpiresAt
				s.StoppedAt = &exp
				systemActor := "SYSTEM_AUTO_EXPIRY"
				s.StoppedBy = &systemActor
				s.UpdatedAt = stoppedAt
				m.scenarios[id] = s
				resetList = append(resetList, s)

				m.events = append(m.events, chaos.ChaosEvent{
					ScenarioID: s.ScenarioID,
					EventType:  chaos.EventTypeChaosExpired,
					TargetID:   s.TargetID,
					FaultType:  string(s.Type),
					ActorID:    "SYSTEM",
					ActorRole:  "SYSTEM",
					Details: map[string]any{
						"expiredAt": s.ExpiresAt.Format(time.RFC3339),
					},
					OccurredAt: s.ExpiresAt,
				})
			} else {
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
					Details: map[string]any{
						"resetAt": stoppedAt.Format(time.RFC3339),
					},
					OccurredAt: stoppedAt,
				})
			}
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
	// Simulate production startup state
	// Legacy bank health adapters registered in healthTargets
	healthTargetsMap := map[string]bool{
		"BANK-A":           true, // legacy bank health adapter key
		"BANK-B":           true, // legacy bank health adapter key
		"SWITCH-HEALTH-1":  true, // explicitly configured non-bank health target
		"EXEC-RAIL-1":      true, // health endpoint for execution target
	}
	// Bank ownership IDs
	bankIDsMap := map[string]string{
		"BANK-A": "00000000-0000-0000-0000-000000000001",
		"BANK-B": "00000000-0000-0000-0000-000000000002",
	}
	// Configured execution rails
	executionTargetsList := []string{"EXEC-RAIL-1", "EXEC-RAIL-2"}

	// Production construction logic (identical to main.go)
	var bankCodes []string
	for code := range bankIDsMap {
		bankCodes = append(bankCodes, code)
	}
	var explicitHealthTargetIDs []string
	for targetID := range healthTargetsMap {
		if _, isBank := bankIDsMap[targetID]; !isBank {
			explicitHealthTargetIDs = append(explicitHealthTargetIDs, targetID)
		}
	}
	validator := chaos.BuildTargetValidator(explicitHealthTargetIDs, executionTargetsList, bankCodes...)

	repo := newMockRepo()
	ctrl := chaos.NewController(repo)
	ctrl.SetTargetValidator(validator)
	ctx := context.Background()

	// 1. Configured executionTargetID is accepted
	reqExec := chaos.StartRequest{
		ScenarioID: "sc-wire-1",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "EXEC-RAIL-1",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	sc, err := ctrl.Start(ctx, reqExec, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("expected Start to succeed for configured executionTargetID, got %v", err)
	}
	if sc.TargetID != "EXEC-RAIL-1" {
		t.Fatalf("expected EXEC-RAIL-1, got %s", sc.TargetID)
	}

	// 2. Explicitly configured non-bank health target is accepted
	reqHealth := chaos.StartRequest{
		ScenarioID: "sc-wire-2",
		Type:       chaos.ScenarioTypeLatency,
		TargetID:   "SWITCH-HEALTH-1",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000, LatencyMs: 100},
	}
	scHealth, err := ctrl.Start(ctx, reqHealth, "ops-1", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("expected Start to succeed for explicitly configured non-bank health target, got %v", err)
	}
	if scHealth.TargetID != "SWITCH-HEALTH-1" {
		t.Fatalf("expected SWITCH-HEALTH-1, got %s", scHealth.TargetID)
	}

	// 3. BANK-A is rejected when it exists only as a bank ownership/legacy health adapter key
	badReqA := chaos.StartRequest{
		ScenarioID: "sc-wire-bank-a",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "BANK-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	_, err = ctrl.Start(ctx, badReqA, "ops-1", "OPS_ADMIN")
	if !errors.Is(err, chaos.ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound for BANK-A, got %v", err)
	}

	// 4. BANK-B is rejected under the same condition
	badReqB := chaos.StartRequest{
		ScenarioID: "sc-wire-bank-b",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "BANK-B",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	_, err = ctrl.Start(ctx, badReqB, "ops-1", "OPS_ADMIN")
	if !errors.Is(err, chaos.ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound for BANK-B, got %v", err)
	}

	// 5. UNKNOWN-RAIL is rejected
	badReqUnknown := chaos.StartRequest{
		ScenarioID: "sc-wire-unknown",
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "UNKNOWN-RAIL",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}
	_, err = ctrl.Start(ctx, badReqUnknown, "ops-1", "OPS_ADMIN")
	if !errors.Is(err, chaos.ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound for UNKNOWN-RAIL, got %v", err)
	}

	// 6. Target A remains isolated from target B
	// sc-wire-1 is active on EXEC-RAIL-1; verify EXEC-RAIL-2 has no active fault
	fault1, hasFault1, err := ctrl.GetActiveFault(ctx, "EXEC-RAIL-1")
	if err != nil || !hasFault1 || fault1 == nil {
		t.Fatalf("EXEC-RAIL-1 must have active fault, got fault=%v, has=%v, err=%v", fault1, hasFault1, err)
	}
	fault2, hasFault2, err := ctrl.GetActiveFault(ctx, "EXEC-RAIL-2")
	if err != nil || hasFault2 || fault2 != nil {
		t.Fatalf("EXEC-RAIL-2 must have NO active fault, got fault=%v, has=%v, err=%v", fault2, hasFault2, err)
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

// 16. Reset vs Expiry atomic semantics and exactly-once terminal event
func TestResetVsExpiryAtomicSemantics(t *testing.T) {
	ctx := context.Background()
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// A) Future active scenario: RESET emits CHAOS_RESET = 1, CHAOS_EXPIRED = 0
	t.Run("Future active scenario reset emits CHAOS_RESET only", func(t *testing.T) {
		repo := newMockRepo()
		ctrl := chaos.NewController(repo)
		currentTime := baseTime
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(t string) bool { return true })

		req := chaos.StartRequest{
			ScenarioID: "sc-future-reset",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 60000}, // expires in 60s
		}
		_, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("start failed: %v", err)
		}

		// Reset while active and unexpired
		currentTime = baseTime.Add(10 * time.Second) // 50s before expiry
		if err := ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN"); err != nil {
			t.Fatalf("reset failed: %v", err)
		}

		// Verify inactive in repo and memory
		sc, err := repo.GetScenario(ctx, "sc-future-reset")
		if err != nil || sc.Active {
			t.Fatalf("scenario must be inactive after reset")
		}
		if sc.StoppedBy == nil || *sc.StoppedBy != "ops-1" {
			t.Fatalf("stoppedBy must be ops-1, got %v", sc.StoppedBy)
		}

		// Count terminal events
		resetEvents := 0
		expiredEvents := 0
		for _, e := range repo.events {
			if e.ScenarioID == "sc-future-reset" {
				if e.EventType == chaos.EventTypeChaosReset {
					resetEvents++
				} else if e.EventType == chaos.EventTypeChaosExpired {
					expiredEvents++
				}
			}
		}
		if resetEvents != 1 || expiredEvents != 0 {
			t.Fatalf("expected CHAOS_RESET=1, CHAOS_EXPIRED=0, got reset=%d, exp=%d", resetEvents, expiredEvents)
		}
	})

	// B) Already-expired active scenario: RESET emits CHAOS_EXPIRED = 1, CHAOS_RESET = 0
	t.Run("Already-expired active scenario reset emits CHAOS_EXPIRED only", func(t *testing.T) {
		repo := newMockRepo()
		ctrl := chaos.NewController(repo)
		currentTime := baseTime
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(t string) bool { return true })

		req := chaos.StartRequest{
			ScenarioID: "sc-expired-reset",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 10000}, // expires in 10s
		}
		_, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("start failed: %v", err)
		}

		// Advance clock past expiry WITHOUT calling GetScenario or anything that triggers auto-expiry
		currentTime = baseTime.Add(20 * time.Second) // 10s after expiry

		// Call Reset
		if err := ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN"); err != nil {
			t.Fatalf("reset failed: %v", err)
		}

		// Verify inactive in repo and memory
		sc, err := repo.GetScenario(ctx, "sc-expired-reset")
		if err != nil || sc.Active {
			t.Fatalf("scenario must be inactive after reset")
		}
		if sc.StoppedBy == nil || *sc.StoppedBy != "SYSTEM_AUTO_EXPIRY" {
			t.Fatalf("stoppedBy must be SYSTEM_AUTO_EXPIRY, got %v", sc.StoppedBy)
		}

		// Count terminal events
		resetEvents := 0
		expiredEvents := 0
		for _, e := range repo.events {
			if e.ScenarioID == "sc-expired-reset" {
				if e.EventType == chaos.EventTypeChaosReset {
					resetEvents++
				} else if e.EventType == chaos.EventTypeChaosExpired {
					expiredEvents++
				}
			}
		}
		if resetEvents != 0 || expiredEvents != 1 {
			t.Fatalf("expected CHAOS_RESET=0, CHAOS_EXPIRED=1, got reset=%d, exp=%d", resetEvents, expiredEvents)
		}
	})

	// C) Run RESET again: no new terminal event
	t.Run("Repeated reset produces no new terminal event", func(t *testing.T) {
		repo := newMockRepo()
		ctrl := chaos.NewController(repo)
		currentTime := baseTime
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(t string) bool { return true })

		req := chaos.StartRequest{
			ScenarioID: "sc-repeated-reset",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 60000},
		}
		_, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("start failed: %v", err)
		}

		if err := ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN"); err != nil {
			t.Fatalf("first reset failed: %v", err)
		}
		eventsAfterFirst := len(repo.events)

		// Call Reset again
		if err := ctrl.Reset(ctx, "RAIL-A", "ops-1", "OPS_ADMIN"); err != nil {
			t.Fatalf("second reset failed: %v", err)
		}
		eventsAfterSecond := len(repo.events)

		if eventsAfterSecond != eventsAfterFirst {
			t.Fatalf("repeated reset must not produce new events, before=%d, after=%d", eventsAfterFirst, eventsAfterSecond)
		}
	})

	// D) Mixed reset: one future scenario, one expired scenario
	t.Run("Mixed reset classifies future to CHAOS_RESET and expired to CHAOS_EXPIRED", func(t *testing.T) {
		repo := newMockRepo()
		ctrl := chaos.NewController(repo)
		currentTime := baseTime
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(t string) bool { return true })

		// Scenario 1: expires in 60s
		req1 := chaos.StartRequest{
			ScenarioID: "sc-mixed-future",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 60000},
		}
		_, err := ctrl.Start(ctx, req1, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("start 1 failed: %v", err)
		}

		// Scenario 2: expires in 10s
		req2 := chaos.StartRequest{
			ScenarioID: "sc-mixed-expired",
			Type:       chaos.ScenarioTypeLatency,
			TargetID:   "RAIL-B",
			Parameters: chaos.ScenarioParameters{DurationMs: 10000, LatencyMs: 50},
		}
		_, err = ctrl.Start(ctx, req2, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("start 2 failed: %v", err)
		}

		// Advance clock past scenario 2 expiry, but before scenario 1 expiry
		currentTime = baseTime.Add(20 * time.Second)

		// Global reset (targetID == "")
		if err := ctrl.Reset(ctx, "", "ops-admin-global", "OPS_ADMIN"); err != nil {
			t.Fatalf("global reset failed: %v", err)
		}

		// Verify sc-mixed-future has CHAOS_RESET=1, CHAOS_EXPIRED=0
		futureReset := 0
		futureExpired := 0
		for _, e := range repo.events {
			if e.ScenarioID == "sc-mixed-future" {
				if e.EventType == chaos.EventTypeChaosReset {
					futureReset++
				} else if e.EventType == chaos.EventTypeChaosExpired {
					futureExpired++
				}
			}
		}
		if futureReset != 1 || futureExpired != 0 {
			t.Fatalf("future scenario: expected reset=1, exp=0, got reset=%d, exp=%d", futureReset, futureExpired)
		}

		// Verify sc-mixed-expired has CHAOS_RESET=0, CHAOS_EXPIRED=1
		expiredReset := 0
		expiredExpired := 0
		for _, e := range repo.events {
			if e.ScenarioID == "sc-mixed-expired" {
				if e.EventType == chaos.EventTypeChaosReset {
					expiredReset++
				} else if e.EventType == chaos.EventTypeChaosExpired {
					expiredExpired++
				}
			}
		}
		if expiredReset != 0 || expiredExpired != 1 {
			t.Fatalf("expired scenario: expected reset=0, exp=1, got reset=%d, exp=%d", expiredReset, expiredExpired)
		}
	})

	// E) Concurrent expiry/reset: multiple attempts against same scenario produce exactly one terminal event
	t.Run("Concurrent expiry and reset produces exactly one terminal lifecycle event", func(t *testing.T) {
		repo := newMockRepo()
		ctx := context.Background()

		// Setup expired scenario in repository directly
		scenarioID := "sc-concurrent-term"
		expiresAt := baseTime.Add(5 * time.Second)
		startedAt := baseTime
		s := chaos.ChaosScenario{
			ScenarioID: scenarioID,
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			StartedAt:  startedAt,
			ExpiresAt:  expiresAt,
			Active:     true,
			Mode:       chaos.ExecutionModeSimulation,
			CreatedBy:  "ops-1",
		}
		repo.scenarios[scenarioID] = s

		// Advance clock past expiry
		now := baseTime.Add(10 * time.Second)

		eventExp := chaos.ChaosEvent{
			ScenarioID: scenarioID,
			EventType:  chaos.EventTypeChaosExpired,
			TargetID:   "RAIL-A",
			FaultType:  string(s.Type),
			ActorID:    "SYSTEM",
			ActorRole:  "SYSTEM",
			OccurredAt: expiresAt,
		}

		var wg sync.WaitGroup
		// 5 goroutines trying ExpireScenario, 5 goroutines trying ResetScenarios
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = repo.ExpireScenario(ctx, scenarioID, now, eventExp)
			}()
		}
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = repo.ResetScenarios(ctx, "RAIL-A", now, "ops-admin", "ops-admin", "OPS_ADMIN")
			}()
		}
		wg.Wait()

		terminalEvents := 0
		for _, e := range repo.events {
			if e.ScenarioID == scenarioID && (e.EventType == chaos.EventTypeChaosExpired || e.EventType == chaos.EventTypeChaosReset) {
				terminalEvents++
			}
		}
		if terminalEvents != 1 {
			t.Fatalf("expected exactly 1 terminal event under concurrent expiry/reset, got %d", terminalEvents)
		}
	})
}

func TestExactBoundaryExpiryAndStaleCleanup(t *testing.T) {
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	// A. Stop at exact expiry: now == expires_at -> CHAOS_EXPIRED, not CHAOS_STOPPED
	t.Run("A_StopAtExactExpiry_EmitsChaosExpiredNotStopped", func(t *testing.T) {
		repo := newMockRepo()
		ctrl := chaos.NewController(repo)
		currentTime := baseTime
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(targetID string) bool { return true })

		req := chaos.StartRequest{
			ScenarioID: "sc-stop-exact-exp",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 5000},
		}
		sc, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}
		if !sc.Active {
			t.Fatalf("scenario should be active")
		}

		// Advance clock to EXACT expiry: now == expires_at
		currentTime = sc.ExpiresAt

		stopped, err := ctrl.Stop(ctx, sc.ScenarioID, "ops-admin", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("failed to stop at exact expiry: %v", err)
		}
		if stopped.Active {
			t.Fatalf("expected scenario to be inactive")
		}
		if stopped.StoppedBy == nil || *stopped.StoppedBy != "SYSTEM_AUTO_EXPIRY" {
			t.Fatalf("expected stoppedBy to be SYSTEM_AUTO_EXPIRY, got %v", stopped.StoppedBy)
		}
		if stopped.StoppedAt == nil || !stopped.StoppedAt.Equal(sc.ExpiresAt) {
			t.Fatalf("expected stoppedAt to be expiresAt (%v), got %v", sc.ExpiresAt, stopped.StoppedAt)
		}

		// Verify repository events: exactly one CHAOS_EXPIRED, zero CHAOS_STOPPED
		expiredCount := 0
		stoppedCount := 0
		for _, e := range repo.events {
			if e.ScenarioID == sc.ScenarioID {
				if e.EventType == chaos.EventTypeChaosExpired {
					expiredCount++
				} else if e.EventType == chaos.EventTypeChaosStopped {
					stoppedCount++
				}
			}
		}
		if expiredCount != 1 || stoppedCount != 0 {
			t.Fatalf("expected exactly 1 CHAOS_EXPIRED and 0 CHAOS_STOPPED, got expired=%d, stopped=%d", expiredCount, stoppedCount)
		}

		// Repeated stop must be idempotent and emit no new events
		eventsBefore := len(repo.events)
		secondStop, err := ctrl.Stop(ctx, sc.ScenarioID, "ops-admin", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("repeated stop error: %v", err)
		}
		if secondStop.Active {
			t.Fatalf("repeated stop must remain inactive")
		}
		if len(repo.events) != eventsBefore {
			t.Fatalf("repeated stop must not emit new events, before=%d, after=%d", eventsBefore, len(repo.events))
		}
	})

	// B. GetActiveFault at exact expiry: -> inactive, exactly one CHAOS_EXPIRED
	t.Run("B_GetActiveFaultAtExactExpiry_BecomesInactiveEmitsExpired", func(t *testing.T) {
		repo := newMockRepo()
		ctrl := chaos.NewController(repo)
		currentTime := baseTime
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(targetID string) bool { return true })

		req := chaos.StartRequest{
			ScenarioID: "sc-fault-exact-exp",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 4000},
		}
		sc, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		// Advance clock to EXACT expiry: now == expires_at
		currentTime = sc.ExpiresAt

		activeFault, active, err := ctrl.GetActiveFault(ctx, "RAIL-A")
		if err != nil {
			t.Fatalf("GetActiveFault error: %v", err)
		}
		if active || activeFault != nil {
			t.Fatalf("expected fault to be inactive at exact expiry, got active=%v, fault=%+v", active, activeFault)
		}

		// Verify exactly one CHAOS_EXPIRED event
		expiredCount := 0
		for _, e := range repo.events {
			if e.ScenarioID == sc.ScenarioID && e.EventType == chaos.EventTypeChaosExpired {
				expiredCount++
			}
		}
		if expiredCount != 1 {
			t.Fatalf("expected exactly 1 CHAOS_EXPIRED event, got %d", expiredCount)
		}

		// Second call returns inactive without duplicate event
		eventsBefore := len(repo.events)
		_, active2, err2 := ctrl.GetActiveFault(ctx, "RAIL-A")
		if err2 != nil || active2 {
			t.Fatalf("second GetActiveFault unexpected: err=%v, active=%v", err2, active2)
		}
		if len(repo.events) != eventsBefore {
			t.Fatalf("second GetActiveFault must not emit additional events")
		}
	})

	// C. ListScenarios(active=true) at exact expiry: -> scenario excluded as active
	t.Run("C_ListScenariosActiveOnlyAtExactExpiry_ExcludesScenario", func(t *testing.T) {
		repo := newMockRepo()
		ctrl := chaos.NewController(repo)
		currentTime := baseTime
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(targetID string) bool { return true })

		req := chaos.StartRequest{
			ScenarioID: "sc-list-exact-exp",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 3000},
		}
		sc, err := ctrl.Start(ctx, req, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		// Advance clock to EXACT expiry: now == expires_at
		currentTime = sc.ExpiresAt

		list, err := ctrl.ListScenarios(ctx, true)
		if err != nil {
			t.Fatalf("ListScenarios error: %v", err)
		}
		for _, s := range list {
			if s.ScenarioID == sc.ScenarioID {
				t.Fatalf("exact-expiry scenario must be excluded from active-only list: %+v", s)
			}
		}
	})

	// D. Start at exact expiry: -> old scenario finalized as expired, new scenario may start
	t.Run("D_StartAtExactExpiry_FinalizesOldAndStartsNew", func(t *testing.T) {
		repo := newMockRepo()
		ctrl := chaos.NewController(repo)
		currentTime := baseTime
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(targetID string) bool { return true })

		req1 := chaos.StartRequest{
			ScenarioID: "sc-start-exact-old",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 6000},
		}
		sc1, err := ctrl.Start(ctx, req1, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("failed to start sc1: %v", err)
		}

		// Advance clock to EXACT expiry: now == sc1.ExpiresAt
		currentTime = sc1.ExpiresAt

		req2 := chaos.StartRequest{
			ScenarioID: "sc-start-exact-new",
			Type:       chaos.ScenarioTypeLatency,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 3000, LatencyMs: 200},
		}
		sc2, err := ctrl.Start(ctx, req2, "ops-2", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("failed to start sc2 at exact expiry of sc1: %v", err)
		}
		if !sc2.Active || sc2.ScenarioID != "sc-start-exact-new" {
			t.Fatalf("sc2 unexpected state: %+v", sc2)
		}

		// Verify sc1 is finalized as inactive with CHAOS_EXPIRED
		oldSc, err := ctrl.GetScenario(ctx, "sc-start-exact-old")
		if err != nil || oldSc.Active {
			t.Fatalf("old scenario must be inactive, got active=%v, err=%v", oldSc.Active, err)
		}
		if oldSc.StoppedBy == nil || *oldSc.StoppedBy != "SYSTEM_AUTO_EXPIRY" {
			t.Fatalf("expected stoppedBy SYSTEM_AUTO_EXPIRY, got %v", oldSc.StoppedBy)
		}

		// Verify events: sc1 has exactly 1 CHAOS_EXPIRED, sc2 has 1 CHAOS_STARTED
		sc1Expired := 0
		sc2Started := 0
		for _, e := range repo.events {
			if e.ScenarioID == "sc-start-exact-old" && e.EventType == chaos.EventTypeChaosExpired {
				sc1Expired++
			}
			if e.ScenarioID == "sc-start-exact-new" && e.EventType == chaos.EventTypeChaosStarted {
				sc2Started++
			}
		}
		if sc1Expired != 1 {
			t.Fatalf("expected sc1 CHAOS_EXPIRED=1, got %d", sc1Expired)
		}
		if sc2Started != 1 {
			t.Fatalf("expected sc2 CHAOS_STARTED=1, got %d", sc2Started)
		}
	})

	// E. Restart stale-expiry case:
	//    1. persist active scenario
	//    2. advance deterministic clock past expiry
	//    3. create fresh Controller
	//    4. Start new scenario on same target
	//    5. old scenario must be inactive in repository
	//    6. old scenario must have exactly one CHAOS_EXPIRED
	//    7. new scenario must be the only active scenario on that target
	t.Run("E_RestartStaleExpiryCase_FreshControllerFinalizesOldOnStart", func(t *testing.T) {
		repo := newMockRepo()
		currentTime := baseTime
		ctrl1 := chaos.NewController(repo)
		ctrl1.SetNowFunc(func() time.Time { return currentTime })
		ctrl1.SetTargetValidator(func(targetID string) bool { return true })

		req1 := chaos.StartRequest{
			ScenarioID: "sc-stale-restart-old",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 5000},
		}
		sc1, err := ctrl1.Start(ctx, req1, "ops-1", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("failed to start sc1: %v", err)
		}

		// 2. Advance deterministic clock past expiry (e.g. +10s)
		currentTime = baseTime.Add(10 * time.Second)

		// 3. Create fresh Controller without calling Hydrate (cold controller)
		ctrl2 := chaos.NewController(repo)
		ctrl2.SetNowFunc(func() time.Time { return currentTime })
		ctrl2.SetTargetValidator(func(targetID string) bool { return true })

		// 4. Start new scenario on the same target
		req2 := chaos.StartRequest{
			ScenarioID: "sc-stale-restart-new",
			Type:       chaos.ScenarioTypeLatency,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 4000, LatencyMs: 150},
		}
		sc2, err := ctrl2.Start(ctx, req2, "ops-2", "OPS_ADMIN")
		if err != nil {
			t.Fatalf("ctrl2.Start failed: %v", err)
		}
		if !sc2.Active {
			t.Fatalf("sc2 should be active")
		}

		// 5. Old scenario must be inactive in repository
		oldInRepo, ok := repo.scenarios["sc-stale-restart-old"]
		if !ok || oldInRepo.Active {
			t.Fatalf("old scenario must be inactive in repo: active=%v", oldInRepo.Active)
		}
		if oldInRepo.StoppedBy == nil || *oldInRepo.StoppedBy != "SYSTEM_AUTO_EXPIRY" {
			t.Fatalf("expected old scenario stoppedBy SYSTEM_AUTO_EXPIRY, got %v", oldInRepo.StoppedBy)
		}
		if oldInRepo.StoppedAt == nil || !oldInRepo.StoppedAt.Equal(sc1.ExpiresAt) {
			t.Fatalf("expected old scenario stoppedAt == expiresAt (%v), got %v", sc1.ExpiresAt, oldInRepo.StoppedAt)
		}

		// 6. Old scenario must have exactly one CHAOS_EXPIRED
		oldExpiredCount := 0
		for _, e := range repo.events {
			if e.ScenarioID == "sc-stale-restart-old" && e.EventType == chaos.EventTypeChaosExpired {
				oldExpiredCount++
			}
		}
		if oldExpiredCount != 1 {
			t.Fatalf("expected old scenario to have exactly 1 CHAOS_EXPIRED, got %d", oldExpiredCount)
		}

		// 7. New scenario must be the only active scenario on that target in repository
		activeCountOnTarget := 0
		for _, s := range repo.scenarios {
			if s.TargetID == "RAIL-A" && s.Active {
				activeCountOnTarget++
				if s.ScenarioID != "sc-stale-restart-new" {
					t.Fatalf("unexpected active scenario in repo: %+v", s)
				}
			}
		}
		if activeCountOnTarget != 1 {
			t.Fatalf("expected exactly 1 active scenario on RAIL-A in repo, got %d", activeCountOnTarget)
		}
	})

	// F. Concurrent stale-expiry case:
	//    race Start/Reset/ExpireScenario against the same expired scenario
	//    -> exactly one terminal event
	//    -> no duplicate active scenarios for the target
	t.Run("F_ConcurrentStaleExpiryCase_RaceStartResetExpire", func(t *testing.T) {
		repo := newMockRepo()
		currentTime := baseTime.Add(10 * time.Second)

		// Setup stale active scenario in repository directly
		scenarioID := "sc-concurrent-stale"
		expiresAt := baseTime.Add(5 * time.Second)
		startedAt := baseTime
		s := chaos.ChaosScenario{
			ScenarioID: scenarioID,
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			StartedAt:  startedAt,
			ExpiresAt:  expiresAt,
			Active:     true,
			Mode:       chaos.ExecutionModeSimulation,
			CreatedBy:  "ops-1",
		}
		repo.scenarios[scenarioID] = s

		ctrl := chaos.NewController(repo)
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(targetID string) bool { return true })

		eventExp := chaos.ChaosEvent{
			ScenarioID: scenarioID,
			EventType:  chaos.EventTypeChaosExpired,
			TargetID:   "RAIL-A",
			FaultType:  string(s.Type),
			ActorID:    "SYSTEM",
			ActorRole:  "SYSTEM",
			OccurredAt: expiresAt,
		}

		var wg sync.WaitGroup
		// Concurrently race:
		// 1. Start a new scenario on the target
		// 2. Reset scenarios for the target
		// 3. Directly call ExpireScenario
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = repo.ExpireScenario(ctx, scenarioID, currentTime, eventExp)
			}()
		}
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = ctrl.Reset(ctx, "RAIL-A", "ops-admin", "OPS_ADMIN")
			}()
		}
		for i := 0; i < 4; i++ {
			idSuffix := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := chaos.StartRequest{
					ScenarioID: fmt.Sprintf("sc-race-new-%d", idSuffix),
					Type:       chaos.ScenarioTypeLatency,
					TargetID:   "RAIL-A",
					Parameters: chaos.ScenarioParameters{DurationMs: 4000, LatencyMs: 100},
				}
				_, _ = ctrl.Start(ctx, req, "ops-admin", "OPS_ADMIN")
			}()
		}
		wg.Wait()

		// Verify S1 has EXACTLY ONE terminal event
		s1TerminalEvents := 0
		for _, e := range repo.events {
			if e.ScenarioID == scenarioID && (e.EventType == chaos.EventTypeChaosExpired || e.EventType == chaos.EventTypeChaosReset) {
				s1TerminalEvents++
			}
		}
		if s1TerminalEvents != 1 {
			t.Fatalf("expected exactly 1 terminal event for stale scenario, got %d", s1TerminalEvents)
		}

		// Verify S1 is inactive in repository
		if repo.scenarios[scenarioID].Active {
			t.Fatalf("stale scenario must be inactive in repository")
		}

		// Verify at most 1 active scenario exists on RAIL-A
		activeCount := 0
		for _, sc := range repo.scenarios {
			if sc.TargetID == "RAIL-A" && sc.Active {
				activeCount++
			}
		}
		if activeCount > 1 {
			t.Fatalf("expected at most 1 active scenario on target, got %d", activeCount)
		}
	})

	// G. Hydration stale-expiry case:
	//    Review Hydrate() for expired rows.
	//    Do not leave expired rows permanently as active=true in PostgreSQL merely because hydration filters them out.
	//    Use the existing transactional expiry mechanism so expired durable rows are reconciled safely.
	t.Run("G_Hydrate_ReconcilesStaleExpiredRowsSafely", func(t *testing.T) {
		repo := newMockRepo()
		currentTime := baseTime.Add(10 * time.Second)

		// Setup 1 expired scenario and 1 unexpired scenario in repository
		staleID := "sc-stale-in-db"
		repo.scenarios[staleID] = chaos.ChaosScenario{
			ScenarioID: staleID,
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			StartedAt:  baseTime,
			ExpiresAt:  baseTime.Add(5 * time.Second), // expired at t0+5s
			Active:     true,
			Mode:       chaos.ExecutionModeSimulation,
			CreatedBy:  "ops-1",
		}

		unexpiredID := "sc-unexpired-in-db"
		repo.scenarios[unexpiredID] = chaos.ChaosScenario{
			ScenarioID: unexpiredID,
			Type:       chaos.ScenarioTypeLatency,
			TargetID:   "RAIL-B",
			StartedAt:  baseTime,
			ExpiresAt:  baseTime.Add(20 * time.Second), // unexpired (currentTime is t0+10s)
			Active:     true,
			Mode:       chaos.ExecutionModeSimulation,
			CreatedBy:  "ops-1",
		}

		ctrl := chaos.NewController(repo)
		ctrl.SetNowFunc(func() time.Time { return currentTime })
		ctrl.SetTargetValidator(func(targetID string) bool { return true })

		if err := ctrl.Hydrate(ctx); err != nil {
			t.Fatalf("Hydrate failed: %v", err)
		}

		// Stale scenario in repo must be reconciled to inactive
		staleInRepo := repo.scenarios[staleID]
		if staleInRepo.Active {
			t.Fatalf("stale scenario must be deactivated during hydration")
		}
		if staleInRepo.StoppedBy == nil || *staleInRepo.StoppedBy != "SYSTEM_AUTO_EXPIRY" {
			t.Fatalf("expected stoppedBy SYSTEM_AUTO_EXPIRY, got %v", staleInRepo.StoppedBy)
		}
		if staleInRepo.StoppedAt == nil || !staleInRepo.StoppedAt.Equal(staleInRepo.ExpiresAt) {
			t.Fatalf("expected stoppedAt == expiresAt, got %v", staleInRepo.StoppedAt)
		}

		// Stale scenario must have exactly one CHAOS_EXPIRED event
		staleExpiredEvents := 0
		for _, e := range repo.events {
			if e.ScenarioID == staleID && e.EventType == chaos.EventTypeChaosExpired {
				staleExpiredEvents++
			}
		}
		if staleExpiredEvents != 1 {
			t.Fatalf("expected exactly 1 CHAOS_EXPIRED event for stale row, got %d", staleExpiredEvents)
		}

		// Unexpired scenario must be active in memory
		faultB, activeB, err := ctrl.GetActiveFault(ctx, "RAIL-B")
		if err != nil || !activeB || faultB == nil || faultB.ScenarioID != unexpiredID {
			t.Fatalf("unexpired scenario must be active in memory: err=%v, active=%v, fault=%+v", err, activeB, faultB)
		}

		// Stale scenario must NOT be active in memory
		faultA, activeA, err := ctrl.GetActiveFault(ctx, "RAIL-A")
		if err != nil || activeA || faultA != nil {
			t.Fatalf("stale scenario must not be active in memory: err=%v, active=%v", err, activeA)
		}
	})
}
