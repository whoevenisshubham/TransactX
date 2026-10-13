package investigator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	ToolReadPaymentEvidence       = "read_payment_evidence"
	ToolReadRecoveryAttempts      = "read_recovery_attempts"
	ToolReadRelatedReconciliation = "read_related_reconciliation"
	ToolReadTraceContext          = "read_trace_context"
	ToolReadOperationalWindow     = "read_operational_window"
	ToolRetrieveRunbook           = "retrieve_runbook"
)

var (
	ErrUnknownTool          = errors.New("unknown investigator tool")
	ErrInvalidToolArguments = errors.New("invalid investigator tool arguments")
	ErrToolBudgetExceeded   = errors.New("investigator tool budget exceeded")
	ErrDuplicateToolCall    = errors.New("duplicate investigator tool call")
	ErrScopeViolation       = errors.New("investigator scope violation")
	traceIDPattern          = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	Payload     json.RawMessage
	EvidenceIds []string
	SourceHash  string
}

type ToolDependencies struct {
	Evidence       EvidenceReader
	Recovery       RecoveryReader
	Reconciliation ReconciliationReader
	Trace          TraceReader
	Analytics      AnalyticsReader
	Runbooks       RunbookRetriever
}

type ToolRegistry struct {
	dependencies ToolDependencies
}

func NewToolRegistry(dependencies ToolDependencies) (*ToolRegistry, error) {
	if dependencies.Evidence == nil || dependencies.Runbooks == nil {
		return nil, errors.New("evidence reader and runbook retriever are required")
	}
	return &ToolRegistry{dependencies: dependencies}, nil
}

type ToolSession struct {
	registry *ToolRegistry
	scope    TrustedScope
	evidence *EvidenceBundle
	maxCalls int
	seen     map[string]struct{}
	keys     []string
}

func NewToolSession(registry *ToolRegistry, scope TrustedScope, evidence *EvidenceBundle, maxCalls int, priorKeys []string) (*ToolSession, error) {
	if registry == nil || maxCalls < 1 || maxCalls > 8 {
		return nil, errors.New("invalid tool session configuration")
	}
	session := &ToolSession{registry: registry, scope: scope, evidence: evidence, maxCalls: maxCalls, seen: make(map[string]struct{})}
	for _, key := range priorKeys {
		if key == "" {
			return nil, errors.New("empty prior tool call key")
		}
		if _, duplicate := session.seen[key]; duplicate {
			return nil, errors.New("duplicate prior tool call key")
		}
		session.seen[key] = struct{}{}
		session.keys = append(session.keys, key)
	}
	if len(session.keys) > maxCalls {
		return nil, ErrToolBudgetExceeded
	}
	return session, nil
}

func (session *ToolSession) SetEvidence(evidence EvidenceBundle) {
	copy := evidence
	session.evidence = &copy
}

func (session *ToolSession) Execute(ctx context.Context, call ToolCall) (ToolResult, error) {
	key, err := toolCallKey(call)
	if err != nil {
		return ToolResult{}, fmt.Errorf("%w: %v", ErrInvalidToolArguments, err)
	}
	if _, duplicate := session.seen[key]; duplicate {
		return ToolResult{}, ErrDuplicateToolCall
	}
	if len(session.seen) >= session.maxCalls {
		return ToolResult{}, ErrToolBudgetExceeded
	}
	session.seen[key] = struct{}{}
	session.keys = append(session.keys, key)
	return session.registry.Execute(ctx, session.scope, session.evidence, call)
}

func (session *ToolSession) Keys() []string {
	return append([]string(nil), session.keys...)
}

func (registry *ToolRegistry) Execute(ctx context.Context, scope TrustedScope, evidence *EvidenceBundle, call ToolCall) (ToolResult, error) {
	if err := validateTrustedScope(scope); err != nil {
		return ToolResult{}, err
	}
	if len(call.Arguments) == 0 {
		call.Arguments = json.RawMessage(`{}`)
	}
	var payload any
	var evidenceIDs []string

	switch call.Name {
	case ToolReadPaymentEvidence:
		var arguments struct {
			PaymentId string `json:"paymentId"`
		}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolResult{}, err
		}
		if arguments.PaymentId != scope.SelectedPaymentID {
			return ToolResult{}, ErrScopeViolation
		}
		bundle, err := registry.dependencies.Evidence.ReadPaymentEvidence(ctx, scope, arguments.PaymentId)
		if err != nil {
			return ToolResult{}, err
		}
		if err := validateEvidenceBundle(scope, arguments.PaymentId, bundle); err != nil {
			return ToolResult{}, err
		}
		payload = bundle
		evidenceIDs = evidenceBundleIDs(bundle)

	case ToolReadRecoveryAttempts:
		if registry.dependencies.Recovery == nil {
			return ToolResult{}, errors.New("recovery reader is unavailable")
		}
		var arguments struct {
			PaymentId string `json:"paymentId"`
			Limit     int    `json:"limit"`
		}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolResult{}, err
		}
		if arguments.PaymentId != scope.SelectedPaymentID {
			return ToolResult{}, ErrScopeViolation
		}
		if arguments.Limit < 1 || arguments.Limit > 20 {
			return ToolResult{}, fmt.Errorf("%w: recovery limit", ErrInvalidToolArguments)
		}
		attempts, err := registry.dependencies.Recovery.ReadRecoveryAttempts(ctx, scope, arguments.PaymentId, arguments.Limit)
		if err != nil {
			return ToolResult{}, err
		}
		if len(attempts) > arguments.Limit {
			return ToolResult{}, errors.New("recovery reader exceeded requested limit")
		}
		for _, attempt := range attempts {
			if attempt.PaymentId != scope.SelectedPaymentID {
				return ToolResult{}, ErrScopeViolation
			}
		}
		payload = attempts

	case ToolReadRelatedReconciliation:
		if registry.dependencies.Reconciliation == nil || evidence == nil {
			return ToolResult{}, errors.New("reconciliation reader or core evidence is unavailable")
		}
		var arguments struct {
			PaymentId string `json:"paymentId"`
			RunId     string `json:"runId"`
		}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolResult{}, err
		}
		if arguments.PaymentId != scope.SelectedPaymentID || !containsString(evidence.Reconciliation.RunIds, arguments.RunId) {
			return ToolResult{}, ErrScopeViolation
		}
		result, err := registry.dependencies.Reconciliation.ReadRelatedReconciliation(ctx, scope, arguments.PaymentId, arguments.RunId)
		if err != nil {
			return ToolResult{}, err
		}
		if result.PaymentId != scope.SelectedPaymentID || result.RunId != arguments.RunId {
			return ToolResult{}, ErrScopeViolation
		}
		payload = result

	case ToolReadTraceContext:
		if registry.dependencies.Trace == nil || evidence == nil {
			return ToolResult{}, errors.New("trace reader or core evidence is unavailable")
		}
		var arguments struct {
			TraceId string `json:"traceId"`
		}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolResult{}, err
		}
		if !traceIDPattern.MatchString(arguments.TraceId) || !containsString(evidence.Traces.TraceIds, arguments.TraceId) {
			return ToolResult{}, ErrScopeViolation
		}
		trace, err := registry.dependencies.Trace.ReadTrace(ctx, scope, arguments.TraceId)
		if err != nil {
			return ToolResult{}, err
		}
		if trace.TraceId != arguments.TraceId || len(trace.Spans) > 200 {
			return ToolResult{}, ErrScopeViolation
		}
		payload = trace

	case ToolReadOperationalWindow:
		if registry.dependencies.Analytics == nil || evidence == nil {
			return ToolResult{}, errors.New("analytics reader or core evidence is unavailable")
		}
		var arguments struct {
			EntityType string `json:"entityType"`
			EntityId   string `json:"entityId"`
			From       string `json:"from"`
			To         string `json:"to"`
			Limit      int    `json:"limit"`
		}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolResult{}, err
		}
		if !allowedAnalyticsEntity(*evidence, arguments.EntityType, arguments.EntityId) {
			return ToolResult{}, ErrScopeViolation
		}
		from, fromErr := time.Parse(time.RFC3339Nano, arguments.From)
		to, toErr := time.Parse(time.RFC3339Nano, arguments.To)
		if fromErr != nil || toErr != nil || !to.After(from) || to.Sub(from) > 5*time.Hour || arguments.Limit < 1 || arguments.Limit > 60 {
			return ToolResult{}, fmt.Errorf("%w: analytics window", ErrInvalidToolArguments)
		}
		windows, err := registry.dependencies.Analytics.ReadWindows(ctx, scope, arguments.EntityType, arguments.EntityId, arguments.From, arguments.To, arguments.Limit)
		if err != nil {
			return ToolResult{}, err
		}
		if len(windows) > arguments.Limit {
			return ToolResult{}, errors.New("analytics reader exceeded requested limit")
		}
		for _, window := range windows {
			if window.EntityType != arguments.EntityType || window.EntityId != arguments.EntityId || !window.Advisory {
				return ToolResult{}, ErrScopeViolation
			}
		}
		payload = windows

	case ToolRetrieveRunbook:
		var arguments struct {
			Query string   `json:"query"`
			Tags  []string `json:"tags"`
			TopK  int      `json:"topK"`
		}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolResult{}, err
		}
		if err := validateRetrieval(scope, arguments.Query, arguments.Tags, arguments.TopK); err != nil {
			return ToolResult{}, fmt.Errorf("%w: %v", ErrInvalidToolArguments, err)
		}
		chunks, err := registry.dependencies.Runbooks.Retrieve(ctx, scope, arguments.Query, arguments.Tags, arguments.TopK)
		if err != nil {
			return ToolResult{}, err
		}
		if err := validateRetrievedChunks(scope, arguments.TopK, chunks); err != nil {
			return ToolResult{}, err
		}
		payload = chunks
		for _, chunk := range chunks {
			evidenceIDs = append(evidenceIDs, chunk.ID)
		}

	default:
		return ToolResult{}, ErrUnknownTool
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return ToolResult{}, fmt.Errorf("encode tool result: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return ToolResult{Payload: encoded, EvidenceIds: evidenceIDs, SourceHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

func decodeToolArguments(raw json.RawMessage, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidToolArguments, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing JSON", ErrInvalidToolArguments)
	}
	return nil
}

func toolCallKey(call ToolCall) (string, error) {
	var arguments any
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.UseNumber()
	if err := decoder.Decode(&arguments); err != nil {
		return "", err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", errors.New("trailing JSON")
	}
	canonical, err := json.Marshal(arguments)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte(call.Name+"\n"), canonical...))
	return hex.EncodeToString(digest[:]), nil
}

func validateTrustedScope(scope TrustedScope) error {
	if scope.ActorID == "" || (scope.Role != "OPS_ADMIN" && scope.Role != "OPS_VIEWER") || scope.SelectedPaymentID == "" {
		return ErrScopeViolation
	}
	return nil
}

func validateEvidenceBundle(scope TrustedScope, requestedPaymentID string, bundle EvidenceBundle) error {
	if requestedPaymentID != scope.SelectedPaymentID || bundle.PaymentId != requestedPaymentID || bundle.SchemaVersion != "tx.evidence.v1" {
		return ErrScopeViolation
	}
	if bundle.SnapshotId == "" || bundle.Payment.EvidenceId == "" || bundle.CollectedAt == "" {
		return errors.New("invalid evidence identity")
	}
	if len(bundle.Operations) > 20 || len(bundle.Transitions) > 200 || len(bundle.Reconciliation.RunIds) > 20 || len(bundle.Traces.TraceIds) > 20 || len(bundle.Sources) > 20 || len(bundle.Warnings) > 20 {
		return errors.New("evidence bundle exceeds shared contract bounds")
	}
	if _, err := time.Parse(time.RFC3339Nano, bundle.CollectedAt); err != nil {
		return errors.New("invalid evidence collection time")
	}
	seen := map[string]struct{}{bundle.Payment.EvidenceId: {}}
	for _, operation := range bundle.Operations {
		if operation.EvidenceId == "" || operation.OperationId == "" {
			return errors.New("invalid operation evidence identity")
		}
		if _, duplicate := seen[operation.EvidenceId]; duplicate {
			return errors.New("duplicate evidence identity")
		}
		seen[operation.EvidenceId] = struct{}{}
		if !validSourceStatus(operation.SourceStatus) {
			return errors.New("invalid operation source status")
		}
	}
	centralAvailable := false
	for _, source := range bundle.Sources {
		if source.Name == "" || !validSourceStatus(source.Status) {
			return errors.New("invalid evidence source")
		}
		if source.Name == "central" && source.Status == "available" {
			centralAvailable = true
		}
	}
	if !centralAvailable {
		return errors.New("central evidence is unavailable")
	}
	return nil
}

func validSourceStatus(status SourceStatus) bool {
	switch status {
	case "available", "unavailable", "not_applicable", "not_collected":
		return true
	default:
		return false
	}
}

func evidenceBundleIDs(bundle EvidenceBundle) []string {
	ids := []string{bundle.Payment.EvidenceId}
	for _, operation := range bundle.Operations {
		ids = append(ids, operation.EvidenceId)
	}
	for _, transition := range bundle.Transitions {
		if transition.EvidenceId != "" {
			ids = append(ids, transition.EvidenceId)
		}
	}
	return ids
}

func validateRetrievedChunks(scope TrustedScope, topK int, chunks []RunbookChunk) error {
	if len(chunks) > topK {
		return errors.New("retriever returned too many chunks")
	}
	seen := make(map[string]struct{}, len(chunks))
	for _, chunk := range chunks {
		if chunk.ID == "" || chunk.Version == "" || !containsString(chunk.AllowedRoles, scope.Role) || len(strings.Fields(chunk.Content)) > maxChunkWords {
			return errors.New("retriever returned invalid chunk")
		}
		if !corpusHashPattern.MatchString(chunk.SHA256) || sha256Hex([]byte(chunk.Content)) != chunk.SHA256 || !corpusHashPattern.MatchString(chunk.SourceSHA256) {
			return errors.New("retriever returned invalid chunk hash")
		}
		if _, duplicate := seen[chunk.ID]; duplicate {
			return errors.New("retriever returned duplicate chunk")
		}
		seen[chunk.ID] = struct{}{}
	}
	return nil
}

func allowedAnalyticsEntity(bundle EvidenceBundle, entityType, entityID string) bool {
	switch entityType {
	case "bank":
		return (bundle.Route.SourceBankCode != nil && *bundle.Route.SourceBankCode == entityID) ||
			(bundle.Route.DestinationBankCode != nil && *bundle.Route.DestinationBankCode == entityID)
	case "execution_target":
		return bundle.Route.ExecutionTargetId != nil && *bundle.Route.ExecutionTargetId == entityID
	default:
		return false
	}
}
