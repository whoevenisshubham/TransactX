package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type sequenceCursorStore struct {
	mu      sync.Mutex
	cursors []map[string]string
	errAt   int
	calls   int
}

func (store *sequenceCursorStore) Current(_ context.Context) (map[string]string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	index := store.calls
	store.calls++
	if index >= len(store.cursors) {
		index = len(store.cursors) - 1
	}
	return store.cursors[index], nil
}

func TestOperationalStreamRequiresOpsAdmin(t *testing.T) {
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, newMemReconStore())
	for _, tc := range []struct {
		role   string
		status int
	}{
		{"", http.StatusUnauthorized},
		{"CUSTOMER", http.StatusForbidden},
		{"MERCHANT", http.StatusForbidden},
		{"OPS_ADMIN", http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/events/stream", nil)
		if tc.role != "" {
			req.Header.Set("Authorization", "Bearer "+tokenFor(t, manager, tc.role))
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("role %q: got %d, want %d", tc.role, res.Code, tc.status)
		}
	}
}

func TestOperationalStreamSendsReadyAndDurableInvalidation(t *testing.T) {
	store := &sequenceCursorStore{cursors: []map[string]string{
		{"health": "1", "routing": "4", "activity": "1:4"},
		{"health": "2", "routing": "4", "activity": "2:4"},
	}}
	handler := &Handler{
		eventCursor:             store,
		streamPollInterval:      time.Millisecond,
		streamHeartbeatInterval: time.Hour,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/ops/events/stream", nil).WithContext(ctx)
	res := httptest.NewRecorder()
	handler.operationalEvents(res, req)

	body := res.Body.String()
	if res.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", res.Header().Get("Content-Type"))
	}
	if !strings.Contains(body, "event: ready") || !strings.Contains(body, `"topics":["all"]`) {
		t.Fatalf("missing ready refetch hint: %s", body)
	}
	if !strings.Contains(body, "event: invalidate") || !strings.Contains(body, `"topics":["activity","health"]`) {
		t.Fatalf("missing deterministic changed topics: %s", body)
	}
}

func TestChangedCursorTopicsStableOrder(t *testing.T) {
	topics := changedCursorTopics(
		map[string]string{"routing": "1", "health": "1", "circuit": "1"},
		map[string]string{"routing": "2", "health": "2", "circuit": "1"},
	)
	if strings.Join(topics, ",") != "health,routing" {
		t.Fatalf("topics = %v, want stable health,routing order", topics)
	}
}
