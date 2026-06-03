package middleware

import (
	"net/http"
)

const (
	RoleViewer = "viewer"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

// Role is the wire enum for a user's role, surfaced on the api/system and
// api/auth response DTOs so trpcgo emits a "viewer" | "admin" | "owner" union
// instead of a bare string. Defined once here, in the package that owns the
// role constants, and referenced from both DTOs so the generated output keeps a
// single Role type. The values alias the Role* constants above, which remain
// the source the HTTP role checks use.
type Role string

const (
	RoleViewerWire Role = RoleViewer
	RoleAdminWire  Role = RoleAdmin
	RoleOwnerWire  Role = RoleOwner
)

var roleLevel = map[string]int{
	RoleViewer: 1,
	RoleAdmin:  2,
	RoleOwner:  3,
}

// RequireRole returns middleware that checks if the user has the minimum required role.
func RequireRole(minRole string) func(http.Handler) http.Handler {
	minLevel := roleLevel[minRole]

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r.Context())
			if user == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			userLevel := roleLevel[user.Role]
			if userLevel < minLevel {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
