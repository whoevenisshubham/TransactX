package http

import (
	"net/http"

	"github.com/transactx/backend/internal/common"
)

func (h *Handler) recipient(writer http.ResponseWriter, request *http.Request) {
	identifier := common.NormalizeIdentifier(request.PathValue("paymentIdentifier"))
	if !common.ValidLength(identifier, 3, 128) {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "payment identifier is invalid", http.StatusBadRequest))
		return
	}
	result, err := h.recipients.Resolve(request.Context(), identifier)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("RECIPIENT_NOT_FOUND", "recipient not found", http.StatusNotFound))
		return
	}
	if result.AccountStatus != "ACTIVE" {
		writeAPIError(writer, request, common.NewAPIError("ACCOUNT_INACTIVE", "recipient account is inactive", http.StatusConflict))
		return
	}
	writeData(writer, http.StatusOK, request, result)
}
