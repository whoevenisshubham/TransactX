package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/reconciliation"
)

// reconciliationCreateRequest is the POST /api/ops/reconciliation/runs request body.
// Participant and scope are explicit; clients cannot select arbitrary SQL or
// database paths.
type reconciliationCreateRequest struct {
	ParticipantID string `json:"participantId"`
	ScopeFrom     string `json:"scopeFrom"` // RFC3339
	ScopeTo       string `json:"scopeTo"`   // RFC3339
}

func (handler *Handler) reconciliationCreateRun(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	var req reconciliationCreateRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "malformed request payload", http.StatusBadRequest))
		return
	}

	req.ParticipantID = strings.TrimSpace(req.ParticipantID)
	if req.ParticipantID == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "participantId is required", http.StatusBadRequest))
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

	run, execErr := handler.reconEngine.Execute(request.Context(), reconciliation.RunRequest{
		ParticipantID: req.ParticipantID,
		ScopeFrom:     scopeFrom,
		ScopeTo:       scopeTo,
	})
	if execErr != nil {
		if errors.Is(execErr, reconciliation.ErrInvalidParticipant) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "unknown or invalid participant", http.StatusBadRequest))
			return
		}
		if errors.Is(execErr, reconciliation.ErrInvalidRunScope) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", execErr.Error(), http.StatusBadRequest))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("reconciliation run failed", "participant_id", req.ParticipantID, "error", execErr)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "reconciliation run failed", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusCreated, request, sanitizeRun(run))
}

func (handler *Handler) reconciliationListRuns(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeData(writer, http.StatusOK, request, emptyRunPage())
		return
	}

	participantID := strings.TrimSpace(request.URL.Query().Get("participantId"))
	limit := parseIntParam(request, "limit", 20)
	offset := parseIntParam(request, "offset", 0)

	page, err := handler.reconEngine.ListRuns(request.Context(), reconciliation.ListRunsRequest{
		ParticipantID: participantID,
		Limit:         limit,
		Offset:        offset,
	})
	if err != nil {
		if handler.logger != nil {
			handler.logger.Error("list reconciliation runs", "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to list reconciliation runs", http.StatusInternalServerError))
		return
	}

	respPage := map[string]any{
		"items":  sanitizeRuns(page.Items),
		"total":  page.Total,
		"limit":  page.Limit,
	}
	if page.NextOffset != nil {
		respPage["nextOffset"] = *page.NextOffset
	}
	writeData(writer, http.StatusOK, request, respPage)
}

func (handler *Handler) reconciliationGetRun(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	runIDStr := strings.TrimSpace(request.PathValue("runID"))
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "runID must be a valid UUID", http.StatusBadRequest))
		return
	}

	run, err := handler.reconEngine.GetRun(request.Context(), runID)
	if err != nil {
		if errors.Is(err, reconciliation.ErrRunNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation run not found", http.StatusNotFound))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("get reconciliation run", "run_id", runID, "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to get reconciliation run", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, sanitizeRun(run))
}

func (handler *Handler) reconciliationListDiscrepancies(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	runIDStr := strings.TrimSpace(request.PathValue("runID"))
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "runID must be a valid UUID", http.StatusBadRequest))
		return
	}

	limit := parseIntParam(request, "limit", 50)
	offset := parseIntParam(request, "offset", 0)

	page, err := handler.reconEngine.ListDiscrepancies(request.Context(), reconciliation.ListDiscrepanciesRequest{
		RunID:  runID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		if errors.Is(err, reconciliation.ErrRunNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation run not found", http.StatusNotFound))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("list discrepancies", "run_id", runID, "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to list discrepancies", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, page)
}

// parseIntParam reads an integer query parameter, falling back to the default.
func parseIntParam(request *http.Request, name string, defaultValue int) int {
	raw := strings.TrimSpace(request.URL.Query().Get(name))
	if raw == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return defaultValue
	}
	return v
}

// sanitizeRun removes internal-only fields that should not be exposed via the
// public API. Raw byte hashes are encoded as hex strings in the JSON response.
func sanitizeRun(run reconciliation.Run) map[string]any {
	out := map[string]any{
		"id":               run.ID,
		"participantId":    run.ParticipantID,
		"scopeFrom":        run.ScopeFrom.UTC().Format(time.RFC3339),
		"scopeTo":          run.ScopeTo.UTC().Format(time.RFC3339),
		"status":           run.Status,
		"recordCount":      run.RecordCount,
		"discrepancyCount": run.DiscrepancyCount,
		"startedAt":        run.StartedAt.UTC().Format(time.RFC3339Nano),
	}
	if run.CompletedAt != nil {
		out["completedAt"] = run.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	if len(run.CanonicalRoot) > 0 {
		out["canonicalRootHex"] = hexEncode(run.CanonicalRoot)
	}
	if run.CanonicalVersion != "" {
		out["canonicalVersion"] = run.CanonicalVersion
	}
	if run.AlgorithmVersion != "" {
		out["algorithmVersion"] = run.AlgorithmVersion
	}
	if run.ErrorMessage != "" {
		out["errorMessage"] = run.ErrorMessage
	}
	return out
}

func sanitizeRuns(runs []reconciliation.Run) []map[string]any {
	out := make([]map[string]any, len(runs))
	for i, run := range runs {
		out[i] = sanitizeRun(run)
	}
	return out
}

func emptyRunPage() map[string]any {
	return map[string]any{
		"items":  []any{},
		"total":  0,
		"limit":  20,
	}
}

func hexEncode(b []byte) string {
	const hextable = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = hextable[v>>4]
		dst[i*2+1] = hextable[v&0x0f]
	}
	return string(dst)
}
