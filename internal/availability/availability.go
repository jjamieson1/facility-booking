// Package availability computes whether a facility is free for a requested time
// window, honouring opening hours, blackout dates, booking-length rules, and the
// setup/cleanup buffer between bookings.
package availability

import (
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Reason is a machine-readable explanation for why a window was rejected.
type Reason string

const (
	OK            Reason = ""
	OutsideHours  Reason = "outside_opening_hours"
	Blackout      Reason = "blackout"
	TooShort      Reason = "too_short"
	TooLong       Reason = "too_long"
	Conflict      Reason = "slot_taken"
	InvalidWindow Reason = "invalid_window"
)

// Input bundles everything Check needs. Bookings should be the facility's active
// bookings that could overlap the window (the caller loads them).
type Input struct {
	Facility  domain.Facility
	Rules     []domain.AvailabilityRule
	Blackouts []domain.Blackout
	Bookings  []domain.Booking // active (pending/confirmed) bookings to check against
	Start     time.Time
	End       time.Time
	// ExcludeBookingID lets a modification ignore its own existing booking.
	ExcludeBookingID string
}

// Check returns OK when the window is bookable, or the first failing Reason.
func Check(in Input) Reason {
	if !in.End.After(in.Start) {
		return InvalidWindow
	}

	minutes := int(in.End.Sub(in.Start).Minutes())
	if in.Facility.MinMinutes > 0 && minutes < in.Facility.MinMinutes {
		return TooShort
	}
	if in.Facility.MaxMinutes > 0 && minutes > in.Facility.MaxMinutes {
		return TooLong
	}
	if !withinOpeningHours(in.Rules, in.Start, in.End) {
		return OutsideHours
	}
	if overlapsBlackout(in.Blackouts, in.Start, in.End) {
		return Blackout
	}
	if overlapsBooking(in.Bookings, in.Start, in.End, in.Facility.BufferMinutes, in.ExcludeBookingID) {
		return Conflict
	}
	return OK
}

// withinOpeningHours requires the whole window to fall inside a single day's
// open period. A window spanning midnight is rejected (kept simple for v1).
//
// Opening hours are wall-clock minutes in the facility's local timezone, so the
// window is compared in local time. Booking requests arrive as UTC instants (the
// SPA sends toISOString()); without converting, an evening slot's UTC hour — and
// even its weekday — fall outside the local open window, rejecting a slot the
// availability view (which is computed in local time) offered as free.
func withinOpeningHours(rules []domain.AvailabilityRule, start, end time.Time) bool {
	start = start.Local()
	end = end.Local()
	if start.YearDay() != end.YearDay() || start.Year() != end.Year() {
		return false
	}
	startMin := start.Hour()*60 + start.Minute()
	endMin := end.Hour()*60 + end.Minute()
	weekday := int(start.Weekday())
	for _, r := range rules {
		if r.Weekday == weekday && startMin >= r.OpenMinute && endMin <= r.CloseMinute {
			return true
		}
	}
	return false
}

func overlapsBlackout(blackouts []domain.Blackout, start, end time.Time) bool {
	for _, b := range blackouts {
		if start.Before(b.EndsAt) && b.StartsAt.Before(end) {
			return true
		}
	}
	return false
}

// overlapsBooking reports whether the window collides with an existing active
// booking once each side is padded by the facility's buffer minutes.
func overlapsBooking(bookings []domain.Booking, start, end time.Time, bufferMin int, excludeID string) bool {
	buffer := time.Duration(bufferMin) * time.Minute
	for _, b := range bookings {
		if b.ID == excludeID || !b.Active() {
			continue
		}
		bStart := b.StartsAt.Add(-buffer)
		bEnd := b.EndsAt.Add(buffer)
		if start.Before(bEnd) && bStart.Before(end) {
			return true
		}
	}
	return false
}
