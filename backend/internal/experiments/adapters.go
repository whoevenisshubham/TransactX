package experiments

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
)

// MockBankAdapter implements bank.BankAdapter with controllable latency and failure injection for benchmarks.
type MockBankAdapter struct {
	mu           sync.RWMutex
	code         string
	latency      time.Duration
	available    bool
	errorHandler func(ctx context.Context, opType string) error
	operations   map[uuid.UUID]bank.OperationStatus
}

var _ bank.BankAdapter = (*MockBankAdapter)(nil)

// NewMockBankAdapter creates a mock bank adapter with the given bank code.
func NewMockBankAdapter(code string) *MockBankAdapter {
	return &MockBankAdapter{
		code:       code,
		available:  true,
		operations: make(map[uuid.UUID]bank.OperationStatus),
	}
}

func (m *MockBankAdapter) SetLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latency = d
}

func (m *MockBankAdapter) SetAvailable(avail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.available = avail
}

func (m *MockBankAdapter) SetErrorHandler(fn func(ctx context.Context, opType string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorHandler = fn
}

func (m *MockBankAdapter) simulate(ctx context.Context, opType string) error {
	m.mu.RLock()
	lat := m.latency
	avail := m.available
	errFn := m.errorHandler
	m.mu.RUnlock()

	if lat > 0 {
		select {
		case <-time.After(lat):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if !avail {
		return &bank.AdapterError{
			Code:    bank.ErrCodeBankUnavailable,
			Message: fmt.Sprintf("mock bank %s is unavailable", m.code),
		}
	}

	if errFn != nil {
		if err := errFn(ctx, opType); err != nil {
			return err
		}
	}

	return nil
}

func (m *MockBankAdapter) GetHealth(ctx context.Context) (bank.HealthResult, error) {
	if err := m.simulate(ctx, "HEALTH"); err != nil {
		return bank.HealthResult{Available: false}, err
	}
	m.mu.RLock()
	avail := m.available
	m.mu.RUnlock()
	return bank.HealthResult{Available: avail}, nil
}

func (m *MockBankAdapter) ResolveAccount(ctx context.Context, req bank.ResolveAccountRequest) (bank.AccountResult, error) {
	if err := m.simulate(ctx, "RESOLVE_ACCOUNT"); err != nil {
		return bank.AccountResult{AccountID: req.AccountID, Status: bank.AccountInactive}, err
	}
	return bank.AccountResult{AccountID: req.AccountID, Status: bank.AccountActive}, nil
}

func (m *MockBankAdapter) HoldFunds(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	if err := m.simulate(ctx, "HOLD_FUNDS"); err != nil {
		return bank.HoldResult{
			OperationResult: bank.OperationResult{
				PaymentID:   req.PaymentID,
				OperationID: req.OperationID,
				Status:      bank.OperationFailed,
			},
		}, err
	}

	m.mu.Lock()
	m.operations[req.OperationID] = bank.OperationSucceeded
	m.mu.Unlock()

	return bank.HoldResult{
		OperationResult: bank.OperationResult{
			PaymentID:     req.PaymentID,
			OperationID:   req.OperationID,
			BankReference: fmt.Sprintf("REF-%s-%s", m.code, req.OperationID),
			Status:        bank.OperationSucceeded,
		},
		HoldID: req.OperationID,
	}, nil
}

func (m *MockBankAdapter) ProvisionalCredit(ctx context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	if err := m.simulate(ctx, "PROVISIONAL_CREDIT"); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}

	m.mu.Lock()
	m.operations[req.OperationID] = bank.OperationSucceeded
	m.mu.Unlock()

	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: fmt.Sprintf("REF-%s-%s", m.code, req.OperationID),
		Status:        bank.OperationSucceeded,
	}, nil
}

func (m *MockBankAdapter) ConfirmHold(ctx context.Context, req bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	if err := m.simulate(ctx, "CONFIRM_HOLD"); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}

	m.mu.Lock()
	m.operations[req.OperationID] = bank.OperationSucceeded
	m.mu.Unlock()

	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: fmt.Sprintf("REF-%s-%s", m.code, req.OperationID),
		Status:        bank.OperationSucceeded,
	}, nil
}

func (m *MockBankAdapter) ReleaseHold(ctx context.Context, req bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	if err := m.simulate(ctx, "RELEASE_HOLD"); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}

	m.mu.Lock()
	m.operations[req.OperationID] = bank.OperationSucceeded
	m.mu.Unlock()

	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: fmt.Sprintf("REF-%s-%s", m.code, req.OperationID),
		Status:        bank.OperationSucceeded,
	}, nil
}

func (m *MockBankAdapter) ReverseProvisionalCredit(ctx context.Context, req bank.ReverseCreditRequest) (bank.OperationResult, error) {
	if err := m.simulate(ctx, "REVERSE_CREDIT"); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}

	m.mu.Lock()
	m.operations[req.OperationID] = bank.OperationSucceeded
	m.mu.Unlock()

	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: fmt.Sprintf("REF-%s-%s", m.code, req.OperationID),
		Status:        bank.OperationSucceeded,
	}, nil
}

func (m *MockBankAdapter) GetOperationStatus(ctx context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	if err := m.simulate(ctx, "OPERATION_STATUS"); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationPending,
		}, err
	}

	m.mu.RLock()
	status, ok := m.operations[req.OperationID]
	m.mu.RUnlock()

	if !ok {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationPending,
		}, nil
	}

	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: fmt.Sprintf("REF-%s-%s", m.code, req.OperationID),
		Status:        status,
	}, nil
}

func (m *MockBankAdapter) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{
		BankID:     m.code,
		SnapshotID: uuid.New(),
		CapturedAt: time.Now(),
	}, nil
}
