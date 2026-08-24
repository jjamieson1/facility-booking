// Package unpaid releases bookings that were billed but never paid.
//
// A hosted gateway settles out of band: the resident is sent to C2's checkout
// and may simply never pay. Without this, one unpaid request would hold a
// popular slot until its own start time, which is a denial-of-service on the
// calendar that any resident can trigger for free.
package unpaid

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/notify"
)

// Sweeper cancels bookings whose bill has gone unpaid past the hold window.
type Sweeper struct {
	db       *gorm.DB
	notifier notify.Notifier
	hold     time.Duration // how long an unpaid booking keeps its slot
	every    time.Duration
	onFreed  func(domain.Booking) // notify the waitlist; may be nil
}

// NewSweeper builds the sweeper. hold is how long a billed-but-unpaid booking
// keeps its slot (24h by product decision); every is the scan interval.
//
// onFreed is called after a slot is released so the waitlist can be told, which
// is the whole reason releasing early is worth doing.
func NewSweeper(db *gorm.DB, notifier notify.Notifier, hold, every time.Duration, onFreed func(domain.Booking)) *Sweeper {
	return &Sweeper{db: db, notifier: notifier, hold: hold, every: every, onFreed: onFreed}
}

// Run scans immediately, then on every tick until ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		if err := s.Scan(ctx, time.Now()); err != nil {
			log.Printf("unpaid: scan failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Scan releases every booking still unpaid past the hold window. Exported so a
// test can drive it at a chosen time rather than waiting on a ticker.
//
// The window runs from when the *bill* was raised, not from when the booking was
// made: the clock a resident is racing should start when they were first asked
// for money.
func (s *Sweeper) Scan(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-s.hold)
	var due []domain.Booking
	err := s.db.WithContext(ctx).Preload("Facility").
		Where(`status IN (?) AND id IN (
			SELECT booking_id FROM payments
			WHERE status = ? AND created_at <= ? AND deleted_at IS NULL)`,
			[]domain.BookingStatus{domain.StatusPending, domain.StatusConfirmed},
			domain.PayPending, cutoff).
		Find(&due).Error
	if err != nil {
		return err
	}

	for i := range due {
		b := due[i]
		// A booking already under way is left alone: releasing a slot someone may
		// be standing in helps nobody, and the money is a billing problem by then.
		if !b.StartsAt.After(now) {
			continue
		}
		if err := s.release(ctx, b); err != nil {
			log.Printf("unpaid: releasing booking %s failed: %v", b.ID, err)
			continue
		}
		s.notifier.BookingCancelled(b, "")
		if s.onFreed != nil {
			s.onFreed(b)
		}
	}
	return nil
}

// release cancels the booking and abandons the bill in one transaction, so a
// slot can never be freed while its payment still reads as outstanding.
func (s *Sweeper) release(ctx context.Context, b domain.Booking) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.Booking{}).Where("id = ?", b.ID).
			Update("status", domain.StatusCancelled).Error; err != nil {
			return err
		}
		if err := tx.Model(&domain.Payment{}).
			Where("booking_id = ? AND status = ?", b.ID, domain.PayPending).
			Update("status", domain.PayUnpaid).Error; err != nil {
			return err
		}
		return tx.Create(&domain.AuditLog{
			Action: "booking.released.unpaid", TargetType: "booking", TargetID: b.ID,
			Detail: "Released automatically: billed but unpaid past the hold window",
		}).Error
	})
}
