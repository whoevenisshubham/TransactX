package accounts

import (
	"time"

	"github.com/google/uuid"
)

type Account struct {
	ID                  uuid.UUID `json:"id"`
	UserID              uuid.UUID `json:"userId"`
	BankID              uuid.UUID `json:"bankId"`
	AccountNumber       string    `json:"accountNumber"`
	BalancePaise        int64     `json:"balancePaise"`
	OpeningBalancePaise int64     `json:"-"`
	Version             int64     `json:"version"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}
