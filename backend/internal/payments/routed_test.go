package payments

import (
	"testing"

	"github.com/google/uuid"
)

func TestRoutedOperationIDsAreStablePerPaymentAndOperation(t *testing.T) {
	paymentID := uuid.New()
	holdID := operationID(paymentID, "hold")
	if holdID == uuid.Nil || holdID != operationID(paymentID, "hold") {
		t.Fatal("hold operation ID was not stable")
	}
	if holdID == operationID(paymentID, "provisional-credit") {
		t.Fatal("different bank operation types shared an operation ID")
	}
}

func TestBankSettledCentralPendingTransitionsCanRecover(t *testing.T) {
	payment := Payment{State: StateProcessing}
	if err := Transition(&payment, StateBankSettledCentralPending); err != nil {
		t.Fatal(err)
	}
	if err := Transition(&payment, StateCommitted); err != nil {
		t.Fatal(err)
	}
	if err := Transition(&payment, StateCompleted); err != nil {
		t.Fatal(err)
	}
}
