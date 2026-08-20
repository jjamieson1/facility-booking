// Package notify sends booking notifications. For the demo it logs the message
// (and would attach the .ics invite) instead of sending real email; a real
// mailer implements the same Notifier interface.
package notify

import (
	"log"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Notifier delivers a booking notification to the booker and/or staff.
type Notifier interface {
	BookingSubmitted(b domain.Booking)
	BookingConfirmed(b domain.Booking, ics string)
	BookingDenied(b domain.Booking)
	BookingCancelled(b domain.Booking, ics string)
	// BookingReminder nudges the booker before the date with the before-use
	// instructions and access details.
	BookingReminder(b domain.Booking, instructions string)
	// WaitlistOpened tells a waitlisted resident that a slot they wanted is free.
	WaitlistOpened(e domain.WaitlistEntry, facilityName string)
}

// LogNotifier writes notifications to the server log — visible proof in the demo
// that the right message fires at each step.
type LogNotifier struct{}

// NewLogNotifier returns the demo notifier.
func NewLogNotifier() *LogNotifier { return &LogNotifier{} }

func (LogNotifier) BookingSubmitted(b domain.Booking) {
	log.Printf("notify: booking %s submitted → staff to review", b.ID)
}

func (LogNotifier) BookingConfirmed(b domain.Booking, _ string) {
	log.Printf("notify: booking %s confirmed → invite (.ics) sent to booker", b.ID)
}

func (LogNotifier) BookingDenied(b domain.Booking) {
	log.Printf("notify: booking %s denied → booker notified", b.ID)
}

func (LogNotifier) BookingCancelled(b domain.Booking, _ string) {
	log.Printf("notify: booking %s cancelled → invite withdrawn", b.ID)
}

func (LogNotifier) BookingReminder(b domain.Booking, instructions string) {
	log.Printf("notify: reminder for booking %s (%s) → before-use instructions: %q", b.ID, b.StartsAt.Format("Jan 2 15:04"), instructions)
}

func (LogNotifier) WaitlistOpened(e domain.WaitlistEntry, facilityName string) {
	log.Printf("notify: waitlist slot opened for user %s → %s on %s is now free", e.UserID, facilityName, e.StartsAt.Format("Jan 2 15:04"))
}
