package waitlist

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

type capturingNotifier struct{ opened []string }

func (c *capturingNotifier) BookingSubmitted(domain.Booking)         {}
func (c *capturingNotifier) BookingConfirmed(domain.Booking, string) {}
func (c *capturingNotifier) BookingDenied(domain.Booking)            {}
func (c *capturingNotifier) BookingConditional(domain.Booking)       {}
func (c *capturingNotifier) BookingCancelled(domain.Booking, string) {}
func (c *capturingNotifier) BookingReminder(domain.Booking, string)  {}
func (c *capturingNotifier) WaitlistOpened(e domain.WaitlistEntry, _ string) {
	c.opened = append(c.opened, e.UserID)
}

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.New(t)
	return db
}

func TestJoinAndNotifyFreed(t *testing.T) {
	db := newDB(t)
	f := domain.Facility{Name: "Hall"}
	db.Create(&f)
	n := &capturingNotifier{}
	svc := NewService(db, n)

	start := time.Date(2026, 8, 1, 14, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)

	// Join twice for the same window → idempotent (one entry).
	e1, _ := svc.Join(context.Background(), "u1", f.ID, start, end)
	e2, _ := svc.Join(context.Background(), "u1", f.ID, start, end)
	if e1.ID != e2.ID {
		t.Errorf("re-join created a second entry: %s vs %s", e1.ID, e2.ID)
	}
	// A different user waiting on an overlapping window.
	svc.Join(context.Background(), "u2", f.ID, start.Add(time.Hour), end.Add(time.Hour))

	// A freed booking overlapping both windows notifies both, once each.
	got, err := svc.NotifyFreed(context.Background(), f.ID, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 || len(n.opened) != 2 {
		t.Fatalf("notified %d (opened %v), want 2", got, n.opened)
	}
	// A second free doesn't re-notify (entries are stamped).
	got, _ = svc.NotifyFreed(context.Background(), f.ID, start, end)
	if got != 0 {
		t.Errorf("re-notify count = %d, want 0", got)
	}
	// Notified entries drop off the user's active list.
	active, _ := svc.ListForUser(context.Background(), "u1")
	if len(active) != 0 {
		t.Errorf("active entries after notify = %d, want 0", len(active))
	}
}

func TestNotifyFreedIgnoresNonOverlapping(t *testing.T) {
	db := newDB(t)
	f := domain.Facility{Name: "Hall"}
	db.Create(&f)
	n := &capturingNotifier{}
	svc := NewService(db, n)

	start := time.Date(2026, 8, 1, 14, 0, 0, 0, time.UTC)
	svc.Join(context.Background(), "u1", f.ID, start, start.Add(time.Hour))

	// Free a window that doesn't overlap (later that day).
	got, _ := svc.NotifyFreed(context.Background(), f.ID, start.Add(4*time.Hour), start.Add(5*time.Hour))
	if got != 0 {
		t.Errorf("notified %d for non-overlapping free, want 0", got)
	}
}

func TestExpireStale(t *testing.T) {
	db := newDB(t)
	f := domain.Facility{Name: "Hall"}
	db.Create(&f)
	svc := NewService(db, &capturingNotifier{})
	now := time.Now()

	// Past slot (fully over), in-progress (started, not ended), and future.
	past, _ := svc.Join(context.Background(), "u1", f.ID, now.Add(-3*time.Hour), now.Add(-2*time.Hour))
	svc.Join(context.Background(), "u2", f.ID, now.Add(-30*time.Minute), now.Add(30*time.Minute))
	svc.Join(context.Background(), "u3", f.ID, now.Add(24*time.Hour), now.Add(25*time.Hour))

	n, err := svc.ExpireStale(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expired %d, want 1 (only the fully-past slot)", n)
	}

	// The past entry is gone; the in-progress and future remain visible.
	var count int64
	db.Model(&domain.WaitlistEntry{}).Count(&count)
	if count != 2 {
		t.Errorf("remaining entries = %d, want 2", count)
	}
	if err := db.First(&domain.WaitlistEntry{}, "id = ?", past.ID).Error; err == nil {
		t.Error("past entry should have been soft-deleted")
	}
}
