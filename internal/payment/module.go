package payment

import (
	"context"
	"errors"
)

// Kind identifies a payment module. The municipality selects one in the staff
// back-office (§4.7, Open-questions.md Round 3) rather than it being fixed at
// build time — the same shape as the calendar integration.
type Kind string

const (
	KindMock    Kind = "mock"   // simulated gateway; the zero-config default
	KindStripe  Kind = "stripe" // Stripe Elements/Checkout
	KindMoneris Kind = "moneris"
)

var (
	// ErrUnknownKind is returned for a module that isn't registered.
	ErrUnknownKind = errors.New("payment: unknown module")
	// ErrUnknownField rejects a config key the module doesn't declare, so a
	// typo'd setting fails loudly instead of being stored and ignored.
	ErrUnknownField = errors.New("payment: unknown configuration field")
	// ErrMissingField reports a required config value left blank.
	ErrMissingField = errors.New("payment: required configuration field is missing")
	// ErrNotConnected is returned by a module that has been selected but whose
	// integration is not built or credentialed yet.
	ErrNotConnected = errors.New("payment: module is not connected yet")
)

// Field is one configuration input the admin form renders. Secrets are
// deliberately absent — API keys come from the environment (Module.SecretEnv),
// never from a form post that would land in the database.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required"`
}

// Module is the admin-facing description of a payment provider.
//
// Capabilities are published, not assumed: SupportsHold decides whether the
// deposit hold in FAC-4 is available at all, and SupportsRefund decides whether
// staff can refund in-app or must do it in the provider's own dashboard. A
// municipality choosing a processor needs to see that before selecting it.
type Module struct {
	Kind           Kind    `json:"kind"`
	Name           string  `json:"name"`
	Summary        string  `json:"summary"`
	Available      bool    `json:"available"` // false = selectable as intent, not yet functional
	SupportsRefund bool    `json:"supportsRefund"`
	SupportsHold   bool    `json:"supportsHold"` // separate authorisation/capture, for deposits
	SecretEnv      string  `json:"secretEnv,omitempty"`
	Fields         []Field `json:"fields"`
}

// modules is the registry, in the order the admin form presents them.
var modules = []Module{
	{
		Kind:           KindMock,
		Name:           "Simulated gateway (demo)",
		Summary:        "A built-in test gateway. No keys, no network, no money moves — test card 4242 4242 4242 4242 succeeds and 4000 0000 0000 0002 declines. Use for demos and training.",
		Available:      true,
		SupportsRefund: true,
		SupportsHold:   false,
	},
	{
		Kind:      KindStripe,
		Name:      "Stripe",
		Summary:   "Card payments via Stripe Elements, so card details never reach this application (PCI SAQ-A). Supports refunds and separate authorisation/capture for deposits.",
		Available: false,
		// Declared now because the admin form must show what a municipality
		// would be choosing; the implementation lands with the Stripe SDK.
		SupportsRefund: true,
		SupportsHold:   true,
		SecretEnv:      "FB_STRIPE_SECRET_KEY",
		Fields: []Field{
			{Key: "publishableKey", Label: "Publishable key", Placeholder: "pk_live_…", Required: true},
			{Key: "statementDescriptor", Label: "Statement descriptor", Placeholder: "CITY OF RIVERMONT", Required: false},
		},
	},
	{
		Kind:      KindMoneris,
		Name:      "Moneris",
		Summary:   "A Canadian processor many municipalities already hold a merchant agreement with. Registered so the choice can be recorded; not implemented.",
		Available: false,
		// Left false deliberately: whether Moneris supports the pre-authorisation
		// that deposits need has not been confirmed, and claiming a capability we
		// have not verified would mislead the person choosing.
		SupportsRefund: false,
		SupportsHold:   false,
		SecretEnv:      "FB_MONERIS_API_TOKEN",
		Fields: []Field{
			{Key: "storeId", Label: "Store ID", Placeholder: "store1", Required: true},
		},
	},
}

// Modules returns the registered payment modules for the admin form.
func Modules() []Module {
	out := make([]Module, len(modules))
	copy(out, modules)
	return out
}

// ModuleFor looks up one module's description.
func ModuleFor(k Kind) (Module, bool) {
	for _, m := range modules {
		if m.Kind == k {
			return m, true
		}
	}
	return Module{}, false
}

// New constructs the Provider for a kind.
func New(k Kind, config map[string]string) (Provider, error) {
	switch k {
	case KindMock:
		return NewMockProvider(), nil
	case KindStripe, KindMoneris:
		return pendingProvider{kind: k}, nil
	default:
		return nil, ErrUnknownKind
	}
}

// pendingProvider stands in for a module the municipality has selected but that
// is not built yet. Every call fails with ErrNotConnected rather than silently
// succeeding: a payment path that quietly no-ops would confirm bookings nobody
// paid for.
type pendingProvider struct{ kind Kind }

func (p pendingProvider) Name() string { return string(p.kind) }
func (p pendingProvider) Charge(context.Context, int, string) (Charge, error) {
	return Charge{}, ErrNotConnected
}
func (p pendingProvider) Refund(context.Context, string, int) (string, error) {
	return "", ErrNotConnected
}
