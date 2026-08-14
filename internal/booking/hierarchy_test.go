package booking

import (
	"context"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// seedHall builds a hall with two sub-spaces (a divided hall) and a booker.
// Every space is open 08:00–22:00 daily.
func seedHall(t *testing.T, db *gorm.DB) (hallID, northID, southID, userID string) {
	t.Helper()
	hall := domain.Facility{Name: "Community Hall", Capacity: 200, MinMinutes: 60, MaxMinutes: 240}
	if err := db.Create(&hall).Error; err != nil {
		t.Fatal(err)
	}
	mkChild := func(name string) string {
		f := domain.Facility{Name: name, Capacity: 100, MinMinutes: 60, MaxMinutes: 240, ParentID: &hall.ID}
		if err := db.Create(&f).Error; err != nil {
			t.Fatal(err)
		}
		return f.ID
	}
	north, south := mkChild("Hall — North half"), mkChild("Hall — South half")

	for _, id := range []string{hall.ID, north, south} {
		for wd := 0; wd < 7; wd++ {
			db.Create(&domain.AvailabilityRule{FacilityID: id, Weekday: wd, OpenMinute: 8 * 60, CloseMinute: 22 * 60})
		}
	}
	u := domain.User{Subject: "hier", Email: "h@x", Role: domain.RoleResident}
	db.Create(&u)
	return hall.ID, north, south, u.ID
}

// Booking the whole hall must block its halves: they are the same physical room.
func TestBookingParentBlocksChild(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	hall, north, _, uid := seedHall(t, db)
	start, end := window()

	if _, err := svc.Request(context.Background(), uid, hall, start, end, "wedding", 150, Pricing{}); err != nil {
		t.Fatalf("booking the hall: %v", err)
	}
	if _, err := svc.Request(context.Background(), uid, north, start, end, "yoga", 20, Pricing{}); err != ErrNotBookable {
		t.Fatalf("booking a half of an already-booked hall: err = %v, want ErrNotBookable", err)
	}
}

// ...and the reverse: a half in use must block the whole hall.
func TestBookingChildBlocksParent(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	hall, north, _, uid := seedHall(t, db)
	start, end := window()

	if _, err := svc.Request(context.Background(), uid, north, start, end, "yoga", 20, Pricing{}); err != nil {
		t.Fatalf("booking the north half: %v", err)
	}
	if _, err := svc.Request(context.Background(), uid, hall, start, end, "wedding", 150, Pricing{}); err != ErrNotBookable {
		t.Fatalf("booking the whole hall over a booked half: err = %v, want ErrNotBookable", err)
	}
}

// Siblings are genuinely separate spaces, so they must stay independently
// bookable — the fix must not over-block.
func TestSiblingsRemainIndependent(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	_, north, south, uid := seedHall(t, db)
	start, end := window()

	if _, err := svc.Request(context.Background(), uid, north, start, end, "yoga", 20, Pricing{}); err != nil {
		t.Fatalf("booking north: %v", err)
	}
	if _, err := svc.Request(context.Background(), uid, south, start, end, "pilates", 20, Pricing{}); err != nil {
		t.Fatalf("south is a different space and should still be bookable: %v", err)
	}
}

// The concurrency case this is really about: two requests arriving at once for
// a hall and one of its halves. Exactly one may win — the same guarantee that
// already held for two requests on the same space.
func TestConcurrentParentAndChildRequests(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	hall, north, _, uid := seedHall(t, db)
	start, end := window()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{hall, north} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			_, errs[i] = svc.Request(context.Background(), uid, id, start, end, "concurrent", 10, Pricing{})
		}(i, id)
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d of 2 concurrent requests succeeded across a hall and its half, want exactly 1 (errs: %v)", won, errs)
	}

	// And the database agrees: one booking exists across the whole set.
	var n int64
	db.Model(&domain.Booking{}).
		Where("facility_id IN ? AND status IN ?", []string{hall, north},
			[]domain.BookingStatus{domain.StatusPending, domain.StatusConfirmed}).Count(&n)
	if n != 1 {
		t.Fatalf("%d bookings persisted across the hall and its half, want 1", n)
	}
}

// The real concurrency proof: the row locks are enforced, several requests
// genuinely overlap, and this exercises both halves of the guarantee — that
// exactly one wins, and that requests locking overlapping subtrees do not
// deadlock each other. Both failure modes have been observed here.
func TestConcurrentHierarchyRequests(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	hall, north, south, uid := seedHall(t, db)
	start, end := window()

	// Eight racers over the same window: the hall, and its two halves. The hall
	// conflicts with both halves; the halves do not conflict with each other. So
	// either the hall wins alone, or both halves win and the hall loses.
	targets := []string{hall, north, south, hall, north, south, hall, north}
	var wg sync.WaitGroup
	errs := make([]error, len(targets))
	winners := make([]string, len(targets))
	for i, id := range targets {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			_, err := svc.Request(context.Background(), uid, id, start, end, "race", 10, Pricing{})
			errs[i] = err
			if err == nil {
				winners[i] = id
			}
		}(i, id)
	}
	wg.Wait()

	won := map[string]int{}
	for _, id := range winners {
		if id != "" {
			won[id]++
		}
	}
	for id, n := range won {
		if n > 1 {
			t.Errorf("space %s was booked %d times for one window", id, n)
		}
	}
	switch {
	case won[hall] == 1 && won[north] == 0 && won[south] == 0:
		// The hall took the window; both halves correctly lost.
	case won[hall] == 0 && (won[north] == 1 || won[south] == 1):
		// A half (or both, being siblings) took it; the hall correctly lost.
	default:
		t.Fatalf("inconsistent outcome across the hierarchy: hall=%d north=%d south=%d (errs %v)",
			won[hall], won[north], won[south], errs)
	}

	// Nothing may fail for a reason other than the slot being taken — a deadlock
	// or lock-wait timeout would surface here rather than as a wrong count.
	for i, err := range errs {
		if err != nil && err != ErrNotBookable {
			t.Errorf("request %d (%s) failed with %v, want nil or ErrNotBookable", i, targets[i], err)
		}
	}
}

// A non-overlapping time on a related space is fine; the constraint is about
// the same window, not the same building.
func TestRelatedSpaceFreeAtAnotherTime(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	hall, north, _, uid := seedHall(t, db)
	start, end := window()

	if _, err := svc.Request(context.Background(), uid, hall, start, end, "wedding", 150, Pricing{}); err != nil {
		t.Fatal(err)
	}
	later, laterEnd := end.Add(2*60*60*1e9), end.Add(3*60*60*1e9)
	if _, err := svc.Request(context.Background(), uid, north, later, laterEnd, "yoga", 20, Pricing{}); err != nil {
		t.Fatalf("a later window on the half should be bookable: %v", err)
	}
}
