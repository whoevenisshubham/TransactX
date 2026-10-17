package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
)

var (
	ErrRateLimitExceeded = errors.New("rate limit exceeded (maximum 5 requests per minute)")
)

// EvidenceReader provides trusted read-only operational evidence extraction.
type EvidenceReader interface {
	ReadPaymentEvidence(ctx context.Context, trustedScope string, paymentID uuid.UUID) (*EvidenceBundle, error)
}

// ActorRateLimiter tracks per-actor request timestamps to enforce rate bounds.
type ActorRateLimiter struct {
	mu      sync.Mutex
	history map[uuid.UUID][]time.Time
	limit   int
	window  time.Duration
}

func NewActorRateLimiter(limit int, window time.Duration) *ActorRateLimiter {
	return &ActorRateLimiter{
		history: make(map[uuid.UUID][]time.Time),
		limit:   limit,
		window:  window,
	}
}

func (rl *ActorRateLimiter) Allow(actorID uuid.UUID) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now().UTC()
	cutoff := now.Add(-rl.window)

	// Filter timestamps outside window
	recent := []time.Time{}
	for _, t := range rl.history[actorID] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.limit {
		rl.history[actorID] = recent
		return false
	}

	recent = append(recent, now)
	rl.history[actorID] = recent
	return true
}

type Service struct {
	repo        *Repository
	adapters    map[uuid.UUID]bank.BankAdapter
	logger      *slog.Logger
	rateLimiter *ActorRateLimiter
}

func NewService(repo *Repository, adapters map[uuid.UUID]bank.BankAdapter, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repo:        repo,
		adapters:    adapters,
		logger:      logger,
		rateLimiter: NewActorRateLimiter(5, 1*time.Minute),
	}
}

// ReadPaymentEvidence implements the EvidenceReader contract.
func (s *Service) ReadPaymentEvidence(ctx context.Context, trustedScope string, paymentID uuid.UUID) (*EvidenceBundle, error) {
	// 3 seconds overall deadline
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	raw, err := s.repo.GetPaymentRawEvidence(timeoutCtx, paymentID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	warnings := []WarningSnapshot{}
	sources := []SourceSnapshot{
		{
			Name:       "central",
			Status:     "available",
			ObservedAt: &now,
			ErrorCode:  nil,
		},
	}

	routeStatus := "available"
	if raw.ExecutionTargetID == nil && raw.SourceBankCode == nil && raw.DestinationBankCode == nil {
		routeStatus = "not_applicable"
	}

	route := RouteSnapshot{
		SourceBankCode:      raw.SourceBankCode,
		DestinationBankCode: raw.DestinationBankCode,
		ExecutionTargetID:   raw.ExecutionTargetID,
		SelectedAt:          raw.RouteSelectedAt,
		ReasonCode:          raw.RouteReasonCode,
		Status:              routeStatus,
	}

	// 3. Process operations and participant presence
	operations := []OperationSnapshot{}
	seenSources := map[string]bool{"central": true}

	for i, rawOp := range raw.Operations {
		if i >= 20 {
			warnings = append(warnings, WarningSnapshot{
				Code:    "EXCESS_OPERATIONS",
				Message: "Payment contains more than 20 operations; display truncated to first 20",
			})
			break
		}

		opSnap := OperationSnapshot{
			OperationID:   rawOp.OperationID,
			OperationType: rawOp.OperationType,
			BankCode:      rawOp.BankCode,
			CentralStatus: rawOp.CentralStatus,
			Presence:      "not_checked",
			SourceStatus:  "not_applicable",
			EvidenceID:    fmt.Sprintf("operation:%s", rawOp.OperationID),
		}

		sourceName := fmt.Sprintf("participant:%s", rawOp.BankCode)

		// Check participant presence if adapter supports OperationPresenceReader
		if s.adapters != nil {
			if adapter, ok := s.adapters[rawOp.BankID]; ok {
				if presenceReader, ok := adapter.(bank.OperationPresenceReader); ok {
					pCtx, pCancel := context.WithTimeout(timeoutCtx, 500*time.Millisecond)
					presResult, pErr := presenceReader.GetOperationPresence(pCtx, paymentID, rawOp.OperationID)
					pCancel()

					observedTime := time.Now().UTC()
					opSnap.LastObservedAt = &observedTime

					if pErr == nil {
						if presResult.Presence == bank.PresencePresent {
							opSnap.Presence = "present"
							opSnap.SourceStatus = "available"
							statusStr := "SUCCEEDED"
							opSnap.ParticipantStatus = &statusStr
						} else if presResult.Presence == bank.PresenceAbsent {
							opSnap.Presence = "absent_confirmed"
							opSnap.SourceStatus = "available"
						} else {
							opSnap.Presence = "unknown"
							opSnap.SourceStatus = "unavailable"
							warnings = append(warnings, WarningSnapshot{
								Code:    "PARTIAL_EVIDENCE",
								Message: fmt.Sprintf("Destination evidence for %s returned ambiguous presence", rawOp.BankCode),
							})
						}
					} else {
						opSnap.Presence = "unknown"
						opSnap.SourceStatus = "unavailable"
						warnings = append(warnings, WarningSnapshot{
							Code:    "PARTIAL_EVIDENCE",
							Message: fmt.Sprintf("Destination evidence for %s is unavailable", rawOp.BankCode),
						})
					}

					if !seenSources[sourceName] {
						seenSources[sourceName] = true
						var errCode *string
						if opSnap.SourceStatus == "unavailable" {
							c := "PARTICIPANT_UNAVAILABLE"
							errCode = &c
						}
						sources = append(sources, SourceSnapshot{
							Name:       sourceName,
							Status:     opSnap.SourceStatus,
							ObservedAt: &observedTime,
							ErrorCode:  errCode,
						})
					}
				}
			}
		}

		operations = append(operations, opSnap)
	}

	// Transitions
	transitionsTruncated := false
	transitions := raw.Transitions
	if len(transitions) > 200 {
		transitionsTruncated = true
		transitions = transitions[:200]
	}

	// Recovery summary
	recSummary := RecoverySnapshot{
		Status:        "none",
		AttemptCount:  0,
		NextAttemptAt: nil,
		LastAttemptID: nil,
		ReasonCode:    nil,
		SourceStatus:  "available",
	}
	if raw.RecoveryJob != nil {
		recSummary.Status = raw.RecoveryJob.Status
		recSummary.AttemptCount = raw.RecoveryJob.AttemptCount
		recSummary.NextAttemptAt = raw.RecoveryJob.DueAt
		recSummary.LastAttemptID = raw.RecoveryJob.LastAttemptID
		recSummary.ReasonCode = raw.RecoveryJob.ReasonCode
	}

	// Reconciliation
	reconSnap := ReconciliationSnapshot{
		SourceStatus:     "not_collected",
		RunIDs:           raw.ReconRunIDs,
		DiscrepancyCount: nil,
		ScopeMismatch:    false,
	}
	if len(raw.ReconRunIDs) > 0 {
		reconSnap.SourceStatus = "available"
	}

	// Integrity
	integSnap := IntegritySnapshot{
		SourceStatus:   "not_collected",
		RunID:          raw.IntegrityRunID,
		OccurredAt:     raw.IntegrityOccurredAt,
		ViolationCount: raw.IntegrityViolations,
	}
	if raw.IntegrityRunID != nil {
		integSnap.SourceStatus = "available"
	}

	// Traces
	traceSnap := TracesSnapshot{
		SourceStatus: "not_collected",
		TraceIDs:     raw.TraceIDs,
		Truncated:    false,
	}
	if len(raw.TraceIDs) > 0 {
		traceSnap.SourceStatus = "available"
	}

	bundle := &EvidenceBundle{
		SchemaVersion: "tx.evidence.v1",
		PaymentID:     paymentID,
		SnapshotID:    "",
		CollectedAt:   now,
		Payment: PaymentSnapshot{
			State:        raw.State,
			AmountPaise:  strconv.FormatInt(raw.AmountPaise, 10),
			Currency:     raw.Currency,
			CreatedAt:    raw.CreatedAt,
			UpdatedAt:    raw.UpdatedAt,
			CompletedAt:  raw.CompletedAt,
			EventVersion: strconv.FormatInt(raw.EventVersion, 10),
			EvidenceID:   fmt.Sprintf("payment:%s", paymentID),
		},
		Route:                route,
		Operations:           operations,
		Transitions:          transitions,
		TransitionsTruncated: transitionsTruncated,
		Recovery:             recSummary,
		Reconciliation:       reconSnap,
		Integrity:            integSnap,
		Traces:               traceSnap,
		Sources:              sources,
		Warnings:             warnings,
	}

	if err := ComputeEvidenceSnapshotID(bundle); err != nil {
		return nil, fmt.Errorf("compute snapshot identity: %w", err)
	}

	return bundle, nil
}

// ComputeEvidenceSnapshotID hashes the bundle excluding snapshotId and unstable observation timestamps,
// and sets bundle.SnapshotID, bundle.Payment.EvidenceID, and bundle.Operations[].EvidenceID deterministically.
func ComputeEvidenceSnapshotID(bundle *EvidenceBundle) error {
	hashBundle := *bundle
	hashBundle.SnapshotID = ""
	hashBundle.CollectedAt = time.Time{}
	hashSources := make([]SourceSnapshot, len(bundle.Sources))
	for i, s := range bundle.Sources {
		s.ObservedAt = nil
		hashSources[i] = s
	}
	hashBundle.Sources = hashSources
	hashOps := make([]OperationSnapshot, len(bundle.Operations))
	for i, op := range bundle.Operations {
		op.LastObservedAt = nil
		hashOps[i] = op
	}
	hashBundle.Operations = hashOps

	bundleBytes, err := json.Marshal(hashBundle)
	if err != nil {
		return fmt.Errorf("marshal evidence for hash: %w", err)
	}
	sum := sha256.Sum256(bundleBytes)
	hashHex := hex.EncodeToString(sum[:])
	snapshotID := "sha256:" + hashHex

	bundle.SnapshotID = snapshotID
	bundle.Payment.EvidenceID = fmt.Sprintf("payment:%s:%s", bundle.PaymentID, hashHex)
	for i := range bundle.Operations {
		bundle.Operations[i].EvidenceID = fmt.Sprintf("operation:%s:%s", bundle.Operations[i].OperationID, hashHex)
	}
	return nil
}

// ExportPaymentEvidence produces a redacted evidence bundle paired with a cryptographic manifest.
func (s *Service) ExportPaymentEvidence(ctx context.Context, trustedScope string, paymentID uuid.UUID) (*EvidenceExportBundle, error) {
	bundle, err := s.ReadPaymentEvidence(ctx, trustedScope, paymentID)
	if err != nil {
		return nil, err
	}

	bundleBytes, err := json.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("marshal export bundle: %w", err)
	}
	sum := sha256.Sum256(bundleBytes)
	bundleSHA256 := hex.EncodeToString(sum[:])

	manifest := ExportManifest{
		PaymentID:    paymentID,
		SnapshotID:   bundle.SnapshotID,
		BundleSHA256: bundleSHA256,
		ExportedAt:   time.Now().UTC(),
		Algorithm:    "SHA-256",
		Note:         "Canonical redacted evidence export. No authenticity claim.",
	}

	return &EvidenceExportBundle{
		Bundle:   *bundle,
		Manifest: manifest,
	}, nil
}

// RecheckRecovery validates actor rate limit and invokes repository expedite logic.
func (s *Service) RecheckRecovery(ctx context.Context, actorID uuid.UUID, actorRole string, paymentID uuid.UUID, reason string, idempotencyKey string) (*RecoveryRecheckResult, error) {
	if !s.rateLimiter.Allow(actorID) {
		return nil, ErrRateLimitExceeded
	}
	return s.repo.RecheckRecovery(ctx, actorID, actorRole, paymentID, reason, idempotencyKey)
}

func (s *Service) GetWorklist(ctx context.Context, filter WorklistFilter) (*Worklist, error) {
	return s.repo.GetWorklist(ctx, filter)
}

func (s *Service) GetRecoveryAttempts(ctx context.Context, paymentID uuid.UUID, limit int, cursor string) (*RecoveryAttemptsPage, error) {
	return s.repo.GetRecoveryAttempts(ctx, paymentID, limit, cursor)
}
