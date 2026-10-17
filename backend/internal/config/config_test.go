package config

import (
	"reflect"
	"testing"
	"time"
)

func TestLoad_RuntimeOptions(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("TX_SIMULATION_MODE", "true")
	t.Setenv("TX_PARTICIPANT_PROVISIONING", "true")
	t.Setenv("DEFAULT_BANK_CODE", "BANK-A")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:5173, https://ops.example.test")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SimulationMode || !cfg.ParticipantProvisioning || cfg.DefaultBankCode != "BANK-A" {
		t.Fatalf("runtime config = %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.CORSAllowedOrigins, []string{"http://localhost:5173", "https://ops.example.test"}) {
		t.Fatalf("origins = %v", cfg.CORSAllowedOrigins)
	}
}

func TestLoad_RejectsUnsafeRuntimeOptions(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")
	if _, err := Load(); err == nil {
		t.Fatal("wildcard CORS origin was accepted")
	}
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:5173")
	t.Setenv("TX_PARTICIPANT_PROVISIONING", "true")
	t.Setenv("TX_SIMULATION_MODE", "false")
	if _, err := Load(); err == nil {
		t.Fatal("participant provisioning outside simulation mode was accepted")
	}
}

func TestLoad_ValidatesParticipantConfiguration(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("BANK_A_URL", "postgres://localhost/bank-a")
	if _, err := Load(); err == nil {
		t.Fatal("non-HTTP BANK_A_URL was accepted")
	}
	t.Setenv("BANK_A_URL", "http://localhost:8081")
	t.Setenv("BANK_B_CODE", "BANK-A")
	if _, err := Load(); err == nil {
		t.Fatal("duplicate participant bank codes were accepted")
	}
}

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

func TestLoad_CircuitSuccessPolicy(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")

	// 1. Default should be DECREMENT
	t.Setenv("CIRCUIT_SUCCESS_POLICY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error with default: %v", err)
	}
	if cfg.CircuitSuccessPolicy != "DECREMENT" {
		t.Fatalf("expected default CIRCUIT_SUCCESS_POLICY to be DECREMENT, got %s", cfg.CircuitSuccessPolicy)
	}

	// 2. Explicit RESET
	t.Setenv("CIRCUIT_SUCCESS_POLICY", "RESET")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error with RESET: %v", err)
	}
	if cfg.CircuitSuccessPolicy != "RESET" {
		t.Fatalf("expected CIRCUIT_SUCCESS_POLICY to be RESET, got %s", cfg.CircuitSuccessPolicy)
	}

	// 3. Lowercase or trimmed reset
	t.Setenv("CIRCUIT_SUCCESS_POLICY", "  reset  ")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error with lowercase reset: %v", err)
	}
	if cfg.CircuitSuccessPolicy != "RESET" {
		t.Fatalf("expected normalized RESET, got %s", cfg.CircuitSuccessPolicy)
	}

	// 4. Invalid policy returns error
	t.Setenv("CIRCUIT_SUCCESS_POLICY", "INVALID_POLICY")
	_, err = Load()
	if err == nil {
		t.Fatal("expected error for invalid CIRCUIT_SUCCESS_POLICY, got nil")
	}
}

func TestLoad_PaymentConcurrencyPoolReserve(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")

	// Valid default: DBMaxConns=8, PaymentConcurrency=2 (8-2=6 >= 2)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error with defaults: %v", err)
	}
	if cfg.DBMaxConns != 8 || cfg.PaymentConcurrency != 2 {
		t.Fatalf("got conns=%d, concurrency=%d", cfg.DBMaxConns, cfg.PaymentConcurrency)
	}

	// Exceeds reserve: DBMaxConns=4, PaymentConcurrency=3 (4-3=1 < 2)
	t.Setenv("TX_DB_MAX_CONNS", "4")
	t.Setenv("TX_PAYMENT_CONCURRENCY", "3")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when concurrency exceeds pool reserve, got nil")
	}

	// Valid constrained: DBMaxConns=4, PaymentConcurrency=2 (4-2=2 >= 2)
	t.Setenv("TX_DB_MAX_CONNS", "4")
	t.Setenv("TX_PAYMENT_CONCURRENCY", "2")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error for valid constrained pool: %v", err)
	}
	if cfg.DBMaxConns != 4 || cfg.PaymentConcurrency != 2 {
		t.Fatalf("got conns=%d, concurrency=%d", cfg.DBMaxConns, cfg.PaymentConcurrency)
	}
}

func TestLoad_RecoveryAndWorkerConfig(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")

	// 1. Defaults
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error with defaults: %v", err)
	}
	if !cfg.RecoveryEnabled || cfg.RecoveryConcurrency != 2 || cfg.RecoveryMaxAttempts != 20 {
		t.Fatalf("unexpected recovery defaults: %+v", cfg)
	}
	if cfg.RecoveryLease != 60*time.Second || cfg.RecoveryHeartbeat != 15*time.Second || cfg.RecoveryAttemptTimeout != 20*time.Second {
		t.Fatalf("unexpected recovery timing defaults: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.WorkerJobFamilies, []string{"recovery"}) || cfg.WorkerLabel != "transactx-worker" {
		t.Fatalf("unexpected worker defaults: %+v", cfg)
	}

	// 2. Recovery concurrency exceeds pool reserve
	t.Setenv("TX_DB_MAX_CONNS", "4")
	t.Setenv("TX_RECOVERY_CONCURRENCY", "3")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when recovery concurrency exceeds pool reserve, got nil")
	}

	// 3. Attempt timeout >= lease duration
	t.Setenv("TX_RECOVERY_CONCURRENCY", "2")
	t.Setenv("TX_RECOVERY_LEASE", "20s")
	t.Setenv("TX_RECOVERY_ATTEMPT_TIMEOUT", "20s")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when attempt timeout >= lease duration, got nil")
	}

	// 4. Heartbeat >= lease duration
	t.Setenv("TX_RECOVERY_ATTEMPT_TIMEOUT", "10s")
	t.Setenv("TX_RECOVERY_HEARTBEAT", "20s")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when heartbeat >= lease duration, got nil")
	}

	// 5. Custom job families and valid timings
	t.Setenv("TX_RECOVERY_HEARTBEAT", "5s")
	t.Setenv("TX_JOB_FAMILIES", "recovery, delivery, analytics")
	t.Setenv("TX_WORKER_LABEL", "custom-worker-1")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error with valid custom recovery config: %v", err)
	}
	if !reflect.DeepEqual(cfg.WorkerJobFamilies, []string{"recovery", "delivery", "analytics"}) {
		t.Fatalf("expected parsed job families, got: %v", cfg.WorkerJobFamilies)
	}
	if cfg.WorkerLabel != "custom-worker-1" {
		t.Fatalf("expected custom worker label, got: %s", cfg.WorkerLabel)
	}
}
