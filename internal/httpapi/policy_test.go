package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// seedPolicyFixture builds a paid, confirmed booking starting `startsIn` from
// now, under the seeded municipal default (100% ≥7d, 50% ≥48h, nothing inside).
func seedPolicyFixture(t *testing.T, db *gorm.DB, startsIn time.Duration, paid bool) (domain.Booking, domain.User) {
	t.Helper()
	policy := domain.CancellationPolicy{Name: "Municipal default", ModificationCutoffHours: 24}
	if err := db.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}
	for _, tier := range []domain.RefundTier{
		{PolicyID: policy.ID, HoursBefore: 168, RefundPercent: 100},
		{PolicyID: policy.ID, HoursBefore: 48, RefundPercent: 50},
	} {
		if err := db.Create(&tier).Error; err != nil {
			t.Fatal(err)
		}
	}

	fac := domain.Facility{Name: "Hall", Capacity: 50, FeeCents: 10000}
	db.Create(&fac)
	for wd := 0; wd < 7; wd++ {
		db.Create(&domain.AvailabilityRule{FacilityID: fac.ID, Weekday: wd, OpenMinute: 0, CloseMinute: 24 * 60})
	}
	u := domain.User{Subject: "policy-" + t.Name(), Email: "p@example.com", Role: domain.RoleResident}
	db.Create(&u)

	start := time.Now().Add(startsIn)
	b := domain.Booking{
		FacilityID: fac.ID, UserID: u.ID, StartsAt: start, EndsAt: start.Add(time.Hour),
		Status: domain.StatusConfirmed, FeeCents: 10000, Purpose: "test",
	}
	db.Create(&b)
	if paid {
		db.Create(&domain.Payment{BookingID: b.ID, AmountCents: 10000, Status: domain.PayPaid, Provider: "mock", ProviderRef: "mock_x"})
	}
	return b, u
}

// openSession opens a session for an EXISTING user. sessionFor (in
// calendar_settings_test.go) creates a fresh user each call, which is no good
// here — these tests need a session for the person who owns the booking.
func openSession(t *testing.T, authSvc *auth.Service, u domain.User) *http.Cookie {
	t.Helper()
	id, err := authSvc.OpenSession(context.Background(), auth.Login{User: &u, IDToken: "raw.id.token"})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "fb_session", Value: id}
}

// AC: cancelling inside the refundable window issues the policy refund, and the
// ledger reflects it.
func TestCancelWithinWindowRefundsPerPolicy(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, u := seedPolicyFixture(t, db, 30*24*time.Hour, true) // a month out: full refund
	cookie := openSession(t, authSvc, u)

	rr := do(t, h, http.MethodPost, "/api/bookings/"+b.ID+"/cancel", "", cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
	var got struct {
		Status string        `json:"status"`
		Refund *domain.Quote `json:"refund"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != string(domain.StatusCancelled) {
		t.Errorf("status = %s, want cancelled", got.Status)
	}
	if got.Refund == nil || got.Refund.RefundCents != 10000 {
		t.Fatalf("refund = %+v, want 10000 cents", got.Refund)
	}

	// The ledger records the refund, so reconciliation sees it.
	var txns []domain.PaymentTransaction
	db.Where("booking_id = ? AND kind = ?", b.ID, domain.TxnRefund).Find(&txns)
	if len(txns) != 1 || txns[0].AmountCents != 10000 {
		t.Fatalf("ledger = %+v, want one 10000-cent refund", txns)
	}
	var pay domain.Payment
	db.First(&pay, "booking_id = ?", b.ID)
	if pay.Status != domain.PayRefunded {
		t.Errorf("payment status = %s, want refunded", pay.Status)
	}
}

// AC: a partial tier refunds part, and the payment stays "paid" because money is
// still held against the booking.
func TestCancelInPartialTierRefundsHalf(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, u := seedPolicyFixture(t, db, 100*time.Hour, true) // between 48h and 7d
	cookie := openSession(t, authSvc, u)

	rr := do(t, h, http.MethodPost, "/api/bookings/"+b.ID+"/cancel", "", cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
	}
	var got struct {
		Refund *domain.Quote `json:"refund"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Refund == nil || got.Refund.RefundCents != 5000 {
		t.Fatalf("refund = %+v, want 5000 cents", got.Refund)
	}
	var pay domain.Payment
	db.First(&pay, "booking_id = ?", b.ID)
	if pay.Status != domain.PayPaid {
		t.Errorf("payment status = %s, want it to stay paid — half the money is still held", pay.Status)
	}
}

// AC: cancelling outside the window refunds nothing, and says why.
func TestCancelOutsideWindowRefundsNothing(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, u := seedPolicyFixture(t, db, 4*time.Hour, true) // inside the last tier
	cookie := openSession(t, authSvc, u)

	rr := do(t, h, http.MethodPost, "/api/bookings/"+b.ID+"/cancel", "", cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
	}
	var got struct {
		Refund *domain.Quote `json:"refund"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Refund == nil || got.Refund.RefundCents != 0 {
		t.Fatalf("refund = %+v, want nothing", got.Refund)
	}
	if got.Refund.Explanation == "" {
		t.Error("the resident must be told why nothing came back")
	}
	var txns int64
	db.Model(&domain.PaymentTransaction{}).Where("booking_id = ? AND kind = ?", b.ID, domain.TxnRefund).Count(&txns)
	if txns != 0 {
		t.Errorf("ledger has %d refunds, want none", txns)
	}
}

// A booking that was never paid cancels cleanly and refunds nothing — the free
// facility case.
func TestCancelUnpaidBookingRefundsNothing(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, u := seedPolicyFixture(t, db, 30*24*time.Hour, false)
	cookie := openSession(t, authSvc, u)

	rr := do(t, h, http.MethodPost, "/api/bookings/"+b.ID+"/cancel", "", cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
	}
	var got struct {
		Refund *domain.Quote `json:"refund"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Refund == nil || got.Refund.RefundCents != 0 {
		t.Fatalf("refund = %+v, want nothing for an unpaid booking", got.Refund)
	}
}

// The quote endpoint shows the consequence before the resident commits, and
// agrees with what cancelling actually does.
func TestRefundQuoteMatchesTheCancellation(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, u := seedPolicyFixture(t, db, 100*time.Hour, true)
	cookie := openSession(t, authSvc, u)

	rr := do(t, h, http.MethodGet, "/api/bookings/"+b.ID+"/refund-quote", "", cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("quote status = %d (%s)", rr.Code, rr.Body.String())
	}
	var quoted domain.Quote
	if err := json.Unmarshal(rr.Body.Bytes(), &quoted); err != nil {
		t.Fatal(err)
	}

	rr = do(t, h, http.MethodPost, "/api/bookings/"+b.ID+"/cancel", "", cookie)
	var got struct {
		Refund *domain.Quote `json:"refund"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)

	if got.Refund == nil || got.Refund.RefundCents != quoted.RefundCents {
		t.Fatalf("quoted %d but refunded %+v — the figure shown must be the figure issued",
			quoted.RefundCents, got.Refund)
	}
}

// Another resident cannot read the quote for someone else's booking.
func TestRefundQuoteRejectsAnotherResident(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, _ := seedPolicyFixture(t, db, 100*time.Hour, true)
	intruder := domain.User{Subject: "intruder", Email: "x@example.com", Role: domain.RoleResident}
	db.Create(&intruder)

	rr := do(t, h, http.MethodGet, "/api/bookings/"+b.ID+"/refund-quote", "", openSession(t, authSvc, intruder))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

// The terms are public: a resident must be able to read them before booking,
// without an account.
func TestCancellationPolicyIsPublic(t *testing.T) {
	h, _, db := fullTestServer(t)
	b, _ := seedPolicyFixture(t, db, 100*time.Hour, false)

	rr := do(t, h, http.MethodGet, "/api/facilities/"+b.FacilityID+"/cancellation-policy", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an anonymous read", rr.Code)
	}
	var p domain.CancellationPolicy
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Name == "" || len(p.Tiers) == 0 {
		t.Fatalf("policy is not usable by the UI: %+v", p)
	}
}

// AC: past the modification cutoff, a reschedule is refused.
func TestRescheduleRefusedInsideCutoff(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, u := seedPolicyFixture(t, db, 6*time.Hour, true) // inside the 24h cutoff
	cookie := openSession(t, authSvc, u)

	start := time.Now().Add(20 * 24 * time.Hour).Truncate(time.Hour)
	body := `{"start":"` + start.Format(time.RFC3339) + `","end":"` + start.Add(time.Hour).Format(time.RFC3339) + `"}`
	rr := do(t, h, http.MethodPost, "/api/bookings/"+b.ID+"/reschedule", body, cookie)
	if rr.Code == http.StatusOK {
		t.Fatal("a booking inside the modification cutoff must not be reschedulable")
	}
}

// ...and comfortably outside it, the same reschedule is allowed.
func TestRescheduleAllowedOutsideCutoff(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, u := seedPolicyFixture(t, db, 30*24*time.Hour, true)
	cookie := openSession(t, authSvc, u)

	start := time.Now().Add(40 * 24 * time.Hour).Truncate(time.Hour)
	body := `{"start":"` + start.Format(time.RFC3339) + `","end":"` + start.Add(time.Hour).Format(time.RFC3339) + `"}`
	rr := do(t, h, http.MethodPost, "/api/bookings/"+b.ID+"/reschedule", body, cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
}

// Staff may depart from the policy with a partial amount, and it is audited.
func TestStaffPartialRefundOverride(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	b, _ := seedPolicyFixture(t, db, 4*time.Hour, true) // policy would refund nothing
	staff := domain.User{Subject: "staff-override", Email: "s@example.com", Role: domain.RoleStaff}
	db.Create(&staff)

	rr := do(t, h, http.MethodPost, "/api/staff/bookings/"+b.ID+"/refund",
		`{"amountCents":2500,"reason":"Facility flooded"}`, openSession(t, authSvc, staff))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
	}
	var txns []domain.PaymentTransaction
	db.Where("booking_id = ? AND kind = ?", b.ID, domain.TxnRefund).Find(&txns)
	if len(txns) != 1 || txns[0].AmountCents != 2500 {
		t.Fatalf("ledger = %+v, want one 2500-cent refund", txns)
	}
	var logs []domain.AuditLog
	db.Where("action = ?", "booking.refund").Find(&logs)
	if len(logs) == 0 {
		t.Error("a policy override must be audited")
	}
}
