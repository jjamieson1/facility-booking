// Package facility manages the facility directory: reads for the public site,
// writes for staff, filtered search, and per-day availability slots.
package facility

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/availability"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// ErrNotFound is returned when a facility id doesn't resolve.
var ErrNotFound = errors.New("facility: not found")

// Service reads and writes facilities.
type Service struct{ db *gorm.DB }

// NewService constructs the facility service.
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// Filter narrows a directory query, covering the five parameters §4.3 requires:
// capacity, required accessories, cost, area, and accessibility needs. Zero
// values mean "no constraint", and every constraint present is ANDed — §4.3 is
// explicit that filters combine and all must match.
type Filter struct {
	MinCapacity int
	FreeOnly    bool

	// Accessories must *all* be present, not any. A resident who ticks both
	// "projector" and "sound system" needs one room with both; offering rooms
	// with either would hand them a list they then have to re-check by hand.
	Accessories []string

	Area string // neighbourhood/zone, matched exactly against Facility.Area

	// Accessibility needs. Only the true case constrains: someone who has not
	// asked for step-free access should still see step-free facilities.
	StepFree           bool
	AccessibleWashroom bool

	// Cost range in cents. MaxFeeCents == 0 means no ceiling rather than "free
	// only" — FreeOnly already expresses that, and a $0 ceiling arriving from an
	// untouched form field would otherwise silently hide every priced facility.
	MinFeeCents int
	MaxFeeCents int

	// Resident selects which of the two price columns the cost range applies to.
	// It is resolved from the session by the handler, never taken from the
	// request: residency is an entitlement, and a filter that priced off a
	// client-supplied flag would quote the resident rate to anyone who asked.
	Resident bool
}

// feeColumn is the price expression the cost filter compares against — the fee
// this viewer would actually be charged. It mirrors domain.Facility.FeeFor,
// including its rule that a facility with no non-resident fee set charges
// everyone the base fee; the two must agree or the directory filters on one
// number and displays another.
func (f Filter) feeColumn() string {
	if f.Resident {
		return "fee_cents"
	}
	return "CASE WHEN non_resident_fee_cents > 0 THEN non_resident_fee_cents ELSE fee_cents END"
}

// List returns facilities matching the filter, with accessories preloaded.
func (s *Service) List(ctx context.Context, f Filter) ([]domain.Facility, error) {
	q := s.db.WithContext(ctx).Preload("Accessories.Accessory").Model(&domain.Facility{})
	if f.MinCapacity > 0 {
		q = q.Where("capacity >= ?", f.MinCapacity)
	}
	if f.FreeOnly {
		q = q.Where("fee_cents = 0")
	}
	if f.Area != "" {
		q = q.Where("area = ?", f.Area)
	}
	if f.StepFree {
		q = q.Where("step_free_access = ?", true)
	}
	if f.AccessibleWashroom {
		q = q.Where("accessible_washroom = ?", true)
	}
	if f.MinFeeCents > 0 {
		q = q.Where(f.feeColumn()+" >= ?", f.MinFeeCents)
	}
	if f.MaxFeeCents > 0 {
		q = q.Where(f.feeColumn()+" <= ?", f.MaxFeeCents)
	}
	if names := dedupe(f.Accessories); len(names) > 0 {
		// COUNT(DISTINCT a.name) = len(names) is what makes this "all of", not
		// "any of": a facility listing the same accessory twice cannot satisfy
		// two requirements, and one listing only some of them falls short.
		q = q.Where(`id IN (SELECT fa.facility_id FROM facility_accessories fa
			JOIN accessories a ON a.id = fa.accessory_id
			WHERE a.name IN (?)
			GROUP BY fa.facility_id
			HAVING COUNT(DISTINCT a.name) = ?)`, names, len(names))
	}
	var out []domain.Facility
	err := q.Order("name asc").Find(&out).Error
	return out, err
}

// dedupe drops blanks and repeats from a filter's accessory list. A repeat would
// inflate the HAVING count above what any facility could reach, turning a
// duplicated checkbox into an empty result set.
func dedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// FilterOptions is the vocabulary the §4.3 filter panel offers.
//
// Both lists are initialised, never nil: a nil Go slice marshals to `null`, and
// a client mapping over null throws. That exact mistake blanked a page once
// already (FAC-42).
type FilterOptions struct {
	Areas       []string `json:"areas"`
	Accessories []string `json:"accessories"`
}

// FilterOptions lists the areas and accessories worth offering, so the panel
// presents real choices rather than free-text boxes that match only on a lucky
// guess.
//
// Deliberately computed from the whole directory rather than from the current
// result set: options that vanish as you filter leave you unable to undo the
// choice that removed them.
func (s *Service) FilterOptions(ctx context.Context) (FilterOptions, error) {
	out := FilterOptions{Areas: []string{}, Accessories: []string{}}
	// Unset areas contribute no option — an empty entry would filter to nothing.
	if err := s.db.WithContext(ctx).Model(&domain.Facility{}).
		Where("area <> ?", "").
		Distinct().Order("area asc").
		Pluck("area", &out.Areas).Error; err != nil {
		return out, err
	}
	// Only accessories some facility actually offers; the rest would be dead
	// checkboxes that always return nothing.
	err := s.db.WithContext(ctx).Model(&domain.Accessory{}).
		Where("id IN (SELECT accessory_id FROM facility_accessories)").
		Distinct().Order("name asc").
		Pluck("name", &out.Accessories).Error
	return out, err
}

// Get loads one facility.
func (s *Service) Get(ctx context.Context, id string) (*domain.Facility, error) {
	var f domain.Facility
	if err := s.db.WithContext(ctx).Preload("Accessories.Accessory").First(&f, "id = ?", id).Error; err != nil {
		return nil, ErrNotFound
	}
	return &f, nil
}

// Create inserts a new facility (staff).
func (s *Service) Create(ctx context.Context, f *domain.Facility) error {
	return s.db.WithContext(ctx).Create(f).Error
}

// Update saves editable fields on a facility (staff).
func (s *Service) Update(ctx context.Context, id string, in *domain.Facility) (*domain.Facility, error) {
	f, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	in.ID = f.ID
	if err := s.db.WithContext(ctx).Model(f).Omit("Accessories").Save(in).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Delete soft-deletes a facility (staff).
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&domain.Facility{}, "id = ?", id).Error
}

// --- blackouts (maintenance / closures) ------------------------------------

// ErrBadRange is returned when a blackout's end is not after its start.
var ErrBadRange = errors.New("facility: end must be after start")

// AddBlackout marks a facility unavailable for [start, end) (staff). Once saved,
// availability and booking both exclude the range via availability.Check.
func (s *Service) AddBlackout(ctx context.Context, facilityID string, start, end time.Time, reason string) (*domain.Blackout, error) {
	if _, err := s.Get(ctx, facilityID); err != nil {
		return nil, err
	}
	if !end.After(start) {
		return nil, ErrBadRange
	}
	b := domain.Blackout{FacilityID: facilityID, StartsAt: start, EndsAt: end, Reason: reason}
	if err := s.db.WithContext(ctx).Create(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// ListBlackouts returns a facility's blackout ranges, soonest first.
func (s *Service) ListBlackouts(ctx context.Context, facilityID string) ([]domain.Blackout, error) {
	var out []domain.Blackout
	err := s.db.WithContext(ctx).Where("facility_id = ?", facilityID).Order("starts_at asc").Find(&out).Error
	return out, err
}

// RemoveBlackout deletes a blackout by id (staff).
func (s *Service) RemoveBlackout(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&domain.Blackout{}, "id = ?", id).Error
}

// Slot is a candidate booking window on a given day and whether it is free.
type Slot struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Available bool      `json:"available"`
}

// Search returns facilities matching the filter that are also free for the whole
// [from, to] window. With a zero window it degrades to List (parameter search
// only). The capacity/accessory filter and the availability check are ANDed, per
// the §4.4 acceptance criteria.
func (s *Service) Search(ctx context.Context, f Filter, from, to time.Time) ([]domain.Facility, error) {
	facilities, err := s.List(ctx, f)
	if err != nil || from.IsZero() || to.IsZero() {
		return facilities, err
	}
	var out []domain.Facility
	for _, fac := range facilities {
		free, err := s.windowFree(ctx, fac, from, to)
		if err != nil {
			return nil, err
		}
		if free {
			out = append(out, fac)
		}
	}
	return out, nil
}

// windowFree reports whether a facility is bookable for exactly [from, to].
func (s *Service) windowFree(ctx context.Context, f domain.Facility, from, to time.Time) (bool, error) {
	rules, blackouts, bookings, err := s.loadWindow(ctx, f.ID, from, to)
	if err != nil {
		return false, err
	}
	reason := availability.Check(availability.Input{
		Facility: f, Rules: rules, Blackouts: blackouts, Bookings: bookings, Start: from, End: to,
	})
	return reason == availability.OK, nil
}

// DayAvailability returns hourly slots for a facility on the given date,
// each marked available or not per the booking rules and existing bookings.
func (s *Service) DayAvailability(ctx context.Context, id string, day time.Time) ([]Slot, error) {
	f, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.Add(24 * time.Hour)
	rules, blackouts, bookings, err := s.loadWindow(ctx, id, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}
	return buildSlots(*f, rules, blackouts, bookings, dayStart), nil
}

// loadWindow fetches a facility's rules, plus the blackouts and active bookings
// that could affect [from, to]. Bookings are scanned a day wider so buffer
// padding around the window is always covered; availability.Check does the exact
// buffer math.
func (s *Service) loadWindow(ctx context.Context, id string, from, to time.Time) ([]domain.AvailabilityRule, []domain.Blackout, []domain.Booking, error) {
	var rules []domain.AvailabilityRule
	if err := s.db.WithContext(ctx).Where("facility_id = ?", id).Find(&rules).Error; err != nil {
		return nil, nil, nil, err
	}
	var blackouts []domain.Blackout
	if err := s.db.WithContext(ctx).Where("facility_id = ? AND starts_at < ? AND ends_at > ?", id, to, from).Find(&blackouts).Error; err != nil {
		return nil, nil, nil, err
	}
	// Bookings come from the whole conflict set — the space itself plus its
	// ancestors and descendants — so what the calendar shows as taken matches
	// what the booking path will refuse. Showing a sub-space as open because only
	// its parent hall is booked would offer a slot that then fails on submit.
	conflicting, err := ConflictSet(s.db.WithContext(ctx), id)
	if err != nil {
		return nil, nil, nil, err
	}
	var bookings []domain.Booking
	if err := s.db.WithContext(ctx).Where("facility_id IN ? AND status IN ? AND ends_at > ? AND starts_at < ?",
		conflicting, []domain.BookingStatus{domain.StatusPending, domain.StatusConfirmed}, from.Add(-24*time.Hour), to.Add(24*time.Hour)).Find(&bookings).Error; err != nil {
		return nil, nil, nil, err
	}
	return rules, blackouts, bookings, nil
}

// --- weekly availability calendar (§4.2) -----------------------------------

// CalendarSlot is one time block with its booking status.
type CalendarSlot struct {
	Start  time.Time `json:"start"`
	Status string    `json:"status"` // open | booked | blackout | closed
}

// CalendarDay is one column of the calendar grid.
type CalendarDay struct {
	Date    string         `json:"date"`  // YYYY-MM-DD
	Label   string         `json:"label"` // e.g. "Mon 21"
	IsToday bool           `json:"isToday"`
	Slots   []CalendarSlot `json:"slots"`
}

// Calendar is a rectangular availability grid: one CalendarDay per day, each with
// the same time-aligned slots, so the UI renders rows (times) × columns (days).
type Calendar struct {
	FacilityID    string        `json:"facilityId"`
	FacilityName  string        `json:"facilityName"`
	From          string        `json:"from"`
	SlotMinutes   int           `json:"slotMinutes"`
	OpenMinute    int           `json:"openMinute"`
	CloseMinute   int           `json:"closeMinute"`
	MinMinutes    int           `json:"minMinutes"`
	BufferMinutes int           `json:"bufferMinutes"`
	Days          []CalendarDay `json:"days"`
}

const calendarSlotMinutes = 120

// Calendar builds a `days`-long availability grid for a facility starting at
// `from`. Each slot is open, booked (an active booking overlaps), blackout, or
// closed (outside opening hours). Public — no auth needed.
func (s *Service) Calendar(ctx context.Context, id string, from time.Time, days int) (*Calendar, error) {
	f, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if days < 1 {
		days = 7
	}
	if days > 42 {
		days = 42
	}
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	to := from.AddDate(0, 0, days)

	rules, blackouts, bookings, err := s.loadWindow(ctx, id, from, to)
	if err != nil {
		return nil, err
	}

	// The grid spans the widest opening window across the week's weekday rules.
	byWeekday := map[int]domain.AvailabilityRule{}
	openMin, closeMin := 24*60, 0
	for _, r := range rules {
		byWeekday[r.Weekday] = r
		if r.OpenMinute < openMin {
			openMin = r.OpenMinute
		}
		if r.CloseMinute > closeMin {
			closeMin = r.CloseMinute
		}
	}
	if closeMin <= openMin { // no rules loaded → sensible default
		openMin, closeMin = 8*60, 22*60
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	cal := &Calendar{
		FacilityID: f.ID, FacilityName: f.Name, From: from.Format("2006-01-02"),
		SlotMinutes: calendarSlotMinutes, OpenMinute: openMin, CloseMinute: closeMin,
		MinMinutes: f.MinMinutes, BufferMinutes: f.BufferMinutes,
	}
	for i := 0; i < days; i++ {
		day := from.AddDate(0, 0, i)
		rule, open := byWeekday[int(day.Weekday())]
		cd := CalendarDay{Date: day.Format("2006-01-02"), Label: day.Format("Mon 2"), IsToday: day.Format("2006-01-02") == today}
		for m := openMin; m+calendarSlotMinutes <= closeMin; m += calendarSlotMinutes {
			slotStart := day.Add(time.Duration(m) * time.Minute)
			slotEnd := slotStart.Add(calendarSlotMinutes * time.Minute)
			cd.Slots = append(cd.Slots, CalendarSlot{Start: slotStart, Status: slotStatus(open, rule, m, slotStart, slotEnd, blackouts, bookings)})
		}
		cal.Days = append(cal.Days, cd)
	}
	return cal, nil
}

func slotStatus(dayOpen bool, rule domain.AvailabilityRule, minute int, start, end time.Time, blackouts []domain.Blackout, bookings []domain.Booking) string {
	if !dayOpen || minute < rule.OpenMinute || minute+calendarSlotMinutes > rule.CloseMinute {
		return "closed"
	}
	for _, b := range blackouts {
		if start.Before(b.EndsAt) && b.StartsAt.Before(end) {
			return "blackout"
		}
	}
	for _, bk := range bookings {
		if bk.Active() && start.Before(bk.EndsAt) && bk.StartsAt.Before(end) {
			return "booked"
		}
	}
	return "open"
}

// buildSlots walks the day's opening hours in one-hour steps, checking each.
func buildSlots(f domain.Facility, rules []domain.AvailabilityRule, blackouts []domain.Blackout, bookings []domain.Booking, dayStart time.Time) []Slot {
	weekday := int(dayStart.Weekday())
	var open, close int
	for _, r := range rules {
		if r.Weekday == weekday {
			open, close = r.OpenMinute, r.CloseMinute
		}
	}
	var slots []Slot
	for m := open; m+60 <= close; m += 60 {
		start := dayStart.Add(time.Duration(m) * time.Minute)
		end := start.Add(time.Hour)
		reason := availability.Check(availability.Input{
			Facility: f, Rules: rules, Blackouts: blackouts, Bookings: bookings, Start: start, End: end,
		})
		slots = append(slots, Slot{Start: start, End: end, Available: reason == availability.OK})
	}
	return slots
}
