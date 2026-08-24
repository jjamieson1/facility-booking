package c2

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Amounts cross the wire as decimal strings while this app holds cents. Every
// crossing is integer arithmetic — a float round-trip is how a cent goes missing
// on a municipal invoice.
func TestMoneyRoundTrip(t *testing.T) {
	for _, cents := range []int{0, 1, 9, 10, 99, 100, 1575, 15000, 22500, 999999} {
		s := CentsToAmount(cents)
		back, err := AmountToCents(s)
		if err != nil {
			t.Fatalf("%d → %q: %v", cents, s, err)
		}
		if back != cents {
			t.Fatalf("%d → %q → %d", cents, s, back)
		}
	}
	if got := CentsToAmount(1575); got != "15.75" {
		t.Fatalf("got %q", got)
	}
	if got := CentsToAmount(5); got != "0.05" {
		t.Fatalf("got %q", got)
	}
	if got := CentsToAmount(15000); got != "150.00" {
		t.Fatalf("got %q", got)
	}
}

func TestAmountToCentsAcceptsShortFractions(t *testing.T) {
	for in, want := range map[string]int{"15": 1500, "15.7": 1570, "15.75": 1575, " 8.00 ": 800} {
		got, err := AmountToCents(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q → %d, want %d", in, got, want)
		}
	}
}

// Truncating a third decimal place would lose money silently, so it is an error
// rather than a rounding decision this layer gets to make.
func TestAmountToCentsRejectsExtraPrecision(t *testing.T) {
	for _, in := range []string{"15.755", "abc", "", "1.2.3"} {
		if _, err := AmountToCents(in); err == nil {
			t.Fatalf("%q should be rejected", in)
		}
	}
}

func TestRaiseInvoiceSendsSubjectAndNoPersonalData(t *testing.T) {
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/partner/invoices" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "client-1" || p != "s3cret" {
			t.Errorf("missing or wrong client credentials")
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(Invoice{
			ID: "9c2a", Ref: "FB-1", Status: InvoicePending,
			PayURL: "https://portal/pay/9c2a", Total: Money{Amount: "150.00", Currency: "CAD"},
		})
	})

	inv, err := c.RaiseInvoice(context.Background(), InvoiceRequest{
		Subject: "sub-123", Ref: "FB-1", Currency: "CAD", Description: "Hall booking",
		Items: []InvoiceLine{{Description: "Booking fee", Amount: CentsToAmount(15000)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.PayURL == "" || inv.Status != InvoicePending {
		t.Fatalf("got %+v", inv)
	}
	if body["user_id"] != "sub-123" {
		t.Fatalf("subject not sent as user_id: %v", body)
	}
	// C2 resolves the person from the subject; sending an email or name here
	// would leak personal data into a system that did not ask for it.
	raw, _ := json.Marshal(body)
	for _, forbidden := range []string{"@", "email", "name", "phone"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Fatalf("request body carries personal data (%q): %s", forbidden, raw)
		}
	}
}

// A citizen who has not accepted this service's terms gets a 403. That is an
// expected outcome, not a failure — and C2 audits a retry against our client id.
func TestRaiseInvoiceMapsConsentRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := c.RaiseInvoice(context.Background(), InvoiceRequest{Subject: "s"}); err != ErrNoConsent {
		t.Fatalf("got %v, want ErrNoConsent", err)
	}
}

// A total that disagrees with the lines is our bug, and retrying the same body
// cannot fix it — so it must not look like a transient failure.
func TestRaiseInvoiceMapsRejection(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	if _, err := c.RaiseInvoice(context.Background(), InvoiceRequest{Subject: "s"}); err != ErrInvoiceRejected {
		t.Fatalf("got %v, want ErrInvoiceRejected", err)
	}
}

func TestInvoiceLookupByOurOwnRef(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/partner/invoices/FB-1" {
			t.Errorf("got path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Invoice{
			ID: "9c2a", Ref: "FB-1", Status: InvoicePaidOnline,
			Total: Money{Amount: "150.00", Currency: "CAD"}, GatewayTxnID: "ch_3P",
		})
	})

	inv, err := c.Invoice(context.Background(), "FB-1")
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status != InvoicePaidOnline || inv.GatewayTxnID != "ch_3P" {
		t.Fatalf("got %+v", inv)
	}
}

// With no origin configured the whole client is inert, which is what lets the
// app run without C2 at all.
func TestInvoiceCallsRequireConfiguration(t *testing.T) {
	c := New(Config{})
	if _, err := c.RaiseInvoice(context.Background(), InvoiceRequest{}); err != ErrNotConfigured {
		t.Fatalf("got %v", err)
	}
	if _, err := c.Invoice(context.Background(), "x"); err != ErrNotConfigured {
		t.Fatalf("got %v", err)
	}
}
