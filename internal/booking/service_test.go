package booking

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.New(t)
	return db
}

// seedFacility inserts a facility open 08:00–22:00 daily with the given approval
// mode, plus a resident user, and returns their ids.
func seedFacility(t *testing.T, db *gorm.DB, requiresApproval bool) (facilityID, userID string) {
	t.Helper()
	f := domain.Facility{Name: "Hall", Capacity: 50, RequiresApproval: requiresApproval, MinMinutes: 60, MaxMinutes: 240, BufferMinutes: 30}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	for wd := 0; wd < 7; wd++ {
		db.Create(&domain.AvailabilityRule{FacilityID: f.ID, Weekday: wd, OpenMinute: 8 * 60, CloseMinute: 22 * 60})
	}
	u := domain.User{Subject: "s1", Email: "r@x", Role: domain.RoleResident}
	db.Create(&u)
	return f.ID, u.ID
}

// A Wednesday 10:00–11:00 window (within opening hours).
func window() (time.Time, time.Time) {
	start := time.Date(2026, 7, 22, 10, 0, 0, 0, time.Local)
	return start, start.Add(time.Hour)
}

func TestRequestAutoConfirm(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	fid, uid := seedFacility(t, db, false)
	start, end := window()

	b, err := svc.Request(context.Background(), uid, fid, start, end, "meeting", 10, Pricing{})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if b.Status != domain.StatusConfirmed {
		t.Errorf("status = %q, want confirmed (auto-confirm facility)", b.Status)
	}
}

func TestRequestRequiresApproval(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	fid, uid := seedFacility(t, db, true)
	start, end := window()

	b, _ := svc.Request(context.Background(), uid, fid, start, end, "meeting", 10, Pricing{})
	if b.Status != domain.StatusPending {
		t.Errorf("status = %q, want pending (approval facility)", b.Status)
	}
}

// TestNoDoubleBooking is the core correctness requirement: a second request for
// an overlapping slot must fail once the first holds it.
func TestNoDoubleBooking(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	fid, uid := seedFacility(t, db, false)
	start, end := window()

	if _, err := svc.Request(context.Background(), uid, fid, start, end, "first", 10, Pricing{}); err != nil {
		t.Fatalf("first request should succeed: %v", err)
	}
	// Overlapping window (11:00 is within the 30-min buffer of the 10–11 booking).
	if _, err := svc.Request(context.Background(), uid, fid, end, end.Add(time.Hour), "second", 10, Pricing{}); err != ErrNotBookable {
		t.Errorf("overlapping request err = %v, want ErrNotBookable", err)
	}
	// A clearly free later slot still works.
	free := end.Add(2 * time.Hour)
	if _, err := svc.Request(context.Background(), uid, fid, free, free.Add(time.Hour), "third", 10, Pricing{}); err != nil {
		t.Errorf("non-overlapping request should succeed: %v", err)
	}
}

func TestApproveDenyCancelFlow(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	fid, uid := seedFacility(t, db, true)
	start, end := window()
	admin := &domain.User{Base: domain.Base{ID: "admin1"}, Role: domain.RoleAdmin}

	b, _ := svc.Request(context.Background(), uid, fid, start, end, "meeting", 10, Pricing{})

	approved, err := svc.Approve(context.Background(), admin.ID, b.ID)
	if err != nil || approved.Status != domain.StatusConfirmed {
		t.Fatalf("approve: status=%v err=%v", approved.Status, err)
	}
	// Approving again is an invalid state transition.
	if _, err := svc.Approve(context.Background(), admin.ID, b.ID); err != ErrBadState {
		t.Errorf("re-approve err = %v, want ErrBadState", err)
	}
	// Owner can cancel their confirmed booking.
	cancelled, err := svc.Cancel(context.Background(), &domain.User{Base: domain.Base{ID: uid}, Role: domain.RoleResident}, b.ID)
	if err != nil || cancelled.Status != domain.StatusCancelled {
		t.Fatalf("cancel: status=%v err=%v", cancelled.Status, err)
	}

	// An audit row should exist for the approve action.
	var count int64
	db.Model(&domain.AuditLog{}).Where("action = ?", "booking.approve").Count(&count)
	if count != 1 {
		t.Errorf("audit rows = %d, want 1", count)
	}
}

func TestReschedule(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	fid, uid := seedFacility(t, db, false)
	owner := &domain.User{Base: domain.Base{ID: uid}, Role: domain.RoleResident}

	// Book a slot well in the future so it's within the modifiable window.
	start := time.Now().Add(48 * time.Hour).Truncate(time.Hour)
	// Align to opening hours (08:00–22:00): pick 10:00 that day.
	start = time.Date(start.Year(), start.Month(), start.Day(), 10, 0, 0, 0, start.Location())
	b, err := svc.Request(context.Background(), uid, fid, start, start.Add(time.Hour), "orig", 5, Pricing{})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	// Move it two hours later — should succeed and persist.
	newStart := start.Add(2 * time.Hour)
	moved, err := svc.Reschedule(context.Background(), owner, b.ID, newStart, newStart.Add(time.Hour))
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if !moved.StartsAt.Equal(newStart) {
		t.Errorf("start = %v, want %v", moved.StartsAt, newStart)
	}

	// A stranger cannot reschedule it.
	stranger := &domain.User{Base: domain.Base{ID: "other"}, Role: domain.RoleResident}
	if _, err := svc.Reschedule(context.Background(), stranger, b.ID, newStart, newStart.Add(time.Hour)); err != ErrForbidden {
		t.Errorf("stranger reschedule err = %v, want ErrForbidden", err)
	}

	// A past booking is not modifiable.
	past := domain.Booking{FacilityID: fid, UserID: uid, Status: domain.StatusConfirmed,
		StartsAt: time.Now().Add(-2 * time.Hour), EndsAt: time.Now().Add(-time.Hour)}
	db.Create(&past)
	if _, err := svc.Reschedule(context.Background(), owner, past.ID, newStart, newStart.Add(time.Hour)); err != ErrNotModifiable {
		t.Errorf("past reschedule err = %v, want ErrNotModifiable", err)
	}
}

func TestResidentPricing(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)

	// Facility with resident $40 / non-resident $60.
	f := domain.Facility{Name: "Room", RequiresApproval: false, MinMinutes: 60, MaxMinutes: 240,
		FeeCents: 4000, NonResidentFeeCents: 6000}
	db.Create(&f)
	for wd := 0; wd < 7; wd++ {
		db.Create(&domain.AvailabilityRule{FacilityID: f.ID, Weekday: wd, OpenMinute: 8 * 60, CloseMinute: 22 * 60})
	}
	resident := domain.User{Base: domain.Base{ID: "r"}, Subject: "r", Role: domain.RoleResident, IsResident: true}
	nonResident := domain.User{Base: domain.Base{ID: "n"}, Subject: "n", Role: domain.RoleResident, IsResident: false}
	db.Create(&resident)
	db.Create(&nonResident)

	start := time.Date(2026, 7, 22, 10, 0, 0, 0, time.Local)
	rb, _ := svc.Request(context.Background(), resident.ID, f.ID, start, start.Add(time.Hour), "x", 2, Pricing{Resident: true})
	nb, _ := svc.Request(context.Background(), nonResident.ID, f.ID, start.Add(3*time.Hour), start.Add(4*time.Hour), "x", 2, Pricing{Resident: false})

	if rb.FeeCents != 4000 {
		t.Errorf("resident fee = %d, want 4000", rb.FeeCents)
	}
	if nb.FeeCents != 6000 {
		t.Errorf("non-resident fee = %d, want 6000", nb.FeeCents)
	}
}

func TestRequestRecurring(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	fid, uid := seedFacility(t, db, false)

	// Wednesday 10:00–11:00, weekly for 4 weeks.
	start := time.Date(2026, 7, 22, 10, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)

	// Pre-book week 3's slot so it must be skipped as a conflict.
	conflict := start.AddDate(0, 0, 14)
	if _, err := svc.Request(context.Background(), uid, fid, conflict, conflict.Add(time.Hour), "pre", 5, Pricing{}); err != nil {
		t.Fatalf("pre-book: %v", err)
	}

	res, err := svc.RequestRecurring(context.Background(), uid, fid, start, end, 4, "weekly club", 12, Pricing{})
	if err != nil {
		t.Fatalf("recurring: %v", err)
	}
	if len(res.Created) != 3 {
		t.Errorf("created = %d, want 3", len(res.Created))
	}
	if len(res.Skipped) != 1 || !res.Skipped[0].Equal(conflict) {
		t.Errorf("skipped = %v, want [%v]", res.Skipped, conflict)
	}
	// All created occurrences share the recurrence id.
	for _, b := range res.Created {
		if b.RecurrenceID == nil || *b.RecurrenceID != res.RecurrenceID {
			t.Errorf("occurrence %s recurrence id = %v, want %s", b.ID, b.RecurrenceID, res.RecurrenceID)
		}
	}
}

func TestCancelForbiddenForOtherResident(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	fid, uid := seedFacility(t, db, false)
	start, end := window()
	b, _ := svc.Request(context.Background(), uid, fid, start, end, "meeting", 10, Pricing{})

	stranger := &domain.User{Base: domain.Base{ID: "other"}, Role: domain.RoleResident}
	if _, err := svc.Cancel(context.Background(), stranger, b.ID); err != ErrForbidden {
		t.Errorf("stranger cancel err = %v, want ErrForbidden", err)
	}
}
