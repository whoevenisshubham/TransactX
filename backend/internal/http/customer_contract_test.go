package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/recipients"
)

type contractFixture struct {
	pool          *pgxpool.Pool
	handler       http.Handler
	payerToken    string
	receiverToken string
	strangerToken string
	payerID       uuid.UUID
	receiverID    uuid.UUID
	strangerID    uuid.UUID
	payerAccount  uuid.UUID
	receiverUPI   string
	payerUPI      string
}

func newContractFixture(t *testing.T) contractFixture {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := auth.NewJWTManager(strings.Repeat("c", 32), "contract-test", time.Hour)
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	authService := auth.NewService(pool, manager, "BANK-DEV-001")
	handler := NewHandler(pool, slog.Default(), authService, manager)

	suffix := uuid.New().String()[:8]
	payerPass := "password-payer-1"
	receiverPass := "password-receiver-1"
	strangerPass := "password-stranger-1"
	payerUPI := "payer-" + suffix + "@transactx"
	receiverUPI := "receiver-" + suffix + "@transactx"
	strangerUPI := "stranger-" + suffix + "@transactx"

	registerUser(t, handler, "Payer "+suffix, "91"+suffix, payerUPI, payerPass)
	registerUser(t, handler, "Receiver "+suffix, "92"+suffix, receiverUPI, receiverPass)
	registerUser(t, handler, "Stranger "+suffix, "93"+suffix, strangerUPI, strangerPass)

	payerToken, payerID := loginUser(t, handler, payerUPI, payerPass)
	receiverToken, receiverID := loginUser(t, handler, receiverUPI, receiverPass)
	strangerToken, strangerID := loginUser(t, handler, strangerUPI, strangerPass)

	var payerAccount uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM accounts WHERE user_id = $1`, payerID).Scan(&payerAccount); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE accounts SET balance_paise = 100000, opening_balance_paise = 100000 WHERE user_id = $1`, payerID); err != nil {
		pool.Close()
		t.Fatal(err)
	}

	fixture := contractFixture{
		pool:          pool,
		handler:       handler,
		payerToken:    payerToken,
		receiverToken: receiverToken,
		strangerToken: strangerToken,
		payerID:       payerID,
		receiverID:    receiverID,
		strangerID:    strangerID,
		payerAccount:  payerAccount,
		receiverUPI:   receiverUPI,
		payerUPI:      payerUPI,
	}
	t.Cleanup(func() { fixture.close(t) })
	return fixture
}

func (f contractFixture) close(t *testing.T) {
	t.Helper()
	userIDs := []uuid.UUID{f.payerID, f.receiverID, f.strangerID}
	for _, userID := range userIDs {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM idempotency_records WHERE user_id = $1`, userID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, userID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM ledger_entries WHERE ledger_transaction_id IN (SELECT lt.id FROM ledger_transactions lt JOIN payments p ON p.id = lt.payment_id WHERE p.initiated_by_user_id = $1)`, userID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM ledger_transactions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, userID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM payments WHERE initiated_by_user_id = $1 OR sender_account_id IN (SELECT id FROM accounts WHERE user_id = $1) OR receiver_account_id IN (SELECT id FROM accounts WHERE user_id = $1)`, userID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM accounts WHERE user_id = $1`, userID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	}
	f.pool.Close()
}

func registerUser(t *testing.T, handler http.Handler, name, phone, paymentID, password string) {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"phone":%q,"paymentIdentifier":%q,"password":%q,"role":"CUSTOMER"}`, name, phone, paymentID, password)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("register %s: status = %d body = %s", paymentID, recorder.Code, recorder.Body.String())
	}
}

func loginUser(t *testing.T, handler http.Handler, paymentID, password string) (string, uuid.UUID) {
	t.Helper()
	body := fmt.Sprintf(`{"identifier":%q,"password":%q}`, paymentID, password)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login %s: status = %d body = %s", paymentID, recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Token string `json:"token"`
			User  struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Token == "" {
		t.Fatalf("login %s returned empty token", paymentID)
	}
	userID, err := uuid.Parse(envelope.Data.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	return envelope.Data.Token, userID
}

func decodeDataInto(t *testing.T, envelope map[string]any, target any) {
	t.Helper()
	raw, err := json.Marshal(envelope["data"])
	if err != nil {
		t.Fatalf("marshal response data: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode public response: %v; data=%s", err, raw)
	}
}

func doJSON(t *testing.T, handler http.Handler, method, path, token string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var parsed map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &parsed)
	return recorder.Code, parsed
}

func dataObject(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", envelope["data"])
	}
	return data
}

func assertNoInternalIDs(t *testing.T, payload map[string]any, label string) {
	t.Helper()
	banned := []string{
		"senderAccountId", "receiverAccountId", "initiatedByUserId",
		"sourceBankId", "destinationBankId", "sourceBankAccountId", "destinationBankAccountId",
		"routeBankId", "operationId", "holdId", "bankOperationId", "id",
	}
	// "id" is allowed on payment objects; check nested/internal names only.
	banned = banned[:len(banned)-1]
	raw, _ := json.Marshal(payload)
	encoded := string(raw)
	for _, key := range banned {
		if strings.Contains(encoded, `"`+key+`"`) {
			t.Errorf("%s leaks internal field %q in %s", label, key, encoded)
		}
	}
}

func TestCustomerHTTP_FreshRegistrationLoginAndAccountAccess(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	manager, err := auth.NewJWTManager(strings.Repeat("c", 32), "fresh-login-regression", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	authService := auth.NewService(pool, manager, "BANK-DEV-001")
	handler := NewHandler(pool, slog.Default(), authService, manager)

	suffix := uuid.New().String()[:8]
	name := "Fresh User " + suffix
	phone := "99" + suffix
	identifier := "fresh-" + suffix + "@transactx"
	password := "fresh-password-1"

	registerUser(t, handler, name, phone, identifier, password)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(fmt.Sprintf(`{"identifier":%q,"password":%q}`, identifier, password)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("fresh login status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var loginEnvelope struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&loginEnvelope); err != nil {
		t.Fatal(err)
	}
	if loginEnvelope.Data.Token == "" {
		t.Fatal("fresh login returned empty token")
	}

	status, envelope := doJSON(t, handler, http.MethodGet, "/api/accounts", loginEnvelope.Data.Token, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("fresh account status = %d body=%v", status, envelope)
	}
	list, ok := envelope["data"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("fresh account data = %#v", envelope["data"])
	}
	account, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("fresh account entry = %#v", list[0])
	}
	for _, key := range []string{"accountNumber", "balancePaise", "status"} {
		if _, found := account[key]; !found {
			t.Fatalf("fresh account is missing %q: %#v", key, account)
		}
	}
	if _, found := account["id"]; found {
		t.Fatalf("fresh account exposes internal field id: %#v", account)
	}
	_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE user_id IN (SELECT id FROM users WHERE payment_identifier = $1)`, identifier)
	_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE payment_identifier = $1`, identifier)
}

func TestCustomerHTTP_AccountsOmitInternalIdentifiers(t *testing.T) {
	fx := newContractFixture(t)
	status, envelope := doJSON(t, fx.handler, http.MethodGet, "/api/accounts", fx.payerToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("accounts status = %d, body=%v", status, envelope)
	}
	if _, ok := envelope["requestId"].(string); !ok || envelope["requestId"] == "" {
		t.Fatal("accounts response missing requestId")
	}
	list, ok := envelope["data"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("accounts data = %#v", envelope["data"])
	}
	account, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("account entry = %#v", list[0])
	}
	for _, key := range []string{"accountNumber", "balancePaise", "status"} {
		if _, found := account[key]; !found {
			t.Errorf("account missing %q", key)
		}
	}
	for _, key := range []string{"id", "userId", "bankId", "bankAccountId", "version"} {
		if _, found := account[key]; found {
			t.Errorf("account exposes internal field %q", key)
		}
	}
	if account["balancePaise"].(float64) != 100000 {
		t.Fatalf("balancePaise = %v, want 100000", account["balancePaise"])
	}
	var publicAccount customerAccountResponse
	decodeDataInto(t, map[string]any{"data": account}, &publicAccount)
	if publicAccount.AccountNumber == "" || publicAccount.Status == "" || publicAccount.BalancePaise != 100000 {
		t.Fatalf("decoded public account = %+v", publicAccount)
	}

	status, envelope = doJSON(t, fx.handler, http.MethodGet, "/api/accounts/"+fx.payerAccount.String(), fx.payerToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("account detail status = %d", status)
	}
	var publicDetail customerAccountResponse
	decodeDataInto(t, envelope, &publicDetail)
	if publicDetail.AccountNumber == "" || publicDetail.Status == "" {
		t.Fatalf("decoded account detail = %+v", publicDetail)
	}
	assertNoInternalIDs(t, dataObject(t, envelope), "account detail")
}

func TestCustomerHTTP_AuthenticationAndAccountAuthorization(t *testing.T) {
	fx := newContractFixture(t)

	status, envelope := doJSON(t, fx.handler, http.MethodGet, "/api/accounts", "", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d, want 401", status)
	}
	if errObj, ok := envelope["error"].(map[string]any); !ok || errObj["code"] != "UNAUTHORIZED" {
		t.Fatalf("missing auth error = %#v", envelope["error"])
	}
	if requestID, ok := envelope["requestId"].(string); !ok || requestID == "" {
		t.Fatal("missing auth response lacks requestId")
	}

	status, envelope = doJSON(t, fx.handler, http.MethodGet, "/api/accounts", "not-a-jwt", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("malformed auth status = %d, want 401", status)
	}
	if errObj, ok := envelope["error"].(map[string]any); !ok || errObj["code"] != "UNAUTHORIZED" {
		t.Fatalf("malformed auth error = %#v", envelope["error"])
	}

	status, envelope = doJSON(t, fx.handler, http.MethodGet, "/api/accounts/"+fx.payerAccount.String(), fx.strangerToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("cross-user account status = %d, want 404", status)
	}
	if errObj, ok := envelope["error"].(map[string]any); !ok || errObj["code"] != "ACCOUNT_NOT_FOUND" {
		t.Fatalf("cross-user account error = %#v", envelope["error"])
	}
}

func TestCustomerHTTP_RecipientResolution(t *testing.T) {
	fx := newContractFixture(t)
	status, envelope := doJSON(t, fx.handler, http.MethodGet, "/api/recipients/"+fx.receiverUPI, fx.payerToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("recipient status = %d body=%v", status, envelope)
	}
	data := dataObject(t, envelope)
	if data["paymentIdentifier"] != fx.receiverUPI {
		t.Fatalf("paymentIdentifier = %v", data["paymentIdentifier"])
	}
	if data["name"] == "" {
		t.Fatal("recipient name empty")
	}
	var publicRecipient recipients.Recipient
	decodeDataInto(t, envelope, &publicRecipient)
	if publicRecipient.PaymentID != fx.receiverUPI || publicRecipient.Name == "" || publicRecipient.AccountStatus == "" {
		t.Fatalf("decoded public recipient = %+v", publicRecipient)
	}
	for _, key := range []string{"accountId", "userId", "bankId", "bankAccountId"} {
		if _, found := data[key]; found {
			t.Errorf("recipient exposes internal field %q", key)
		}
	}
}

func TestCustomerHTTP_CreatePaymentContract(t *testing.T) {
	fx := newContractFixture(t)
	payload := map[string]any{
		"recipient":   fx.receiverUPI,
		"amountPaise": 12550,
		"currency":    "INR",
		"note":        "Lunch",
	}
	status, envelope := doJSON(t, fx.handler, http.MethodPost, "/api/payments", fx.payerToken, payload, map[string]string{
		"Idempotency-Key": "contract-create-1",
	})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d body=%v", status, envelope)
	}
	if _, ok := envelope["requestId"].(string); !ok || envelope["requestId"] == "" {
		t.Fatal("create response missing requestId")
	}
	payment := dataObject(t, envelope)
	required := []string{"id", "amountPaise", "currency", "state", "direction", "senderName", "receiverName", "senderPaymentIdentifier", "receiverPaymentIdentifier", "origin", "createdAt", "note"}
	for _, key := range required {
		if _, found := payment[key]; !found {
			t.Errorf("create payment missing %q", key)
		}
	}
	if payment["amountPaise"].(float64) != 12550 {
		t.Fatalf("amountPaise = %v, want 12550", payment["amountPaise"])
	}
	if payment["state"] != payments.StateCompleted {
		t.Fatalf("state = %v, want COMPLETED", payment["state"])
	}
	if payment["direction"] != "SENT" {
		t.Fatalf("direction = %v, want SENT", payment["direction"])
	}
	if payment["note"] != "Lunch" {
		t.Fatalf("note = %v", payment["note"])
	}
	var publicPayment payments.CustomerPayment
	decodeDataInto(t, envelope, &publicPayment)
	if publicPayment.ID == uuid.Nil || publicPayment.AmountPaise != 12550 || publicPayment.Direction != "SENT" || publicPayment.ReceiverPaymentID != fx.receiverUPI {
		t.Fatalf("decoded public payment = %+v", publicPayment)
	}
	assertNoInternalIDs(t, payment, "create payment")

	// Same key + payload replays.
	status, envelope = doJSON(t, fx.handler, http.MethodPost, "/api/payments", fx.payerToken, payload, map[string]string{
		"Idempotency-Key": "contract-create-1",
	})
	if status != http.StatusOK {
		t.Fatalf("replay status = %d body=%v", status, envelope)
	}
	replay := dataObject(t, envelope)
	if replay["id"] != payment["id"] {
		t.Fatalf("replay id = %v, want %v", replay["id"], payment["id"])
	}
	paymentUUID, err := uuid.Parse(payment["id"].(string))
	if err != nil {
		t.Fatalf("payment id = %v: %v", payment["id"], err)
	}
	var paymentCount, ledgerCount int
	if err := fx.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE id = $1`, paymentUUID).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := fx.pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_transactions WHERE payment_id = $1`, paymentUUID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 || ledgerCount != 1 {
		t.Fatalf("same-key replay created duplicate settlement: payments=%d ledger_transactions=%d", paymentCount, ledgerCount)
	}

	// Same key + changed payload conflicts.
	changed := map[string]any{
		"recipient":   fx.receiverUPI,
		"amountPaise": 12551,
		"currency":    "INR",
		"note":        "Lunch",
	}
	status, envelope = doJSON(t, fx.handler, http.MethodPost, "/api/payments", fx.payerToken, changed, map[string]string{
		"Idempotency-Key": "contract-create-1",
	})
	if status != http.StatusConflict {
		t.Fatalf("conflict status = %d body=%v", status, envelope)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict code = %v", errObj["code"])
	}

	// Insufficient funds is a safe typed failure.
	status, envelope = doJSON(t, fx.handler, http.MethodPost, "/api/payments", fx.payerToken, map[string]any{
		"recipient":   fx.receiverUPI,
		"amountPaise": 999999999,
		"currency":    "INR",
	}, map[string]string{"Idempotency-Key": "contract-insufficient"})
	if status != http.StatusConflict {
		t.Fatalf("insufficient status = %d body=%v", status, envelope)
	}
	errObj, _ = envelope["error"].(map[string]any)
	if errObj["code"] != "INSUFFICIENT_FUNDS" {
		t.Fatalf("insufficient code = %v", errObj["code"])
	}
	message, _ := errObj["message"].(string)
	for _, banned := range []string{"pq:", "SQLSTATE", "dial tcp", "127.0.0.1"} {
		if strings.Contains(message, banned) {
			t.Fatalf("insufficient message leaks %q: %q", banned, message)
		}
	}
}

func TestCustomerHTTP_PaymentHistoryAndDetailAuthorization(t *testing.T) {
	fx := newContractFixture(t)
	status, envelope := doJSON(t, fx.handler, http.MethodPost, "/api/payments", fx.payerToken, map[string]any{
		"recipient":   fx.receiverUPI,
		"amountPaise": 500,
		"currency":    "INR",
		"note":        "History",
	}, map[string]string{"Idempotency-Key": "contract-history-1"})
	if status != http.StatusCreated {
		t.Fatalf("create status = %d body=%v", status, envelope)
	}
	paymentID, _ := dataObject(t, envelope)["id"].(string)

	status, envelope = doJSON(t, fx.handler, http.MethodGet, "/api/payments", fx.payerToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("payer list status = %d", status)
	}
	payerList, _ := envelope["data"].([]any)
	foundSent := false
	for _, item := range payerList {
		entry := item.(map[string]any)
		if entry["id"] == paymentID && entry["direction"] == "SENT" {
			foundSent = true
			assertNoInternalIDs(t, entry, "payer history")
		}
	}
	if !foundSent {
		t.Fatal("payer history missing SENT payment")
	}

	status, envelope = doJSON(t, fx.handler, http.MethodGet, "/api/payments", fx.receiverToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("receiver list status = %d", status)
	}
	receiverList, _ := envelope["data"].([]any)
	foundReceived := false
	for _, item := range receiverList {
		entry := item.(map[string]any)
		if entry["id"] == paymentID && entry["direction"] == "RECEIVED" {
			foundReceived = true
		}
	}
	if !foundReceived {
		t.Fatal("receiver history missing RECEIVED payment")
	}

	status, envelope = doJSON(t, fx.handler, http.MethodGet, "/api/payments", fx.strangerToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("stranger list status = %d", status)
	}
	for _, item := range envelope["data"].([]any) {
		if item.(map[string]any)["id"] == paymentID {
			t.Fatal("stranger can see payment in history")
		}
	}

	status, envelope = doJSON(t, fx.handler, http.MethodGet, "/api/payments/"+paymentID, fx.payerToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("sender detail status = %d", status)
	}
	assertNoInternalIDs(t, dataObject(t, envelope), "sender detail")

	status, _ = doJSON(t, fx.handler, http.MethodGet, "/api/payments/"+paymentID, fx.receiverToken, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("receiver detail status = %d", status)
	}

	status, envelope = doJSON(t, fx.handler, http.MethodGet, "/api/payments/"+paymentID, fx.strangerToken, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("stranger detail status = %d, want 404", status)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "PAYMENT_NOT_FOUND" {
		t.Fatalf("stranger detail code = %v", errObj["code"])
	}
}

func TestCustomerHTTP_PendingPaymentReturnsAccepted(t *testing.T) {
	fx := newContractFixture(t)
	pendingAdapter := pendingHoldAdapter{}
	manager, err := auth.NewJWTManager(strings.Repeat("c", 32), "contract-pending", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	authService := auth.NewService(fx.pool, manager, "BANK-DEV-001")
	adapters := map[uuid.UUID]bank.BankAdapter{}
	var bankID uuid.UUID
	if err := fx.pool.QueryRow(context.Background(), `SELECT bank_id FROM accounts WHERE id = $1`, fx.payerAccount).Scan(&bankID); err != nil {
		t.Fatal(err)
	}
	adapters[bankID] = pendingAdapter
	handler := NewHandlerWithBankAdapters(fx.pool, slog.Default(), authService, manager, adapters)

	// Re-issue tokens against the same manager used by the pending handler.
	payerToken, _ := loginUser(t, handler, fx.payerUPI, "password-payer-1")

	status, envelope := doJSON(t, handler, http.MethodPost, "/api/payments", payerToken, map[string]any{
		"recipient":   fx.receiverUPI,
		"amountPaise": 100,
		"currency":    "INR",
	}, map[string]string{"Idempotency-Key": "contract-pending-1"})
	if status != http.StatusAccepted {
		t.Fatalf("pending status = %d body=%v", status, envelope)
	}
	payment := dataObject(t, envelope)
	if payment["state"] != payments.StatePendingReconciliation {
		t.Fatalf("pending state = %v, want %s", payment["state"], payments.StatePendingReconciliation)
	}
	var publicPending payments.CustomerPayment
	decodeDataInto(t, envelope, &publicPending)
	assertNoInternalIDs(t, payment, "pending payment")
}

type pendingHoldAdapter struct{}

func (pendingHoldAdapter) GetHealth(context.Context) (bank.HealthResult, error) {
	return bank.HealthResult{Available: true}, nil
}
func (pendingHoldAdapter) ResolveAccount(_ context.Context, req bank.ResolveAccountRequest) (bank.AccountResult, error) {
	return bank.AccountResult{AccountID: req.AccountID, Status: bank.AccountActive}, nil
}
func (pendingHoldAdapter) HoldFunds(_ context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	return bank.HoldResult{OperationResult: bank.OperationResult{
		PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationPending,
	}, HoldID: req.OperationID}, nil
}
func (pendingHoldAdapter) ProvisionalCredit(_ context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationSucceeded}, nil
}
func (pendingHoldAdapter) ConfirmHold(_ context.Context, req bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationSucceeded}, nil
}
func (pendingHoldAdapter) ReleaseHold(_ context.Context, req bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationSucceeded}, nil
}
func (pendingHoldAdapter) ReverseProvisionalCredit(_ context.Context, req bank.ReverseCreditRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationSucceeded}, nil
}
func (pendingHoldAdapter) GetOperationStatus(_ context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationPending}, nil
}
func (pendingHoldAdapter) GetLedgerSnapshot(context.Context, bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{}, nil
}

func TestK15_WritePaymentErrorUsesTypedHTTPEnvelope(t *testing.T) {
	cases := []struct {
		err            error
		wantStatus     int
		wantCode       string
		bannedFragment string
	}{
		{payments.ErrInsufficientFunds, http.StatusConflict, "INSUFFICIENT_FUNDS", "pq:"},
		{&bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "dial tcp 127.0.0.1:5432 SQLSTATE"}, http.StatusServiceUnavailable, "BANK_UNAVAILABLE", "127.0.0.1"},
		{payments.ErrIdempotencyConflict, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "SQLSTATE"},
		{fmt.Errorf("pq: FATAL connection"), http.StatusInternalServerError, "INTERNAL_ERROR", "pq:"},
	}
	for _, c := range cases {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/payments", nil)
		request = request.WithContext(common.ContextWithRequestID(request.Context(), "k15-req"))
		writePaymentError(recorder, request, c.err)
		if recorder.Code != c.wantStatus {
			t.Fatalf("K15 HTTP: status = %d, want %d for %v", recorder.Code, c.wantStatus, c.err)
		}
		var body struct {
			RequestID string `json:"requestId"`
			Error     struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
			t.Fatalf("K15 HTTP: decode: %v", err)
		}
		if body.RequestID == "" {
			t.Fatal("K15 HTTP: missing requestId")
		}
		if body.Error.Code != c.wantCode {
			t.Fatalf("K15 HTTP: code = %q, want %q", body.Error.Code, c.wantCode)
		}
		if body.Error.Message == "" {
			t.Fatal("K15 HTTP: empty message")
		}
		if strings.Contains(body.Error.Message, c.bannedFragment) {
			t.Fatalf("K15 HTTP: message leaks %q: %q", c.bannedFragment, body.Error.Message)
		}
	}
}
