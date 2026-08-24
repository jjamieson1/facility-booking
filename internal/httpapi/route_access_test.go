package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/booking"
	"github.com/jjamieson1/facility-booking/internal/calendar"
	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/entitlement"
	"github.com/jjamieson1/facility-booking/internal/facility"
	"github.com/jjamieson1/facility-booking/internal/media"
	"github.com/jjamieson1/facility-booking/internal/notify"
	"github.com/jjamieson1/facility-booking/internal/payment"
	"github.com/jjamieson1/facility-booking/internal/policy"
	"github.com/jjamieson1/facility-booking/internal/reports"
	"github.com/jjamieson1/facility-booking/internal/testdb"
	"github.com/jjamieson1/facility-booking/internal/users"
	"github.com/jjamieson1/facility-booking/internal/waitlist"
	"github.com/jjamieson1/facility-booking/internal/waiver"
)

// access is the authorization level a route sits behind.
type access int

const (
	accessPublic  access = iota // no session needed
	accessSession               // auth.RequireSession — a guest session may pass
	accessAccount               // auth.RequireAccount — real accounts only
	accessStaff                 // auth.RequireRole(staff, admin)
	accessAdmin                 // auth.RequireRole(admin)
)

func (a access) String() string {
	return [...]string{"public", "session", "account", "staff", "admin"}[a]
}

// routeAccess classifies EVERY route the router registers.
//
// This is the audit that FAC-16 requires: a new route must be added here
// deliberately, and TestEveryRouteIsClassified fails until it is. When the right
// level is not obvious, choose the stricter one — relaxing a route later is a
// one-line change, discovering that guests could reach it is an incident.
var routeAccess = map[string]access{
	// Health check.
	"GET /healthz": accessPublic,

	// Login/logout. Anonymous by necessity.
	"GET /api/auth/login":               accessPublic,
	"GET /api/auth/callback":            accessPublic,
	"GET /api/auth/me":                  accessPublic, // returns null when anonymous
	"POST /api/auth/logout":             accessPublic,
	"POST /api/auth/backchannel-logout": accessPublic, // authenticated by C2's logout token

	// Public directory, availability, and the city calendar feed (§4.11 —
	// deliberately readable without an account, to reduce enquiries).
	// C2 posts settlements server-to-server with no session; the signed status
	// token is the authentication, verified in the handler.
	"POST /api/payments/c2/callback":               accessPublic,
	"GET /api/facilities":                          accessPublic,
	"GET /api/facilities/filter-options":           accessPublic,
	"GET /api/facilities/{id}":                     accessPublic,
	"GET /api/facilities/{id}/availability":        accessPublic,
	"GET /api/facilities/{id}/calendar":            accessPublic,
	"GET /api/facilities/{id}/cancellation-policy": accessPublic,
	"GET /api/calendar.ics":                        accessPublic,
	"GET /api/waiver-template.pdf":                 accessPublic,
	"GET /api/citizens/{sub}/status":               accessPublic, // authenticated by a C2-issued JWT, not a cookie

	// A booker acting on their own booking. Guests included: the handler's
	// ownership check is what protects the data.
	"POST /api/bookings":     accessSession,
	"GET /api/bookings/mine": accessSession,
	// A guest holds a language preference too: it is about how we speak to them,
	// not about durable identity.
	"PUT /api/me/language":                accessSession,
	"GET /api/bookings/{id}":              accessSession,
	"GET /api/bookings/{id}/invite.ics":   accessSession,
	"POST /api/bookings/{id}/cancel":      accessSession,
	"GET /api/bookings/{id}/refund-quote": accessSession,
	"POST /api/bookings/{id}/pay":         accessSession,
	"POST /api/bookings/{id}/waiver":      accessSession,
	"GET /api/bookings/{id}/waiver":       accessSession,

	// Tied to a durable identity rather than to one booking. Entitlements are
	// account-bound: a guest has no durable identity to attach one to, and the
	// old self-declaring POST /api/verify-residency is gone entirely.
	"GET /api/entitlements":                   accessAccount,
	"GET /api/entitlements/{type}/descriptor": accessAccount,
	"POST /api/entitlements/{type}/prove":     accessAccount,
	"POST /api/bookings/{id}/reschedule":      accessAccount,
	"POST /api/facilities/{id}/waitlist":      accessAccount,
	"GET /api/waitlist/mine":                  accessAccount,
	"DELETE /api/waitlist/{id}":               accessAccount,

	// Staff back-office.
	"GET /api/staff/bookings/pending":                          accessStaff,
	"POST /api/staff/bookings/{id}/approve":                    accessStaff,
	"POST /api/staff/bookings/{id}/deny":                       accessStaff,
	"POST /api/staff/bookings/{id}/refund":                     accessStaff,
	"POST /api/staff/facilities":                               accessStaff,
	"PUT /api/staff/facilities/{id}":                           accessStaff,
	"DELETE /api/staff/facilities/{id}":                        accessStaff,
	"GET /api/staff/facilities/{id}/translations":              accessStaff,
	"PUT /api/staff/facilities/{id}/translations":              accessStaff,
	"GET /api/staff/facilities/{id}/blackouts":                 accessStaff,
	"POST /api/staff/facilities/{id}/blackouts":                accessStaff,
	"DELETE /api/staff/facilities/{id}/blackouts/{blackoutId}": accessStaff,
	"GET /api/staff/reports/summary":                           accessStaff,
	"GET /api/staff/payments":                                  accessStaff,
	"GET /api/staff/audit":                                     accessStaff,
	"GET /api/staff/calendar-settings":                         accessStaff,
	"GET /api/staff/refund-obligations":                        accessStaff,
	"GET /api/staff/payment-settings":                          accessStaff,

	// Admin only.
	"PUT /api/staff/payment-settings":      accessAdmin,
	"GET /api/staff/users":                 accessAdmin,
	"POST /api/staff/users/invite":         accessAdmin,
	"PUT /api/staff/users/{id}/role":       accessAdmin,
	"DELETE /api/staff/users/invites/{id}": accessAdmin,
	"PUT /api/staff/calendar-settings":     accessAdmin,
}

// fullTestServer wires every service over an in-memory DB, so a request that
// passes the auth gate reaches a real handler instead of panicking on a nil
// service — otherwise "not 403" would prove nothing.
func fullTestServer(t *testing.T) (http.Handler, *auth.Service, *gorm.DB) {
	t.Helper()
	db := testdb.New(t)
	cfg := config.Config{
		AppOrigin:                 "http://localhost:5180",
		OIDCIssuer:                "http://localhost:5173/oidc",
		OIDCBaseURL:               "http://localhost:5173/oidc",
		OIDCClientID:              "client-1",
		OIDCClientSecret:          "secret",
		OIDCPostLogoutRedirectURL: "http://localhost:5180/",
	}
	authSvc, err := auth.NewService(context.Background(), db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	mediaStore, err := media.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	notifier := notify.NewLogNotifier()
	audit := auditlog.New("", "")
	return New(Deps{
		Cfg:      cfg,
		Auth:     authSvc,
		Facility: facility.NewService(db),
		Booking:  booking.NewService(db, policy.NewService(db)),
		Payment:  payment.NewService(db, payment.Fixed(payment.NewMockProvider())),
		Reports:  reports.NewService(db),
		Waitlist: waitlist.NewService(db, notifier),
		Waiver:   waiver.NewService(db, mediaStore),
		Users:    users.NewService(db, audit),
		Calendar: calendar.NewService(db, audit),
		Entitlements: entitlement.NewService(db, audit,
			entitlement.NewRollProvider([]string{"Willow Lane"}, time.Hour)),
		PaymentSettings: payment.NewSettingsService(db, audit),
		Policy:          policy.NewService(db),
		DB:              db,
		Notifier:        notifier,
		Audit:           audit,
	}), authSvc, db
}

// registeredRoutes walks the router and returns "METHOD /pattern" for every
// route it actually serves.
func registeredRoutes(t *testing.T, h http.Handler) []string {
	t.Helper()
	mux, ok := h.(chi.Routes)
	if !ok {
		t.Fatal("router is not a chi.Routes; cannot enumerate routes")
	}
	var out []string
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		if route == "" {
			route = "/"
		}
		out = append(out, method+" "+route)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestEveryRouteIsClassified is the guard: adding a route without deciding who
// may reach it fails the build.
func TestEveryRouteIsClassified(t *testing.T) {
	h, _, _ := fullTestServer(t)
	routes := registeredRoutes(t, h)

	seen := map[string]bool{}
	for _, r := range routes {
		seen[r] = true
		if _, ok := routeAccess[r]; !ok {
			t.Errorf("route %q is not classified in routeAccess — decide whether a guest session may reach it (when unsure, use accessAccount)", r)
		}
	}
	for r := range routeAccess {
		if !seen[r] {
			t.Errorf("routeAccess lists %q, which the router no longer registers — remove the stale entry", r)
		}
	}
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// requestFor turns a route pattern into a concrete request path.
func requestFor(route string) (method, path string) {
	parts := strings.SplitN(route, " ", 2)
	return parts[0], pathParam.ReplaceAllString(parts[1], "00000000-0000-0000-0000-000000000000")
}

// TestGuestSessionCannotReachAccountRoutes is the behavioural half: a guest is
// blocked from every account/staff/admin route, and reaches every route meant
// for a booker.
func TestGuestSessionCannotReachAccountRoutes(t *testing.T) {
	h, authSvc, db := fullTestServer(t)

	for _, route := range registeredRoutes(t, h) {
		level, ok := routeAccess[route]
		if !ok {
			continue // reported by TestEveryRouteIsClassified
		}
		t.Run(fmt.Sprintf("%s/%s", level, route), func(t *testing.T) {
			// A fresh session per route: one of the routes under test is logout,
			// which invalidates the session it is handed. Reusing one session
			// would silently turn every later request anonymous.
			guest := sessionFor(t, authSvc, db, domain.RoleGuest)
			method, path := requestFor(route)
			rr := do(t, h, method, path, "", guest)

			if level == accessPublic || level == accessSession {
				if rr.Code == http.StatusForbidden {
					t.Fatalf("%s is %s but a guest was refused (403)", route, level)
				}
				return
			}
			if rr.Code != http.StatusForbidden {
				t.Fatalf("%s is %s but a guest got %d, want 403", route, level, rr.Code)
			}
		})
	}
}

// A real account must not be caught by the guest check.
func TestResidentReachesAccountRoutes(t *testing.T) {
	h, authSvc, db := fullTestServer(t)

	for route, level := range routeAccess {
		if level != accessAccount {
			continue
		}
		t.Run(route, func(t *testing.T) {
			resident := sessionFor(t, authSvc, db, domain.RoleResident)
			method, path := requestFor(route)
			if rr := do(t, h, method, path, "", resident); rr.Code == http.StatusForbidden {
				t.Fatalf("%s refused a resident (403)", route)
			}
		})
	}
}

// The point of letting a guest session through at all is that ownership checks
// still protect the data. A guest reaches their own booking and is refused
// someone else's, exactly as an account holder would be — this is the IDOR guard
// that lets RequireSession be safe.
func TestGuestOwnershipChecksBehaveLikeAnAccount(t *testing.T) {
	h, authSvc, db := fullTestServer(t)

	mine := domain.User{Subject: "guest-" + uuid.NewString(), Email: "g1@example.com", Role: domain.RoleGuest}
	theirs := domain.User{Subject: "guest-" + uuid.NewString(), Email: "g2@example.com", Role: domain.RoleGuest}
	resident := domain.User{Subject: "res-" + uuid.NewString(), Email: "r@example.com", Role: domain.RoleResident}
	for _, u := range []*domain.User{&mine, &theirs, &resident} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}

	fac := domain.Facility{Name: "Hall", Capacity: 10}
	if err := db.Create(&fac).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(48 * time.Hour)
	book := func(owner domain.User) domain.Booking {
		b := domain.Booking{
			FacilityID: fac.ID, UserID: owner.ID, StartsAt: start, EndsAt: start.Add(time.Hour),
			Status: domain.StatusConfirmed, Purpose: "test",
		}
		if err := db.Create(&b).Error; err != nil {
			t.Fatal(err)
		}
		return b
	}
	guestBooking, residentBooking := book(mine), book(resident)

	cookieFor := func(u domain.User) *http.Cookie {
		id, err := authSvc.OpenSession(context.Background(), auth.Login{User: &u, IDToken: "tok"})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: "fb_session", Value: id}
	}

	cases := []struct {
		name    string
		actor   domain.User
		booking domain.Booking
		want    int
	}{
		{"guest reads own booking", mine, guestBooking, http.StatusOK},
		{"guest refused another guest's booking", theirs, guestBooking, http.StatusForbidden},
		{"guest refused a resident's booking", mine, residentBooking, http.StatusForbidden},
		{"resident reads own booking", resident, residentBooking, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := do(t, h, http.MethodGet, "/api/bookings/"+tc.booking.ID, "", cookieFor(tc.actor))
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
}

// Anonymous callers get 401 — identity missing — on everything non-public,
// including the routes a guest may reach.
func TestAnonymousIsRejectedFromNonPublicRoutes(t *testing.T) {
	h, _, _ := fullTestServer(t)

	for route, level := range routeAccess {
		if level == accessPublic {
			continue
		}
		t.Run(route, func(t *testing.T) {
			method, path := requestFor(route)
			if rr := do(t, h, method, path, "", nil); rr.Code != http.StatusUnauthorized {
				t.Fatalf("%s gave anonymous %d, want 401", route, rr.Code)
			}
		})
	}
}
