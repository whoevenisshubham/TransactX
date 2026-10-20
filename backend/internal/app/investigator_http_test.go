package app

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/investigator"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type admissionOnlyEvidence struct{}

func (admissionOnlyEvidence) ReadPaymentEvidence(context.Context, investigator.TrustedScope, string) (investigator.EvidenceBundle, error) {
	panic("HTTP admission must not execute tools or call a provider")
}

// Exercise cmd/api composition: a configured worker does not prove HTTP admission.
func TestHTTPInvestigationUsesConfiguredService(t *testing.T) {
	for _, mode := range []string{investigator.ModeEvidenceOnly, investigator.ModeLLM} {
		t.Run(mode, func(t *testing.T) {
			cfg := investigator.DefaultServiceConfig()
			cfg.Mode = mode
			if mode == investigator.ModeLLM {
				cfg.MaxProviderRounds = 1
				cfg.UnavailableReason = "provider_free_tier_unverified"
			}
			svc, err := investigator.NewService(cfg, investigator.NewMemoryInvestigationStore(time.Now), investigator.ToolDependencies{Evidence: admissionOnlyEvidence{}, Runbooks: investigator.NewMemoryCorpus()})
			if err != nil {
				t.Fatal(err)
			}
			jwt, err := auth.NewJWTManager(strings.Repeat("x", 32), "http-composition-test", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			token, err := jwt.Issue(uuid.NewString(), "OPS_ADMIN", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			env := &Environment{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), JWTManager: jwt, InvestigatorService: svc}
			handler := env.NewHTTPHandler()
			req := httptest.NewRequest("POST", "/api/ops/investigations", strings.NewReader(`{"paymentId":"`+uuid.NewString()+`","question":"What did each bank report?"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Idempotency-Key", uuid.NewString())
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != 202 {
				t.Fatalf("HTTP admission: %d %s", response.Code, response.Body.String())
			}
			var accepted struct {
				Data struct {
					Mode string `json:"mode"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil {
				t.Fatal(err)
			}
			if accepted.Data.Mode != mode {
				t.Fatalf("HTTP discarded configured mode: got %q want %q", accepted.Data.Mode, mode)
			}
			req = httptest.NewRequest("GET", "/api/ops/runtime", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			var runtime struct {
				Data struct {
					Mode  string `json:"investigatorMode"`
					Model string `json:"modelStatus"`
				} `json:"data"`
			}
			if response.Code != 200 {
				t.Fatalf("runtime status: %d", response.Code)
			}
			if err := json.Unmarshal(response.Body.Bytes(), &runtime); err != nil {
				t.Fatal(err)
			}
			expectedModel := "disabled"
			if mode == investigator.ModeLLM {
				expectedModel = "unavailable"
			}
			if runtime.Data.Mode != mode || runtime.Data.Model != expectedModel {
				t.Fatalf("misleading runtime status: %+v", runtime.Data)
			}
		})
	}
}
