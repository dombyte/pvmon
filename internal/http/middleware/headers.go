// Package middleware holds HTTP middleware; handlers live in package handler.
package middleware

import "net/http"

// csp allows only same-origin resources; inline styles are needed by Vue style bindings
// (spec 10).
const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'"

// Headers sets the security headers of spec 10 on every response.
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
