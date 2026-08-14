package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/servicecard"
)

type serviceCardHandler struct {
	auth *auth.Service        // verifies the callout JWT against C2's JWKS
	svc  *servicecard.Service // builds the per-citizen payload
}

// status is the C2 service-card callout: a server-to-server GET that C2 makes,
// authenticated by a short-lived RS256 JWT, returning this citizen's live booking
// summary. It is NOT session-authenticated — it does its own JWT verification, so
// it lives outside the cookie-auth group. See ServiceCardCallback.md.
func (h serviceCardHandler) status(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		// OIDC not configured → we can't verify the callout. Non-2xx tells C2 to
		// fall back to the static card.
		writeError(w, http.StatusServiceUnavailable, "callout not available")
		return
	}

	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	subject, err := h.auth.VerifyServiceToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	// The verified token's subject must match the citizen named in the path — a
	// caller can only ever read the subject its own token is about.
	if pathSub := chi.URLParam(r, "sub"); pathSub != "" && pathSub != subject {
		writeError(w, http.StatusForbidden, "subject mismatch")
		return
	}

	payload, found, err := h.svc.StatusForSubject(r.Context(), subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not build payload")
		return
	}
	if !found {
		// Unknown citizen → empty-but-valid 200 (spec §4), never a guess.
		writeJSON(w, http.StatusOK, struct{}{})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// bearerToken extracts a Bearer token from the Authorization header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
