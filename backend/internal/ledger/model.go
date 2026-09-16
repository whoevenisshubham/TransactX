package ledger

import "github.com/google/uuid"

const (
	EntryDebit  = "DEBIT"
	EntryCredit = "CREDIT"
)

type Entry struct {
	ID                  uuid.UUID
	LedgerTransactionID uuid.UUID
	AccountID           uuid.UUID
	EntryType           string
	AmountPaise         int64
}
