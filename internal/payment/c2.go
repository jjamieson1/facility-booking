package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jjamieson1/facility-booking/internal/c2"
)

// C2Provider bills through C2's payment broker (§4.7).
//
// C2 raises the invoice, notifies the citizen, hosts the checkout on a
// PCI-compliant gateway, and reports the outcome with a signed token. This app
// never sees card data and never builds a payment page — which is the point:
// PCI-DSS stays with the gateway, and the municipality already runs C2.
//
// Two properties shape everything here:
//
//   - Settlement is asynchronous. RaiseBill returns a payUrl, not a receipt.
//     The booking's payment sits pending until the callback (or a poll) says
//     otherwise.
//   - Refunds are not ours to make. The partner API exposes only POST
//     /partner/invoices and GET /partner/invoices/{ref}; refunding lives on C2's
//     admin surface behind WRITE_PAYMENTS. Refund therefore always returns
//     ErrRefundNotSupported, and the caller records what is owed.
type C2Provider struct {
	client      *c2.Client
	callbackURL string
	currency    string
}

// NewC2Provider builds the broker-backed provider. callbackURL is where C2
// pushes signed settlement notices; it must be publicly reachable, and an empty
// one is allowed — C2 then sends nothing and reconciliation falls back to
// polling, which the docs call the source of truth anyway.
func NewC2Provider(client *c2.Client, callbackURL, currency string) *C2Provider {
	if strings.TrimSpace(currency) == "" {
		currency = "CAD"
	}
	return &C2Provider{client: client, callbackURL: callbackURL, currency: currency}
}

// Name identifies the gateway on payments and ledger rows.
func (p *C2Provider) Name() string { return string(KindC2) }

// Charge is unreachable for a hosted gateway: there is no card to take. It is
// implemented only to satisfy Provider, and fails loudly rather than returning a
// zero Charge that a caller might read as success.
func (p *C2Provider) Charge(context.Context, int, string) (Charge, error) {
	return Charge{}, fmt.Errorf("%w: C2 hosts its own checkout, so there is no card to charge", ErrNotConnected)
}

// Refund always refuses. C2's partner API has no refund endpoint — refunds are
// an operator action inside C2 — so claiming to have refunded, or silently
// doing nothing, would both lose a resident's money.
func (p *C2Provider) Refund(context.Context, string, int) (string, error) {
	return "", ErrRefundNotSupported
}

// RaiseBill creates the invoice and returns where the citizen pays.
//
// Idempotent on b.Ref: C2 returns the existing invoice for a repeated
// reference, so a retry after a timeout cannot bill twice.
func (p *C2Provider) RaiseBill(ctx context.Context, b Bill) (Hosted, error) {
	if strings.TrimSpace(b.Subject) == "" {
		return Hosted{}, ErrNoPayerIdentity
	}
	currency := b.Currency
	if currency == "" {
		currency = p.currency
	}
	inv, err := p.client.RaiseInvoice(ctx, c2.InvoiceRequest{
		Subject:     b.Subject,
		Ref:         b.Ref,
		Currency:    currency,
		Description: b.Description,
		CallbackURL: p.callbackURL,
		// One line, because the fee is one thing. C2 computes the total from the
		// lines; sending our own total as well only creates a way to disagree.
		Items: []c2.InvoiceLine{{
			Description: b.Description,
			Amount:      c2.CentsToAmount(b.AmountCents),
		}},
	})
	switch {
	case errors.Is(err, c2.ErrNoConsent):
		return Hosted{}, ErrNoConsent
	case errors.Is(err, c2.ErrUnknownSubject):
		// A subject C2 has never heard of — a guest booker, most likely.
		return Hosted{}, ErrNoPayerIdentity
	case errors.Is(err, c2.ErrNotConfigured):
		return Hosted{}, ErrNotConnected
	case err != nil:
		return Hosted{}, err
	}
	return Hosted{Ref: inv.ID, PayURL: inv.PayURL}, nil
}

// Settled reports whether C2 considers a bill paid, by asking C2 directly.
//
// This is the reconciliation backstop. C2 delivers callbacks best-effort and
// detached, so a missed one must never mean a resident who paid is still shown
// as owing — anything that needs certainty asks here.
func (p *C2Provider) Settled(ctx context.Context, ref string) (paid bool, gatewayTxn string, err error) {
	inv, err := p.client.Invoice(ctx, ref)
	if err != nil {
		return false, "", err
	}
	switch inv.Status {
	case c2.InvoicePaidOnline, c2.InvoicePaidAtCounter:
		return true, inv.GatewayTxnID, nil
	default:
		return false, "", nil
	}
}
