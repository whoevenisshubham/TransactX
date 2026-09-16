package payments

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/ledger"
)

func (repository *Repository) CreateRoutedIdempotent(ctx context.Context, payment Payment, key, requestHash string, sourceBankID, destinationBankID uuid.UUID, sourceAdapter, destinationAdapter bank.BankAdapter) (Payment, bool, error) {
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
		existing, getErr := scanPayment(tx.QueryRow(ctx, `SELECT id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, route_bank_id, failure_reason, created_at, updated_at, completed_at FROM payments WHERE id = $1`, *existingPaymentID))
		return existing, true, getErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, false, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, source_bank_id, destination_bank_id, routing_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'static M1-6 adapter route')`, payment.ID, payment.InitiatedByUserID, payment.SenderAccountID, payment.ReceiverAccountID, payment.AmountPaise, payment.Currency, payment.State, sourceBankID, destinationBankID); err != nil {
		return Payment{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id, response_snapshot) VALUES ($1, $2, $3, $4, $5, jsonb_build_object('paymentId', $6::text, 'state', $7::text))`, uuid.New(), payment.InitiatedByUserID, key, requestHash, payment.ID, payment.ID.String(), payment.State); err != nil {
		return Payment{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Payment{}, false, err
	}

	if err := repository.runRoutedSaga(ctx, payment, sourceBankID, destinationBankID, sourceAdapter, destinationAdapter); err != nil {
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
	if err := repository.updateState(ctx, payment.ID, StateValidating); err != nil {
		return err
	}
	if err := repository.updateState(ctx, payment.ID, StateRouting); err != nil {
		return err
	}
	if err := repository.updateState(ctx, payment.ID, StateProcessing); err != nil {
		return err
	}

	holdID := operationID(payment.ID, "hold")
	hold, err := sourceAdapter.HoldFunds(ctx, bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{PaymentID: payment.ID, OperationID: holdID, IdempotencyKey: holdID.String(), AccountID: payment.SenderAccountID, AmountPaise: payment.AmountPaise, Currency: payment.Currency}})
	if err != nil {
		return repository.failOrPend(ctx, payment.ID, err)
	}

	creditID := operationID(payment.ID, "provisional-credit")
	credit, err := destinationAdapter.ProvisionalCredit(ctx, bank.ProvisionalCreditRequest{OperationRequest: bank.OperationRequest{PaymentID: payment.ID, OperationID: creditID, IdempotencyKey: creditID.String(), AccountID: payment.ReceiverAccountID, AmountPaise: payment.AmountPaise, Currency: payment.Currency}})
	if err != nil {
		releaseID := operationID(payment.ID, "release-hold")
		_, releaseErr := sourceAdapter.ReleaseHold(ctx, bank.ReleaseHoldRequest{PaymentID: payment.ID, OperationID: releaseID, IdempotencyKey: releaseID.String(), HoldID: hold.HoldID})
		if releaseErr != nil || credit.Status == bank.OperationPending {
			return repository.markPending(ctx, payment.ID)
		}
		return repository.failOrPend(ctx, payment.ID, err)
	}

	confirmID := operationID(payment.ID, "confirm-hold")
	if _, err := sourceAdapter.ConfirmHold(ctx, bank.ConfirmHoldRequest{PaymentID: payment.ID, OperationID: confirmID, IdempotencyKey: confirmID.String(), HoldID: hold.HoldID}); err != nil {
		return repository.compensateRoutedPayment(ctx, payment, sourceAdapter, destinationAdapter, hold.HoldID, credit.OperationID, err)
	}

	finalizeID := operationID(payment.ID, "finalize-credit")
	if _, err := destinationAdapter.ConfirmHold(ctx, bank.ConfirmHoldRequest{PaymentID: payment.ID, OperationID: finalizeID, IdempotencyKey: finalizeID.String(), HoldID: credit.OperationID}); err != nil {
		return repository.compensateRoutedPayment(ctx, payment, sourceAdapter, destinationAdapter, hold.HoldID, credit.OperationID, err)
	}

	if err := repository.settleRoutedCentral(ctx, payment); err != nil {
		return repository.markBankSettledPending(ctx, payment.ID)
	}
	return nil
}

func (repository *Repository) compensateRoutedPayment(ctx context.Context, payment Payment, sourceAdapter, destinationAdapter bank.BankAdapter, holdID, creditID uuid.UUID, originalErr error) error {
	releaseID := operationID(payment.ID, "release-hold")
	_, releaseErr := sourceAdapter.ReleaseHold(ctx, bank.ReleaseHoldRequest{PaymentID: payment.ID, OperationID: releaseID, IdempotencyKey: releaseID.String(), HoldID: holdID})
	reverseID := operationID(payment.ID, "reverse-credit")
	_, reverseErr := destinationAdapter.ReverseProvisionalCredit(ctx, bank.ReverseCreditRequest{PaymentID: payment.ID, OperationID: reverseID, IdempotencyKey: reverseID.String(), OriginalOperationID: creditID})
	if releaseErr != nil || reverseErr != nil {
		return repository.markPending(ctx, payment.ID)
	}
	return repository.failOrPend(ctx, payment.ID, originalErr)
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
	if _, err := tx.Exec(ctx, `UPDATE payments SET state = 'COMMITTED', bank_settled_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND state IN ('PROCESSING', 'BANK_SETTLED_CENTRAL_PENDING')`, payment.ID); err != nil {
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

func (repository *Repository) failOrPend(ctx context.Context, paymentID uuid.UUID, err error) error {
	var adapterErr *bank.AdapterError
	if errors.As(err, &adapterErr) && adapterErr.Code == bank.ErrCodeTransientFailure {
		return repository.markPending(ctx, paymentID)
	}
	if updateErr := repository.updateState(ctx, paymentID, StateFailed); updateErr != nil {
		return updateErr
	}
	return err
}

func operationID(paymentID uuid.UUID, name string) uuid.UUID {
	return uuid.NewSHA1(paymentID, []byte(name))
}
