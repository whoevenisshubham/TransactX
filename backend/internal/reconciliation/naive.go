package reconciliation

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// NaiveInstrumentation records explicit execution metrics for the naive baseline.
type NaiveInstrumentation struct {
	RecordsConsidered  int64         `json:"recordsConsidered"`
	RecordsCompared    int64         `json:"recordsCompared"`
	DiscrepanciesCount int64         `json:"discrepanciesCount"`
	Duration           time.Duration `json:"durationNs"`
}

// NaiveResult contains the outcome and instrumentation of a naive reconciliation run.
type NaiveResult struct {
	ParticipantID   string               `json:"participantId"`
	Scope           Scope                `json:"scope"`
	Discrepancies   []Discrepancy        `json:"discrepancies"`
	Instrumentation NaiveInstrumentation `json:"instrumentation"`
}

// NaiveReconciler implements the M3-6 naive reconciliation baseline.
// It is used exclusively for baseline research, correctness validation,
// and benchmark comparison against the optimized Merkle reconciliation engine.
//
// In strict adherence to M3-6 requirements, this implementation MUST NOT use:
//   - Merkle root equality as a shortcut
//   - Subtree hash pruning
//   - Divergent-region queue traversal
//   - Commitment-tree localization
//
// It performs a full, complete ordered comparison across all logical records
// within the normalized scope [From, To) for both canonical and participant datasets.
type NaiveReconciler struct {
	bucketWidth time.Duration
}

// NewNaiveReconciler creates a NaiveReconciler with the specified bucket width.
// If bucketWidth <= 0, the standard 1-hour width is used.
func NewNaiveReconciler(bucketWidth time.Duration) *NaiveReconciler {
	if bucketWidth <= 0 {
		bucketWidth = time.Hour
	}
	return &NaiveReconciler{bucketWidth: bucketWidth}
}

// Reconcile executes a full naive reconciliation comparing the complete ordered
// logical record sets between canonical and participant sides for the given scope.
func (n *NaiveReconciler) Reconcile(
	ctx context.Context,
	participantID string,
	scope Scope,
	canonicalRecords []CanonicalRecord,
	participantRecords []CanonicalRecord,
) (NaiveResult, error) {
	start := time.Now()

	if err := ctx.Err(); err != nil {
		return NaiveResult{}, err
	}

	scope = scope.Normalize()
	if err := validateRepositoryScope(scope); err != nil {
		return NaiveResult{}, fmt.Errorf("%w: %v", ErrInvalidRunScope, err)
	}

	// 1. Filter records strictly within normalized half-open interval [From, To)
	var filteredCanon []CanonicalRecord
	for _, r := range canonicalRecords {
		occ := r.OccurredAt.UTC()
		if !occ.Before(scope.From) && occ.Before(scope.To) {
			filteredCanon = append(filteredCanon, r.Normalize())
		}
	}

	var filteredPart []CanonicalRecord
	for _, r := range participantRecords {
		occ := r.OccurredAt.UTC()
		if !occ.Before(scope.From) && occ.Before(scope.To) {
			filteredPart = append(filteredPart, r.Normalize())
		}
	}

	recordsConsidered := int64(len(filteredCanon) + len(filteredPart))

	// 2. Sort both sets deterministically using canonical record ordering
	canonSorted := SortRecords(filteredCanon)
	partSorted := SortRecords(filteredPart)

	// 3. Index by OperationID
	canonByOp := make(map[uuid.UUID]CanonicalRecord, len(canonSorted))
	for _, r := range canonSorted {
		canonByOp[r.OperationID] = r
	}
	partByOp := make(map[uuid.UUID]CanonicalRecord, len(partSorted))
	for _, r := range partSorted {
		partByOp[r.OperationID] = r
	}

	var discrepancies []Discrepancy
	var recordsCompared int64
	now := time.Now().UTC()

	// 4. Compare all canonical records against participant records
	for _, cRec := range canonSorted {
		recordsCompared++
		bID, err := BucketForRecord(cRec, participantID, n.bucketWidth)
		if err != nil {
			bID = BucketID{
				Partition: participantID,
				Start:     cRec.OccurredAt.Truncate(n.bucketWidth),
				Width:     n.bucketWidth,
			}
		}

		pRec, exists := partByOp[cRec.OperationID]
		if !exists {
			discrepancies = append(discrepancies, Discrepancy{
				ParticipantID:    participantID,
				BucketKey:        bID.String(),
				BucketPartition:  bID.Partition,
				BucketStart:      bID.Start,
				BucketWidthNs:    int64(bID.Width),
				MismatchCategory: MismatchMissingRecord,
				Evidence: map[string]string{
					"reason":       "missing participant record",
					"operation_id": cRec.OperationID.String(),
					"payment_id":   cRec.PaymentID.String(),
					"account_id":   cRec.AccountID.String(),
					"entry_type":   cRec.EntryType,
					"amount_paise": strconv.FormatInt(cRec.AmountPaise, 10),
					"currency":     cRec.Currency,
					"occurred_at":  cRec.OccurredAt.Format(time.RFC3339Nano),
				},
				DetectedAt: now,
			})
			continue
		}

		// Field differences
		if cRec.AmountPaise != pRec.AmountPaise {
			discrepancies = append(discrepancies, Discrepancy{
				ParticipantID:    participantID,
				BucketKey:        bID.String(),
				BucketPartition:  bID.Partition,
				BucketStart:      bID.Start,
				BucketWidthNs:    int64(bID.Width),
				MismatchCategory: MismatchRecordDifference,
				Evidence: map[string]string{
					"field":        "amount_paise",
					"expected":     strconv.FormatInt(cRec.AmountPaise, 10),
					"observed":     strconv.FormatInt(pRec.AmountPaise, 10),
					"operation_id": cRec.OperationID.String(),
					"payment_id":   cRec.PaymentID.String(),
				},
				DetectedAt: now,
			})
		}
		if cRec.Currency != pRec.Currency {
			discrepancies = append(discrepancies, Discrepancy{
				ParticipantID:    participantID,
				BucketKey:        bID.String(),
				BucketPartition:  bID.Partition,
				BucketStart:      bID.Start,
				BucketWidthNs:    int64(bID.Width),
				MismatchCategory: MismatchRecordDifference,
				Evidence: map[string]string{
					"field":        "currency",
					"expected":     cRec.Currency,
					"observed":     pRec.Currency,
					"operation_id": cRec.OperationID.String(),
					"payment_id":   cRec.PaymentID.String(),
				},
				DetectedAt: now,
			})
		}
		if cRec.EntryType != pRec.EntryType {
			discrepancies = append(discrepancies, Discrepancy{
				ParticipantID:    participantID,
				BucketKey:        bID.String(),
				BucketPartition:  bID.Partition,
				BucketStart:      bID.Start,
				BucketWidthNs:    int64(bID.Width),
				MismatchCategory: MismatchRecordDifference,
				Evidence: map[string]string{
					"field":        "entry_type",
					"expected":     cRec.EntryType,
					"observed":     pRec.EntryType,
					"operation_id": cRec.OperationID.String(),
					"payment_id":   cRec.PaymentID.String(),
				},
				DetectedAt: now,
			})
		}
		if cRec.PaymentID != pRec.PaymentID {
			discrepancies = append(discrepancies, Discrepancy{
				ParticipantID:    participantID,
				BucketKey:        bID.String(),
				BucketPartition:  bID.Partition,
				BucketStart:      bID.Start,
				BucketWidthNs:    int64(bID.Width),
				MismatchCategory: MismatchRecordDifference,
				Evidence: map[string]string{
					"field":        "payment_id",
					"expected":     cRec.PaymentID.String(),
					"observed":     pRec.PaymentID.String(),
					"operation_id": cRec.OperationID.String(),
				},
				DetectedAt: now,
			})
		}
		if cRec.AccountID != pRec.AccountID {
			discrepancies = append(discrepancies, Discrepancy{
				ParticipantID:    participantID,
				BucketKey:        bID.String(),
				BucketPartition:  bID.Partition,
				BucketStart:      bID.Start,
				BucketWidthNs:    int64(bID.Width),
				MismatchCategory: MismatchRecordDifference,
				Evidence: map[string]string{
					"field":        "account_id",
					"expected":     cRec.AccountID.String(),
					"observed":     pRec.AccountID.String(),
					"operation_id": cRec.OperationID.String(),
				},
				DetectedAt: now,
			})
		}
		if !cRec.OccurredAt.Equal(pRec.OccurredAt) {
			discrepancies = append(discrepancies, Discrepancy{
				ParticipantID:    participantID,
				BucketKey:        bID.String(),
				BucketPartition:  bID.Partition,
				BucketStart:      bID.Start,
				BucketWidthNs:    int64(bID.Width),
				MismatchCategory: MismatchRecordDifference,
				Evidence: map[string]string{
					"field":        "occurred_at",
					"expected":     cRec.OccurredAt.Format(time.RFC3339Nano),
					"observed":     pRec.OccurredAt.Format(time.RFC3339Nano),
					"operation_id": cRec.OperationID.String(),
				},
				DetectedAt: now,
			})
		}
	}

	// 5. Compare participant records to detect extra participant records
	for _, pRec := range partSorted {
		recordsCompared++
		if _, exists := canonByOp[pRec.OperationID]; !exists {
			bID, err := BucketForRecord(pRec, participantID, n.bucketWidth)
			if err != nil {
				bID = BucketID{
					Partition: participantID,
					Start:     pRec.OccurredAt.Truncate(n.bucketWidth),
					Width:     n.bucketWidth,
				}
			}
			discrepancies = append(discrepancies, Discrepancy{
				ParticipantID:    participantID,
				BucketKey:        bID.String(),
				BucketPartition:  bID.Partition,
				BucketStart:      bID.Start,
				BucketWidthNs:    int64(bID.Width),
				MismatchCategory: MismatchExtraRecord,
				Evidence: map[string]string{
					"reason":       "extra participant record",
					"operation_id": pRec.OperationID.String(),
					"payment_id":   pRec.PaymentID.String(),
					"account_id":   pRec.AccountID.String(),
					"entry_type":   pRec.EntryType,
					"amount_paise": strconv.FormatInt(pRec.AmountPaise, 10),
					"currency":     pRec.Currency,
					"occurred_at":  pRec.OccurredAt.Format(time.RFC3339Nano),
				},
				DetectedAt: now,
			})
		}
	}

	elapsed := time.Since(start)

	return NaiveResult{
		ParticipantID: participantID,
		Scope:         scope,
		Discrepancies: discrepancies,
		Instrumentation: NaiveInstrumentation{
			RecordsConsidered:  recordsConsidered,
			RecordsCompared:    recordsCompared,
			DiscrepanciesCount: int64(len(discrepancies)),
			Duration:           elapsed,
		},
	}, nil
}
