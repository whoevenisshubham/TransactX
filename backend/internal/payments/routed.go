package payments

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/ledger"
)

const (
	bankOperationProcessing = "PROCESSING"
	bankOperationSucceeded  = "SUCCEEDED"
	bankOperationPending    = "PENDING"
	bankOperationFailed     = "FAILED"
)

type routedOperation struct {
	PaymentID           uuid.UUID
	BankID              uuid.UUID
	OperationID         uuid.UUID
	OperationType       string
	Status              string
	HoldID              *uuid.UUID
	OriginalOperationID *uuid.UUID
	AccountID           *uuid.UUID
}

type operationResolution uint8

const (
	operationSucceeded operationResolution = iota + 1
	operationPending
	operationFailed
)

// CreateRoutedIdempotent creates the central intent first, then executes the
// durable bank saga. The central insert and bank calls intentionally do not
// share a database transaction.
func (repository *Repository) CreateRoutedIdempotent(ctx context.Context, payment Payment, key, requestHash string, sourceBankID, destinationBankID uuid.UUID, sourceAdapter, destinationAdapter bank.BankAdapter) (Payment, bool, error) {
	sourceBankAccountID := payment.SourceBankAccountID
	if sourceBankAccountID == nil {
		value := payment.SenderAccountID
		sourceBankAccountID = &value
	}
	destinationBankAccountID := payment.DestinationBankAccountID
	if destinationBankAccountID == nil {
		value := payment.ReceiverAccountID
		destinationBankAccountID = &value
	}
	payment.SourceBankID = uuidPointer(sourceBankID)
	payment.DestinationBankID = uuidPointer(destinationBankID)
	payment.SourceBankAccountID = sourceBankAccountID
	payment.DestinationBankAccountID = destinationBankAccountID

	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return Payment{}, false, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('idemp:' || $1::text || ':' || $2))`, payment.InitiatedByUserID, key); err != nil {
		return Payment{}, false, err
	}

	var existingHash string
	var existingPaymentID *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT request_hash, payment_id FROM idempotency_records WHERE user_id = $1 AND key = $2`, payment.InitiatedByUserID, key).Scan(&existingHash, &existingPaymentID)
	if err == nil {
		if existingHash != requestHash {
			return Payment{}, false, ErrIdempotencyConflict
		}
		if existingPaymentID == nil {
			return Payment{}, false, ErrNotFound
		}
		existing, getErr := scanPayment(tx.QueryRow(ctx, routedPaymentSelect+` FROM payments WHERE id = $1`, *existingPaymentID))
		if getErr != nil {
			return Payment{}, false, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Payment{}, false, err
		}
		return repository.replayRoutedPayment(ctx, existing, true, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, false, err
	}

	created, err := scanPayment(tx.QueryRow(ctx, `
		INSERT INTO payments
			(id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, note, origin,
			 source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id, routing_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE(NULLIF($9, ''), 'ONLINE'), $10, $11, $12, $13, 'static M1 routed adapter route')
		RETURNING `+routedPaymentColumns, payment.ID, payment.InitiatedByUserID, payment.SenderAccountID,
		payment.ReceiverAccountID, payment.AmountPaise, payment.Currency, payment.State, payment.Note, payment.Origin,
		sourceBankID, destinationBankID, *sourceBankAccountID, *destinationBankAccountID))
	if err != nil {
		return Payment{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id, response_snapshot) VALUES ($1, $2, $3, $4, $5, jsonb_build_object('paymentId', $6::text, 'state', $7::text))`, uuid.New(), payment.InitiatedByUserID, key, requestHash, payment.ID, payment.ID.String(), payment.State); err != nil {
		if !isUniqueViolation(err) {
			return Payment{}, false, err
		}
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return Payment{}, false, rollbackErr
		}
		existing, duplicate, lookupErr := repository.GetIdempotent(ctx, payment.InitiatedByUserID, key, requestHash)
		if lookupErr != nil || !duplicate {
			return Payment{}, false, lookupErr
		}
		return repository.replayRoutedPayment(ctx, existing, true, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter)
	}
	if err := tx.Commit(ctx); err != nil {
		return Payment{}, false, err
	}

	return repository.replayRoutedPayment(ctx, created, false, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter)
}

func (repository *Repository) replayRoutedPayment(ctx context.Context, payment Payment, duplicate bool, sourceBankID, destinationBankID uuid.UUID, sourceAdapter, destinationAdapter bank.BankAdapter) (Payment, bool, error) {
	unlock, err := repository.lockPayment(ctx, payment.ID)
	if err != nil {
		return Payment{}, false, err
	}
	defer unlock()

	current, err := repository.Get(ctx, payment.ID)
	if err != nil {
		return Payment{}, false, err
	}
	if current.State == StateCompleted || current.State == StateFailed || current.State == StateReversed {
		return current, duplicate, nil
	}

	var sagaErr error
	switch current.State {
	case StateCreated, StateValidating, StateRouting:
		sagaErr = repository.runRoutedSaga(ctx, current, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter)
	case StateProcessing, StatePendingReconciliation:
		sagaErr = repository.RecoverRoutedPayment(ctx, current, sourceAdapter, destinationAdapter)
	case StateBankSettledCentralPending:
		current, sagaErr = repository.RecoverBankSettledCentralPending(ctx, current.ID)
	}
	if refreshed, getErr := repository.Get(ctx, payment.ID); getErr == nil {
		current = refreshed
	} else if sagaErr == nil {
		sagaErr = getErr
	}
	return current, duplicate, sagaErr
}

func (repository *Repository) lockPayment(ctx context.Context, paymentID uuid.UUID) (func(), error) {
	conn, err := repository.db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	lockKey := int64(binary.BigEndian.Uint64(paymentID[:8])) ^ int64(binary.BigEndian.Uint64(paymentID[8:]))
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		conn.Release()
		return nil, err
	}
	unlock := func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey)
		conn.Release()
	}
	return unlock, nil
}

func (repository *Repository) runRoutedSaga(ctx context.Context, payment Payment, sourceBankID, destinationBankID uuid.UUID, sourceAdapter, destinationAdapter bank.BankAdapter) error {
	for {
		current, err := repository.Get(ctx, payment.ID)
		if err != nil {
			return err
		}
		var next string
		switch current.State {
		case StateCreated:
			next = StateValidating
		case StateValidating:
			next = StateRouting
		case StateRouting:
			next = StateProcessing
		default:
			payment = current
			goto processing
		}
		if err := repository.updateState(ctx, payment.ID, next); err != nil {
			return err
		}
	}

processing:
	if err := repository.resolveRoutedAccounts(ctx, payment, sourceAdapter, destinationAdapter); err != nil {
		return repository.handleInitialFailure(ctx, payment.ID, err)
	}
	return repository.advanceRoutedSaga(ctx, payment, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter)
}

// RecoverRoutedPayment is the deterministic recovery entry point for a
// payment in PROCESSING or PENDING_RECONCILIATION. It resolves persisted
// operation identities through status lookup before making any new monetary
// call. A retry never creates a replacement operation ID.
func (repository *Repository) RecoverRoutedPayment(ctx context.Context, payment Payment, sourceAdapter, destinationAdapter bank.BankAdapter) error {
	if payment.State != StateProcessing && payment.State != StatePendingReconciliation {
		return nil
	}
	if sourceAdapter == nil || destinationAdapter == nil {
		return repository.markPending(ctx, payment.ID)
	}
	if err := repository.resolveRoutedAccounts(ctx, payment, sourceAdapter, destinationAdapter); err != nil {
		if isTransientBankError(err) {
			return repository.markPending(ctx, payment.ID)
		}
		return repository.markFailure(ctx, payment.ID, err)
	}
	sourceBankID := pointerValue(payment.SourceBankID)
	destinationBankID := pointerValue(payment.DestinationBankID)
	return repository.advanceRoutedSaga(ctx, payment, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter)
}

func (repository *Repository) resolveRoutedAccounts(ctx context.Context, payment Payment, sourceAdapter, destinationAdapter bank.BankAdapter) error {
	if sourceAdapter == nil || destinationAdapter == nil {
		return &bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "routed bank adapter is not configured"}
	}
	sourceResult, err := sourceAdapter.ResolveAccount(ctx, bank.ResolveAccountRequest{AccountID: bankAccountID(payment.SourceBankAccountID, payment.SenderAccountID)})
	if err != nil {
		return err
	}
	if sourceResult.Status != bank.AccountActive {
		return accountStatusError(sourceResult.Status, "source")
	}
	destinationResult, err := destinationAdapter.ResolveAccount(ctx, bank.ResolveAccountRequest{AccountID: bankAccountID(payment.DestinationBankAccountID, payment.ReceiverAccountID)})
	if err != nil {
		return err
	}
	if destinationResult.Status != bank.AccountActive {
		return accountStatusError(destinationResult.Status, "destination")
	}
	return nil
}

func (repository *Repository) advanceRoutedSaga(ctx context.Context, payment Payment, sourceBankID, destinationBankID uuid.UUID, sourceAdapter, destinationAdapter bank.BankAdapter) error {
	holdID := operationID(payment.ID, "hold")
	holdOutcome, holdErr := repository.ensureBankOperation(ctx, payment, sourceBankID, holdID, "HOLD", bankAccountID(payment.SourceBankAccountID, payment.SenderAccountID), holdID, uuid.Nil, sourceAdapter, func() (bank.OperationResult, error) {
		result, err := sourceAdapter.HoldFunds(ctx, bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{
			PaymentID: payment.ID, OperationID: holdID, IdempotencyKey: holdID.String(), AccountID: bankAccountID(payment.SourceBankAccountID, payment.SenderAccountID), AmountPaise: payment.AmountPaise, Currency: payment.Currency,
		}})
		return result.OperationResult, err
	})
	if holdOutcome == operationPending {
		return repository.markPending(ctx, payment.ID)
	}
	if holdOutcome == operationFailed {
		return repository.markFailure(ctx, payment.ID, holdErr)
	}

	creditID := operationID(payment.ID, "provisional-credit")
	creditOutcome, creditErr := repository.ensureBankOperation(ctx, payment, destinationBankID, creditID, "PROVISIONAL_CREDIT", bankAccountID(payment.DestinationBankAccountID, payment.ReceiverAccountID), holdID, uuid.Nil, destinationAdapter, func() (bank.OperationResult, error) {
		return destinationAdapter.ProvisionalCredit(ctx, bank.ProvisionalCreditRequest{OperationRequest: bank.OperationRequest{
			PaymentID: payment.ID, OperationID: creditID, IdempotencyKey: creditID.String(), AccountID: bankAccountID(payment.DestinationBankAccountID, payment.ReceiverAccountID), AmountPaise: payment.AmountPaise, Currency: payment.Currency,
		}})
	})
	if creditOutcome == operationPending {
		return repository.markPending(ctx, payment.ID)
	}
	if creditOutcome == operationFailed {
		if !repository.compensateHold(ctx, payment, sourceBankID, sourceAdapter, holdID) {
			return repository.markPending(ctx, payment.ID)
		}
		return repository.markFailure(ctx, payment.ID, creditErr)
	}

	confirmID := operationID(payment.ID, "confirm-hold")
	confirmOutcome, confirmErr := repository.ensureBankOperation(ctx, payment, sourceBankID, confirmID, "CONFIRM_SOURCE_HOLD", bankAccountID(payment.SourceBankAccountID, payment.SenderAccountID), holdID, uuid.Nil, sourceAdapter, func() (bank.OperationResult, error) {
		return sourceAdapter.ConfirmHold(ctx, bank.ConfirmHoldRequest{PaymentID: payment.ID, OperationID: confirmID, IdempotencyKey: confirmID.String(), HoldID: holdID})
	})
	if confirmOutcome == operationPending {
		return repository.markPending(ctx, payment.ID)
	}
	if confirmOutcome == operationFailed {
		if !repository.compensateRoutedPayment(ctx, payment, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter, holdID, creditID) {
			return repository.markPending(ctx, payment.ID)
		}
		return repository.markFailure(ctx, payment.ID, confirmErr)
	}

	finalizeID := operationID(payment.ID, "finalize-credit")
	finalizeOutcome, _ := repository.ensureBankOperation(ctx, payment, destinationBankID, finalizeID, "FINALIZE_CREDIT", bankAccountID(payment.DestinationBankAccountID, payment.ReceiverAccountID), creditID, creditID, destinationAdapter, func() (bank.OperationResult, error) {
		return destinationAdapter.ConfirmHold(ctx, bank.ConfirmHoldRequest{PaymentID: payment.ID, OperationID: finalizeID, IdempotencyKey: finalizeID.String(), HoldID: creditID})
	})
	if finalizeOutcome != operationSucceeded {
		// The source hold is already confirmed. There is no safe generic undo for
		// that bank-side debit, so the payment remains reconciliation-pending.
		return repository.markPending(ctx, payment.ID)
	}

	if err := repository.settleRoutedCentral(ctx, payment); err != nil {
		return repository.markBankSettledPending(ctx, payment.ID)
	}
	return nil
}

func (repository *Repository) ensureBankOperation(ctx context.Context, payment Payment, bankID, operationID uuid.UUID, operationType string, accountID, holdID, originalOperationID uuid.UUID, adapter bank.BankAdapter, invoke func() (bank.OperationResult, error)) (operationResolution, error) {
	record, found, err := repository.getBankOperation(ctx, payment.ID, operationType)
	if err != nil {
		return operationFailed, err
	}
	if found {
		if record.OperationID != operationID || record.BankID != bankID {
			return operationFailed, fmt.Errorf("persisted bank operation identity mismatch for %s", operationType)
		}
		if record.Status == bankOperationSucceeded {
			return operationSucceeded, nil
		}
		if record.Status == bankOperationFailed {
			return operationFailed, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: operationType + " failed"}
		}
		status, statusErr := adapter.GetOperationStatus(ctx, bank.OperationStatusRequest{PaymentID: payment.ID, OperationID: operationID})
		if statusErr != nil {
			_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
			return operationPending, statusErr
		}
		if !validOperationCorrelation(status, payment.ID, operationID) {
			_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
			return operationPending, &bank.AdapterError{Code: bank.ErrCodeTransientFailure, Message: operationType + " returned an unresolved status"}
		}
		switch status.Status {
		case bank.OperationSucceeded:
			if err := repository.updateBankOperationResult(ctx, operationID, bankOperationSucceeded, status.BankReference); err != nil {
				return operationPending, err
			}
			return operationSucceeded, nil
		case bank.OperationFailed:
			_ = repository.updateBankOperation(ctx, operationID, bankOperationFailed)
			return operationFailed, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: operationType + " failed"}
		case bank.OperationPending:
			_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
			return operationPending, nil
		default:
			_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
			return operationPending, &bank.AdapterError{Code: bank.ErrCodeTransientFailure, Message: operationType + " returned an invalid or unsupported status"}
		}
	}

	inserted, err := repository.trackBankOperation(ctx, payment.ID, bankID, operationID, operationType, accountID, payment.AmountPaise, payment.Currency, holdID, originalOperationID, bankOperationProcessing)
	if err != nil {
		return operationFailed, err
	}
	if !inserted {
		return operationPending, &bank.AdapterError{Code: bank.ErrCodeTransientFailure, Message: operationType + " is already being resolved"}
	}
	result, callErr := invoke()
	if callErr != nil {
		status := bankOperationFailed
		if isTransientBankError(callErr) {
			status = bankOperationPending
		}
		_ = repository.updateBankOperation(ctx, operationID, status)
		if status == bankOperationPending {
			return operationPending, nil
		}
		return operationFailed, callErr
	}
	if !validOperationCorrelation(result, payment.ID, operationID) {
		_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
		return operationPending, &bank.AdapterError{Code: bank.ErrCodeTransientFailure, Message: operationType + " returned an unresolved status"}
	}
	switch result.Status {
	case bank.OperationSucceeded:
		if err := repository.updateBankOperationResult(ctx, operationID, bankOperationSucceeded, result.BankReference); err != nil {
			return operationPending, err
		}
		return operationSucceeded, nil
	case bank.OperationFailed:
		_ = repository.updateBankOperation(ctx, operationID, bankOperationFailed)
		return operationFailed, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: operationType + " failed"}
	case bank.OperationPending:
		_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
		return operationPending, nil
	default:
		_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
		return operationPending, &bank.AdapterError{Code: bank.ErrCodeTransientFailure, Message: operationType + " returned an invalid or unsupported status"}
	}
}

func validOperationCorrelation(result bank.OperationResult, paymentID, operationID uuid.UUID) bool {
	return result.PaymentID == paymentID && result.OperationID == operationID
}

func (repository *Repository) compensateHold(ctx context.Context, payment Payment, sourceBankID uuid.UUID, sourceAdapter bank.BankAdapter, holdID uuid.UUID) bool {
	releaseID := operationID(payment.ID, "release-hold")
	resolution, _ := repository.ensureBankOperation(ctx, payment, sourceBankID, releaseID, "RELEASE_HOLD", bankAccountID(payment.SourceBankAccountID, payment.SenderAccountID), holdID, uuid.Nil, sourceAdapter, func() (bank.OperationResult, error) {
		return sourceAdapter.ReleaseHold(ctx, bank.ReleaseHoldRequest{PaymentID: payment.ID, OperationID: releaseID, IdempotencyKey: releaseID.String(), HoldID: holdID})
	})
	return resolution == operationSucceeded
}

func (repository *Repository) compensateRoutedPayment(ctx context.Context, payment Payment, sourceBankID, destinationBankID uuid.UUID, sourceAdapter, destinationAdapter bank.BankAdapter, holdID, creditID uuid.UUID) bool {
	releaseOK := repository.compensateHold(ctx, payment, sourceBankID, sourceAdapter, holdID)
	reverseID := operationID(payment.ID, "reverse-credit")
	resolution, _ := repository.ensureBankOperation(ctx, payment, destinationBankID, reverseID, "REVERSE_CREDIT", bankAccountID(payment.DestinationBankAccountID, payment.ReceiverAccountID), uuid.Nil, creditID, destinationAdapter, func() (bank.OperationResult, error) {
		return destinationAdapter.ReverseProvisionalCredit(ctx, bank.ReverseCreditRequest{PaymentID: payment.ID, OperationID: reverseID, IdempotencyKey: reverseID.String(), OriginalOperationID: creditID})
	})
	return releaseOK && resolution == operationSucceeded
}

func (repository *Repository) settleRoutedCentral(ctx context.Context, payment Payment) error {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentState string
	if err := tx.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1 FOR UPDATE`, payment.ID).Scan(&currentState); err != nil {
		return err
	}
	if currentState == StateCompleted {
		return tx.Commit(ctx)
	}
	if currentState == StateCommitted {
		current := Payment{State: currentState}
		if err := Transition(&current, StateCompleted); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE payments SET state = $2, completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, payment.ID, current.State); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	current := Payment{State: currentState}
	if err := Transition(&current, StateCommitted); err != nil {
		return err
	}
	accountsRepository := accounts.NewRepository(repository.db)
	ledgerRepository := ledger.NewRepository(repository.db)
	if err := accountsRepository.Debit(ctx, tx, payment.SenderAccountID, payment.AmountPaise); err != nil {
		return err
	}
	if err := accountsRepository.Credit(ctx, tx, payment.ReceiverAccountID, payment.AmountPaise); err != nil {
		return err
	}
	ledgerTransactionID, err := ledgerRepository.CreateTransaction(ctx, tx, payment.ID)
	if err != nil {
		return err
	}
	if err := ledgerRepository.CreateEntry(ctx, tx, ledger.Entry{ID: uuid.New(), LedgerTransactionID: ledgerTransactionID, AccountID: payment.SenderAccountID, EntryType: ledger.EntryDebit, AmountPaise: payment.AmountPaise}); err != nil {
		return err
	}
	if err := ledgerRepository.CreateEntry(ctx, tx, ledger.Entry{ID: uuid.New(), LedgerTransactionID: ledgerTransactionID, AccountID: payment.ReceiverAccountID, EntryType: ledger.EntryCredit, AmountPaise: payment.AmountPaise}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE payments SET state = $2, bank_settled_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, payment.ID, current.State); err != nil {
		return err
	}
	if err := Transition(&current, StateCompleted); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE payments SET state = $2, completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, payment.ID, current.State); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	requestID := common.RequestIDFromContext(ctx)
	slog.InfoContext(ctx, "routed payment settled central",
		"request_id", requestID,
		"payment_id", payment.ID.String(),
		"ledger_transaction_id", ledgerTransactionID.String(),
		"old_state", currentState,
		"new_state", StateCompleted,
	)
	return nil
}

func (repository *Repository) RecoverBankSettledCentralPending(ctx context.Context, paymentID uuid.UUID) (Payment, error) {
	payment, err := repository.Get(ctx, paymentID)
	if err != nil {
		return Payment{}, err
	}
	if payment.State != StateBankSettledCentralPending {
		return payment, nil
	}
	if err := repository.settleRoutedCentral(ctx, payment); err != nil {
		return Payment{}, err
	}
	return repository.Get(ctx, paymentID)
}

func (repository *Repository) getBankOperation(ctx context.Context, paymentID uuid.UUID, operationType string) (routedOperation, bool, error) {
	var operation routedOperation
	err := repository.db.QueryRow(ctx, `
		SELECT payment_id, bank_id, operation_id, operation_type, status, hold_id, original_operation_id, account_id
		FROM payment_bank_operations WHERE payment_id = $1 AND operation_type = $2`, paymentID, operationType).Scan(
		&operation.PaymentID, &operation.BankID, &operation.OperationID, &operation.OperationType, &operation.Status,
		&operation.HoldID, &operation.OriginalOperationID, &operation.AccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return routedOperation{}, false, nil
	}
	return operation, true, err
}

func (repository *Repository) updateState(ctx context.Context, paymentID uuid.UUID, state string) error {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var currentState string
	if err := tx.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1 FOR UPDATE`, paymentID).Scan(&currentState); err != nil {
		return err
	}
	if currentState != state {
		current := Payment{State: currentState}
		if err := Transition(&current, state); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE payments SET state = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, paymentID, state); err != nil {
			return err
		}
		requestID := common.RequestIDFromContext(ctx)
		slog.InfoContext(ctx, "payment state transition",
			"request_id", requestID,
			"payment_id", paymentID.String(),
			"old_state", currentState,
			"new_state", state,
		)
	}
	return tx.Commit(ctx)
}

func (repository *Repository) markPending(ctx context.Context, paymentID uuid.UUID) error {
	return repository.updateState(ctx, paymentID, StatePendingReconciliation)
}

func (repository *Repository) markBankSettledPending(ctx context.Context, paymentID uuid.UUID) error {
	return repository.updateState(ctx, paymentID, StateBankSettledCentralPending)
}

func (repository *Repository) markFailure(ctx context.Context, paymentID uuid.UUID, cause error) error {
	requestID := common.RequestIDFromContext(ctx)
	slog.WarnContext(ctx, "payment marked failed",
		"request_id", requestID,
		"payment_id", paymentID.String(),
		"error", cause,
	)
	if err := repository.updateState(ctx, paymentID, StateFailed); err != nil {
		return err
	}
	_, err := repository.db.Exec(ctx, `UPDATE payments SET failure_reason = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND state = 'FAILED'`, paymentID, customerSafeFailureReason(cause))
	return err
}

func (repository *Repository) handleInitialFailure(ctx context.Context, paymentID uuid.UUID, cause error) error {
	if isTransientBankError(cause) {
		if err := repository.markPending(ctx, paymentID); err != nil {
			return err
		}
		return nil
	}
	if err := repository.markFailure(ctx, paymentID, cause); err != nil {
		return err
	}
	return cause
}

func (repository *Repository) trackBankOperation(ctx context.Context, paymentID, bankID, operationID uuid.UUID, operationType string, accountID uuid.UUID, amount int64, currency string, holdID, originalOperationID uuid.UUID, status string) (bool, error) {
	var insertedID uuid.UUID
	err := repository.db.QueryRow(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, hold_id, original_operation_id, account_id, idempotency_key, amount_paise, currency)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '00000000-0000-0000-0000-000000000000'::uuid), NULLIF($8, '00000000-0000-0000-0000-000000000000'::uuid), NULLIF($9, '00000000-0000-0000-0000-000000000000'::uuid), $10, $11, $12)
		ON CONFLICT (bank_id, operation_id) DO NOTHING
		RETURNING id`, uuid.New(), paymentID, bankID, operationID, operationType, status, holdID, originalOperationID, accountID, operationID.String(), amount, currency).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (repository *Repository) updateBankOperation(ctx context.Context, operationID uuid.UUID, status string) error {
	_, err := repository.db.Exec(ctx, `UPDATE payment_bank_operations SET status = $2, updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, operationID, status)
	return err
}

func (repository *Repository) updateBankOperationResult(ctx context.Context, operationID uuid.UUID, status, bankReference string) error {
	_, err := repository.db.Exec(ctx, `UPDATE payment_bank_operations SET status = $2, bank_reference = NULLIF($3, ''), updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, operationID, status, bankReference)
	return err
}

func isTransientBankError(err error) bool {
	var adapterErr *bank.AdapterError
	return errors.As(err, &adapterErr) && (adapterErr.Code == bank.ErrCodeTransientFailure || adapterErr.Code == bank.ErrCodeBankUnavailable)
}

func accountStatusError(status bank.AccountStatus, side string) error {
	if status == bank.AccountInactive {
		return &bank.AdapterError{Code: bank.ErrCodeInactiveAccount, Message: side + " bank account is inactive"}
	}
	return &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: side + " bank account is invalid"}
}

func customerSafeFailureReason(cause error) string {
	if cause == nil {
		return ""
	}
	var adapterErr *bank.AdapterError
	if errors.As(cause, &adapterErr) {
		switch adapterErr.Code {
		case bank.ErrCodeInsufficientFunds:
			return "Insufficient funds."
		case bank.ErrCodeInvalidAccount:
			return "Account could not be found."
		case bank.ErrCodeInactiveAccount:
			return "Account is inactive."
		case bank.ErrCodeBankUnavailable:
			return "Bank service is temporarily unavailable."
		case bank.ErrCodeTransientFailure:
			return "Payment is still being confirmed."
		default:
			return "Payment could not be completed."
		}
	}
	switch {
	case errors.Is(cause, ErrInsufficientFunds):
		return "Insufficient funds."
	case errors.Is(cause, ErrSourceNotFound), errors.Is(cause, ErrRecipientNotFound):
		return "Account could not be found."
	case errors.Is(cause, ErrSourceInactive), errors.Is(cause, ErrRecipientInactive):
		return "Account is inactive."
	case errors.Is(cause, ErrSelfPayment):
		return "Payer cannot pay their own account."
	case errors.Is(cause, ErrIdempotencyConflict):
		return "Payment request conflicted with an existing request."
	case errors.Is(cause, ErrBankRouteUnavailable):
		return "Bank service is temporarily unavailable."
	default:
		return "Payment could not be completed."
	}
}

func bankAccountID(mapped *uuid.UUID, fallback uuid.UUID) uuid.UUID {
	if mapped != nil && *mapped != uuid.Nil {
		return *mapped
	}
	return fallback
}

func pointerValue(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}

func uuidPointer(value uuid.UUID) *uuid.UUID { return &value }

func operationID(paymentID uuid.UUID, name string) uuid.UUID {
	return uuid.NewSHA1(paymentID, []byte(name))
}
