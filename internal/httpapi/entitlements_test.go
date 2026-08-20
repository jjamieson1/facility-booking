package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/entitlement"
)

// The defect FAC-15 closes: posting an address used to set the residency flag,
// so anyone could take the resident rate. Now the provider decides, and the
// test server's roll contains only "Willow Lane".
func TestResidencyCannotBeSelfDeclared(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	cookie := sessionFor(t, authSvc, db, domain.RoleResident)

	rr := do(t, h, http.MethodPost, "/api/entitlements/residency/prove",
		`{"inputs":{"address":"1 Anywhere Blvd"}}`, cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}

	var body struct {
		Outcome      domain.EntitlementOutcome `json:"outcome"`
		Entitlements entitlement.Set           `json:"entitlements"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Outcome != domain.EntitlementDenied {
		t.Fatalf("an address off the roll was accepted: %s", body.Outcome)
	}
	if body.Entitlements.IsResident() {
		t.Fatal("self-declared residency was granted")
	}

	var u domain.User
	db.First(&u, "role = ?", domain.RoleResident)
	if u.IsResident {
		t.Fatal("posting an address still sets the resident flag")
	}
}

// An address that IS on the municipal roll is granted — the check is real in
// both directions, not a blanket refusal.
func TestResidencyGrantedForAnAddressOnTheRoll(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	cookie := sessionFor(t, authSvc, db, domain.RoleResident)

	rr := do(t, h, http.MethodPost, "/api/entitlements/residency/prove",
		`{"inputs":{"address":"12 Willow Lane"}}`, cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}
	var body struct {
		Outcome domain.EntitlementOutcome `json:"outcome"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.Outcome != domain.EntitlementGranted {
		t.Fatalf("outcome = %s, want granted", body.Outcome)
	}
}

// The prove endpoint accepts evidence, never an outcome. A caller trying to
// assert the decision is rejected by the strict decoder rather than obeyed.
func TestProveRejectsACallerSuppliedOutcome(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	cookie := sessionFor(t, authSvc, db, domain.RoleResident)

	for _, body := range []string{
		`{"outcome":"granted"}`,
		`{"inputs":{"address":"1 Anywhere Blvd"},"outcome":"granted"}`,
		`{"inputs":{"address":"1 Anywhere Blvd"},"isResident":true}`,
	} {
		rr := do(t, h, http.MethodPost, "/api/entitlements/residency/prove", body, cookie)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("body %s gave %d, want 400", body, rr.Code)
		}
	}
}

// The proving form renders from the provider's published contract.
func TestDescriptorEndpoint(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	cookie := sessionFor(t, authSvc, db, domain.RoleResident)

	rr := do(t, h, http.MethodGet, "/api/entitlements/residency/descriptor", "", cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var d entitlement.Descriptor
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Fields) == 0 || d.Provider == "" {
		t.Fatalf("descriptor is not usable by a form: %+v", d)
	}
}

func TestDescriptorUnknownType(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	cookie := sessionFor(t, authSvc, db, domain.RoleResident)

	if rr := do(t, h, http.MethodGet, "/api/entitlements/unicorn/descriptor", "", cookie); rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// AC: the determinations that produced a price are stamped on the booking, and
// the booking is priced from them.
func TestBookingStampsTheEntitlementsThatPricedIt(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	cookie := sessionFor(t, authSvc, db, domain.RoleResident)

	fac := domain.Facility{Name: "Hall", Capacity: 50, FeeCents: 4000, NonResidentFeeCents: 6000}
	if err := db.Create(&fac).Error; err != nil {
		t.Fatal(err)
	}
	for wd := 0; wd < 7; wd++ {
		db.Create(&domain.AvailabilityRule{FacilityID: fac.ID, Weekday: wd, OpenMinute: 0, CloseMinute: 24 * 60})
	}

	// Establish residency through the provider, then book.
	if rr := do(t, h, http.MethodPost, "/api/entitlements/residency/prove",
		`{"inputs":{"address":"12 Willow Lane"}}`, cookie); rr.Code != http.StatusOK {
		t.Fatalf("prove failed: %s", rr.Body.String())
	}

	start := time.Now().Add(72 * time.Hour).Truncate(time.Hour)
	body := `{"facilityId":"` + fac.ID + `","start":"` + start.Format(time.RFC3339) +
		`","end":"` + start.Add(time.Hour).Format(time.RFC3339) + `","purpose":"meeting","attendance":5,"repeatWeeks":0}`
	rr := do(t, h, http.MethodPost, "/api/bookings", body, cookie)
	if rr.Code != http.StatusCreated {
		t.Fatalf("booking failed: %d %s", rr.Code, rr.Body.String())
	}
	var b domain.Booking
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}

	if b.FeeCents != 4000 {
		t.Errorf("fee = %d, want the resident rate 4000", b.FeeCents)
	}
	if !b.Resident {
		t.Error("Booking.Resident should stay set — the reports split reads it")
	}

	var stamped []domain.BookingEntitlement
	if err := db.Where("booking_id = ?", b.ID).Find(&stamped).Error; err != nil {
		t.Fatal(err)
	}
	if len(stamped) != 1 || stamped[0].Type != string(entitlement.TypeResidency) {
		t.Fatalf("want the residency determination stamped on the booking, got %+v", stamped)
	}
	if stamped[0].Provider == "" || stamped[0].Category != "resident" {
		t.Errorf("the stamp must carry provenance and category: %+v", stamped[0])
	}
}

// A booker with no determination pays the non-resident rate — and nothing is
// stamped, so a later enrolment cannot be mistaken for one in force at the time.
func TestBookingWithoutEntitlementUsesNonResidentRate(t *testing.T) {
	h, authSvc, db := fullTestServer(t)
	cookie := sessionFor(t, authSvc, db, domain.RoleResident)

	fac := domain.Facility{Name: "Hall", Capacity: 50, FeeCents: 4000, NonResidentFeeCents: 6000}
	db.Create(&fac)
	for wd := 0; wd < 7; wd++ {
		db.Create(&domain.AvailabilityRule{FacilityID: fac.ID, Weekday: wd, OpenMinute: 0, CloseMinute: 24 * 60})
	}

	start := time.Now().Add(96 * time.Hour).Truncate(time.Hour)
	body := `{"facilityId":"` + fac.ID + `","start":"` + start.Format(time.RFC3339) +
		`","end":"` + start.Add(time.Hour).Format(time.RFC3339) + `","purpose":"meeting","attendance":5,"repeatWeeks":0}`
	rr := do(t, h, http.MethodPost, "/api/bookings", body, cookie)
	if rr.Code != http.StatusCreated {
		t.Fatalf("booking failed: %d %s", rr.Code, rr.Body.String())
	}
	var b domain.Booking
	_ = json.Unmarshal(rr.Body.Bytes(), &b)

	if b.FeeCents != 6000 {
		t.Errorf("fee = %d, want the non-resident rate 6000", b.FeeCents)
	}
	var stamped int64
	db.Model(&domain.BookingEntitlement{}).Where("booking_id = ?", b.ID).Count(&stamped)
	if stamped != 0 {
		t.Errorf("nothing should be stamped when no entitlement was held, got %d", stamped)
	}
}
