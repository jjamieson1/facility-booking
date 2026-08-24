package servicecard

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/booking"
	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/notify"
	"github.com/jjamieson1/facility-booking/internal/testdb"
	"github.com/jjamieson1/facility-booking/internal/waitlist"
)

const appURL = "https://app.example/base"

func newSvc(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := testdb.New(t)
	contact := config.ServiceCardContact{Email: "facilities@rivermont.ca", Phone: "+1 555-0142", City: "Rivermont"}
	wl := waitlist.NewService(db, notify.NewLogNotifier())
	return NewService(db, booking.NewService(db, nil), wl, appURL, contact), db
}

func mkWaitlist(t *testing.T, db *gorm.DB, userID, facID string, start time.Time) {
	t.Helper()
	e := domain.WaitlistEntry{
		Base: domain.Base{ID: uuid.NewString()}, UserID: userID, FacilityID: facID,
		StartsAt: start, EndsAt: start.Add(time.Hour),
	}
	if err := db.Create(&e).Error; err != nil {
		t.Fatal(err)
	}
}

func mkUser(t *testing.T, db *gorm.DB, sub string) domain.User {
	t.Helper()
	u := domain.User{Subject: sub, Email: sub + "@x.com", Name: "Pat Citizen"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func mkBooking(t *testing.T, db *gorm.DB, userID, facID string, start time.Time, status domain.BookingStatus) {
	t.Helper()
	b := domain.Booking{
		Base: domain.Base{ID: uuid.NewString()}, UserID: userID, FacilityID: facID,
		StartsAt: start, EndsAt: start.Add(time.Hour), Status: status,
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
}

func TestStatusForSubjectBuildsPayload(t *testing.T) {
	svc, db := newSvc(t)
	u := mkUser(t, db, "sub-1")
	fac := domain.Facility{Base: domain.Base{ID: uuid.NewString()}, Name: "Community Hall"}
	db.Create(&fac)
	now := time.Now()

	mkBooking(t, db, u.ID, fac.ID, now.Add(48*time.Hour), domain.StatusConfirmed)
	mkBooking(t, db, u.ID, fac.ID, now.Add(72*time.Hour), domain.StatusPending)
	mkBooking(t, db, u.ID, fac.ID, now.Add(-48*time.Hour), domain.StatusConfirmed) // past → excluded
	mkBooking(t, db, u.ID, fac.ID, now.Add(96*time.Hour), domain.StatusCancelled)  // cancelled → excluded
	// Active waitlist entries show regardless of slot time (matching the app) — a
	// past-slot entry must still appear. Only a notified entry is excluded.
	mkWaitlist(t, db, u.ID, fac.ID, now.Add(120*time.Hour)) // future active → included
	mkWaitlist(t, db, u.ID, fac.ID, now.Add(-24*time.Hour)) // past active → included (regression)
	notified := now
	if err := db.Create(&domain.WaitlistEntry{
		Base: domain.Base{ID: uuid.NewString()}, UserID: u.ID, FacilityID: fac.ID,
		StartsAt: now.Add(6 * time.Hour), EndsAt: now.Add(7 * time.Hour), NotifiedAt: &notified,
	}).Error; err != nil {
		t.Fatal(err)
	}

	p, found, err := svc.StatusForSubject(context.Background(), "sub-1")
	if err != nil || !found {
		t.Fatalf("StatusForSubject: found=%v err=%v", found, err)
	}
	if p.Title == "" || !strings.Contains(p.Description, "2 upcoming") {
		t.Errorf("description = %q, want it to mention 2 upcoming", p.Description)
	}
	if !strings.Contains(p.Description, "waitlist for 2 slots") {
		t.Errorf("description = %q, want it to mention 2 waitlisted slots", p.Description)
	}
	// 2 booking tasks + 2 active waitlist tasks (notified excluded) + the
	// always-present Browse Facilities task at the end.
	if len(p.Tasks) != 5 {
		t.Fatalf("tasks = %d, want 5 (2 bookings + 2 waitlist + Browse Facilities)", len(p.Tasks))
	}
	// The waitlist task sits after the bookings and links to the facility page.
	wlTask := p.Tasks[2]
	if !strings.HasPrefix(wlTask.URL, appURL+"/facilities/") || !strings.Contains(wlTask.Description, "waitlist") {
		t.Errorf("waitlist task = %+v, want a facility link + waitlist description", wlTask)
	}
	// Booking tasks are soonest-first and link back into the SPA.
	if !strings.HasPrefix(p.Tasks[0].URL, appURL+"/bookings/") {
		t.Errorf("task url = %q, want it under %s/bookings/", p.Tasks[0].URL, appURL)
	}
	if p.Tasks[0].Description != "Confirmed" || p.Tasks[1].Description != "Awaiting approval" {
		t.Errorf("task statuses = %q/%q", p.Tasks[0].Description, p.Tasks[1].Description)
	}
	assertBrowseTask(t, p.Tasks[len(p.Tasks)-1])
	if p.Contact == nil || p.Contact.Email != "facilities@rivermont.ca" {
		t.Errorf("contact = %+v, want the configured contact", p.Contact)
	}
	if p.CTA != appURL+"/my-bookings" {
		t.Errorf("CTA = %q", p.CTA)
	}
}

func TestStatusForSubjectNoBookings(t *testing.T) {
	svc, db := newSvc(t)
	mkUser(t, db, "sub-2")
	p, found, err := svc.StatusForSubject(context.Background(), "sub-2")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if !strings.Contains(p.Description, "no upcoming") {
		t.Errorf("description = %q, want a no-bookings message", p.Description)
	}
	if len(p.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1 (just Browse Facilities)", len(p.Tasks))
	}
	assertBrowseTask(t, p.Tasks[0])
}

// assertBrowseTask checks the always-present default directory task.
func assertBrowseTask(t *testing.T, task Task) {
	t.Helper()
	if task.Name != "Browse Facilities" {
		t.Errorf("browse task name = %q, want %q", task.Name, "Browse Facilities")
	}
	if task.Description != "Browse Rivermont's facilities, or find one free at a specific time." {
		t.Errorf("browse task description = %q", task.Description)
	}
	if task.URL != appURL+"/" {
		t.Errorf("browse task url = %q, want %q", task.URL, appURL+"/")
	}
}

func TestStatusWaitlistOnly(t *testing.T) {
	svc, db := newSvc(t)
	u := mkUser(t, db, "sub-3")
	fac := domain.Facility{Base: domain.Base{ID: uuid.NewString()}, Name: "Ice Arena"}
	db.Create(&fac)
	mkWaitlist(t, db, u.ID, fac.ID, time.Now().Add(24*time.Hour))

	p, found, err := svc.StatusForSubject(context.Background(), "sub-3")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if !strings.Contains(p.Description, "no upcoming bookings") || !strings.Contains(p.Description, "waitlist for 1 slot") {
		t.Errorf("description = %q, want no-bookings + waitlist", p.Description)
	}
	// 1 waitlist task + Browse Facilities.
	if len(p.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2 (waitlist + Browse Facilities)", len(p.Tasks))
	}
	assertBrowseTask(t, p.Tasks[len(p.Tasks)-1])
}

func TestStatusForSubjectUnknown(t *testing.T) {
	svc, _ := newSvc(t)
	p, found, err := svc.StatusForSubject(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if found || p != nil {
		t.Errorf("unknown subject: found=%v p=%v, want false/nil", found, p)
	}
}
