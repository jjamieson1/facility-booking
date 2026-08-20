// Package entitlement determines externally-sourced, categorised, expiring
// qualifications — residency today, fee-assistance subsidy next (§P2-5.11a).
//
// The rule the whole package exists to enforce: **an entitlement is determined
// by a provider, never asserted by the client.** An applicant may supply inputs
// (an address, a document); the decision is always the provider's.
package entitlement

import (
	"context"
	"errors"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Type identifies a kind of entitlement.
type Type string

const (
	TypeResidency Type = "residency"
	TypeSubsidy   Type = "subsidy" // §5.8 fee assistance — not yet implemented
)

// Disclosure says whether an entitlement may be named in front of bystanders.
// This is a property of the model, not of a screen: a generic "entitlements
// applied: …" line would leak a discreet one onto a counter display or a printed
// receipt, which §5.8 forbids. Fee breakdowns, receipts and staff views all
// honour it.
type Disclosure string

const (
	// DisclosurePublic may be shown — "resident rate applied" is fine.
	DisclosurePublic Disclosure = "public"
	// DisclosureDiscreet must never appear on a patron-facing display; the
	// internal record still itemises it for audit and GL.
	DisclosureDiscreet Disclosure = "discreet"
)

// TypeInfo is the per-type policy. Order is the stacking order for the fee
// engine: lower applies first, each reduction acting on the running amount
// (resident discount before subsidy).
type TypeInfo struct {
	Type       Type       `json:"type"`
	Name       string     `json:"name"`
	Disclosure Disclosure `json:"disclosure"`
	Order      int        `json:"order"`
}

// types is the registry of known entitlement types.
var types = map[Type]TypeInfo{
	TypeResidency: {Type: TypeResidency, Name: "Residency", Disclosure: DisclosurePublic, Order: 10},
	TypeSubsidy:   {Type: TypeSubsidy, Name: "Fee assistance", Disclosure: DisclosureDiscreet, Order: 20},
}

// InfoFor returns the policy for a type.
func InfoFor(t Type) (TypeInfo, bool) {
	i, ok := types[t]
	return i, ok
}

// Public reports whether a determination of this type may be shown to
// bystanders. Unknown types are treated as discreet — the safe default.
func (t Type) Public() bool {
	i, ok := types[t]
	return ok && i.Disclosure == DisclosurePublic
}

// Sentinel errors. The distinction between them is the whole of constraint 3:
// an unreachable provider is *not* a denial.
var (
	// ErrUnreachable means the provider could not be consulted. The caller must
	// fall back to the last good determination, never to a denial.
	ErrUnreachable = errors.New("entitlement: provider unreachable")
	// ErrRefUnknown means the provider no longer recognises the stored
	// reference (enrolment deleted, provider migrated). The holder must re-prove,
	// but this is not their fault and must not be worded as though it were.
	ErrRefUnknown = errors.New("entitlement: provider does not recognise this enrolment")
	// ErrUnsupportedType means no provider is registered for the type.
	ErrUnsupportedType = errors.New("entitlement: no provider for this type")
	// ErrInvalidInput means the applicant's submission did not satisfy the
	// provider's declared input contract.
	ErrInvalidInput = errors.New("entitlement: required information is missing")
)

// Field is one input a provider needs from the applicant.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required"`
}

// Descriptor is the provider's published input contract — what it needs *from
// the applicant* in order to evaluate them, so the proving form renders from
// this rather than from rules hardcoded per municipality.
//
// It deliberately does not publish what makes the check pass. Municipal
// residency rules are public and Statement may describe them in prose, but the
// machine-readable part is the input schema only: publishing the passing
// criteria tells someone what to forge.
type Descriptor struct {
	Type      Type     `json:"type"`
	Provider  string   `json:"provider"`
	Version   string   `json:"version"`
	Statement string   `json:"statement"`
	Fields    []Field  `json:"fields"`
	Evidence  []string `json:"evidence,omitempty"` // accepted document kinds, if any
}

// Result is a provider's decision.
type Result struct {
	Outcome    domain.EntitlementOutcome
	Category   string     // e.g. "resident" / "non-resident"
	Ref        string     // provider-scoped reference for silent re-validation
	ValidUntil *time.Time // nil = no expiry
}

// Provider evaluates one or more entitlement types. Implementations need not be
// remote: subsidy may be verified by staff against an uploaded document, and
// that must satisfy the same interface — forcing every type through an HTTP
// adapter would be the wrong generalisation.
type Provider interface {
	Name() string
	Types() []Type
	// Describe publishes the input contract for the proving form.
	Describe(t Type) Descriptor
	// Evaluate re-checks an existing enrolment by its provider-scoped reference.
	// This is the silent path: a returning holder proves nothing again.
	Evaluate(ctx context.Context, t Type, ref string) (Result, error)
	// Enrol establishes a determination from applicant-supplied inputs.
	Enrol(ctx context.Context, t Type, inputs map[string]string) (Result, error)
}

// Determination is a live entitlement as the app reports it. Ref is carried for
// stamping and re-validation but never serialised: it is the provider's handle
// on a person, and the client has no use for it.
type Determination struct {
	Type        Type       `json:"type"`
	Category    string     `json:"category"`
	Provider    string     `json:"provider"`
	Ref         string     `json:"-"`
	EvaluatedAt time.Time  `json:"evaluatedAt"`
	ValidUntil  *time.Time `json:"validUntil,omitempty"`
	// Stale marks a determination served from cache because the provider could
	// not be reached. It still applies — it is simply not freshly confirmed.
	Stale bool `json:"stale"`
}

// Reason explains why an entitlement is not currently applied.
type Reason string

const (
	// ReasonNotHeld — nothing on file; the holder may prove.
	ReasonNotHeld Reason = "not_held"
	// ReasonNeedsProving — expired, revoked, or the reference is unknown.
	ReasonNeedsProving Reason = "needs_proving"
	// ReasonUnavailable — the provider could not be reached and no usable
	// determination was cached. Normal rates apply, with an explanation.
	ReasonUnavailable Reason = "unavailable"
)

// Notice is a non-applied entitlement and why.
type Notice struct {
	Type   Type   `json:"type"`
	Reason Reason `json:"reason"`
}

// Set is the resolved entitlement picture for one person at one moment. It is
// resolved once, before the booking transaction opens, and the same set prices
// the quote and the charge.
type Set struct {
	Live    []Determination `json:"live"`
	Notices []Notice        `json:"notices"`
}

// Has reports whether a live entitlement of this type is held.
func (s Set) Has(t Type) bool {
	for _, d := range s.Live {
		if d.Type == t {
			return true
		}
	}
	return false
}

// IsResident is the residency shorthand the fee path and reporting split use.
func (s Set) IsResident() bool { return s.Has(TypeResidency) }

// Stamp renders the set as the rows recorded against a booking.
func (s Set) Stamp(bookingID string) []domain.BookingEntitlement {
	out := make([]domain.BookingEntitlement, 0, len(s.Live))
	for _, d := range s.Live {
		out = append(out, domain.BookingEntitlement{
			BookingID: bookingID, Type: string(d.Type), Outcome: domain.EntitlementGranted,
			Category: d.Category, Provider: d.Provider, Ref: d.Ref,
		})
	}
	return out
}
