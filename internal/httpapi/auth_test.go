package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// logoutTestHandler wires a real auth service (OIDC configured, but no network
// calls on this path) over an in-memory DB.
func logoutTestHandler(t *testing.T) (authHandler, *gorm.DB) {
	t.Helper()
	db := testdb.New(t)
	cfg := config.Config{
		AppOrigin:                 "http://localhost:5180",
		PublicAppURL:              "http://localhost:5180",
		OIDCIssuer:                "http://localhost:5173/oidc",
		OIDCBaseURL:               "http://localhost:5173/oidc",
		OIDCClientID:              "client-1",
		OIDCClientSecret:          "secret",
		OIDCPostLogoutRedirectURL: "http://localhost:5180/",
	}
	svc, err := auth.NewService(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return authHandler{svc: svc, appBase: cfg.PublicAppURL}, db
}

// Logout must end the local session AND hand back C2's RP-initiated logout URL:
// clearing only the local session leaves the user SSO'd at C2, which silently
// signs them straight back in (C2-Integration-Guide §6.1).
func TestLogoutReturnsC2LogoutURL(t *testing.T) {
	h, db := logoutTestHandler(t)

	u := domain.User{Subject: "sub-1", Email: "a@example.com", Name: "A"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	sessionID, err := h.svc.OpenSession(context.Background(), auth.Login{User: &u, IDToken: "raw.id.token"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "fb_session", Value: sessionID})
	rr := httptest.NewRecorder()
	h.logout(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Status    string `json:"status"`
		LogoutURL string `json:"logoutUrl"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	logoutURL, err := url.Parse(body.LogoutURL)
	if err != nil {
		t.Fatalf("logoutUrl = %q: %v", body.LogoutURL, err)
	}
	if !strings.HasSuffix(logoutURL.Path, "/oidc/end_session") {
		t.Errorf("logoutUrl path = %q, want C2's end_session endpoint", logoutURL.Path)
	}
	if got := logoutURL.Query().Get("id_token_hint"); got != "raw.id.token" {
		t.Errorf("id_token_hint = %q, want the session's ID token", got)
	}

	// Local session gone and cookie expired.
	var count int64
	if err := db.Model(&domain.Session{}).Where("id = ?", sessionID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("sessions remaining = %d, want 0", count)
	}
	if !strings.Contains(rr.Header().Get("Set-Cookie"), "fb_session=;") {
		t.Errorf("Set-Cookie = %q, want the session cookie cleared", rr.Header().Get("Set-Cookie"))
	}
}

// With OIDC unconfigured the service is nil: logout still succeeds locally and
// simply offers no upstream logout URL.
func TestLogoutWithoutOIDC(t *testing.T) {
	h := authHandler{appBase: "http://localhost:5180"}
	rr := httptest.NewRecorder()
	h.logout(rr, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "logoutUrl") {
		t.Errorf("body = %s, want no logoutUrl", rr.Body.String())
	}
}

// Behind a prefix-stripping proxy the SPA lives under a base path while the API
// sees bare /api/... paths. Post-login the browser must land inside the SPA, so
// the redirect is built from the SPA's public base, never the bare origin.
//
// Getting this wrong is not a 404 you would notice in dev, where base and origin
// are the same string: on the QA host the citizen lands on Apache's default page
// and it looks as though login silently failed.
func TestLandingURLUsesTheSPABaseNotTheOrigin(t *testing.T) {
	h := authHandler{appBase: "https://facility-booking.dev-pro.app/facility-booking"}

	for _, tc := range []struct{ name, returnTo, want string }{
		{
			name:     "default lands on the SPA root, not the server root",
			returnTo: "",
			want:     "https://facility-booking.dev-pro.app/facility-booking/",
		},
		{
			name: "a base-relative return path keeps the prefix",
			// What the SPA sends: react-router reports paths without the
			// basename, so this is "/my-bookings", not the full public path.
			returnTo: "/my-bookings",
			want:     "https://facility-booking.dev-pro.app/facility-booking/my-bookings",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.landingURL(tc.returnTo); got != tc.want {
				t.Errorf("landingURL(%q) = %q, want %q", tc.returnTo, got, tc.want)
			}
		})
	}
}

// With no base path (local dev) FB_PUBLIC_APP_URL defaults to the origin, and
// the behaviour must be exactly what it always was.
func TestLandingURLWithoutABasePath(t *testing.T) {
	h := authHandler{appBase: "http://localhost:5180"}
	if got, want := h.landingURL(""), "http://localhost:5180/"; got != want {
		t.Errorf("landingURL(%q) = %q, want %q", "", got, want)
	}
}
