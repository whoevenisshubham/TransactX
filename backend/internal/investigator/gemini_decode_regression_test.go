package investigator

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const completeGeminiReport = `{"schemaVersion":"tx.investigation.v1","mode":"llm","summary":"state","facts":[],"inferences":[],"recommendations":[],"missingEvidence":[],"evidenceCoverage":"limited","snapshotId":"fixture","collectedAt":"2026-10-19T00:00:00Z"}`

func geminiEnvelope(t *testing.T, text, finish string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"responseId": "fixture-response", "modelVersion": "gemini-3.5-flash-lite", "candidates": []any{map[string]any{"finishReason": finish, "content": map[string]any{"parts": []any{map[string]any{"text": text}}}}}, "usageMetadata": map[string]any{"promptTokenCount": 4979, "candidatesTokenCount": 584}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGeminiDecoderFailureBoundaries(t *testing.T) {
	for _, c := range []struct{ name, text, finish, want string }{
		{"complete", completeGeminiReport, "STOP", "valid"},
		{"truncated_finish", completeGeminiReport, "MAX_TOKENS", "finish_not_stop"},
		{"truncated_json", `{"schemaVersion":`, "STOP", "report_json_or_type"},
		{"missing_required", `{}`, "STOP", "report_required_fields"},
		{"null_arrays", strings.Replace(completeGeminiReport, `"facts":[]`, `"facts":null`, 1), "STOP", "report_schema_type"},
		{"wrong_enum", strings.Replace(completeGeminiReport, `"mode":"llm"`, `"mode":"evidence_only"`, 1), "STOP", "report_schema_enum"},
		{"unknown_field", strings.TrimSuffix(completeGeminiReport, "}") + `,"injected":"private"}`, "STOP", "report_json_or_type"},
		{"trailing", completeGeminiReport + `{}`, "STOP", "report_trailing_json"},
	} {
		t.Run(c.name, func(t *testing.T) {
			raw := geminiEnvelope(t, c.text, c.finish)
			if got := DiagnoseGeminiResponse(raw, "gemini-3.5-flash-lite", 8000, 600); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
			if c.want != "valid" {
				if _, err := decodeGeminiResponse(raw, "gemini-3.5-flash-lite", 8000, 600); !errors.Is(err, ErrProviderMalformed) {
					t.Fatal("public failure classification changed")
				}
			}
		})
	}
	var report map[string]any
	if json.Unmarshal([]byte(completeGeminiReport), &report) != nil {
		t.Fatal("fixture")
	}
	for field := range report {
		t.Run("omit_"+field, func(t *testing.T) {
			var copy map[string]any
			json.Unmarshal([]byte(completeGeminiReport), &copy)
			delete(copy, field)
			raw, _ := json.Marshal(copy)
			if got := DiagnoseGeminiResponse(geminiEnvelope(t, string(raw), "STOP"), "gemini-3.5-flash-lite", 8000, 600); got != "report_required_fields" {
				t.Fatalf("omitted required %s: %s", field, got)
			}
		})
	}
}

func TestGeminiDecodedCitationsStillRequireGrounding(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	base, err := BuildEvidenceOnlyReport(fixture.Evidence, nil, "state")
	if err != nil {
		t.Fatal(err)
	}
	base.Mode = ModeLLM
	base.Facts = base.Facts[:1]
	base.Inferences = []Inference{}
	base.Recommendations = []Recommendation{}
	base.MissingEvidence = []string{}
	for _, c := range []string{"valid", "foreign_citation", "wrong_assertion", "stale_snapshot", "missing_assertions", "missing_nested_field", "null_entity", "foreign_runbook", "evidence_id_as_entity"} {
		t.Run(c, func(t *testing.T) {
			encoded, _ := json.Marshal(base)
			var report InvestigationReport
			json.Unmarshal(encoded, &report)
			switch c {
			case "foreign_citation":
				report.Facts[0].EvidenceIds = []string{"foreign"}
			case "wrong_assertion":
				report.Facts[0].Assertions[0].Value = "invented"
			case "stale_snapshot":
				report.SnapshotId = "other"
			case "missing_assertions":
				report.Facts[0].Assertions = []Assertion{}
			case "null_entity":
				report.Facts[0].Assertions[0].EntityId = nil
			case "evidence_id_as_entity":
				report.Facts[0].Assertions[0].EntityId = &report.Facts[0].Assertions[0].EvidenceId
			case "foreign_runbook":
				report.Recommendations = []Recommendation{{Text: "Review", RunbookChunkIds: []string{"foreign"}}}
			}
			encoded, _ = json.Marshal(report)
			if c == "missing_nested_field" {
				var shape map[string]any
				json.Unmarshal(encoded, &shape)
				delete(shape["facts"].([]any)[0].(map[string]any), "assertions")
				encoded, _ = json.Marshal(shape)
			}
			result, err := decodeGeminiResponse(geminiEnvelope(t, string(encoded), "STOP"), "gemini-3.5-flash-lite", 8000, 600)
			if c == "missing_nested_field" {
				if !errors.Is(err, ErrProviderMalformed) {
					t.Fatal("nested required field accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = GroundGeneratedReport(*result.Report, fixture.Evidence, nil, "state")
			if (err == nil) != (c == "valid") {
				t.Fatalf("grounding accepted=%t case=%s", err == nil, c)
			}
		})
	}
}
