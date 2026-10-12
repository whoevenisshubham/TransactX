package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/operations"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to postgresql (%s): %v", dbURL, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("cannot ping postgresql (%s): %v", dbURL, err)
	}
	_, _ = pool.Exec(ctx, "DELETE FROM investigation_jobs;")
	return pool
}

func TestSchemaCompliance_Investigation_AgainstV2Schema(t *testing.T) {
	schemaPaths := []string{
		"C:/DL_EXam/tut/m/Project_update/TransactX_M2/docs/next-gen/execution-pack/docs/next-gen/contracts/v2.schema.json",
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
		t.Skip("v2.schema.json not accessible from test path, skipping schema contract test")
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
		SnapshotID:       "sha256:test",
		CollectedAt:      now,
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

type fakeEvidenceReader struct {
	bundle *operations.EvidenceBundle
	err    error
}

func (f *fakeEvidenceReader) ReadPaymentEvidence(ctx context.Context, trustedScope string, paymentID uuid.UUID) (*operations.EvidenceBundle, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.bundle, nil
}

func TestInvestigatorService_ExecuteJob(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	repo := NewPostgresRepository(pool)

	paymentID := createTestPayment(t, pool)
	bundle := &operations.EvidenceBundle{
		SchemaVersion: "tx.v2.1",
		PaymentID:     paymentID,
		SnapshotID:    "sha256:fake-snapshot",
		CollectedAt:   time.Now().UTC(),
		Payment: operations.PaymentSnapshot{
			State:        "PENDING_RECONCILIATION",
			AmountPaise:  "50000",
			Currency:     "INR",
			CreatedAt:    time.Now().UTC().Add(-10 * time.Minute),
			UpdatedAt:    time.Now().UTC(),
			EventVersion: "2",
			EvidenceID:   "payment:" + paymentID.String() + ":snap",
		},
		Route: operations.RouteSnapshot{
			Status: "routed",
		},
		Operations: []operations.OperationSnapshot{
			{
				OperationID:   uuid.New(),
				OperationType: "DEBIT",
				BankCode:      "BANK_A",
				CentralStatus: "DISPATCHED",
				Presence:      "absent_confirmed",
				EvidenceID:    "op:fake:1",
			},
		},
		Recovery: operations.RecoverySnapshot{
			Status:       "running",
			AttemptCount: 1,
		},
		Sources: []operations.SourceSnapshot{
			{
				Name:   "bank_b",
				Status: "unavailable",
			},
		},
	}

	fakeReader := &fakeEvidenceReader{bundle: bundle}
	svc := NewService(repo, fakeReader, nil)

	if err := svc.SeedRunbooks(ctx); err != nil {
		t.Fatalf("seed runbooks failed: %v", err)
	}

	actorID := "actor-exec-" + uuid.New().String()
	idempKey := "idemp-exec-" + uuid.New().String()

	acc, err := svc.CreateInvestigation(ctx, actorID, idempKey, CreateJobRequest{
		PaymentID: paymentID.String(),
		Question:  "Why is this payment pending?",
	})
	if err != nil {
		t.Fatalf("create investigation failed: %v", err)
	}

	claimed, err := repo.ClaimNextJob(ctx, "worker-test", 10*time.Second)
	if err != nil || claimed == nil {
		t.Fatalf("claim failed: %v", err)
	}

	if err := svc.ExecuteJob(ctx, claimed); err != nil {
		t.Fatalf("execute job failed: %v", err)
	}

	inv, err := svc.GetInvestigation(ctx, acc.InvestigationID)
	if err != nil {
		t.Fatalf("get investigation failed: %v", err)
	}

	if inv.Status != StatusCompleted {
		t.Errorf("expected status %s, got %s", StatusCompleted, inv.Status)
	}
	if inv.Report == nil {
		t.Fatalf("expected non-nil report")
	}
	if inv.Report.SchemaVersion != SchemaVersionV1 {
		t.Errorf("expected schemaVersion %s, got %s", SchemaVersionV1, inv.Report.SchemaVersion)
	}
	if len(inv.Report.Facts) == 0 {
		t.Errorf("expected facts in report, got 0")
	}

	var foundStateAssertion bool
	for _, f := range inv.Report.Facts {
		for _, a := range f.Assertions {
			if a.Field == "central_state" && a.Value == "PENDING_RECONCILIATION" {
				foundStateAssertion = true
			}
		}
	}
	if !foundStateAssertion {
		t.Errorf("expected central_state assertion PENDING_RECONCILIATION in facts")
	}

	if len(inv.ToolTimeline) < 2 {
		t.Errorf("expected at least 2 timeline steps, got %d", len(inv.ToolTimeline))
	}
}
