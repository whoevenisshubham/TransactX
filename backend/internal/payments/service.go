package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/ledger"
	"github.com/transactx/backend/internal/recipients"
)

var (
	ErrInvalidRequest       = errors.New("invalid payment request")
	ErrSourceNotFound       = errors.New("source account not found")
	ErrSourceInactive       = errors.New("source account is inactive")
	ErrRecipientNotFound    = errors.New("recipient not found")
	ErrRecipientInactive    = errors.New("recipient account is inactive")
	ErrSelfPayment          = errors.New("payer cannot pay their own account")
	ErrBankRouteUnavailable = errors.New("selected bank route is unavailable")
)

type CreateInput struct {
	UserID          uuid.UUID
	SourceAccountID uuid.UUID
	Recipient       string
	AmountPaise     int64
	Currency        string
	IdempotencyKey  string
}

type Service struct {
	accounts   *accounts.Repository
	recipients *recipients.Repository
	payments   *Repository
	ledger     *ledger.Repository
	adapter    bank.BankAdapter
	adapters   map[uuid.UUID]bank.BankAdapter
}

func NewService(accountsRepository *accounts.Repository, recipientsRepository *recipients.Repository, paymentRepository *Repository, adapter bank.BankAdapter) *Service {
	return &Service{accounts: accountsRepository, recipients: recipientsRepository, payments: paymentRepository, ledger: ledger.NewRepository(paymentRepository.db), adapter: adapter}
}

func NewServiceWithAdapters(accountsRepository *accounts.Repository, recipientsRepository *recipients.Repository, paymentRepository *Repository, adapters map[uuid.UUID]bank.BankAdapter) *Service {
	return &Service{accounts: accountsRepository, recipients: recipientsRepository, payments: paymentRepository, ledger: ledger.NewRepository(paymentRepository.db), adapters: adapters}
}

func (service *Service) Create(ctx context.Context, input CreateInput) (Payment, error) {
	payment, _, err := service.CreateWithResult(ctx, input)
	return payment, err
}

func (service *Service) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]CustomerPayment, error) {
	return service.payments.ListForUser(ctx, userID, limit)
}

func (service *Service) GetForUser(ctx context.Context, userID, paymentID uuid.UUID) (CustomerPayment, error) {
	return service.payments.GetForUser(ctx, userID, paymentID)
}

func (service *Service) CreateWithResult(ctx context.Context, input CreateInput) (Payment, bool, error) {
	key := strings.TrimSpace(input.IdempotencyKey)
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.UserID == uuid.Nil || input.SourceAccountID == uuid.Nil ||
		!common.ValidLength(strings.TrimSpace(input.Recipient), 3, 128) ||
		input.AmountPaise <= 0 ||
		!common.ValidLength(key, 1, 255) || strings.ContainsAny(key, "\r\n\t") {
		return Payment{}, false, ErrInvalidRequest
	}
	recipientIdentifier := common.NormalizeIdentifier(input.Recipient)
	requestHash := paymentRequestHash(input.SourceAccountID, recipientIdentifier, input.AmountPaise, currency)
	if payment, duplicate, err := service.payments.GetIdempotent(ctx, input.UserID, key, requestHash); err != nil || duplicate {
		if err == nil && duplicate && service.hasRoutedAdapters() {
			if payment.State == StateBankSettledCentralPending {
				recovered, recoveryErr := service.payments.RecoverBankSettledCentralPending(ctx, payment.ID)
				return recovered, true, recoveryErr
			}
			if payment.State == StatePendingReconciliation || payment.State == StateProcessing {
				sourceAdapter, destinationAdapter, ok := service.routedAdaptersFor(payment.SourceBankID, payment.DestinationBankID)
				if ok {
					recoveryErr := service.payments.RecoverRoutedPayment(ctx, payment, sourceAdapter, destinationAdapter)
					recovered, getErr := service.payments.Get(ctx, payment.ID)
					if getErr != nil {
						return Payment{}, true, getErr
					}
					return recovered, true, recoveryErr
				}
			}
		}
		return payment, duplicate, err
	}
	if currency != "INR" {
		return Payment{}, false, ErrInvalidRequest
	}

	source, err := service.accounts.GetOwned(ctx, input.UserID, input.SourceAccountID)
	if err != nil {
		return Payment{}, false, ErrSourceNotFound
	}
	if source.Status != "ACTIVE" {
		return Payment{}, false, ErrSourceInactive
	}

	recipient, err := service.recipients.Resolve(ctx, recipientIdentifier)
	if err != nil {
		return Payment{}, false, ErrRecipientNotFound
	}
	if recipient.AccountStatus != "ACTIVE" {
		return Payment{}, false, ErrRecipientInactive
	}
	if recipient.AccountID == input.SourceAccountID || recipient.UserID == input.UserID {
		return Payment{}, false, ErrSelfPayment
	}
	if service.adapters != nil || service.adapter != nil {
		sourceAdapter, sourceOK := service.adapter, service.adapter != nil
		destinationAdapter, destinationOK := service.adapter, service.adapter != nil
		if service.adapters != nil {
			sourceAdapter, sourceOK = service.adapters[source.BankID]
			destinationAdapter, destinationOK = service.adapters[recipient.BankID]
		}
		if !sourceOK || !destinationOK {
			return Payment{}, false, ErrBankRouteUnavailable
		}
		return service.payments.CreateRoutedIdempotent(ctx, Payment{
			ID:                       uuid.New(),
			InitiatedByUserID:        input.UserID,
			SenderAccountID:          input.SourceAccountID,
			ReceiverAccountID:        recipient.AccountID,
			AmountPaise:              input.AmountPaise,
			Currency:                 "INR",
			State:                    StateCreated,
			SourceBankAccountID:      uuidPointer(source.BankAccountID),
			DestinationBankAccountID: uuidPointer(recipient.BankAccountID),
		}, key, requestHash, source.BankID, recipient.BankID, sourceAdapter, destinationAdapter)
	}

	return service.payments.CreateAndSettleIdempotent(ctx, Payment{
		ID:                uuid.New(),
		InitiatedByUserID: input.UserID,
		SenderAccountID:   input.SourceAccountID,
		ReceiverAccountID: recipient.AccountID,
		AmountPaise:       input.AmountPaise,
		Currency:          "INR",
		State:             StateCreated,
	}, key, requestHash, service.accounts, service.ledger)
}

func (service *Service) hasRoutedAdapters() bool {
	return service.adapter != nil || service.adapters != nil
}

func (service *Service) routedAdaptersFor(sourceBankID, destinationBankID *uuid.UUID) (bank.BankAdapter, bank.BankAdapter, bool) {
	if service.adapters != nil && sourceBankID != nil && destinationBankID != nil {
		source, sourceOK := service.adapters[*sourceBankID]
		destination, destinationOK := service.adapters[*destinationBankID]
		return source, destination, sourceOK && destinationOK
	}
	if service.adapter != nil {
		return service.adapter, service.adapter, true
	}
	return nil, nil, false
}

func paymentRequestHash(sourceAccountID uuid.UUID, recipient string, amountPaise int64, currency string) string {
	payload, _ := json.Marshal(struct {
		SourceAccountID string `json:"sourceAccountId"`
		Recipient       string `json:"recipient"`
		AmountPaise     int64  `json:"amountPaise"`
		Currency        string `json:"currency"`
	}{sourceAccountID.String(), recipient, amountPaise, currency})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
