package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jjamieson1/facility-booking/internal/auth"
)

// authHandler wires the OIDC login flow. When svc is nil (OIDC not configured)
// the routes return 503 so the rest of the demo still works.
type authHandler struct {
	svc       *auth.Service
	appOrigin string
}

func (h authHandler) routes(r chi.Router) {
	r.Get("/login", h.login)
	r.Get("/callback", h.callback)
	r.Get("/me", h.me)
	r.Post("/logout", h.logout)
	r.Post("/backchannel-logout", h.backchannelLogout)
}

// login redirects the browser to C2's authorize endpoint, stashing state + PKCE
// verifier in a signed cookie for the return trip.
func (h authHandler) login(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "login is not configured")
		return
	}
	state, verifier := randToken(), randToken()
	if err := h.svc.SetStateCookie(w, auth.NewLoginState(state, verifier, sanitizeReturnTo(r.URL.Query().Get("return_to")))); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start login")
		return
	}
	http.Redirect(w, r, h.svc.AuthCodeURL(state, verifier), http.StatusFound)
}

// callback completes the flow: validate state, exchange the code, open a
// session, then bounce back to the SPA.
func (h authHandler) callback(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "login is not configured")
		return
	}
	st, err := h.svc.ReadStateCookie(r)
	h.svc.ClearStateCookie(w)
	if err != nil || r.URL.Query().Get("state") != st.State {
		writeError(w, http.StatusBadRequest, "invalid login state")
		return
	}
	login, err := h.svc.Complete(r.Context(), r.URL.Query().Get("code"), st.Verifier)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "login failed")
		return
	}
	sessionID, err := h.svc.OpenSession(r.Context(), login)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open session")
		return
	}
	h.svc.SetSessionCookie(w, sessionID)
	to := "/"
	if st.ReturnTo != "" {
		to = st.ReturnTo
	}
	http.Redirect(w, r, h.appOrigin+to, http.StatusFound)
}

// me returns the current user, or 200 with null when anonymous (so the SPA can
// render a logged-out state without treating it as an error).
func (h authHandler) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, auth.FromContext(r.Context()))
}

// Residency is no longer set from a request body. It is an entitlement
// determined by a provider — see internal/entitlement and the routes under
// /api/entitlements. The old POST /verify-residency accepted an address and set
// the flag, so anyone could self-declare residency and take the resident rate.

// logoutResp tells the SPA where to send the browser next. logoutUrl is C2's
// RP-initiated logout endpoint; following it ends the C2 session too, so the user
// isn't silently signed back in by SSO. Empty when OIDC is not configured.
type logoutResp struct {
	Status    string `json:"status"`
	LogoutURL string `json:"logoutUrl,omitempty"`
}

// logout clears the session server-side and the cookie client-side, then hands
// the SPA C2's logout URL to complete the single sign-out (§6.1).
func (h authHandler) logout(w http.ResponseWriter, r *http.Request) {
	resp := logoutResp{Status: "logged out"}
	if h.svc != nil {
		var idToken string
		if id := auth.SessionIDFromRequest(r); id != "" {
			idToken, _ = h.svc.CloseSession(r.Context(), id)
		}
		h.svc.ClearSessionCookie(w)
		resp.LogoutURL = h.svc.LogoutURL(idToken)
	}
	writeJSON(w, http.StatusOK, resp)
}

// backchannelLogout receives OIDC Back-Channel Logout notifications from C2,
// validates the logout token, and terminates all local sessions for that user.
func (h authHandler) backchannelLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.svc == nil {
		writeError(w, http.StatusBadRequest, "logout endpoint is not configured")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid logout request")
		return
	}
	raw := r.PostForm.Get("logout_token")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing logout_token")
		return
	}
	subject, err := h.svc.VerifyBackchannelLogoutToken(r.Context(), raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid logout_token")
		return
	}
	if err := h.svc.CloseSessionsForSubject(r.Context(), subject); err != nil {
		writeError(w, http.StatusInternalServerError, "could not apply logout")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// randToken returns a URL-safe 256-bit random string for state/PKCE.
func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func sanitizeReturnTo(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
		return ""
	}
	return v
}
