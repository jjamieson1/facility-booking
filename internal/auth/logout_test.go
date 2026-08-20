package auth

import (
	"context"
	"net/url"
	"testing"

	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func logoutTestService(t *testing.T) *Service {
	t.Helper()
	db := testdb.New(t)
	return &Service{
		db:            db,
		cfg:           config.Config{OIDCClientID: "client-1"},
		endSessionURL: "http://localhost:5173/oidc/end_session",
		postLogoutURL: "http://localhost:5180/",
	}
}

// The session keeps the login's ID token so logout can present it to C2 as
// id_token_hint; closing the session returns it and removes the row.
func TestCloseSessionReturnsIDToken(t *testing.T) {
	svc := logoutTestService(t)
	ctx := context.Background()

	u := domain.User{Subject: "sub-1", Email: "a@example.com", Name: "A"}
	if err := svc.db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	id, err := svc.OpenSession(ctx, Login{User: &u, IDToken: "raw.id.token"})
	if err != nil {
		t.Fatalf("OpenSession() err = %v", err)
	}

	got, err := svc.CloseSession(ctx, id)
	if err != nil {
		t.Fatalf("CloseSession() err = %v", err)
	}
	if got != "raw.id.token" {
		t.Fatalf("CloseSession() token = %q, want %q", got, "raw.id.token")
	}

	var count int64
	if err := svc.db.Model(&domain.Session{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("sessions remaining = %d, want 0", count)
	}
}

// Logout is idempotent: an unknown session id is not an error (the cookie may
// point at a session a back-channel logout already revoked).
func TestCloseSessionUnknownID(t *testing.T) {
	svc := logoutTestService(t)
	got, err := svc.CloseSession(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("CloseSession() err = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("CloseSession() token = %q, want empty", got)
	}
}

// LogoutURL points at C2's end_session endpoint carrying the client id, the
// id_token_hint (C2 ends no session without it) and the registered
// post_logout_redirect_uri.
func TestLogoutURL(t *testing.T) {
	svc := logoutTestService(t)

	raw := svc.LogoutURL("raw.id.token")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("LogoutURL() = %q, not a URL: %v", raw, err)
	}
	if got := u.Scheme + "://" + u.Host + u.Path; got != "http://localhost:5173/oidc/end_session" {
		t.Fatalf("LogoutURL() endpoint = %q", got)
	}
	q := u.Query()
	if q.Get("client_id") != "client-1" {
		t.Fatalf("client_id = %q, want client-1", q.Get("client_id"))
	}
	if q.Get("id_token_hint") != "raw.id.token" {
		t.Fatalf("id_token_hint = %q, want raw.id.token", q.Get("id_token_hint"))
	}
	if q.Get("post_logout_redirect_uri") != "http://localhost:5180/" {
		t.Fatalf("post_logout_redirect_uri = %q", q.Get("post_logout_redirect_uri"))
	}
}

// With no ID token on the session (e.g. a session opened before this field
// existed) the URL still resolves, minus the hint.
func TestLogoutURLWithoutIDToken(t *testing.T) {
	svc := logoutTestService(t)
	q, err := url.Parse(svc.LogoutURL(""))
	if err != nil {
		t.Fatal(err)
	}
	if q.Query().Has("id_token_hint") {
		t.Fatal("id_token_hint present for an empty token")
	}
}

// Without OIDC there is no upstream session to end, so callers get "" and fall
// back to a local-only logout.
func TestLogoutURLNotConfigured(t *testing.T) {
	if got := (&Service{}).LogoutURL("raw.id.token"); got != "" {
		t.Fatalf("LogoutURL() = %q, want empty", got)
	}
	var nilSvc *Service
	if got := nilSvc.LogoutURL(""); got != "" {
		t.Fatalf("LogoutURL() on nil service = %q, want empty", got)
	}
}
