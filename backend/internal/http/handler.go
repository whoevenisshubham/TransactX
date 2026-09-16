package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db     *pgxpool.Pool
	logger *slog.Logger
}

func NewHandler(db *pgxpool.Pool, logger *slog.Logger) http.Handler {
	handler := &Handler{db: db, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("GET /health/db", handler.databaseHealth)

	return cors(mux)
}

func (h *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "transactx-api",
	})
}

func (h *Handler) databaseHealth(writer http.ResponseWriter, request *http.Request) {
	if err := h.db.Ping(request.Context()); err != nil {
		h.logger.Warn("database readiness check failed", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
		})
		return
	}

	writeJSON(writer, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		writer.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
