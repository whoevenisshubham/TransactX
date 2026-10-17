package experiments

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSourceProvenanceReflectsCheckout(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	if commit, state, dirty := sourceProvenance(dir); commit != "unknown" || state != "unknown" || !dirty {
		t.Fatalf("nonrepository reported known/clean provenance: %s %s %v", commit, state, dirty)
	}
	git("init")
	git("-c", "user.name=Regression", "-c", "user.email=regression@localhost", "commit", "--allow-empty", "-m", "test fixture")
	if commit, state, dirty := sourceProvenance(dir); len(commit) != 40 || state != "clean" || dirty {
		t.Fatalf("clean repository: %s %s %v", commit, state, dirty)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte("untracked"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, state, dirty := sourceProvenance(dir); state != "dirty" || !dirty {
		t.Fatalf("untracked file not reflected: %s %v", state, dirty)
	}
	git("add", "fixture")
	git("-c", "user.name=Regression", "-c", "user.email=regression@localhost", "commit", "-m", "test fixture")
	if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, state, dirty := sourceProvenance(dir); state != "dirty" || !dirty {
		t.Fatalf("tracked edit not reflected: %s %v", state, dirty)
	}
}

func TestCrossBankRecovery_OverallSQLFailureCannotPass(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://unused@127.0.0.1:1/transactx_disposable_closed")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close() // No connection is made; all assertions must observe the pool error.
	summary := (&CrossBankRecoveryHarness{pool: pool}).verifyOverallFinancialInvariants(context.Background())
	if summary.TotalViolations == 0 || summary.ParticipantUnique || summary.DebitCreditConservation || summary.WorklistConvergence {
		t.Fatalf("SQL errors published passing invariants: %+v", summary)
	}
}
