package reconciliation

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/bank"
)

type postgresRestartState struct {
	ParticipantID         string    `json:"participantId"`
	ScopeFrom             time.Time `json:"scopeFrom"`
	ScopeTo               time.Time `json:"scopeTo"`
	CanonicalGeneration   string    `json:"canonicalGeneration"`
	ParticipantGeneration string    `json:"participantGeneration"`
	RootHex               string    `json:"rootHex"`
}

// TestDurableCommitmentSurvivesPostgresRestart is driven in two independent
// processes by scripts/verify-merkle-postgres-restart.ps1. The script performs
// a real PostgreSQL restart between prepare and verify. Keeping pg_ctl outside
// the Go process avoids inheriting test-runner process handles on Windows.
func TestDurableCommitmentSurvivesPostgresRestart(t *testing.T) {
	phase := os.Getenv("M3_RESTART_PHASE")
	statePath := os.Getenv("M3_RESTART_STATE_PATH")
	if phase == "" || statePath == "" {
		t.Skip("run through scripts/verify-merkle-postgres-restart.ps1")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var databaseName string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if databaseName != "transactx_m3_restart" {
		t.Fatalf("refusing restart test on database %q", databaseName)
	}

	switch phase {
	case "prepare":
		preparePostgresRestartState(t, ctx, pool, statePath)
	case "verify":
		verifyPostgresRestartState(t, ctx, pool, statePath)
	default:
		t.Fatalf("M3_RESTART_PHASE must be prepare or verify, got %q", phase)
	}
}

func preparePostgresRestartState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statePath string) {
	t.Helper()
	participantID := "BANK-RESTART-" + uuid.NewString()[:12]
	scope := Scope{From: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)}
	record := participantFixtureRecord(0, scope.From.Add(30*time.Minute))
	source := fakeSnapshotSource{snapshot: bank.LedgerSnapshot{
		BankID: participantID, CapturedAt: time.Now().UTC(),
		Entries: []bank.LedgerEntry{{OperationID: record.OperationID, PaymentID: record.PaymentID, AccountID: record.AccountID, EntryType: record.EntryType, AmountPaise: record.AmountPaise, Currency: record.Currency, OccurredAt: record.OccurredAt}},
	}}
	canonical, err := NewDurableRepositoryParticipant(pool, source, participantID, "canonical:"+participantID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := NewDurableRepositoryParticipant(pool, source, participantID, "participant:"+participantID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := canonical.Initialize(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if err := participant.Initialize(ctx, scope); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := canonical.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	participantRoot, err := participant.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalRoot.Root, participantRoot.Root) {
		t.Fatal("prepared canonical and participant roots differ")
	}
	state := postgresRestartState{
		ParticipantID: participantID, ScopeFrom: scope.From, ScopeTo: scope.To,
		CanonicalGeneration:   canonicalRoot.Ref.Generation,
		ParticipantGeneration: participantRoot.Ref.Generation,
		RootHex:               hex.EncodeToString(canonicalRoot.Root),
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func verifyPostgresRestartState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statePath string) {
	t.Helper()
	encoded, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var expected postgresRestartState
	if err := json.Unmarshal(encoded, &expected); err != nil {
		t.Fatal(err)
	}
	scope := Scope{From: expected.ScopeFrom, To: expected.ScopeTo}
	sourceCalls := 0
	source := fakeSnapshotSource{err: errors.New("ledger source must not be read after restart"), calls: &sourceCalls}
	canonical, err := NewDurableRepositoryParticipant(pool, source, expected.ParticipantID, "canonical:"+expected.ParticipantID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	participant, err := NewDurableRepositoryParticipant(pool, source, expected.ParticipantID, "participant:"+expected.ParticipantID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := canonical.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	participantRoot, err := participant.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalRoot.Ref.Generation != expected.CanonicalGeneration || participantRoot.Ref.Generation != expected.ParticipantGeneration {
		t.Fatalf("generation changed across PostgreSQL restart: canonical=%q participant=%q", canonicalRoot.Ref.Generation, participantRoot.Ref.Generation)
	}
	root, err := hex.DecodeString(expected.RootHex)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalRoot.Root, root) || !bytes.Equal(participantRoot.Root, root) {
		t.Fatal("root changed across PostgreSQL restart")
	}
	engine := NewEngine(KnownParticipants{expected.ParticipantID: true}, NewRunRepository(pool),
		func(context.Context, string, Scope) (ReconciliationParticipant, error) { return canonical, nil },
		func(context.Context, string, Scope) (ReconciliationParticipant, error) { return participant, nil },
	)
	run, err := engine.Execute(ctx, RunRequest{ParticipantID: expected.ParticipantID, ScopeFrom: scope.From, ScopeTo: scope.To})
	if err != nil {
		t.Fatal(err)
	}
	if run.DiscrepancyCount != 0 || run.RecordsInspected != 0 {
		t.Fatalf("restart reconciliation did not prune equal roots: discrepancies=%d recordsInspected=%d", run.DiscrepancyCount, run.RecordsInspected)
	}
	if sourceCalls != 0 {
		t.Fatalf("restart read rebuilt from the ledger %d times", sourceCalls)
	}
}
