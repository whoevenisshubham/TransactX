// Package reconciliation contains the research-facing participant contract.
// It is deliberately separate from bank.BankAdapter, which remains the
// payment-switch boundary.
package reconciliation

import (
	"context"
	"time"
)

// Scope identifies the logical ledger interval requested from a participant.
// Implementations must apply the same half-open interval [From, To) to both
// participants in a reconciliation run.
type Scope struct {
	From time.Time
	To   time.Time
}

// RootResult is the participant's current commitment for a scope. The root is
// opaque at this layer; tree construction is intentionally deferred to M3-2.
type RootResult struct {
	Root      []byte
	Algorithm string
	Version   string
}

// NodeRef identifies a commitment node for the future hierarchical tree.
type NodeRef struct {
	ScopeID string
	Path    string
}

// NodeResult is a child commitment returned for a node reference.
type NodeResult struct {
	Ref  NodeRef
	Hash []byte
}

// BucketRef identifies a deterministic ledger bucket. Bucket allocation is
// defined by the Merkle layer; this type keeps the participant boundary typed.
type BucketRef struct {
	ScopeID string
	Key     string
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
	GetMetadata(context.Context, Scope) (ParticipantMetadata, error)
}
