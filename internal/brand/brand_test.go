package brand

import "testing"

func reset(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { name, short = demoName, shortDemoName })
}

func TestSetRecordsTheMunicipality(t *testing.T) {
	reset(t)
	Set("Saint-Jean Spaces", "")

	if Name() != "Saint-Jean Spaces" {
		t.Fatalf("name = %q", Name())
	}
	// The city alone, for sentences that name it rather than the service.
	if Short() != "Saint-Jean" {
		t.Fatalf("short = %q, want the trailing service word dropped", Short())
	}
}

// Only a known service word is dropped, never simply the last word — otherwise
// every multi-word city name loses half of itself.
func TestDeriveShortKeepsMultiWordCityNames(t *testing.T) {
	cases := map[string]string{
		"Rivermont Spaces":          "Rivermont",
		"Saint-Jean-sur-Richelieu":  "Saint-Jean-sur-Richelieu",
		"Ville de Québec":           "Ville de Québec",
		"Grande Prairie Recreation": "Grande Prairie",
		"Thunder Bay Facilities":    "Thunder Bay",
		"Halifax":                   "Halifax",
	}
	for full, want := range cases {
		if got := deriveShort(full); got != want {
			t.Errorf("%q → %q, want %q", full, got, want)
		}
	}
}

// A municipality whose name the heuristic would mangle can say so outright.
func TestExplicitShortNameOverridesTheHeuristic(t *testing.T) {
	reset(t)
	Set("Municipalité régionale de comté des Sources", "MRC des Sources")

	if Short() != "MRC des Sources" {
		t.Fatalf("short = %q", Short())
	}
}

// An unconfigured deployment keeps the demo identity rather than describing
// itself as nothing.
func TestBlankNameKeepsTheDefault(t *testing.T) {
	reset(t)
	Set("  ", "")

	if Name() != demoName || Short() != shortDemoName {
		t.Fatalf("blank override changed identity: %q / %q", Name(), Short())
	}
}
