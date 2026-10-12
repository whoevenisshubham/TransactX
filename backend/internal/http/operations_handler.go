package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/operations"
	"github.com/transactx/backend/internal/telemetry"
)

func (h *Handler) opsWorklist(writer http.ResponseWriter, request *http.Request) {
	if h.operationsSvc == nil {
		writeAPIError(writer, request, common.NewAPIError("SERVICE_UNAVAILABLE", "operations service not configured", http.StatusServiceUnavailable))
		return
	}

	filter := operations.WorklistFilter{
		State:          request.URL.Query().Get("state"),
		RecoveryStatus: request.URL.Query().Get("recoveryStatus"),
		BankCode:       request.URL.Query().Get("bankCode"),
		Cursor:         request.URL.Query().Get("cursor"),
	}

	if limitStr := request.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = limit
		} else {
			writeAPIError(writer, request, common.NewAPIError("INVALID_LIMIT", "limit must be a valid integer", http.StatusBadRequest))
			return
		}
	}

	if pidStr := request.URL.Query().Get("paymentId"); pidStr != "" {
		pid, err := uuid.Parse(pidStr)
		if err != nil {
			writeAPIError(writer, request, common.NewAPIError("INVALID_PAYMENT_ID", "paymentId must be a valid UUID", http.StatusBadRequest))
			return
		}
		filter.PaymentID = &pid
	}

	if fromStr := request.URL.Query().Get("createdFrom"); fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			writeAPIError(writer, request, common.NewAPIError("INVALID_TIMESTAMP", "createdFrom must be RFC3339 format", http.StatusBadRequest))
			return
		}
		filter.CreatedFrom = &t
	}

	if toStr := request.URL.Query().Get("createdTo"); toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			writeAPIError(writer, request, common.NewAPIError("INVALID_TIMESTAMP", "createdTo must be RFC3339 format", http.StatusBadRequest))
			return
		}
		filter.CreatedTo = &t
	}

	worklist, err := h.operationsSvc.GetWorklist(request.Context(), filter)
	if err != nil {
		if errors.Is(err, operations.ErrInvalidCursor) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_CURSOR", err.Error(), http.StatusBadRequest))
			return
		}
		if errors.Is(err, operations.ErrInvalidFilterState) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_FILTER", err.Error(), http.StatusBadRequest))
			return
		}
		h.logger.Error("failed to query worklist", "error", err)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to query worklist projection", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, worklist)
}

func (h *Handler) opsPaymentEvidence(writer http.ResponseWriter, request *http.Request) {
	if h.operationsSvc == nil {
		writeAPIError(writer, request, common.NewAPIError("SERVICE_UNAVAILABLE", "operations service not configured", http.StatusServiceUnavailable))
		return
	}

	paymentIDStr := request.PathValue("paymentID")
	paymentID, err := uuid.Parse(paymentIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_PAYMENT_ID", "paymentID must be a valid UUID", http.StatusBadRequest))
		return
	}

	evidence, err := h.operationsSvc.ReadPaymentEvidence(request.Context(), "trusted_ops", paymentID)
	if err != nil {
		if errors.Is(err, operations.ErrPaymentNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "payment not found", http.StatusNotFound))
			return
		}
		if errors.Is(err, operations.ErrCentralUnavailable) {
			writeAPIError(writer, request, common.NewAPIError("SERVICE_UNAVAILABLE", "central payment authority unavailable", http.StatusServiceUnavailable))
			return
		}
		h.logger.Error("failed to read payment evidence", "error", err, "payment_id", paymentID)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to assemble payment evidence", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, evidence)
}

func (h *Handler) opsPaymentRecoveryAttempts(writer http.ResponseWriter, request *http.Request) {
	if h.operationsSvc == nil {
		writeAPIError(writer, request, common.NewAPIError("SERVICE_UNAVAILABLE", "operations service not configured", http.StatusServiceUnavailable))
		return
	}

	paymentIDStr := request.PathValue("paymentID")
	paymentID, err := uuid.Parse(paymentIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_PAYMENT_ID", "paymentID must be a valid UUID", http.StatusBadRequest))
		return
	}

	limit := 25
	if limitStr := request.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		} else {
			writeAPIError(writer, request, common.NewAPIError("INVALID_LIMIT", "limit must be a valid integer", http.StatusBadRequest))
			return
		}
	}
	cursor := request.URL.Query().Get("cursor")

	page, err := h.operationsSvc.GetRecoveryAttempts(request.Context(), paymentID, limit, cursor)
	if err != nil {
		if errors.Is(err, operations.ErrPaymentNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "payment not found", http.StatusNotFound))
			return
		}
		if errors.Is(err, operations.ErrInvalidCursor) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_CURSOR", err.Error(), http.StatusBadRequest))
			return
		}
		h.logger.Error("failed to read recovery attempts", "error", err, "payment_id", paymentID)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to read recovery attempts", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, page)
}

func (h *Handler) opsPaymentRecoveryRecheck(writer http.ResponseWriter, request *http.Request) {
	if h.operationsSvc == nil {
		writeAPIError(writer, request, common.NewAPIError("SERVICE_UNAVAILABLE", "operations service not configured", http.StatusServiceUnavailable))
		return
	}

	if !h.recoveryControlEnabled {
		writeAPIError(writer, request, common.NewAPIError("RECOVERY_CONTROL_DISABLED", "recovery control is disabled in runtime configuration", http.StatusForbidden))
		return
	}

	idempotencyKey := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		writeAPIError(writer, request, common.NewAPIError("MISSING_IDEMPOTENCY_KEY", "Idempotency-Key header is required", http.StatusBadRequest))
		return
	}

	paymentIDStr := request.PathValue("paymentID")
	paymentID, err := uuid.Parse(paymentIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_PAYMENT_ID", "paymentID must be a valid UUID", http.StatusBadRequest))
		return
	}

	var req operations.RecoveryRecheckRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "malformed request body", http.StatusBadRequest))
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if len(reason) < 1 || len(reason) > 200 {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REASON", "reason must be between 1 and 200 characters", http.StatusBadRequest))
		return
	}

	identity, ok := auth.IdentityFromRequest(request)
	if !ok {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "unauthorized", http.StatusUnauthorized))
		return
	}
	actorID, err := uuid.Parse(identity.UserID)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "invalid actor identity", http.StatusUnauthorized))
		return
	}

	res, err := h.operationsSvc.RecheckRecovery(request.Context(), actorID, identity.Role, paymentID, reason, idempotencyKey)
	if err != nil {
		if errors.Is(err, operations.ErrRateLimitExceeded) {
			writeAPIError(writer, request, common.NewAPIError("RATE_LIMIT_EXCEEDED", err.Error(), http.StatusTooManyRequests))
			return
		}
		if errors.Is(err, operations.ErrIdempotencyConflict) {
			writeAPIError(writer, request, common.NewAPIError("IDEMPOTENCY_CONFLICT", err.Error(), http.StatusConflict))
			return
		}
		if errors.Is(err, operations.ErrPaymentNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "payment not found", http.StatusNotFound))
			return
		}
		h.logger.Error("failed to expedite recovery recheck", "error", err, "payment_id", paymentID)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to expedite recovery recheck", http.StatusInternalServerError))
		return
	}

	status := http.StatusAccepted
	if res.Status == "already_terminal" {
		status = http.StatusOK
	}

	writeData(writer, status, request, res)
}

func (h *Handler) opsPaymentEvidenceExport(writer http.ResponseWriter, request *http.Request) {
	if h.operationsSvc == nil {
		writeAPIError(writer, request, common.NewAPIError("SERVICE_UNAVAILABLE", "operations service not configured", http.StatusServiceUnavailable))
		return
	}

	paymentIDStr := request.PathValue("paymentID")
	paymentID, err := uuid.Parse(paymentIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_PAYMENT_ID", "paymentID must be a valid UUID", http.StatusBadRequest))
		return
	}

	exportBundle, err := h.operationsSvc.ExportPaymentEvidence(request.Context(), "trusted_ops", paymentID)
	if err != nil {
		if errors.Is(err, operations.ErrPaymentNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "payment not found", http.StatusNotFound))
			return
		}
		if errors.Is(err, operations.ErrCentralUnavailable) {
			writeAPIError(writer, request, common.NewAPIError("SERVICE_UNAVAILABLE", "central payment authority unavailable", http.StatusServiceUnavailable))
			return
		}
		h.logger.Error("failed to export payment evidence", "error", err, "payment_id", paymentID)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to export payment evidence", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, exportBundle)
}

func (h *Handler) opsTraceDetails(writer http.ResponseWriter, request *http.Request) {
	traceID := request.PathValue("traceID")
	if traceID == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_ARGUMENT", "traceId path parameter is required", http.StatusBadRequest))
		return
	}

	if err := telemetry.ValidateTraceID(traceID); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_ARGUMENT", "traceId must be a 32-character lowercase hex string", http.StatusBadRequest))
		return
	}

	if h.traceStore == nil {
		resp := telemetry.TraceResponse{
			TraceID:      traceID,
			SourceStatus: "not_collected",
			CollectedAt:  time.Now().UTC(),
			Truncated:    false,
			Spans:        []telemetry.SpanDTO{},
		}
		writeData(writer, http.StatusOK, request, resp)
		return
	}

	traceResp, err := h.traceStore.GetTrace(request.Context(), traceID)
	if err != nil {
		if errors.Is(err, telemetry.ErrInvalidTraceID) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_ARGUMENT", err.Error(), http.StatusBadRequest))
			return
		}
		h.logger.Error("failed to query trace details", "error", err, "trace_id", traceID)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to read trace", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, traceResp)
}
