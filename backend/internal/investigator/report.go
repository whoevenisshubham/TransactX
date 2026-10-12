package investigator

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func BuildEvidenceOnlyReport(evidence EvidenceBundle, chunks []RunbookChunk, question string) (InvestigationReport, error) {
	scope := TrustedScope{ActorID: "report-builder", Role: "OPS_ADMIN", SelectedPaymentID: evidence.PaymentId}
	if err := validateEvidenceBundle(scope, evidence.PaymentId, evidence); err != nil {
		return InvestigationReport{}, err
	}

	entityID := evidence.PaymentId
	report := InvestigationReport{
		SchemaVersion: "tx.investigation.v1",
		Mode:          ModeEvidenceOnly,
		Summary:       evidenceSummary(evidence),
		SnapshotId:    evidence.SnapshotId,
		CollectedAt:   evidence.CollectedAt,
		Facts: []Fact{
			{
				Text:        fmt.Sprintf("The central payment is %s.", evidence.Payment.State),
				EvidenceIds: []string{evidence.Payment.EvidenceId},
				Assertions: []Assertion{{
					Field: "central_state", Value: string(evidence.Payment.State), EntityId: &entityID, EvidenceId: evidence.Payment.EvidenceId,
				}},
			},
			{
				Text:        fmt.Sprintf("The selected payment amount is %s %s in integer paise.", evidence.Payment.AmountPaise, evidence.Payment.Currency),
				EvidenceIds: []string{evidence.Payment.EvidenceId},
				Assertions: []Assertion{{
					Field: "amount_paise", Value: evidence.Payment.AmountPaise, EntityId: &entityID, EvidenceId: evidence.Payment.EvidenceId,
				}},
			},
		},
	}

	for _, operation := range evidence.Operations {
		if operation.SourceStatus != "available" || operation.ParticipantStatus == nil {
			continue
		}
		operationID := operation.OperationId
		report.Facts = append(report.Facts, Fact{
			Text:        fmt.Sprintf("Participant %s reports operation %s as %s.", operation.BankCode, operation.OperationType, *operation.ParticipantStatus),
			EvidenceIds: []string{operation.EvidenceId},
			Assertions: []Assertion{
				{Field: "operation_id", Value: operation.OperationId, EntityId: &operationID, EvidenceId: operation.EvidenceId},
				{Field: "participant_status", Value: *operation.ParticipantStatus, EntityId: &operationID, EvidenceId: operation.EvidenceId},
				{Field: "bank_code", Value: operation.BankCode, EntityId: &operationID, EvidenceId: operation.EvidenceId},
			},
		})
	}

	report.MissingEvidence, report.EvidenceCoverage = missingEvidenceForQuestion(evidence, question)
	for _, chunk := range chunks {
		if len(report.Recommendations) == 3 {
			break
		}
		report.Recommendations = append(report.Recommendations, Recommendation{
			Text:            fmt.Sprintf("Review the approved %q runbook section; it is %s guidance, not financial evidence.", chunk.Heading, chunk.ImplementationStatus),
			RunbookChunkIds: []string{chunk.ID},
		})
	}
	if report.Facts == nil {
		report.Facts = []Fact{}
	}
	if report.Inferences == nil {
		report.Inferences = []Inference{}
	}
	if report.Recommendations == nil {
		report.Recommendations = []Recommendation{}
	}
	if report.MissingEvidence == nil {
		report.MissingEvidence = []string{}
	}
	if err := ValidateReportGrounding(report, evidence, chunks); err != nil {
		return InvestigationReport{}, err
	}
	return report, nil
}

func evidenceSummary(evidence EvidenceBundle) string {
	switch evidence.Payment.State {
	case "COMPLETED":
		return "The selected snapshot records the central payment as completed. Participant observations are listed separately."
	case "BANK_SETTLED_CENTRAL_PENDING":
		return "The selected snapshot records participant settlement with final central completion still pending."
	case "PENDING_RECONCILIATION":
		return "The central payment remains pending reconciliation; unavailable or unknown participant evidence is not treated as failure."
	case "FAILED", "REVERSED":
		return fmt.Sprintf("The selected snapshot records the central payment as %s; the cited evidence below remains authoritative.", evidence.Payment.State)
	default:
		return fmt.Sprintf("The selected snapshot records the central payment as %s; no unobserved outcome is inferred.", evidence.Payment.State)
	}
}

func missingEvidenceForQuestion(evidence EvidenceBundle, question string) ([]string, string) {
	missing := make(map[string]struct{})
	for _, operation := range evidence.Operations {
		if operation.SourceStatus != "available" || operation.ParticipantStatus == nil || operation.Presence == "unknown" || operation.Presence == "not_checked" {
			missing["participant_operation_status:"+operation.OperationId] = struct{}{}
		}
	}
	for _, source := range evidence.Sources {
		if source.Status == "unavailable" {
			missing["source:"+source.Name] = struct{}{}
		}
	}
	lowerQuestion := strings.ToLower(question)
	if strings.Contains(lowerQuestion, "trace") && evidence.Traces.SourceStatus != "available" {
		missing["trace_context"] = struct{}{}
	}
	if strings.Contains(lowerQuestion, "reconcil") && evidence.Reconciliation.SourceStatus != "available" {
		missing["reconciliation_context"] = struct{}{}
	}
	if strings.Contains(lowerQuestion, "integrity") && evidence.Integrity.SourceStatus != "available" {
		missing["integrity_context"] = struct{}{}
	}
	result := make([]string, 0, len(missing))
	for value := range missing {
		result = append(result, value)
	}
	sort.Strings(result)
	coverage := "complete_for_question"
	if len(result) > 0 {
		coverage = "limited"
	}
	if evidence.Payment.EvidenceId == "" {
		coverage = "insufficient"
	}
	return result, coverage
}

func ValidateReportGrounding(report InvestigationReport, evidence EvidenceBundle, chunks []RunbookChunk) error {
	if report.SchemaVersion != "tx.investigation.v1" || (report.Mode != ModeEvidenceOnly && report.Mode != ModeLLM) || report.SnapshotId != evidence.SnapshotId || report.CollectedAt != evidence.CollectedAt {
		return errors.New("report envelope does not match evidence snapshot")
	}
	evidenceRegistry := make(map[string]map[string]string)
	evidenceRegistry[evidence.Payment.EvidenceId] = map[string]string{
		"central_state": string(evidence.Payment.State),
		"amount_paise":  evidence.Payment.AmountPaise,
	}
	entityRegistry := map[string]string{evidence.Payment.EvidenceId: evidence.PaymentId}
	for _, operation := range evidence.Operations {
		values := map[string]string{"operation_id": operation.OperationId, "bank_code": operation.BankCode}
		if operation.ParticipantStatus != nil && operation.SourceStatus == "available" {
			values["participant_status"] = *operation.ParticipantStatus
		}
		evidenceRegistry[operation.EvidenceId] = values
		entityRegistry[operation.EvidenceId] = operation.OperationId
	}
	for _, fact := range report.Facts {
		if len(fact.EvidenceIds) == 0 || len(fact.Assertions) == 0 {
			return errors.New("published fact lacks typed citations")
		}
		for _, evidenceID := range fact.EvidenceIds {
			if _, exists := evidenceRegistry[evidenceID]; !exists {
				return fmt.Errorf("fact cites unknown evidence %q", evidenceID)
			}
		}
		for _, assertion := range fact.Assertions {
			values, exists := evidenceRegistry[assertion.EvidenceId]
			if !exists || values[assertion.Field] != assertion.Value {
				return fmt.Errorf("assertion %s is not supported by %s", assertion.Field, assertion.EvidenceId)
			}
			if assertion.EntityId == nil || *assertion.EntityId != entityRegistry[assertion.EvidenceId] {
				return fmt.Errorf("assertion %s has the wrong entity", assertion.Field)
			}
		}
	}
	for _, inference := range report.Inferences {
		if strings.TrimSpace(inference.Text) == "" || len(inference.EvidenceIds) == 0 {
			return errors.New("inference lacks text or evidence citations")
		}
		for _, evidenceID := range inference.EvidenceIds {
			if _, exists := evidenceRegistry[evidenceID]; !exists {
				return fmt.Errorf("inference cites unknown evidence %q", evidenceID)
			}
		}
	}
	chunkRegistry := make(map[string]struct{}, len(chunks))
	for _, chunk := range chunks {
		chunkRegistry[chunk.ID] = struct{}{}
	}
	for _, recommendation := range report.Recommendations {
		if len(recommendation.RunbookChunkIds) == 0 {
			return errors.New("recommendation lacks a runbook citation")
		}
		for _, chunkID := range recommendation.RunbookChunkIds {
			if _, exists := chunkRegistry[chunkID]; !exists {
				return fmt.Errorf("recommendation cites unknown runbook chunk %q", chunkID)
			}
		}
	}
	switch report.EvidenceCoverage {
	case "complete_for_question", "limited", "insufficient":
	default:
		return errors.New("invalid evidence coverage")
	}
	return nil
}

// GroundGeneratedReport validates semantic citation meaning and replaces the
// model-controlled financial prose and recommendations with deterministic
// renderings. Free-form provider text can remain only in explicitly labelled
// inferences backed by known evidence IDs.
func GroundGeneratedReport(report InvestigationReport, evidence EvidenceBundle, chunks []RunbookChunk, question string) (InvestigationReport, error) {
	if report.Mode != ModeLLM || len(report.Facts) > 50 || len(report.Inferences) > 20 || len(report.Recommendations) > 10 {
		return InvestigationReport{}, errors.New("generated report exceeds structural bounds")
	}
	if err := ValidateReportGrounding(report, evidence, chunks); err != nil {
		return InvestigationReport{}, err
	}
	for index := range report.Facts {
		report.Facts[index].Text = renderAssertions(report.Facts[index].Assertions)
	}
	for index, inference := range report.Inferences {
		if len([]rune(inference.Text)) > 500 {
			return InvestigationReport{}, errors.New("generated inference exceeds length bound")
		}
		report.Inferences[index].Text = "Advisory inference: " + inference.Text
	}
	recommendations := make([]Recommendation, 0, len(report.Recommendations))
	chunkByID := make(map[string]RunbookChunk, len(chunks))
	for _, chunk := range chunks {
		chunkByID[chunk.ID] = chunk
	}
	seenRecommendations := make(map[string]struct{})
	for _, recommendation := range report.Recommendations {
		for _, chunkID := range recommendation.RunbookChunkIds {
			if _, duplicate := seenRecommendations[chunkID]; duplicate {
				continue
			}
			chunk := chunkByID[chunkID]
			recommendations = append(recommendations, Recommendation{
				Text:            fmt.Sprintf("Review the approved %q runbook section; it is %s guidance, not financial evidence.", chunk.Heading, chunk.ImplementationStatus),
				RunbookChunkIds: []string{chunkID},
			})
			seenRecommendations[chunkID] = struct{}{}
			if len(recommendations) == 3 {
				break
			}
		}
		if len(recommendations) == 3 {
			break
		}
	}
	report.Recommendations = recommendations
	report.Summary = evidenceSummary(evidence)
	report.MissingEvidence, report.EvidenceCoverage = missingEvidenceForQuestion(evidence, question)
	return report, nil
}

func renderAssertions(assertions []Assertion) string {
	if len(assertions) == 1 {
		switch assertions[0].Field {
		case "central_state":
			return fmt.Sprintf("The central payment is %s.", assertions[0].Value)
		case "amount_paise":
			return fmt.Sprintf("The selected payment amount is %s in integer paise.", assertions[0].Value)
		}
	}
	values := make([]string, len(assertions))
	for index, assertion := range assertions {
		values[index] = assertion.Field + "=" + assertion.Value
	}
	return "Verified typed evidence: " + strings.Join(values, ", ") + "."
}
