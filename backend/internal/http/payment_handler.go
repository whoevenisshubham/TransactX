package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/payments"
)

type createPaymentRequest struct {
	Recipient   string `json:"recipient"`
	AmountPaise int64  `json:"amountPaise"`
	Currency    string `json:"currency"`
	Note        string `json:"note"`
}

func (h *Handler) createPayment(writer http.ResponseWriter, request *http.Request) {
	var input createPaymentRequest
	if err := decodeJSON(writer, request, &input); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "request body is invalid", http.StatusBadRequest))
		return
	}
	identity, ok := auth.IdentityFromRequest(request)
	if !ok {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}
	userID, err := uuid.Parse(identity.UserID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}
	source, err := h.accountsRepo.GetPrimaryOwned(request.Context(), userID)
	if errors.Is(err, accounts.ErrAmbiguousPrimary) {
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_AMBIGUOUS", "your account setup needs attention before sending a payment", http.StatusConflict))
		return
	}
	if errors.Is(err, accounts.ErrNotFound) {
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_NOT_FOUND", "source account not found", http.StatusNotFound))
		return
	}
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "account details are temporarily unavailable", http.StatusInternalServerError))
		return
	}

	payment, duplicate, createErr := h.payments.CreateWithResult(request.Context(), payments.CreateInput{
		UserID:          userID,
		SourceAccountID: source.ID,
		Recipient:       input.Recipient,
		AmountPaise:     input.AmountPaise,
		Currency:        input.Currency,
		Note:            input.Note,
		IdempotencyKey:  request.Header.Get("Idempotency-Key"),
	})
	if createErr != nil {
		if payment.ID != uuid.Nil {
			if customer, viewErr := h.payments.GetForUser(request.Context(), userID, payment.ID); viewErr == nil && intermediatePaymentState(customer.State) {
				writeData(writer, http.StatusAccepted, request, customer)
				return
			}
		}
		writePaymentError(writer, request, createErr)
		return
	}
	customer, err := h.payments.GetForUser(request.Context(), userID, payment.ID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "payment details are temporarily unavailable", http.StatusInternalServerError))
		return
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	if intermediatePaymentState(customer.State) {
		status = http.StatusAccepted
	}
	writeData(writer, status, request, customer)
}

func (h *Handler) paymentsList(writer http.ResponseWriter, request *http.Request) {
	identity, ok := auth.IdentityFromRequest(request)
	if !ok {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}
	userID, err := uuid.Parse(identity.UserID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	result, err := h.payments.ListForUser(request.Context(), userID, limit)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "transactions are temporarily unavailable", http.StatusInternalServerError))
		return
	}
	writeData(writer, http.StatusOK, request, result)
}

func (h *Handler) paymentDetails(writer http.ResponseWriter, request *http.Request) {
	identity, ok := auth.IdentityFromRequest(request)
	if !ok {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}
	userID, userErr := uuid.Parse(identity.UserID)
	paymentID, paymentErr := uuid.Parse(request.PathValue("paymentID"))
	if userErr != nil || paymentErr != nil {
		writeAPIError(writer, request, common.NewAPIError("PAYMENT_NOT_FOUND", "transaction not found", http.StatusNotFound))
		return
	}
	result, err := h.payments.GetForUser(request.Context(), userID, paymentID)
	if errors.Is(err, payments.ErrNotFound) {
		writeAPIError(writer, request, common.NewAPIError("PAYMENT_NOT_FOUND", "transaction not found", http.StatusNotFound))
		return
	}
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "transaction details are temporarily unavailable", http.StatusInternalServerError))
		return
	}
	writeData(writer, http.StatusOK, request, result)
}

func intermediatePaymentState(state string) bool {
	switch state {
	case payments.StateProcessing, payments.StatePendingReconciliation, payments.StateBankSettledCentralPending:
		return true
	default:
		return false
	}
}

func writePaymentError(writer http.ResponseWriter, request *http.Request, err error) {
	var adapterErr *bank.AdapterError
	if errors.As(err, &adapterErr) {
		switch adapterErr.Code {
		case bank.ErrCodeInsufficientFunds:
			writeAPIError(writer, request, common.NewAPIError("INSUFFICIENT_FUNDS", "source account has insufficient funds", http.StatusConflict))
			return
		case bank.ErrCodeInvalidAccount:
			writeAPIError(writer, request, common.NewAPIError("ACCOUNT_NOT_FOUND", "bank account not found", http.StatusNotFound))
			return
		case bank.ErrCodeInactiveAccount:
			writeAPIError(writer, request, common.NewAPIError("ACCOUNT_INACTIVE", "bank account is inactive", http.StatusConflict))
			return
		case bank.ErrCodeBankUnavailable:
			writeAPIError(writer, request, common.NewAPIError("BANK_UNAVAILABLE", "selected bank route is unavailable", http.StatusServiceUnavailable))
			return
		case bank.ErrCodeTransientFailure:
			writeAPIError(writer, request, common.NewAPIError("BANK_UNAVAILABLE", "bank communication failed", http.StatusServiceUnavailable))
			return
		}
	}

	switch {
	case errors.Is(err, payments.ErrInvalidRequest):
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "payment request is invalid", http.StatusBadRequest))
	case errors.Is(err, payments.ErrSourceNotFound):
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_NOT_FOUND", "source account not found", http.StatusNotFound))
	case errors.Is(err, payments.ErrSourceInactive):
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_INACTIVE", "source account is inactive", http.StatusConflict))
	case errors.Is(err, payments.ErrRecipientNotFound):
		writeAPIError(writer, request, common.NewAPIError("RECIPIENT_NOT_FOUND", "recipient not found", http.StatusNotFound))
	case errors.Is(err, payments.ErrRecipientInactive):
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_INACTIVE", "recipient account is inactive", http.StatusConflict))
	case errors.Is(err, payments.ErrSelfPayment):
		writeAPIError(writer, request, common.NewAPIError("SELF_PAYMENT_NOT_ALLOWED", "payer cannot pay their own account", http.StatusConflict))
	case errors.Is(err, payments.ErrInsufficientFunds):
		writeAPIError(writer, request, common.NewAPIError("INSUFFICIENT_FUNDS", "source account has insufficient funds", http.StatusConflict))
	case errors.Is(err, payments.ErrIdempotencyConflict):
		writeAPIError(writer, request, common.NewAPIError("IDEMPOTENCY_CONFLICT", "idempotency key was already used for a different payment request", http.StatusConflict))
	case errors.Is(err, payments.ErrAmbiguousSource):
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_AMBIGUOUS", "your account setup needs attention before sending a payment", http.StatusConflict))
	case errors.Is(err, payments.ErrBankRouteUnavailable):
		writeAPIError(writer, request, common.NewAPIError("BANK_UNAVAILABLE", "selected bank route is unavailable", http.StatusServiceUnavailable))
	default:
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "internal server error", http.StatusInternalServerError))
	}
}
