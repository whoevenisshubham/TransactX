package recipients

import "github.com/google/uuid"

type Recipient struct {
	AccountID     uuid.UUID `json:"-"`
	UserID        uuid.UUID `json:"-"`
	BankID        uuid.UUID `json:"-"`
	Name          string    `json:"name"`
	PaymentID     string    `json:"paymentIdentifier"`
	AccountStatus string    `json:"status"`
}
