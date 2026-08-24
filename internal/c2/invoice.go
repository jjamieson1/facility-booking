package c2

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ErrInvoiceRejected means C2 refused the invoice on its own terms — most often
// a total that does not equal the sum of its lines. It is a bug in the caller,
// not a transient failure, so retrying the same body will not help.
var ErrInvoiceRejected = errors.New("c2: invoice rejected")

// Invoice statuses, as C2 reports them.
const (
	InvoicePending       = "PENDING"
	InvoicePaidOnline    = "PAID_ONLINE"
	InvoicePaidAtCounter = "PAID_AT_LOCATION"
	InvoiceCancelled     = "CANCELLED"
	InvoiceRefunded      = "REFUNDED"
)

// Money is C2's amount wire format. Amounts cross the wire as decimal strings
// ("15.75"), never floats or minor units — this app holds cents, so every
// crossing goes through CentsToAmount/AmountToCents rather than arithmetic on
// the string.
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// InvoiceLine is one billable line. C2 sums the lines itself.
type InvoiceLine struct {
	Description string `json:"description"`
	Amount      string `json:"amount"`
}

// InvoiceRequest raises a bill against a citizen.
type InvoiceRequest struct {
	// Subject is the citizen's OIDC `sub`. As with notifications, we never send
	// an email or a name — C2 resolves the person.
	Subject string `json:"user_id"`
	// Ref is *our* invoice id. C2 is idempotent on it: repeating a Ref returns
	// the existing invoice unchanged, which is what makes a retry safe.
	Ref         string        `json:"invoice_id"`
	Currency    string        `json:"currency"`
	Description string        `json:"description,omitempty"`
	CallbackURL string        `json:"callback_url,omitempty"`
	Items       []InvoiceLine `json:"items"`
}

// Invoice is C2's view of a bill.
type Invoice struct {
	ID           string `json:"invoiceId"`
	Ref          string `json:"clientInvoiceRef"`
	Status       string `json:"status"`
	PayURL       string `json:"payUrl"`
	Total        Money  `json:"total"`
	GatewayTxnID string `json:"gatewayTxnId,omitempty"`
	PaidAt       string `json:"paidAt,omitempty"`
}

// RaiseInvoice bills a citizen and returns the invoice, including the payUrl the
// citizen pays at. C2 notifies them with that link itself.
//
// Idempotent on req.Ref: raising the same reference twice returns the first
// invoice rather than billing twice, so a retry after a timeout is safe.
func (c *Client) RaiseInvoice(ctx context.Context, req InvoiceRequest) (*Invoice, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	var out Invoice
	if err := c.post(ctx, "/partner/invoices", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Invoice fetches an invoice by C2's id or our own Ref.
//
// This is the reconciliation path, and the source of truth. Callbacks are
// best-effort and detached on C2's side, so a missed one must never mean a
// payment goes unnoticed — anything that needs certainty polls here.
func (c *Client) Invoice(ctx context.Context, ref string) (*Invoice, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	var out Invoice
	if err := c.get(ctx, "/partner/invoices/"+url.PathEscape(ref), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// get issues an authenticated JSON GET, sharing post's status-code mapping.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
		[]byte(c.clientID+":"+c.secret)))

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("c2: %s: %w", path, err)
	}
	defer res.Body.Close()
	return decodeStatus(path, res, out)
}

// CentsToAmount renders minor units as C2's decimal string. Done with integer
// arithmetic and string formatting rather than floats: 1575 must become exactly
// "15.75", and a float round-trip is how a cent goes missing.
func CentsToAmount(cents int) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// AmountToCents parses C2's decimal string back to minor units. It accepts a
// missing or short fractional part ("15", "15.7") because C2 is the one
// formatting these and we would rather read a valid amount than reject it;
// anything with more than two decimal places is an error, since silently
// truncating it would lose money.
func AmountToCents(amount string) (int, error) {
	s := strings.TrimSpace(amount)
	if s == "" {
		return 0, fmt.Errorf("c2: empty amount")
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		return 0, fmt.Errorf("c2: amount %q has more than two decimal places", amount)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.Atoi(whole)
	if err != nil {
		return 0, fmt.Errorf("c2: bad amount %q", amount)
	}
	f, err := strconv.Atoi(frac)
	if err != nil {
		return 0, fmt.Errorf("c2: bad amount %q", amount)
	}
	cents := w*100 + f
	if neg {
		cents = -cents
	}
	return cents, nil
}
