package http

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/reconciliation"
)

// reconciliationCreateRequest is the POST /api/ops/reconciliation/runs request body.
// Participant and scope are explicit; clients cannot select arbitrary SQL or
// database paths.
type reconciliationCreateRequest struct {
	ParticipantID string `json:"participantId"`
	ScopeFrom     string `json:"scopeFrom"` // RFC3339
	ScopeTo       string `json:"scopeTo"`   // RFC3339
}

func (handler *Handler) reconciliationCreateRun(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	var req reconciliationCreateRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "malformed request payload", http.StatusBadRequest))
		return
	}

	req.ParticipantID = strings.TrimSpace(req.ParticipantID)
	if req.ParticipantID == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "participantId is required", http.StatusBadRequest))
		return
	}

	scopeFrom, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScopeFrom))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeFrom must be an RFC3339 timestamp", http.StatusBadRequest))
		return
	}
	scopeTo, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScopeTo))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeTo must be an RFC3339 timestamp", http.StatusBadRequest))
		return
	}
	if !scopeTo.After(scopeFrom) {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeTo must be after scopeFrom", http.StatusBadRequest))
		return
	}

	run, execErr := handler.reconEngine.Execute(request.Context(), reconciliation.RunRequest{
		ParticipantID: req.ParticipantID,
		ScopeFrom:     scopeFrom,
		ScopeTo:       scopeTo,
	})
	if execErr != nil {
		if errors.Is(execErr, reconciliation.ErrInvalidParticipant) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "unknown or invalid participant", http.StatusBadRequest))
			return
		}
		if errors.Is(execErr, reconciliation.ErrInvalidRunScope) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", execErr.Error(), http.StatusBadRequest))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("reconciliation run failed", "participant_id", req.ParticipantID, "error", execErr)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "reconciliation run failed", http.StatusInternalServerError))
		return
	}
	if handler.integrityCoordinator != nil {
		handler.integrityCoordinator.Trigger(reconciliation.IntegrityEventReconciliation)
	}

	writeData(writer, http.StatusCreated, request, sanitizeRun(run))
}

func (handler *Handler) reconciliationListRuns(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeData(writer, http.StatusOK, request, emptyRunPage())
		return
	}

	participantID := strings.TrimSpace(request.URL.Query().Get("participantId"))
	limit := parseIntParam(request, "limit", 20)
	offset := parseIntParam(request, "offset", 0)

	page, err := handler.reconEngine.ListRuns(request.Context(), reconciliation.ListRunsRequest{
		ParticipantID: participantID,
		Limit:         limit,
		Offset:        offset,
	})
	if err != nil {
		if handler.logger != nil {
			handler.logger.Error("list reconciliation runs", "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to list reconciliation runs", http.StatusInternalServerError))
		return
	}

	respPage := map[string]any{
		"items": sanitizeRuns(page.Items),
		"total": page.Total,
		"limit": page.Limit,
	}
	if page.NextOffset != nil {
		respPage["nextOffset"] = *page.NextOffset
	}
	writeData(writer, http.StatusOK, request, respPage)
}

func (handler *Handler) reconciliationGetRun(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	runIDStr := strings.TrimSpace(request.PathValue("runID"))
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "runID must be a valid UUID", http.StatusBadRequest))
		return
	}

	run, err := handler.reconEngine.GetRun(request.Context(), runID)
	if err != nil {
		if errors.Is(err, reconciliation.ErrRunNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation run not found", http.StatusNotFound))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("get reconciliation run", "run_id", runID, "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to get reconciliation run", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, sanitizeRun(run))
}

func (handler *Handler) reconciliationListDiscrepancies(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	runIDStr := strings.TrimSpace(request.PathValue("runID"))
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "runID must be a valid UUID", http.StatusBadRequest))
		return
	}

	limit := parseIntParam(request, "limit", 50)
	offset := parseIntParam(request, "offset", 0)

	page, err := handler.reconEngine.ListDiscrepancies(request.Context(), reconciliation.ListDiscrepanciesRequest{
		RunID:  runID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		if errors.Is(err, reconciliation.ErrRunNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation run not found", http.StatusNotFound))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("list discrepancies", "run_id", runID, "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to list discrepancies", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, page)
}

// parseIntParam reads an integer query parameter, falling back to the default.
func parseIntParam(request *http.Request, name string, defaultValue int) int {
	raw := strings.TrimSpace(request.URL.Query().Get(name))
	if raw == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return defaultValue
	}
	return v
}

// sanitizeRun removes internal-only fields that should not be exposed via the
// public API. Raw byte hashes are encoded as hex strings in the JSON response.
func sanitizeRun(run reconciliation.Run) map[string]any {
	out := map[string]any{
		"id":               run.ID,
		"participantId":    run.ParticipantID,
		"scopeFrom":        run.ScopeFrom.UTC().Format(time.RFC3339),
		"scopeTo":          run.ScopeTo.UTC().Format(time.RFC3339),
		"status":           run.Status,
		"recordCount":      run.RecordCount,
		"discrepancyCount": run.DiscrepancyCount,
		"elapsedNs":        run.ElapsedNs,
		"nodesVisited":     run.NodesVisited,
		"recordsInspected": run.RecordsInspected,
		"bytesExamined":    run.BytesExamined,
		"divergentBuckets": run.DivergentBuckets,
		"divergentRecords": run.DivergentRecords,
		"startedAt":        run.StartedAt.UTC().Format(time.RFC3339Nano),
	}
	if run.CompletedAt != nil {
		out["completedAt"] = run.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	if len(run.CanonicalRoot) > 0 {
		out["canonicalRootHex"] = hexEncode(run.CanonicalRoot)
	}
	if run.CanonicalVersion != "" {
		out["canonicalVersion"] = run.CanonicalVersion
	}
	if run.AlgorithmVersion != "" {
		out["algorithmVersion"] = run.AlgorithmVersion
	}
	if run.ErrorMessage != "" {
		out["errorMessage"] = run.ErrorMessage
	}
	return out
}

func sanitizeRuns(runs []reconciliation.Run) []map[string]any {
	out := make([]map[string]any, len(runs))
	for i, run := range runs {
		out[i] = sanitizeRun(run)
	}
	return out
}

func emptyRunPage() map[string]any {
	return map[string]any{
		"items": []any{},
		"total": 0,
		"limit": 20,
	}
}

func hexEncode(b []byte) string {
	const hextable = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = hextable[v>>4]
		dst[i*2+1] = hextable[v&0x0f]
	}
	return string(dst)
}

type publicProofRecord struct {
	OperationID uuid.UUID `json:"operationId"`
	PaymentID   uuid.UUID `json:"paymentId"`
	AccountID   uuid.UUID `json:"accountId"`
	EntryType   string    `json:"entryType"`
	AmountPaise int64     `json:"amountPaise"`
	Currency    string    `json:"currency"`
	OccurredAt  string    `json:"occurredAt"`
}

type publicBucketID struct {
	Partition string `json:"partition"`
	Start     string `json:"start"`
	WidthNs   int64  `json:"widthNs"`
}

type publicProofStep struct {
	HashHex  string                       `json:"hashHex,omitempty"`
	Position reconciliation.SiblingOrder `json:"position"`
}

type publicIntegrityProof struct {
	OperationID       uuid.UUID                             `json:"operationId"`
	Record            publicProofRecord                     `json:"record"`
	LeafHashHex       string                                `json:"leafHashHex"`
	BucketID          publicBucketID                        `json:"bucketId"`
	ParticipantID     string                                `json:"participantId"`
	ScopeFrom         string                                `json:"scopeFrom"`
	ScopeTo           string                                `json:"scopeTo"`
	Generation        string                                `json:"generation"`
	CanonicalVersion  string                                `json:"canonicalVersion"`
	AlgorithmVersion  string                                `json:"algorithmVersion"`
	BucketRootHex     string                                `json:"bucketRootHex"`
	ExpectedRootHex   string                                `json:"expectedRootHex"`
	BucketPath        []publicProofStep                     `json:"bucketPath"`
	GlobalPath        []publicProofStep                     `json:"globalPath"`
	GenerationMetrics reconciliation.ProofGenerationMetrics `json:"generationMetrics"`
}

func publicProof(proof reconciliation.IntegrityProof) publicIntegrityProof {
	steps := func(path []reconciliation.ProofStep) []publicProofStep {
		out := make([]publicProofStep, len(path))
		for i, step := range path {
			out[i] = publicProofStep{HashHex: hexEncode(step.Hash), Position: step.Order}
		}
		return out
	}
	record := proof.Record.Normalize()
	return publicIntegrityProof{
		OperationID: record.OperationID,
		Record: publicProofRecord{
			OperationID: record.OperationID, PaymentID: record.PaymentID, AccountID: record.AccountID,
			EntryType: record.EntryType, AmountPaise: record.AmountPaise, Currency: record.Currency,
			OccurredAt: record.OccurredAt.Format(time.RFC3339Nano),
		},
		LeafHashHex: hexEncode(proof.LeafHash),
		BucketID: publicBucketID{
			Partition: proof.BucketID.Partition, Start: proof.BucketID.Start.UTC().Format(time.RFC3339Nano), WidthNs: int64(proof.BucketID.Width),
		},
		ParticipantID: proof.ParticipantID, ScopeFrom: proof.Scope.From.UTC().Format(time.RFC3339Nano),
		ScopeTo: proof.Scope.To.UTC().Format(time.RFC3339Nano), Generation: proof.Generation,
		CanonicalVersion: proof.CanonicalVersion, AlgorithmVersion: proof.AlgorithmVersion,
		BucketRootHex: hexEncode(proof.BucketRoot), ExpectedRootHex: hexEncode(proof.ExpectedRoot),
		BucketPath: steps(proof.BucketPath), GlobalPath: steps(proof.GlobalPath), GenerationMetrics: proof.GenerationMetrics,
	}
}

func decodePublicProof(body []byte) (reconciliation.IntegrityProof, error) {
	var dto publicIntegrityProof
	if err := json.Unmarshal(body, &dto); err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	if dto.LeafHashHex == "" {
		return reconciliation.DeserializeProof(body)
	}
	if dto.OperationID == uuid.Nil || dto.OperationID != dto.Record.OperationID {
		return reconciliation.IntegrityProof{}, errors.New("operationId must match record.operationId")
	}
	parseTime := func(name, value string) (time.Time, error) {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
		if err != nil {
			return time.Time{}, fmt.Errorf("%s must be RFC3339: %w", name, err)
		}
		return parsed.UTC(), nil
	}
	occurredAt, err := parseTime("record.occurredAt", dto.Record.OccurredAt)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	bucketStart, err := parseTime("bucketId.start", dto.BucketID.Start)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	from, err := parseTime("scopeFrom", dto.ScopeFrom)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	to, err := parseTime("scopeTo", dto.ScopeTo)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	decodeHash := func(name, value string, allowEmpty bool) ([]byte, error) {
		if value == "" && allowEmpty {
			return nil, nil
		}
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("%s must be a 32-byte hex digest", name)
		}
		return decoded, nil
	}
	leaf, err := decodeHash("leafHashHex", dto.LeafHashHex, false)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	bucketRoot, err := decodeHash("bucketRootHex", dto.BucketRootHex, false)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	expectedRoot, err := decodeHash("expectedRootHex", dto.ExpectedRootHex, false)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	decodeSteps := func(name string, path []publicProofStep) ([]reconciliation.ProofStep, error) {
		out := make([]reconciliation.ProofStep, len(path))
		for i, step := range path {
			hash, err := decodeHash(fmt.Sprintf("%s[%d].hashHex", name, i), step.HashHex, step.Position == reconciliation.SiblingPromoted)
			if err != nil {
				return nil, err
			}
			out[i] = reconciliation.ProofStep{Hash: hash, Order: step.Position}
		}
		return out, nil
	}
	bucketPath, err := decodeSteps("bucketPath", dto.BucketPath)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	globalPath, err := decodeSteps("globalPath", dto.GlobalPath)
	if err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	proof := reconciliation.IntegrityProof{
		Record: reconciliation.CanonicalRecord{
			OperationID: dto.Record.OperationID, PaymentID: dto.Record.PaymentID, AccountID: dto.Record.AccountID,
			EntryType: dto.Record.EntryType, AmountPaise: dto.Record.AmountPaise, Currency: dto.Record.Currency, OccurredAt: occurredAt,
		},
		LeafHash: leaf, BucketID: reconciliation.BucketID{Partition: dto.BucketID.Partition, Start: bucketStart, Width: time.Duration(dto.BucketID.WidthNs)},
		ParticipantID: dto.ParticipantID, Scope: reconciliation.Scope{From: from, To: to}, Generation: dto.Generation,
		CanonicalVersion: dto.CanonicalVersion, AlgorithmVersion: dto.AlgorithmVersion,
		BucketPath: bucketPath, BucketRoot: bucketRoot, GlobalPath: globalPath, ExpectedRoot: expectedRoot,
		GenerationMetrics: dto.GenerationMetrics,
	}
	if err := proof.Validate(); err != nil {
		return reconciliation.IntegrityProof{}, err
	}
	return proof, nil
}

func writeReconciliationReadError(writer http.ResponseWriter, request *http.Request, err error, fallback string) {
	if errors.Is(err, reconciliation.ErrCommitmentUnavailable) {
		writeAPIError(writer, request, common.NewAPIError("COMMITMENT_UNAVAILABLE", "maintained commitment is unavailable for the requested scope", http.StatusServiceUnavailable))
		return
	}
	if errors.Is(err, reconciliation.ErrStaleReference) {
		writeAPIError(writer, request, common.NewAPIError("STALE_COMMITMENT_GENERATION", "commitment generation is stale", http.StatusConflict))
		return
	}
	writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", fallback, http.StatusInternalServerError))
}

func (handler *Handler) reconciliationTreeRoot(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	participantID := strings.TrimSpace(request.URL.Query().Get("participantId"))
	if participantID == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "participantId is required", http.StatusBadRequest))
		return
	}

	scopeFrom, err := time.Parse(time.RFC3339, strings.TrimSpace(request.URL.Query().Get("scopeFrom")))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeFrom must be a valid RFC3339 timestamp", http.StatusBadRequest))
		return
	}
	scopeTo, err := time.Parse(time.RFC3339, strings.TrimSpace(request.URL.Query().Get("scopeTo")))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeTo must be a valid RFC3339 timestamp", http.StatusBadRequest))
		return
	}

	scope := reconciliation.Scope{From: scopeFrom, To: scopeTo}
	participant, err := handler.reconEngine.GetCanonicalParticipant(request.Context(), participantID, scope)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "invalid participant or scope", http.StatusBadRequest))
		return
	}

	root, err := participant.GetRoot(request.Context(), scope)
	if err != nil {
		if handler.logger != nil {
			handler.logger.Error("get reconciliation tree root", "error", err)
		}
		writeReconciliationReadError(writer, request, err, "failed to get tree root")
		return
	}

	out := map[string]any{
		"rootHex":   hexEncode(root.Root),
		"algorithm": root.Algorithm,
		"version":   root.Version,
		"ref":       root.Ref,
		"region":    root.Region,
	}
	writeData(writer, http.StatusOK, request, out)
}

func (handler *Handler) reconciliationTreeChildren(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	participantID := strings.TrimSpace(request.URL.Query().Get("participantId"))
	if participantID == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "participantId is required", http.StatusBadRequest))
		return
	}

	scopeFrom, err := time.Parse(time.RFC3339, strings.TrimSpace(request.URL.Query().Get("scopeFrom")))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeFrom must be a valid RFC3339 timestamp", http.StatusBadRequest))
		return
	}
	scopeTo, err := time.Parse(time.RFC3339, strings.TrimSpace(request.URL.Query().Get("scopeTo")))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeTo must be a valid RFC3339 timestamp", http.StatusBadRequest))
		return
	}

	scope := reconciliation.Scope{From: scopeFrom, To: scopeTo}
	participant, err := handler.reconEngine.GetCanonicalParticipant(request.Context(), participantID, scope)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "invalid participant or scope", http.StatusBadRequest))
		return
	}

	ref := reconciliation.NodeRef{
		ParticipantID: participantID,
		ScopeID:       reconciliation.ScopeIdentity(scope),
		Generation:    strings.TrimSpace(request.URL.Query().Get("generation")),
		Path:          strings.TrimSpace(request.URL.Query().Get("path")),
	}

	if ref.Generation == "" || ref.Path == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "generation and path are required", http.StatusBadRequest))
		return
	}

	// We only need the path, generation, and participant/scope identity to look up children.
	// The region is only needed for display purposes or strict validation in some repos.
	// Try parsing Region if provided.
	startStr := request.URL.Query().Get("regionStart")
	endStr := request.URL.Query().Get("regionEnd")
	if startStr != "" && endStr != "" {
		start, _ := time.Parse(time.RFC3339, startStr)
		end, _ := time.Parse(time.RFC3339, endStr)
		ref.Region = reconciliation.LogicalRegion{Start: start, End: end}
	}

	children, err := participant.GetChildren(request.Context(), ref)
	if err != nil {
		if errors.Is(err, reconciliation.ErrInvalidNodeReference) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "invalid node reference", http.StatusBadRequest))
			return
		}
		if errors.Is(err, reconciliation.ErrNodeNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "node not found", http.StatusNotFound))
			return
		}
		if errors.Is(err, reconciliation.ErrStaleReference) || errors.Is(err, reconciliation.ErrCommitmentUnavailable) {
			writeReconciliationReadError(writer, request, err, "failed to get tree children")
			return
		}
		if handler.logger != nil {
			handler.logger.Error("get reconciliation tree children", "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to get tree children", http.StatusInternalServerError))
		return
	}

	out := make([]map[string]any, len(children))
	for i, c := range children {
		out[i] = map[string]any{
			"hashHex": hexEncode(c.Hash),
			"ref":     c.Ref,
			"region":  c.Region,
		}
	}
	writeData(writer, http.StatusOK, request, out)
}

func (handler *Handler) reconciliationGetProof(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	opIDStr := strings.TrimSpace(request.PathValue("operationID"))
	opID, err := uuid.Parse(opIDStr)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "operationID must be a valid UUID", http.StatusBadRequest))
		return
	}

	participantID := strings.TrimSpace(request.URL.Query().Get("participantId"))
	if participantID == "" {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "participantId is required", http.StatusBadRequest))
		return
	}

	scopeFrom, err := time.Parse(time.RFC3339, strings.TrimSpace(request.URL.Query().Get("scopeFrom")))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeFrom must be a valid RFC3339 timestamp", http.StatusBadRequest))
		return
	}
	scopeTo, err := time.Parse(time.RFC3339, strings.TrimSpace(request.URL.Query().Get("scopeTo")))
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "scopeTo must be a valid RFC3339 timestamp", http.StatusBadRequest))
		return
	}

	scope := reconciliation.Scope{From: scopeFrom, To: scopeTo}
	participant, err := handler.reconEngine.GetCanonicalParticipant(request.Context(), participantID, scope)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "invalid participant or scope", http.StatusBadRequest))
		return
	}

	proof, err := reconciliation.NewIntegrityEngine().GenerateProofByOperationID(request.Context(), participant, scope, opID)
	if err != nil {
		if errors.Is(err, reconciliation.ErrRecordNotFound) {
			writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "record not found or proof unavailable", http.StatusNotFound))
			return
		}
		if handler.logger != nil {
			handler.logger.Error("generate integrity proof", "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "failed to generate proof", http.StatusInternalServerError))
		return
	}

	writeData(writer, http.StatusOK, request, publicProof(proof))
}

func (handler *Handler) reconciliationVerifyProof(writer http.ResponseWriter, request *http.Request) {
	if handler.reconEngine == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "reconciliation engine is not configured", http.StatusNotFound))
		return
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "failed to read request body", http.StatusBadRequest))
		return
	}

	proof, err := decodePublicProof(body)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", fmt.Sprintf("invalid proof: %v", err), http.StatusBadRequest))
		return
	}

	participantID := proof.ParticipantID
	expectedScope := proof.Scope
	if queryParticipant := strings.TrimSpace(request.URL.Query().Get("participantId")); queryParticipant != "" && queryParticipant != participantID {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "participantId query parameter does not match proof", http.StatusBadRequest))
		return
	}
	for name, expected := range map[string]time.Time{"scopeFrom": expectedScope.From, "scopeTo": expectedScope.To} {
		if raw := strings.TrimSpace(request.URL.Query().Get(name)); raw != "" {
			parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
			if parseErr != nil || !parsed.UTC().Equal(expected.UTC()) {
				writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", name+" query parameter does not match proof", http.StatusBadRequest))
				return
			}
		}
	}

	participant, err := handler.reconEngine.GetCanonicalParticipant(request.Context(), participantID, expectedScope)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", "invalid participant or scope", http.StatusBadRequest))
		return
	}

	rootRes, err := participant.GetRoot(request.Context(), expectedScope)
	if err != nil {
		writeReconciliationReadError(writer, request, err, "failed to fetch authoritative root")
		return
	}
	if proof.Generation != rootRes.Ref.Generation {
		writeAPIError(writer, request, common.NewAPIError("STALE_COMMITMENT_GENERATION", "proof generation is no longer current", http.StatusConflict))
		return
	}
	if rootRes.Region.IsZero() {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "no authoritative commitment found for scope", http.StatusNotFound))
		return
	}

	expectedCtx := reconciliation.ProofVerificationContext{
		ParticipantID:    participantID,
		Scope:            expectedScope,
		BucketID:         proof.BucketID,
		Generation:       rootRes.Ref.Generation,
		CanonicalVersion: reconciliation.CanonicalVersion,
		AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		ExpectedRoot:     rootRes.Root,
	}

	res, err := reconciliation.NewIntegrityEngine().VerifyProof(request.Context(), proof, expectedCtx)
	if err != nil {
		if errors.Is(err, reconciliation.ErrMissingVerificationContext) || errors.Is(err, reconciliation.ErrProofIncompatibleVersion) || errors.Is(err, reconciliation.ErrProofMalformedPath) {
			writeAPIError(writer, request, common.NewAPIError("INVALID_REQUEST", err.Error(), http.StatusBadRequest))
			return
		}
		// Verification failure (e.g. hash mismatch, root mismatch) will return an error from VerifyProof
		writeAPIError(writer, request, common.NewAPIError("VERIFICATION_FAILED", err.Error(), http.StatusConflict))
		return
	}

	out := map[string]any{
		"valid":                   res.Valid,
		"reconstructedBucketRoot": hexEncode(res.ReconstructedBucketRoot),
		"reconstructedRoot":       hexEncode(res.ReconstructedRoot),
		"metrics":                 res.Metrics,
	}
	writeData(writer, http.StatusOK, request, out)
}
