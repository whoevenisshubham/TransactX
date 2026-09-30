package reconciliation

import (
	"context"
	"sync"
	"time"
)

type IntegrityLifecycleEvent string

const (
	IntegrityEventCentralSettlement IntegrityLifecycleEvent = "CENTRAL_SETTLEMENT_COMPLETED"
	IntegrityEventRoutedRecovery    IntegrityLifecycleEvent = "ROUTED_RECOVERY_COMPLETED"
	IntegrityEventReconciliation    IntegrityLifecycleEvent = "RECONCILIATION_COMPLETED"
	IntegrityEventChaosRecovery     IntegrityLifecycleEvent = "CHAOS_RECOVERY_COMPLETED"
)

var automaticIntegrityEvents = map[IntegrityLifecycleEvent]bool{
	IntegrityEventCentralSettlement: true,
	IntegrityEventRoutedRecovery:    true,
	IntegrityEventReconciliation:    true,
	IntegrityEventChaosRecovery:     true,
}

// AutomaticIntegrityCheckCodes returns the bounded checks suitable for
// debounced lifecycle scans. Full Merkle verification remains operator-run.
func AutomaticIntegrityCheckCodes() []string {
	return []string{
		CheckDebitCreditConservation,
		CheckNonNegativeBalances,
		CheckTransactionUniqueness,
		CheckIdempotencyMapping,
		CheckPaymentStateValidity,
		CheckCompletedPaymentLedgerCompleteness,
	}
}

type integrityRunExecutor interface {
	Run(context.Context, IntegrityRunRequest) (IntegrityRunResult, error)
}

// IntegrityTriggerCoordinator coalesces high-value lifecycle events and runs
// read-only fast checks in the background after a short debounce interval.
type IntegrityTriggerCoordinator struct {
	mu      sync.Mutex
	runner  integrityRunExecutor
	delay   time.Duration
	timeout time.Duration
	timer   *time.Timer
	pending map[IntegrityLifecycleEvent]bool
	onError func(error)
}

func NewIntegrityTriggerCoordinator(runner integrityRunExecutor, delay time.Duration) *IntegrityTriggerCoordinator {
	if delay <= 0 {
		delay = 2 * time.Second
	}
	return &IntegrityTriggerCoordinator{runner: runner, delay: delay, timeout: 30 * time.Second, pending: make(map[IntegrityLifecycleEvent]bool)}
}

func (coordinator *IntegrityTriggerCoordinator) SetErrorObserver(observer func(error)) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.onError = observer
}

func (coordinator *IntegrityTriggerCoordinator) Trigger(event IntegrityLifecycleEvent) bool {
	if coordinator == nil || coordinator.runner == nil || !automaticIntegrityEvents[event] {
		return false
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.pending[event] = true
	if coordinator.timer == nil {
		coordinator.timer = time.AfterFunc(coordinator.delay, coordinator.run)
	} else {
		coordinator.timer.Reset(coordinator.delay)
	}
	return true
}

func (coordinator *IntegrityTriggerCoordinator) run() {
	coordinator.mu.Lock()
	if len(coordinator.pending) == 0 {
		coordinator.timer = nil
		coordinator.mu.Unlock()
		return
	}
	coordinator.pending = make(map[IntegrityLifecycleEvent]bool)
	coordinator.timer = nil
	observer := coordinator.onError
	coordinator.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), coordinator.timeout)
	defer cancel()
	_, err := coordinator.runner.Run(ctx, IntegrityRunRequest{CheckCodes: AutomaticIntegrityCheckCodes()})
	if err != nil && observer != nil {
		observer(err)
	}
}
