package reconciliation

import (
	"context"
	"slices"
	"testing"
	"time"
)

type recordingIntegrityRunner struct {
	requests chan IntegrityRunRequest
}

func (runner recordingIntegrityRunner) Run(_ context.Context, request IntegrityRunRequest) (IntegrityRunResult, error) {
	runner.requests <- request
	return IntegrityRunResult{}, nil
}

func TestIntegrityTriggerCoordinatorDebouncesFastChecks(t *testing.T) {
	runner := recordingIntegrityRunner{requests: make(chan IntegrityRunRequest, 2)}
	coordinator := NewIntegrityTriggerCoordinator(runner, 10*time.Millisecond)
	if !coordinator.Trigger(IntegrityEventCentralSettlement) || !coordinator.Trigger(IntegrityEventRoutedRecovery) || coordinator.Trigger("UNKNOWN") {
		t.Fatal("lifecycle event policy mismatch")
	}
	select {
	case request := <-runner.requests:
		if slices.Contains(request.CheckCodes, CheckMerkleCommitmentConsistency) {
			t.Fatal("automatic scan included full Merkle consistency")
		}
		if len(request.CheckCodes) != len(AutomaticIntegrityCheckCodes()) {
			t.Fatalf("automatic checks = %v", request.CheckCodes)
		}
	case <-time.After(time.Second):
		t.Fatal("automatic integrity scan did not run")
	}
	select {
	case <-runner.requests:
		t.Fatal("debounced events produced more than one scan")
	case <-time.After(40 * time.Millisecond):
	}
}
