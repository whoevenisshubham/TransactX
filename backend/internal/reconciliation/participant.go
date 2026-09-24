// Package reconciliation contains the research-facing participant contract.
// It is deliberately separate from bank.BankAdapter, which remains the
// payment-switch boundary.
package reconciliation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidScope           = errors.New("invalid reconciliation scope")
	ErrInvalidNodeReference   = errors.New("invalid reconciliation node reference")
	ErrNodeNotFound           = errors.New("reconciliation node not found")
	ErrInvalidBucketReference = errors.New("invalid reconciliation bucket reference")
	ErrBucketNotFound         = errors.New("reconciliation bucket not found")
	ErrParticipantMismatch    = errors.New("reconciliation participant mismatch")
	ErrCommitmentUnavailable  = errors.New("reconciliation commitment is not initialized")
	ErrStaleReference         = errors.New("stale reconciliation commitment reference")
)

// Scope identifies the logical ledger interval requested from a participant.
// Implementations must apply the same half-open interval [From, To) to both
// participants in a reconciliation run.
type Scope struct {
	From time.Time
	To   time.Time
}

// Validate applies the shared half-open [From, To) scope semantics. Zero
// endpoints remain valid for in-memory callers that intentionally represent an
// unbounded side; concrete repository readers may require explicit endpoints.
func (scope Scope) Validate() error {
	if !scope.From.IsZero() && !scope.To.IsZero() && !scope.To.After(scope.From) {
		return fmt.Errorf("%w: To must be after From", ErrInvalidScope)
	}
	return nil
}

func (scope Scope) Normalize() Scope {
	scope.From = scope.From.UTC()
	scope.To = scope.To.UTC()
	return scope
}

// ScopeIdentity is the deterministic wire identity carried by node and bucket
// references. It contains no participant/database identifiers.
func ScopeIdentity(scope Scope) string {
	scope = scope.Normalize()
	return scope.From.Format(time.RFC3339Nano) + "|" + scope.To.Format(time.RFC3339Nano)
}

func parseScopeIdentity(identity string) (Scope, error) {
	parts := strings.Split(identity, "|")
	if len(parts) != 2 {
		return Scope{}, ErrInvalidScope
	}
	from, err := parseScopeTime(parts[0])
	if err != nil {
		return Scope{}, err
	}
	to, err := parseScopeTime(parts[1])
	if err != nil {
		return Scope{}, err
	}
	scope := Scope{From: from, To: to}
	if err := scope.Validate(); err != nil {
		return Scope{}, err
	}
	return scope, nil
}

func parseScopeTime(value string) (time.Time, error) {
	if value == "0001-01-01T00:00:00Z" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: malformed scope timestamp", ErrInvalidScope)
	}
	return parsed.UTC(), nil
}

// RootResult is the participant's current commitment for a scope. The root is
// opaque at this layer; tree construction is intentionally deferred to M3-2.
type RootResult struct {
	Root      []byte
	Algorithm string
	Version   string
	Ref       NodeRef
}

// NodeRef identifies a commitment node for the future hierarchical tree.
type NodeRef struct {
	ParticipantID string
	ScopeID       string
	Generation    string
	Path          string
}

// NodeResult is a child commitment returned for a node reference.
type NodeResult struct {
	Ref  NodeRef
	Hash []byte
}

// BucketRef identifies a deterministic ledger bucket. Bucket allocation is
// defined by the Merkle layer; this type keeps the participant boundary typed.
type BucketRef struct {
	ParticipantID string
	ScopeID       string
	Generation    string
	Key           string
}

// ParticipantMetadata describes the source of a reconciliation snapshot
// without becoming part of a canonical transaction record.
type ParticipantMetadata struct {
	ParticipantID string
	CapturedAt    time.Time
	RecordCount   int64
}

// ReconciliationParticipant is the research/reconciliation read boundary.
// BankAdapter must not be expanded with these methods.
type ReconciliationParticipant interface {
	GetRoot(context.Context, Scope) (RootResult, error)
	GetChildren(context.Context, NodeRef) ([]NodeResult, error)
	GetRecords(context.Context, BucketRef) ([]CanonicalRecord, error)
	GetBucketID(context.Context, NodeRef) (BucketID, error)
	GetMetadata(context.Context, Scope) (ParticipantMetadata, error)
}
