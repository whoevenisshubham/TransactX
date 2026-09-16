package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/recipients"
	"github.com/transactx/backend/internal/users"
)

type Handler struct {
	db           *pgxpool.Pool
	logger       *slog.Logger
	auth         *auth.Service
	users        *users.Repository
	accountsRepo *accounts.Repository
	recipients   *recipients.Repository
	payments     *payments.Service
}

func NewHandler(db *pgxpool.Pool, logger *slog.Logger, authService *auth.Service, jwtManager *auth.JWTManager) http.Handler {
	handler := &Handler{
		db:           db,
		logger:       logger,
		auth:         authService,
		users:        users.NewRepository(db),
		accountsRepo: accounts.NewRepository(db),
		recipients:   recipients.NewRepository(db),
	}
	handler.payments = payments.NewService(handler.accountsRepo, handler.recipients, payments.NewRepository(db))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("GET /health/db", handler.databaseHealth)
	mux.HandleFunc("POST /api/auth/register", handler.register)
	mux.HandleFunc("POST /api/auth/login", handler.login)
	mux.Handle("GET /api/me", auth.Authentication(jwtManager, http.HandlerFunc(handler.me)))
	mux.Handle("GET /api/accounts", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.accounts))))
	mux.Handle("GET /api/accounts/{accountID}", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.account))))
	mux.Handle("GET /api/recipients/{paymentIdentifier}", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.recipient))))
	mux.Handle("POST /api/payments", auth.Authentication(jwtManager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(handler.createPayment))))
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
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
