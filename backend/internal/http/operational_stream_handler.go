package http

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/common"
)

const (
	defaultStreamPollInterval      = 2 * time.Second
	defaultStreamHeartbeatInterval = 15 * time.Second
)

type operationalCursorStore interface {
	Current(ctx context.Context) (map[string]string, error)
}

type postgresOperationalCursorStore struct {
	db *pgxpool.Pool
}

func newPostgresOperationalCursorStore(db *pgxpool.Pool) *postgresOperationalCursorStore {
	return &postgresOperationalCursorStore{db: db}
}

// Current returns durable source watermarks. Reconciliation and integrity use
// terminal timestamps as well as counts because their rows are updated when a
// run completes; append-only sources use their monotonically increasing IDs.
func (store *postgresOperationalCursorStore) Current(ctx context.Context) (map[string]string, error) {
	var health, routing, circuit, chaos, reconciliation, integrity string
	err := store.db.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT max(id)::text FROM health_samples), '0'),
			COALESCE((SELECT max(id)::text FROM payment_route_decisions), '0'),
			COALESCE((SELECT max(id)::text FROM circuit_transition_events), '0'),
			COALESCE((SELECT max(id)::text FROM chaos_events), '0'),
			COALESCE((SELECT max(GREATEST(started_at, COALESCE(completed_at, started_at)))::text || ':' || count(*)::text FROM recon_runs), ''),
			COALESCE((SELECT max(GREATEST(started_at, COALESCE(completed_at, started_at)))::text || ':' || count(*)::text FROM integrity_runs), '')
	`).Scan(&health, &routing, &circuit, &chaos, &reconciliation, &integrity)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"health":         health,
		"routing":        routing,
		"circuit":        circuit,
		"chaos":          chaos,
		"reconciliation": reconciliation,
		"integrity":      integrity,
		"activity":       health + ":" + routing + ":" + circuit + ":" + chaos + ":" + reconciliation + ":" + integrity,
	}, nil
}

type operationalStreamHint struct {
	Topics   []string `json:"topics"`
	Reason   string   `json:"reason"`
	Sequence int64    `json:"sequence"`
}

func changedCursorTopics(previous, current map[string]string) []string {
	topics := make([]string, 0)
	for topic, watermark := range current {
		if previous[topic] != watermark {
			topics = append(topics, topic)
		}
	}
	sort.Strings(topics)
	return topics
}

func writeSSE(writer http.ResponseWriter, event string, payload operationalStreamHint) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = writer.Write([]byte("event: " + event + "\ndata: " + string(data) + "\n\n"))
	return err
}

// operationalEvents streams invalidation hints. HTTP reads remain the source
// of truth; the stream carries no authoritative financial or health state.
func (handler *Handler) operationalEvents(writer http.ResponseWriter, request *http.Request) {
	if handler.eventCursor == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "operational event stream is not configured", http.StatusNotFound))
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "streaming is unavailable", http.StatusInternalServerError))
		return
	}

	cursor, err := handler.eventCursor.Current(request.Context())
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "operational event stream is unavailable", http.StatusInternalServerError))
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	if err := writeSSE(writer, "ready", operationalStreamHint{Topics: []string{"all"}, Reason: "connected", Sequence: time.Now().UTC().UnixNano()}); err != nil {
		return
	}
	flusher.Flush()

	pollInterval := handler.streamPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultStreamPollInterval
	}
	heartbeatInterval := handler.streamHeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultStreamHeartbeatInterval
	}
	poll := time.NewTicker(pollInterval)
	heartbeat := time.NewTicker(heartbeatInterval)
	defer poll.Stop()
	defer heartbeat.Stop()

	for {
		select {
		case <-request.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := writer.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
			next, err := handler.eventCursor.Current(request.Context())
			if err != nil {
				_ = writeSSE(writer, "stale", operationalStreamHint{Topics: []string{"all"}, Reason: "source_unavailable", Sequence: time.Now().UTC().UnixNano()})
				flusher.Flush()
				return
			}
			topics := changedCursorTopics(cursor, next)
			if len(topics) == 0 {
				continue
			}
			cursor = next
			if err := writeSSE(writer, "invalidate", operationalStreamHint{Topics: topics, Reason: "durable_state_changed", Sequence: time.Now().UTC().UnixNano()}); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
