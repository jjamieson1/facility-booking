package payment

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

func paidBooking(t *testing.T, svc *Service, db *gorm.DB, status domain.PaymentStatus) domain.Booking {
	t.Helper()
	b := seedBooking(t, db, 15000)
	if err := db.Create(&domain.Payment{
		BookingID: b.ID, AmountCents: 15000, Status: status,
		Provider: "fake-hosted", ProviderRef: "inv-1",
	}).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func statusOf(t *testing.T, db *gorm.DB, bookingID string) domain.PaymentStatus {
	t.Helper()
	var p domain.Payment
	if err := db.First(&p, "booking_id = ?", bookingID).Error; err != nil {
		t.Fatal(err)
	}
	return p.Status
}

func countTxns(t *testing.T, db *gorm.DB, bookingID string, kind domain.TxnKind, status domain.TxnStatus) int {
	t.Helper()
	var n int64
	if err := db.Model(&domain.PaymentTransaction{}).
		Where("booking_id = ? AND kind = ? AND status = ?", bookingID, kind, status).
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestSettlementMarksPendingPaymentPaid(t *testing.T) {
	p := &fakeHosted{}
	svc, db := hostedService(t, p)
	b := paidBooking(t, svc, db, domain.PayPending)

	if _, err := svc.ApplySettlement(context.Background(), Settlement{
		Ref: BillRef(b.ID), AmountCents: 15000, GatewayRef: "ch_3P",
	}); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, db, b.ID); got != domain.PayPaid {
		t.Fatalf("status = %q, want paid", got)
	}
}

// C2 delivers callbacks best-effort and re-delivers, so this runs twice as a
// matter of course. Applying a payment twice double-counts revenue on the §4.8
// report.
func TestSettlementIsIdempotentOnGatewayReference(t *testing.T) {
	p := &fakeHosted{}
	svc, db := hostedService(t, p)
	b := paidBooking(t, svc, db, domain.PayPending)

	st := Settlement{Ref: BillRef(b.ID), AmountCents: 15000, GatewayRef: "ch_3P"}
	for i := 0; i < 3; i++ {
		if _, err := svc.ApplySettlement(context.Background(), st); err != nil {
			t.Fatal(err)
		}
	}
	if n := countTxns(t, db, b.ID, domain.TxnCharge, domain.TxnSucceeded); n != 1 {
		t.Fatalf("charge recorded %d times, want 1", n)
	}
}

// A partial refund leaves the payment paid: money is still held against the
// booking, and marking it refunded would misreport revenue and hide the
// remainder from reconciliation. The in-app refund path already works this way;
// an instruction arriving from outside must not diverge.
func TestPartialRefundLeavesPaymentPaid(t *testing.T) {
	p := &fakeHosted{}
	svc, db := hostedService(t, p)
	b := paidBooking(t, svc, db, domain.PayPaid)

	if _, err := svc.ApplySettlement(context.Background(), Settlement{
		Ref: BillRef(b.ID), Refund: true, AmountCents: 7500, GatewayRef: "re_1",
		FullyRefunded: false,
	}); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, db, b.ID); got != domain.PayPaid {
		t.Fatalf("status = %q, want paid after a partial refund", got)
	}
	if n := countTxns(t, db, b.ID, domain.TxnRefund, domain.TxnSucceeded); n != 1 {
		t.Fatalf("refund recorded %d times, want 1", n)
	}
}

func TestFullRefundMarksPaymentRefunded(t *testing.T) {
	p := &fakeHosted{}
	svc, db := hostedService(t, p)
	b := paidBooking(t, svc, db, domain.PayPaid)

	if _, err := svc.ApplySettlement(context.Background(), Settlement{
		Ref: BillRef(b.ID), Refund: true, AmountCents: 15000, GatewayRef: "re_1",
		FullyRefunded: true,
	}); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, db, b.ID); got != domain.PayRefunded {
		t.Fatalf("status = %q, want refunded", got)
	}
}

// The obligation and the settlement are two halves of one story: staff cancel,
// an operator refunds in C2, and the debt closes itself.
func TestRefundSettlementClosesTheObligation(t *testing.T) {
	p := &fakeHosted{}
	svc, db := hostedService(t, p)
	b := paidBooking(t, svc, db, domain.PayPaid)

	if _, err := svc.RefundAmount(context.Background(), b.ID, 7500, "policy"); !errors.Is(err, ErrRefundNotSupported) {
		t.Fatal(err)
	}
	if obs, _ := svc.Obligations(context.Background(), domain.RefundOwed); len(obs) != 1 {
		t.Fatalf("expected an open obligation, got %d", len(obs))
	}

	if _, err := svc.ApplySettlement(context.Background(), Settlement{
		Ref: BillRef(b.ID), Refund: true, AmountCents: 7500, GatewayRef: "re_9",
	}); err != nil {
		t.Fatal(err)
	}

	if obs, _ := svc.Obligations(context.Background(), domain.RefundOwed); len(obs) != 0 {
		t.Fatalf("obligation still open after the refund settled: %+v", obs)
	}
	settled, _ := svc.Obligations(context.Background(), domain.RefundSettled)
	if len(settled) != 1 || settled[0].SettledRef != "re_9" || settled[0].SettledCents != 7500 {
		t.Fatalf("got %+v", settled)
	}
}

// A verified token naming a bill we never raised must not reach into an
// unrelated booking.
func TestSettlementRejectsForeignReference(t *testing.T) {
	p := &fakeHosted{}
	svc, db := hostedService(t, p)
	b := paidBooking(t, svc, db, domain.PayPending)

	// The bare booking id is the case the prefix guard exists for: without it,
	// TrimPrefix returns the string unchanged and a token carrying someone's
	// reference in our id format would settle a real booking. Every other case
	// here is caught by the lookup that follows.
	refs := []string{b.ID, "SOMEONE-ELSE-123", "", "FB-", "FB-not-a-real-id"}
	for _, ref := range refs {
		if _, err := svc.ApplySettlement(context.Background(), Settlement{Ref: ref, GatewayRef: "x"}); !errors.Is(err, ErrUnknownBill) {
			t.Fatalf("ref %q: got %v, want ErrUnknownBill", ref, err)
		}
	}
	if got := statusOf(t, db, b.ID); got != domain.PayPending {
		t.Fatalf("a rejected settlement changed the payment: %q", got)
	}
}

func TestBookingIDFromRefRequiresOurPrefix(t *testing.T) {
	if id, ok := BookingIDFromRef("FB-abc"); !ok || id != "abc" {
		t.Fatalf("got %q, %v", id, ok)
	}
	for _, ref := range []string{"abc", "", "FB-", "fb-abc", "XX-abc"} {
		if _, ok := BookingIDFromRef(ref); ok {
			t.Fatalf("%q should not be accepted as one of our references", ref)
		}
	}
}
