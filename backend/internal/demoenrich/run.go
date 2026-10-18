package demoenrich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Preview struct {
	Manifest       string  `json:"manifest"`
	SourceSHA      string  `json:"toolSourceSHA"`
	Project        string  `json:"ownerAcknowledgedProject"`
	Host           string  `json:"verifiedEndpoint"`
	Database       string  `json:"verifiedDatabase"`
	Today          int64   `json:"paymentsToday"`
	Total          int64   `json:"paymentsLifetime"`
	Missing        int64   `json:"missing"`
	VisitorReserve int     `json:"visitorReserve"`
	Ready          bool    `json:"capacityReady"`
	Entries        []Entry `json:"entries"`
}

func Run(ctx context.Context, c Config, out io.Writer) error {
	if err := c.Validate(); err != nil {
		return err
	}
	pc, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return errors.New("invalid private database configuration")
	}
	pc.MaxConns = 2
	pc.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pc.ConnConfig.RuntimeParams["search_path"] = "public"
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return errors.New("demo read-only connection failed")
	}
	defer pool.Close()
	var actual string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != c.ExpectedDatabase {
		return errors.New("connected database does not match acknowledged demo")
	}
	var definition string
	var protected bool
	if err := pool.QueryRow(ctx, `SELECT pg_get_functiondef('demo_limits.admit()'::regprocedure),EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='public_demo_payment_capacity' AND tgrelid='public.payments'::regclass AND tgenabled<>'D' AND tgfoid='demo_limits.admit()'::regprocedure)`).Scan(&definition, &protected); err != nil || !protected {
		return errors.New("existing initialized demo capacity guard is required")
	}
	compact := strings.Join(strings.Fields(definition), "")
	if !strings.Contains(compact, "total<1000") || !strings.Contains(compact, "today<50") {
		return errors.New("demo capacity policy changed; inspect it before enrichment")
	}
	entries := Plan(c)
	missing := int64(0)
	for i := range entries {
		if err := lookup(ctx, pool, &entries[i]); err != nil {
			return err
		}
		if entries[i].State == "" {
			missing++
		} else if entries[i].State != "COMPLETED" {
			return fmt.Errorf("manifest entry %d remains %s; inspect original payment, do not resend", entries[i].Number, entries[i].State)
		}
	}
	today, total, err := capacity(ctx, pool)
	if err != nil {
		return err
	}
	preview := Preview{ManifestVersion, c.SourceSHA, c.ExpectedProject, c.ExpectedHost, actual, today, total, missing, 10, CanEnrich(today, total, missing), entries}
	if err := json.NewEncoder(out).Encode(preview); err != nil {
		return err
	}
	if !c.Apply {
		return nil
	} // Dry-run never logs in or invokes a mutation endpoint.
	if !preview.Ready {
		return errors.New("enrichment deferred to preserve ten visitor slots")
	}
	if !filepath.IsAbs(c.ReceiptFile) {
		return errors.New("apply requires an absolute private receipt-file path")
	}
	for parent := filepath.Dir(c.ReceiptFile); ; parent = filepath.Dir(parent) {
		if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil {
			return errors.New("receipt must stay outside Git checkouts")
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	// Serializes only enrichment clients; public visitors retain normal access.
	lock, err := pool.Acquire(ctx)
	if err != nil {
		return errors.New("cannot acquire enrichment admission")
	}
	defer lock.Release()
	var locked bool
	if err := lock.QueryRow(ctx, `SELECT pg_try_advisory_lock(644091033)`).Scan(&locked); err != nil || !locked {
		return errors.New("another enrichment client is active")
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var unlocked bool
		if err := lock.QueryRow(releaseCtx, `SELECT pg_advisory_unlock(644091033)`).Scan(&unlocked); err != nil || !unlocked {
			fmt.Fprintln(out, "Enrichment lock release was not confirmed; database session closure releases it.")
		}
	}()
	receipt, err := os.OpenFile(c.ReceiptFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("private receipt cannot be opened")
	}
	defer receipt.Close()
	writeReceipt := func(value any) error {
		if err := json.NewEncoder(receipt).Encode(value); err != nil {
			return err
		}
		return receipt.Sync()
	}
	if err := writeReceipt(map[string]any{"kind": "begin", "preview": preview, "observedAt": time.Now().UTC()}); err != nil {
		return err
	}
	base, err := balances(ctx, pool)
	if err != nil {
		return err
	}
	if base.Central != base.Participants {
		return errors.New("existing central/participant totals have not converged; defer enrichment")
	}
	needed := map[string]int64{}
	for _, entry := range entries {
		if entry.State == "" {
			needed[entry.Sender] += entry.AmountPaise
		}
	}
	for sender, amount := range needed {
		var count int
		var balance int64
		if err := pool.QueryRow(ctx, `SELECT count(a.id),COALESCE(SUM(a.balance_paise),0) FROM users u JOIN accounts a ON a.user_id=u.id WHERE u.upi_id=$1 AND u.role='CUSTOMER' AND a.status='ACTIVE'`, sender).Scan(&count, &balance); err != nil || count != 1 || balance < amount {
			return errors.New("sender funding/account ownership is not ready; no balance is minted by enrichment")
		}
	}
	client := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	tokens := map[string]string{}
	for i := range entries {
		entry := &entries[i]
		// Another completed run may have committed after preview; never reapply it.
		if err := lookup(ctx, pool, entry); err != nil {
			return err
		}
		if entry.State == "COMPLETED" {
			continue
		}
		if entry.State != "" {
			return errors.New("original manifest payment is unresolved; resume only after review")
		}
		today, total, err = capacity(ctx, pool)
		if err != nil {
			return err
		}
		if !CanEnrich(today, total, 1) {
			return errors.New("visitor reserve reached; stop and resume another day")
		}
		token := tokens[entry.Sender]
		if token == "" {
			var login struct {
				Token string `json:"token"`
				User  struct {
					ID, Role          string
					PaymentIdentifier string `json:"paymentIdentifier"`
				} `json:"user"`
			}
			if err := request(ctx, client, c.APIOrigin, "POST", "/api/auth/demo", "", "", map[string]string{"persona": entry.Persona}, &login); err != nil {
				return err
			}
			var actualID string
			if err := pool.QueryRow(ctx, `SELECT id::text FROM users WHERE upi_id=$1 AND role='CUSTOMER'`, entry.Sender).Scan(&actualID); err != nil || actualID != login.User.ID || login.User.Role != "CUSTOMER" || login.User.PaymentIdentifier != entry.Sender || login.Token == "" {
				return errors.New("API demo identity does not match the acknowledged database")
			}
			token = login.Token
			tokens[entry.Sender] = token
		}
		if err := writeReceipt(map[string]any{"kind": "attempt", "entry": entry, "observedAt": time.Now().UTC()}); err != nil {
			return err
		}
		var payment struct{ ID, State string }
		body := map[string]any{"recipient": entry.Recipient, "amountPaise": entry.AmountPaise, "currency": "INR", "note": fmt.Sprintf("Experience demo · %02d", entry.Number)}
		if err := request(ctx, client, c.APIOrigin, "POST", "/api/payments", token, entry.Key, body, &payment); err != nil {
			return err
		}
		if err := lookup(ctx, pool, entry); err != nil {
			return err
		}
		if entry.PaymentID == "" || payment.ID != entry.PaymentID || entry.State != payment.State {
			return errors.New("API payment does not match the acknowledged database/idempotency identity")
		}
		if err := writeReceipt(map[string]any{"kind": "observed", "entry": entry, "observedAt": time.Now().UTC()}); err != nil {
			return err
		}
		if entry.State != "COMPLETED" {
			return errors.New("payment is still confirming; retain original identity and inspect recovery before resuming")
		}
		timer := time.NewTimer(4 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := verifyFinancials(ctx, pool, entries, base); err != nil {
		return err
	}
	today, total, err = capacity(ctx, pool)
	if err != nil {
		return err
	}
	if !CanEnrich(today, total, 0) {
		return errors.New("concurrent visitor activity used the reserved capacity; acceptance requires review")
	}
	if err := writeReceipt(map[string]any{"kind": "financial_checks_passed", "entries": entries, "paymentSlotsRemaining": Available(today, total), "observedAt": time.Now().UTC()}); err != nil {
		return err
	}
	return nil
}
func capacity(ctx context.Context, pool *pgxpool.Pool) (today, total int64, err error) {
	err = pool.QueryRow(ctx, `SELECT CASE WHEN day=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date THEN today ELSE 0 END,total FROM demo_limits.budgets WHERE kind='payments'`).Scan(&today, &total)
	if err != nil {
		return 0, 0, errors.New("existing demo payment budget is unavailable")
	}
	return
}
func lookup(ctx context.Context, pool *pgxpool.Pool, entry *Entry) error {
	var id *string
	var state string
	err := pool.QueryRow(ctx, `SELECT i.payment_id::text,COALESCE(p.state,'RESERVED') FROM idempotency_records i JOIN users u ON u.id=i.user_id LEFT JOIN payments p ON p.id=i.payment_id WHERE u.upi_id=$1 AND i.key=$2`, entry.Sender, entry.Key).Scan(&id, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		entry.State = ""
		entry.PaymentID = ""
		return nil
	}
	if err != nil {
		return errors.New("cannot read original manifest identity")
	}
	entry.State = state
	if id != nil {
		entry.PaymentID = *id
	}
	return nil
}
func request(ctx context.Context, client *http.Client, origin, method, path, token, key string, body any, result any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(encoded))
	if err != nil {
		return errors.New("invalid API request")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("API outcome is unknown; stop and inspect the original manifest key before resuming")
	}
	defer response.Body.Close()
	if response.StatusCode == 429 {
		return fmt.Errorf("API rate limit; stop and honor Retry-After=%q before resuming", response.Header.Get("Retry-After"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("API returned status %d; stop without retrying or printing response secrets", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return errors.New("API response is incomplete; original outcome requires inspection")
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || json.Unmarshal(envelope.Data, result) != nil {
		return errors.New("API envelope is invalid; original outcome requires inspection")
	}
	return nil
}

type balanceTotals struct{ Central, Participants int64 }

func balances(ctx context.Context, pool *pgxpool.Pool) (balanceTotals, error) {
	var result balanceTotals
	err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT SUM(balance_paise) FROM accounts),0),COALESCE((SELECT SUM(balance_paise) FROM bank_a.accounts),0)+COALESCE((SELECT SUM(balance_paise) FROM bank_b.accounts),0)`).Scan(&result.Central, &result.Participants)
	if err != nil {
		return result, errors.New("cannot independently read central/participant totals")
	}
	return result, nil
}
func verifyFinancials(ctx context.Context, pool *pgxpool.Pool, entries []Entry, before balanceTotals) error {
	after, err := balances(ctx, pool)
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("central/participant conservation snapshot has not converged; acceptance pending")
	}
	var mismatches int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM accounts a JOIN banks b ON b.id=a.bank_id LEFT JOIN bank_a.accounts pa ON b.code='BANK-A' AND pa.id=a.bank_account_id LEFT JOIN bank_b.accounts pb ON b.code='BANK-B' AND pb.id=a.bank_account_id WHERE (b.code='BANK-A' AND (pa.id IS NULL OR pa.balance_paise<>a.balance_paise)) OR (b.code='BANK-B' AND (pb.id IS NULL OR pb.balance_paise<>a.balance_paise))`).Scan(&mismatches); err != nil || mismatches != 0 {
		return errors.New("central/participant account snapshot is not independently converged")
	}
	for _, entry := range entries {
		var valid bool
		if err := pool.QueryRow(ctx, ledgerCheckSQL, entry.PaymentID, entry.AmountPaise).Scan(&valid); err != nil || !valid {
			return errors.New("manifest double-entry/amount invariant is not independently verified")
		}
		var duplicates int
		if err := pool.QueryRow(ctx, participantMultiplicitySQL, entry.PaymentID).Scan(&duplicates); err != nil || duplicates != 0 {
			return errors.New("participant operation multiplicity needs review; no uniqueness claim made")
		}
	}
	return nil
}

const ledgerCheckSQL = `SELECT p.state='COMPLETED' AND p.amount_paise=$2 AND count(e.id)=2 AND count(e.id) FILTER(WHERE e.entry_type='DEBIT' AND e.account_id=p.sender_account_id AND e.amount_paise=$2)=1 AND count(e.id) FILTER(WHERE e.entry_type='CREDIT' AND e.account_id=p.receiver_account_id AND e.amount_paise=$2)=1 FROM payments p JOIN ledger_transactions t ON t.payment_id=p.id JOIN ledger_entries e ON e.ledger_transaction_id=t.id WHERE p.id=$1 GROUP BY p.id`
const participantMultiplicitySQL = `SELECT count(*) FROM (SELECT operation_type,account_id FROM bank_a.operations WHERE payment_id=$1 GROUP BY operation_type,account_id HAVING count(*)>1 UNION ALL SELECT operation_type,account_id FROM bank_b.operations WHERE payment_id=$1 GROUP BY operation_type,account_id HAVING count(*)>1) d`
