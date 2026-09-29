package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeActivityStore struct {
	events []ActivityEvent
	err    error
	limit  int
	offset int
}

func (store *fakeActivityStore) List(_ context.Context, limit, offset int) ([]ActivityEvent, error) {
	store.limit = limit
	store.offset = offset
	return append([]ActivityEvent(nil), store.events...), store.err
}

func TestActivityFeedRequiresOpsAdmin(t *testing.T) {
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
		req := httptest.NewRequest(http.MethodGet, "/api/ops/activity", nil)
		if tc.role != "" {
			req.Header.Set("Authorization", "Bearer "+tokenFor(t, manager, tc.role))
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("role %q: got %d, want %d: %s", tc.role, res.Code, tc.status, res.Body.String())
		}
	}
}

func TestActivityFeedDeterministicOrderAndBoundedPage(t *testing.T) {
	now := time.Date(2026, 9, 29, 1, 2, 3, 0, time.UTC)
	store := &fakeActivityStore{events: []ActivityEvent{
		{ID: "route:1", Category: "ROUTING", EventType: "PAYMENT_ROUTED", OccurredAt: now.Add(-time.Second), Details: map[string]any{}},
		{ID: "circuit:8", Category: "CIRCUIT", EventType: "CIRCUIT_STATE_TRANSITION", OccurredAt: now, Details: map[string]any{}},
		{ID: "chaos:9", Category: "CHAOS", EventType: "CHAOS_STARTED", OccurredAt: now, Details: map[string]any{}},
	}}
	handler := &Handler{activity: store}
	req := httptest.NewRequest(http.MethodGet, "/api/ops/activity?limit=2&offset=7", nil)
	res := httptest.NewRecorder()
	handler.activityFeed(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	if store.limit != 3 || store.offset != 7 {
		t.Fatalf("store page = limit %d offset %d, want 3 and 7", store.limit, store.offset)
	}
	var response struct {
		Data struct {
			Items      []ActivityEvent `json:"items"`
			Limit      int             `json:"limit"`
			NextOffset *int            `json:"nextOffset"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Data.Items) != 2 || response.Data.Items[0].ID != "circuit:8" || response.Data.Items[1].ID != "chaos:9" {
		t.Fatalf("items not ordered by occurredAt DESC, id DESC: %+v", response.Data.Items)
	}
	if response.Data.Limit != 2 || response.Data.NextOffset == nil || *response.Data.NextOffset != 9 {
		t.Fatalf("page metadata = limit %d next %v", response.Data.Limit, response.Data.NextOffset)
	}
}

func TestActivityFeedReportsStoreFailure(t *testing.T) {
	handler := &Handler{activity: &fakeActivityStore{err: errors.New("query failed")}}
	req := httptest.NewRequest(http.MethodGet, "/api/ops/activity", nil)
	res := httptest.NewRecorder()
	handler.activityFeed(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.Code)
	}
}

func TestActivityFeedNormalizesUnsafePagination(t *testing.T) {
	store := &fakeActivityStore{}
	handler := &Handler{activity: store}
	req := httptest.NewRequest(http.MethodGet, "/api/ops/activity?limit=9999&offset=-4", nil)
	res := httptest.NewRecorder()
	handler.activityFeed(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", res.Code, res.Body.String())
	}
	if store.limit != maxActivityLimit+1 || store.offset != 0 {
		t.Fatalf("store page = limit %d offset %d, want %d and 0", store.limit, store.offset, maxActivityLimit+1)
	}
}
