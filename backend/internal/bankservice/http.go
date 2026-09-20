package bankservice

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
)

func Handler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(writer http.ResponseWriter, request *http.Request) {
		result, err := service.GetHealth(request.Context())
		writeResult(writer, result, err)
	})
	mux.HandleFunc("POST /v1/accounts/resolve", func(writer http.ResponseWriter, request *http.Request) {
		var input bank.ResolveAccountRequest
		if !decode(writer, request, &input) {
			return
		}
		result, err := service.ResolveAccount(request.Context(), input)
		writeResult(writer, result, err)
	})
	mux.HandleFunc("POST /v1/holds", func(writer http.ResponseWriter, request *http.Request) {
		var input bank.HoldFundsRequest
		if !decode(writer, request, &input) {
			return
		}
		result, err := service.HoldFunds(request.Context(), input)
		writeResult(writer, result, err)
	})
	mux.HandleFunc("POST /v1/credits/provisional", func(writer http.ResponseWriter, request *http.Request) {
		var input bank.ProvisionalCreditRequest
		if !decode(writer, request, &input) {
			return
		}
		result, err := service.ProvisionalCredit(request.Context(), input)
		writeResult(writer, result, err)
	})
	mux.HandleFunc("POST /v1/holds/confirm", func(writer http.ResponseWriter, request *http.Request) {
		var input bank.ConfirmHoldRequest
		if !decode(writer, request, &input) {
			return
		}
		result, err := service.ConfirmHold(request.Context(), input)
		writeResult(writer, result, err)
	})
	mux.HandleFunc("POST /v1/holds/release", func(writer http.ResponseWriter, request *http.Request) {
		var input bank.ReleaseHoldRequest
		if !decode(writer, request, &input) {
			return
		}
		result, err := service.ReleaseHold(request.Context(), input)
		writeResult(writer, result, err)
	})
	mux.HandleFunc("POST /v1/credits/reverse", func(writer http.ResponseWriter, request *http.Request) {
		var input bank.ReverseCreditRequest
		if !decode(writer, request, &input) {
			return
		}
		result, err := service.ReverseProvisionalCredit(request.Context(), input)
		writeResult(writer, result, err)
	})
	mux.HandleFunc("GET /v1/operations/{operationID}", func(writer http.ResponseWriter, request *http.Request) {
		operationID, err := uuid.Parse(request.PathValue("operationID"))
		if err != nil {
			writeError(writer, http.StatusBadRequest, "invalid operation ID")
			return
		}
		var paymentID uuid.UUID
		if value := request.URL.Query().Get("paymentId"); value != "" {
			paymentID, err = uuid.Parse(value)
			if err != nil {
				writeError(writer, http.StatusBadRequest, "invalid payment ID")
				return
			}
		}
		result, serviceErr := service.GetOperationStatus(request.Context(), bank.OperationStatusRequest{PaymentID: paymentID, OperationID: operationID})
		writeResult(writer, result, serviceErr)
	})
	mux.HandleFunc("GET /v1/ledger/snapshot", func(writer http.ResponseWriter, request *http.Request) {
		from, err := time.Parse(time.RFC3339, request.URL.Query().Get("from"))
		if err != nil {
			from = time.Unix(0, 0).UTC()
		}
		to, err := time.Parse(time.RFC3339, request.URL.Query().Get("to"))
		if err != nil {
			to = time.Now().UTC().Add(24 * time.Hour)
		}
		result, serviceErr := service.GetLedgerSnapshot(request.Context(), bank.LedgerScope{From: from, To: to})
		writeResult(writer, result, serviceErr)
	})
	return mux
}

func decode(writer http.ResponseWriter, request *http.Request, target any) bool {
	if err := json.NewDecoder(request.Body).Decode(target); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func writeResult(writer http.ResponseWriter, result any, err error) {
	if err != nil {
		writeAdapterError(writer, statusForError(err), err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(result)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeAdapterError(writer, status, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: message})
}

func writeAdapterError(writer http.ResponseWriter, status int, err error) {
	code := bank.ErrCodeTransientFailure
	message := "bank operation could not be completed"
	if adapterErr, ok := err.(*bank.AdapterError); ok {
		code = adapterErr.Code
		if adapterErr.Message != "" {
			message = adapterErr.Message
		}
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"error": map[string]string{"code": string(code), "message": message}})
}

func statusForError(err error) int {
	if adapterErr, ok := err.(*bank.AdapterError); ok {
		switch adapterErr.Code {
		case bank.ErrCodeInvalidAccount, bank.ErrCodeInactiveAccount:
			return http.StatusNotFound
		case bank.ErrCodeInsufficientFunds:
			return http.StatusConflict
		case bank.ErrCodeBankUnavailable:
			return http.StatusServiceUnavailable
		}
	}
	return http.StatusInternalServerError
}
