package http

import (
	"net/http"

	"github.com/transactx/backend/internal/common"
)

// routingDistribution reports actual persisted route selections in a fixed
// 24-hour window. Circuit eligibility is a separate operational signal.
func (handler *Handler) routingDistribution(writer http.ResponseWriter, request *http.Request) {
	if handler.db == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "routing history is not configured", http.StatusNotFound))
		return
	}
	rows, err := handler.db.Query(request.Context(), `
		SELECT execution_target_id, count(*)
		FROM payment_route_decisions
		WHERE selected_at >= now() - interval '24 hours'
		GROUP BY execution_target_id
		ORDER BY count(*) DESC, execution_target_id ASC
		LIMIT 100`)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "routing distribution unavailable", http.StatusInternalServerError))
		return
	}
	defer rows.Close()
	type targetCount struct {
		TargetID string `json:"targetId"`
		Payments int64  `json:"payments"`
	}
	items := make([]targetCount, 0)
	for rows.Next() {
		var item targetCount
		if err := rows.Scan(&item.TargetID, &item.Payments); err != nil {
			writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "routing distribution unavailable", http.StatusInternalServerError))
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "routing distribution unavailable", http.StatusInternalServerError))
		return
	}
	writeData(writer, http.StatusOK, request, map[string]any{"windowHours": 24, "items": items})
}
