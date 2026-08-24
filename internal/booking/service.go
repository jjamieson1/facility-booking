// Package booking owns the booking lifecycle: requesting a slot (with
// double-booking prevention), staff approval/denial, cancellation, and the
// queries behind "my bookings" and the staff queue.
package booking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jjamieson1/facility-booking/internal/availability"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/facility"
)

// Sentinel errors mapped to HTTP status codes by the handler layer.
var (
	ErrFacilityNotFound = errors.New("booking: facility not found")
	ErrNotBookable      = errors.New("booking: window not available")
	ErrNotFound         = errors.New("booking: not found")
	ErrForbidden        = errors.New("booking: not permitted")
	ErrBadState         = errors.New("booking: invalid state for this action")
	ErrNotModifiable    = errors.New("booking: past the modifiable window")
)

// CutoffResolver reports how close to its start a booking may still be
// rescheduled, per the facility's cancellation policy (§4.9). It is an interface
// here rather than a direct dependency on internal/policy so the booking service
// keeps its narrow surface — and so tests can set a cutoff without a policy row.
type CutoffResolver interface {
	ModificationCutoff(ctx context.Context, facilityID string) (time.Duration, error)
}

// Service holds the DB handle and, optionally, the policy that governs how late
// a booking may be changed.
type Service struct {
	db      *gorm.DB
	cutoffs CutoffResolver
}

// NewService constructs the booking service. A nil CutoffResolver means no
// policy cutoff is enforced beyond "the booking has not started".
func NewService(db *gorm.DB, cutoffs CutoffResolver) *Service {
	return &Service{db: db, cutoffs: cutoffs}
}

// Pricing is the entitlement picture resolved for the booker *before* this
// transaction opens (§P2-5.11a constraint 1 — a provider callout inside the
// transaction would hold the double-booking row locks for the provider's
// latency) and applied unchanged inside it (constraint 2 — the quote and the
// charge must use the same determinations, or the price shown is not the price
// paid).
//
// Stamp is written against the booking and never rewritten: a later enrolment
// applies to subsequent bookings rather than repricing a completed one.
type Pricing struct {
	Resident bool
	Stamp    []domain.BookingEntitlement
}

// Request creates a booking for the user. It runs inside a transaction that
// locks the facility's active bookings, re-checks availability, and inserts —
// so two concurrent requests for the same slot cannot both succeed. A facility
// that auto-confirms yields a confirmed booking; otherwise it is pending.
func (s *Service) Request(ctx context.Context, userID, facilityID string, start, end time.Time, purpose string, attendance int, pricing Pricing) (*domain.Booking, error) {
	return s.requestOne(ctx, userID, facilityID, start, end, purpose, attendance, nil, pricing)
}

// requestOne is the single-occurrence booking core shared by Request and
// RequestRecurring; recurrenceID groups occurrences of a recurring booking.
func (s *Service) requestOne(ctx context.Context, userID, facilityID string, start, end time.Time, purpose string, attendance int, recurrenceID *string, pricing Pricing) (*domain.Booking, error) {
	var created *domain.Booking
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fac domain.Facility
		if err := tx.First(&fac, "id = ?", facilityID).Error; err != nil {
			return ErrFacilityNotFound
		}

		rules, blackouts, bookings, err := loadWindow(tx, facilityID, start, end)
		if err != nil {
			return err
		}

		reason := availability.Check(availability.Input{
			Facility: fac, Rules: rules, Blackouts: blackouts, Bookings: bookings,
			Start: start, End: end,
		})
		if reason != availability.OK {
			return ErrNotBookable
		}

		// A facility needing staff approval or a waiver holds the booking as
		// pending until that gate is met (§4.11 waiver-before-confirmation).
		status := domain.StatusConfirmed
		if fac.RequiresApproval || fac.RequiresWaiver {
			status = domain.StatusPending
		}
		// Price from the entitlements resolved before this transaction opened —
		// never from a fresh lookup here, so the amount quoted is the amount
		// charged. Booking.Resident stays the reporting split (reports.residentPct).
		b := domain.Booking{
			FacilityID: facilityID, UserID: userID, StartsAt: start, EndsAt: end,
			Status: status, Purpose: purpose, Attendance: attendance,
			FeeCents:     fac.FeeFor(pricing.Resident),
			Resident:     pricing.Resident,
			RecurrenceID: recurrenceID,
		}
		if err := tx.Create(&b).Error; err != nil {
			return err
		}
		// Stamp the determinations that produced this price. Written once, with
		// the booking, so the record can never disagree with what was charged.
		for _, e := range pricing.Stamp {
			e.ID, e.BookingID = "", b.ID
			if err := tx.Create(&e).Error; err != nil {
				return err
			}
		}
		created = &b
		return nil
	})
	return created, err
}

// loadWindow fetches the rules, blackouts, and active bookings relevant to a
// window, taking an update lock on the bookings row-set to serialize concurrent
// requests.
//
// Bookings are gathered across the facility's **conflict set** — itself, its
// ancestors and its descendants — because a hall and its halves are the same
// physical space. Locking that whole set, in the sorted order ConflictSet
// returns, is what keeps two concurrent requests on overlapping subtrees from
// both succeeding or from deadlocking against each other.
//
// Opening hours and blackouts stay scoped to the facility being booked: those
// are its own rules, not the building's.
func loadWindow(tx *gorm.DB, facilityID string, start, end time.Time) ([]domain.AvailabilityRule, []domain.Blackout, []domain.Booking, error) {
	var rules []domain.AvailabilityRule
	if err := tx.Where("facility_id = ?", facilityID).Find(&rules).Error; err != nil {
		return nil, nil, nil, err
	}
	var blackouts []domain.Blackout
	if err := tx.Where("facility_id = ? AND starts_at < ? AND ends_at > ?", facilityID, end, start).Find(&blackouts).Error; err != nil {
		return nil, nil, nil, err
	}
	conflicting, err := facility.ConflictSet(tx, facilityID)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := lockFacilities(tx, conflicting); err != nil {
		return nil, nil, nil, err
	}
	// Widen the booking scan by a day so buffer padding around the window is
	// always covered; the in-memory Check applies the exact buffer math.
	//
	// FOR UPDATE here is doing a second, less obvious job: under REPEATABLE READ
	// a plain SELECT reads the transaction's snapshot, so a request that waited
	// on the facility lock would still not see the booking the winner just
	// committed — and both would succeed. A locking read always sees the latest
	// committed row. Dropping this clause makes every concurrent request win.
	var bookings []domain.Booking
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("facility_id IN ? AND status IN ? AND ends_at > ? AND starts_at < ?",
			conflicting, []domain.BookingStatus{domain.StatusPending, domain.StatusConfirmed},
			start.Add(-24*time.Hour), end.Add(24*time.Hour)).
		Find(&bookings).Error; err != nil {
		return nil, nil, nil, err
	}
	return rules, blackouts, bookings, nil
}

// lockFacilities takes an exclusive row lock on each facility in the conflict
// set, **one at a time in sorted order**. The facility rows act as named mutexes
// for the booking path: any two conflicting requests share at least one id (a
// parent and child both contain the parent), so they serialise; unrelated
// facilities share none and stay concurrent.
//
// Locking these first, in one fixed order, is what prevents deadlock. An
// earlier version locked only the *bookings* range with `ORDER BY facility_id,
// id` and assumed that fixed the acquisition order. It does not: ORDER BY
// governs the result set, not the order InnoDB takes locks, and
// `SELECT … FOR UPDATE` over a sparse range also takes gap locks — two
// transactions on overlapping subtrees then deadlocked (MariaDB error 1213).
// SQLite cannot show this, because FOR UPDATE is a no-op there; it surfaced
// only once the suite ran on MariaDB.
func lockFacilities(tx *gorm.DB, ids []string) error {
	for _, id := range ids { // ids arrive sorted from ConflictSet
		var locked []domain.Facility
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").Find(&locked, "id = ?", id).Error; err != nil {
			return err
		}
	}
	return nil
}

// Reschedule moves a booking to a new [start, end] window. The owner may change
// their own booking; staff may change any. It is only allowed while the booking
// is active and hasn't started yet (the modifiable window), and the new window
// must be available (checked ignoring this booking's own slot). Confirmed
// bookings stay confirmed; the caller re-sends the calendar invite.
func (s *Service) Reschedule(ctx context.Context, actor *domain.User, bookingID string, start, end time.Time) (*domain.Booking, error) {
	var b domain.Booking
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&b, "id = ?", bookingID).Error; err != nil {
			return ErrNotFound
		}
		if b.UserID != actor.ID && actor.Role != domain.RoleStaff && actor.Role != domain.RoleAdmin {
			return ErrForbidden
		}
		if !b.Active() || !b.StartsAt.After(time.Now()) {
			return ErrNotModifiable
		}
		// The policy's modification cutoff: too close to the start and the
		// booking is fixed, because staff have already planned around it.
		// Resolved outside this transaction's locking concerns — it is a local
		// read, but keep it before loadWindow so a refusal costs no locks.
		if s.cutoffs != nil {
			cutoff, err := s.cutoffs.ModificationCutoff(ctx, b.FacilityID)
			if err != nil {
				return err
			}
			if cutoff > 0 && time.Until(b.StartsAt) < cutoff {
				return ErrNotModifiable
			}
		}

		var fac domain.Facility
		if err := tx.First(&fac, "id = ?", b.FacilityID).Error; err != nil {
			return ErrFacilityNotFound
		}
		rules, blackouts, bookings, err := loadWindow(tx, b.FacilityID, start, end)
		if err != nil {
			return err
		}
		reason := availability.Check(availability.Input{
			Facility: fac, Rules: rules, Blackouts: blackouts, Bookings: bookings,
			Start: start, End: end, ExcludeBookingID: b.ID,
		})
		if reason != availability.OK {
			return ErrNotBookable
		}

		b.StartsAt, b.EndsAt = start, end
		if err := tx.Model(&b).Updates(map[string]any{"starts_at": start, "ends_at": end}).Error; err != nil {
			return err
		}
		staff := actor.Role == domain.RoleStaff || actor.Role == domain.RoleAdmin
		if staff && b.UserID != actor.ID {
			return writeAudit(tx, actor.ID, "booking.reschedule", b.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// RecurringResult reports the outcome of a recurring booking request: the
// occurrences that were booked and the start times skipped for conflicts.
type RecurringResult struct {
	RecurrenceID string           `json:"recurrenceId"`
	Created      []domain.Booking `json:"created"`
	Skipped      []time.Time      `json:"skipped"`
}

// RequestRecurring books a weekly-repeating slot for `weeks` occurrences in one
// request (§4.11). Each occurrence is booked independently: those that conflict
// (double-book, blackout, outside hours) are skipped and reported, not fatal —
// so a regular group still gets every free week. weeks is clamped to [1, 52].
// Entitlements are resolved once by the caller and applied to every occurrence,
// so a repeating booking makes one round of provider calls rather than one per
// week.
func (s *Service) RequestRecurring(ctx context.Context, userID, facilityID string, start, end time.Time, weeks int, purpose string, attendance int, pricing Pricing) (*RecurringResult, error) {
	if weeks < 1 {
		weeks = 1
	}
	if weeks > 52 {
		weeks = 52
	}
	recurrenceID := uuid.NewString()
	// Initialize as empty (not nil) slices so they serialize as [] not null.
	res := &RecurringResult{RecurrenceID: recurrenceID, Created: []domain.Booking{}, Skipped: []time.Time{}}
	for i := 0; i < weeks; i++ {
		occStart := start.AddDate(0, 0, 7*i)
		occEnd := end.AddDate(0, 0, 7*i)
		b, err := s.requestOne(ctx, userID, facilityID, occStart, occEnd, purpose, attendance, &recurrenceID, pricing)
		switch {
		case errors.Is(err, ErrNotBookable):
			res.Skipped = append(res.Skipped, occStart)
		case err != nil:
			return nil, err
		default:
			res.Created = append(res.Created, *b)
		}
	}
	return res, nil
}

// Approve confirms a pending booking (staff action).
func (s *Service) Approve(ctx context.Context, actorID, bookingID string) (*domain.Booking, error) {
	return s.transition(ctx, actorID, bookingID, domain.StatusPending, domain.StatusConfirmed, "booking.approve")
}

// Deny rejects a pending booking (staff action).
func (s *Service) Deny(ctx context.Context, actorID, bookingID string) (*domain.Booking, error) {
	return s.transition(ctx, actorID, bookingID, domain.StatusPending, domain.StatusDenied, "booking.deny")
}

// transition moves a booking from want→next, writing an audit record. The
// from-status guard keeps actions idempotent and prevents illegal jumps.
func (s *Service) transition(ctx context.Context, actorID, bookingID string, want, next domain.BookingStatus, action string) (*domain.Booking, error) {
	var b domain.Booking
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&b, "id = ?", bookingID).Error; err != nil {
			return ErrNotFound
		}
		if b.Status != want {
			return ErrBadState
		}
		b.Status = next
		if err := tx.Model(&b).Update("status", next).Error; err != nil {
			return err
		}
		return writeAudit(tx, actorID, action, b.ID)
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// Cancel cancels a booking. The owner may cancel their own; staff may cancel any.
func (s *Service) Cancel(ctx context.Context, actor *domain.User, bookingID string) (*domain.Booking, error) {
	var b domain.Booking
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&b, "id = ?", bookingID).Error; err != nil {
			return ErrNotFound
		}
		staff := actor.Role == domain.RoleStaff || actor.Role == domain.RoleAdmin
		if b.UserID != actor.ID && !staff {
			return ErrForbidden
		}
		if b.Status == domain.StatusCancelled || b.Status == domain.StatusDenied {
			return ErrBadState
		}
		b.Status = domain.StatusCancelled
		if err := tx.Model(&b).Update("status", domain.StatusCancelled).Error; err != nil {
			return err
		}
		if staff && b.UserID != actor.ID {
			return writeAudit(tx, actor.ID, "booking.cancel", b.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ListForUser returns a user's bookings, newest first.
func (s *Service) ListForUser(ctx context.Context, userID string) ([]domain.Booking, error) {
	var out []domain.Booking
	err := s.db.WithContext(ctx).Preload("Facility").Preload("Payment").Preload("Waiver").
		Where("user_id = ?", userID).Order("starts_at desc").Find(&out).Error
	return out, err
}

// UpcomingForUser returns a user's future bookings that are confirmed or pending
// (awaiting approval), soonest first, with their facility loaded. Used to build
// the C2 service-card callout payload.
func (s *Service) UpcomingForUser(ctx context.Context, userID string) ([]domain.Booking, error) {
	var out []domain.Booking
	err := s.db.WithContext(ctx).Preload("Facility").
		Where("user_id = ? AND starts_at > ? AND status IN ?",
			userID, time.Now(), []domain.BookingStatus{domain.StatusConfirmed, domain.StatusPending}).
		Order("starts_at asc").Find(&out).Error
	return out, err
}

// ListPending returns bookings awaiting STAFF approval, oldest first. It excludes
// bookings that are pending only because they need a waiver — those are the
// booker's to resolve (upload on the booking page), not a staff decision.
func (s *Service) ListPending(ctx context.Context) ([]domain.Booking, error) {
	var out []domain.Booking
	err := s.db.WithContext(ctx).Preload("Facility").Preload("User").
		Joins("JOIN facilities ON facilities.id = bookings.facility_id").
		Where("bookings.status = ? AND facilities.requires_approval = ?", domain.StatusPending, true).
		Order("bookings.starts_at asc").Find(&out).Error
	return out, err
}

// ListForCalendar returns confirmed bookings (with facility) for the city iCal
// feed, ordered by start time.
func (s *Service) ListForCalendar(ctx context.Context) ([]domain.Booking, error) {
	var out []domain.Booking
	err := s.db.WithContext(ctx).Preload("Facility").
		Where("status = ?", domain.StatusConfirmed).Order("starts_at asc").Find(&out).Error
	return out, err
}

// Get loads a booking with its facility and payment.
func (s *Service) Get(ctx context.Context, id string) (*domain.Booking, error) {
	var b domain.Booking
	if err := s.db.WithContext(ctx).Preload("Facility").Preload("Payment").Preload("Waiver").First(&b, "id = ?", id).Error; err != nil {
		return nil, ErrNotFound
	}
	return &b, nil
}

func writeAudit(tx *gorm.DB, actorID, action, targetID string) error {
	return tx.Create(&domain.AuditLog{
		ActorID: actorID, Action: action, TargetType: "booking", TargetID: targetID,
	}).Error
}

// Audit records a staff action to the local audit log outside a transaction —
// for actions (like refunds) handled outside the booking service's own methods.
func (s *Service) Audit(ctx context.Context, actorID, action, bookingID string) error {
	return writeAudit(s.db.WithContext(ctx), actorID, action, bookingID)
}
