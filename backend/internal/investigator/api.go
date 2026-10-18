package investigator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const MaxCreateInvestigationRequestBytes int64 = 32 * 1024

var ErrInvalidCreateInvestigationRequest = errors.New("invalid investigation create request")

// CreateInvestigationRequest is the body M2 decodes after common auth, body
// size, unknown-field and idempotency middleware have run. TrustedScope and
// provider selection are deliberately absent.
type CreateInvestigationRequest struct {
	PaymentId string `json:"paymentId"`
	Question  string `json:"question"`
}

// DecodeCreateInvestigationRequest is the bounded HTTP composition seam. It
// accepts exactly one JSON object with only the public create fields; trusted
// actor, role, payment scope, mode and provider configuration stay server-owned.
func DecodeCreateInvestigationRequest(reader io.Reader) (CreateInvestigationRequest, error) {
	if reader == nil {
		return CreateInvestigationRequest{}, ErrInvalidCreateInvestigationRequest
	}
	raw, err := io.ReadAll(io.LimitReader(reader, MaxCreateInvestigationRequestBytes+1))
	if err != nil {
		return CreateInvestigationRequest{}, fmt.Errorf("%w: read body", ErrInvalidCreateInvestigationRequest)
	}
	if int64(len(raw)) > MaxCreateInvestigationRequestBytes {
		return CreateInvestigationRequest{}, fmt.Errorf("%w: body exceeds %d bytes", ErrInvalidCreateInvestigationRequest, MaxCreateInvestigationRequestBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request CreateInvestigationRequest
	if err := decoder.Decode(&request); err != nil {
		return CreateInvestigationRequest{}, fmt.Errorf("%w: malformed body", ErrInvalidCreateInvestigationRequest)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CreateInvestigationRequest{}, fmt.Errorf("%w: trailing JSON", ErrInvalidCreateInvestigationRequest)
	}
	return request, nil
}

type CreateInvestigationResponse struct {
	InvestigationId string `json:"investigationId"`
	Status          string `json:"status"`
	Mode            string `json:"mode"`
}

type InvestigationResponse struct {
	Id           string               `json:"id"`
	Status       string               `json:"status"`
	Mode         string               `json:"mode"`
	Report       *InvestigationReport `json:"report"`
	Snapshot     *EvidenceBundle      `json:"snapshot,omitempty"`
	ToolTimeline []ToolTimelineEntry  `json:"toolTimeline"`
	Error        *InvestigationError  `json:"error"`
	CreatedAt    string               `json:"createdAt"`
	CompletedAt  *string              `json:"completedAt"`
	Usage        InvestigationUsage   `json:"usage"`
}

type InvestigationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type InvestigationUsage struct {
	InputTokens   *int64  `json:"inputTokens"`
	OutputTokens  *int64  `json:"outputTokens"`
	Model         *string `json:"model"`
	PromptVersion string  `json:"promptVersion"`
}

func CreateResponse(job Job) CreateInvestigationResponse {
	return CreateInvestigationResponse{InvestigationId: job.ID, Status: job.Status, Mode: job.Mode}
}

func PublicInvestigation(job Job) InvestigationResponse {
	usage := InvestigationUsage{InputTokens: job.Usage.InputTokens, OutputTokens: job.Usage.OutputTokens, PromptVersion: job.PromptVersion}
	if job.ProviderModel != "" {
		model := job.ProviderModel
		usage.Model = &model
	}
	timeline := append([]ToolTimelineEntry(nil), job.ToolTimeline...)
	for index := range timeline {
		if len(timeline[index].Arguments) == 0 {
			timeline[index].Arguments = []byte(`{}`)
		}
		if timeline[index].EvidenceIds == nil {
			timeline[index].EvidenceIds = []string{}
		}
	}
	response := InvestigationResponse{
		Id: job.ID, Status: job.Status, Mode: job.Mode, Report: job.Report,
		Snapshot:     job.Snapshot,
		ToolTimeline: timeline,
		CreatedAt:    job.CreatedAt.UTC().Format(time.RFC3339Nano), Usage: usage,
	}
	if response.ToolTimeline == nil {
		response.ToolTimeline = []ToolTimelineEntry{}
	}
	if !job.CompletedAt.IsZero() {
		value := job.CompletedAt.UTC().Format(time.RFC3339Nano)
		response.CompletedAt = &value
	}
	if job.ErrorCode != "" {
		response.Error = &InvestigationError{Code: job.ErrorCode, Message: publicInvestigationErrorMessage(job.ErrorCode)}
	}
	return response
}

func publicInvestigationErrorMessage(code string) string {
	switch code {
	case "provider_free_tier_unverified":
		return "The Gemini free-project review is missing or expired; deterministic evidence is retained when present."
	case "budget_exhausted":
		return "Generation budget was unavailable; deterministic evidence is retained when present."
	case "provider_disabled", "provider_timeout", "provider_rate_limited", "provider_refused", "provider_malformed", "provider_round_limit", "provider_unavailable", "provider_usage_uncertain", "usage_commit_failed":
		return "Provider generation was unavailable; deterministic evidence is retained when present."
	case "grounding_rejected", "provider_tool_rejected":
		return "Generated output failed safety validation; deterministic evidence is retained when present."
	case "deadline_exceeded":
		return "The investigation deadline expired before completion."
	case "source_unavailable":
		return "Required evidence was unavailable."
	case "tool_attempts_exhausted":
		return "Bounded evidence collection attempts were exhausted."
	default:
		return "The investigation could not complete safely."
	}
}
