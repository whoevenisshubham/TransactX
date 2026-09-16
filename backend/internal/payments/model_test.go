package payments

import "testing"

func TestPaymentStateTransitions(t *testing.T) {
	tests := []struct {
		from string
		to   string
		want bool
	}{
		{StateCreated, StateValidating, true},
		{StateValidating, StateLocalSettlement, true},
		{StateLocalSettlement, StateCommitted, true},
		{StateValidating, StateRouting, true},
		{StateRouting, StateProcessing, true},
		{StateProcessing, StateCommitted, true},
		{StateCommitted, StateCompleted, true},
		{StateProcessing, StatePendingReconciliation, true},
		{StatePendingReconciliation, StateReversed, true},
		{StateCreated, StateCompleted, false},
		{StateCompleted, StateProcessing, false},
		{StateFailed, StateCompleted, false},
	}
	for _, test := range tests {
		if got := CanTransition(test.from, test.to); got != test.want {
			t.Errorf("CanTransition(%q, %q) = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestTransitionRejectsInvalidState(t *testing.T) {
	payment := Payment{State: StateCreated}
	if err := Transition(&payment, StateCompleted); err != ErrInvalidTransition {
		t.Fatalf("Transition returned %v, want ErrInvalidTransition", err)
	}
	if payment.State != StateCreated {
		t.Fatalf("state changed after rejected transition: %q", payment.State)
	}
}

func TestTransitionAcceptsValidState(t *testing.T) {
	payment := Payment{State: StateCreated}
	if err := Transition(&payment, StateValidating); err != nil {
		t.Fatal(err)
	}
	if payment.State != StateValidating {
		t.Fatalf("state = %q, want %q", payment.State, StateValidating)
	}
}
