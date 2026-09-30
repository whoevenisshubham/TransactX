package experiments

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/recipients"
	"github.com/transactx/backend/internal/reconciliation"
)

type liveStormSample struct {
	Phase           string `json:"phase"`
	Index           int    `json:"index"`
	Outcome         string `json:"outcome"`
	PaymentID       string `json:"paymentId,omitempty"`
	DuplicateReplay bool   `json:"duplicateReplay"`
}

type liveIntegrityCheck struct {
	Code           string `json:"code"`
	Status         string `json:"status"`
	ViolationCount int    `json:"violationCount"`
}

type liveConcurrencyEvidence struct {
	ExperimentID string            `json:"experimentId"`
	GeneratedAt  time.Time         `json:"generatedAt"`
	GitCommit    string            `json:"gitCommit"`
	WorkingTree  string            `json:"workingTree"`
	Dirty        bool              `json:"dirty"`
	EvidenceMode string            `json:"evidenceMode"`
	Environment  map[string]string `json:"environment"`
	Workload     struct {
		UniqueKeyRequests   int   `json:"uniqueKeyRequests"`
		SameKeyRequests     int   `json:"sameKeyRequests"`
		Concurrency         int   `json:"concurrency"`
		AmountPaise         int64 `json:"amountPaise"`
		OpeningBalancePaise int64 `json:"openingBalancePaise"`
	} `json:"workload"`
	Observed struct {
		SuccessfulResponses        int   `json:"successfulResponses"`
		InsufficientFundsResponses int   `json:"insufficientFundsResponses"`
		PendingResponses           int   `json:"pendingResponses"`
		UnexpectedErrors           int   `json:"unexpectedErrors"`
		LogicalPayments            int   `json:"logicalPayments"`
		CompletedLogicalPayments   int   `json:"completedLogicalPayments"`
		IdempotencyRecords         int   `json:"idempotencyRecords"`
		DuplicateReplayResponses   int   `json:"duplicateReplayResponses"`
		DistinctSameKeyPayments    int   `json:"distinctSameKeyPayments"`
		MinimumBalancePaise        int64 `json:"minimumBalancePaise"`
		LedgerDebitsPaise          int64 `json:"ledgerDebitsPaise"`
		LedgerCreditsPaise         int64 `json:"ledgerCreditsPaise"`
	} `json:"observed"`
	IntegrityRunID           string               `json:"integrityRunId"`
	IntegrityChecks          []liveIntegrityCheck `json:"integrityChecks"`
	TotalIntegrityViolations int                  `json:"totalIntegrityViolations"`
	AllInvariantsSatisfied   bool                 `json:"allInvariantsSatisfied"`
	Samples                  []liveStormSample    `json:"samples"`
}

func evidenceDatabaseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "configured PostgreSQL database (URL unavailable)"
	}
	if parsed.User != nil {
		parsed.User = url.User(parsed.User.Username())
	}
	return parsed.String()
}

func TestPostgresConcurrencyStormEvidence(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	outputPath := os.Getenv("M3_CONCURRENCY_EVIDENCE_PATH")
	if databaseURL == "" || outputPath == "" {
		t.Skip("DATABASE_URL and M3_CONCURRENCY_EVIDENCE_PATH are required")
	}
	commitOutput, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("resolve git commit: %v", err)
	}
	statusOutput, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("inspect working tree: %v", err)
	}
	dirty := strings.TrimSpace(string(statusOutput)) != ""
	evidenceMode := strings.ToLower(strings.TrimSpace(os.Getenv("M3_EVIDENCE_MODE")))
	if evidenceMode == "" {
		evidenceMode = "release"
	}
	if dirty && evidenceMode != "exploratory" {
		t.Fatal("release evidence requires a clean working tree; set M3_EVIDENCE_MODE=exploratory only for non-release measurements")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	var databaseName string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if databaseName != "transactx_m3_evidence" {
		t.Fatalf("evidence storm requires disposable database transactx_m3_evidence, got %q", databaseName)
	}

	const (
		uniqueRequests  = 75
		sameKeyRequests = 20
		concurrency     = 25
		amountPaise     = int64(7)
		openingBalance  = int64(450)
	)
	userID, receiverUserID, bankID := uuid.New(), uuid.New(), uuid.New()
	sourceID, receiverID := uuid.New(), uuid.New()
	suffix := uuid.NewString()
	recipientID := "storm-receiver-" + suffix
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,phone,upi_id,password_hash,role) VALUES
		($1,'Storm Sender',$2,$3,'hash','CUSTOMER'),($4,'Storm Receiver',$5,$6,'hash','CUSTOMER')`,
		userID, "s"+suffix[:20], "storm-sender-"+suffix, receiverUserID, "r"+suffix[:20], recipientID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO banks (id,code,name,status) VALUES ($1,$2,'Storm Bank','ACTIVE')`, bankID, "STORM-"+suffix[:16]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id,user_id,bank_id,bank_account_id,account_number,balance_paise,opening_balance_paise,status) VALUES
		($1,$2,$3,$1,$4,$5,$5,'ACTIVE'),($6,$7,$3,$6,$8,0,0,'ACTIVE')`,
		sourceID, userID, bankID, "storm-source-"+suffix, openingBalance, receiverID, receiverUserID, "storm-receiver-"+suffix); err != nil {
		t.Fatal(err)
	}

	service := payments.NewService(
		accounts.NewRepository(pool),
		recipients.NewRepository(pool),
		payments.NewRepository(pool),
		nil,
	)
	type result struct {
		sample liveStormSample
		err    error
	}
	runWave := func(phase string, requests, workers int, sameKey bool, amount int64) []result {
		tasks := make(chan int, requests)
		results := make(chan result, requests)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for index := range tasks {
					key := "storm-unique-" + suffix + "-" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(strconv.Itoa(index)+suffix)).String()
					if sameKey {
						key = "storm-same-key-" + suffix
					}
					payment, duplicate, callErr := service.CreateWithResult(ctx, payments.CreateInput{
						UserID: userID, SourceAccountID: sourceID, Recipient: recipientID,
						AmountPaise: amount, Currency: "INR", IdempotencyKey: key,
					})
					outcome := "COMPLETED"
					if errors.Is(callErr, payments.ErrInsufficientFunds) {
						outcome = "INSUFFICIENT_FUNDS"
					} else if callErr != nil {
						outcome = "UNEXPECTED_ERROR"
					} else if payment.State != payments.StateCompleted {
						outcome = payment.State
					}
					paymentID := ""
					if payment.ID != uuid.Nil {
						paymentID = payment.ID.String()
					}
					results <- result{sample: liveStormSample{Phase: phase, Index: index, Outcome: outcome, PaymentID: paymentID, DuplicateReplay: duplicate}, err: callErr}
				}
			}()
		}
		for index := 0; index < requests; index++ {
			tasks <- index
		}
		close(tasks)
		close(start)
		wg.Wait()
		close(results)
		out := make([]result, 0, requests)
		for item := range results {
			out = append(out, item)
		}
		return out
	}

	results := append(
		runWave("same_idempotency_key", sameKeyRequests, sameKeyRequests, true, 100),
		runWave("unique_hot_account", uniqueRequests, concurrency, false, amountPaise)...,
	)

	evidence := liveConcurrencyEvidence{ExperimentID: "postgres-concurrency-integrity", GeneratedAt: time.Now().UTC(), Samples: make([]liveStormSample, 0, len(results))}
	evidence.Workload.UniqueKeyRequests = uniqueRequests
	evidence.Workload.SameKeyRequests = sameKeyRequests
	evidence.Workload.Concurrency = concurrency
	evidence.Workload.AmountPaise = amountPaise
	evidence.Workload.OpeningBalancePaise = openingBalance
	for _, item := range results {
		evidence.Samples = append(evidence.Samples, item.sample)
		switch item.sample.Outcome {
		case "COMPLETED":
			evidence.Observed.SuccessfulResponses++
		case "INSUFFICIENT_FUNDS":
			evidence.Observed.InsufficientFundsResponses++
		case payments.StateProcessing, payments.StatePendingReconciliation, payments.StateBankSettledCentralPending:
			evidence.Observed.PendingResponses++
		default:
			evidence.Observed.UnexpectedErrors++
		}
		if item.sample.DuplicateReplay {
			evidence.Observed.DuplicateReplayResponses++
		}
	}
	sort.Slice(evidence.Samples, func(i, j int) bool {
		if evidence.Samples[i].Phase != evidence.Samples[j].Phase {
			return evidence.Samples[i].Phase < evidence.Samples[j].Phase
		}
		return evidence.Samples[i].Index < evidence.Samples[j].Index
	})

	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE state='COMPLETED') FROM payments WHERE initiated_by_user_id=$1`, userID).Scan(&evidence.Observed.LogicalPayments, &evidence.Observed.CompletedLogicalPayments); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id=$1`, userID).Scan(&evidence.Observed.IdempotencyRecords); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT payment_id) FROM idempotency_records WHERE user_id=$1 AND key=$2`, userID, "storm-same-key-"+suffix).Scan(&evidence.Observed.DistinctSameKeyPayments); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT min(balance_paise) FROM accounts WHERE id IN ($1,$2)`, sourceID, receiverID).Scan(&evidence.Observed.MinimumBalancePaise); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT
		COALESCE(sum(le.amount_paise) FILTER (WHERE le.entry_type='DEBIT'),0),
		COALESCE(sum(le.amount_paise) FILTER (WHERE le.entry_type='CREDIT'),0)
		FROM ledger_entries le JOIN ledger_transactions lt ON lt.id=le.ledger_transaction_id
		JOIN payments p ON p.id=lt.payment_id WHERE p.initiated_by_user_id=$1`, userID).Scan(&evidence.Observed.LedgerDebitsPaise, &evidence.Observed.LedgerCreditsPaise); err != nil {
		t.Fatal(err)
	}

	integrityStore := reconciliation.NewPostgresIntegrityRunStore(pool)
	integrityEngine := reconciliation.NewRuntimeIntegrityEngine(reconciliation.NewProductionPostgresFinancialDataStore(pool), integrityStore)
	integrity, err := integrityEngine.Run(ctx, reconciliation.IntegrityRunRequest{})
	if err != nil {
		t.Fatalf("post-storm integrity run: %v", err)
	}
	evidence.IntegrityRunID = integrity.RunID.String()
	for _, check := range integrity.Checks {
		evidence.IntegrityChecks = append(evidence.IntegrityChecks, liveIntegrityCheck{Code: check.Code, Status: string(check.Status), ViolationCount: len(check.Violations)})
		evidence.TotalIntegrityViolations += len(check.Violations)
	}
	sort.Slice(evidence.IntegrityChecks, func(i, j int) bool { return evidence.IntegrityChecks[i].Code < evidence.IntegrityChecks[j].Code })

	evidence.GitCommit = strings.TrimSpace(string(commitOutput))
	evidence.Dirty = dirty
	evidence.EvidenceMode = evidenceMode
	if dirty {
		evidence.WorkingTree = "dirty"
	} else {
		evidence.WorkingTree = "clean"
	}
	var postgresVersion string
	_ = pool.QueryRow(ctx, `SHOW server_version`).Scan(&postgresVersion)
	evidence.Environment = map[string]string{
		"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"postgres": postgresVersion, "database": evidenceDatabaseURL(databaseURL),
	}

	evidence.AllInvariantsSatisfied = evidence.Observed.UnexpectedErrors == 0 &&
		evidence.Observed.PendingResponses == 0 &&
		evidence.Observed.MinimumBalancePaise >= 0 &&
		evidence.Observed.LedgerDebitsPaise == evidence.Observed.LedgerCreditsPaise &&
		evidence.Observed.DistinctSameKeyPayments == 1 &&
		evidence.TotalIntegrityViolations == 0
	for _, check := range evidence.IntegrityChecks {
		if check.Status != string(reconciliation.CheckStatusPass) && check.Status != string(reconciliation.CheckStatusNotApplicable) {
			evidence.AllInvariantsSatisfied = false
		}
	}
	if !evidence.AllInvariantsSatisfied {
		t.Fatalf("post-storm invariant failure: %+v", evidence)
	}

	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatalf("create evidence without overwrite: %v", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote verified PostgreSQL concurrency evidence to %s", outputPath)
}
