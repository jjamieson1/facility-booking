package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeJWT assembles a deliberately-invalid token from its parts.
//
// Built at runtime rather than written as a literal so no JWT-shaped string
// appears in the source. Secret scanners flag those on shape and entropy alone
// and cannot tell a forged fixture from a leaked credential — and a scanner that
// cries wolf on test data is one people start ignoring. These carry nothing: the
// payload is a made-up invoice reference and the signature is the words
// "not-a-signature".
//
// The three literals this replaced are still in a merged commit and are
// suppressed by fingerprint in .gitleaksignore; assembling new ones at runtime
// is what stops the list growing every time this test gains a case.
func fakeJWT(header, signature string) string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	payload := `{"client_invoice_ref":"FB-1","event":"payment"}`
	return enc(header) + "." + enc(payload) + "." + enc(signature)
}

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
		{"unsigned jwt with alg none", "status_token=" + fakeJWT(`{"alg":"none"}`, "")},
		{"structurally valid but forged", "status_token=" + fakeJWT(`{"alg":"RS256"}`, "not-a-signature")},
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
		"status_token="+fakeJWT(`{"alg":"RS256"}`, "sig"))
	body := strings.ToLower(rr.Body.String())
	for _, leak := range []string{"issuer", "audience", "signature", "kid", "expired", "claim"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response names the failing check (%q): %s", leak, body)
		}
	}
}
