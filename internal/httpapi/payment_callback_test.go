package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postForm posts a form-encoded body, as C2 does for settlement callbacks.
func postForm(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// The settlement endpoint is unauthenticated by necessity — C2 posts
// server-to-server with no session and no shared secret — so the signed token is
// the only thing between a stranger's POST and a booking marked paid. Nothing
// unsigned may get through.
//
// The test server has no OIDC configured, so verification cannot succeed here at
// all; what it pins is that no request path skips verification and applies a
// settlement anyway.
func TestSettlementCallbackNeverAppliesAnUnverifiedToken(t *testing.T) {
	h, _, _ := fullTestServer(t)

	cases := []struct {
		name string
		body string
	}{
		{"no token at all", ""},
		{"empty token", "status_token="},
		{"not a jwt", "status_token=hello"},
		{"unsigned jwt with alg none", "status_token=eyJhbGciOiJub25lIn0.eyJjbGllbnRfaW52b2ljZV9yZWYiOiJGQi0xIiwiZXZlbnQiOiJwYXltZW50In0."},
		{"structurally valid but forged", "status_token=eyJhbGciOiJSUzI1NiJ9.eyJjbGllbnRfaW52b2ljZV9yZWYiOiJGQi0xIiwiZXZlbnQiOiJwYXltZW50In0.bm90LWEtc2lnbmF0dXJl"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := postForm(t, h, "/api/payments/c2/callback", tc.body)
			if rr.Code >= 200 && rr.Code < 300 {
				t.Fatalf("an unverified settlement was accepted: %d %s", rr.Code, rr.Body.String())
			}
		})
	}
}

// The endpoint must not leak which check failed: the caller is unauthenticated,
// and naming the failing claim helps someone forging a token more than it helps
// C2, which reads only the status code.
func TestSettlementCallbackDoesNotExplainWhyATokenFailed(t *testing.T) {
	h, _, _ := fullTestServer(t)

	rr := postForm(t, h, "/api/payments/c2/callback",
		"status_token=eyJhbGciOiJSUzI1NiJ9.eyJjbGllbnRfaW52b2ljZV9yZWYiOiJGQi0xIn0.c2ln")
	body := strings.ToLower(rr.Body.String())
	for _, leak := range []string{"issuer", "audience", "signature", "kid", "expired", "claim"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response names the failing check (%q): %s", leak, body)
		}
	}
}
