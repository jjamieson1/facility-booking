package calendar

import (
	"strings"
	"testing"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

func sampleBooking() domain.Booking {
	start := time.Date(2026, 8, 1, 14, 0, 0, 0, time.UTC)
	return domain.Booking{
		Base:     domain.Base{ID: "bk-1"},
		Status:   domain.StatusConfirmed,
		StartsAt: start,
		EndsAt:   start.Add(time.Hour),
		Purpose:  "Private family memorial",
		Facility: &domain.Facility{Name: "Willow Park Pavilion", Location: "Riverside Ave"},
	}
}

// The public city feed must not leak the resident-entered purpose, but must still
// carry the space, time, and status.
func TestFeedOmitsPurpose(t *testing.T) {
	out := Feed([]domain.Booking{sampleBooking()})
	if strings.Contains(out, "DESCRIPTION") || strings.Contains(out, "Private family memorial") {
		t.Errorf("public feed leaked the booking purpose:\n%s", out)
	}
	if !strings.Contains(out, "SUMMARY:Willow Park Pavilion") {
		t.Error("feed should still include the facility name")
	}
	if !strings.Contains(out, "DTSTART:20260801T140000Z") {
		t.Error("feed should still include the start time")
	}
}

// The booker's own invite is authenticated and may carry the purpose.
func TestInviteIncludesPurpose(t *testing.T) {
	out := Invite(sampleBooking())
	if !strings.Contains(out, "DESCRIPTION:Private family memorial") {
		t.Errorf("invite should include the purpose for the booker:\n%s", out)
	}
}
