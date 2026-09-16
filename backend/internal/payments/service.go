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
	payment, _, err := service.CreateWithResult(ctx, input)
	return payment, err
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

	return service.payments.CreateIdempotent(ctx, Payment{
		ID:                uuid.New(),
		InitiatedByUserID: input.UserID,
		SenderAccountID:   input.SourceAccountID,
		ReceiverAccountID: recipient.AccountID,
		AmountPaise:       input.AmountPaise,
		Currency:          "INR",
		State:             StateCreated,
	}, key, requestHash)
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
