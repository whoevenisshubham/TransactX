package reconciliation

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// KnownParticipants is the authoritative set of reconciliation participant IDs
// that the engine accepts. Clients cannot submit arbitrary values.
type KnownParticipants map[string]bool

// ParticipantFactory builds a ReconciliationParticipant for a given participant
// identity and normalized scope. Production wires separate canonical (central)
// and participant-side implementations.
type ParticipantFactory func(ctx context.Context, participantID string, scope Scope) (ReconciliationParticipant, error)

// RunStore is the persistence boundary for reconciliation runs and discrepancy
// evidence. The PostgreSQL-backed RunRepository implements this interface;
// tests inject an in-memory store.
type RunStore interface {
	CreateRun(ctx context.Context, participantID string, scope Scope) (Run, error)
	CompleteRun(ctx context.Context, runID uuid.UUID, canonicalRoot, participantRoot []byte, canonicalVersion, algorithmVersion string, recordCount, discrepancyCount int64) (Run, error)
	FailRun(ctx context.Context, runID uuid.UUID, errMsg string) (Run, error)
	GetRun(ctx context.Context, runID uuid.UUID) (Run, error)
	ListRuns(ctx context.Context, req ListRunsRequest) (RunListPage, error)
	SaveDiscrepancy(ctx context.Context, disc Discrepancy) (Discrepancy, error)
	ListDiscrepancies(ctx context.Context, req ListDiscrepanciesRequest) (DiscrepancyListPage, error)
}

// Compile-time check that RunRepository satisfies RunStore.
var _ RunStore = (*RunRepository)(nil)

// Engine orchestrates true two-sided reconciliation runs between the authoritative
// central PostgreSQL financial ledger (canonical side) and a bank participant's
// ledger (participant side). It uses the Merkle and canonical foundations from
// M3-0..M3-4 without mutating financial state.
type Engine struct {
	participants       KnownParticipants
	store              RunStore
	canonicalFactory   ParticipantFactory
	participantFactory ParticipantFactory
}

// NewEngineWithRepo creates an Engine with an injected RunStore and two distinct
// participant factories: one for the canonical source and one for the participant.
func NewEngineWithRepo(
	participants KnownParticipants,
	store RunStore,
	canonicalFactory ParticipantFactory,
	participantFactory ParticipantFactory,
) *Engine {
	return &Engine{
		participants:       participants,
		store:              store,
		canonicalFactory:   canonicalFactory,
		participantFactory: participantFactory,
	}
}

// NewEngine creates an Engine backed by the PostgreSQL RunRepository.
func NewEngine(
	participants KnownParticipants,
	repo *RunRepository,
	canonicalFactory ParticipantFactory,
	participantFactory ParticipantFactory,
) *Engine {
	return NewEngineWithRepo(participants, repo, canonicalFactory, participantFactory)
}

// RunRequest is the validated input for starting a reconciliation run.
type RunRequest struct {
	ParticipantID string
	ScopeFrom     time.Time
	ScopeTo       time.Time
}

// Execute orchestrates a two-sided reconciliation run. It creates a durable
// RUNNING record, loads canonical and participant commitments, traverses
// commitment trees, identifies all divergent regions, persists discrepancies,
// and completes the run with distinct canonical and participant roots.
func (engine *Engine) Execute(ctx context.Context, req RunRequest) (Run, error) {
	// 1. Validate participant identity against server-configured list.
	if !engine.participants[req.ParticipantID] {
		return Run{}, ErrInvalidParticipant
	}

	scope := Scope{From: req.ScopeFrom, To: req.ScopeTo}
	scope = scope.Normalize()
	if err := validateRepositoryScope(scope); err != nil {
		return Run{}, fmt.Errorf("%w: %v", ErrInvalidRunScope, err)
	}

	// 2. Persist a RUNNING record before any external IO.
	run, err := engine.store.CreateRun(ctx, req.ParticipantID, scope)
	if err != nil {
		return Run{}, fmt.Errorf("create run record: %w", err)
	}

	completedRun, execErr := engine.execute(ctx, run.ID, req.ParticipantID, scope)
	if execErr != nil {
		// Operational failure: transition the run to FAILED.
		// Use a background context so the fail-transition is not cancelled by
		// an already-cancelled request context.
		failCtx := context.Background()
		if _, failErr := engine.store.FailRun(failCtx, run.ID, execErr.Error()); failErr != nil {
			_ = failErr
		}
		return Run{}, execErr
	}
	return completedRun, nil
}

type nodePair struct {
	canonical   NodeRef
	participant NodeRef
}

func (engine *Engine) execute(ctx context.Context, runID uuid.UUID, participantID string, scope Scope) (Run, error) {
	// 3. Obtain two distinct read sources: canonical and participant.
	canonicalParticipant, err := engine.canonicalFactory(ctx, participantID, scope)
	if err != nil {
		return Run{}, fmt.Errorf("initialize canonical participant for %q: %w", participantID, err)
	}
	participantParticipant, err := engine.participantFactory(ctx, participantID, scope)
	if err != nil {
		return Run{}, fmt.Errorf("initialize participant %q: %w", participantID, err)
	}

	// 4. Both sources receive exactly the same normalized scope [From, To).
	canonicalRoot, err := canonicalParticipant.GetRoot(ctx, scope)
	if err != nil {
		return Run{}, fmt.Errorf("get canonical root for %q: %w", participantID, err)
	}
	participantRoot, err := participantParticipant.GetRoot(ctx, scope)
	if err != nil {
		return Run{}, fmt.Errorf("get participant root for %q: %w", participantID, err)
	}

	// 5. Validate commitment versions.
	if versionErr := ValidateCommitmentVersions(canonicalRoot.Version, canonicalRoot.Algorithm); versionErr != nil {
		disc := Discrepancy{
			RunID:            runID,
			ParticipantID:    participantID,
			BucketKey:        "global",
			BucketPartition:  participantID,
			BucketStart:      scope.From,
			BucketWidthNs:    int64(scope.To.Sub(scope.From)),
			ExpectedRoot:     canonicalRoot.Root,
			ObservedRoot:     participantRoot.Root,
			MismatchCategory: MismatchVersionIncompatible,
			Evidence: map[string]string{
				"error":            versionErr.Error(),
				"canonicalVersion": canonicalRoot.Version,
				"algorithmVersion": canonicalRoot.Algorithm,
			},
			DetectedAt: time.Now().UTC(),
		}
		if _, discErr := engine.store.SaveDiscrepancy(ctx, disc); discErr != nil {
			return Run{}, fmt.Errorf("save version mismatch discrepancy: %w", discErr)
		}
		return engine.store.CompleteRun(ctx, runID, canonicalRoot.Root, participantRoot.Root, canonicalRoot.Version, canonicalRoot.Algorithm, 0, 1)
	}

	// 6. Record metadata for summary reporting.
	meta, metaErr := canonicalParticipant.GetMetadata(ctx, scope)
	recordCount := int64(0)
	if metaErr == nil {
		recordCount = meta.RecordCount
	}

	// 7. Root comparison: if identical, run completes with zero discrepancies.
	if bytes.Equal(canonicalRoot.Root, participantRoot.Root) {
		return engine.store.CompleteRun(
			ctx,
			runID,
			canonicalRoot.Root,
			participantRoot.Root,
			canonicalRoot.Version,
			canonicalRoot.Algorithm,
			recordCount,
			0,
		)
	}

	// 8. Roots differ: initialize queue containing the root pair.
	// Queue item contains BOTH canonical NodeRef and participant NodeRef.
	queue := []nodePair{
		{
			canonical:   canonicalRoot.Ref,
			participant: participantRoot.Ref,
		},
	}

	var discrepancyCount int64
	var nodesVisited int
	var recordsInspected int
	var divergentBuckets int
	var divergentRecords int

	// 9. Two-sided breadth-first child traversal until queue is empty.
	// NEVER stop after the first mismatch.
	for len(queue) > 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Run{}, ctxErr
		}

		pair := queue[0]
		queue = queue[1:]
		nodesVisited++

		canonChildren, cErr := canonicalParticipant.GetChildren(ctx, pair.canonical)
		if cErr != nil {
			return Run{}, fmt.Errorf("canonical GetChildren on %s: %w", pair.canonical.Path, cErr)
		}
		partChildren, pErr := participantParticipant.GetChildren(ctx, pair.participant)
		if pErr != nil {
			return Run{}, fmt.Errorf("participant GetChildren on %s: %w", pair.participant.Path, pErr)
		}

		// Leaf level: both have zero children (level 0 bucket leaves)
		if len(canonChildren) == 0 && len(partChildren) == 0 {
			divergentBuckets++

			// Resolve bucket identity from whichever side is available
			bucketID, bErr := canonicalParticipant.GetBucketID(ctx, pair.canonical)
			if bErr != nil {
				bucketID, bErr = participantParticipant.GetBucketID(ctx, pair.participant)
				if bErr != nil {
					bucketID = BucketID{Partition: participantID, Start: scope.From, Width: scope.To.Sub(scope.From)}
				}
			}

			// Obtain canonical records using canonical bucket reference
			canonBucketKey := bucketID.String()
			if cID, cErr := canonicalParticipant.GetBucketID(ctx, pair.canonical); cErr == nil {
				canonBucketKey = cID.String()
			}
			canonRecords, _ := canonicalParticipant.GetRecords(ctx, BucketRef{
				ParticipantID: pair.canonical.ParticipantID,
				ScopeID:       pair.canonical.ScopeID,
				Generation:    pair.canonical.Generation,
				Key:           canonBucketKey,
			})

			// Obtain participant records using participant bucket reference
			partBucketKey := bucketID.String()
			if pID, pErr := participantParticipant.GetBucketID(ctx, pair.participant); pErr == nil {
				partBucketKey = pID.String()
			}
			partRecords, _ := participantParticipant.GetRecords(ctx, BucketRef{
				ParticipantID: pair.participant.ParticipantID,
				ScopeID:       pair.participant.ScopeID,
				Generation:    pair.participant.Generation,
				Key:           partBucketKey,
			})

			recordsInspected += len(canonRecords) + len(partRecords)

			// Detailed record-level comparison
			discs := compareBucketRecords(
				runID,
				participantID,
				bucketID,
				canonicalRoot.Root,
				participantRoot.Root,
				canonRecords,
				partRecords,
			)

			for _, disc := range discs {
				if _, saveErr := engine.store.SaveDiscrepancy(ctx, disc); saveErr != nil {
					return Run{}, fmt.Errorf("save discrepancy: %w", saveErr)
				}
				discrepancyCount++
				divergentRecords++
			}
			continue
		}

		// Internal node level: match children using deterministic node identity/path
		canonByPath := make(map[string]NodeResult, len(canonChildren))
		for _, c := range canonChildren {
			canonByPath[c.Ref.Path] = c
		}
		partByPath := make(map[string]NodeResult, len(partChildren))
		for _, p := range partChildren {
			partByPath[p.Ref.Path] = p
		}

		pathSet := make(map[string]bool)
		for p := range canonByPath {
			pathSet[p] = true
		}
		for p := range partByPath {
			pathSet[p] = true
		}
		allPaths := make([]string, 0, len(pathSet))
		for p := range pathSet {
			allPaths = append(allPaths, p)
		}
		sort.Strings(allPaths)

		for _, p := range allPaths {
			cChild, inCanon := canonByPath[p]
			pChild, inPart := partByPath[p]

			if inCanon && inPart {
				if bytes.Equal(cChild.Hash, pChild.Hash) {
					// Hashes equal: skip
					continue
				}
				// Hashes differ: enqueue canonical child + participant child
				queue = append(queue, nodePair{
					canonical:   cChild.Ref,
					participant: pChild.Ref,
				})
			} else if inCanon && !inPart {
				// Child exists only on canonical side: record discrepancy immediately
				divergentBuckets++
				bucketID, bErr := canonicalParticipant.GetBucketID(ctx, cChild.Ref)
				if bErr != nil {
					bucketID = BucketID{Partition: participantID, Start: scope.From, Width: scope.To.Sub(scope.From)}
				}
				cRecords, _ := canonicalParticipant.GetRecords(ctx, BucketRef{
					ParticipantID: cChild.Ref.ParticipantID,
					ScopeID:       cChild.Ref.ScopeID,
					Generation:    cChild.Ref.Generation,
					Key:           bucketID.String(),
				})
				recordsInspected += len(cRecords)
				disc := Discrepancy{
					RunID:            runID,
					ParticipantID:    participantID,
					BucketKey:        bucketID.String(),
					BucketPartition:  bucketID.Partition,
					BucketStart:      bucketID.Start,
					BucketWidthNs:    int64(bucketID.Width),
					ExpectedRoot:     cChild.Hash,
					ObservedRoot:     nil,
					MismatchCategory: MismatchMissingRecord,
					Evidence: map[string]string{
						"reason":    "node exists only in canonical ledger",
						"node_path": cChild.Ref.Path,
					},
					DetectedAt: time.Now().UTC(),
				}
				if _, saveErr := engine.store.SaveDiscrepancy(ctx, disc); saveErr != nil {
					return Run{}, fmt.Errorf("save discrepancy: %w", saveErr)
				}
				discrepancyCount++
			} else if !inCanon && inPart {
				// Child exists only on participant side: record discrepancy immediately
				divergentBuckets++
				bucketID, bErr := participantParticipant.GetBucketID(ctx, pChild.Ref)
				if bErr != nil {
					bucketID = BucketID{Partition: participantID, Start: scope.From, Width: scope.To.Sub(scope.From)}
				}
				pRecords, _ := participantParticipant.GetRecords(ctx, BucketRef{
					ParticipantID: pChild.Ref.ParticipantID,
					ScopeID:       pChild.Ref.ScopeID,
					Generation:    pChild.Ref.Generation,
					Key:           bucketID.String(),
				})
				recordsInspected += len(pRecords)
				disc := Discrepancy{
					RunID:            runID,
					ParticipantID:    participantID,
					BucketKey:        bucketID.String(),
					BucketPartition:  bucketID.Partition,
					BucketStart:      bucketID.Start,
					BucketWidthNs:    int64(bucketID.Width),
					ExpectedRoot:     nil,
					ObservedRoot:     pChild.Hash,
					MismatchCategory: MismatchExtraRecord,
					Evidence: map[string]string{
						"reason":    "node exists only in participant ledger",
						"node_path": pChild.Ref.Path,
					},
					DetectedAt: time.Now().UTC(),
				}
				if _, saveErr := engine.store.SaveDiscrepancy(ctx, disc); saveErr != nil {
					return Run{}, fmt.Errorf("save discrepancy: %w", saveErr)
				}
				discrepancyCount++
			}
		}
	}

	// Safety fallback: if roots differed but no leaf discrepancies were produced,
	// record the root discrepancy so divergence is never silently dropped.
	if discrepancyCount == 0 {
		disc := Discrepancy{
			RunID:            runID,
			ParticipantID:    participantID,
			BucketKey:        "global",
			BucketPartition:  participantID,
			BucketStart:      scope.From,
			BucketWidthNs:    int64(scope.To.Sub(scope.From)),
			ExpectedRoot:     canonicalRoot.Root,
			ObservedRoot:     participantRoot.Root,
			MismatchCategory: MismatchCanonicalRoot,
			Evidence: map[string]string{
				"reason":           "commitment root mismatch",
				"nodesVisited":     strconv.Itoa(nodesVisited),
				"recordsInspected": strconv.Itoa(recordsInspected),
			},
			DetectedAt: time.Now().UTC(),
		}
		if _, saveErr := engine.store.SaveDiscrepancy(ctx, disc); saveErr != nil {
			return Run{}, fmt.Errorf("save fallback root discrepancy: %w", saveErr)
		}
		discrepancyCount++
	}

	// 10. Complete run with distinct canonical and participant roots.
	return engine.store.CompleteRun(
		ctx,
		runID,
		canonicalRoot.Root,
		participantRoot.Root,
		canonicalRoot.Version,
		canonicalRoot.Algorithm,
		recordCount,
		discrepancyCount,
	)
}

// compareBucketRecords implements Rule 6 (LEAF / BUCKET COMPARISON).
// It compares canonical and participant record sets using canonical ordering,
// detecting missing records, extra records, and mutated fields.
func compareBucketRecords(
	runID uuid.UUID,
	participantID string,
	bucketID BucketID,
	canonHash, partHash []byte,
	canonRecords, partRecords []CanonicalRecord,
) []Discrepancy {
	var discrepancies []Discrepancy

	canonSorted := SortRecords(canonRecords)
	partSorted := SortRecords(partRecords)

	canonByOp := make(map[uuid.UUID]CanonicalRecord, len(canonSorted))
	for _, r := range canonSorted {
		canonByOp[r.OperationID] = r
	}
	partByOp := make(map[uuid.UUID]CanonicalRecord, len(partSorted))
	for _, r := range partSorted {
		partByOp[r.OperationID] = r
	}

	now := time.Now().UTC()

	// Detect missing participant records and record-level mutations
	for _, cRec := range canonSorted {
		pRec, exists := partByOp[cRec.OperationID]
		if !exists {
			discrepancies = append(discrepancies, Discrepancy{
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        bucketID.String(),
				BucketPartition:  bucketID.Partition,
				BucketStart:      bucketID.Start,
				BucketWidthNs:    int64(bucketID.Width),
				ExpectedRoot:     canonHash,
				ObservedRoot:     partHash,
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

		// Field-level comparison
		if cRec.AmountPaise != pRec.AmountPaise {
			discrepancies = append(discrepancies, Discrepancy{
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        bucketID.String(),
				BucketPartition:  bucketID.Partition,
				BucketStart:      bucketID.Start,
				BucketWidthNs:    int64(bucketID.Width),
				ExpectedRoot:     canonHash,
				ObservedRoot:     partHash,
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
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        bucketID.String(),
				BucketPartition:  bucketID.Partition,
				BucketStart:      bucketID.Start,
				BucketWidthNs:    int64(bucketID.Width),
				ExpectedRoot:     canonHash,
				ObservedRoot:     partHash,
				MismatchCategory: MismatchRecordDifference,
				Evidence: map[string]string{
					"field":        "currency",
					"expected":     cRec.Currency,
					"observed":     pRec.Currency,
					"operation_id": cRec.OperationID.String(),
				},
				DetectedAt: now,
			})
		}
		if cRec.EntryType != pRec.EntryType {
			discrepancies = append(discrepancies, Discrepancy{
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        bucketID.String(),
				BucketPartition:  bucketID.Partition,
				BucketStart:      bucketID.Start,
				BucketWidthNs:    int64(bucketID.Width),
				ExpectedRoot:     canonHash,
				ObservedRoot:     partHash,
				MismatchCategory: MismatchRecordDifference,
				Evidence: map[string]string{
					"field":        "entry_type",
					"expected":     cRec.EntryType,
					"observed":     pRec.EntryType,
					"operation_id": cRec.OperationID.String(),
				},
				DetectedAt: now,
			})
		}
		if cRec.PaymentID != pRec.PaymentID {
			discrepancies = append(discrepancies, Discrepancy{
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        bucketID.String(),
				BucketPartition:  bucketID.Partition,
				BucketStart:      bucketID.Start,
				BucketWidthNs:    int64(bucketID.Width),
				ExpectedRoot:     canonHash,
				ObservedRoot:     partHash,
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
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        bucketID.String(),
				BucketPartition:  bucketID.Partition,
				BucketStart:      bucketID.Start,
				BucketWidthNs:    int64(bucketID.Width),
				ExpectedRoot:     canonHash,
				ObservedRoot:     partHash,
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
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        bucketID.String(),
				BucketPartition:  bucketID.Partition,
				BucketStart:      bucketID.Start,
				BucketWidthNs:    int64(bucketID.Width),
				ExpectedRoot:     canonHash,
				ObservedRoot:     partHash,
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

	// Detect extra participant records
	for _, pRec := range partSorted {
		if _, exists := canonByOp[pRec.OperationID]; !exists {
			discrepancies = append(discrepancies, Discrepancy{
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        bucketID.String(),
				BucketPartition:  bucketID.Partition,
				BucketStart:      bucketID.Start,
				BucketWidthNs:    int64(bucketID.Width),
				ExpectedRoot:     canonHash,
				ObservedRoot:     partHash,
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

	// If hashes differ but record fields matched, flag bucket root mismatch
	if len(discrepancies) == 0 {
		discrepancies = append(discrepancies, Discrepancy{
			RunID:            runID,
			ParticipantID:    participantID,
			BucketKey:        bucketID.String(),
			BucketPartition:  bucketID.Partition,
			BucketStart:      bucketID.Start,
			BucketWidthNs:    int64(bucketID.Width),
			ExpectedRoot:     canonHash,
			ObservedRoot:     partHash,
			MismatchCategory: MismatchBucketRoot,
			Evidence: map[string]string{
				"reason": "bucket root hash mismatch with matching record count",
			},
			DetectedAt: now,
		})
	}

	return discrepancies
}

// GetRun returns one reconciliation run by its UUID.
func (engine *Engine) GetRun(ctx context.Context, runID uuid.UUID) (Run, error) {
	return engine.store.GetRun(ctx, runID)
}

// ListRuns returns a bounded page of runs matching the request parameters.
func (engine *Engine) ListRuns(ctx context.Context, req ListRunsRequest) (RunListPage, error) {
	return engine.store.ListRuns(ctx, req)
}

// ListDiscrepancies returns a bounded page of discrepancy evidence for a run.
func (engine *Engine) ListDiscrepancies(ctx context.Context, req ListDiscrepanciesRequest) (DiscrepancyListPage, error) {
	// Verify that the run exists first.
	if _, err := engine.store.GetRun(ctx, req.RunID); err != nil {
		return DiscrepancyListPage{}, err
	}
	return engine.store.ListDiscrepancies(ctx, req)
}
