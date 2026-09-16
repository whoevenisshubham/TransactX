package payments

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/recipients"
)

var (
	ErrInvalidRequest    = errors.New("invalid payment request")
	ErrSourceNotFound    = errors.New("source account not found")
	ErrSourceInactive    = errors.New("source account is inactive")
	ErrRecipientNotFound = errors.New("recipient not found")
	ErrRecipientInactive = errors.New("recipient account is inactive")
	ErrSelfPayment       = errors.New("payer cannot pay their own account")
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
}

func NewService(accountsRepository *accounts.Repository, recipientsRepository *recipients.Repository, paymentRepository *Repository) *Service {
	return &Service{accounts: accountsRepository, recipients: recipientsRepository, payments: paymentRepository}
}

func (service *Service) Create(ctx context.Context, input CreateInput) (Payment, error) {
	if input.UserID == uuid.Nil || input.SourceAccountID == uuid.Nil ||
		!common.ValidLength(strings.TrimSpace(input.Recipient), 3, 128) ||
		input.AmountPaise <= 0 || strings.ToUpper(strings.TrimSpace(input.Currency)) != "INR" ||
		!common.ValidLength(strings.TrimSpace(input.IdempotencyKey), 0, 255) {
		return Payment{}, ErrInvalidRequest
	}

	source, err := service.accounts.GetOwned(ctx, input.UserID, input.SourceAccountID)
	if err != nil {
		return Payment{}, ErrSourceNotFound
	}
	if source.Status != "ACTIVE" {
		return Payment{}, ErrSourceInactive
	}

	recipient, err := service.recipients.Resolve(ctx, common.NormalizeIdentifier(input.Recipient))
	if err != nil {
		return Payment{}, ErrRecipientNotFound
	}
	if recipient.AccountStatus != "ACTIVE" {
		return Payment{}, ErrRecipientInactive
	}
	if recipient.AccountID == input.SourceAccountID || recipient.UserID == input.UserID {
		return Payment{}, ErrSelfPayment
	}

	return service.payments.Create(ctx, Payment{
		ID:                uuid.New(),
		InitiatedByUserID: input.UserID,
		SenderAccountID:   input.SourceAccountID,
		ReceiverAccountID: recipient.AccountID,
		AmountPaise:       input.AmountPaise,
		Currency:          "INR",
		State:             StateCreated,
	})
}
