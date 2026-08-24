package c2

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Config{Origin: srv.URL, ClientID: "client-1", Secret: "s3cret", AppBaseURL: "https://app.example"})
}

// The wire format C2 documents: POST {origin}/partner/notifications, Basic auth
// with the OIDC client credentials, and `sub` — never an email address.
func TestPostNotificationSendsTheDocumentedRequest(t *testing.T) {
	var gotPath, gotAuth string
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(NotificationResult{NotificationID: "n1", Channels: []string{"EMAIL"}})
	})

	res, err := c.PostNotification(context.Background(), Notification{
		Subject: "sub-123", Title: "Booking confirmed", Body: "Full text", ShortBody: "Short", Category: "BUSINESS",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/partner/notifications" {
		t.Errorf("path = %q, want /partner/notifications", gotPath)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("client-1:s3cret"))
	if gotAuth != want {
		t.Errorf("auth header = %q, want Basic client credentials", gotAuth)
	}
	if body["sub"] != "sub-123" || body["subject"] != "Booking confirmed" {
		t.Errorf("body = %+v, want the citizen's sub and the title", body)
	}
	if res.NotificationID != "n1" || len(res.Channels) != 1 {
		t.Errorf("result = %+v, want the id and dispatched channels", res)
	}
}

// We must never send identifying detail: C2 resolves the citizen from `sub`.
func TestNotificationCarriesNoPersonalIdentifiers(t *testing.T) {
	var raw map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := c.PostNotification(context.Background(), Notification{
		Subject: "sub-1", Title: "t", Body: "b",
	}); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"email", "name", "phone", "address"} {
		if _, present := raw[forbidden]; present {
			t.Errorf("request carries %q — C2 resolves the citizen from sub alone", forbidden)
		}
	}
}

// Each documented status maps to a distinct outcome, because the right response
// differs: 403 must not be retried, 429 must be backed off, 401 is ours to fix.
func TestStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusForbidden, ErrNoConsent},
		{http.StatusNotFound, ErrUnknownSubject},
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusTooManyRequests, ErrRateLimited},
	}
	for _, tc := range cases {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
		_, err := c.PostNotification(context.Background(), Notification{Subject: "s", Title: "t", Body: "b"})
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d gave %v, want %v", tc.status, err, tc.want)
		}
	}
}

// An unconfigured client is inert rather than erroring at a bad URL — that is
// how the app runs with no C2 at all.
func TestUnconfiguredClientIsInert(t *testing.T) {
	c := New(Config{})
	if c.Configured() {
		t.Fatal("a client with no origin must not report as configured")
	}
	if _, err := c.PostNotification(context.Background(), Notification{Subject: "s"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

// A partial configuration is still not configured: sending with no credentials
// would just produce 401s against C2 and be audited as failed attempts.
func TestPartialConfigurationIsNotConfigured(t *testing.T) {
	for _, cfg := range []Config{
		{Origin: "https://c2.example"},
		{Origin: "https://c2.example", ClientID: "id"},
		{ClientID: "id", Secret: "s"},
	} {
		if New(cfg).Configured() {
			t.Errorf("%+v should not report as configured", cfg)
		}
	}
}

// An unexpected status must not leak the remote body into our logs or errors.
func TestUnexpectedStatusDoesNotEchoTheBody(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal detail: citizen jane.doe@example.com not found in shard 4"))
	})
	_, err := c.PostNotification(context.Background(), Notification{Subject: "s", Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("want an error")
	}
	if got := err.Error(); contains(got, "jane.doe") || contains(got, "shard") {
		t.Errorf("error echoes the remote body: %q", got)
	}
}

func contains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
