package booking

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

func staffUser(t *testing.T, db *gorm.DB) domain.User {
	t.Helper()
	u := domain.User{Subject: "staff-" + t.Name(), Email: "staff@rivermont.ca", Role: domain.RoleStaff}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// pendingBooking seeds an approval-required booking in the pending state.
func pendingBooking(t *testing.T, db *gorm.DB, fee int) domain.Booking {
	t.Helper()
	facilityID, userID := seedFacility(t, db, true)
	start, end := window()
	b := domain.Booking{
		FacilityID: facilityID, UserID: userID, Status: domain.StatusPending,
		StartsAt: start, EndsAt: end, FeeCents: fee,
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func reload(t *testing.T, db *gorm.DB, id string) domain.Booking {
	t.Helper()
	var b domain.Booking
	if err := db.Preload("Condition").Preload("Payment").First(&b, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

// The single most important property in this feature: a conditionally-approved
// booking still holds its slot. Releasing it would sell the space out from under
// a resident who is busy satisfying the conditions staff just set.
func TestConditionalBookingStillHoldsItsSlot(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 15000)
	staff := staffUser(t, db)

	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "No amplified music after 9pm"}); err != nil {
		t.Fatal(err)
	}

	got := reload(t, db, b.ID)
	if got.Status != domain.StatusConditional {
		t.Fatalf("status = %q", got.Status)
	}
	if !got.Active() {
		t.Fatal("a conditional booking must block its slot")
	}
	// And in SQL, not just in Go — the locking read in the booking transaction
	// filters on this list, so a status missing from it double-books.
	var n int64
	if err := db.Model(&domain.Booking{}).
		Where("id = ? AND status IN ?", b.ID, domain.ActiveStatuses()).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("conditional is missing from ActiveStatuses, so the lock query cannot see it")
	}
}

// A second request for the same slot must lose to a conditional booking exactly
// as it would to a confirmed one.
func TestConditionalBookingBlocksAnotherRequest(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 0)
	staff := staffUser(t, db)
	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "Security guard required"}); err != nil {
		t.Fatal(err)
	}

	other := domain.User{Subject: "other-" + t.Name(), Email: "other@x", Role: domain.RoleResident}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	start, end := window()
	_, err := svc.Request(context.Background(), other.ID, b.FacilityID, start, end, "", 1, Pricing{})
	if !errors.Is(err, ErrNotBookable) {
		t.Fatalf("got %v, want ErrNotBookable — the slot is held", err)
	}
}

// Staff meaning "approve" should approve. A condition set imposing nothing would
// park the booking short of confirmed while telling the resident nothing to do.
func TestEmptyConditionSetIsRejected(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 15000)
	staff := staffUser(t, db)

	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "   "}); !errors.Is(err, ErrEmptyCondition) {
		t.Fatalf("got %v, want ErrEmptyCondition", err)
	}
	if got := reload(t, db, b.ID); got.Status != domain.StatusPending {
		t.Fatalf("a rejected condition set changed the status to %q", got.Status)
	}
}

func TestAdditionalFeeIsAddedToTheBooking(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 15000)
	staff := staffUser(t, db)

	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "Security guard", AdditionalFeeCents: 5000}); err != nil {
		t.Fatal(err)
	}

	got := reload(t, db, b.ID)
	// Folded into the booking fee so every pricing, payment and reporting path
	// keeps working without learning that conditions exist.
	if got.FeeCents != 20000 {
		t.Fatalf("fee = %d, want 20000", got.FeeCents)
	}
	if got.Condition.AdditionalFeeCents != 5000 {
		t.Fatalf("condition fee = %d", got.Condition.AdditionalFeeCents)
	}
}

// Silently repricing a paid booking would leave the resident owing money nobody
// asked them for, and the gate would then hold their booking hostage to it.
func TestAddingAFeeToAPaidBookingIsRefused(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 15000)
	staff := staffUser(t, db)
	if err := db.Create(&domain.Payment{
		BookingID: b.ID, AmountCents: 15000, Status: domain.PayPaid,
	}).Error; err != nil {
		t.Fatal(err)
	}

	_, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{AdditionalFeeCents: 5000})
	if !errors.Is(err, ErrFeeAlreadyPaid) {
		t.Fatalf("got %v, want ErrFeeAlreadyPaid", err)
	}
	if got := reload(t, db, b.ID); got.FeeCents != 15000 {
		t.Fatalf("fee changed to %d despite the refusal", got.FeeCents)
	}
}

// Terms-only conditions still block confirmation until agreed.
func TestOutstandingListsEachUnmetCondition(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 15000)
	staff := staffUser(t, db)
	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID, ConditionInput{
		Terms: "No amplified music after 9pm", AdditionalFeeCents: 5000, DocumentLabel: "Proof of insurance",
	}); err != nil {
		t.Fatal(err)
	}

	got := reload(t, db, b.ID)
	out := WhatIsOutstanding(got, nil, false)
	if !out.AcceptTerms {
		t.Error("terms not reported as outstanding")
	}
	if out.PayCents != 20000 {
		t.Errorf("owed = %d, want the full 20000", out.PayCents)
	}
	if out.UploadLabel != "Proof of insurance" {
		t.Errorf("upload = %q", out.UploadLabel)
	}
	if out.AllSatisfied {
		t.Error("nothing has been done yet")
	}
}

// Accepting terms is not paying, and paying is not accepting. Each condition
// must be satisfied on its own.
func TestConfirmationRequiresEveryCondition(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 10000)
	staff := staffUser(t, db)
	var owner domain.User
	if err := db.First(&owner, "id = ?", b.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID, ConditionInput{
		Terms: "No amplified music", DocumentLabel: "Proof of insurance",
	}); err != nil {
		t.Fatal(err)
	}

	// Terms accepted, fee unpaid, no document: still conditional.
	if _, err := svc.AcceptConditions(context.Background(), &owner, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := svc.ConfirmIfSatisfied(context.Background(), b.ID, false); err != nil || ok {
		t.Fatalf("confirmed with a document and a fee still outstanding (ok=%v, err=%v)", ok, err)
	}

	// Fee paid, still no document.
	if err := db.Create(&domain.Payment{BookingID: b.ID, AmountCents: 10000, Status: domain.PayPaid}).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := svc.ConfirmIfSatisfied(context.Background(), b.ID, false); ok {
		t.Fatal("confirmed with the document still outstanding")
	}

	// Document arrives: now it confirms.
	got, ok, err := svc.ConfirmIfSatisfied(context.Background(), b.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Status != domain.StatusConfirmed {
		t.Fatalf("did not confirm once everything was satisfied: ok=%v status=%q", ok, got.Status)
	}
}

// The acceptance is the resident's agreement; one recorded by the staff member
// who imposed the condition is worth nothing.
func TestOnlyTheBookerCanAcceptConditions(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 0)
	staff := staffUser(t, db)
	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "No amplified music"}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AcceptConditions(context.Background(), &staff, b.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("staff accepted on the resident's behalf: %v", err)
	}
}

// Agreeing twice is agreeing once — a double-clicked button must not look like
// an error to the resident.
func TestAcceptingConditionsIsIdempotent(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 0)
	staff := staffUser(t, db)
	var owner domain.User
	if err := db.First(&owner, "id = ?", b.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "No amplified music"}); err != nil {
		t.Fatal(err)
	}

	var first time.Time
	for i := 0; i < 3; i++ {
		if _, err := svc.AcceptConditions(context.Background(), &owner, b.ID); err != nil {
			t.Fatal(err)
		}
		got := reload(t, db, b.ID)
		if i == 0 {
			first = *got.Condition.AcceptedAt
		} else if !got.Condition.AcceptedAt.Equal(first) {
			t.Fatal("re-accepting moved the acceptance timestamp")
		}
	}
}

// §4.8 requires the condition set be attributable — "who decided this" is the
// first question a resident disputing a condition asks.
func TestConditionalApprovalIsAudited(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 0)
	staff := staffUser(t, db)
	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "No amplified music"}); err != nil {
		t.Fatal(err)
	}

	var log domain.AuditLog
	if err := db.Where("target_id = ? AND action = ?", b.ID, "booking.approve.conditional").
		First(&log).Error; err != nil {
		t.Fatal(err)
	}
	if log.ActorID != staff.ID {
		t.Fatalf("actor = %q, want the staff member who imposed them", log.ActorID)
	}
	got := reload(t, db, b.ID)
	if got.Condition.SetByID != staff.ID {
		t.Fatalf("condition setBy = %q", got.Condition.SetByID)
	}
}

// Staff need to see which conditional bookings are still waiting on someone.
func TestAwaitingResidentListsConditionalBookings(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	waiting := pendingBooking(t, db, 0)
	staff := staffUser(t, db)
	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, waiting.ID,
		ConditionInput{Terms: "No amplified music"}); err != nil {
		t.Fatal(err)
	}
	// A plain pending booking is not waiting on the resident.
	other := domain.User{Subject: "other-" + t.Name(), Email: "other@x", Role: domain.RoleResident}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	start, end := window()
	if err := db.Create(&domain.Booking{
		FacilityID: waiting.FacilityID, UserID: other.ID, Status: domain.StatusPending,
		StartsAt: start.Add(48 * time.Hour), EndsAt: end.Add(48 * time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}

	list, err := svc.AwaitingResident(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != waiting.ID {
		t.Fatalf("got %d bookings, want just the conditional one", len(list))
	}
	if list[0].Condition == nil {
		t.Fatal("staff cannot act on this without seeing the conditions")
	}
}

// Re-imposing conditions replaces the set rather than accumulating rows, so
// there is never a question of which set governs.
func TestReapprovingReplacesTheConditionSet(t *testing.T) {
	db := newDB(t)
	svc := NewService(db, nil)
	b := pendingBooking(t, db, 10000)
	staff := staffUser(t, db)

	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "First take"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveWithConditions(context.Background(), staff.ID, b.ID,
		ConditionInput{Terms: "Corrected terms"}); err != nil {
		t.Fatal(err)
	}

	var n int64
	db.Model(&domain.BookingCondition{}).Where("booking_id = ?", b.ID).Count(&n)
	if n != 1 {
		t.Fatalf("%d condition rows, want 1", n)
	}
	if got := reload(t, db, b.ID); got.Condition.Terms != "Corrected terms" {
		t.Fatalf("terms = %q", got.Condition.Terms)
	}
}
