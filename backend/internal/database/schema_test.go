package database

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPhase1ASchema(t *testing.T) {
	pool := newSchemaTestPool(t)
	defer pool.Close()

	tests := []struct {
		name string
		test func(*testing.T, pgx.Tx, testFixtures)
	}{
		{"duplicate phone is rejected", testDuplicatePhone},
		{"duplicate UPI ID is rejected", testDuplicateUPIID},
		{"duplicate bank code is rejected", testDuplicateBankCode},
		{"duplicate account number is rejected", testDuplicateAccountNumber},
		{"foreign keys are enforced", testForeignKeys},
		{"negative account balance is rejected", testNegativeAccountBalance},
		{"negative account version is rejected", testNegativeAccountVersion},
		{"non-positive payment amount is rejected", testNonPositivePaymentAmount},
		{"unsupported currency is rejected", testUnsupportedCurrency},
		{"invalid payment state is rejected", testInvalidPaymentState},
		{"invalid ledger entry type is rejected", testInvalidLedgerEntryType},
		{"non-positive ledger entry amount is rejected", testNonPositiveLedgerEntryAmount},
		{"duplicate payment ID in ledger transactions is rejected", testDuplicateLedgerPayment},
		{"duplicate user-scoped idempotency key is rejected", testDuplicateIdempotencyKey},
		{"idempotency key can be reused by another user", testIdempotencyKeyAcrossUsers},
		{"financial values are stored as integer paise", testIntegerPaise},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())

			test.test(t, tx, newTestFixtures())
		})
	}
}

type testFixtures struct {
	userID        string
	secondUserID  string
	bankID        string
	secondBankID  string
	accountID     string
	secondAccount string
	paymentID     string
}

func newTestFixtures() testFixtures {
	return testFixtures{
		userID: testUUID(), secondUserID: testUUID(),
		bankID: testUUID(), secondBankID: testUUID(),
		accountID: testUUID(), secondAccount: testUUID(), paymentID: testUUID(),
	}
}

func newSchemaTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return pool
}

func testDuplicatePhone(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertUser(t, tx, fixtures.userID, testPhone(fixtures.userID), "upi-"+fixtures.userID)
	expectDatabaseError(t, tx, `INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES ($1, 'Second', $2, $3, 'hash', 'CUSTOMER')`, testUUID(), testPhone(fixtures.userID), "upi-"+testUUID())
}

func testDuplicateUPIID(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertUser(t, tx, fixtures.userID, testPhone(fixtures.userID), "upi-"+fixtures.userID)
	expectDatabaseError(t, tx, `INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES ($1, 'Second', $2, $3, 'hash', 'CUSTOMER')`, testUUID(), testPhone(testUUID()), "upi-"+fixtures.userID)
}

func testDuplicateBankCode(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertBank(t, tx, fixtures.bankID, testBankCode(fixtures.bankID))
	expectDatabaseError(t, tx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Second Bank', 'ACTIVE')`, testUUID(), testBankCode(fixtures.bankID))
}

func testDuplicateAccountNumber(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertUser(t, tx, fixtures.userID, testPhone(fixtures.userID), "upi-"+fixtures.userID)
	insertBank(t, tx, fixtures.bankID, testBankCode(fixtures.bankID))
	insertAccount(t, tx, fixtures.accountID, fixtures.userID, fixtures.bankID, "account-"+fixtures.accountID, 10050, 0)
	expectDatabaseError(t, tx, `INSERT INTO accounts (id, user_id, bank_id, account_number, status) VALUES ($1, $2, $3, $4, 'ACTIVE')`, testUUID(), fixtures.userID, fixtures.bankID, "account-"+fixtures.accountID)
}

func testForeignKeys(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	expectDatabaseError(t, tx, `INSERT INTO accounts (id, user_id, bank_id, account_number, status) VALUES ($1, $2, $3, $4, 'ACTIVE')`, fixtures.accountID, testUUID(), testUUID(), "account-"+fixtures.accountID)
}

func testNegativeAccountBalance(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertUser(t, tx, fixtures.userID, testPhone(fixtures.userID), "upi-"+fixtures.userID)
	insertBank(t, tx, fixtures.bankID, testBankCode(fixtures.bankID))
	expectDatabaseError(t, tx, `INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES ($1, $2, $3, $4, -1, 'ACTIVE')`, fixtures.accountID, fixtures.userID, fixtures.bankID, "account-"+fixtures.accountID)
}

func testNegativeAccountVersion(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertUser(t, tx, fixtures.userID, testPhone(fixtures.userID), "upi-"+fixtures.userID)
	insertBank(t, tx, fixtures.bankID, testBankCode(fixtures.bankID))
	expectDatabaseError(t, tx, `INSERT INTO accounts (id, user_id, bank_id, account_number, version, status) VALUES ($1, $2, $3, $4, -1, 'ACTIVE')`, fixtures.accountID, fixtures.userID, fixtures.bankID, "account-"+fixtures.accountID)
}

func testNonPositivePaymentAmount(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertPaymentFixtures(t, tx, fixtures)
	expectDatabaseError(t, tx, paymentInsertSQL, testUUID(), fixtures.userID, fixtures.accountID, fixtures.secondAccount, 0, "INR", "CREATED")
}

func testUnsupportedCurrency(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertPaymentFixtures(t, tx, fixtures)
	expectDatabaseError(t, tx, paymentInsertSQL, testUUID(), fixtures.userID, fixtures.accountID, fixtures.secondAccount, 10050, "USD", "CREATED")
}

func testInvalidPaymentState(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertPaymentFixtures(t, tx, fixtures)
	expectDatabaseError(t, tx, paymentInsertSQL, testUUID(), fixtures.userID, fixtures.accountID, fixtures.secondAccount, 10050, "INR", "INVALID")
}

func testInvalidLedgerEntryType(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertPaymentFixtures(t, tx, fixtures)
	insertLedgerTransaction(t, tx, fixtures.paymentID, fixtures.paymentID)
	expectDatabaseError(t, tx, `INSERT INTO ledger_entries (id, ledger_transaction_id, account_id, entry_type, amount_paise) VALUES ($1, $2, $3, 'INVALID', 10050)`, testUUID(), fixtures.paymentID, fixtures.accountID)
}

func testNonPositiveLedgerEntryAmount(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertPaymentFixtures(t, tx, fixtures)
	insertLedgerTransaction(t, tx, fixtures.paymentID, fixtures.paymentID)
	expectDatabaseError(t, tx, `INSERT INTO ledger_entries (id, ledger_transaction_id, account_id, entry_type, amount_paise) VALUES ($1, $2, $3, 'DEBIT', 0)`, testUUID(), fixtures.paymentID, fixtures.accountID)
}

func testDuplicateLedgerPayment(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertPaymentFixtures(t, tx, fixtures)
	insertLedgerTransaction(t, tx, fixtures.paymentID, fixtures.paymentID)
	expectDatabaseError(t, tx, `INSERT INTO ledger_transactions (id, payment_id) VALUES ($1, $2)`, testUUID(), fixtures.paymentID)
}

func testDuplicateIdempotencyKey(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertPaymentFixtures(t, tx, fixtures)
	insertIdempotencyRecord(t, tx, fixtures.userID, "same-key", fixtures.paymentID)
	expectDatabaseError(t, tx, `INSERT INTO idempotency_records (id, user_id, key, request_hash) VALUES ($1, $2, 'same-key', 'hash-2')`, testUUID(), fixtures.userID)
}

func testIdempotencyKeyAcrossUsers(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertUser(t, tx, fixtures.userID, testPhone(fixtures.userID), "upi-"+fixtures.userID)
	insertUser(t, tx, fixtures.secondUserID, testPhone(fixtures.secondUserID), "upi-"+fixtures.secondUserID)
	insertIdempotencyRecord(t, tx, fixtures.userID, "same-key", "")
	insertIdempotencyRecord(t, tx, fixtures.secondUserID, "same-key", "")
}

func testIntegerPaise(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	insertPaymentFixtures(t, tx, fixtures)
	var amount int64
	if err := tx.QueryRow(context.Background(), `SELECT amount_paise FROM payments WHERE id = $1`, fixtures.paymentID).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 10050 {
		t.Fatalf("amount_paise = %d, want 10050", amount)
	}
}

const paymentInsertSQL = `INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state) VALUES ($1, $2, $3, $4, $5, $6, $7)`

func insertUser(t *testing.T, tx pgx.Tx, id, phone, upiID string) {
	t.Helper()
	_, err := tx.Exec(context.Background(), `INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES ($1, 'Test User', $2, $3, 'hash', 'CUSTOMER')`, id, phone, upiID)
	if err != nil {
		t.Fatal(err)
	}
}

func insertBank(t *testing.T, tx pgx.Tx, id, code string) {
	t.Helper()
	_, err := tx.Exec(context.Background(), `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Test Bank', 'ACTIVE')`, id, code)
	if err != nil {
		t.Fatal(err)
	}
}

func insertAccount(t *testing.T, tx pgx.Tx, id, userID, bankID, accountNumber string, balance, version int64) {
	t.Helper()
	_, err := tx.Exec(context.Background(), `INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, version, status) VALUES ($1, $2, $3, $4, $5, $6, 'ACTIVE')`, id, userID, bankID, accountNumber, balance, version)
	if err != nil {
		t.Fatal(err)
	}
}

func insertPaymentFixtures(t *testing.T, tx pgx.Tx, fixtures testFixtures) {
	t.Helper()
	insertUser(t, tx, fixtures.userID, testPhone(fixtures.userID), "upi-"+fixtures.userID)
	insertBank(t, tx, fixtures.bankID, testBankCode(fixtures.bankID))
	insertAccount(t, tx, fixtures.accountID, fixtures.userID, fixtures.bankID, "account-"+fixtures.accountID, 10050, 0)
	insertAccount(t, tx, fixtures.secondAccount, fixtures.userID, fixtures.bankID, "account-"+fixtures.secondAccount, 0, 0)
	_, err := tx.Exec(context.Background(), paymentInsertSQL, fixtures.paymentID, fixtures.userID, fixtures.accountID, fixtures.secondAccount, 10050, "INR", "CREATED")
	if err != nil {
		t.Fatal(err)
	}
}

func insertLedgerTransaction(t *testing.T, tx pgx.Tx, id, paymentID string) {
	t.Helper()
	_, err := tx.Exec(context.Background(), `INSERT INTO ledger_transactions (id, payment_id) VALUES ($1, $2)`, id, paymentID)
	if err != nil {
		t.Fatal(err)
	}
}

func insertIdempotencyRecord(t *testing.T, tx pgx.Tx, userID, key, paymentID string) {
	t.Helper()
	var err error
	if paymentID == "" {
		_, err = tx.Exec(context.Background(), `INSERT INTO idempotency_records (id, user_id, key, request_hash) VALUES ($1, $2, $3, 'hash')`, testUUID(), userID, key)
	} else {
		_, err = tx.Exec(context.Background(), `INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id) VALUES ($1, $2, $3, 'hash', $4)`, testUUID(), userID, key, paymentID)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func expectDatabaseError(t *testing.T, tx pgx.Tx, query string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(context.Background(), `SAVEPOINT schema_test_case`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), query, args...); err == nil {
		t.Fatal("expected database constraint error")
	}
	if _, err := tx.Exec(context.Background(), `ROLLBACK TO SAVEPOINT schema_test_case`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `RELEASE SAVEPOINT schema_test_case`); err != nil {
		t.Fatal(err)
	}
}

func testUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate test UUID: %v", err))
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

func testPhone(id string) string {
	return "919876" + id[:6]
}

func testBankCode(id string) string {
	return "BANK-" + id[:8]
}
