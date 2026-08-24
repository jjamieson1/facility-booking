package calendar

import (
	"strings"
	"testing"

	"github.com/jjamieson1/facility-booking/internal/brand"
)

// resetBrand restores the default so tests do not leak identity into each other.
// Identity is process-wide by design (one municipality per deployment), so the
// cleanup matters — without it these tests would rename the app for whatever
// runs next in this package.
func resetBrand(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { brand.Set(defaultBrandName, defaultBrandShort) })
}

// The demo identity, duplicated here only so a test can restore it.
const (
	defaultBrandName  = "Rivermont Spaces"
	defaultBrandShort = "Rivermont"
)

func TestSetBrandStampsThePRODID(t *testing.T) {
	resetBrand(t)
	brand.Set("Ville de Saint-Jean", "")

	if got := productID(); got != "-//Ville de Saint-Jean//Facility Booking//EN" {
		t.Fatalf("got %q", got)
	}
}

// PRODID uses slashes as its own separators, so a municipality with one in its
// name would otherwise emit a malformed identifier.
func TestPRODIDStripsSlashesFromTheName(t *testing.T) {
	resetBrand(t)
	brand.Set("Ville de Saint-Jean/Richelieu", "")

	got := productID()
	if strings.Count(got, "//") != 3 {
		t.Fatalf("malformed PRODID: %q", got)
	}
	if strings.Contains(got, "Jean/Richelieu") {
		t.Fatalf("slash survived into the name: %q", got)
	}
}

// An empty name must keep the default rather than produce a calendar that
// identifies itself as nothing.
func TestSetBrandIgnoresBlank(t *testing.T) {
	resetBrand(t)
	brand.Set("   ", "")

	if !strings.Contains(productID(), defaultBrandName) {
		t.Fatalf("blank name overrode the default: %q", productID())
	}
}

func TestFeedFilenameIsSafeForAnyName(t *testing.T) {
	resetBrand(t)
	// Accents transliterate rather than vanish: this app ships bilingual, so an
	// accented municipality name is the expected case.
	cases := map[string]string{
		"Rivermont Spaces":              "rivermont-spaces.ics",
		"Ville de Saint-Jean/Richelieu": "ville-de-saint-jean-richelieu.ics",
		"Municipalité d'Été":            "municipalite-d-ete.ics",
		"!!!":                           "facility-bookings.ics",
	}
	for name, want := range cases {
		brand.Set(name, "")
		if got := FeedFilename(); got != want {
			t.Errorf("%q → %q, want %q", name, got, want)
		}
		// Whatever the name, the filename must never carry a path separator or
		// quote that could escape the Content-Disposition header.
		got := FeedFilename()
		for _, bad := range []string{"/", "\\", "\"", " "} {
			if strings.Contains(got, bad) {
				t.Errorf("%q produced an unsafe filename %q", name, got)
			}
		}
	}
}

// The invite must carry the configured identity, not just the PRODID helper.
func TestInviteCarriesTheConfiguredBrand(t *testing.T) {
	resetBrand(t)
	brand.Set("Testville", "")

	var sb strings.Builder
	writeHeader(&sb)
	if !strings.Contains(sb.String(), "PRODID:-//Testville//") {
		t.Fatalf("header does not carry the brand: %q", sb.String())
	}
}
