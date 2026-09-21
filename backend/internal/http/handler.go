package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/circuit"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/health"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/recipients"
	"github.com/transactx/backend/internal/users"
)

type Handler struct {
	db              *pgxpool.Pool
	logger          *slog.Logger
	auth            *auth.Service
	users           *users.Repository
	accountsRepo    *accounts.Repository
	recipients      *recipients.Repository
	payments        *payments.Service
	healthService   *health.Service
	healthTargets   map[string]health.HealthChecker
	circuitBreaker  *circuit.CircuitBreaker
	chaosController *chaos.Controller
}

func NewHandler(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager) http.Handler {
	return newHandler(db, logger, authService, jwtManager, nil)
}

func NewHandlerWithBankAdapter(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapter bank.BankAdapter) http.Handler {
	return newHandler(db, logger, authService, jwtManager, adapter)
}

func NewHandlerWithBankAdapters(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapters map[uuid.UUID]bank.BankAdapter) http.Handler {
	return newHandlerWithAdapters(db, logger, authService, jwtManager, nil, adapters, nil, nil, nil, nil, payments.SelectionModeAdaptive, "")
}

func NewHandlerWithHealth(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, healthService *health.Service) http.Handler {
	return newHandlerWithAdapters(db, logger, authService, jwtManager, nil, nil, nil, nil, nil, healthService, payments.SelectionModeAdaptive, "")
}

func NewHandlerWithBankAdaptersAndHealth(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapters map[uuid.UUID]bank.BankAdapter, healthService *health.Service) http.Handler {
	return newHandlerWithAdapters(db, logger, authService, jwtManager, nil, adapters, nil, nil, nil, healthService, payments.SelectionModeAdaptive, "")
}

func NewHandlerWithBankAdaptersAndHealthTargets(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapters map[uuid.UUID]bank.BankAdapter, healthTargets map[string]health.HealthChecker, healthService *health.Service) http.Handler {
	return newHandlerWithAdapters(db, logger, authService, jwtManager, nil, adapters, healthTargets, nil, nil, healthService, payments.SelectionModeAdaptive, "")
}

func NewHandlerWithBankAdaptersHealthRouting(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapters map[uuid.UUID]bank.BankAdapter, healthTargets map[string]health.HealthChecker, routeTargets map[uuid.UUID]string, healthService *health.Service) http.Handler {
	return newHandlerWithAdapters(db, logger, authService, jwtManager, nil, adapters, healthTargets, routeTargets, nil, healthService, payments.SelectionModeAdaptive, "")
}

func NewHandlerWithExecutionTargets(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapters map[uuid.UUID]bank.BankAdapter, healthTargets map[string]health.HealthChecker, executionTargets map[payments.RouteKey][]payments.ExecutionTarget, healthService *health.Service, mode payments.SelectionMode, staticBaseline string) http.Handler {
	return newHandlerWithAdapters(db, logger, authService, jwtManager, nil, adapters, healthTargets, nil, executionTargets, healthService, mode, staticBaseline)
}

func NewHandlerWithExecutionTargetsAndCircuit(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapters map[uuid.UUID]bank.BankAdapter, healthTargets map[string]health.HealthChecker, executionTargets map[payments.RouteKey][]payments.ExecutionTarget, healthService *health.Service, mode payments.SelectionMode, staticBaseline string, circuitBreaker *circuit.CircuitBreaker) http.Handler {
	return newHandlerWithAdaptersAndChaos(db, logger, authService, jwtManager, nil, adapters, healthTargets, nil, executionTargets, healthService, mode, staticBaseline, circuitBreaker, nil)
}

func NewHandlerWithChaos(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, chaosController *chaos.Controller) http.Handler {
	return newHandlerWithAdaptersAndChaos(db, logger, authService, jwtManager, nil, nil, nil, nil, nil, nil, payments.SelectionModeAdaptive, "", nil, chaosController)
}

func NewHandlerWithExecutionTargetsCircuitAndChaos(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapters map[uuid.UUID]bank.BankAdapter, healthTargets map[string]health.HealthChecker, executionTargets map[payments.RouteKey][]payments.ExecutionTarget, healthService *health.Service, mode payments.SelectionMode, staticBaseline string, circuitBreaker *circuit.CircuitBreaker, chaosController *chaos.Controller) http.Handler {
	return newHandlerWithAdaptersAndChaos(db, logger, authService, jwtManager, nil, adapters, healthTargets, nil, executionTargets, healthService, mode, staticBaseline, circuitBreaker, chaosController)
}

func newHandler(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapter bank.BankAdapter) http.Handler {
	return newHandlerWithAdaptersAndChaos(db, logger, authService, jwtManager, adapter, nil, nil, nil, nil, nil, payments.SelectionModeAdaptive, "", nil, nil)
}

func newHandlerWithAdapters(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapter bank.BankAdapter, adapters map[uuid.UUID]bank.BankAdapter, healthTargets map[string]health.HealthChecker, routeTargets map[uuid.UUID]string, executionTargets map[payments.RouteKey][]payments.ExecutionTarget, healthService *health.Service, mode payments.SelectionMode, staticBaseline string, circuitBreakers ...*circuit.CircuitBreaker) http.Handler {
	var cb *circuit.CircuitBreaker
	if len(circuitBreakers) > 0 {
		cb = circuitBreakers[0]
	}
	return newHandlerWithAdaptersAndChaos(db, logger, authService, jwtManager, adapter, adapters, healthTargets, routeTargets, executionTargets, healthService, mode, staticBaseline, cb, nil)
}

func newHandlerWithAdaptersAndChaos(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager, adapter bank.BankAdapter, adapters map[uuid.UUID]bank.BankAdapter, healthTargets map[string]health.HealthChecker, routeTargets map[uuid.UUID]string, executionTargets map[payments.RouteKey][]payments.ExecutionTarget, healthService *health.Service, mode payments.SelectionMode, staticBaseline string, circuitBreaker *circuit.CircuitBreaker, chaosController *chaos.Controller) http.Handler {
	handler := &Handler{
		db:              db,
		logger:          logger,
		auth:            authService,
		users:           users.NewRepository(db),
		accountsRepo:    accounts.NewRepository(db),
		recipients:      recipients.NewRepository(db),
		healthService:   healthService,
		healthTargets:   healthTargets,
		circuitBreaker:  circuitBreaker,
		chaosController: chaosController,
	}
	if adapters != nil {
		if executionTargets != nil {
			handler.payments = payments.NewServiceWithExecutionTargets(handler.accountsRepo, handler.recipients, payments.NewRepository(db), adapters, executionTargets, healthService, mode, staticBaseline)
		} else if routeTargets != nil {
			handler.payments = payments.NewServiceWithAdaptiveRouting(handler.accountsRepo, handler.recipients, payments.NewRepository(db), adapters, routeTargets, healthService)
		} else {
			handler.payments = payments.NewServiceWithAdapters(handler.accountsRepo, handler.recipients, payments.NewRepository(db), adapters)
		}
	} else {
		handler.payments = payments.NewService(handler.accountsRepo, handler.recipients, payments.NewRepository(db), adapter)
	}
	if handler.circuitBreaker != nil && handler.payments != nil {
		handler.payments.SetCircuitEligibility(handler.circuitBreaker.EligibilityHook())
	}
	if handler.circuitBreaker != nil && handler.healthService != nil {
		handler.healthService.SetSampleObserver(handler.circuitBreaker.RecordHealthSample)
		handler.healthService.SetProbeGate(handler.circuitBreaker)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("GET /health/db", handler.databaseHealth)
	if healthService != nil {
		mux.Handle("GET /api/ops/health/{targetID}", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.healthSnapshot))))
		mux.Handle("POST /api/ops/health/{targetID}/sample", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.healthSample))))
	}
	if handler.circuitBreaker != nil {
		mux.Handle("GET /api/ops/circuit", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.circuitSnapshots))))
		mux.Handle("GET /api/ops/circuit/{targetID}", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.circuitSnapshot))))
		mux.Handle("GET /api/ops/circuit/{targetID}/events", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.circuitEvents))))
	}
	if handler.chaosController != nil {
		mux.Handle("POST /api/ops/chaos/start", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.chaosStart))))
		mux.Handle("GET /api/ops/chaos/scenarios", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.chaosScenarios))))
		mux.Handle("GET /api/ops/chaos/scenarios/{scenarioID}", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.chaosScenario))))
		mux.Handle("POST /api/ops/chaos/scenarios/{scenarioID}/stop", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.chaosStop))))
		mux.Handle("POST /api/ops/chaos/reset", auth.Authentication(jwtManager, auth.RequireRole(users.RoleOpsAdmin)(http.HandlerFunc(handler.chaosReset))))
	}
	mux.HandleFunc("POST /api/auth/register", handler.register)
	mux.HandleFunc("POST /api/auth/login", handler.login)
	mux.Handle("GET /api/me", auth.Authentication(jwtManager, http.HandlerFunc(handler.me)))
	mux.Handle("GET /api/accounts", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.accounts))))
	mux.Handle("GET /api/accounts/{accountID}", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.account))))
	mux.Handle("GET /api/recipients/{paymentIdentifier}", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.recipient))))
	mux.Handle("POST /api/payments", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.createPayment))))
	mux.Handle("GET /api/payments", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.paymentsList))))
	mux.Handle("GET /api/payments/{paymentID}", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.paymentDetails))))
	return common.RequestIDMiddleware(cors(mux))
}

func (h *Handler) health(writer http.ResponseWriter, request *http.Request) {
	writeData(writer, http.StatusOK, request, map[string]string{"status": "ok", "service": "transactx-api"})
}

func (h *Handler) databaseHealth(writer http.ResponseWriter, request *http.Request) {
	if err := h.db.Ping(request.Context()); err != nil {
		h.logger.Warn("database readiness check failed", "request_id", common.GetRequestID(request), "error", err)
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "database unavailable", http.StatusServiceUnavailable))
		return
	}
	writeData(writer, http.StatusOK, request, map[string]string{"status": "ok"})
}

func writeData(writer http.ResponseWriter, status int, request *http.Request, data any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"requestId": common.GetRequestID(request), "data": data})
}

func writeAPIError(writer http.ResponseWriter, request *http.Request, err *common.APIError) {
	common.WriteError(writer, common.GetRequestID(request), err)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key, X-Request-ID")
		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
