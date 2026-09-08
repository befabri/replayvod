package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

// CORS returns middleware that lets pages from allowedOrigins read responses
// to allowedMethods and send allowedHeaders with credentials. go-chi/cors
// gates actual requests by method as well as preflights, so allowedMethods
// must cover every routed method.
func CORS(allowedOrigins, allowedMethods, allowedHeaders []string) func(http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   allowedMethods,
		AllowedHeaders:   allowedHeaders,
		AllowCredentials: true,
		MaxAge:           300,
	})
}
