package payments

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
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
		if existing.State == StatePendingReconciliation || existing.State == StateProcessing {
			recoveryErr := repository.RecoverRoutedPayment(ctx, existing, sourceAdapter, destinationAdapter)
			recovered, getErr := repository.Get(ctx, existing.ID)
			if getErr != nil {
				return Payment{}, false, getErr
			}
			return recovered, true, recoveryErr
		}
		return existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, false, err
	}

	created, err := scanPayment(tx.QueryRow(ctx, `
		INSERT INTO payments
			(id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state,
			 source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id, routing_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'static M1-6 adapter route')
		RETURNING `+routedPaymentColumns, payment.ID, payment.InitiatedByUserID, payment.SenderAccountID,
		payment.ReceiverAccountID, payment.AmountPaise, payment.Currency, payment.State, sourceBankID, destinationBankID,
		*sourceBankAccountID, *destinationBankAccountID))
	if err != nil {
		return Payment{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id, response_snapshot) VALUES ($1, $2, $3, $4, $5, jsonb_build_object('paymentId', $6::text, 'state', $7::text))`, uuid.New(), payment.InitiatedByUserID, key, requestHash, payment.ID, payment.ID.String(), payment.State); err != nil {
		return Payment{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Payment{}, false, err
	}

	if err := repository.runRoutedSaga(ctx, created, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter); err != nil {
		result, getErr := repository.Get(ctx, payment.ID)
		if getErr != nil {
			return Payment{}, false, getErr
		}
		return result, false, err
	}
	result, err := repository.Get(ctx, payment.ID)
	return result, false, err
}

func (repository *Repository) runRoutedSaga(ctx context.Context, payment Payment, sourceBankID, destinationBankID uuid.UUID, sourceAdapter, destinationAdapter bank.BankAdapter) error {
	for _, next := range []string{StateValidating, StateRouting, StateProcessing} {
		if err := repository.updateState(ctx, payment.ID, next); err != nil {
			return err
		}
	}
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
		if statusErr != nil || status.Status == bank.OperationPending {
			_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
			return operationPending, statusErr
		}
		if status.Status == bank.OperationFailed {
			_ = repository.updateBankOperation(ctx, operationID, bankOperationFailed)
			return operationFailed, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: operationType + " failed"}
		}
		if status.OperationID != uuid.Nil && status.OperationID != operationID {
			return operationFailed, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: operationType + " returned the wrong operation identity"}
		}
		if err := repository.updateBankOperationResult(ctx, operationID, bankOperationSucceeded, status.BankReference); err != nil {
			return operationPending, err
		}
		return operationSucceeded, nil
	}

	if err := repository.trackBankOperation(ctx, payment.ID, bankID, operationID, operationType, accountID, payment.AmountPaise, payment.Currency, holdID, originalOperationID, bankOperationProcessing); err != nil {
		return operationFailed, err
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
	if result.OperationID != uuid.Nil && result.OperationID != operationID {
		_ = repository.updateBankOperation(ctx, operationID, bankOperationFailed)
		return operationFailed, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: operationType + " returned the wrong operation identity"}
	}
	if result.Status == bank.OperationPending {
		_ = repository.updateBankOperation(ctx, operationID, bankOperationPending)
		return operationPending, nil
	}
	if result.Status == bank.OperationFailed {
		_ = repository.updateBankOperation(ctx, operationID, bankOperationFailed)
		return operationFailed, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: operationType + " failed"}
	}
	if err := repository.updateBankOperationResult(ctx, operationID, bankOperationSucceeded, result.BankReference); err != nil {
		return operationPending, err
	}
	return operationSucceeded, nil
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
	if _, err := tx.Exec(ctx, `UPDATE payments SET state = 'COMMITTED', bank_settled_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND state IN ('PROCESSING', 'PENDING_RECONCILIATION', 'BANK_SETTLED_CENTRAL_PENDING')`, payment.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE payments SET state = 'COMPLETED', completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND state = 'COMMITTED'`, payment.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	_, err := repository.db.Exec(ctx, `UPDATE payments SET state = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, paymentID, state)
	return err
}

func (repository *Repository) markPending(ctx context.Context, paymentID uuid.UUID) error {
	return repository.updateState(ctx, paymentID, StatePendingReconciliation)
}

func (repository *Repository) markBankSettledPending(ctx context.Context, paymentID uuid.UUID) error {
	return repository.updateState(ctx, paymentID, StateBankSettledCentralPending)
}

func (repository *Repository) markFailure(ctx context.Context, paymentID uuid.UUID, cause error) error {
	_, err := repository.db.Exec(ctx, `UPDATE payments SET state = 'FAILED', failure_reason = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, paymentID, errorMessage(cause))
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

func (repository *Repository) trackBankOperation(ctx context.Context, paymentID, bankID, operationID uuid.UUID, operationType string, accountID uuid.UUID, amount int64, currency string, holdID, originalOperationID uuid.UUID, status string) error {
	_, err := repository.db.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, hold_id, original_operation_id, account_id, idempotency_key, amount_paise, currency)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '00000000-0000-0000-0000-000000000000'::uuid), NULLIF($8, '00000000-0000-0000-0000-000000000000'::uuid), NULLIF($9, '00000000-0000-0000-0000-000000000000'::uuid), $10, $11, $12)
		ON CONFLICT (bank_id, operation_id) DO UPDATE SET status = EXCLUDED.status, updated_at = CURRENT_TIMESTAMP`,
		uuid.New(), paymentID, bankID, operationID, operationType, status, holdID, originalOperationID, accountID, operationID.String(), amount, currency)
	return err
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

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
