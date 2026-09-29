package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/auth"
)

func TestRoutingDistributionRequiresOpsAdmin(t *testing.T) {
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, newMemReconStore())
	for _, tc := range []struct {
		role   string
		status int
	}{
		{"", http.StatusUnauthorized},
		{"CUSTOMER", http.StatusForbidden},
		{"MERCHANT", http.StatusForbidden},
		{"OPS_ADMIN", http.StatusNotFound}, // No database in this isolated handler test.
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/routing/distribution", nil)
		if tc.role != "" {
			req.Header.Set("Authorization", "Bearer "+tokenFor(t, manager, tc.role))
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("role %s: got %d: %s", tc.role, res.Code, res.Body.String())
		}
	}
}

func TestRoutingDistributionPostgresPersistenceWindowAndTargets(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewJWTManager(strings.Repeat("d", 32), "routing-live-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool, nil, nil, manager)
	token := tokenFor(t, manager, "OPS_ADMIN")

	requestDistribution := func() (int, []struct {
		TargetID string `json:"targetId"`
		Payments int64  `json:"payments"`
	}) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/ops/routing/distribution", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		var body struct {
			Data struct {
				Items []struct {
					TargetID string `json:"targetId"`
					Payments int64  `json:"payments"`
				} `json:"items"`
				WindowHours int `json:"windowHours"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Data.WindowHours != 24 {
			t.Fatalf("windowHours = %d, want 24", body.Data.WindowHours)
		}
		return res.Code, body.Data.Items
	}

	// A fresh migrated database returns a real empty array, not a fabricated
	// zero-traffic target or healthy state.
	status, empty := requestDistribution()
	if status != http.StatusOK || len(empty) != 0 {
		t.Fatalf("fresh distribution = status %d items %+v, want 200 and []", status, empty)
	}

	suffix := uuid.NewString()
	userID, sourceBankID, destinationBankID := uuid.New(), uuid.New(), uuid.New()
	senderID, receiverID := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES ($1, 'Routing Test', $2, $3, 'hash', 'CUSTOMER')`,
		userID, "+91"+strings.ReplaceAll(suffix[:12], "-", ""), "routing-"+suffix)
	if err != nil {
		t.Fatalf("insert routing user: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Source', 'ACTIVE'), ($3, $4, 'Destination', 'ACTIVE')`,
		sourceBankID, "SRC"+suffix[:8], destinationBankID, "DST"+suffix[:8]); err != nil {
		t.Fatalf("insert routing banks: %v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES
		($1, $2, $3, $4, 10000, 'ACTIVE'), ($5, $2, $6, $7, 10000, 'ACTIVE')`,
		senderID, userID, sourceBankID, "sender-"+suffix, receiverID, destinationBankID, "receiver-"+suffix); err != nil {
		t.Fatalf("insert routing accounts: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM payment_route_decisions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM payments WHERE initiated_by_user_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE user_id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM banks WHERE id IN ($1,$2)`, sourceBankID, destinationBankID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	targetA, targetB := "RAIL-LIVE-A-"+suffix[:8], "RAIL-LIVE-B-"+suffix[:8]
	for index, fixture := range []struct {
		target string
		when   time.Time
	}{{targetA, time.Now().UTC()}, {targetA, time.Now().UTC()}, {targetB, time.Now().UTC()}, {targetA, time.Now().UTC().Add(-25 * time.Hour)}} {
		paymentID := uuid.New()
		_, err := pool.Exec(ctx, `INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, source_bank_id, destination_bank_id)
			VALUES ($1,$2,$3,$4,100,'INR','COMPLETED',$5,$6)`, paymentID, userID, senderID, receiverID, sourceBankID, destinationBankID)
		if err != nil {
			t.Fatalf("insert payment %d: %v", index, err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO payment_route_decisions (payment_id, candidate_id, source_bank_id, destination_bank_id, execution_target_id, selected_score, health_snapshot, reason_code, selection_mode, selected_at, event_type)
			VALUES ($1,$2,$3,$4,$5,0.9,'{}'::jsonb,'BEST_HEALTH_SCORE','ADAPTIVE',$6,'PAYMENT_ROUTED')`,
			paymentID, "candidate-"+suffix+string(rune('a'+index)), sourceBankID, destinationBankID, fixture.target, fixture.when)
		if err != nil {
			t.Fatalf("insert route decision %d: %v", index, err)
		}
	}

	status, items := requestDistribution()
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	counts := make(map[string]int64)
	for _, item := range items {
		counts[item.TargetID] = item.Payments
	}
	if counts[targetA] != 2 || counts[targetB] != 1 {
		t.Fatalf("persisted target counts = %+v, want A=2 B=1 and exclude 25-hour row", counts)
	}
}
