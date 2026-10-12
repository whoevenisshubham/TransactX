package payments

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ResumeStatus represents the operational outcome of a recovery continuation.
type ResumeStatus string

const (
	ResumeStatusNoOp       ResumeStatus = "NO_OP"       // Terminal state (COMPLETED, FAILED, REVERSED)
	ResumeStatusCompleted  ResumeStatus = "COMPLETED"   // Reached terminal COMPLETED during this continuation
	ResumeStatusInProgress ResumeStatus = "IN_PROGRESS" // Continued safely, still in non-terminal state (e.g. PROCESSING, PENDING_RECONCILIATION)
	ResumeStatusFailed     ResumeStatus = "FAILED"      // Reached terminal FAILED or encountered unrecoverable failure
	ResumeStatusAttention  ResumeStatus = "ATTENTION"   // Parked / requires operator attention (e.g. route missing, target unconfigured, unsupported state)
)

// ResumeResult represents the result of invoking RecoveryResumer.Resume.
type ResumeResult struct {
	Payment          Payment       `json:"payment"`
	Status           ResumeStatus  `json:"status"`
	Reason           string        `json:"reason,omitempty"`
	Terminal         bool          `json:"terminal"`
	ActionTaken      string        `json:"actionTaken,omitempty"`
	LockWaitDuration time.Duration `json:"lockWaitDuration,omitempty"`
}

// RecoveryResumer is the single shared entry point for payment state continuation.
// Both duplicate request handlers and background recovery workers invoke Resume.
type RecoveryResumer interface {
	Resume(ctx context.Context, paymentID uuid.UUID) (ResumeResult, error)
}

var _ RecoveryResumer = (*Service)(nil)

// RecoveryMetrics records performance and operational counters for recovery.
type RecoveryMetrics struct {
	LockWaitTotalNs atomic.Int64
	LockWaitCount   atomic.Int64
	ResumeCount     atomic.Int64
	ResumeNoOp      atomic.Int64
	ResumeCompleted atomic.Int64
	ResumeFailed    atomic.Int64
	ResumeAttention atomic.Int64
	OverloadCount   atomic.Int64
}

// Snapshot returns a copy of current metrics counters.
func (m *RecoveryMetrics) Snapshot() map[string]int64 {
	if m == nil {
		return nil
	}
	return map[string]int64{
		"lock_wait_total_ns": m.LockWaitTotalNs.Load(),
		"lock_wait_count":    m.LockWaitCount.Load(),
		"resume_count":       m.ResumeCount.Load(),
		"resume_noop":        m.ResumeNoOp.Load(),
		"resume_completed":   m.ResumeCompleted.Load(),
		"resume_failed":      m.ResumeFailed.Load(),
		"resume_attention":   m.ResumeAttention.Load(),
		"overload_count":     m.OverloadCount.Load(),
	}
}

func (service *Service) ensureConcurrencyInit() {
	service.concurrencyOnce.Do(func() {
		if service.concurrencyLimit <= 0 {
			service.concurrencyLimit = 2
		}
		if service.concurrencySem == nil {
			service.concurrencySem = make(chan struct{}, service.concurrencyLimit)
		}
		if service.recoveryMetrics == nil {
			service.recoveryMetrics = &RecoveryMetrics{}
		}
	})
}

// SetPaymentConcurrency configures the maximum concurrent payment continuation executions.
func (service *Service) SetPaymentConcurrency(limit int) {
	if limit <= 0 {
		limit = 1
	}
	service.concurrencyLimit = limit
	service.concurrencySem = make(chan struct{}, limit)
	if service.recoveryMetrics == nil {
		service.recoveryMetrics = &RecoveryMetrics{}
	}
}

// PaymentConcurrency returns the configured concurrency limit.
func (service *Service) PaymentConcurrency() int {
	service.ensureConcurrencyInit()
	return service.concurrencyLimit
}

// RecoveryMetrics returns the operational recovery metrics tracker.
func (service *Service) RecoveryMetrics() *RecoveryMetrics {
	service.ensureConcurrencyInit()
	return service.recoveryMetrics
}

func (service *Service) acquireConcurrency(ctx context.Context) error {
	service.ensureConcurrencyInit()
	select {
	case service.concurrencySem <- struct{}{}:
		return nil
	case <-ctx.Done():
		if service.recoveryMetrics != nil {
			service.recoveryMetrics.OverloadCount.Add(1)
		}
		return ctx.Err()
	}
}

func (service *Service) releaseConcurrency() {
	if service.concurrencySem != nil {
		select {
		case <-service.concurrencySem:
		default:
		}
	}
}

// Resume is the single, canonical entrypoint for serializing and continuing payment execution.
// Initial execution duplicate handling, caller retries, and background worker recovery all share this method.
//
// Invariants enforced:
// 1. Concurrency limit and connection pool reserve (bounded semaphore).
// 2. Dedicated session advisory lock per payment UUID with guaranteed connection cleanup.
// 3. Re-reads payment state after acquiring lock.
// 4. Resolves original persisted route before executing monetary operations.
// 5. Handles terminal states as no-ops.
// 6. Validates central ledger for COMMITTED state without adding duplicate transactions.
// 7. Fails closed / leaves pending if route or execution target is missing.
func (service *Service) Resume(ctx context.Context, paymentID uuid.UUID) (ResumeResult, error) {
	if service.payments == nil || (service.payments.db == nil && service.payments.getFn == nil && service.payments.recoverRoutedFn == nil) {
		return ResumeResult{}, errors.New("payment repository database is not initialized")
	}

	// 1. Acquire bounded concurrency semaphore
	if err := service.acquireConcurrency(ctx); err != nil {
		return ResumeResult{}, fmt.Errorf("concurrency limit / context timeout: %w", err)
	}
	defer service.releaseConcurrency()

	if service.recoveryMetrics != nil {
		service.recoveryMetrics.ResumeCount.Add(1)
	}

	// 2. Acquire per-payment session advisory lock on dedicated connection
	startLock := time.Now()
	unlock, err := service.payments.lockPayment(ctx, paymentID)
	if err != nil {
		if service.recoveryMetrics != nil {
			service.recoveryMetrics.ResumeFailed.Add(1)
		}
		return ResumeResult{}, fmt.Errorf("failed to acquire payment lock: %w", err)
	}
	lockWaitDuration := time.Since(startLock)
	if service.recoveryMetrics != nil {
		service.recoveryMetrics.LockWaitTotalNs.Add(lockWaitDuration.Nanoseconds())
		service.recoveryMetrics.LockWaitCount.Add(1)
	}
	defer unlock()

	// 3. Re-read payment state under lock
	current, err := service.payments.Get(ctx, paymentID)
	if err != nil {
		if service.recoveryMetrics != nil {
			service.recoveryMetrics.ResumeFailed.Add(1)
		}
		return ResumeResult{LockWaitDuration: lockWaitDuration}, fmt.Errorf("failed to re-read payment state under lock: %w", err)
	}

	// 4. Dispatch continuation based on current state
	res, err := service.dispatchContinuation(ctx, current, lockWaitDuration)
	if service.recoveryMetrics != nil {
		switch res.Status {
		case ResumeStatusNoOp:
			service.recoveryMetrics.ResumeNoOp.Add(1)
		case ResumeStatusCompleted:
			service.recoveryMetrics.ResumeCompleted.Add(1)
		case ResumeStatusAttention:
			service.recoveryMetrics.ResumeAttention.Add(1)
		case ResumeStatusFailed:
			service.recoveryMetrics.ResumeFailed.Add(1)
		}
	}
	return res, err
}

func (service *Service) dispatchContinuation(ctx context.Context, current Payment, lockWaitDuration time.Duration) (ResumeResult, error) {
	// Terminal states
	if current.State == StateCompleted || current.State == StateFailed || current.State == StateReversed {
		return ResumeResult{
			Payment:          current,
			Status:           ResumeStatusNoOp,
			Reason:           fmt.Sprintf("payment is already in terminal state %s", current.State),
			Terminal:         true,
			ActionTaken:      "TERMINAL_NOOP",
			LockWaitDuration: lockWaitDuration,
		}, nil
	}

	// Unsupported local settlement state in routed recovery
	if current.State == StateLocalSettlement {
		return ResumeResult{
			Payment:          current,
			Status:           ResumeStatusAttention,
			Reason:           "local settlement state cannot be resumed via routed recovery",
			Terminal:         false,
			ActionTaken:      "UNSUPPORTED_LOCAL_STATE",
			LockWaitDuration: lockWaitDuration,
		}, nil
	}

	// Central-only completion states
	switch current.State {
	case StateCommitted:
		completed, err := service.payments.CompleteCommittedPayment(ctx, current.ID)
		if err != nil {
			return ResumeResult{
				Payment:          current,
				Status:           ResumeStatusFailed,
				Reason:           err.Error(),
				Terminal:         false,
				ActionTaken:      "COMPLETE_COMMITTED",
				LockWaitDuration: lockWaitDuration,
			}, err
		}
		return ResumeResult{
			Payment:          completed,
			Status:           ResumeStatusCompleted,
			Reason:           "committed payment central ledger verified and transition completed",
			Terminal:         true,
			ActionTaken:      "COMPLETE_COMMITTED",
			LockWaitDuration: lockWaitDuration,
		}, nil

	case StateBankSettledCentralPending:
		completed, err := service.payments.RecoverBankSettledCentralPending(ctx, current.ID)
		if err != nil {
			return ResumeResult{
				Payment:          current,
				Status:           ResumeStatusFailed,
				Reason:           err.Error(),
				Terminal:         false,
				ActionTaken:      "BANK_SETTLED_CENTRAL_PENDING",
				LockWaitDuration: lockWaitDuration,
			}, err
		}
		status := ResumeStatusInProgress
		terminal := false
		if completed.State == StateCompleted {
			status = ResumeStatusCompleted
			terminal = true
		}
		return ResumeResult{
			Payment:          completed,
			Status:           status,
			Terminal:         terminal,
			ActionTaken:      "BANK_SETTLED_CENTRAL_PENDING",
			LockWaitDuration: lockWaitDuration,
		}, nil
	}

	// States requiring bank adapters: CREATED, VALIDATING, ROUTING, PROCESSING, PENDING_RECONCILIATION.
	// Incomplete route identity
	if current.SourceBankID == nil || current.DestinationBankID == nil {
		return ResumeResult{
			Payment:          current,
			Status:           ResumeStatusAttention,
			Reason:           "incomplete route identity: missing bank participants",
			Terminal:         false,
			ActionTaken:      "INCOMPLETE_ROUTE_IDENTITY",
			LockWaitDuration: lockWaitDuration,
		}, nil
	}

	// States requiring bank adapters: CREATED, VALIDATING, ROUTING, PROCESSING, PENDING_RECONCILIATION.
	// Resolve original persisted route before executing monetary operations!
	sourceAdapter, destinationAdapter, ok, err := service.RoutedAdaptersForPayment(ctx, current)
	if err != nil {
		return ResumeResult{
			Payment:          current,
			Status:           ResumeStatusFailed,
			Reason:           fmt.Sprintf("failed resolving route adapters: %v", err),
			Terminal:         false,
			ActionTaken:      "ROUTE_RESOLUTION_ERROR",
			LockWaitDuration: lockWaitDuration,
		}, err
	}
	if !ok || sourceAdapter == nil || destinationAdapter == nil {
		// Route removed or execution target no longer configured; fail closed / leave pending.
		return ResumeResult{
			Payment:          current,
			Status:           ResumeStatusAttention,
			Reason:           "persisted route or execution target is missing or no longer configured",
			Terminal:         false,
			ActionTaken:      "ROUTE_UNAVAILABLE_LEAVE_PENDING",
			LockWaitDuration: lockWaitDuration,
		}, nil
	}

	var sagaErr error
	switch current.State {
	case StateCreated, StateValidating, StateRouting:
		sagaErr = service.payments.runRoutedSaga(ctx, current, *current.SourceBankID, *current.DestinationBankID, sourceAdapter, destinationAdapter)
	case StateProcessing, StatePendingReconciliation:
		sagaErr = service.payments.RecoverRoutedPayment(ctx, current, sourceAdapter, destinationAdapter)
	default:
		return ResumeResult{
			Payment:          current,
			Status:           ResumeStatusAttention,
			Reason:           fmt.Sprintf("unsupported payment state %s for routed recovery", current.State),
			Terminal:         false,
			ActionTaken:      "UNSUPPORTED_STATE",
			LockWaitDuration: lockWaitDuration,
		}, nil
	}

	refreshed, getErr := service.payments.Get(ctx, current.ID)
	if getErr != nil {
		return ResumeResult{
			Payment:          current,
			Status:           ResumeStatusFailed,
			Reason:           getErr.Error(),
			Terminal:         false,
			ActionTaken:      "REFRESH_FAILED",
			LockWaitDuration: lockWaitDuration,
		}, getErr
	}

	status := ResumeStatusInProgress
	terminal := false
	if refreshed.State == StateCompleted {
		status = ResumeStatusCompleted
		terminal = true
	} else if refreshed.State == StateFailed || refreshed.State == StateReversed {
		status = ResumeStatusFailed
		terminal = true
	}

	reason := ""
	if sagaErr != nil {
		reason = sagaErr.Error()
	}
	return ResumeResult{
		Payment:          refreshed,
		Status:           status,
		Terminal:         terminal,
		Reason:           reason,
		ActionTaken:      "SAGA_CONTINUATION",
		LockWaitDuration: lockWaitDuration,
	}, sagaErr
}
