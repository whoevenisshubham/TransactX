package recipients

import "github.com/google/uuid"

type Recipient struct {
	UserID        uuid.UUID `json:"-"`
	Name          string    `json:"name"`
	PaymentID     string    `json:"paymentIdentifier"`
	AccountStatus string    `json:"status"`
}
