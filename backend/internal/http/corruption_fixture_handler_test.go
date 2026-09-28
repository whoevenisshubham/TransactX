package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLedgerCorruptionFixtureModeAndAuthorization(t *testing.T) {
	const path = "/api/ops/chaos/ledger-corruption-fixture"
	t.Setenv("TX_SIMULATION_MODE", "false")
	disabled, manager := makeReconHandler(t, []string{"BANK-A"}, newMemReconStore())
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+tokenFor(t, manager, "OPS_ADMIN"))
	res := httptest.NewRecorder()
	disabled.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("fixture must be absent outside simulation mode: %d", res.Code)
	}

	t.Setenv("TX_SIMULATION_MODE", "true")
	enabled, manager := makeReconHandler(t, []string{"BANK-A"}, newMemReconStore())
	for _, tc := range []struct {
		role   string
		status int
	}{
		{"", http.StatusUnauthorized}, {"CUSTOMER", http.StatusForbidden},
		{"MERCHANT", http.StatusForbidden}, {"OPS_ADMIN", http.StatusOK},
	} {
		t.Run(tc.role, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			if tc.role != "" {
				req.Header.Set("Authorization", "Bearer "+tokenFor(t, manager, tc.role))
			}
			res := httptest.NewRecorder()
			enabled.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("%s: got %d: %s", tc.role, res.Code, res.Body.String())
			}
			if tc.role == "OPS_ADMIN" {
				var body struct {
					Data struct {
						Mode                        string `json:"mode"`
						MerkleRootMismatch          bool   `json:"merkleRootMismatch"`
						ReconciliationDiscrepancies int    `json:"reconciliationDiscrepancies"`
						BaselineIntegrity           struct {
							Status string `json:"status"`
						} `json:"baselineIntegrity"`
						CorruptedIntegrity struct {
							Status string `json:"status"`
						} `json:"corruptedIntegrity"`
					} `json:"data"`
				}
				if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Data.Mode != "ISOLATED_SIMULATION" || !body.Data.MerkleRootMismatch || body.Data.ReconciliationDiscrepancies == 0 || body.Data.BaselineIntegrity.Status != "PASS" || body.Data.CorruptedIntegrity.Status != "FAIL" {
					t.Fatalf("corruption not detected by both checks: %+v", body.Data)
				}
			}
		})
	}
}
