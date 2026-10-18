package demoenrich

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestEnrichmentReadChecksAgainstCanonicalPostgres(t *testing.T) {
	dsn := os.Getenv("ENRICH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ENRICH_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(u.Path, "/txv3_gemini_store_") {
		t.Fatal("requires isolated canonical disposable schema")
	}
	ctx := context.Background()
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pc.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	base, err := balances(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinancials(ctx, pool, nil, base); err != nil {
		t.Fatal(err)
	}
	unknown := Entry{PaymentID: uuid.NewString(), AmountPaise: 125}
	if err := verifyFinancials(ctx, pool, []Entry{unknown}, base); err == nil {
		t.Fatal("missing payment/ledger evidence passed financial checks")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Conn().Prepare(ctx, "enrichment_ledger_check", ledgerCheckSQL); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Conn().Prepare(ctx, "enrichment_participant_check", participantMultiplicitySQL); err != nil {
		t.Fatal(err)
	}
	// This validates SQL/error behavior only. No financial fixture/history is
	// inserted, and an empty schema is not live financial acceptance.
}
