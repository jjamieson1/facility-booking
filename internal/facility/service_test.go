package facility

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

// makeBooker seeds a user to own test bookings. MariaDB enforces the
// bookings→users foreign key, so an ownerless booking is rejected outright —
// and because these fixtures do not check the insert error, that surfaced as
// "the space is unexpectedly free" rather than as a failed insert.
func makeBooker(t *testing.T, db *gorm.DB) domain.User {
	t.Helper()
	u := domain.User{Subject: "fixture-" + t.Name(), Email: "fixture@example.com", Role: domain.RoleResident}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// makeFacility seeds a facility open 08:00–22:00 daily with the given capacity.
func makeFacility(t *testing.T, db *gorm.DB, name string, capacity int) domain.Facility {
	t.Helper()
	f := domain.Facility{Name: name, Capacity: capacity, MinMinutes: 60, MaxMinutes: 480, BufferMinutes: 30}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	for wd := 0; wd < 7; wd++ {
		db.Create(&domain.AvailabilityRule{FacilityID: f.ID, Weekday: wd, OpenMinute: 8 * 60, CloseMinute: 22 * 60})
	}
	return f
}

func TestSearchByWindow(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	small := makeFacility(t, db, "Small Room", 20)
	makeFacility(t, db, "Big Hall", 200)

	// Wednesday 14:00–17:00 (local wall-clock — opening hours are local).
	from := time.Date(2026, 7, 22, 14, 0, 0, 0, time.Local)
	to := from.Add(3 * time.Hour)

	// Confirmed booking blocks Small Room for the window.
	if err := db.Create(&domain.Booking{FacilityID: small.ID, UserID: makeBooker(t, db).ID,
		Status: domain.StatusConfirmed, StartsAt: from, EndsAt: to}).Error; err != nil {
		t.Fatal(err)
	}

	names := func(fs []domain.Facility) []string {
		out := make([]string, len(fs))
		for i, f := range fs {
			out[i] = f.Name
		}
		return out
	}

	// Window only: Small Room is taken, Big Hall is free.
	got, err := svc.Search(context.Background(), Filter{}, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if g := names(got); len(g) != 1 || g[0] != "Big Hall" {
		t.Errorf("window search = %v, want [Big Hall]", g)
	}

	// Window ANDed with capacity: Big Hall meets 100, Small Room wouldn't anyway.
	got, _ = svc.Search(context.Background(), Filter{MinCapacity: 100}, from, to)
	if g := names(got); len(g) != 1 || g[0] != "Big Hall" {
		t.Errorf("window+capacity = %v, want [Big Hall]", g)
	}

	// Outside opening hours → nothing free.
	night := time.Date(2026, 7, 22, 23, 0, 0, 0, time.Local)
	got, _ = svc.Search(context.Background(), Filter{}, night, night.Add(time.Hour))
	if len(got) != 0 {
		t.Errorf("outside hours = %v, want none", names(got))
	}

	// Zero window degrades to parameter search (both facilities).
	got, _ = svc.Search(context.Background(), Filter{}, time.Time{}, time.Time{})
	if len(got) != 2 {
		t.Errorf("no window = %d facilities, want 2", len(got))
	}
}

func TestCalendarStatuses(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	hall := makeFacility(t, db, "Hall", 100) // open 08:00–22:00 daily

	// A Monday two weeks out (avoids "past" ambiguity).
	base := time.Now().AddDate(0, 0, 14)
	monday := time.Date(base.Year(), base.Month(), base.Day()-((int(base.Weekday())+6)%7), 0, 0, 0, 0, base.Location())

	// A confirmed booking Wed 14:00–16:00.
	wed := monday.AddDate(0, 0, 2).Add(14 * time.Hour)
	if err := db.Create(&domain.Booking{FacilityID: hall.ID, UserID: makeBooker(t, db).ID,
		Status: domain.StatusConfirmed, StartsAt: wed, EndsAt: wed.Add(2 * time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	// A blackout all day Fri.
	fri := monday.AddDate(0, 0, 4)
	db.Create(&domain.Blackout{FacilityID: hall.ID, StartsAt: fri, EndsAt: fri.Add(24 * time.Hour)})

	cal, err := svc.Calendar(context.Background(), hall.ID, monday, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(cal.Days) != 7 {
		t.Fatalf("days = %d, want 7", len(cal.Days))
	}
	// Rows: 8,10,12,14,16,18,20 → the 14:00 slot is index 3.
	if got := cal.Days[2].Slots[3].Status; got != "booked" {
		t.Errorf("Wed 14:00 = %q, want booked", got)
	}
	if got := cal.Days[0].Slots[3].Status; got != "open" {
		t.Errorf("Mon 14:00 = %q, want open", got)
	}
	for i, s := range cal.Days[4].Slots {
		if s.Status != "blackout" {
			t.Errorf("Fri slot %d = %q, want blackout", i, s.Status)
		}
	}
}

func TestBlackoutBlocksBooking(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	hall := makeFacility(t, db, "Hall", 100)

	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	from := day.Add(14 * time.Hour)
	to := from.Add(2 * time.Hour)

	// Free before any blackout.
	if free, _ := svc.windowFree(context.Background(), hall, from, to); !free {
		t.Fatal("window should be free before blackout")
	}
	// Staff marks the whole day unavailable.
	if _, err := svc.AddBlackout(context.Background(), hall.ID, day, day.Add(24*time.Hour), "maintenance"); err != nil {
		t.Fatalf("AddBlackout: %v", err)
	}
	// Now the window is not bookable, and search excludes it.
	if free, _ := svc.windowFree(context.Background(), hall, from, to); free {
		t.Error("window should be blocked during blackout")
	}
	got, _ := svc.Search(context.Background(), Filter{}, from, to)
	if len(got) != 0 {
		t.Errorf("search during blackout = %d facilities, want 0", len(got))
	}
	// A reversed range is rejected.
	if _, err := svc.AddBlackout(context.Background(), hall.ID, to, from, ""); err != ErrBadRange {
		t.Errorf("reversed range err = %v, want ErrBadRange", err)
	}
}
