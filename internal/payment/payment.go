// Package payment handles the money side of a booking behind a Provider
// interface. The demo ships a mock (simulated Stripe) so it needs no keys and
// charges nothing; a real Stripe provider implements the same interface later.
package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

	// A hosted gateway runs its own checkout: there is no card to take here, and
	// no synchronous outcome. Raise the bill, record it pending, and hand back
	// where to pay.
	if hosted, ok := provider.(HostedProvider); ok {
		return s.raiseHosted(ctx, hosted, &b)
	}

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

// BillRef is the invoice reference this app sends a hosted gateway. Derived from
// the booking id so it is stable across retries — that stability is what makes
// the gateway's idempotency work, and a random reference would bill twice.
func BillRef(bookingID string) string { return "FB-" + bookingID }

// raiseHosted bills through a gateway that hosts its own checkout.
//
// The Payment row is written *pending*, not paid: the resident has been asked
// for money, not taken it. Marking it paid here would confirm bookings nobody
// has paid for, and would misreport revenue on the §4.8 report.
func (s *Service) raiseHosted(ctx context.Context, provider HostedProvider, b *domain.Booking) (*domain.Payment, error) {
	subject, err := s.payerSubject(ctx, b.UserID)
	if err != nil {
		return nil, err
	}
	var facilityName string
	// Best-effort: the description is a courtesy on the citizen's invoice, and a
	// missing facility name must not stop the bill going out.
	var f domain.Facility
	if s.db.WithContext(ctx).Select("name").First(&f, "id = ?", b.FacilityID).Error == nil {
		facilityName = f.Name
	}

	out, err := provider.RaiseBill(ctx, Bill{
		Subject:     subject,
		Ref:         BillRef(b.ID),
		AmountCents: b.FeeCents,
		Description: billDescription(facilityName, b.StartsAt),
	})
	if err != nil {
		s.record(ctx, domain.PaymentTransaction{
			BookingID: b.ID, Kind: domain.TxnCharge, Status: domain.TxnFailed,
			AmountCents: b.FeeCents, Provider: provider.Name(),
			Message: "could not raise the bill: " + err.Error(),
		})
		return nil, err
	}

	pay := domain.Payment{
		BookingID: b.ID, AmountCents: b.FeeCents, Status: domain.PayPending,
		Provider: provider.Name(), ProviderRef: out.Ref, PayURL: out.PayURL,
	}
	if err := s.db.WithContext(ctx).Where("booking_id = ?", b.ID).Assign(pay).FirstOrCreate(&pay).Error; err != nil {
		return nil, err
	}
	s.record(ctx, domain.PaymentTransaction{
		BookingID: b.ID, PaymentID: pay.ID, Kind: domain.TxnCharge, Status: domain.TxnPending,
		AmountCents: b.FeeCents, Provider: provider.Name(), ProviderRef: out.Ref,
		Message: "billed; awaiting payment at the gateway",
	})
	return &pay, nil
}

// payerSubject resolves who the gateway should bill. Guests carry a local
// `guest:` subject that no external gateway knows, so they are rejected here
// rather than being sent to the gateway to fail — the booking stands, but the
// money has to be taken another way.
func (s *Service) payerSubject(ctx context.Context, userID string) (string, error) {
	var u domain.User
	if err := s.db.WithContext(ctx).Select("subject").First(&u, "id = ?", userID).Error; err != nil {
		return "", ErrNotFound
	}
	if u.Subject == "" || strings.HasPrefix(u.Subject, "guest:") {
		return "", ErrNoPayerIdentity
	}
	return u.Subject, nil
}

// billDescription is what the citizen sees on their invoice in C2. It has to
// stand alone: they may read it days later, in a system that knows nothing about
// this app.
func billDescription(facility string, starts time.Time) string {
	if facility == "" {
		facility = "Facility booking"
	}
	return fmt.Sprintf("%s — %s", facility, starts.Format("2 Jan 2006, 3:04pm"))
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
	if errors.Is(err, ErrRefundNotSupported) {
		// The gateway will not take instructions from us — C2's refunds are an
		// operator action inside C2. The cancellation has already happened and
		// the slot is already free, so refusing here would strand the resident
		// with neither booking nor money. Record the debt instead, and leave the
		// payment paid: the money genuinely is still held.
		if err := s.recordObligation(ctx, pay, amountCents, reason); err != nil {
			return nil, err
		}
		return &pay, ErrRefundNotSupported
	}
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

// recordObligation books money owed to a resident that this app cannot return
// itself, so an operator can action it at the gateway and the debt can be
// reconciled when they do.
//
// Idempotent per booking: cancelling is not repeatable, but a retried request or
// a double-clicked button must not owe the resident twice.
func (s *Service) recordObligation(ctx context.Context, pay domain.Payment, amountCents int, reason string) error {
	var existing domain.RefundObligation
	err := s.db.WithContext(ctx).
		Where("booking_id = ? AND status = ?", pay.BookingID, domain.RefundOwed).
		First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	ob := domain.RefundObligation{
		BookingID: pay.BookingID, PaymentID: pay.ID, AmountCents: amountCents,
		Reason: reason, Status: domain.RefundOwed,
		Provider: pay.Provider, ProviderRef: pay.ProviderRef,
	}
	if err := s.db.WithContext(ctx).Create(&ob).Error; err != nil {
		return err
	}
	s.record(ctx, domain.PaymentTransaction{
		BookingID: pay.BookingID, PaymentID: pay.ID, Kind: domain.TxnRefund, Status: domain.TxnPending,
		AmountCents: amountCents, Provider: pay.Provider, ProviderRef: pay.ProviderRef,
		Message: "refund owed — issue it at the gateway: " + reason,
	})
	return nil
}

// Obligations lists refunds still owed, oldest first — the staff work queue.
func (s *Service) Obligations(ctx context.Context, status domain.RefundObligationStatus) ([]domain.RefundObligation, error) {
	out := []domain.RefundObligation{}
	q := s.db.WithContext(ctx).Preload("Booking.Facility").Order("created_at asc")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	return out, q.Find(&out).Error
}

// SettleObligation closes a debt once the gateway reports the refund. Called
// from the settlement callback rather than by staff: the app learns the money
// moved, it does not assert it.
func (s *Service) SettleObligation(ctx context.Context, bookingID, gatewayRef string, cents int) error {
	return s.db.WithContext(ctx).Model(&domain.RefundObligation{}).
		Where("booking_id = ? AND status = ?", bookingID, domain.RefundOwed).
		Updates(map[string]any{
			"status":        domain.RefundSettled,
			"settled_ref":   gatewayRef,
			"settled_cents": cents,
		}).Error
}

// normalizeCard strips spaces so "4242 4242 …" and "4242…" compare equally.
func normalizeCard(card string) string {
	return strings.ReplaceAll(card, " ", "")
}
