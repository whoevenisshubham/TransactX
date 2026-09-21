package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/common"
)

func (handler *Handler) chaosStart(writer http.ResponseWriter, request *http.Request) {
	if handler.chaosController == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "chaos controller is not configured", http.StatusNotFound))
		return
	}

	identity, ok := auth.IdentityFromRequest(request)
	if !ok {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}

	var req chaos.StartRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "malformed request payload", http.StatusBadRequest))
		return
	}

	scenario, err := handler.chaosController.Start(request.Context(), req, identity.UserID, identity.Role)
	if err != nil {
		if errors.Is(err, chaos.ErrTargetConflict) {
			writeAPIError(writer, request, common.NewAPIError("TARGET_CONFLICT", err.Error(), http.StatusConflict))
			return
		}
		if errors.Is(err, chaos.ErrInvalidScenarioID) ||
			errors.Is(err, chaos.ErrInvalidTargetID) ||
			errors.Is(err, chaos.ErrInvalidScenario) ||
			errors.Is(err, chaos.ErrInvalidParameters) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", err.Error(), http.StatusBadRequest))
			return
		}
		if errors.Is(err, chaos.ErrUnauthorized) {
			writeAPIError(writer, request, common.NewAPIError("FORBIDDEN", err.Error(), http.StatusForbidden))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("start chaos scenario", "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to start chaos scenario", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusCreated, request, scenario)
}

func (handler *Handler) chaosScenarios(writer http.ResponseWriter, request *http.Request) {
	if handler.chaosController == nil {
		writeData(writer, http.StatusOK, request, []chaos.ChaosScenario{})
		return
	}

	activeOnly := request.URL.Query().Get("active") == "true"
	scenarios, err := handler.chaosController.ListScenarios(request.Context(), activeOnly)
	if err != nil {
		if handler.logger != nil {
			handler.logger.Error("list chaos scenarios", "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to list chaos scenarios", http.StatusInternalServerError))
		return
	}
	if scenarios == nil {
		scenarios = []chaos.ChaosScenario{}
	}

	writeData(writer, http.StatusOK, request, scenarios)
}

func (handler *Handler) chaosScenario(writer http.ResponseWriter, request *http.Request) {
	if handler.chaosController == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "chaos controller is not configured", http.StatusNotFound))
		return
	}

	scenarioID := strings.TrimSpace(request.PathValue("scenarioID"))
	if scenarioID == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scenario ID is required", http.StatusBadRequest))
		return
	}

	scenario, err := handler.chaosController.GetScenario(request.Context(), scenarioID)
	if err != nil {
		if errors.Is(err, chaos.ErrScenarioNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "chaos scenario not found", http.StatusNotFound))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("get chaos scenario", "scenario_id", scenarioID, "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to get chaos scenario", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, scenario)
}

func (handler *Handler) chaosStop(writer http.ResponseWriter, request *http.Request) {
	if handler.chaosController == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "chaos controller is not configured", http.StatusNotFound))
		return
	}

	identity, ok := auth.IdentityFromRequest(request)
	if !ok {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}

	scenarioID := strings.TrimSpace(request.PathValue("scenarioID"))
	if scenarioID == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scenario ID is required", http.StatusBadRequest))
		return
	}

	scenario, err := handler.chaosController.Stop(request.Context(), scenarioID, identity.UserID, identity.Role)
	if err != nil {
		if errors.Is(err, chaos.ErrScenarioNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "chaos scenario not found", http.StatusNotFound))
			return
		}
		if errors.Is(err, chaos.ErrUnauthorized) {
			writeAPIError(writer, request, common.NewAPIError("FORBIDDEN", err.Error(), http.StatusForbidden))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("stop chaos scenario", "scenario_id", scenarioID, "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to stop chaos scenario", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, scenario)
}

func (handler *Handler) chaosReset(writer http.ResponseWriter, request *http.Request) {
	if handler.chaosController == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "chaos controller is not configured", http.StatusNotFound))
		return
	}

	identity, ok := auth.IdentityFromRequest(request)
	if !ok {
		writeAPIError(writer, request, common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
		return
	}

	targetID := strings.TrimSpace(request.URL.Query().Get("targetId"))
	if targetID == "" && request.Body != nil {
		var body struct {
			TargetID string `json:"targetId"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		targetID = strings.TrimSpace(body.TargetID)
	}

	if err := handler.chaosController.Reset(request.Context(), targetID, identity.UserID, identity.Role); err != nil {
		if errors.Is(err, chaos.ErrUnauthorized) {
			writeAPIError(writer, request, common.NewAPIError("FORBIDDEN", err.Error(), http.StatusForbidden))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("reset chaos", "target_id", targetID, "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to reset chaos", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, map[string]any{"status": "reset", "targetId": targetID})
}
