package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/calendar"
	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// calendarTestServer wires the real router over an in-memory DB so the calendar
// settings routes are exercised through their actual middleware chain.
func calendarTestServer(t *testing.T) (http.Handler, *auth.Service, *gorm.DB) {
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
	h := New(Deps{
		Cfg:      cfg,
		Auth:     authSvc,
		Calendar: calendar.NewService(db, auditlog.New("", "")),
	})
	return h, authSvc, db
}

// sessionFor creates a user with the given role and returns their session
// cookie. The subject is unique per call so a test can mint several sessions —
// needed when a route under test (logout) invalidates the session it is given.
func sessionFor(t *testing.T, authSvc *auth.Service, db *gorm.DB, role domain.Role) *http.Cookie {
	t.Helper()
	u := domain.User{Subject: string(role) + "-" + uuid.NewString(), Email: string(role) + "@rivermont.ca", Name: "Test", Role: role}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	id, err := authSvc.OpenSession(context.Background(), auth.Login{User: &u, IDToken: "raw.id.token"})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "fb_session", Value: id}
}

func do(t *testing.T, h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// Reading the integration is staff-visible; changing it is admin-only. Residents
// and anonymous callers get nothing.
func TestCalendarSettingsAuthorization(t *testing.T) {
	h, authSvc, db := calendarTestServer(t)
	staff := sessionFor(t, authSvc, db, domain.RoleStaff)
	resident := sessionFor(t, authSvc, db, domain.RoleResident)
	admin := sessionFor(t, authSvc, db, domain.RoleAdmin)

	cases := []struct {
		name   string
		method string
		body   string
		cookie *http.Cookie
		want   int
	}{
		{"anonymous read", http.MethodGet, "", nil, http.StatusUnauthorized},
		{"resident read", http.MethodGet, "", resident, http.StatusForbidden},
		{"staff read", http.MethodGet, "", staff, http.StatusOK},
		{"admin read", http.MethodGet, "", admin, http.StatusOK},
		{"anonymous write", http.MethodPut, `{"kind":"none","config":{}}`, nil, http.StatusUnauthorized},
		{"resident write", http.MethodPut, `{"kind":"none","config":{}}`, resident, http.StatusForbidden},
		{"staff write", http.MethodPut, `{"kind":"none","config":{}}`, staff, http.StatusForbidden},
		{"admin write", http.MethodPut, `{"kind":"none","config":{}}`, admin, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := do(t, h, tc.method, "/api/staff/calendar-settings", tc.body, tc.cookie)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tc.want, rr.Body.String())
			}
		})
	}
}

// The form renders straight off this payload, so it must carry the modules and
// the current selection together.
func TestCalendarSettingsGetReturnsModulesAndSelection(t *testing.T) {
	h, authSvc, db := calendarTestServer(t)
	staff := sessionFor(t, authSvc, db, domain.RoleStaff)

	rr := do(t, h, http.MethodGet, "/api/staff/calendar-settings", "", staff)
	var body struct {
		Modules  []calendar.Module `json:"modules"`
		Settings calendar.Settings `json:"settings"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Modules) < 2 {
		t.Fatalf("want the module list, got %d", len(body.Modules))
	}
	if body.Settings.Selected != calendar.DefaultKind {
		t.Errorf("want default %s, got %s", calendar.DefaultKind, body.Settings.Selected)
	}
}

func TestCalendarSettingsUpdateRejectsBadInput(t *testing.T) {
	h, authSvc, db := calendarTestServer(t)
	admin := sessionFor(t, authSvc, db, domain.RoleAdmin)

	cases := []struct {
		name string
		body string
	}{
		{"unknown module", `{"kind":"dropbox","config":{}}`},
		{"unknown config field", `{"kind":"google","config":{"calendarId":"x","nope":"y"}}`},
		{"missing required field", `{"kind":"google","config":{}}`},
		{"unknown json field", `{"kind":"none","surprise":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := do(t, h, http.MethodPut, "/api/staff/calendar-settings", tc.body, admin)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rr.Code, rr.Body.String())
			}
		})
	}
}

// Selecting a two-way module records the decision while the app keeps running
// the one-way default — the response must make both halves visible.
func TestCalendarSettingsUpdateReportsFallback(t *testing.T) {
	h, authSvc, db := calendarTestServer(t)
	admin := sessionFor(t, authSvc, db, domain.RoleAdmin)

	rr := do(t, h, http.MethodPut, "/api/staff/calendar-settings",
		`{"kind":"google","config":{"calendarId":"spaces@rivermont.ca"}}`, admin)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}
	var body struct {
		Settings calendar.Settings `json:"settings"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Settings.Selected != calendar.KindGoogle {
		t.Errorf("selected = %s, want google", body.Settings.Selected)
	}
	if body.Settings.Effective != calendar.KindICS {
		t.Errorf("effective = %s, want ics", body.Settings.Effective)
	}
	if body.Settings.FallbackNotes == "" {
		t.Error("want the fallback explained in the response")
	}
}
