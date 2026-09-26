package bank_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
)

func TestHTTPClientPreservesOperationIdentity(t *testing.T) {
	operationID := uuid.New()
	paymentID := uuid.New()
	var received bank.HoldFundsRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/holds" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"paymentID":"` + paymentID.String() + `","operationID":"` + operationID.String() + `","status":"SUCCEEDED","holdID":"` + operationID.String() + `"}`))
	}))
	defer server.Close()

	client, err := bank.NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.HoldFunds(context.Background(), bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{
		PaymentID: paymentID, OperationID: operationID, IdempotencyKey: "hold-1", AccountID: uuid.New(), AmountPaise: 100, Currency: "INR",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if received.OperationID != operationID || received.PaymentID != paymentID || result.OperationID != operationID || result.HoldID != operationID {
		t.Fatalf("identity was not preserved: received=%+v result=%+v", received, result)
	}
}

func TestHTTPClientMapsUnavailableBankError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{"error":"bank unavailable"}`))
	}))
	defer server.Close()
	client, err := bank.NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetHealth(context.Background())
	var adapterErr *bank.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Code != bank.ErrCodeBankUnavailable || !strings.Contains(adapterErr.Message, "bank unavailable") {
		t.Fatalf("error = %v, want typed unavailable error", err)
	}
}

func TestHTTPClientPropagatesRequestID(t *testing.T) {
	var capturedRequestID string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		capturedRequestID = request.Header.Get("X-Request-ID")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client, err := bank.NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := bank.ContextWithRequestIDForTest(context.Background(), "req-test-12345")
	_, err = client.GetHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if capturedRequestID != "req-test-12345" {
		t.Fatalf("capturedRequestID = %q, want %q", capturedRequestID, "req-test-12345")
	}
}
