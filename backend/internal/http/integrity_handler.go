package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/reconciliation"
)

func (handler *Handler) integrityCheck(writer http.ResponseWriter, request *http.Request) {
	if handler.integrityEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "integrity engine is not configured", http.StatusNotFound))
		return
	}
	var input reconciliation.IntegrityRunRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "invalid integrity request", http.StatusBadRequest))
		return
	}
	if input.Scope != nil {
		*input.Scope = input.Scope.Normalize()
		if input.Scope.From.IsZero() || input.Scope.To.IsZero() || input.Scope.Validate() != nil {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "a valid bounded scope is required", http.StatusBadRequest))
			return
		}
	}
	for _, code := range input.CheckCodes {
		found := false
		for _, def := range reconciliation.AuthoritativeCheckRegistry {
			if def.Code == code {
				found = true
				break
			}
		}
		if !found {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "unknown integrity check code", http.StatusBadRequest))
			return
		}
	}
	run, err := handler.integrityEngine.Run(request.Context(), input)
	if err != nil {
		if errors.Is(err, reconciliation.ErrIntegrityRunPersistence) {
			writeAPIError(writer, request, common.NewAPIError("INTEGRITY_PERSISTENCE_UNAVAILABLE", "integrity run could not be persisted", http.StatusServiceUnavailable))
			return
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "integrity check failed", http.StatusInternalServerError))
		return
	}
	writeData(writer, http.StatusCreated, request, run)
}

func (handler *Handler) integrityStatus(writer http.ResponseWriter, request *http.Request) {
	if handler.integrityRuns == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "integrity engine is not configured", http.StatusNotFound))
		return
	}
	runs, total, err := handler.integrityRuns.ListRuns(request.Context(), 20, 0)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "integrity history unavailable", http.StatusInternalServerError))
		return
	}
	if runs == nil {
		runs = []reconciliation.IntegrityRunResult{}
	}
	writeData(writer, http.StatusOK, request, map[string]any{"runs": runs, "total": total})
}

func (handler *Handler) integrityRun(writer http.ResponseWriter, request *http.Request) {
	if handler.integrityRuns == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "integrity engine is not configured", http.StatusNotFound))
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(request.PathValue("runID")))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "invalid run ID", http.StatusBadRequest))
		return
	}
	run, err := handler.integrityRuns.GetRun(request.Context(), id)
	if errors.Is(err, reconciliation.ErrIntegrityRunNotFound) {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "integrity run not found", http.StatusNotFound))
		return
	}
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "integrity run unavailable", http.StatusInternalServerError))
		return
	}
	writeData(writer, http.StatusOK, request, run)
}
