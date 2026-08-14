// Package waitlist lets residents wait for a taken slot and be notified when a
// booking overlapping their window is cancelled or denied (§4.11).
package waitlist

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/notify"
)

// ErrFacilityNotFound is returned when the facility id doesn't resolve.
var ErrFacilityNotFound = errors.New("waitlist: facility not found")

// Service manages waitlist entries and notifies them when slots free up.
type Service struct {
	db       *gorm.DB
	notifier notify.Notifier
}

// NewService constructs the waitlist service.
func NewService(db *gorm.DB, notifier notify.Notifier) *Service {
	return &Service{db: db, notifier: notifier}
}

// Join adds the user to a facility's waitlist for [start, end]. It is idempotent:
// re-joining the same window returns the existing entry.
func (s *Service) Join(ctx context.Context, userID, facilityID string, start, end time.Time) (*domain.WaitlistEntry, error) {
	if err := s.db.WithContext(ctx).First(&domain.Facility{}, "id = ?", facilityID).Error; err != nil {
		return nil, ErrFacilityNotFound
	}
	var e domain.WaitlistEntry
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND facility_id = ? AND starts_at = ? AND ends_at = ?", userID, facilityID, start, end).
		Attrs(domain.WaitlistEntry{FacilityID: facilityID, UserID: userID, StartsAt: start, EndsAt: end}).
		FirstOrCreate(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListForUser returns a user's active (un-notified) waitlist entries.
func (s *Service) ListForUser(ctx context.Context, userID string) ([]domain.WaitlistEntry, error) {
	var out []domain.WaitlistEntry
	err := s.db.WithContext(ctx).Preload("Facility").
		Where("user_id = ? AND notified_at IS NULL", userID).
		Order("starts_at asc").Find(&out).Error
	return out, err
}

// ExpireStale soft-deletes waitlist entries whose slot has fully passed
// (ends_at < now). A past slot can never free up, so the entry is dead weight —
// removing it keeps the resident's list and the C2 service-card callout tidy.
// Returns how many were expired.
func (s *Service) ExpireStale(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).
		Where("ends_at < ?", time.Now()).
		Delete(&domain.WaitlistEntry{})
	return res.RowsAffected, res.Error
}

// Leave removes a waitlist entry owned by the user.
func (s *Service) Leave(ctx context.Context, userID, id string) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&domain.WaitlistEntry{}).Error
}

// NotifyFreed is called when a booking is cancelled or denied. It notifies every
// un-notified waitlist entry whose window overlaps the freed booking's window,
// stamping NotifiedAt so each is told once. Returns how many were notified.
func (s *Service) NotifyFreed(ctx context.Context, facilityID string, start, end time.Time) (int, error) {
	var facility domain.Facility
	if err := s.db.WithContext(ctx).First(&facility, "id = ?", facilityID).Error; err != nil {
		return 0, nil // facility gone; nothing to notify
	}
	var entries []domain.WaitlistEntry
	err := s.db.WithContext(ctx).
		Where("facility_id = ? AND notified_at IS NULL AND starts_at < ? AND ends_at > ?", facilityID, end, start).
		Find(&entries).Error
	if err != nil {
		return 0, err
	}
	now := time.Now()
	for i := range entries {
		s.notifier.WaitlistOpened(entries[i], facility.Name)
		if err := s.db.WithContext(ctx).Model(&domain.WaitlistEntry{}).
			Where("id = ?", entries[i].ID).Update("notified_at", now).Error; err != nil {
			return i, err
		}
	}
	return len(entries), nil
}
