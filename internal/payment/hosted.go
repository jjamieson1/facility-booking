package payment

import (
	"context"
	"errors"
)

var (
	// ErrNoPayerIdentity means the booker has no identity at the hosted gateway,
	// so no bill can be raised against them. Guests are the case that matters
	// (FAC-24 gives them `guest:<uuid>` subjects, which C2 has never heard of);
	// the booking is still valid, but the money has to be taken another way.
	ErrNoPayerIdentity = errors.New("payment: payer has no account with the gateway")
	// ErrNoConsent means the payer has not accepted this service's terms at the
	// gateway, so it refuses to bill them. Expected, not a failure — and not
	// worth retrying until they consent.
	ErrNoConsent = errors.New("payment: payer has not consented to this service")
	// ErrRefundNotSupported is returned by Provider.Refund on a gateway that
	// cannot be told to refund. The caller must record the debt rather than
	// treat it as done — declared as an error on the existing method rather than
	// as a separate interface, so every caller has to handle it.
	ErrRefundNotSupported = errors.New("payment: this gateway cannot be asked to refund")
)

// Bill is a request to a hosted gateway to collect money from a payer.
type Bill struct {
	// Subject identifies the payer at the gateway — for C2, the citizen's OIDC
	// `sub`. Never a name or an email: the gateway resolves the person.
	Subject string
	// Ref is *our* reference for this bill, and the idempotency key. Raising the
	// same Ref twice must return the first bill rather than charging twice.
	Ref         string
	AmountCents int
	Currency    string
	Description string
}

// Hosted is what a hosted gateway returns: somewhere to send the payer, and the
// gateway's own reference for reconciling later.
type Hosted struct {
	Ref    string
	PayURL string
}

// HostedProvider is a gateway that hosts its own checkout. The app raises a bill
// and hands the payer a URL; settlement arrives later, out of band, and the app
// learns about it from a signed callback (or by polling).
//
// This is a different shape from Provider.Charge, not an extension of it: there
// is no card to take and no synchronous outcome to report. A provider
// implementing this interface makes Charge unreachable — Service.Pay branches on
// the interface, never on the module name.
type HostedProvider interface {
	Provider
	RaiseBill(ctx context.Context, b Bill) (Hosted, error)
}
