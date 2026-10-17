package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func validateInvestigatorTestDBName(dbName string) error {
	trimmed := strings.TrimSpace(dbName)
	if trimmed == "" {
		return fmt.Errorf("database name cannot be empty")
	}
	lower := strings.ToLower(trimmed)
	if lower == "transactx" {
		return fmt.Errorf("refusing test execution against source database %q: integration tests require an isolated disposable database", dbName)
	}
	for _, sysDB := range []string{"postgres", "template0", "template1"} {
		if lower == sysDB {
			return fmt.Errorf("refusing test execution against system database %q", dbName)
		}
	}
	for _, pfx := range []string{"transactx_disposable_", "transactx_cb_restore_", "transactx_test_", "transactx_m2_disposable_"} {
		if strings.HasPrefix(lower, pfx) {
			return nil
		}
	}
	return fmt.Errorf("database %q is not explicitly identified as disposable (must start with one of: transactx_disposable_, transactx_cb_restore_, transactx_test_, transactx_m2_disposable_)", dbName)
}

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping live PostgreSQL investigator tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("configured PostgreSQL connection failed: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("configured PostgreSQL ping failed: %v", err)
	}

	var currentDB string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB); err != nil {
		pool.Close()
		t.Fatalf("failed to query current_database(): %v", err)
	}
	if err := validateInvestigatorTestDBName(currentDB); err != nil {
		pool.Close()
		t.Fatalf("unsafe test database connection rejected: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM investigation_jobs;"); err != nil {
		pool.Close()
		t.Fatalf("clean disposable investigation jobs: %v", err)
	}
	return pool
}

func TestSchemaCompliance_Investigation_AgainstV2Schema(t *testing.T) {
	schemaPaths := []string{
		"../../../docs/next-gen/execution-pack/docs/next-gen/contracts/v2.schema.json",
		"docs/next-gen/execution-pack/docs/next-gen/contracts/v2.schema.json",
	}

	var schemaData []byte
	var err error
	for _, p := range schemaPaths {
		schemaData, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("required v2.schema.json is not accessible: %v", err)
	}

	var schemaRoot struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		} `json:"$defs"`
	}

	if err := json.Unmarshal(schemaData, &schemaRoot); err != nil {
		t.Fatalf("failed to unmarshal v2.schema.json: %v", err)
	}

	typeMapping := map[string]any{
		"Investigation":         Investigation{},
		"InvestigationAccepted": InvestigationAccepted{},
		"InvestigationReport":   InvestigationReport{},
		"Fact":                  Fact{},
		"Assertion":             Assertion{},
		"Inference":             Inference{},
		"Recommendation":        Recommendation{},
		"ToolStep":              ToolStep{},
	}

	for defName, dtoInstance := range typeMapping {
		def, ok := schemaRoot.Defs[defName]
		if !ok {
			t.Errorf("definition %s missing from schema $defs", defName)
			continue
		}

		dtoFields := getJSONTags(reflect.TypeOf(dtoInstance))

		for _, req := range def.Required {
			if !dtoFields[req] {
				t.Errorf("DTO for %s missing required schema field: %q", defName, req)
			}
		}

		for prop := range def.Properties {
			if !dtoFields[prop] {
				t.Errorf("DTO for %s missing schema property: %q", defName, prop)
			}
		}
	}
}

func getJSONTags(t reflect.Type) map[string]bool {
	tags := make(map[string]bool)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag != "" && tag != "-" {
			parts := strings.Split(tag, ",")
			tags[parts[0]] = true
		}
	}
	return tags
}

func createTestPayment(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	ctx := context.Background()
	bankID := uuid.New()
	bankCode := "BANK_TEST_" + uuid.NewString()[:8]
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank Test', 'ACTIVE')", bankID, bankCode)

	senderID := uuid.New()
	senderPhone := "+9199" + uuid.NewString()[:8]
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, phone, name, upi_id, password_hash, role)
		VALUES ($1, $2, 'Test User', $3, 'hash', 'CUSTOMER');
	`, senderID, senderPhone, senderPhone+"@upi")
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}

	accID := uuid.New()
	_, _ = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status)
		VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE');
	`, accID, senderID, bankID, "ACC_"+uuid.NewString()[:8])

	paymentID := uuid.New()
	query := `
		INSERT INTO payments (
			id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
			currency, state, source_bank_id, destination_bank_id,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $3, 50000, 'INR', 'PENDING_RECONCILIATION', $4, $4, NOW(), NOW()
		);
	`
	_, err = pool.Exec(ctx, query, paymentID, senderID, accID, bankID)
	if err != nil {
		t.Fatalf("failed to insert test payment: %v", err)
	}
	return paymentID
}

func TestInvestigatorRepository_CreateJobAndIdempotency(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	repo := NewPostgresRepository(pool)

	paymentID := createTestPayment(t, pool)
	actorID := "actor-" + uuid.New().String()
	idempKey := "idemp-" + uuid.New().String()
	question := "Why is payment pending reconciliation?"

	// 1. Initial create
	job, isNew, err := repo.CreateJob(ctx, paymentID, actorID, idempKey, question, ModeEvidenceOnly)
	if err != nil {
		t.Fatalf("initial create job failed: %v", err)
	}
	if !isNew {
		t.Errorf("expected isNew = true on first create")
	}
	if job.Status != StatusQueued {
		t.Errorf("expected job status %s, got %s", StatusQueued, job.Status)
	}

	// 2. Exact idempotency replay
	job2, isNew2, err := repo.CreateJob(ctx, paymentID, actorID, idempKey, question, ModeEvidenceOnly)
	if err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	if isNew2 {
		t.Errorf("expected isNew = false on replay")
	}
	if job2.ID != job.ID {
		t.Errorf("expected matching job ID %s, got %s", job.ID, job2.ID)
	}

	// 3. Idempotency conflict with different question
	_, _, err = repo.CreateJob(ctx, paymentID, actorID, idempKey, "Different question?", ModeEvidenceOnly)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("expected ErrIdempotencyConflict, got %v", err)
	}

	// 4. Actor active limit (1 active per actor)
	otherKey := "idemp-other-" + uuid.New().String()
	_, _, err = repo.CreateJob(ctx, paymentID, actorID, otherKey, "Another question?", ModeEvidenceOnly)
	if !errors.Is(err, ErrActorLimitExceeded) {
		t.Errorf("expected ErrActorLimitExceeded, got %v", err)
	}
}

func TestInvestigatorRepository_ClaimAndComplete(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	repo := NewPostgresRepository(pool)

	paymentID := createTestPayment(t, pool)
	actorID := "actor-claim-" + uuid.New().String()
	idempKey := "idemp-claim-" + uuid.New().String()
	question := "What happened to this transaction?"

	job, isNew, err := repo.CreateJob(ctx, paymentID, actorID, idempKey, question, ModeEvidenceOnly)
	if err != nil || !isNew || job.ID == uuid.Nil {
		t.Fatalf("create job failed: %v", err)
	}

	// Worker 1 claims job
	claimed, err := repo.ClaimNextJob(ctx, "worker-1", 10*time.Second)
	if err != nil {
		t.Fatalf("claim next job failed: %v", err)
	}
	if claimed == nil {
		t.Fatalf("expected claim to return a job")
	}
	if claimed.Status != StatusRunning {
		t.Errorf("expected status %s, got %s", StatusRunning, claimed.Status)
	}

	// Complete job
	now := time.Now().UTC()
	report := &InvestigationReport{
		SchemaVersion:    SchemaVersionV1,
		Mode:             ModeEvidenceOnly,
		Summary:          "Payment analyzed.",
		Facts:            []Fact{},
		Inferences:       []Inference{},
		Recommendations:  []Recommendation{},
		MissingEvidence:  []string{},
		EvidenceCoverage: CoverageCompleteForQuestion,
		SnapshotId:       "sha256:test",
		CollectedAt:      now.Format(time.RFC3339),
	}
	timeline := []ToolStep{
		{
			ToolName:    "read_payment_evidence",
			Arguments:   map[string]any{"paymentId": paymentID.String()},
			StartedAt:   now,
			FinishedAt:  &now,
			Status:      ToolStepStatusCompleted,
			EvidenceIDs: []string{"evidence-1"},
		},
	}
	usage := &UsageInfo{
		PromptVersion: "evidence_only.v1",
	}

	err = repo.CompleteJob(ctx, claimed.ID, claimed.LeaseEpoch, report, timeline, usage)
	if err != nil {
		t.Fatalf("complete job failed: %v", err)
	}

	// Re-read job
	completedJob, err := repo.GetJob(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if completedJob.Status != StatusCompleted {
		t.Errorf("expected completed status, got %s", completedJob.Status)
	}
	if completedJob.Report == nil {
		t.Errorf("expected report to be persisted")
	}
	if len(completedJob.ToolTimeline) != 1 {
		t.Errorf("expected 1 timeline step, got %d", len(completedJob.ToolTimeline))
	}
}

func TestInvestigatorRepository_CorpusSearch(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	repo := NewPostgresRepository(pool)

	docPath := "docs/runbooks/test-runbook-" + uuid.NewString() + ".md"
	testChunk := CorpusChunk{
		ID:            uuid.New(),
		CorpusVersion: "v1",
		DocumentPath:  docPath,
		Heading:       "Test Invariant Recovery Procedures",
		StartLine:     1,
		EndLine:       20,
		Content:       "When reconciliation is interrupted, inspect presence before retrying participant dispatch.",
		SHA256:        "abc123456789",
		ApprovedTags:  []string{"test_recovery", "invariant"},
		AllowedRoles:  []string{"OPS_ADMIN", "OPS_VIEWER"},
		Active:        true,
	}

	if err := repo.InsertCorpusChunk(ctx, testChunk); err != nil {
		t.Fatalf("insert corpus chunk failed: %v", err)
	}

	results, err := repo.SearchCorpus(ctx, "reconciliation participant dispatch", []string{"test_recovery"}, 5)
	if err != nil {
		t.Fatalf("search corpus failed: %v", err)
	}
	if len(results) == 0 {
		t.Errorf("expected at least 1 search result, got 0")
	}

	var found bool
	for _, c := range results {
		if c.Heading == testChunk.Heading {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected test chunk heading %q in search results", testChunk.Heading)
	}
}

type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *rateLimiter) Allow(actorID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	times := rl.requests[actorID]
	var valid []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		rl.requests[actorID] = valid
		return false
	}

	valid = append(valid, now)
	rl.requests[actorID] = valid
	return true
}

func TestInvestigatorService_RateLimit(t *testing.T) {
	rl := newRateLimiter(3, 1*time.Minute)
	actor := "test-actor"

	if !rl.Allow(actor) {
		t.Errorf("call 1 should be allowed")
	}
	if !rl.Allow(actor) {
		t.Errorf("call 2 should be allowed")
	}
	if !rl.Allow(actor) {
		t.Errorf("call 3 should be allowed")
	}
	if rl.Allow(actor) {
		t.Errorf("call 4 should be rejected by rate limit")
	}
}

func TestInvestigatorService_ExecuteJob(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	invStore, err := NewPostgresInvestigationStore(pool, time.Now)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	corpusRepo, err := NewPostgresCorpusRepository(pool)
	if err != nil {
		t.Fatalf("new corpus repo: %v", err)
	}

	paymentID := createTestPayment(t, pool)
	fixture := loadFixture(t, "healthy.json")
	fixture.Evidence.PaymentId = paymentID.String()
	fixture.Evidence.Payment.EvidenceId = "payment:" + paymentID.String()

	reader := fakeReader{bundle: fixture.Evidence}
	deps := ToolDependencies{
		Evidence: reader,
		Runbooks: corpusRepo,
	}

	cfg := DefaultServiceConfig()
	cfg.JobTimeout = 2 * time.Minute
	svc, err := NewService(cfg, invStore, deps)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	actorID := "actor-exec-" + uuid.New().String()
	idempKey := "idemp-exec-" + uuid.New().String()
	scope := TrustedScope{
		ActorID:           actorID,
		Role:              "OPS_ADMIN",
		SelectedPaymentID: paymentID.String(),
	}

	job, _, err := svc.Start(ctx, scope, paymentID.String(), "Why is this payment pending?", idempKey)
	if err != nil {
		t.Fatalf("start job: %v", err)
	}

	completedJob, err := svc.Run(ctx, job.ID, "worker-test")
	if err != nil {
		t.Fatalf("run job: %v", err)
	}

	if completedJob.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, completedJob.Status)
	}
	if completedJob.Report == nil {
		t.Fatalf("expected non-nil report")
	}
	if completedJob.Report.SchemaVersion != SchemaVersionV1 {
		t.Errorf("expected schemaVersion %s, got %s", SchemaVersionV1, completedJob.Report.SchemaVersion)
	}
	if len(completedJob.Report.Facts) == 0 {
		t.Errorf("expected facts in report, got 0")
	}
	if len(completedJob.ToolTimeline) < 1 {
		t.Errorf("expected at least 1 timeline step, got %d", len(completedJob.ToolTimeline))
	}
}
