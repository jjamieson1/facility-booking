package availability

import (
	"testing"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// A facility open 08:00–22:00 every day, 60–240 min bookings, 30-min buffer.
func testFacility() domain.Facility {
	return domain.Facility{
		Base:          domain.Base{ID: "f1"},
		MinMinutes:    60,
		MaxMinutes:    240,
		BufferMinutes: 30,
	}
}

func dailyHours() []domain.AvailabilityRule {
	rules := make([]domain.AvailabilityRule, 7)
	for i := 0; i < 7; i++ {
		rules[i] = domain.AvailabilityRule{Weekday: i, OpenMinute: 8 * 60, CloseMinute: 22 * 60}
	}
	return rules
}

func at(hour, min int) time.Time {
	// A fixed Wednesday so weekday lookup is stable. Local time, because opening
	// hours are local wall-clock minutes (Check compares in local time).
	return time.Date(2026, 7, 22, hour, min, 0, 0, time.Local)
}

func TestCheck(t *testing.T) {
	confirmed := domain.Booking{
		Base:     domain.Base{ID: "b1"},
		Status:   domain.StatusConfirmed,
		StartsAt: at(12, 0),
		EndsAt:   at(14, 0),
	}

	cases := []struct {
		name  string
		start time.Time
		end   time.Time
		want  Reason
	}{
		{"free morning slot", at(9, 0), at(10, 0), OK},
		{"exact overlap", at(12, 0), at(13, 0), Conflict},
		{"partial overlap", at(13, 30), at(15, 0), Conflict},
		{"within buffer before", at(11, 0), at(12, 0), Conflict}, // ends when booking starts; 30m buffer collides
		{"just outside buffer", at(10, 0), at(11, 30), OK},       // ends exactly 30m before booking start
		{"before opening", at(7, 0), at(8, 30), OutsideHours},
		{"after closing", at(21, 30), at(22, 30), OutsideHours},
		{"too short", at(9, 0), at(9, 30), TooShort},
		{"too long", at(9, 0), at(14, 0), TooLong},
		{"zero window", at(9, 0), at(9, 0), InvalidWindow},
		{"end before start", at(10, 0), at(9, 0), InvalidWindow},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Check(Input{
				Facility: testFacility(),
				Rules:    dailyHours(),
				Bookings: []domain.Booking{confirmed},
				Start:    tc.start,
				End:      tc.end,
			})
			if got != tc.want {
				t.Errorf("Check = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckIgnoresCancelledAndExcluded(t *testing.T) {
	cancelled := domain.Booking{Base: domain.Base{ID: "b1"}, Status: domain.StatusCancelled, StartsAt: at(12, 0), EndsAt: at(14, 0)}
	if got := Check(Input{Facility: testFacility(), Rules: dailyHours(), Bookings: []domain.Booking{cancelled}, Start: at(12, 0), End: at(13, 0)}); got != OK {
		t.Errorf("cancelled booking should not block: got %q", got)
	}

	own := domain.Booking{Base: domain.Base{ID: "b1"}, Status: domain.StatusConfirmed, StartsAt: at(12, 0), EndsAt: at(14, 0)}
	if got := Check(Input{Facility: testFacility(), Rules: dailyHours(), Bookings: []domain.Booking{own}, Start: at(12, 0), End: at(13, 0), ExcludeBookingID: "b1"}); got != OK {
		t.Errorf("excluded booking should not block itself: got %q", got)
	}
}

// A booking window arrives from the SPA as a UTC instant (toISOString), while
// opening hours are local wall-clock. An evening slot the availability view
// offered must still be bookable. Regression for "that time is not available".
func TestCheckAcceptsUTCInstantForLocalOpeningHours(t *testing.T) {
	// Pin a non-UTC server zone (e.g. MDT) so the bug reproduces on any machine,
	// including a UTC CI box.
	orig := time.Local
	time.Local = time.FixedZone("TEST-6", -6*3600)
	defer func() { time.Local = orig }()

	// 7:00–8:00 PM local on the fixed Wednesday, sent as UTC instants.
	start := time.Date(2026, 7, 22, 19, 0, 0, 0, time.Local).UTC()
	end := time.Date(2026, 7, 22, 20, 0, 0, 0, time.Local).UTC()

	got := Check(Input{Facility: testFacility(), Rules: dailyHours(), Start: start, End: end})
	if got != OK {
		t.Fatalf("Check(7pm local sent as UTC) = %q, want OK", got)
	}
}

func TestBlackoutBlocks(t *testing.T) {
	bo := domain.Blackout{StartsAt: at(9, 0), EndsAt: at(17, 0)}
	if got := Check(Input{Facility: testFacility(), Rules: dailyHours(), Blackouts: []domain.Blackout{bo}, Start: at(10, 0), End: at(11, 0)}); got != Blackout {
		t.Errorf("blackout should block: got %q", got)
	}
}
