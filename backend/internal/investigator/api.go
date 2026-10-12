package investigator

import "time"

// CreateInvestigationRequest is the body M2 decodes after common auth, body
// size, unknown-field and idempotency middleware have run. TrustedScope and
// provider selection are deliberately absent.
type CreateInvestigationRequest struct {
	PaymentId string `json:"paymentId"`
	Question  string `json:"question"`
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
	ToolTimeline []ToolTimelineEntry  `json:"toolTimeline"`
	Error        *InvestigationError  `json:"error"`
	CreatedAt    string               `json:"createdAt"`
	CompletedAt  *string              `json:"completedAt"`
	Usage        Usage                `json:"usage"`
}

type InvestigationError struct {
	Code string `json:"code"`
}

func CreateResponse(job Job) CreateInvestigationResponse {
	return CreateInvestigationResponse{InvestigationId: job.ID, Status: job.Status, Mode: job.Mode}
}

func PublicInvestigation(job Job) InvestigationResponse {
	response := InvestigationResponse{
		Id: job.ID, Status: job.Status, Mode: job.Mode, Report: job.Report,
		ToolTimeline: append([]ToolTimelineEntry(nil), job.ToolTimeline...),
		CreatedAt:    job.CreatedAt.UTC().Format(time.RFC3339Nano), Usage: job.Usage,
	}
	if response.ToolTimeline == nil {
		response.ToolTimeline = []ToolTimelineEntry{}
	}
	if !job.CompletedAt.IsZero() {
		value := job.CompletedAt.UTC().Format(time.RFC3339Nano)
		response.CompletedAt = &value
	}
	if job.ErrorCode != "" {
		response.Error = &InvestigationError{Code: job.ErrorCode}
	}
	return response
}
