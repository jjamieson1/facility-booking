package entitlement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// RollProvider decides residency by checking a submitted address against the
// municipality's address roll.
//
// This is what makes it a *determination* rather than self-assertion: the
// applicant supplies an address — that part is fine, and unavoidable — but the
// provider decides, and an address that is not on the roll is refused. The old
// endpoint took the address and set the flag, which is the flaw this closes.
//
// A remote provider (C2, a tax-roll service) implements the same interface; the
// only difference is where the roll lives.
type RollProvider struct {
	// streets is the set of street names on the municipal roll, lower-cased.
	streets map[string]bool
	// validFor is how long a determination stands before silent re-validation
	// must confirm it again. People move.
	validFor time.Duration
}

// NewRollProvider builds the provider from a municipal address roll.
func NewRollProvider(streets []string, validFor time.Duration) *RollProvider {
	set := make(map[string]bool, len(streets))
	for _, s := range streets {
		if s = normalize(s); s != "" {
			set[s] = true
		}
	}
	if validFor <= 0 {
		validFor = 365 * 24 * time.Hour
	}
	return &RollProvider{streets: set, validFor: validFor}
}

func (p *RollProvider) Name() string  { return "municipal-roll" }
func (p *RollProvider) Types() []Type { return []Type{TypeResidency} }

// Describe publishes what the applicant must supply — an address — and a
// human-readable statement of the policy. Municipal residency rules are public,
// so the prose is fine; what is deliberately absent is the machine-readable
// passing criteria (the roll itself), which would tell someone what to forge.
func (p *RollProvider) Describe(Type) Descriptor {
	return Descriptor{
		Type:      TypeResidency,
		Provider:  p.Name(),
		Version:   "1",
		Statement: "Residents of the municipality pay the resident rate. Enter the address where you live; it is checked against the municipal address roll.",
		Fields: []Field{
			{Key: "address", Label: "Street address", Placeholder: "12 Willow Lane", Required: true},
		},
	}
}

// Evaluate re-checks an existing enrolment. The reference encodes the address
// that was accepted, so a returning resident proves nothing again — and an
// address struck from the roll stops qualifying at the next check.
func (p *RollProvider) Evaluate(_ context.Context, t Type, ref string) (Result, error) {
	if t != TypeResidency {
		return Result{}, ErrUnsupportedType
	}
	street, ok := p.streetForRef(ref)
	if !ok {
		return Result{}, ErrRefUnknown
	}
	return p.decide(street, ref), nil
}

// Enrol evaluates a submitted address for the first time.
func (p *RollProvider) Enrol(_ context.Context, t Type, inputs map[string]string) (Result, error) {
	if t != TypeResidency {
		return Result{}, ErrUnsupportedType
	}
	address := strings.TrimSpace(inputs["address"])
	if address == "" {
		return Result{}, ErrInvalidInput
	}
	street := streetOf(address)
	return p.decide(street, p.refFor(street)), nil
}

// decide is the single place the roll is consulted, so Enrol and Evaluate can
// never drift apart.
func (p *RollProvider) decide(street, ref string) Result {
	if !p.streets[street] {
		return Result{Outcome: domain.EntitlementDenied, Category: "non-resident"}
	}
	until := time.Now().Add(p.validFor)
	return Result{
		Outcome: domain.EntitlementGranted, Category: "resident",
		Ref: ref, ValidUntil: &until,
	}
}

// refFor derives the provider-scoped reference. It is a hash rather than the
// address itself: the reference is stored in our database, and constraint 6 says
// keep the evidence with the provider.
func (p *RollProvider) refFor(street string) string {
	sum := sha256.Sum256([]byte("roll:" + street))
	return hex.EncodeToString(sum[:16])
}

// streetForRef reverses a reference back to the street it was issued for. The
// roll is small and local, so this is a scan; a remote provider would simply ask
// its own system.
func (p *RollProvider) streetForRef(ref string) (string, bool) {
	for street := range p.streets {
		if p.refFor(street) == ref {
			return street, true
		}
	}
	return "", false
}

// streetOf strips a leading civic number so "12 Willow Lane" matches the roll
// entry "Willow Lane".
func streetOf(address string) string {
	fields := strings.Fields(address)
	if len(fields) > 1 && strings.IndexFunc(fields[0], isDigit) >= 0 {
		fields = fields[1:]
	}
	return normalize(strings.Join(fields, " "))
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func normalize(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
