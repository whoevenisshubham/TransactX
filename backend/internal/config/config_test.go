package config

import (
	"testing"
)

func TestParseExecutionTargetConfig_ValidDelimited(t *testing.T) {
	raw := "direct:RAIL-A:BANK-A:BANK-B:http://localhost:8081;rail-b:RAIL-B:BANK-A:BANK-B:http://localhost:8082"
	targets, err := ParseExecutionTargetConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}
	if targets[0].CandidateID != "direct" || targets[0].ExecutionTargetID != "RAIL-A" || targets[0].SourceBank != "BANK-A" || targets[0].DestinationBank != "BANK-B" || targets[0].Endpoint != "http://localhost:8081" {
		t.Fatalf("target[0] mismatch: %+v", targets[0])
	}
	if targets[1].CandidateID != "rail-b" || targets[1].ExecutionTargetID != "RAIL-B" || targets[1].SourceBank != "BANK-A" || targets[1].DestinationBank != "BANK-B" || targets[1].Endpoint != "http://localhost:8082" {
		t.Fatalf("target[1] mismatch: %+v", targets[1])
	}

	// Test pipe-separated format with explicit source and destination endpoints
	pipeRaw := "direct|RAIL-A|BANK-A|BANK-B|http://localhost:8080|http://localhost:8081|http://localhost:8082"
	pipeTargets, err := ParseExecutionTargetConfig(pipeRaw)
	if err != nil {
		t.Fatalf("unexpected error with pipe format: %v", err)
	}
	if len(pipeTargets) != 1 {
		t.Fatalf("got %d targets, want 1", len(pipeTargets))
	}
	pt := pipeTargets[0]
	if pt.CandidateID != "direct" || pt.ExecutionTargetID != "RAIL-A" || pt.SourceBank != "BANK-A" || pt.DestinationBank != "BANK-B" || pt.Endpoint != "http://localhost:8080" || pt.SourceEndpoint != "http://localhost:8081" || pt.DestinationEndpoint != "http://localhost:8082" {
		t.Fatalf("pipe target mismatch: %+v", pt)
	}
}

func TestParseExecutionTargetConfig_ValidJSON(t *testing.T) {
	raw := `[
		{
			"candidateId": "direct",
			"executionTargetId": "RAIL-A",
			"sourceBank": "BANK-A",
			"destinationBank": "BANK-B",
			"endpoint": "http://localhost:8081"
		},
		{
			"candidate_id": "rail-b",
			"execution_target_id": "RAIL-B",
			"source_bank": "BANK-A",
			"destination_bank": "BANK-B",
			"endpoint": "http://localhost:8082"
		}
	]`
	targets, err := ParseExecutionTargetConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}
	if targets[0].CandidateID != "direct" || targets[0].ExecutionTargetID != "RAIL-A" {
		t.Fatalf("target[0] mismatch: %+v", targets[0])
	}
	if targets[1].CandidateID != "rail-b" || targets[1].ExecutionTargetID != "RAIL-B" {
		t.Fatalf("target[1] mismatch: %+v", targets[1])
	}
}

func TestParseExecutionTargetConfig_EmptyReturnsNil(t *testing.T) {
	targets, err := ParseExecutionTargetConfig("")
	if err != nil || targets != nil {
		t.Fatalf("targets=%v, err=%v", targets, err)
	}
	targets, err = ParseExecutionTargetConfig("   ")
	if err != nil || targets != nil {
		t.Fatalf("targets=%v, err=%v", targets, err)
	}
}

func TestParseExecutionTargetConfig_Malformed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"too few fields", "direct:RAIL-A:BANK-A"},
		{"empty candidate ID", ":RAIL-A:BANK-A:BANK-B:http://localhost:8081"},
		{"empty target ID", "direct::BANK-A:BANK-B:http://localhost:8081"},
		{"empty source bank", "direct:RAIL-A::BANK-B:http://localhost:8081"},
		{"empty destination bank", "direct:RAIL-A:BANK-A::http://localhost:8081"},
		{"missing endpoint", "direct:RAIL-A:BANK-A:BANK-B:"},
		{"invalid endpoint URL", "direct:RAIL-A:BANK-A:BANK-B:not-a-valid-url"},
		{"pipe too few fields", "direct|RAIL-A|BANK-A"},
		{"json missing candidate", `[{"executionTargetId":"RAIL-A","sourceBank":"BANK-A","destinationBank":"BANK-B","endpoint":"http://localhost:8081"}]`},
		{"json empty array", `[]`},
		{"json invalid syntax", `[{"candidateId":}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseExecutionTargetConfig(tc.raw)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
