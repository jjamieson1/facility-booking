// Package servicecard builds the payload C2 (TrustIdentity) fetches for the
// Rivermont Spaces service card. C2 makes a server-to-server GET, authenticated
// by a short-lived JWT, and renders the JSON we return — a per-citizen summary of
// their upcoming bookings. See ServiceCardCallback.md for the wire contract.
package servicecard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/booking"
	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/waitlist"
)

// Payload is the JSON contract C2 renders on the card. Every field is optional;
// C2 falls back to the admin-configured card content for anything omitted. Note
// the deliberate `CTA` capitalization required by the spec.
type Payload struct {
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	CTA         string   `json:"CTA,omitempty"`
	Contact     *Contact `json:"contact,omitempty"`
	Tasks       []Task   `json:"tasks,omitempty"`
}

// Contact is the optional "contact us" block.
type Contact struct {
	Address1   string `json:"address1,omitempty"`
	Address2   string `json:"address2,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
}

// Task is one action button on the card.
type Task struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// maxTasks caps how many bookings become task buttons on the card.
const maxTasks = 5

// Service builds callout payloads from the caller's local data.
type Service struct {
	db       *gorm.DB
	bookings *booking.Service
	waitlist *waitlist.Service
	appURL   string // public SPA base, e.g. https://facility-booking.celestialtech.ca/facility-booking
	contact  Contact
}

// NewService wires the payload builder.
func NewService(db *gorm.DB, bookings *booking.Service, wl *waitlist.Service, appURL string, c config.ServiceCardContact) *Service {
	return &Service{
		db:       db,
		bookings: bookings,
		waitlist: wl,
		appURL:   appURL,
		contact: Contact{
			Address1: c.Address1, City: c.City, State: c.State,
			PostalCode: c.PostalCode, Email: c.Email, Phone: c.Phone,
		},
	}
}

// StatusForSubject builds the card payload for a citizen identified by their C2
// subject. The bool is false when no local user maps to that subject (the caller
// should then return an empty-but-valid 200 per the spec). It never returns a
// user's data for a different subject.
func (s *Service) StatusForSubject(ctx context.Context, subject string) (*Payload, bool, error) {
	if subject == "" {
		return nil, false, errors.New("servicecard: empty subject")
	}
	var u domain.User
	err := s.db.WithContext(ctx).Where("subject = ?", subject).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	upcoming, err := s.bookings.UpcomingForUser(ctx, u.ID)
	if err != nil {
		return nil, false, err
	}
	// All still-active waitlist entries (notified_at IS NULL) — same set the app's
	// "My bookings" shows. We don't time-filter these: an un-notified entry means
	// the citizen is still waiting, so the card should reflect that as the app does.
	waiting, err := s.waitlist.ListForUser(ctx, u.ID)
	if err != nil {
		return nil, false, err
	}
	return s.build(upcoming, waiting), true, nil
}

// build assembles the payload from a citizen's upcoming bookings and waitlisted
// slots. Task order: bookings (soonest first), then waitlisted slots, then the
// always-present Browse Facilities link.
func (s *Service) build(upcoming []domain.Booking, waiting []domain.WaitlistEntry) *Payload {
	p := &Payload{
		Title:       "Your Rivermont bookings",
		Description: describe(upcoming, waiting),
		CTA:         s.appURL + "/my-bookings",
		Contact:     &s.contact,
		Tasks:       []Task{},
	}

	for i, b := range upcoming {
		if i >= maxTasks {
			break
		}
		p.Tasks = append(p.Tasks, Task{
			Name:        fmt.Sprintf("%s — %s", facilityName(b.Facility), whenOf(b.StartsAt)),
			Description: statusLabel(b.Status),
			URL:         fmt.Sprintf("%s/bookings/%s", s.appURL, b.ID),
		})
	}
	for i, e := range waiting {
		if i >= maxTasks {
			break
		}
		p.Tasks = append(p.Tasks, Task{
			Name:        fmt.Sprintf("%s — %s", facilityName(e.Facility), whenOf(e.StartsAt)),
			Description: "On the waitlist — we'll notify you if it frees up",
			URL:         fmt.Sprintf("%s/facilities/%s", s.appURL, e.FacilityID),
		})
	}
	// Always end with the directory link so the citizen can book another space.
	p.Tasks = append(p.Tasks, s.browseTask())
	return p
}

// describe is the card's summary line, covering upcoming bookings and any
// waitlisted slots.
func describe(upcoming []domain.Booking, waiting []domain.WaitlistEntry) string {
	if len(upcoming) == 0 && len(waiting) == 0 {
		return "You have no upcoming bookings. Browse Rivermont's facilities to reserve a space."
	}
	sentence := bookingSentence(upcoming)
	if w := waitSentence(waiting); w != "" {
		sentence += " " + w
	}
	return sentence
}

func bookingSentence(upcoming []domain.Booking) string {
	switch len(upcoming) {
	case 0:
		return "You have no upcoming bookings."
	case 1:
		return fmt.Sprintf("You have 1 upcoming booking: %s on %s.", facilityName(upcoming[0].Facility), whenOf(upcoming[0].StartsAt))
	default:
		return fmt.Sprintf("You have %d upcoming bookings. Next: %s on %s.", len(upcoming), facilityName(upcoming[0].Facility), whenOf(upcoming[0].StartsAt))
	}
}

func waitSentence(waiting []domain.WaitlistEntry) string {
	switch n := len(waiting); {
	case n == 1:
		return "You're on the waitlist for 1 slot."
	case n > 1:
		return fmt.Sprintf("You're on the waitlist for %d slots.", n)
	default:
		return ""
	}
}

// browseTask is the default action, always present on the card: a link to the
// main facilities directory page.
func (s *Service) browseTask() Task {
	return Task{
		Name:        "Browse Facilities",
		Description: "Browse Rivermont's facilities, or find one free at a specific time.",
		URL:         s.appURL + "/",
	}
}

func facilityName(f *domain.Facility) string {
	if f != nil && f.Name != "" {
		return f.Name
	}
	return "Facility"
}

// whenOf renders a friendly local date/time, e.g. "Aug 2, 2:00 PM".
func whenOf(t time.Time) string {
	return t.Local().Format("Jan 2, 3:04 PM")
}

func statusLabel(s domain.BookingStatus) string {
	if s == domain.StatusPending {
		return "Awaiting approval"
	}
	return "Confirmed"
}
