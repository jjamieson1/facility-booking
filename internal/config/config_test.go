package config

import (
	"os"
	"testing"
)

// The partner origin is derived from the OIDC base URL, since C2 mounts
// {origin}/partner as a sibling of {origin}/oidc.
//
// This has a test because the wiring silently failed once: the field was
// declared but never assigned in Load, so the origin was always empty and
// notifications fell back to logging while everything still built and passed.
func TestPartnerOriginFrom(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://portal.example.gov/c2/oidc", "https://portal.example.gov/c2"},
		{"http://localhost:5173/oidc", "http://localhost:5173"},
		{"http://localhost:5173/oidc/", "http://localhost:5173"},
		{"  http://localhost:5173/oidc  ", "http://localhost:5173"},
		{"https://portal.example.gov/c2", "https://portal.example.gov/c2"}, // already an origin
		{"", ""}, // nothing configured stays nothing, not a bare "/"
	}
	for _, tc := range cases {
		if got := partnerOriginFrom(tc.in); got != tc.want {
			t.Errorf("partnerOriginFrom(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Load must actually populate the field — the regression above.
func TestLoadPopulatesThePartnerOrigin(t *testing.T) {
	t.Setenv("FB_OIDC_BASE_URL", "https://portal.example.gov/c2/oidc")
	os.Unsetenv("FB_C2_PARTNER_ORIGIN")

	if got := Load().C2PartnerOrigin; got != "https://portal.example.gov/c2" {
		t.Fatalf("C2PartnerOrigin = %q, want it derived from the OIDC base URL", got)
	}
}

// An explicit setting wins, for a deployment where the guess is wrong.
func TestExplicitPartnerOriginWins(t *testing.T) {
	t.Setenv("FB_OIDC_BASE_URL", "https://portal.example.gov/c2/oidc")
	t.Setenv("FB_C2_PARTNER_ORIGIN", "https://partner.example.gov")

	if got := Load().C2PartnerOrigin; got != "https://partner.example.gov" {
		t.Fatalf("C2PartnerOrigin = %q, want the explicit value", got)
	}
}

// The database DSN has no default on purpose: a fallback would let the app boot
// healthy while writing somewhere nobody backs up.
func TestNoDefaultDatabaseDSN(t *testing.T) {
	os.Unsetenv("FB_DB_DSN")
	if got := Load().DBDSN; got != "" {
		t.Fatalf("DBDSN = %q, want empty so db.Open refuses to start", got)
	}
}
