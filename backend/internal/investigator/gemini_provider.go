package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GeminiProviderConfig struct {
	APIKey, Model, Provider, ProjectID string
	Review                             GeminiReview
	DB                                 *pgxpool.Pool
	Client                             *http.Client
}
type GeminiProvider struct {
	key, model, provider, projectID string
	review                          GeminiReview
	admission                       geminiAdmission
	client                          *http.Client
	assets                          PromptAssets
}

func NewGeminiProvider(c GeminiProviderConfig) (*GeminiProvider, error) {
	if c.DB == nil || !c.Review.valid(c.APIKey, c.Provider, c.ProjectID, c.Model, time.Now()) {
		return nil, ErrFreeTierUnverified
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var canonical bool
	if err := c.DB.QueryRow(ctx, `SELECT singleton AND disabled IS NOT NULL FROM gemini_free_admission WHERE singleton`).Scan(&canonical); err != nil || !canonical {
		return nil, errors.New("Gemini canonical admission migration is required")
	}
	assets, err := LoadPromptAssets()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.Client != nil {
		client.Transport = c.Client.Transport
	}
	return &GeminiProvider{key: c.APIKey, model: c.Model, provider: c.Provider, projectID: c.ProjectID, review: c.Review, admission: geminiAdmission{c.DB}, client: client, assets: assets}, nil
}

// This stays true. Zero cost is accepted only through the explicit reviewed,
// configuration-bound free-project review plus durable admission, never a fake dollar budget.
func (p *GeminiProvider) RequiresCostReservation() bool { return true }
func (p *GeminiProvider) reviewedConfigurationReady() bool {
	return p.admission.pool != nil && p.review.valid(p.key, p.provider, p.projectID, p.model, time.Now())
}
func (p *GeminiProvider) Generate(ctx context.Context, r GenerationRequest) (result GenerationResult, retErr error) {
	if !p.reviewedConfigurationReady() {
		return result, ErrFreeTierUnverified
	}
	if r.PromptVersion != p.assets.Version || r.Evidence.PaymentId == "" || strings.TrimSpace(r.Question) == "" || r.InputTokenLimit < 1 || r.InputTokenLimit > 8000 || r.OutputTokenLimit < 1 || r.OutputTokenLimit > 1200 || len(r.Continuation) > 0 || len(r.ToolOutputs) > 0 {
		return result, ErrProviderMalformed
	}
	schema, bindings, err := geminiCitationSchema(p.assets.ReportSchema, r.Evidence)
	if err != nil {
		return result, err
	}
	packet := struct {
		Question         string                  `json:"question"`
		Evidence         EvidenceBundle          `json:"evidence"`
		Runbooks         []RunbookChunk          `json:"runbooks"`
		CitationBindings []geminiCitationBinding `json:"citationBindings"`
	}{r.Question, r.Evidence, r.Chunks, bindings}
	text, err := json.Marshal(packet)
	if err != nil || len(text) > 64<<10 {
		return result, ErrProviderMalformed
	}
	thinking := map[string]any{"thinkingLevel": "minimal"}
	if p.model == "gemini-2.5-flash-lite" {
		thinking = map[string]any{"thinkingBudget": 0}
	}
	content := map[string]any{"model": "models/" + p.model, "systemInstruction": map[string]any{"parts": []any{map[string]any{"text": p.assets.Prompt}}}, "contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": string(text)}}}}, "generationConfig": map[string]any{"maxOutputTokens": r.OutputTokenLimit, "candidateCount": 1, "responseMimeType": "application/json", "responseJsonSchema": schema, "thinkingConfig": thinking}}
	countBody, err := json.Marshal(map[string]any{"generateContentRequest": content})
	if err != nil {
		return result, ErrProviderMalformed
	}
	owner := uuid.New()
	releaseSafe := true
	defer func() {
		if releaseSafe {
			if err := p.admission.release(owner); err != nil {
				result = GenerationResult{}
				retErr = ErrProviderUsageUncertain
			}
		}
	}()
	raw, id, ended, err := p.call(ctx, owner, "countTokens", countBody)
	releaseSafe = ended
	if err != nil {
		return result, err
	}
	var count struct {
		TotalTokens *int64 `json:"totalTokens"`
	}
	countErr := json.Unmarshal(raw, &count)
	if countErr != nil || count.TotalTokens == nil || *count.TotalTokens < 1 {
		countErr = ErrProviderMalformed
	}
	usage := Usage{InputTokens: count.TotalTokens}
	if countErr != nil {
		usage = Usage{}
	}
	if err := p.admission.finish(id, finishStatus(countErr), "", usage); err != nil {
		releaseSafe = false
		return result, ErrProviderUsageUncertain
	}
	if countErr != nil {
		return result, countErr
	}
	if *count.TotalTokens > int64(r.InputTokenLimit) {
		return result, ErrProviderMalformed
	}
	if !p.reviewedConfigurationReady() {
		return result, ErrFreeTierUnverified
	}
	body, err := json.Marshal(content)
	if err != nil {
		return result, ErrProviderMalformed
	}
	raw, id, ended, err = p.call(ctx, owner, "generateContent", body)
	releaseSafe = ended
	if err != nil {
		return result, err
	}
	result, err = decodeGeminiResponse(raw, p.model, int64(r.InputTokenLimit), int64(r.OutputTokenLimit))
	if finishErr := p.admission.finish(id, finishStatus(err), result.ProviderRequestID, result.Usage); finishErr != nil {
		releaseSafe = false
		return GenerationResult{}, ErrProviderUsageUncertain
	}
	return result, err
}
func finishStatus(err error) string {
	if err != nil {
		return "failed"
	}
	return "completed"
}
func (p *GeminiProvider) call(ctx context.Context, owner uuid.UUID, kind string, body []byte) ([]byte, uuid.UUID, bool, error) {
	id, err := p.admission.reserve(ctx, owner, kind, p.model)
	if err != nil {
		return nil, uuid.Nil, true, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://generativelanguage.googleapis.com/v1beta/models/"+p.model+":"+kind, bytes.NewReader(body))
	if err != nil {
		if p.admission.finish(id, "failed", "", Usage{}) != nil {
			return nil, id, false, ErrProviderUsageUncertain
		}
		return nil, id, true, ErrProviderUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.key)
	response, err := p.client.Do(req)
	if err != nil {
		if p.admission.finish(id, "uncertain", "", Usage{}) != nil {
			return nil, id, false, ErrProviderUsageUncertain
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, id, false, context.DeadlineExceeded
		}
		return nil, id, false, ErrProviderUnavailable
	}
	defer response.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if response.StatusCode < 200 || response.StatusCode >= 300 || readErr != nil || len(raw) > maxProviderResponseBytes {
		failure := ErrProviderUnavailable
		if response.StatusCode == 429 {
			failure = ErrProviderRateLimited
		} else if readErr != nil || len(raw) > maxProviderResponseBytes {
			failure = ErrProviderMalformed
		}
		status := "failed"
		if readErr != nil {
			status = "uncertain"
		}
		if p.admission.finish(id, status, "", Usage{}) != nil {
			return nil, id, false, ErrProviderUsageUncertain
		}
		return nil, id, readErr == nil, failure
	}
	return raw, id, true, nil
}
func decodeGeminiResponse(raw []byte, model string, inputLimit, outputLimit int64) (GenerationResult, error) {
	var response struct {
		ID         string `json:"responseId"`
		Model      string `json:"modelVersion"`
		Candidates []struct {
			Finish  string `json:"finishReason"`
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Usage struct {
			Input    *int64 `json:"promptTokenCount"`
			Output   *int64 `json:"candidatesTokenCount"`
			Thoughts *int64 `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return GenerationResult{}, malformedGemini("envelope_json")
	}
	result := GenerationResult{ProviderRequestID: response.ID, ProviderModel: response.Model, Usage: Usage{InputTokens: response.Usage.Input, OutputTokens: response.Usage.Output}}
	if (response.Usage.Input != nil && *response.Usage.Input < 0) || (response.Usage.Output != nil && *response.Usage.Output < 0) {
		return result, malformedGemini("usage_negative")
	}
	if response.Usage.Thoughts != nil {
		if *response.Usage.Thoughts < 0 {
			return result, malformedGemini("usage_negative")
		}
		if result.Usage.OutputTokens != nil {
			total := *result.Usage.OutputTokens + *response.Usage.Thoughts
			result.Usage.OutputTokens = &total
		}
	}
	if result.ProviderRequestID == "" || len(result.ProviderRequestID) > 256 || !(response.Model == model || strings.HasPrefix(response.Model, model+"-")) || len(response.Candidates) != 1 {
		return result, malformedGemini("envelope_identity_or_candidates")
	}
	if response.Candidates[0].Finish == "SAFETY" || response.Candidates[0].Finish == "RECITATION" {
		return result, ErrProviderRefusal
	}
	if response.Candidates[0].Finish != "STOP" {
		return result, malformedGemini(geminiFinishFailure(response.Candidates[0].Finish))
	}
	if (result.Usage.InputTokens != nil && (*result.Usage.InputTokens < 0 || *result.Usage.InputTokens > inputLimit)) || (result.Usage.OutputTokens != nil && (*result.Usage.OutputTokens < 0 || *result.Usage.OutputTokens > outputLimit)) {
		return result, malformedGemini("usage_limit")
	}
	output := ""
	for _, part := range response.Candidates[0].Content.Parts {
		if part.Thought {
			continue
		}
		if part.Text != "" {
			if output != "" {
				return result, malformedGemini("multiple_text_parts")
			}
			output = part.Text
		}
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.DisallowUnknownFields()
	var report InvestigationReport
	if decoder.Decode(&report) != nil {
		return result, malformedGemini("report_json_or_type")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return result, malformedGemini("report_trailing_json")
	}
	if reason := validateGeminiReportShape([]byte(output)); reason != "" {
		return result, malformedGemini(reason)
	}
	result.Report = &report
	return result, nil
}
