package auth

import (
	"context"
	"time"

	"github.com/transactx/backend/internal/users"
)

// DemoLogin is an explicit public, shared synthetic identity flow. The HTTP
// composition must gate it off unless public demo access is configured.
// Neither role, user ID nor an arbitrary credential is accepted from a caller.
func (service *Service) DemoLogin(ctx context.Context, persona string, now time.Time) (string, users.PublicUser, error) {
	identifier, role := demoIdentity(persona)
	if identifier == "" {
		return "", users.PublicUser{}, ErrInvalidCredentials
	}
	// Require the demo initializer's persistent namespace. A similarly named
	// account in an ordinary database must never become passwordless by accident.
	var initialized bool
	if err := service.db.QueryRow(ctx, `SELECT to_regclass('demo_limits.budgets') IS NOT NULL`).Scan(&initialized); err != nil || !initialized {
		return "", users.PublicUser{}, ErrInvalidCredentials
	}
	var user users.User
	// Exact payment identifier lookup deliberately avoids phone/identifier OR
	// matching in the ordinary password login path.
	err := service.db.QueryRow(ctx, `SELECT id, name, upi_id, role FROM users WHERE upi_id=$1`, identifier).Scan(&user.ID, &user.Name, &user.PaymentID, &user.Role)
	if err != nil || user.Role != role {
		return "", users.PublicUser{}, ErrInvalidCredentials
	}
	token, err := service.jwt.Issue(user.ID.String(), user.Role, now)
	if err != nil {
		return "", users.PublicUser{}, err
	}
	return token, user.Public(), nil
}

func demoIdentity(persona string) (string, string) {
	switch persona {
	case "customer":
		return "alice@transactx", users.RoleCustomer
	case "recipient":
		return "bob@transactx", users.RoleCustomer
	case "merchant":
		return "store@transactx", users.RoleMerchant
	case "operator":
		return "viewer@transactx", users.RoleOpsViewer
	default:
		return "", ""
	}
}
