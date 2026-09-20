package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/health"
)

func (handler *Handler) healthSnapshot(writer http.ResponseWriter, request *http.Request) {
	targetID := strings.TrimSpace(request.PathValue("targetID"))
	if targetID == "" || len(targetID) > 120 {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "health target is invalid", http.StatusBadRequest))
		return
	}
	snapshot, err := handler.healthService.GetSnapshot(request.Context(), targetID, time.Now())
	if errors.Is(err, health.ErrInvalidTarget) {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "health target is invalid", http.StatusBadRequest))
		return
	}
	if err != nil {
		handler.logger.Warn("health snapshot failed", "request_id", common.GetRequestID(request), "error", err)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "health data is temporarily unavailable", http.StatusServiceUnavailable))
		return
	}
	writeData(writer, http.StatusOK, request, snapshot)
}

func (handler *Handler) healthSample(writer http.ResponseWriter, request *http.Request) {
	targetID := strings.TrimSpace(request.PathValue("targetID"))
	if targetID == "" || len(targetID) > 120 {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "health target is invalid", http.StatusBadRequest))
		return
	}
	checker := handler.healthTargets[targetID]
	if checker == nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "health target is invalid", http.StatusBadRequest))
		return
	}
	sample, err := handler.healthService.Sample(request.Context(), targetID, checker)
	if errors.Is(err, health.ErrInvalidTarget) {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "health target is invalid", http.StatusBadRequest))
		return
	}
	if err != nil {
		handler.logger.Warn("health sample failed", "request_id", common.GetRequestID(request), "error", err)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "health sample could not be recorded", http.StatusServiceUnavailable))
		return
	}
	writeData(writer, http.StatusOK, request, sample)
}
