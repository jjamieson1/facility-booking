package reminders

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// capturingNotifier records the bookings it was asked to remind.
type capturingNotifier struct{ reminded []string }

func (c *capturingNotifier) BookingSubmitted(domain.Booking)         {}
func (c *capturingNotifier) BookingConfirmed(domain.Booking, string) {}
func (c *capturingNotifier) BookingDenied(domain.Booking)            {}
func (c *capturingNotifier) BookingConditional(domain.Booking)       {}
func (c *capturingNotifier) BookingCancelled(domain.Booking, string) {}
func (c *capturingNotifier) BookingReminder(b domain.Booking, _ string) {
	c.reminded = append(c.reminded, b.ID)
}
func (c *capturingNotifier) WaitlistOpened(domain.WaitlistEntry, string) {}

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.New(t)
	return db
}

func TestScanRemindsOnceForImminentConfirmed(t *testing.T) {
	db := newDB(t)
	f := domain.Facility{Name: "Hall", BeforeInstructions: "Collect keys at the desk."}
	db.Create(&f)
	// MariaDB enforces the bookings→users foreign key; SQLite did not, so these
	// fixtures used to get away with an ownerless booking.
	booker := domain.User{Subject: "reminders-booker", Email: "b@example.com", Role: domain.RoleResident}
	db.Create(&booker)

	soon := domain.Booking{FacilityID: f.ID, UserID: booker.ID, Status: domain.StatusConfirmed, StartsAt: time.Now().Add(3 * time.Hour), EndsAt: time.Now().Add(4 * time.Hour)}
	farOff := domain.Booking{FacilityID: f.ID, UserID: booker.ID, Status: domain.StatusConfirmed, StartsAt: time.Now().Add(72 * time.Hour), EndsAt: time.Now().Add(73 * time.Hour)}
	pending := domain.Booking{FacilityID: f.ID, UserID: booker.ID, Status: domain.StatusPending, StartsAt: time.Now().Add(2 * time.Hour), EndsAt: time.Now().Add(3 * time.Hour)}
	past := domain.Booking{FacilityID: f.ID, UserID: booker.ID, Status: domain.StatusConfirmed, StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now()}
	for _, b := range []*domain.Booking{&soon, &farOff, &pending, &past} {
		db.Create(b)
	}

	n := &capturingNotifier{}
	s := NewScheduler(db, n, 24*time.Hour, time.Minute)

	if err := s.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(n.reminded) != 1 || n.reminded[0] != soon.ID {
		t.Fatalf("reminded = %v, want [%s] (only the imminent confirmed booking)", n.reminded, soon.ID)
	}

	// A second scan must not remind again (idempotent via ReminderSentAt).
	if err := s.scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(n.reminded) != 1 {
		t.Errorf("second scan reminded again: %v", n.reminded)
	}
}
