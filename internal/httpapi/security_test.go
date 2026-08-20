package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	cases := []struct {
		name     string
		prod     bool
		wantHSTS bool
	}{
		{"dev omits HSTS", false, false},
		{"prod sets HSTS", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			securityHeaders(tc.prod)(ok).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
			h := rr.Header()
			for _, header := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Content-Security-Policy"} {
				if h.Get(header) == "" {
					t.Errorf("missing %s", header)
				}
			}
			if h.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("nosniff = %q", h.Get("X-Content-Type-Options"))
			}
			if got := h.Get("Strict-Transport-Security") != ""; got != tc.wantHSTS {
				t.Errorf("HSTS present = %v, want %v", got, tc.wantHSTS)
			}
		})
	}
}
