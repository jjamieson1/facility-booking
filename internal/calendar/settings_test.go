package calendar

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func newService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := testdb.New(t)
	return NewService(db, auditlog.New("", "")), db
}

func mkAdmin(t *testing.T, db *gorm.DB) domain.User {
	t.Helper()
	u := domain.User{Subject: uuid.NewString(), Email: "admin@rivermont.ca", Name: "Admin", Role: domain.RoleAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func TestGetDefaultsToOneWayICS(t *testing.T) {
	svc, _ := newService(t)

	set, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if set.Selected != KindICS || set.Effective != KindICS {
		t.Fatalf("want ics/ics before any choice, got %s/%s", set.Selected, set.Effective)
	}
	if !set.Connected {
		t.Error("the default module should report as connected")
	}
}

func TestSetAvailableModuleRoundTrips(t *testing.T) {
	svc, db := newService(t)
	admin := mkAdmin(t, db)

	if _, err := svc.Set(context.Background(), KindNone, nil, admin); err != nil {
		t.Fatal(err)
	}

	set, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if set.Selected != KindNone || set.Effective != KindNone {
		t.Fatalf("want none/none, got %s/%s", set.Selected, set.Effective)
	}
	if set.FallbackNotes != "" {
		t.Errorf("an available module should not report a fallback: %q", set.FallbackNotes)
	}
}

// A two-way module can be selected to record the municipality's decision, but
// the app must keep running the one-way default until that module is built.
func TestSelectingUnbuiltModuleFallsBackButRecordsChoice(t *testing.T) {
	svc, db := newService(t)
	admin := mkAdmin(t, db)

	set, err := svc.Set(context.Background(), KindGoogle, map[string]string{"calendarId": "spaces@rivermont.ca"}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if set.Selected != KindGoogle {
		t.Errorf("selection should be recorded as google, got %s", set.Selected)
	}
	if set.Effective != KindICS {
		t.Errorf("effective module should fall back to ics, got %s", set.Effective)
	}
	if set.Connected {
		t.Error("an unbuilt module must not report as connected")
	}
	if set.FallbackNotes == "" {
		t.Error("the fallback should be explained to the operator")
	}

	// The provider the app actually uses is the working one, not a broken stub.
	if got := svc.Provider(context.Background()).Kind(); got != KindICS {
		t.Errorf("Provider() should hand back the effective module, got %s", got)
	}
}

func TestSetPersistsConfigAndActor(t *testing.T) {
	svc, db := newService(t)
	admin := mkAdmin(t, db)

	cfg := map[string]string{"tenantId": "t-1", "calendarId": "spaces@rivermont.ca", "timeZone": "  America/Toronto  "}
	if _, err := svc.Set(context.Background(), KindMicrosoft, cfg, admin); err != nil {
		t.Fatal(err)
	}

	set, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if set.Config["timeZone"] != "America/Toronto" {
		t.Errorf("config values should be trimmed, got %q", set.Config["timeZone"])
	}
	if set.UpdatedByID != admin.ID {
		t.Errorf("want the acting admin recorded, got %q", set.UpdatedByID)
	}
}

// Repeated writes must update the one singleton row, never accumulate rows.
func TestSetIsSingleton(t *testing.T) {
	svc, db := newService(t)
	admin := mkAdmin(t, db)

	for _, k := range []Kind{KindNone, KindICS, KindGoogle} {
		cfg := map[string]string{}
		if k == KindGoogle {
			cfg["calendarId"] = "spaces@rivermont.ca"
		}
		if _, err := svc.Set(context.Background(), k, cfg, admin); err != nil {
			t.Fatal(err)
		}
	}

	var n int64
	db.Model(&domain.CalendarIntegration{}).Count(&n)
	if n != 1 {
		t.Fatalf("want exactly 1 settings row, got %d", n)
	}
}

func TestSetRejectsBadInput(t *testing.T) {
	svc, db := newService(t)
	admin := mkAdmin(t, db)

	cases := []struct {
		name string
		kind Kind
		cfg  map[string]string
		want error
	}{
		{"unknown module", Kind("dropbox"), nil, ErrUnknownKind},
		{"unknown field", KindGoogle, map[string]string{"calendarId": "x", "nope": "y"}, ErrUnknownField},
		{"missing required field", KindGoogle, map[string]string{"timeZone": "America/Toronto"}, ErrMissingField},
		{"blank required field", KindGoogle, map[string]string{"calendarId": "   "}, ErrMissingField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Set(context.Background(), tc.kind, tc.cfg, admin); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestSetWritesAuditRow(t *testing.T) {
	svc, db := newService(t)
	admin := mkAdmin(t, db)

	if _, err := svc.Set(context.Background(), KindNone, nil, admin); err != nil {
		t.Fatal(err)
	}

	var log domain.AuditLog
	if err := db.First(&log, "action = ?", "calendar.settings.update").Error; err != nil {
		t.Fatalf("expected an audit row: %v", err)
	}
	if log.ActorID != admin.ID {
		t.Errorf("want actor %s, got %s", admin.ID, log.ActorID)
	}
}

// A stored module this build no longer registers must not break the app.
func TestGetFallsBackOnUnknownStoredKind(t *testing.T) {
	svc, db := newService(t)
	db.Create(&domain.CalendarIntegration{
		Base: domain.Base{ID: domain.CalendarIntegrationID}, Kind: "retired-module",
	})

	set, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if set.Effective != DefaultKind {
		t.Fatalf("want fallback to %s, got %s", DefaultKind, set.Effective)
	}
}
