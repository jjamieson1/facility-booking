package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/c2"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// maxStaffFanout caps how many staff a single "needs approval" event notifies.
// C2 takes one recipient per call, so an event costs one request per staff
// member; a municipality that promotes thirty people should not turn every
// booking request into thirty calls. Per-facility approver routing (FAC-27)
// replaces this with the *right* recipients rather than all of them.
const maxStaffFanout = 10

// C2Notifier delivers notifications through C2's partner API: C2 creates the
// citizen's in-app notification and fans out to email/SMS on the channels they
// have opted into (§4.10). The citizen's preferences decide the channels — this
// app sends one message and does not choose how it travels.
//
// Delivery is best-effort by design. A notification that fails must never roll
// back the booking it describes: the booking is the record, the message is a
// courtesy, and a resident with a confirmed booking and no email is in a far
// better position than one whose booking was refused because email was down.
type C2Notifier struct {
	db       *gorm.DB
	client   *c2.Client
	fallback Notifier // logs, so a failed or unconfigured send is still visible
}

// translateFacility renames a booking's facility into the recipient's language.
//
// Without this the message text is French but the facility is still called
// "Rivermont Community Hall" — which is the exact half-translated result FAC-12
// exists to avoid. Read directly rather than through the facility service to
// keep the notifier free of that dependency; it is one indexed lookup.
func (n *C2Notifier) translateFacility(b domain.Booking, l string) domain.Booking {
	if n.db == nil || b.Facility == nil || domain.NormalizeLanguage(l) == domain.DefaultLanguage {
		return b
	}
	var rows []domain.FacilityTranslation
	if err := n.db.Limit(1).Find(&rows, "facility_id = ? AND language = ?",
		b.FacilityID, domain.NormalizeLanguage(l)).Error; err != nil || len(rows) == 0 {
		return b
	}
	// Copy the facility so the caller's booking — which may be shared with other
	// recipients in a different language — is untouched.
	fac := *b.Facility
	fac.ApplyTranslation(rows[0])
	b.Facility = &fac
	return b
}

// NewC2Notifier wires the notifier to C2's partner API.
func NewC2Notifier(db *gorm.DB, client *c2.Client) *C2Notifier {
	return &C2Notifier{db: db, client: client, fallback: NewLogNotifier()}
}

func (n *C2Notifier) BookingSubmitted(b domain.Booking) {
	// The booker learns it is pending; staff learn there is something to review.
	n.toBooker(b, func(b domain.Booking, l string) message { return bookingSubmitted(b, l) })
	n.toStaff(b)
	n.fallback.BookingSubmitted(b)
}

func (n *C2Notifier) BookingConfirmed(b domain.Booking, _ string) {
	// The .ics is a link, not an attachment: C2's notification API carries text
	// only. It points at this app's authenticated invite endpoint, which is
	// right for a document naming someone's booking.
	invite := n.inviteURL(b.ID)
	n.toBooker(b, func(b domain.Booking, l string) message { return bookingConfirmed(b, l, invite) })
	n.fallback.BookingConfirmed(b, "")
}

func (n *C2Notifier) BookingDenied(b domain.Booking) {
	n.toBooker(b, func(b domain.Booking, l string) message { return bookingDenied(b, l) })
	n.fallback.BookingDenied(b)
}

func (n *C2Notifier) BookingConditional(b domain.Booking) {
	n.toBooker(b, func(b domain.Booking, l string) message { return bookingConditional(b, l) })
	n.fallback.BookingConditional(b)
}

func (n *C2Notifier) BookingCancelled(b domain.Booking, _ string) {
	n.toBooker(b, func(b domain.Booking, l string) message { return bookingCancelled(b, l) })
	n.fallback.BookingCancelled(b, "")
}

func (n *C2Notifier) BookingReminder(b domain.Booking, instructions string) {
	n.toBooker(b, func(b domain.Booking, l string) message { return bookingReminder(b, l, instructions) })
	n.fallback.BookingReminder(b, instructions)
}

func (n *C2Notifier) WaitlistOpened(e domain.WaitlistEntry, facilityName string) {
	u, ok := n.user(e.UserID)
	if !ok {
		return
	}
	l := lang(u.Language)
	n.send(u, waitlistOpened(e, facilityName, l, n.client.AppBaseURL()))
	n.fallback.WaitlistOpened(e, facilityName)
}

// toBooker resolves the booking's owner and sends them the message their
// language calls for.
func (n *C2Notifier) toBooker(b domain.Booking, build func(b domain.Booking, l string) message) {
	u, ok := n.user(b.UserID)
	if !ok {
		return
	}
	l := lang(u.Language)
	n.send(u, build(n.translateFacility(b, l), l))
}

// toStaff notifies staff that a request needs review. Until per-facility
// approvers exist (FAC-27), "the responsible staff" is every staff and admin
// account, capped.
func (n *C2Notifier) toStaff(b domain.Booking) {
	if n.db == nil {
		return
	}
	var staff []domain.User
	if err := n.db.Limit(maxStaffFanout).
		Find(&staff, "role IN ?", []domain.Role{domain.RoleStaff, domain.RoleAdmin}).Error; err != nil {
		log.Printf("notify: could not list staff for booking %s: %v", b.ID, err)
		return
	}
	for _, s := range staff {
		l := lang(s.Language)
		n.send(s, staffReviewNeeded(n.translateFacility(b, l), l))
	}
}

// send posts one notification, translating C2's outcomes into the right
// response for each.
func (n *C2Notifier) send(u domain.User, m message) {
	if !n.client.Configured() {
		return // the log fallback already recorded the event
	}
	// A guest booked without a C2 identity, so there is no inbox to deliver to.
	// Not an error: it is the expected shape of a guest booking (FAC-24).
	if u.IsGuest() || u.Subject == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := n.client.PostNotification(ctx, c2.Notification{
		Subject:   u.Subject,
		Title:     m.Title,
		Body:      m.Body,
		ShortBody: m.Short,
		Category:  "BUSINESS",
	})
	switch {
	case err == nil:
	case errors.Is(err, c2.ErrNoConsent):
		// Expected: the citizen has not accepted this service's terms, or has
		// unlinked it. C2 sent nothing and will keep refusing until they
		// re-consent, so retrying is useless and is audited against our client.
		log.Printf("notify: %s not delivered — citizen has not consented", m.Title)
	case errors.Is(err, c2.ErrUnknownSubject):
		log.Printf("notify: %s not delivered — C2 does not recognise the subject", m.Title)
	default:
		// Includes credentials and rate limiting: both are ours to fix, and
		// neither may disturb the booking this message describes.
		log.Printf("notify: %s not delivered: %v", m.Title, err)
	}
}

// user loads a booking's owner. A missing user means no recipient — never a
// failure of the action that triggered the notification.
func (n *C2Notifier) user(id string) (domain.User, bool) {
	if n.db == nil || id == "" {
		return domain.User{}, false
	}
	var users []domain.User
	if err := n.db.Limit(1).Find(&users, "id = ?", id).Error; err != nil || len(users) == 0 {
		return domain.User{}, false
	}
	return users[0], true
}

// inviteURL points at this app's .ics endpoint for a booking.
func (n *C2Notifier) inviteURL(bookingID string) string {
	base := n.client.AppBaseURL()
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/api/bookings/%s/invite.ics", base, bookingID)
}
