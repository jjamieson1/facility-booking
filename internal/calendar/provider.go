package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Kind identifies a calendar integration module. The municipality picks one in
// the staff back-office; the app constructs the matching Provider from it.
type Kind string

const (
	KindNone      Kind = "none"      // no city calendar at all
	KindICS       Kind = "ics"       // one-way: .ics invite + public feed (the default)
	KindGoogle    Kind = "google"    // Google Workspace (two-way)
	KindMicrosoft Kind = "microsoft" // Microsoft 365 / Outlook (two-way)
)

var (
	// ErrNotSupported is returned by a one-way module asked to read busy times.
	// Callers treat it as "no external blocks", not as a failure.
	ErrNotSupported = errors.New("calendar: module does not support this direction")
	// ErrNotConnected is returned by a two-way module that has been selected but
	// whose integration is not built or credentialed yet.
	ErrNotConnected = errors.New("calendar: module is not connected yet")
	// ErrUnknownKind is returned when a caller names a module that isn't registered.
	ErrUnknownKind = errors.New("calendar: unknown module")
)

// Busy is a window blocked directly on the city calendar, pulled back so the
// app can show it as unavailable (§4.6).
type Busy struct {
	FacilityID string    `json:"facilityId"`
	StartsAt   time.Time `json:"startsAt"`
	EndsAt     time.Time `json:"endsAt"`
	Summary    string    `json:"summary"`
}

// Provider is one calendar module. Every module implements the whole interface;
// one-way modules return ErrNotSupported from BusyWindows rather than pretending
// to have read the city calendar.
type Provider interface {
	Kind() Kind
	// Publish creates or updates the city-calendar event for a confirmed booking.
	Publish(ctx context.Context, b domain.Booking) error
	// Withdraw removes it when the booking is cancelled or denied.
	Withdraw(ctx context.Context, b domain.Booking) error
	// BusyWindows reads back externally-created blocks for a facility.
	BusyWindows(ctx context.Context, facilityID string, from, to time.Time) ([]Busy, error)
}

// Field describes one configuration input the admin form renders for a module.
// Secrets are deliberately absent: credentials come from the environment (see
// Module.SecretEnv), never from a form post into the database.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required"`
}

// Module is the admin-facing description of a calendar type: what it is, what
// it can do, what it needs configured, and whether it is connectable today.
type Module struct {
	Kind      Kind    `json:"kind"`
	Name      string  `json:"name"`
	Summary   string  `json:"summary"`
	TwoWay    bool    `json:"twoWay"`
	Available bool    `json:"available"` // false = selectable as intent, not yet functional
	SecretEnv string  `json:"secretEnv,omitempty"`
	Fields    []Field `json:"fields"`
}

// modules is the registry, in the order the admin form presents them.
var modules = []Module{
	{
		Kind:      KindICS,
		Name:      "iCal feed + .ics invites",
		Summary:   "One-way: confirmed bookings publish to a subscribable city feed and the booker gets an .ics invite. Blocks made directly on the city calendar are not seen by the app.",
		TwoWay:    false,
		Available: true,
	},
	{
		Kind:      KindGoogle,
		Name:      "Google Workspace",
		Summary:   "Two-way with Google Calendar: bookings push to the city calendar and externally-blocked events come back as unavailable.",
		TwoWay:    true,
		Available: false,
		SecretEnv: "FB_CALENDAR_GOOGLE_CREDENTIALS",
		Fields: []Field{
			{Key: "calendarId", Label: "Calendar ID", Placeholder: "spaces@rivermont.ca", Required: true},
			{Key: "timeZone", Label: "Time zone", Placeholder: "America/Toronto", Required: false},
		},
	},
	{
		Kind:      KindMicrosoft,
		Name:      "Microsoft 365 / Outlook",
		Summary:   "Two-way with Outlook via Microsoft Graph: bookings push to the city calendar and externally-blocked events come back as unavailable.",
		TwoWay:    true,
		Available: false,
		SecretEnv: "FB_CALENDAR_MS_CLIENT_SECRET",
		Fields: []Field{
			{Key: "tenantId", Label: "Tenant ID", Placeholder: "00000000-0000-0000-0000-000000000000", Required: true},
			{Key: "calendarId", Label: "Calendar or group ID", Placeholder: "spaces@rivermont.ca", Required: true},
			{Key: "timeZone", Label: "Time zone", Placeholder: "America/Toronto", Required: false},
		},
	},
	{
		Kind:      KindNone,
		Name:      "No city calendar",
		Summary:   "Bookings are tracked only in this app. The booker still gets an .ics invite; nothing is published for the city.",
		TwoWay:    false,
		Available: true,
	},
}

// Modules returns the registered calendar types for the admin form.
func Modules() []Module {
	out := make([]Module, len(modules))
	for i, m := range modules {
		out[i] = withFields(m)
	}
	return out
}

// ModuleFor looks up one module's description.
func ModuleFor(k Kind) (Module, bool) {
	for _, m := range modules {
		if m.Kind == k {
			return withFields(m), true
		}
	}
	return Module{}, false
}

// withFields guarantees Fields is non-nil.
//
// A nil slice marshals to `null`, and the admin form does `module.fields.length`
// — which throws on null and takes the whole page down, not just the field list.
// The modules with no configuration (ics, none) are exactly the common ones, so
// this is the default path rather than an edge case.
func withFields(m Module) Module {
	if m.Fields == nil {
		m.Fields = []Field{}
	}
	return m
}

// New constructs the Provider for a kind. Config holds the module's non-secret
// settings as entered in the admin form.
func New(k Kind, config map[string]string) (Provider, error) {
	switch k {
	case KindICS:
		return icsProvider{}, nil
	case KindNone:
		return noneProvider{}, nil
	case KindGoogle, KindMicrosoft:
		return pendingProvider{kind: k, config: config}, nil
	default:
		return nil, ErrUnknownKind
	}
}

// icsProvider is today's behaviour: the public feed is rendered on demand by
// Feed(), so publishing is a no-op — a confirmed booking appears in the feed
// simply by existing. Nothing can be read back.
type icsProvider struct{}

func (icsProvider) Kind() Kind                                     { return KindICS }
func (icsProvider) Publish(context.Context, domain.Booking) error  { return nil }
func (icsProvider) Withdraw(context.Context, domain.Booking) error { return nil }
func (icsProvider) BusyWindows(context.Context, string, time.Time, time.Time) ([]Busy, error) {
	return nil, ErrNotSupported
}

// noneProvider publishes nothing at all.
type noneProvider struct{}

func (noneProvider) Kind() Kind                                     { return KindNone }
func (noneProvider) Publish(context.Context, domain.Booking) error  { return nil }
func (noneProvider) Withdraw(context.Context, domain.Booking) error { return nil }
func (noneProvider) BusyWindows(context.Context, string, time.Time, time.Time) ([]Busy, error) {
	return nil, ErrNotSupported
}

// pendingProvider stands in for a two-way module the municipality has selected
// but that isn't built yet (FAC-6). It records the choice without pretending to
// sync: every call fails loudly with ErrNotConnected so a half-working
// integration can never silently drop a booking.
type pendingProvider struct {
	kind   Kind
	config map[string]string
}

func (p pendingProvider) Kind() Kind { return p.kind }
func (p pendingProvider) Publish(context.Context, domain.Booking) error {
	return ErrNotConnected
}
func (p pendingProvider) Withdraw(context.Context, domain.Booking) error {
	return ErrNotConnected
}
func (p pendingProvider) BusyWindows(context.Context, string, time.Time, time.Time) ([]Busy, error) {
	return nil, ErrNotConnected
}
