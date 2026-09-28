package http

import (
	"net/http"
	"strings"

	"github.com/transactx/backend/internal/common"
)

func (handler *Handler) circuitSnapshots(writer http.ResponseWriter, request *http.Request) {
	if handler.circuitBreaker == nil {
		writeData(writer, http.StatusOK, request, map[string]any{})
		return
	}
	writeData(writer, http.StatusOK, request, handler.circuitBreaker.Snapshots())
}

func (handler *Handler) circuitSnapshot(writer http.ResponseWriter, request *http.Request) {
	targetID := strings.TrimSpace(request.PathValue("targetID"))
	if targetID == "" || len(targetID) > 120 {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "circuit target is invalid", http.StatusBadRequest))
		return
	}
	if handler.circuitBreaker == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "circuit breaker is not configured", http.StatusNotFound))
		return
	}
	snapshot := handler.circuitBreaker.Snapshot(targetID)
	writeData(writer, http.StatusOK, request, snapshot)
}

func (handler *Handler) circuitEvents(writer http.ResponseWriter, request *http.Request) {
	targetID := strings.TrimSpace(request.PathValue("targetID"))
	if targetID == "" || len(targetID) > 120 {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "circuit target is invalid", http.StatusBadRequest))
		return
	}
	if handler.circuitBreaker == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "circuit breaker is not configured", http.StatusNotFound))
		return
	}
	events, err := handler.circuitBreaker.GetEventsForTarget(request.Context(), targetID, 50)
	if err != nil {
		handler.logger.Error("list circuit transition events", "target_id", targetID, "error", err)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to retrieve circuit events", http.StatusInternalServerError))
		return
	}
	writeData(writer, http.StatusOK, request, events)
}
