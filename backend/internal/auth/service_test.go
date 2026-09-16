package auth

import (
	"context"
	"testing"
)

func TestPublicRegistrationRejectsPrivilegedRolesBeforeDatabaseAccess(t *testing.T) {
	service := &Service{}
	for _, role := range []string{"OPS_ADMIN", "UNKNOWN"} {
		_, err := service.Register(context.Background(), RegisterInput{
			Name:              "Test User",
			Phone:             "919876543210",
			PaymentIdentifier: "test@transactx",
			Password:          "password123",
			Role:              role,
		})
		if err == nil || err.Error() != "INVALID_REQUEST" {
			t.Fatalf("role %q was not rejected before database access: %v", role, err)
		}
	}
}
