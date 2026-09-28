package http

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/reconciliation"
)

// ledgerCorruptionFixture runs a fixed, isolated ledger corruption scenario.
// Registration is restricted to TX_SIMULATION_MODE=true and OPS_ADMIN.
// It does not write to PostgreSQL or mutate production payment authority.
func (handler *Handler) ledgerCorruptionFixture(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(time.Hour)}
	const participantID = "FIXTURE-BANK"
	baseline := reconciliation.CanonicalRecord{
		OperationID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("m3-corruption-operation")),
		PaymentID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("m3-corruption-payment")),
		AccountID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("m3-corruption-account")),
		EntryType:   "DEBIT", AmountPaise: 1000, Currency: "INR", OccurredAt: base.Add(time.Minute),
	}
	canonical, err := reconciliation.NewMemoryParticipant(participantID, participantID, time.Hour, []reconciliation.CanonicalRecord{baseline})
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "fixture setup failed", http.StatusInternalServerError))
		return
	}
	committedRoot, err := canonical.GetRoot(ctx, scope)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "fixture commitment failed", http.StatusInternalServerError))
		return
	}
	corrupted := baseline
	corrupted.AmountPaise++
	participant, err := reconciliation.NewMemoryParticipant(participantID, participantID, time.Hour, []reconciliation.CanonicalRecord{corrupted})
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "fixture setup failed", http.StatusInternalServerError))
		return
	}
	corruptedRoot, err := participant.GetRoot(ctx, scope)
	if err != nil {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "fixture comparison failed", http.StatusInternalServerError))
		return
	}
	reconStore := &corruptionFixtureRuns{}
	reconEngine := reconciliation.NewEngineWithRepo(reconciliation.KnownParticipants{participantID: true}, reconStore,
		func(context.Context, string, reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return canonical, nil
		},
		func(context.Context, string, reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return participant, nil
		})
	reconRun, err := reconEngine.Execute(ctx, reconciliation.RunRequest{ParticipantID: participantID, ScopeFrom: scope.From, ScopeTo: scope.To})
	if err != nil || reconRun.DiscrepancyCount == 0 {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "fixture reconciliation failed", http.StatusInternalServerError))
		return
	}
	key := fmt.Sprintf("%s:%d:%d", participantID, scope.From.UnixNano(), scope.To.UnixNano())
	store := reconciliation.NewMemoryFinancialDataStore()
	store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
		ParticipantID: participantID, Partition: participantID, BucketWidth: time.Hour,
		CanonicalVersion: reconciliation.CanonicalVersion, AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		Generation: committedRoot.Ref.Generation, Root: committedRoot.Root, RecordCount: 1, Scope: scope, CapturedAt: base,
	}
	store.AuthoritativeRecords[key] = []reconciliation.CanonicalRecord{baseline}
	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	checkReq := reconciliation.IntegrityRunRequest{Scope: &scope, ParticipantID: participantID, CheckCodes: []string{reconciliation.CheckMerkleCommitmentConsistency}}
	before, err := engine.Run(ctx, checkReq)
	if err != nil || len(before.Checks) != 1 || before.Checks[0].Status != reconciliation.CheckStatusPass {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "fixture baseline verification failed", http.StatusInternalServerError))
		return
	}
	store.AuthoritativeRecords[key] = []reconciliation.CanonicalRecord{corrupted}
	after, err := engine.Run(ctx, checkReq)
	if err != nil || len(after.Checks) != 1 || after.Checks[0].Status != reconciliation.CheckStatusFail || bytes.Equal(committedRoot.Root, corruptedRoot.Root) {
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "fixture corruption was not detected", http.StatusInternalServerError))
		return
	}
	writeData(writer, http.StatusOK, request, map[string]any{
		"mode": "ISOLATED_SIMULATION", "mutation": "amount_paise +1 on one fixed fixture record",
		"baselineIntegrity": before.Checks[0], "corruptedIntegrity": after.Checks[0],
		"merkleRootMismatch": true, "reconciliationDiscrepancies": reconRun.DiscrepancyCount,
		"discrepancies": reconStore.discrepancies, "scope": scope,
	})
}
