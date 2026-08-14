package reports

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

func TestSummarizeMetrics(t *testing.T) {
	db := newDB(t)
	f := domain.Facility{Name: "Hall", FeeCents: 10000}
	db.Create(&f)
	// A real booker: MariaDB enforces the bookings→users foreign key, so a
	// booking with no owner is rejected there (SQLite silently accepted it).
	booker := domain.User{Subject: "reports-booker", Email: "b@example.com", Role: domain.RoleResident}
	db.Create(&booker)
	for wd := 0; wd < 7; wd++ {
		db.Create(&domain.AvailabilityRule{FacilityID: f.ID, Weekday: wd, OpenMinute: 8 * 60, CloseMinute: 22 * 60})
	}

	now := time.Now()
	// 3 confirmed bookings this year on distinct days: 2 resident, 1 non-resident.
	// Two are paid ($100 each), one is unpaid.
	mk := func(daysAgo int, resident, paid bool) {
		start := now.AddDate(0, 0, -daysAgo)
		b := domain.Booking{FacilityID: f.ID, UserID: booker.ID, Status: domain.StatusConfirmed, StartsAt: start, EndsAt: start.Add(time.Hour), FeeCents: 10000, Resident: resident}
		db.Create(&b)
		if paid {
			db.Create(&domain.Payment{BookingID: b.ID, AmountCents: 10000, Status: domain.PayPaid})
		}
	}
	mk(5, true, true)
	mk(20, true, true)
	mk(40, false, false)
	// A cancelled booking must not count toward bookings/revenue.
	cancelled := domain.Booking{FacilityID: f.ID, UserID: booker.ID, Status: domain.StatusCancelled, StartsAt: now.AddDate(0, 0, -3), EndsAt: now}
	db.Create(&cancelled)
	// A pending booking created 2 days ago (counts as pending + over 24h).
	db.Create(&domain.Booking{FacilityID: f.ID, UserID: booker.ID, Status: domain.StatusPending, StartsAt: now.AddDate(0, 0, 5), EndsAt: now.AddDate(0, 0, 5).Add(time.Hour), Base: domain.Base{CreatedAt: now.Add(-48 * time.Hour)}})

	d, err := NewService(db).Summarize(context.Background(), Year)
	if err != nil {
		t.Fatal(err)
	}
	if d.Bookings != 3 {
		t.Errorf("bookings = %d, want 3 (confirmed only)", d.Bookings)
	}
	if d.RevenueCents != 20000 {
		t.Errorf("revenue = %d, want 20000 (two paid)", d.RevenueCents)
	}
	if d.Pending != 1 || d.PendingOver24h != 1 {
		t.Errorf("pending = %d / over24h = %d, want 1/1", d.Pending, d.PendingOver24h)
	}
	if d.ResidentPct != 66 {
		t.Errorf("residentPct = %d, want 66 (2 of 3)", d.ResidentPct)
	}
	if len(d.ByFacility) != 1 || d.ByFacility[0].Bookings != 3 {
		t.Errorf("byFacility = %+v, want one facility with 3", d.ByFacility)
	}
	if len(d.TopSpaces) != 1 || d.TopSpaces[0].RevenueCents != 20000 {
		t.Errorf("topSpaces = %+v, want one with 20000", d.TopSpaces)
	}
	if len(d.Trend) != 6 {
		t.Errorf("trend points = %d, want 6", len(d.Trend))
	}
}
