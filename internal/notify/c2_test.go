package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/c2"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// capture records what C2 would have received.
type capture struct {
	mu     sync.Mutex
	sent   []c2.Notification
	status int
}

func (c *capture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var n c2.Notification
		_ = json.NewDecoder(r.Body).Decode(&n)
		c.mu.Lock()
		c.sent = append(c.sent, n)
		status := c.status
		c.mu.Unlock()
		if status == 0 {
			status = http.StatusAccepted
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"notificationId":"n1","channels":["EMAIL"]}`))
	}
}

func (c *capture) all() []c2.Notification {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]c2.Notification(nil), c.sent...)
}

func newNotifier(t *testing.T) (*C2Notifier, *capture, *gorm.DB) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	db := testdb.New(t)
	client := c2.New(c2.Config{Origin: srv.URL, ClientID: "id", Secret: "s", AppBaseURL: "https://app.example"})
	return NewC2Notifier(db, client), cap, db
}

func mkBooking(t *testing.T, db *gorm.DB, u domain.User) domain.Booking {
	t.Helper()
	f := domain.Facility{Name: "Rivermont Hall", BeforeInstructions: "Keys at the desk."}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	b := domain.Booking{
		FacilityID: f.ID, UserID: u.ID, Facility: &f,
		StartsAt: time.Now().Add(72 * time.Hour), EndsAt: time.Now().Add(73 * time.Hour),
		Status: domain.StatusConfirmed,
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func mkUser(t *testing.T, db *gorm.DB, role domain.Role, language string) domain.User {
	t.Helper()
	// Unique per call: two residents in one test would otherwise collide on the
	// users.subject unique index.
	u := domain.User{Subject: "sub-" + uuid.NewString(), Email: "u@example.com", Role: role, Language: language}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// The booker is notified in the language they chose — the whole reason the
// preference is stored server-side.
func TestNotificationUsesTheRecipientsLanguage(t *testing.T) {
	for _, tc := range []struct{ language, wantWord string }{
		{"en", "confirmed"},
		{"fr", "confirmée"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			n, cap, db := newNotifier(t)
			u := mkUser(t, db, domain.RoleResident, tc.language)
			n.BookingConfirmed(mkBooking(t, db, u), "")

			sent := cap.all()
			if len(sent) != 1 {
				t.Fatalf("sent %d notifications, want 1", len(sent))
			}
			if !strings.Contains(strings.ToLower(sent[0].Title+sent[0].Body), tc.wantWord) {
				t.Errorf("message not in %s: %q / %q", tc.language, sent[0].Title, sent[0].Body)
			}
			if sent[0].Subject != u.Subject {
				t.Errorf("sent to %q, want the citizen's OIDC sub", sent[0].Subject)
			}
		})
	}
}

// C2 carries no attachments, so the invite must travel as a link.
func TestConfirmationCarriesTheInviteAsALink(t *testing.T) {
	n, cap, db := newNotifier(t)
	u := mkUser(t, db, domain.RoleResident, "en")
	b := mkBooking(t, db, u)
	n.BookingConfirmed(b, "BEGIN:VCALENDAR…")

	sent := cap.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].Body, "/api/bookings/"+b.ID+"/invite.ics") {
		t.Errorf("body has no invite link: %q", sent[0].Body)
	}
	// The raw calendar data must not be pasted into the message body.
	if strings.Contains(sent[0].Body, "BEGIN:VCALENDAR") {
		t.Error("the .ics payload was inlined; it should be a link")
	}
}

// A guest has no C2 identity, so there is no inbox to deliver to. That is the
// expected shape of a guest booking, not an error.
func TestGuestBookerIsSkipped(t *testing.T) {
	n, cap, db := newNotifier(t)
	guest := domain.User{Subject: "guest:abc", Email: "g@example.com", Role: domain.RoleGuest}
	db.Create(&guest)
	n.BookingConfirmed(mkBooking(t, db, guest), "")

	if got := len(cap.all()); got != 0 {
		t.Fatalf("sent %d notifications to a guest, want 0", got)
	}
}

// A new request notifies the booker AND the staff who must review it.
func TestSubmittedNotifiesBookerAndStaff(t *testing.T) {
	n, cap, db := newNotifier(t)
	booker := mkUser(t, db, domain.RoleResident, "en")
	staff := mkUser(t, db, domain.RoleStaff, "en")
	admin := mkUser(t, db, domain.RoleAdmin, "en")
	n.BookingSubmitted(mkBooking(t, db, booker))

	subs := map[string]bool{}
	for _, s := range cap.all() {
		subs[s.Subject] = true
	}
	for who, u := range map[string]domain.User{"booker": booker, "staff": staff, "admin": admin} {
		if !subs[u.Subject] {
			t.Errorf("%s was not notified", who)
		}
	}
}

// Residents must never receive the staff "needs approval" message.
func TestResidentsAreNotToldToApprove(t *testing.T) {
	n, cap, db := newNotifier(t)
	booker := mkUser(t, db, domain.RoleResident, "en")
	other := mkUser(t, db, domain.RoleResident, "en")
	n.BookingSubmitted(mkBooking(t, db, booker))

	for _, s := range cap.all() {
		if s.Subject == other.Subject {
			t.Fatal("an unrelated resident was notified")
		}
		if s.Subject == booker.Subject && strings.Contains(strings.ToLower(s.Title), "approval") {
			t.Fatal("the booker was sent the staff approval message")
		}
	}
}

// A citizen who has not consented is skipped without disturbing anything. C2
// returns 403 and the booking stands — the message is a courtesy, the booking is
// the record.
func TestNoConsentIsNotAFailure(t *testing.T) {
	n, cap, db := newNotifier(t)
	cap.status = http.StatusForbidden
	u := mkUser(t, db, domain.RoleResident, "en")

	// Must not panic and must not block; the booking already exists.
	n.BookingConfirmed(mkBooking(t, db, u), "")
	if got := len(cap.all()); got != 1 {
		t.Fatalf("attempted %d sends, want 1 (the attempt is made, the refusal accepted)", got)
	}
}

// C2 being down must not stop anything either.
func TestProviderFailureIsSwallowed(t *testing.T) {
	n, cap, db := newNotifier(t)
	cap.status = http.StatusInternalServerError
	u := mkUser(t, db, domain.RoleResident, "en")
	n.BookingCancelled(mkBooking(t, db, u), "")
	if got := len(cap.all()); got != 1 {
		t.Fatalf("attempted %d sends, want 1", got)
	}
}

// The reminder's whole purpose is the before-use instructions (§4.10).
func TestReminderCarriesTheInstructions(t *testing.T) {
	n, cap, db := newNotifier(t)
	u := mkUser(t, db, domain.RoleResident, "en")
	n.BookingReminder(mkBooking(t, db, u), "Collect keys at the front desk.")

	sent := cap.all()
	if len(sent) != 1 || !strings.Contains(sent[0].Body, "Collect keys") {
		t.Fatalf("reminder body = %+v, want the before-use instructions", sent)
	}
}

// Every message must carry an SMS-sized short form that stands alone — a citizen
// who only reads the SMS still needs to know which booking changed.
func TestEveryMessageHasAUsefulShortForm(t *testing.T) {
	n, cap, db := newNotifier(t)
	u := mkUser(t, db, domain.RoleResident, "en")
	b := mkBooking(t, db, u)

	n.BookingSubmitted(b)
	n.BookingConfirmed(b, "")
	n.BookingDenied(b)
	n.BookingCancelled(b, "")
	n.BookingReminder(b, "Keys at the desk.")
	n.WaitlistOpened(domain.WaitlistEntry{UserID: u.ID, StartsAt: b.StartsAt}, "Rivermont Hall")

	for _, s := range cap.all() {
		if s.ShortBody == "" {
			t.Errorf("%q has no short form — SMS would fall back to the full body", s.Title)
		}
		if len(s.ShortBody) > 160 {
			t.Errorf("%q short form is %d chars, over one SMS segment", s.Title, len(s.ShortBody))
		}
		if !strings.Contains(s.ShortBody, "Rivermont Hall") {
			t.Errorf("%q short form does not name the facility: %q", s.Title, s.ShortBody)
		}
	}
}

// AC: a French-preferring recipient gets French facility content, not French
// wording wrapped around an English facility name.
func TestNotificationUsesTranslatedFacilityName(t *testing.T) {
	n, cap, db := newNotifier(t)
	u := mkUser(t, db, domain.RoleResident, "fr")
	b := mkBooking(t, db, u)
	db.Create(&domain.FacilityTranslation{
		FacilityID: b.FacilityID, Language: domain.LangFR, Name: "Salle de Rivermont",
	})

	n.BookingConfirmed(b, "")
	sent := cap.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d, want 1", len(sent))
	}
	if !strings.Contains(sent[0].Body, "Salle de Rivermont") {
		t.Errorf("body uses the English facility name: %q", sent[0].Body)
	}
	if strings.Contains(sent[0].Body, "Rivermont Hall") {
		t.Errorf("body still contains the English name: %q", sent[0].Body)
	}
}

// Translating for one recipient must not corrupt the booking for the next: two
// staff in different languages each get their own.
func TestTranslationDoesNotLeakBetweenRecipients(t *testing.T) {
	n, cap, db := newNotifier(t)
	booker := mkUser(t, db, domain.RoleResident, "en")
	mkUser(t, db, domain.RoleStaff, "fr")
	b := mkBooking(t, db, booker)
	db.Create(&domain.FacilityTranslation{
		FacilityID: b.FacilityID, Language: domain.LangFR, Name: "Salle de Rivermont",
	})

	n.BookingSubmitted(b)

	var sawEnglish, sawFrench bool
	for _, s := range cap.all() {
		if strings.Contains(s.Body, "Rivermont Hall") {
			sawEnglish = true
		}
		if strings.Contains(s.Body, "Salle de Rivermont") {
			sawFrench = true
		}
	}
	if !sawEnglish || !sawFrench {
		t.Fatalf("each recipient should see their own language (english=%v french=%v)", sawEnglish, sawFrench)
	}
}
