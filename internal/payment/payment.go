// Package payment handles the money side of a booking behind a Provider
// interface. The demo ships a mock (simulated Stripe) so it needs no keys and
// charges nothing; a real Stripe provider implements the same interface later.
package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

var (
	ErrDeclined = errors.New("payment: card declined")
	ErrNotFound = errors.New("payment: booking not found")
	// ErrProviderMismatch guards a refund against a payment taken through a
	// different gateway than the one currently selected.
	ErrProviderMismatch = errors.New("payment: this payment was taken through a different provider")
)

// Charge is the result of a provider authorization. Message is the human-readable
// line the gateway returned (shown on the reconciliation screen); Last4 is the
// masked card tail — never the full PAN.
type Charge struct {
	Ref     string
	Status  domain.PaymentStatus
	Message string
	Last4   string
}

// Provider abstracts a payment gateway. Card is the raw PAN from the (demo)
// checkout form — a real provider would never see this; the mock uses it only to
// branch success vs decline for the demo. A declined Charge is returned alongside
// ErrDeclined so the caller can both reject the booking and log the reason.
type Provider interface {
	Name() string
	Charge(ctx context.Context, amountCents int, card string) (Charge, error)
	// Refund returns amountCents to the payer. Partial refunds are required by
	// the cancellation policy (§4.7), so the amount is explicit rather than
	// implied — a provider that can only refund in full must reject a partial
	// rather than quietly refunding everything.
	Refund(ctx context.Context, ref string, amountCents int) (message string, err error)
}

// ProviderFunc resolves which provider to charge through, per request. The
// module is an admin setting (§4.7), so it can change while the app is running —
// resolving at construction time would pin the process to whatever was selected
// at boot.
type ProviderFunc func(ctx context.Context) Provider

// Fixed returns a resolver that always yields p. Used by tests and by any
// caller that genuinely wants one hardcoded gateway.
func Fixed(p Provider) ProviderFunc {
	return func(context.Context) Provider { return p }
}

// Service records payments against bookings using the configured Provider.
type Service struct {
	db      *gorm.DB
	resolve ProviderFunc
}

// NewService wires the payment service to a provider resolver.
func NewService(db *gorm.DB, resolve ProviderFunc) *Service {
	return &Service{db: db, resolve: resolve}
}

// Pay charges the booking's fee and records a paid Payment. Idempotent-ish: a
// booking already paid is returned as-is.
func (s *Service) Pay(ctx context.Context, bookingID, card string) (*domain.Payment, error) {
	var b domain.Booking
	if err := s.db.WithContext(ctx).First(&b, "id = ?", bookingID).Error; err != nil {
		return nil, ErrNotFound
	}

	var existing domain.Payment
	if err := s.db.WithContext(ctx).First(&existing, "booking_id = ?", bookingID).Error; err == nil && existing.Status == domain.PayPaid {
		return &existing, nil
	}

	provider := s.resolve(ctx)
	charge, err := provider.Charge(ctx, b.FeeCents, card)
	if err != nil {
		// Record the decline so it shows on the reconciliation ledger, then
		// surface the error to reject the booking's payment.
		s.record(ctx, domain.PaymentTransaction{
			BookingID: bookingID, Kind: domain.TxnCharge, Status: domain.TxnFailed,
			AmountCents: b.FeeCents, Provider: provider.Name(), ProviderRef: charge.Ref,
			CardLast4: charge.Last4, Message: charge.Message,
		})
		return nil, err
	}
	pay := domain.Payment{
		BookingID: bookingID, AmountCents: b.FeeCents, Status: charge.Status,
		Provider: provider.Name(), ProviderRef: charge.Ref,
	}
	// One payment per booking (unique index); upsert on conflict.
	if err := s.db.WithContext(ctx).Where("booking_id = ?", bookingID).Assign(pay).FirstOrCreate(&pay).Error; err != nil {
		return nil, err
	}
	s.record(ctx, domain.PaymentTransaction{
		BookingID: bookingID, PaymentID: pay.ID, Kind: domain.TxnCharge, Status: domain.TxnSucceeded,
		AmountCents: b.FeeCents, Provider: provider.Name(), ProviderRef: charge.Ref,
		CardLast4: charge.Last4, Message: charge.Message,
	})
	return &pay, nil
}

// record appends a transaction to the ledger. Best-effort: a ledger write must
// never fail the underlying charge/refund the caller already committed.
func (s *Service) record(ctx context.Context, txn domain.PaymentTransaction) {
	_ = s.db.WithContext(ctx).Create(&txn).Error
}

// Refund returns the whole payment (staff action, and the manual override the
// cancellation policy allows for).
func (s *Service) Refund(ctx context.Context, bookingID string) (*domain.Payment, error) {
	return s.RefundAmount(ctx, bookingID, 0, "")
}

// RefundAmount returns amountCents of a booking's payment. amountCents <= 0 or
// greater than the amount paid means the full amount — callers computing a
// policy refund pass the exact figure they quoted the resident.
//
// reason is recorded on the ledger so a partial refund can be explained months
// later ("50% under Municipal default") rather than looking like an error.
func (s *Service) RefundAmount(ctx context.Context, bookingID string, amountCents int, reason string) (*domain.Payment, error) {
	var pay domain.Payment
	if err := s.db.WithContext(ctx).First(&pay, "booking_id = ?", bookingID).Error; err != nil {
		return nil, ErrNotFound
	}
	if pay.Status != domain.PayPaid {
		return &pay, nil
	}
	// Refund through the provider that took the money. If the module has been
	// switched since, the old charge still lives with the old gateway — refunding
	// through the new one would fail or, worse, refund an unrelated charge.
	provider := s.resolve(ctx)
	if pay.Provider != "" && pay.Provider != provider.Name() {
		return nil, fmt.Errorf("%w: paid via %q, now configured for %q", ErrProviderMismatch, pay.Provider, provider.Name())
	}
	if amountCents <= 0 || amountCents > pay.AmountCents {
		amountCents = pay.AmountCents
	}
	msg, err := provider.Refund(ctx, pay.ProviderRef, amountCents)
	if err != nil {
		return nil, err
	}
	if reason != "" {
		msg = reason + " — " + msg
	}
	// A partial refund leaves the payment paid: money is still held against this
	// booking, and marking it refunded would misreport revenue and hide the
	// remainder from reconciliation.
	if amountCents == pay.AmountCents {
		pay.Status = domain.PayRefunded
		if err := s.db.WithContext(ctx).Model(&pay).Update("status", domain.PayRefunded).Error; err != nil {
			return nil, err
		}
	}
	s.record(ctx, domain.PaymentTransaction{
		BookingID: pay.BookingID, PaymentID: pay.ID, Kind: domain.TxnRefund, Status: domain.TxnSucceeded,
		AmountCents: amountCents, Provider: provider.Name(), ProviderRef: pay.ProviderRef, Message: msg,
	})
	return &pay, nil
}

// normalizeCard strips spaces so "4242 4242 …" and "4242…" compare equally.
func normalizeCard(card string) string {
	return strings.ReplaceAll(card, " ", "")
}
