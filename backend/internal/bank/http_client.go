package bank

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPClient(baseURL string, httpClient *http.Client) (*HTTPClient, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("bank base URL is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &HTTPClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}, nil
}

func (client *HTTPClient) GetHealth(ctx context.Context) (HealthResult, error) {
	var result HealthResult
	return result, client.do(ctx, http.MethodGet, "/health", "", &result)
}

func (client *HTTPClient) ResolveAccount(ctx context.Context, request ResolveAccountRequest) (AccountResult, error) {
	var result AccountResult
	return result, client.doJSON(ctx, http.MethodPost, "/v1/accounts/resolve", request, &result)
}

func (client *HTTPClient) HoldFunds(ctx context.Context, request HoldFundsRequest) (HoldResult, error) {
	var result HoldResult
	return result, client.doJSON(ctx, http.MethodPost, "/v1/holds", request, &result)
}

func (client *HTTPClient) ProvisionalCredit(ctx context.Context, request ProvisionalCreditRequest) (OperationResult, error) {
	var result OperationResult
	return result, client.doJSON(ctx, http.MethodPost, "/v1/credits/provisional", request, &result)
}

func (client *HTTPClient) ConfirmHold(ctx context.Context, request ConfirmHoldRequest) (OperationResult, error) {
	var result OperationResult
	return result, client.doJSON(ctx, http.MethodPost, "/v1/holds/confirm", request, &result)
}

func (client *HTTPClient) ReleaseHold(ctx context.Context, request ReleaseHoldRequest) (OperationResult, error) {
	var result OperationResult
	return result, client.doJSON(ctx, http.MethodPost, "/v1/holds/release", request, &result)
}

func (client *HTTPClient) ReverseProvisionalCredit(ctx context.Context, request ReverseCreditRequest) (OperationResult, error) {
	var result OperationResult
	return result, client.doJSON(ctx, http.MethodPost, "/v1/credits/reverse", request, &result)
}

func (client *HTTPClient) GetOperationStatus(ctx context.Context, request OperationStatusRequest) (OperationResult, error) {
	var result OperationResult
	path := "/v1/operations/" + url.PathEscape(request.OperationID.String()) + "?paymentId=" + url.QueryEscape(request.PaymentID.String())
	return result, client.do(ctx, http.MethodGet, path, "", &result)
}

func (client *HTTPClient) GetLedgerSnapshot(ctx context.Context, scope LedgerScope) (LedgerSnapshot, error) {
	var result LedgerSnapshot
	path := "/v1/ledger/snapshot?from=" + url.QueryEscape(scope.From.Format(time.RFC3339Nano)) + "&to=" + url.QueryEscape(scope.To.Format(time.RFC3339Nano))
	return result, client.do(ctx, http.MethodGet, path, "", &result)
}

func (client *HTTPClient) doJSON(ctx context.Context, method, path string, input any, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return client.do(ctx, method, path, string(payload), output)
}

func (client *HTTPClient) do(ctx context.Context, method, path, payload string, output any) error {
	var body *strings.Reader
	if payload == "" {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return err
	}
	if payload != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return &AdapterError{Code: ErrCodeTransientFailure, Message: "bank request failed", Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var failure struct {
			Error json.RawMessage `json:"error"`
		}
		_ = json.NewDecoder(response.Body).Decode(&failure)
		var typed struct {
			Code    ErrorCode `json:"code"`
			Message string    `json:"message"`
		}
		_ = json.Unmarshal(failure.Error, &typed)
		return classifyHTTPError(response.StatusCode, typed.Code, typed.Message)
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return &AdapterError{Code: ErrCodeTransientFailure, Message: "bank response is invalid", Err: err}
	}
	return nil
}

func classifyHTTPError(status int, typedCode ErrorCode, message string) error {
	code := ErrCodeTransientFailure
	if typedCode != "" {
		code = typedCode
	}
	if typedCode == "" {
		switch status {
		case http.StatusNotFound:
			code = ErrCodeInvalidAccount
		case http.StatusConflict:
			code = ErrCodeInsufficientFunds
		case http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			code = ErrCodeBankUnavailable
		case http.StatusBadRequest:
			code = ErrCodePermanentFailure
		}
	}
	if message == "" {
		if status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout {
			message = "bank unavailable"
		} else {
			message = fmt.Sprintf("bank request returned HTTP %d", status)
		}
	}
	return &AdapterError{Code: code, Message: message}
}

var _ BankAdapter = (*HTTPClient)(nil)
