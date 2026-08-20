// Package waiver handles waiver / proof-of-insurance documents attached to a
// booking (§4.11): secure upload (via internal/media), ownership-checked access,
// and confirming a booking whose only remaining gate was the waiver.
package waiver

import (
	"context"
	"errors"
	"io"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/media"
)

var (
	ErrNotFound  = errors.New("waiver: booking or document not found")
	ErrForbidden = errors.New("waiver: not permitted")
)

// Service stores waiver documents and gates booking confirmation on them.
type Service struct {
	db    *gorm.DB
	store *media.Store
}

// NewService wires the waiver service to its on-disk media store.
func NewService(db *gorm.DB, store *media.Store) *Service {
	return &Service{db: db, store: store}
}

// Upload validates and stores a waiver for the caller's booking, replacing any
// prior document. If the facility required only a waiver (not staff approval), a
// pending booking is confirmed. Returns the (possibly updated) booking.
func (s *Service) Upload(ctx context.Context, actor *domain.User, bookingID string, r io.Reader) (*domain.Booking, error) {
	var b domain.Booking
	if err := s.db.WithContext(ctx).Preload("Facility").First(&b, "id = ?", bookingID).Error; err != nil {
		return nil, ErrNotFound
	}
	if b.UserID != actor.ID {
		return nil, ErrForbidden
	}

	stored, err := s.store.Save(r)
	if err != nil {
		return nil, err
	}
	doc := domain.WaiverDocument{
		BookingID: bookingID, StoredName: stored.Name,
		ContentType: stored.ContentType, SizeBytes: stored.Size,
	}
	if err := s.db.WithContext(ctx).
		Where("booking_id = ?", bookingID).
		Assign(doc).FirstOrCreate(&doc).Error; err != nil {
		return nil, err
	}

	// Waiver was the only gate → confirm now.
	if b.Facility != nil && b.Facility.RequiresWaiver && !b.Facility.RequiresApproval && b.Status == domain.StatusPending {
		b.Status = domain.StatusConfirmed
		if err := s.db.WithContext(ctx).Model(&b).Update("status", domain.StatusConfirmed).Error; err != nil {
			return nil, err
		}
	}
	return &b, nil
}

// Has reports whether a booking has a waiver document on file.
func (s *Service) Has(ctx context.Context, bookingID string) bool {
	var n int64
	s.db.WithContext(ctx).Model(&domain.WaiverDocument{}).Where("booking_id = ?", bookingID).Count(&n)
	return n > 0
}

// Open returns the document bytes and content type for the booking's owner or
// any staff member. The caller streams it back with hardened headers.
func (s *Service) Open(ctx context.Context, actor *domain.User, bookingID string) (io.ReadCloser, string, error) {
	var b domain.Booking
	if err := s.db.WithContext(ctx).First(&b, "id = ?", bookingID).Error; err != nil {
		return nil, "", ErrNotFound
	}
	staff := actor.Role == domain.RoleStaff || actor.Role == domain.RoleAdmin
	if b.UserID != actor.ID && !staff {
		return nil, "", ErrForbidden
	}
	var doc domain.WaiverDocument
	if err := s.db.WithContext(ctx).First(&doc, "booking_id = ?", bookingID).Error; err != nil {
		return nil, "", ErrNotFound
	}
	rc, err := s.store.Open(doc.StoredName)
	if err != nil {
		return nil, "", ErrNotFound
	}
	return rc, doc.ContentType, nil
}
