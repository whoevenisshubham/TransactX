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
	canonical       NodeRef
	canonicalHash   []byte
	participant     NodeRef
	participantHash []byte
}

type leafBucket struct {
	ref      NodeRef
	hash     []byte
	bucketID BucketID
}

type nodeRegion struct {
	node   NodeResult
	leaves []leafBucket
	start  time.Time
	end    time.Time
}

func regionKey(start, end time.Time) string {
	return fmt.Sprintf("%d_%d", start.UTC().UnixNano(), end.UTC().UnixNano())
}

func collectLeafBuckets(ctx context.Context, p ReconciliationParticipant, ref NodeRef, hash []byte) ([]leafBucket, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if ref.Path == emptyNodePath {
		return []leafBucket{}, nil
	}
	children, err := p.GetChildren(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("GetChildren on %s: %w", ref.Path, err)
	}
	if len(children) == 0 {
		bID, err := p.GetBucketID(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("GetBucketID on leaf %s: %w", ref.Path, err)
		}
		return []leafBucket{
			{
				ref:      ref,
				hash:     hash,
				bucketID: bID,
			},
		}, nil
	}
	var leaves []leafBucket
	for _, child := range children {
		childLeaves, err := collectLeafBuckets(ctx, p, child.Ref, child.Hash)
		if err != nil {
			return nil, err
		}
		leaves = append(leaves, childLeaves...)
	}
	return leaves, nil
}

func getChildRegions(ctx context.Context, p ReconciliationParticipant, children []NodeResult) ([]nodeRegion, error) {
	regions := make([]nodeRegion, len(children))
	for i, c := range children {
		leaves, err := collectLeafBuckets(ctx, p, c.Ref, c.Hash)
		if err != nil {
			return nil, err
		}
		var start, end time.Time
		if len(leaves) > 0 {
			start = leaves[0].bucketID.Start
			end = leaves[len(leaves)-1].bucketID.Start.Add(leaves[len(leaves)-1].bucketID.Width)
		}
		regions[i] = nodeRegion{
			node:   c,
			leaves: leaves,
			start:  start,
			end:    end,
		}
	}
	return regions, nil
}

func (engine *Engine) reconcileLeavesByBucketID(
	ctx context.Context,
	runID uuid.UUID,
	participantID string,
	canonicalParticipant ReconciliationParticipant,
	participantParticipant ReconciliationParticipant,
	canonLeaves []leafBucket,
	partLeaves []leafBucket,
	recordsInspected *int,
	divergentBuckets *int,
	divergentRecords *int,
) (int64, error) {
	var newDiscrepancies int64

	canonByBucket := make(map[string]leafBucket, len(canonLeaves))
	for _, l := range canonLeaves {
		canonByBucket[l.bucketID.String()] = l
	}

	partByBucket := make(map[string]leafBucket, len(partLeaves))
	for _, l := range partLeaves {
		partByBucket[l.bucketID.String()] = l
	}

	bucketKeySet := make(map[string]bool)
	for k := range canonByBucket {
		bucketKeySet[k] = true
	}
	for k := range partByBucket {
		bucketKeySet[k] = true
	}

	allBucketKeys := make([]string, 0, len(bucketKeySet))
	for k := range bucketKeySet {
		allBucketKeys = append(allBucketKeys, k)
	}
	sort.Strings(allBucketKeys)

	for _, key := range allBucketKeys {
		cL, inCanon := canonByBucket[key]
		pL, inPart := partByBucket[key]

		if inCanon && inPart {
			if bytes.Equal(cL.hash, pL.hash) {
				// Hashes equal: identical bucket on both sides, skip
				continue
			}
			*divergentBuckets++

			cRecords, cErr := canonicalParticipant.GetRecords(ctx, BucketRef{
				ParticipantID: cL.ref.ParticipantID,
				ScopeID:       cL.ref.ScopeID,
				Generation:    cL.ref.Generation,
				Key:           key,
			})
			if cErr != nil {
				return newDiscrepancies, fmt.Errorf("canonical GetRecords for bucket %s: %w", key, cErr)
			}

			pRecords, pErr := participantParticipant.GetRecords(ctx, BucketRef{
				ParticipantID: pL.ref.ParticipantID,
				ScopeID:       pL.ref.ScopeID,
				Generation:    pL.ref.Generation,
				Key:           key,
			})
			if pErr != nil {
				return newDiscrepancies, fmt.Errorf("participant GetRecords for bucket %s: %w", key, pErr)
			}

			*recordsInspected += len(cRecords) + len(pRecords)

			discs := compareBucketRecords(
				runID,
				participantID,
				cL.bucketID,
				cL.hash,
				pL.hash,
				cRecords,
				pRecords,
			)

			for _, disc := range discs {
				if _, saveErr := engine.store.SaveDiscrepancy(ctx, disc); saveErr != nil {
					return newDiscrepancies, fmt.Errorf("save discrepancy: %w", saveErr)
				}
				newDiscrepancies++
				*divergentRecords++
			}
		} else if inCanon && !inPart {
			// Bucket exists only on canonical side: record MismatchMissingRecord
			*divergentBuckets++

			cRecords, cErr := canonicalParticipant.GetRecords(ctx, BucketRef{
				ParticipantID: cL.ref.ParticipantID,
				ScopeID:       cL.ref.ScopeID,
				Generation:    cL.ref.Generation,
				Key:           key,
			})
			if cErr != nil {
				return newDiscrepancies, fmt.Errorf("canonical GetRecords for missing bucket %s: %w", key, cErr)
			}
			*recordsInspected += len(cRecords)

			disc := Discrepancy{
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        cL.bucketID.String(),
				BucketPartition:  cL.bucketID.Partition,
				BucketStart:      cL.bucketID.Start,
				BucketWidthNs:    int64(cL.bucketID.Width),
				ExpectedRoot:     cL.hash,
				ObservedRoot:     nil,
				MismatchCategory: MismatchMissingRecord,
				Evidence: map[string]string{
					"reason":    "node exists only in canonical ledger",
					"node_path": cL.ref.Path,
					"bucket_id": cL.bucketID.String(),
				},
				DetectedAt: time.Now().UTC(),
			}
			if _, saveErr := engine.store.SaveDiscrepancy(ctx, disc); saveErr != nil {
				return newDiscrepancies, fmt.Errorf("save discrepancy: %w", saveErr)
			}
			newDiscrepancies++
		} else if !inCanon && inPart {
			// Bucket exists only on participant side: record MismatchExtraRecord
			*divergentBuckets++

			pRecords, pErr := participantParticipant.GetRecords(ctx, BucketRef{
				ParticipantID: pL.ref.ParticipantID,
				ScopeID:       pL.ref.ScopeID,
				Generation:    pL.ref.Generation,
				Key:           key,
			})
			if pErr != nil {
				return newDiscrepancies, fmt.Errorf("participant GetRecords for extra bucket %s: %w", key, pErr)
			}
			*recordsInspected += len(pRecords)

			disc := Discrepancy{
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        pL.bucketID.String(),
				BucketPartition:  pL.bucketID.Partition,
				BucketStart:      pL.bucketID.Start,
				BucketWidthNs:    int64(pL.bucketID.Width),
				ExpectedRoot:     nil,
				ObservedRoot:     pL.hash,
				MismatchCategory: MismatchExtraRecord,
				Evidence: map[string]string{
					"reason":    "node exists only in participant ledger",
					"node_path": pL.ref.Path,
					"bucket_id": pL.bucketID.String(),
				},
				DetectedAt: time.Now().UTC(),
			}
			if _, saveErr := engine.store.SaveDiscrepancy(ctx, disc); saveErr != nil {
				return newDiscrepancies, fmt.Errorf("save discrepancy: %w", saveErr)
			}
			newDiscrepancies++
		}
	}

	return newDiscrepancies, nil
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
	// Queue item contains BOTH canonical NodeRef and participant NodeRef with their hashes.
	queue := []nodePair{
		{
			canonical:       canonicalRoot.Ref,
			canonicalHash:   canonicalRoot.Root,
			participant:     participantRoot.Ref,
			participantHash: participantRoot.Root,
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

		// Leaf level or one side is a leaf:
		if len(canonChildren) == 0 || len(partChildren) == 0 {
			cLeaves, err := collectLeafBuckets(ctx, canonicalParticipant, pair.canonical, pair.canonicalHash)
			if err != nil {
				return Run{}, err
			}
			pLeaves, err := collectLeafBuckets(ctx, participantParticipant, pair.participant, pair.participantHash)
			if err != nil {
				return Run{}, err
			}
			newDiscs, err := engine.reconcileLeavesByBucketID(
				ctx, runID, participantID, canonicalParticipant, participantParticipant,
				cLeaves, pLeaves,
				&recordsInspected, &divergentBuckets, &divergentRecords,
			)
			if err != nil {
				return Run{}, err
			}
			discrepancyCount += newDiscs
			continue
		}

		// Internal node level: both have children.
		// Group children by their logical ledger time region [start, end),
		// NOT by absolute implementation tree coordinates (NodeRef.Path).
		canonRegions, cErr := getChildRegions(ctx, canonicalParticipant, canonChildren)
		if cErr != nil {
			return Run{}, cErr
		}
		partRegions, pErr := getChildRegions(ctx, participantParticipant, partChildren)
		if pErr != nil {
			return Run{}, pErr
		}

		partByRegion := make(map[string]nodeRegion, len(partRegions))
		for _, pr := range partRegions {
			partByRegion[regionKey(pr.start, pr.end)] = pr
		}

		matchedPartRegions := make(map[string]bool)
		var unmatchedCanonLeaves []leafBucket

		for _, cr := range canonRegions {
			rk := regionKey(cr.start, cr.end)
			pr, found := partByRegion[rk]
			if found {
				matchedPartRegions[rk] = true
				if bytes.Equal(cr.node.Hash, pr.node.Hash) {
					// Hashes equal: entire subtree is identical, skip (O(1) pruning)
					continue
				}
				// Hashes differ: enqueue pair into work queue to descend
				queue = append(queue, nodePair{
					canonical:       cr.node.Ref,
					canonicalHash:   cr.node.Hash,
					participant:     pr.node.Ref,
					participantHash: pr.node.Hash,
				})
			} else {
				// No participant child covers this exact region: structurally incompatible
				unmatchedCanonLeaves = append(unmatchedCanonLeaves, cr.leaves...)
			}
		}

		var unmatchedPartLeaves []leafBucket
		for _, pr := range partRegions {
			rk := regionKey(pr.start, pr.end)
			if !matchedPartRegions[rk] {
				unmatchedPartLeaves = append(unmatchedPartLeaves, pr.leaves...)
			}
		}

		// At structurally incompatible subtrees, enumerate descendant leaf/bucket regions
		// on both sides and match them by logical BucketID.
		if len(unmatchedCanonLeaves) > 0 || len(unmatchedPartLeaves) > 0 {
			newDiscs, err := engine.reconcileLeavesByBucketID(
				ctx, runID, participantID, canonicalParticipant, participantParticipant,
				unmatchedCanonLeaves, unmatchedPartLeaves,
				&recordsInspected, &divergentBuckets, &divergentRecords,
			)
			if err != nil {
				return Run{}, err
			}
			discrepancyCount += newDiscs
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
