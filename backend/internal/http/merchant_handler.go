package http

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/common"
)

// merchantReceiveInfoResponse is the safe, merchant-facing response for
// the receive/QR flow. It intentionally omits internal UUIDs, bank IDs,
// and any other implementation detail.
type merchantReceiveInfoResponse struct {
	PaymentIdentifier string `json:"paymentIdentifier"`
	AccountNumber     string `json:"accountNumber"`
	AccountStatus     string `json:"accountStatus"`
}

// merchantReceiveInfo handles GET /api/merchant/receive-info.
// Only MERCHANT role users can call this endpoint.
func (h *Handler) merchantReceiveInfo(writer http.ResponseWriter, request *http.Request) {
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

	// Fetch the merchant's public profile for the payment identifier.
	user, err := h.users.GetByID(request.Context(), userID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "account details are temporarily unavailable", http.StatusInternalServerError))
		return
	}

	// Fetch the primary account for the account number and status.
	accounts, err := h.accountsRepo.ListOwned(request.Context(), userID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "account details are temporarily unavailable", http.StatusInternalServerError))
		return
	}

	if len(accounts) == 0 {
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_NOT_FOUND", "merchant account not found", http.StatusNotFound))
		return
	}

	primary := accounts[0]

	writeData(writer, http.StatusOK, request, merchantReceiveInfoResponse{
		PaymentIdentifier: user.Public().PaymentID,
		AccountNumber:     primary.AccountNumber,
		AccountStatus:     primary.Status,
	})
}
