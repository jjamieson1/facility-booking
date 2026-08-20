// Package reminders sends a one-time reminder to bookers as their confirmed
// booking approaches (§4.10). It runs as a background ticker; the reminder is
// idempotent via Booking.ReminderSentAt.
package reminders

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/notify"
)

// Scheduler periodically reminds bookers of imminent confirmed bookings.
type Scheduler struct {
	db       *gorm.DB
	notifier notify.Notifier
	lead     time.Duration // remind when a booking starts within this window
	every    time.Duration // how often to scan
}

// NewScheduler builds the reminder scheduler. lead is how far ahead to remind
// (e.g. 24h); every is the scan interval.
func NewScheduler(db *gorm.DB, notifier notify.Notifier, lead, every time.Duration) *Scheduler {
	return &Scheduler{db: db, notifier: notifier, lead: lead, every: every}
}

// Run scans immediately, then on every tick until ctx is cancelled. Intended to
// be started in its own goroutine.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		if err := s.scan(ctx); err != nil {
			log.Printf("reminders: scan failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// scan finds confirmed bookings starting within the lead window that haven't
// been reminded, sends each a reminder, and stamps ReminderSentAt.
func (s *Scheduler) scan(ctx context.Context) error {
	now := time.Now()
	var due []domain.Booking
	err := s.db.WithContext(ctx).Preload("Facility").
		Where("status = ? AND reminder_sent_at IS NULL AND starts_at > ? AND starts_at <= ?",
			domain.StatusConfirmed, now, now.Add(s.lead)).
		Find(&due).Error
	if err != nil {
		return err
	}
	for i := range due {
		b := due[i]
		instructions := ""
		if b.Facility != nil {
			instructions = b.Facility.BeforeInstructions
		}
		s.notifier.BookingReminder(b, instructions)
		sent := time.Now()
		if err := s.db.WithContext(ctx).Model(&domain.Booking{}).
			Where("id = ?", b.ID).Update("reminder_sent_at", sent).Error; err != nil {
			log.Printf("reminders: stamp booking %s failed: %v", b.ID, err)
		}
	}
	return nil
}
