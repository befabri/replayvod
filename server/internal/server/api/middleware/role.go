package middleware

import (
	"net/http"
)

const (
	RoleViewer = "viewer"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
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
