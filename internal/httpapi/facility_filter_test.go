package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// residentSession mints a session for a user with the given residency. Residency
// is normally written by the entitlement provider; setting it directly here is
// the point — the test asserts the handler reads it from the session rather than
// from anything the caller sends.
func residentSession(t *testing.T, authSvc *auth.Service, db *gorm.DB, isResident bool) *http.Cookie {
	t.Helper()
	u := domain.User{
		Subject: "filter-" + uuid.NewString(), Email: "filter@rivermont.ca", Name: "Test",
		Role: domain.RoleResident, IsResident: isResident,
	}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	id, err := authSvc.OpenSession(context.Background(), auth.Login{User: &u, IDToken: "raw.id.token"})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "fb_session", Value: id}
}

func namesFrom(t *testing.T, body []byte) []string {
	t.Helper()
	var out []domain.Facility
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	names := make([]string, len(out))
	for i := range out {
		names[i] = out[i].Name
	}
	return names
}

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// The cost filter prices against the viewer's own rate, and that rate comes from
// the session. A non-resident who asks for it by query string must not get it:
// residency is an entitlement a provider decides, so a filter that trusted the
// request would quote the resident rate to anyone who typed it.
func TestCostFilterUsesSessionResidencyNotQueryString(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	if err := db.Create(&domain.Facility{
		Name: "Resident Bargain", Capacity: 50, FeeCents: 8000, NonResidentFeeCents: 15000,
	}).Error; err != nil {
		t.Fatal(err)
	}

	resident := residentSession(t, authSvc, db, true)
	nonResident := residentSession(t, authSvc, db, false)

	rr := do(t, h, http.MethodGet, "/api/facilities?maxFee=10000", "", resident)
	if got := namesFrom(t, rr.Body.Bytes()); !contains(got, "Resident Bargain") {
		t.Fatalf("resident should see the $80 rate under a $100 ceiling, got %v", got)
	}

	rr = do(t, h, http.MethodGet, "/api/facilities?maxFee=10000", "", nonResident)
	if got := namesFrom(t, rr.Body.Bytes()); contains(got, "Resident Bargain") {
		t.Fatalf("non-resident pays $150, which is over the $100 ceiling, got %v", got)
	}

	// The attempt: claim residency in the query string.
	rr = do(t, h, http.MethodGet, "/api/facilities?maxFee=10000&resident=true", "", nonResident)
	if got := namesFrom(t, rr.Body.Bytes()); contains(got, "Resident Bargain") {
		t.Fatalf("query string granted the resident rate, got %v", got)
	}

	// And anonymously, where there is no session to read.
	rr = do(t, h, http.MethodGet, "/api/facilities?maxFee=10000&resident=true", "", nil)
	if got := namesFrom(t, rr.Body.Bytes()); contains(got, "Resident Bargain") {
		t.Fatalf("anonymous request granted the resident rate, got %v", got)
	}
}

// Repeated ?accessory= means "all of these", so a facility with only one of the
// two must not come back.
func TestAccessoryParamsRepeatForAllOf(t *testing.T) {
	h, _, db := fullTestServer(t)
	both := domain.Facility{Name: "Fully Equipped", Capacity: 50}
	partial := domain.Facility{Name: "Projector Only", Capacity: 50}
	for _, f := range []*domain.Facility{&both, &partial} {
		if err := db.Create(f).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Projector", "Sound system"} {
		a := domain.Accessory{Name: name}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&domain.FacilityAccessory{FacilityID: both.ID, AccessoryID: a.ID, Quantity: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if name == "Projector" {
			if err := db.Create(&domain.FacilityAccessory{FacilityID: partial.ID, AccessoryID: a.ID, Quantity: 1}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}

	rr := do(t, h, http.MethodGet, "/api/facilities?accessory=Projector&accessory=Sound+system", "", nil)
	got := namesFrom(t, rr.Body.Bytes())
	if !contains(got, "Fully Equipped") || contains(got, "Projector Only") {
		t.Fatalf("expected only the facility with both, got %v", got)
	}
}

// The filter panel renders straight off this, so its arrays must serialise as
// [] rather than null — the FAC-42 failure mode.
func TestFilterOptionsArePublicAndNeverNull(t *testing.T) {
	h, _, _ := fullTestServer(t)

	rr := do(t, h, http.MethodGet, "/api/facilities/filter-options", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("anonymous access: got %d", rr.Code)
	}
	// Asserted on the raw bytes, not a decoded struct: nil and empty are
	// indistinguishable once decoded, which is how FAC-42 stayed green.
	body := rr.Body.String()
	for _, want := range []string{`"areas":[]`, `"accessories":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in %s", want, body)
		}
	}
}
