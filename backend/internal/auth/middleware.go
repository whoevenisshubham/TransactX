package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/users"
)

type identityKey struct{}

type Identity struct {
	UserID string
	Role   string
}

func Authentication(manager *JWTManager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := request.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			common.WriteError(writer, common.GetRequestID(request), common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
			return
		}
		claims, err := manager.Parse(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), time.Now())
		if err != nil {
			common.WriteError(writer, common.GetRequestID(request), common.NewAPIError("UNAUTHORIZED", "authentication is required", http.StatusUnauthorized))
			return
		}
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), identityKey{}, Identity{UserID: claims.Subject, Role: claims.Role})))
	})
}

func RequireRole(allowed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			identity, ok := IdentityFromRequest(request)
			if !ok || !contains(allowed, identity.Role) {
				common.WriteError(writer, common.GetRequestID(request), common.NewAPIError("FORBIDDEN", "access is not permitted", http.StatusForbidden))
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func IdentityFromRequest(request *http.Request) (Identity, bool) {
	identity, ok := request.Context().Value(identityKey{}).(Identity)
	return identity, ok
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func PublicRoles() []string { return []string{users.RoleCustomer, users.RoleMerchant} }
