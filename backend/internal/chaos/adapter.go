package chaos

import (
	"context"
	"time"

	"github.com/transactx/backend/internal/bank"
)

// ChaosAdapter wraps a bank.BankAdapter and intercepts operations to inject
// target-scoped operational faults based on active ChaosScenarios in the Controller.
// If no fault is active for the adapter's targetID, operations pass directly to the
// underlying adapter unchanged.
type ChaosAdapter struct {
	targetID   string
	underlying bank.BankAdapter
	controller *Controller
}

var _ bank.BankAdapter = (*ChaosAdapter)(nil)

func NewChaosAdapter(targetID string, underlying bank.BankAdapter, controller *Controller) *ChaosAdapter {
	return &ChaosAdapter{
		targetID:   targetID,
		underlying: underlying,
		controller: controller,
	}
}

func (a *ChaosAdapter) TargetID() string {
	return a.targetID
}

func (a *ChaosAdapter) Underlying() bank.BankAdapter {
	return a.underlying
}

// injectFault evaluates if an active fault applies for this target, handles latency delays,
// and returns a simulated error if the scenario mandates a fault.
func (a *ChaosAdapter) injectFault(ctx context.Context, isHealthCheck bool) error {
	if a.controller == nil {
		return nil
	}

	scenario, active := a.controller.GetActiveFault(a.targetID)
	if !active || scenario == nil {
		return nil
	}

	switch scenario.Type {
	case ScenarioTypeBankOutage:
		msg := "chaos simulation: bank outage"
		if scenario.Parameters.ErrorMessage != "" {
			msg = scenario.Parameters.ErrorMessage
		}
		return &bank.AdapterError{
			Code:    bank.ErrCodeBankUnavailable,
			Message: msg,
			Err:     ErrBankOutage,
		}

	case ScenarioTypeLatency:
		if scenario.Parameters.LatencyMs > 0 {
			delay := time.Duration(scenario.Parameters.LatencyMs) * time.Millisecond
			if err := a.controller.sleep(ctx, delay); err != nil {
				return err
			}
		}
		return nil

	case ScenarioTypeTransientDrop, ScenarioTypeTransient, ScenarioTypeMessageDrop:
		// Pre-call transient / request-drop simulation:
		// Intercepts the request prior to bank execution and returns a definite transport-level
		// transient failure (ErrCodeTransientFailure).
		// This simulates network/connection drops before reaching the bank endpoint.
		// It is NOT an unknown outcome (OperationPending); unknown outcomes remain governed
		// by M1/M2 saga recovery where GetOperationStatus is used before any safe retry.
		shouldDrop := a.controller.CheckAndRecordInvocation(scenario.ScenarioID, scenario.Parameters)
		if shouldDrop {
			msg := "chaos simulation: transient drop"
			if scenario.Parameters.ErrorMessage != "" {
				msg = scenario.Parameters.ErrorMessage
			}
			return &bank.AdapterError{
				Code:    bank.ErrCodeTransientFailure,
				Message: msg,
				Err:     ErrTransientDrop,
			}
		}
		return nil

	case ScenarioTypePartition:
		msg := "chaos simulation: network partition"
		if scenario.Parameters.ErrorMessage != "" {
			msg = scenario.Parameters.ErrorMessage
		}
		return &bank.AdapterError{
			Code:    bank.ErrCodeBankUnavailable,
			Message: msg,
			Err:     ErrNetworkPartition,
		}
	}

	return nil
}

func (a *ChaosAdapter) GetHealth(ctx context.Context) (bank.HealthResult, error) {
	if err := a.injectFault(ctx, true); err != nil {
		return bank.HealthResult{Available: false}, err
	}
	return a.underlying.GetHealth(ctx)
}

func (a *ChaosAdapter) ResolveAccount(ctx context.Context, req bank.ResolveAccountRequest) (bank.AccountResult, error) {
	if err := a.injectFault(ctx, false); err != nil {
		return bank.AccountResult{Status: bank.AccountInactive}, err
	}
	return a.underlying.ResolveAccount(ctx, req)
}

func (a *ChaosAdapter) HoldFunds(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	if err := a.injectFault(ctx, false); err != nil {
		return bank.HoldResult{
			OperationResult: bank.OperationResult{
				PaymentID:   req.PaymentID,
				OperationID: req.OperationID,
				Status:      bank.OperationFailed,
			},
		}, err
	}
	return a.underlying.HoldFunds(ctx, req)
}

func (a *ChaosAdapter) ProvisionalCredit(ctx context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	if err := a.injectFault(ctx, false); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}
	return a.underlying.ProvisionalCredit(ctx, req)
}

func (a *ChaosAdapter) ConfirmHold(ctx context.Context, req bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	if err := a.injectFault(ctx, false); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}
	return a.underlying.ConfirmHold(ctx, req)
}

func (a *ChaosAdapter) ReleaseHold(ctx context.Context, req bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	if err := a.injectFault(ctx, false); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}
	return a.underlying.ReleaseHold(ctx, req)
}

func (a *ChaosAdapter) ReverseProvisionalCredit(ctx context.Context, req bank.ReverseCreditRequest) (bank.OperationResult, error) {
	if err := a.injectFault(ctx, false); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}
	return a.underlying.ReverseProvisionalCredit(ctx, req)
}

func (a *ChaosAdapter) GetOperationStatus(ctx context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	if err := a.injectFault(ctx, false); err != nil {
		return bank.OperationResult{
			PaymentID:   req.PaymentID,
			OperationID: req.OperationID,
			Status:      bank.OperationFailed,
		}, err
	}
	return a.underlying.GetOperationStatus(ctx, req)
}

func (a *ChaosAdapter) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	if err := a.injectFault(ctx, false); err != nil {
		return bank.LedgerSnapshot{}, err
	}
	return a.underlying.GetLedgerSnapshot(ctx, scope)
}
