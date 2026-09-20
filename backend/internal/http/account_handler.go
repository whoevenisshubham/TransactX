package http

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/common"
)

type customerAccountResponse struct {
	AccountNumber string `json:"accountNumber"`
	BalancePaise  int64  `json:"balancePaise"`
	Status        string `json:"status"`
}

func safeAccount(account accounts.Account) customerAccountResponse {
	return customerAccountResponse{AccountNumber: account.AccountNumber, BalancePaise: account.BalancePaise, Status: account.Status}
}

func (h *Handler) me(writer http.ResponseWriter, request *http.Request) {
	identity, _ := auth.IdentityFromRequest(request)
	userID, err := uuid.Parse(identity.UserID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}
	user, err := h.users.GetByID(request.Context(), userID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}
	writeData(writer, http.StatusOK, request, user.Public())
}

func (h *Handler) accounts(writer http.ResponseWriter, request *http.Request) {
	identity, _ := auth.IdentityFromRequest(request)
	userID, err := uuid.Parse(identity.UserID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}
	result, err := h.accountsRepo.ListOwned(request.Context(), userID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "internal server error", http.StatusInternalServerError))
		return
	}
	safe := make([]customerAccountResponse, 0, len(result))
	for _, account := range result {
		safe = append(safe, safeAccount(account))
	}
	writeData(writer, http.StatusOK, request, safe)
}

func (h *Handler) account(writer http.ResponseWriter, request *http.Request) {
	identity, _ := auth.IdentityFromRequest(request)
	userID, userErr := uuid.Parse(identity.UserID)
	accountID, accountErr := uuid.Parse(request.PathValue("accountID"))
	if userErr != nil || accountErr != nil {
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_NOT_FOUND", "account not found", http.StatusNotFound))
		return
	}
	account, err := h.accountsRepo.GetOwned(request.Context(), userID, accountID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_NOT_FOUND", "account not found", http.StatusNotFound))
		return
	}
	writeData(writer, http.StatusOK, request, safeAccount(account))
}
