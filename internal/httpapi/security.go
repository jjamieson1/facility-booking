package httpapi

import "net/http"

// securityHeaders sets baseline response security headers on every API response
// (skills/Security.md §4). HSTS is only meaningful over TLS, so it's emitted in
// production. Handlers that need a different policy for their body (e.g. the
// waiver download's locking CSP) override these with their own Header().Set.
func securityHeaders(prod bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			if prod {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
