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

func TestParseExecutionTargetConfig_HealthEndpointRules(t *testing.T) {
	// 1. Missing health endpoint -> rejected
	jsonMissingHealth := `[{"candidateId":"c1","executionTargetId":"t1","sourceBank":"BANK-A","destinationBank":"BANK-B","sourceEndpoint":"http://localhost:8081","destinationEndpoint":"http://localhost:8082"}]`
	if _, err := ParseExecutionTargetConfig(jsonMissingHealth); err == nil {
		t.Fatal("expected error for JSON missing health endpoint, got nil")
	}

	delimMissingHealth := "c1:t1:BANK-A:BANK-B:"
	if _, err := ParseExecutionTargetConfig(delimMissingHealth); err == nil {
		t.Fatal("expected error for delimited missing health endpoint, got nil")
	}

	pipeMissingHealth := "c1|t1|BANK-A|BANK-B||http://localhost:8081|http://localhost:8082"
	if _, err := ParseExecutionTargetConfig(pipeMissingHealth); err == nil {
		t.Fatal("expected error for pipe missing health endpoint, got nil")
	}

	// 2. Valid health endpoint -> accepted
	jsonValidHealth := `[{"candidateId":"c1","executionTargetId":"t1","sourceBank":"BANK-A","destinationBank":"BANK-B","endpoint":"http://localhost:8080"}]`
	targets, err := ParseExecutionTargetConfig(jsonValidHealth)
	if err != nil || len(targets) != 1 || targets[0].Endpoint != "http://localhost:8080" {
		t.Fatalf("expected valid JSON health endpoint accepted, got targets=%+v, err=%v", targets, err)
	}

	delimValidHealth := "c1:t1:BANK-A:BANK-B:http://localhost:8080"
	targets, err = ParseExecutionTargetConfig(delimValidHealth)
	if err != nil || len(targets) != 1 || targets[0].Endpoint != "http://localhost:8080" {
		t.Fatalf("expected valid delimited health endpoint accepted, got targets=%+v, err=%v", targets, err)
	}

	// 3. Source/destination endpoints + health endpoint -> accepted
	jsonFull := `[{"candidateId":"c1","executionTargetId":"t1","sourceBank":"BANK-A","destinationBank":"BANK-B","endpoint":"http://localhost:8080","sourceEndpoint":"http://localhost:8081","destinationEndpoint":"http://localhost:8082"}]`
	targets, err = ParseExecutionTargetConfig(jsonFull)
	if err != nil || len(targets) != 1 || targets[0].Endpoint != "http://localhost:8080" || targets[0].SourceEndpoint != "http://localhost:8081" || targets[0].DestinationEndpoint != "http://localhost:8082" {
		t.Fatalf("expected full JSON accepted, got targets=%+v, err=%v", targets, err)
	}

	pipeFull := "c1|t1|BANK-A|BANK-B|http://localhost:8080|http://localhost:8081|http://localhost:8082"
	targets, err = ParseExecutionTargetConfig(pipeFull)
	if err != nil || len(targets) != 1 || targets[0].Endpoint != "http://localhost:8080" || targets[0].SourceEndpoint != "http://localhost:8081" || targets[0].DestinationEndpoint != "http://localhost:8082" {
		t.Fatalf("expected full pipe delimited accepted, got targets=%+v, err=%v", targets, err)
	}

	// 4. Malformed health endpoint -> rejected
	jsonMalformedHealth := `[{"candidateId":"c1","executionTargetId":"t1","sourceBank":"BANK-A","destinationBank":"BANK-B","endpoint":"://bad-url"}]`
	if _, err := ParseExecutionTargetConfig(jsonMalformedHealth); err == nil {
		t.Fatal("expected error for malformed JSON health endpoint, got nil")
	}

	delimMalformedHealth := "c1:t1:BANK-A:BANK-B:not-a-valid-url"
	if _, err := ParseExecutionTargetConfig(delimMalformedHealth); err == nil {
		t.Fatal("expected error for malformed delimited health endpoint, got nil")
	}

	// 5. JSON and delimited configuration follow the same rule (verified across sub-tests 1-4)
}
