package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/payments"
)

type createPaymentRequest struct {
	SourceAccountID string `json:"sourceAccountId"`
	Recipient       string `json:"recipient"`
	AmountPaise     int64  `json:"amountPaise"`
	Currency        string `json:"currency"`
}

type paymentResponse struct {
	ID          string    `json:"id"`
	AmountPaise int64     `json:"amountPaise"`
	Currency    string    `json:"currency"`
	State       string    `json:"state"`
	CreatedAt   time.Time `json:"createdAt"`
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
	sourceAccountID, err := uuid.Parse(strings.TrimSpace(input.SourceAccountID))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "source account ID is invalid", http.StatusBadRequest))
		return
	}

	payment, duplicate, err := h.payments.CreateWithResult(request.Context(), payments.CreateInput{
		UserID:          userID,
		SourceAccountID: sourceAccountID,
		Recipient:       input.Recipient,
		AmountPaise:     input.AmountPaise,
		Currency:        input.Currency,
		IdempotencyKey:  request.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writePaymentError(writer, request, err)
		return
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeData(writer, status, request, paymentResponse{
		ID:          payment.ID.String(),
		AmountPaise: payment.AmountPaise,
		Currency:    payment.Currency,
		State:       payment.State,
		CreatedAt:   payment.CreatedAt,
	})
}

func writePaymentError(writer http.ResponseWriter, request *http.Request, err error) {
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
	default:
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "internal server error", http.StatusInternalServerError))
	}
}
