package reconciliation

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
)

// CanonicalVersion is the version frozen for the M3-1 logical record format.
const CanonicalVersion = "v1"

const (
	canonicalHeader = "TXCANON|v1"
	leafDomain      = "TXLEAF|v1|"
)

// CanonicalRecord contains only stable logical participant-ledger fields.
// Snapshot IDs, capture times, and physical database row IDs are intentionally
// absent because they describe an observation, not a ledger fact.
type CanonicalRecord struct {
	OperationID uuid.UUID
	PaymentID   uuid.UUID
	AccountID   uuid.UUID
	EntryType   string
	AmountPaise int64
	Currency    string
	OccurredAt  time.Time
}

// FromLedgerEntry adapts the frozen BankAdapter ledger shape without changing
// the BankAdapter contract or importing snapshot metadata into canonical data.
func FromLedgerEntry(entry bank.LedgerEntry) CanonicalRecord {
	return CanonicalRecord{
		OperationID: entry.OperationID,
		PaymentID:   entry.PaymentID,
		AccountID:   entry.AccountID,
		EntryType:   entry.EntryType,
		AmountPaise: entry.AmountPaise,
		Currency:    entry.Currency,
		OccurredAt:  entry.OccurredAt.UTC(),
	}
}

// Normalize returns the record with its timestamp represented in UTC. The
// logical values are otherwise preserved exactly.
func (record CanonicalRecord) Normalize() CanonicalRecord {
	record.OccurredAt = record.OccurredAt.UTC()
	return record
}

// CanonicalBytes serializes one record using an explicit, versioned format:
// ASCII "TXCANON|v1", followed by seven fields. Each field has a uint32
// big-endian byte length and UTF-8 bytes: operation UUID, payment UUID,
// account UUID, entry type, amount (decimal), currency, and occurred_at (UTC
// RFC3339Nano). Length prefixes make strings unambiguous.
func CanonicalBytes(record CanonicalRecord) ([]byte, error) {
	record = record.Normalize()
	fields := []string{
		record.OperationID.String(),
		record.PaymentID.String(),
		record.AccountID.String(),
		record.EntryType,
		strconv.FormatInt(record.AmountPaise, 10),
		record.Currency,
		record.OccurredAt.Format(time.RFC3339Nano),
	}

	var out bytes.Buffer
	out.WriteString(canonicalHeader)
	for _, field := range fields {
		if uint64(len(field)) > uint64(^uint32(0)) {
			return nil, errors.New("canonical field exceeds uint32 length")
		}
		if err := binary.Write(&out, binary.BigEndian, uint32(len(field))); err != nil {
			return nil, err
		}
		out.WriteString(field)
	}
	return out.Bytes(), nil
}

// LeafHash returns SHA-256("TXLEAF|v1|" || CanonicalBytes(record)).
func LeafHash(record CanonicalRecord) ([]byte, error) {
	canonical, err := CanonicalBytes(record)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(leafDomain))
	_, _ = hash.Write(canonical)
	return hash.Sum(nil), nil
}

// SortRecords returns a normalized, stable copy ordered independently of
// database row or snapshot IDs. The primary key is occurred_at UTC, followed
// by operation ID, entry type, account ID, payment ID, amount, and currency.
func SortRecords(records []CanonicalRecord) []CanonicalRecord {
	ordered := append([]CanonicalRecord(nil), records...)
	for i := range ordered {
		ordered[i] = ordered[i].Normalize()
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if !left.OccurredAt.Equal(right.OccurredAt) {
			return left.OccurredAt.Before(right.OccurredAt)
		}
		if left.OperationID != right.OperationID {
			return left.OperationID.String() < right.OperationID.String()
		}
		if left.EntryType != right.EntryType {
			return left.EntryType < right.EntryType
		}
		if left.AccountID != right.AccountID {
			return left.AccountID.String() < right.AccountID.String()
		}
		if left.PaymentID != right.PaymentID {
			return left.PaymentID.String() < right.PaymentID.String()
		}
		if left.AmountPaise != right.AmountPaise {
			return left.AmountPaise < right.AmountPaise
		}
		return left.Currency < right.Currency
	})
	if ordered == nil {
		return []CanonicalRecord{}
	}
	return ordered
}
