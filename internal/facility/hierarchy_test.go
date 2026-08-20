package facility

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// makeChild seeds a sub-space of parent, open the same hours.
func makeChild(t *testing.T, db *gorm.DB, parent domain.Facility, name string) domain.Facility {
	t.Helper()
	f := domain.Facility{Name: name, Capacity: 50, MinMinutes: 60, MaxMinutes: 480, BufferMinutes: 30, ParentID: &parent.ID}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	for wd := 0; wd < 7; wd++ {
		db.Create(&domain.AvailabilityRule{FacilityID: f.ID, Weekday: wd, OpenMinute: 8 * 60, CloseMinute: 22 * 60})
	}
	return f
}

func TestConflictSetIncludesAncestorsAndDescendants(t *testing.T) {
	db := newDB(t)
	building := makeFacility(t, db, "Community Centre", 500)
	hall := makeChild(t, db, building, "Hall")
	north := makeChild(t, db, hall, "Hall — North")
	other := makeFacility(t, db, "Ice Arena", 250)

	set, err := ConflictSet(db, hall.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range set {
		got[id] = true
	}

	for _, want := range []struct {
		id, name string
	}{{hall.ID, "itself"}, {building.ID, "its parent"}, {north.ID, "its child"}} {
		if !got[want.id] {
			t.Errorf("conflict set is missing %s", want.name)
		}
	}
	if got[other.ID] {
		t.Error("conflict set must not include an unrelated facility")
	}
}

// Siblings share a parent but are separate spaces; including them would block
// bookings that should be allowed.
func TestConflictSetExcludesSiblings(t *testing.T) {
	db := newDB(t)
	hall := makeFacility(t, db, "Hall", 200)
	north := makeChild(t, db, hall, "North")
	south := makeChild(t, db, hall, "South")

	set, err := ConflictSet(db, north.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range set {
		if id == south.ID {
			t.Fatal("a sibling must not be in the conflict set")
		}
	}
}

// The set is used for ordered row locking, so its order must be stable.
func TestConflictSetIsSorted(t *testing.T) {
	db := newDB(t)
	hall := makeFacility(t, db, "Hall", 200)
	makeChild(t, db, hall, "North")
	makeChild(t, db, hall, "South")

	set, err := ConflictSet(db, hall.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(set); i++ {
		if set[i-1] >= set[i] {
			t.Fatalf("conflict set is not sorted: %v", set)
		}
	}
}

// Bad data must not hang a transaction that is holding row locks.
func TestConflictSetTerminatesOnACycle(t *testing.T) {
	db := newDB(t)
	a := makeFacility(t, db, "A", 10)
	b := makeChild(t, db, a, "B")
	// Close the loop: A's parent becomes B.
	if err := db.Model(&domain.Facility{}).Where("id = ?", a.ID).Update("parent_id", b.ID).Error; err != nil {
		t.Fatal(err)
	}

	done := make(chan []string, 1)
	go func() {
		set, err := ConflictSet(db, a.ID)
		if err != nil {
			t.Error(err)
		}
		done <- set
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ConflictSet did not terminate on a cyclic hierarchy")
	}
}

// The calendar must not offer a slot the booking path would refuse: a booked
// parent hall makes its sub-space unavailable, and that has to be visible.
func TestDayAvailabilityReflectsParentBooking(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	hall := makeFacility(t, db, "Hall", 200)
	north := makeChild(t, db, hall, "North")

	day := time.Date(2026, 7, 22, 0, 0, 0, 0, time.Local)
	start := day.Add(10 * time.Hour)
	u := domain.User{Subject: "s", Email: "e@x", Role: domain.RoleResident}
	db.Create(&u)
	db.Create(&domain.Booking{
		FacilityID: hall.ID, UserID: u.ID, StartsAt: start, EndsAt: start.Add(time.Hour),
		Status: domain.StatusConfirmed, Purpose: "wedding",
	})

	slots, err := svc.DayAvailability(context.Background(), north.ID, day)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slots {
		if s.Start.Equal(start) && s.Available {
			t.Fatal("the sub-space shows as free while its parent hall is booked")
		}
	}
}
