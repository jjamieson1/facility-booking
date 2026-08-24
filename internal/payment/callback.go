package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// ErrUnknownBill means the settlement names a reference this app never raised.
// Treated as an error rather than ignored: it is either a bug in the reference
// scheme or a token meant for someone else, and both deserve to be visible.
var ErrUnknownBill = errors.New("payment: settlement names an unknown bill")

// Settlement is a verified statement from a hosted gateway that money moved.
//
// It is deliberately not the raw token: verifying the signature, issuer,
// audience and expiry is the transport layer's job, and this package should not
// be able to apply an unverified one by accident.
type Settlement struct {
	// Ref is our own bill reference (BillRef), the correlation key.
	Ref string
	// Refund distinguishes money out from money in. A partial refund arrives as
	// a refund event while the invoice itself is still paid, so the event kind —
	// not the invoice status — decides what to apply.
	Refund bool
	// AmountCents is the amount of *this* event, not the invoice total.
	AmountCents int
	// GatewayRef is the gateway's id for this event (the charge, or the refund).
	GatewayRef string
	// FullyRefunded is the gateway saying nothing is left owing on the invoice.
	FullyRefunded bool
}

// BookingIDFromRef reverses BillRef. Returns false for a reference this app did
// not mint, so a token addressed to some other application cannot reach into
// our bookings.
func BookingIDFromRef(ref string) (string, bool) {
	id := strings.TrimPrefix(ref, "FB-")
	if id == ref || id == "" {
		return "", false
	}
	return id, true
}

// ApplySettlement records a verified settlement against its booking.
//
// Idempotent on the gateway's own reference: callbacks may be re-delivered, and
// C2's docs are explicit that delivery is best-effort, so this runs more than
// once for the same event as a matter of course. Applying a payment twice would
// double-count revenue on the §4.8 report; applying a refund twice would close
// an obligation that is still owed.
func (s *Service) ApplySettlement(ctx context.Context, st Settlement) (*domain.Payment, error) {
	bookingID, ok := BookingIDFromRef(st.Ref)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBill, st.Ref)
	}
	var pay domain.Payment
	if err := s.db.WithContext(ctx).First(&pay, "booking_id = ?", bookingID).Error; err != nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownBill, st.Ref)
	}
	if s.alreadyApplied(ctx, bookingID, st) {
		return &pay, nil
	}
	if st.Refund {
		return &pay, s.applyRefund(ctx, pay, st)
	}
	return &pay, s.applyPayment(ctx, pay, st)
}

// alreadyApplied reports whether this exact gateway event is already on the
// ledger. Keyed on the gateway's reference rather than on the payment's status,
// because a partial refund leaves the status unchanged and would otherwise look
// unapplied every time.
func (s *Service) alreadyApplied(ctx context.Context, bookingID string, st Settlement) bool {
	if st.GatewayRef == "" {
		return false // nothing to key on; better to re-apply than to drop it
	}
	kind := domain.TxnCharge
	if st.Refund {
		kind = domain.TxnRefund
	}
	var n int64
	s.db.WithContext(ctx).Model(&domain.PaymentTransaction{}).
		Where("booking_id = ? AND kind = ? AND provider_ref = ? AND status = ?",
			bookingID, kind, st.GatewayRef, domain.TxnSucceeded).
		Count(&n)
	return n > 0
}

// applyPayment marks a pending bill paid.
func (s *Service) applyPayment(ctx context.Context, pay domain.Payment, st Settlement) error {
	if err := s.db.WithContext(ctx).Model(&pay).
		Updates(map[string]any{"status": domain.PayPaid}).Error; err != nil {
		return err
	}
	s.record(ctx, domain.PaymentTransaction{
		BookingID: pay.BookingID, PaymentID: pay.ID, Kind: domain.TxnCharge, Status: domain.TxnSucceeded,
		AmountCents: st.AmountCents, Provider: pay.Provider, ProviderRef: st.GatewayRef,
		Message: "paid at the gateway",
	})
	return nil
}

// applyRefund records money returned and closes any obligation it settles.
//
// A partial refund leaves the payment paid: money is still held against the
// booking, and marking it refunded would misreport revenue and hide the
// remainder from reconciliation. That rule already governs the in-app refund
// path; it must not diverge here just because the instruction came from outside.
func (s *Service) applyRefund(ctx context.Context, pay domain.Payment, st Settlement) error {
	if st.FullyRefunded {
		if err := s.db.WithContext(ctx).Model(&pay).
			Updates(map[string]any{"status": domain.PayRefunded}).Error; err != nil {
			return err
		}
	}
	s.record(ctx, domain.PaymentTransaction{
		BookingID: pay.BookingID, PaymentID: pay.ID, Kind: domain.TxnRefund, Status: domain.TxnSucceeded,
		AmountCents: st.AmountCents, Provider: pay.Provider, ProviderRef: st.GatewayRef,
		Message: "refunded at the gateway",
	})
	// Close the debt this refund answers, if there was one. Best-effort by
	// design: a refund an operator issued without a matching obligation is still
	// a real refund, and must be recorded either way.
	if err := s.SettleObligation(ctx, pay.BookingID, st.GatewayRef, st.AmountCents); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}
