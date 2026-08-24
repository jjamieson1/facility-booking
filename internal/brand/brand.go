// Package brand holds the municipality's identity for the API side: the service
// name that reaches residents through calendar invites, the C2 service card and
// the waiver template.
//
// It exists so rebranding is a config change rather than a hunt through
// components (FAC-20). The SPA has its own single source in
// web/src/lib/brand.ts — separate only because that is a build-time TypeScript
// constant this process cannot read. The two are set together.
package brand

import "strings"

// demoName is the sales-demo identity, used when FB_BRAND_NAME is unset.
const demoName = "Rivermont Spaces"

// shortDemoName is the municipality without the service word, for sentences
// that name the city rather than the service ("the City of Rivermont").
const shortDemoName = "Rivermont"

var (
	name  = demoName
	short = shortDemoName
)

// Set records the municipality's name. Called once during wiring, before
// anything serves a request: this is process-wide identity, not per-request
// state. A blank name keeps the demo identity rather than leaving the app
// describing itself as nothing.
//
// shortName is the city on its own ("Saint-Jean" for "Saint-Jean Spaces"); when
// blank it is derived by dropping a trailing service word, which is right often
// enough to be a useful default and always overridable.
func Set(fullName, shortName string) {
	if n := strings.TrimSpace(fullName); n != "" {
		name = n
		short = deriveShort(n)
	}
	if s := strings.TrimSpace(shortName); s != "" {
		short = s
	}
}

// Name is the full service name ("Rivermont Spaces").
func Name() string { return name }

// Short is the municipality alone ("Rivermont"), for sentences that name the
// city rather than the service.
func Short() string { return short }

// deriveShort drops a trailing generic service word. "Rivermont Spaces" is the
// city plus a label; "Saint-Jean" is not. Multi-word city names survive because
// only a known service word is removed, never simply the last word.
func deriveShort(full string) string {
	fields := strings.Fields(full)
	if len(fields) < 2 {
		return full
	}
	switch strings.ToLower(fields[len(fields)-1]) {
	case "spaces", "bookings", "booking", "facilities", "recreation":
		return strings.Join(fields[:len(fields)-1], " ")
	}
	return full
}
