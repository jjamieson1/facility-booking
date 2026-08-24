package facility

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// equip attaches named accessories to a facility, creating each accessory once.
func equip(t *testing.T, db *gorm.DB, f domain.Facility, names ...string) {
	t.Helper()
	for _, name := range names {
		var a domain.Accessory
		if err := db.Where("name = ?", name).First(&a).Error; err != nil {
			a = domain.Accessory{Name: name}
			if err := db.Create(&a).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Create(&domain.FacilityAccessory{FacilityID: f.ID, AccessoryID: a.ID, Quantity: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

// listNames applies a filter and returns the matching facility names.
func listNames(t *testing.T, svc *Service, f Filter) []string {
	t.Helper()
	got, err := svc.List(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(got))
	for i := range got {
		out[i] = got[i].Name
	}
	return out
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// §4.3's own worked example: capacity and an accessory, combined.
func TestFilterCapacityAndAccessory(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	big := makeFacility(t, db, "Big Hall", 200)
	small := makeFacility(t, db, "Small Room", 20)
	makeFacility(t, db, "Bare Hall", 200)
	equip(t, db, big, "Projector")
	equip(t, db, small, "Projector")

	got := listNames(t, svc, Filter{MinCapacity: 50, Accessories: []string{"Projector"}})
	if !same(got, []string{"Big Hall"}) {
		t.Fatalf("got %v", got)
	}
}

// Two accessories means both, not either — the gap this ticket exists to close.
func TestFilterRequiresAllAccessories(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	both := makeFacility(t, db, "Both", 50)
	onlyProjector := makeFacility(t, db, "Projector Only", 50)
	equip(t, db, both, "Projector", "Sound system")
	equip(t, db, onlyProjector, "Projector")

	got := listNames(t, svc, Filter{Accessories: []string{"Projector", "Sound system"}})
	if !same(got, []string{"Both"}) {
		t.Fatalf("expected only the facility with both, got %v", got)
	}
}

// A repeated selection must not inflate the HAVING count past what any facility
// can reach — that would turn a duplicated checkbox into "no results".
func TestFilterIgnoresDuplicateAccessories(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	f := makeFacility(t, db, "Hall", 50)
	equip(t, db, f, "Projector")

	got := listNames(t, svc, Filter{Accessories: []string{"Projector", "Projector", "  "}})
	if !same(got, []string{"Hall"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFilterAccessibilityNeeds(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	for _, f := range []domain.Facility{
		{Name: "Step Free Only", StepFreeAccess: true},
		{Name: "Washroom Only", AccessibleWashroom: true},
		{Name: "Both Access", StepFreeAccess: true, AccessibleWashroom: true},
		{Name: "Neither", Capacity: 10},
	} {
		if err := db.Create(&f).Error; err != nil {
			t.Fatal(err)
		}
	}

	if got := listNames(t, svc, Filter{StepFree: true}); !same(got, []string{"Both Access", "Step Free Only"}) {
		t.Fatalf("step-free: got %v", got)
	}
	if got := listNames(t, svc, Filter{StepFree: true, AccessibleWashroom: true}); !same(got, []string{"Both Access"}) {
		t.Fatalf("both needs: got %v", got)
	}
	// Not asking for step-free must not hide step-free facilities.
	if got := listNames(t, svc, Filter{}); len(got) != 4 {
		t.Fatalf("unfiltered should return all four, got %v", got)
	}
}

func TestFilterArea(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	for _, f := range []domain.Facility{
		{Name: "North Hall", Area: "North End"},
		{Name: "South Hall", Area: "South End"},
		{Name: "Unplaced", Area: ""},
	} {
		if err := db.Create(&f).Error; err != nil {
			t.Fatal(err)
		}
	}

	if got := listNames(t, svc, Filter{Area: "North End"}); !same(got, []string{"North Hall"}) {
		t.Fatalf("got %v", got)
	}

	opts, err := svc.FilterOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The unplaced facility contributes no option — an empty one would filter to
	// nothing and read as a broken dropdown entry.
	if !same(opts.Areas, []string{"North End", "South End"}) {
		t.Fatalf("areas: got %v", opts.Areas)
	}
}

// The panel offers only accessories some facility actually has; an unused one
// would be a checkbox that always returns nothing.
func TestFilterOptionsListOnlyAccessoriesInUse(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	f := makeFacility(t, db, "Hall", 50)
	equip(t, db, f, "Projector", "Sound system")
	if err := db.Create(&domain.Accessory{Name: "Unused Thing"}).Error; err != nil {
		t.Fatal(err)
	}

	opts, err := svc.FilterOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !same(opts.Accessories, []string{"Projector", "Sound system"}) {
		t.Fatalf("got %v", opts.Accessories)
	}
}

// The cost ceiling is compared against the price *this viewer* would pay. A
// non-resident filtering at $100 must not be shown a facility that costs them
// $150 just because residents pay $80.
func TestFilterCostRangeFollowsViewerPrice(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	for _, f := range []domain.Facility{
		{Name: "Cheap For Residents", FeeCents: 8000, NonResidentFeeCents: 15000},
		{Name: "Flat Rate", FeeCents: 9000}, // no non-resident fee: everyone pays 9000
		{Name: "Free Field", FeeCents: 0},
	} {
		if err := db.Create(&f).Error; err != nil {
			t.Fatal(err)
		}
	}

	resident := Filter{MaxFeeCents: 10000, Resident: true}
	if got := listNames(t, svc, resident); !same(got, []string{"Cheap For Residents", "Flat Rate", "Free Field"}) {
		t.Fatalf("resident: got %v", got)
	}

	nonResident := Filter{MaxFeeCents: 10000}
	if got := listNames(t, svc, nonResident); !same(got, []string{"Flat Rate", "Free Field"}) {
		t.Fatalf("non-resident: got %v", got)
	}
}

// A zero ceiling is an untouched form field, not "free only". Reading it as a
// $0 maximum would silently hide every priced facility.
func TestFilterZeroMaxFeeIsNoCeiling(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	if err := db.Create(&domain.Facility{Name: "Priced", FeeCents: 9000}).Error; err != nil {
		t.Fatal(err)
	}

	if got := listNames(t, svc, Filter{MaxFeeCents: 0}); !same(got, []string{"Priced"}) {
		t.Fatalf("got %v", got)
	}
	if got := listNames(t, svc, Filter{FreeOnly: true}); len(got) != 0 {
		t.Fatalf("free-only should exclude a priced facility, got %v", got)
	}
}

func TestFilterCostFloor(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	for _, f := range []domain.Facility{
		{Name: "Budget", FeeCents: 2000},
		{Name: "Premium", FeeCents: 20000},
	} {
		if err := db.Create(&f).Error; err != nil {
			t.Fatal(err)
		}
	}

	if got := listNames(t, svc, Filter{MinFeeCents: 5000, Resident: true}); !same(got, []string{"Premium"}) {
		t.Fatalf("got %v", got)
	}
}

// §4.3 and §4.4 combine: a result must be free at the requested time *and*
// match every parameter. Search layers the window on top of List, so this is
// the test that keeps the two paths honest about ANDing.
func TestParameterFiltersCombineWithWindow(t *testing.T) {
	db := newDB(t)
	svc := NewService(db)
	booked := makeFacility(t, db, "Booked Big Hall", 200)
	free := makeFacility(t, db, "Free Big Hall", 200)
	makeFacility(t, db, "Free Small Room", 10)
	equip(t, db, booked, "Projector")
	equip(t, db, free, "Projector")

	from := time.Date(2026, 7, 22, 14, 0, 0, 0, time.Local)
	to := from.Add(3 * time.Hour)
	if err := db.Create(&domain.Booking{FacilityID: booked.ID, UserID: makeBooker(t, db).ID,
		Status: domain.StatusConfirmed, StartsAt: from, EndsAt: to}).Error; err != nil {
		t.Fatal(err)
	}

	got, err := svc.Search(context.Background(), Filter{MinCapacity: 50, Accessories: []string{"Projector"}}, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Free Big Hall" {
		t.Fatalf("expected only the free 200-seat room with a projector, got %v", got)
	}
}
