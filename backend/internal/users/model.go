package users

import "github.com/google/uuid"

const (
	RoleCustomer = "CUSTOMER"
	RoleMerchant = "MERCHANT"
	RoleOpsAdmin = "OPS_ADMIN"
)

type User struct {
	ID           uuid.UUID
	Name         string
	Phone        string
	PaymentID    string
	PasswordHash string
	Role         string
}

type PublicUser struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	PaymentID string    `json:"paymentIdentifier"`
	Role      string    `json:"role"`
}

func (user User) Public() PublicUser {
	return PublicUser{ID: user.ID, Name: user.Name, PaymentID: user.PaymentID, Role: user.Role}
}

func IsPublicRole(role string) bool {
	return role == RoleCustomer || role == RoleMerchant
}
