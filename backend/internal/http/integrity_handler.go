package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/reconciliation"
)

type integrityCreateRequest struct {
	ParticipantID string `json:"participantId,omitempty"`
	ScopeFrom     string `json:"scopeFrom,omitempty"` // RFC3339
	ScopeTo       string `json:"scopeTo,omitempty"`   // RFC3339
	CheckCodes    []string `json:"checkCodes,omitempty"`
}

func (handler *Handler) integrityCreateRun(writer http.ResponseWriter, request *http.Request) {
	if handler.integrityEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "financial integrity engine is not configured", http.StatusNotFound))
		return
	}

	var req integrityCreateRequest
	if request.Body != nil && request.ContentLength > 0 {
		if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "malformed request payload", http.StatusBadRequest))
			return
		}
	}

	req.ParticipantID = strings.TrimSpace(req.ParticipantID)

	var runScope *reconciliation.Scope
	if req.ScopeFrom != "" || req.ScopeTo != "" {
		if req.ScopeFrom == "" || req.ScopeTo == "" {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "both scopeFrom and scopeTo are required when specifying scope", http.StatusBadRequest))
			return
		}
		scopeFrom, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScopeFrom))
		if err != nil {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeFrom must be an RFC3339 timestamp", http.StatusBadRequest))
			return
		}
		scopeTo, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScopeTo))
		if err != nil {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeTo must be an RFC3339 timestamp", http.StatusBadRequest))
			return
		}
		if !scopeTo.After(scopeFrom) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeTo must be after scopeFrom", http.StatusBadRequest))
			return
		}
		s := reconciliation.Scope{From: scopeFrom, To: scopeTo}
		runScope = &s
	}

	run, execErr := handler.integrityEngine.Run(request.Context(), reconciliation.IntegrityRunRequest{
		ParticipantID: req.ParticipantID,
		Scope:         runScope,
		CheckCodes:    req.CheckCodes,
	})
	if execErr != nil {
		if handler.logger != nil {
			handler.logger.Error("integrity run failed", "error", execErr)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "integrity run execution failed", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusCreated, request, sanitizeIntegrityRun(run))
}

func (handler *Handler) integrityListRuns(writer http.ResponseWriter, request *http.Request) {
	if handler.integrityEngine == nil || handler.integrityRunStore == nil {
		writeData(writer, http.StatusOK, request, map[string]any{
			"items": []any{},
			"total": 0,
			"limit": 20,
		})
		return
	}

	limit := parseIntParam(request, "limit", 20)
	offset := parseIntParam(request, "offset", 0)

	runs, total, err := handler.integrityRunStore.ListRuns(request.Context(), limit, offset)
	if err != nil {
		if handler.logger != nil {
			handler.logger.Error("list integrity runs", "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to list integrity runs", http.StatusInternalServerError))
		return
	}

	sanitized := make([]map[string]any, len(runs))
	for i, r := range runs {
		sanitized[i] = sanitizeIntegrityRun(r)
	}

	writeData(writer, http.StatusOK, request, map[string]any{
		"items": sanitized,
		"total": total,
		"limit": limit,
	})
}

func (handler *Handler) integrityGetRun(writer http.ResponseWriter, request *http.Request) {
	if handler.integrityEngine == nil || handler.integrityRunStore == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "financial integrity engine is not configured", http.StatusNotFound))
		return
	}

	runIDStr := strings.TrimSpace(request.PathValue("runID"))
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "runID must be a valid UUID", http.StatusBadRequest))
		return
	}

	run, err := handler.integrityRunStore.GetRun(request.Context(), runID)
	if err != nil {
		if errors.Is(err, reconciliation.ErrIntegrityRunNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "integrity run not found", http.StatusNotFound))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("get integrity run", "run_id", runID, "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to get integrity run", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, sanitizeIntegrityRun(run))
}

func (handler *Handler) integrityListChecks(writer http.ResponseWriter, request *http.Request) {
	writeData(writer, http.StatusOK, request, reconciliation.AuthoritativeCheckRegistry)
}

func sanitizeIntegrityRun(run reconciliation.IntegrityRunResult) map[string]any {
	out := map[string]any{
		"id":          run.RunID,
		"runId":       run.RunID,
		"status":      run.Status,
		"summary":     run.Summary,
		"checks":      run.Checks,
		"startedAt":   run.StartedAt.UTC().Format(time.RFC3339Nano),
		"completedAt": run.CompletedAt.UTC().Format(time.RFC3339Nano),
	}
	if run.ParticipantID != "" {
		out["participantId"] = run.ParticipantID
	}
	if run.Scope != nil {
		out["scopeFrom"] = run.Scope.From.UTC().Format(time.RFC3339)
		out["scopeTo"] = run.Scope.To.UTC().Format(time.RFC3339)
	}
	if run.ErrorMessage != "" {
		out["errorMessage"] = run.ErrorMessage
	}
	return out
}
