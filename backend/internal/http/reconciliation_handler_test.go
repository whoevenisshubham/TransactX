package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/reconciliation"
)

// --- helper: in-memory RunStore for handler tests ---

type memReconStore struct {
	runs          map[uuid.UUID]reconciliation.Run
	discrepancies []reconciliation.Discrepancy
}

func newMemReconStore() *memReconStore {
	return &memReconStore{runs: make(map[uuid.UUID]reconciliation.Run)}
}

func (s *memReconStore) CreateRun(_ context.Context, participantID string, scope reconciliation.Scope) (reconciliation.Run, error) {
	run := reconciliation.Run{
		ID:            uuid.New(),
		ParticipantID: participantID,
		ScopeFrom:     scope.From.UTC(),
		ScopeTo:       scope.To.UTC(),
		Status:        reconciliation.RunStatusRunning,
		StartedAt:     time.Now().UTC(),
	}
	s.runs[run.ID] = run
	return run, nil
}

func (s *memReconStore) CompleteRun(_ context.Context, runID uuid.UUID, canonRoot, partRoot []byte, canonVer, algoVer string, recordCount, discrepancyCount int64) (reconciliation.Run, error) {
	run, ok := s.runs[runID]
	if !ok {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	run.Status = reconciliation.RunStatusCompleted
	run.CanonicalRoot = canonRoot
	run.ParticipantRoot = partRoot
	run.CanonicalVersion = canonVer
	run.AlgorithmVersion = algoVer
	run.RecordCount = recordCount
	run.DiscrepancyCount = discrepancyCount
	now := time.Now().UTC()
	run.CompletedAt = &now
	s.runs[runID] = run
	return run, nil
}

func (s *memReconStore) FailRun(_ context.Context, runID uuid.UUID, errMsg string) (reconciliation.Run, error) {
	run, ok := s.runs[runID]
	if !ok {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	run.Status = reconciliation.RunStatusFailed
	run.ErrorMessage = errMsg
	now := time.Now().UTC()
	run.CompletedAt = &now
	s.runs[runID] = run
	return run, nil
}

func (s *memReconStore) GetRun(_ context.Context, runID uuid.UUID) (reconciliation.Run, error) {
	run, ok := s.runs[runID]
	if !ok {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	return run, nil
}

func (s *memReconStore) ListRuns(_ context.Context, req reconciliation.ListRunsRequest) (reconciliation.RunListPage, error) {
	items := make([]reconciliation.Run, 0)
	for _, run := range s.runs {
		if req.ParticipantID == "" || run.ParticipantID == req.ParticipantID {
			items = append(items, run)
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	page := reconciliation.RunListPage{Total: len(items), Limit: limit, Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		nextOff := req.Offset + limit
		page.NextOffset = &nextOff
	}
	return page, nil
}

func (s *memReconStore) SaveDiscrepancy(_ context.Context, disc reconciliation.Discrepancy) (reconciliation.Discrepancy, error) {
	disc.ID = uuid.New()
	disc.DetectedAt = time.Now().UTC()
	s.discrepancies = append(s.discrepancies, disc)
	return disc, nil
}

func (s *memReconStore) ListDiscrepancies(_ context.Context, req reconciliation.ListDiscrepanciesRequest) (reconciliation.DiscrepancyListPage, error) {
	items := make([]reconciliation.Discrepancy, 0)
	for _, d := range s.discrepancies {
		if d.RunID == req.RunID {
			items = append(items, d)
		}
	}
	page := reconciliation.DiscrepancyListPage{Total: len(items), Limit: 50, Items: items}
	return page, nil
}

// --- test helper ---

func makeReconHandler(t *testing.T, participants []string, store *memReconStore) (http.Handler, *auth.JWTManager) {
	t.Helper()
	manager, err := auth.NewJWTManager(strings.Repeat("r", 32), "recon-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	known := make(reconciliation.KnownParticipants)
	for _, p := range participants {
		known[p] = true
	}
	engine := reconciliation.NewEngineWithRepo(known, store,
		func(ctx context.Context, id string, scope reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			p, err := reconciliation.NewMemoryParticipant(id, "test-partition", time.Hour, nil)
			if err != nil {
				return nil, err
			}
			return p, nil
		},
	)
	handler := NewHandlerWithReconciliation(nil, nil, nil, manager, nil, engine)
	return handler, manager
}

func tokenFor(t *testing.T, manager *auth.JWTManager, role string) string {
	t.Helper()
	tok, err := manager.Issue("11111111-1111-4111-8111-111111111111", role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// --- authorization tests ---

func TestReconciliationEndpointsRejectPublicRoles(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)

	now := time.Now().UTC()
	validBody, _ := json.Marshal(map[string]string{
		"participantId": "BANK-A",
		"scopeFrom":     now.Add(-time.Hour).Format(time.RFC3339),
		"scopeTo":       now.Format(time.RFC3339),
	})

	endpoints := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/api/ops/reconciliation/runs", validBody},
		{http.MethodGet, "/api/ops/reconciliation/runs", nil},
		{http.MethodGet, "/api/ops/reconciliation/runs/" + uuid.New().String(), nil},
		{http.MethodGet, "/api/ops/reconciliation/runs/" + uuid.New().String() + "/discrepancies", nil},
	}

	// Unauthenticated → 401.
	for _, ep := range endpoints {
		t.Run("Unauthenticated_"+ep.method+"_"+ep.path, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if ep.body != nil {
				bodyReader = bytes.NewReader(ep.body)
			} else {
				bodyReader = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(ep.method, ep.path, bodyReader)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// CUSTOMER and MERCHANT → 403.
	for _, role := range []string{"CUSTOMER", "MERCHANT"} {
		token := tokenFor(t, manager, role)
		for _, ep := range endpoints {
			t.Run(role+"_"+ep.method+"_"+ep.path, func(t *testing.T) {
				var bodyReader *bytes.Reader
				if ep.body != nil {
					bodyReader = bytes.NewReader(ep.body)
				} else {
					bodyReader = bytes.NewReader(nil)
				}
				req := httptest.NewRequest(ep.method, ep.path, bodyReader)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("expected 403 for %s on %s %s, got %d", role, ep.method, ep.path, rec.Code)
				}
			})
		}
	}
}

func TestReconciliationCreateRunOpsAdmin(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]string{
		"participantId": "BANK-A",
		"scopeFrom":     now.Add(-time.Hour).Format(time.RFC3339),
		"scopeTo":       now.Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ops/reconciliation/runs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data["status"] != "COMPLETED" {
		t.Fatalf("expected status COMPLETED, got %v", resp.Data["status"])
	}
	if resp.Data["participantId"] != "BANK-A" {
		t.Fatalf("expected participantId BANK-A, got %v", resp.Data["participantId"])
	}
}

func TestReconciliationCreateRunInvalidParticipant(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]string{
		"participantId": "UNKNOWN-X",
		"scopeFrom":     now.Add(-time.Hour).Format(time.RFC3339),
		"scopeTo":       now.Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ops/reconciliation/runs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown participant, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReconciliationCreateRunMalformedBody(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	req := httptest.NewRequest(http.MethodPost, "/api/ops/reconciliation/runs", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestReconciliationCreateRunInvalidScopeSameTime(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]string{
		"participantId": "BANK-A",
		"scopeFrom":     now.Format(time.RFC3339),
		"scopeTo":       now.Format(time.RFC3339), // same as From → invalid
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ops/reconciliation/runs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for scopeTo == scopeFrom, got %d", rec.Code)
	}
}

func TestReconciliationListRuns(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	// Create a run first.
	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]string{
		"participantId": "BANK-A",
		"scopeFrom":     now.Add(-time.Hour).Format(time.RFC3339),
		"scopeTo":       now.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/ops/reconciliation/runs", bytes.NewReader(body))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup: create run failed: %d %s", createRec.Code, createRec.Body.String())
	}

	// Now list.
	req := httptest.NewRequest(http.MethodGet, "/api/ops/reconciliation/runs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items, _ := resp.Data["items"].([]any)
	if len(items) == 0 {
		t.Fatal("expected at least one run in list")
	}
}

func TestReconciliationGetRunNotFound(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	req := httptest.NewRequest(http.MethodGet, "/api/ops/reconciliation/runs/"+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent run, got %d", rec.Code)
	}
}

func TestReconciliationGetRunBadUUID(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	req := httptest.NewRequest(http.MethodGet, "/api/ops/reconciliation/runs/not-a-uuid", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad UUID, got %d", rec.Code)
	}
}

func TestReconciliationGetRunOpsAdmin(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]string{
		"participantId": "BANK-A",
		"scopeFrom":     now.Add(-time.Hour).Format(time.RFC3339),
		"scopeTo":       now.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/ops/reconciliation/runs", bytes.NewReader(body))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create run: %d %s", createRec.Code, createRec.Body.String())
	}

	var createResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	runID := fmt.Sprintf("%v", createResp.Data["id"])

	req := httptest.NewRequest(http.MethodGet, "/api/ops/reconciliation/runs/"+runID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data["id"] != runID {
		t.Fatalf("expected run ID %s, got %v", runID, resp.Data["id"])
	}
}

func TestReconciliationListDiscrepanciesRunNotFound(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	req := httptest.NewRequest(http.MethodGet, "/api/ops/reconciliation/runs/"+uuid.New().String()+"/discrepancies", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown run discrepancies, got %d", rec.Code)
	}
}

func TestReconciliationListDiscrepanciesOpsAdmin(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	// Create a run.
	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]string{
		"participantId": "BANK-A",
		"scopeFrom":     now.Add(-time.Hour).Format(time.RFC3339),
		"scopeTo":       now.Format(time.RFC3339),
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/ops/reconciliation/runs", bytes.NewReader(body))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create run: %d %s", createRec.Code, createRec.Body.String())
	}

	var createResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	runID := fmt.Sprintf("%v", createResp.Data["id"])

	req := httptest.NewRequest(http.MethodGet, "/api/ops/reconciliation/runs/"+runID+"/discrepancies", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for discrepancies, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReconciliationResponseDoesNotExposeInternalFields(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	now := time.Now().UTC()
	body, _ := json.Marshal(map[string]string{
		"participantId": "BANK-A",
		"scopeFrom":     now.Add(-time.Hour).Format(time.RFC3339),
		"scopeTo":       now.Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/ops/reconciliation/runs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	rawBody := rec.Body.String()
	forbidden := []string{"database", "password", "credential", "pgx", "pgxpool", "secret"}
	for _, f := range forbidden {
		if strings.Contains(strings.ToLower(rawBody), f) {
			t.Errorf("response contains forbidden field %q: %s", f, rawBody)
		}
	}
}

func TestReconciliationBoundedListLimit(t *testing.T) {
	store := newMemReconStore()
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, store)
	token := tokenFor(t, manager, "OPS_ADMIN")

	// Request with an absurdly large limit should be capped.
	req := httptest.NewRequest(http.MethodGet, "/api/ops/reconciliation/runs?limit=99999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The handler must succeed without dumping the entire table.
}
