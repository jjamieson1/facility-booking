// Package seed populates the database with believable "Rivermont" demo data so
// every screen looks alive in a sales demo — including a year of booking history
// that drives the reporting dashboard. It is idempotent (no-ops if facilities
// already exist) and deterministic (a fixed RNG seed) so reseeds are stable.
package seed

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// historyBookings is how many past bookings to generate across the year. Tuned
// to populate the dashboard richly (the mockup shows ~612); dial down for a
// lighter demo.
const historyBookings = 600

// seedCancellationPolicy writes the municipality-wide default (§4.7). Seeded
// rather than left to policy.DefaultPolicy() so staff can see and edit the terms
// in the back-office instead of discovering them in code — a policy nobody can
// find is a policy nobody can change.
func seedCancellationPolicy(tx *gorm.DB) error {
	var existing int64
	if err := tx.Model(&domain.CancellationPolicy{}).Where("facility_id IS NULL").Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil // idempotent, like the rest of the seed
	}
	p := domain.CancellationPolicy{
		Name:                    "Municipal default",
		ModificationCutoffHours: 24,
	}
	if err := tx.Create(&p).Error; err != nil {
		return err
	}
	for _, t := range []domain.RefundTier{
		{HoursBefore: 168, RefundPercent: 100}, // a week or more: full refund
		{HoursBefore: 48, RefundPercent: 50},   // two days to a week: half
	} {
		t.PolicyID = p.ID
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
	}
	return nil
}

// MunicipalRoll is Rivermont's address roll — the streets inside the municipal
// boundary. The residency entitlement provider checks a submitted address
// against this; anything else is a non-resident, so residency is decided here
// rather than accepted from the booker.
//
// A real deployment loads this from the city's roll or asks a provider; the
// point of the interface is that swapping the source changes nothing else.
func MunicipalRoll() []string {
	return []string{
		"Riverside Ave",
		"Rink Rd",
		"Willow Lane",
		"Mill St",
		"Bridge St",
		"Elm Crescent",
		"Park Row",
		"Station Rd",
		"Cedar Way",
		"Harbour Dr",
	}
}

// Run seeds demo data when the facilities table is empty.
//
// The cancellation policy is seeded separately, on every boot, because it is not
// demo data: it is configuration the municipality is expected to edit. Gating it
// behind "no facilities yet" would mean an existing deployment silently ran on
// the built-in fallback, which staff cannot see or change in the back-office.
// Both steps are idempotent.
func Run(db *gorm.DB) error {
	if err := db.Transaction(seedCancellationPolicy); err != nil {
		return err
	}

	var count int64
	if err := db.Model(&domain.Facility{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return db.Transaction(seed)
}

// facilitySpec bundles a facility with its accessories and a popularity weight
// used when generating history (higher = more bookings).
type facilitySpec struct {
	f      domain.Facility
	items  map[string]int
	weight int
}

func seed(tx *gorm.DB) error {
	accessories := map[string]*domain.Accessory{}
	for _, name := range []string{"Projector", "Screen", "Sound system", "Wi-Fi", "Coffee maker", "Chairs", "Tables", "Kitchen", "Ice resurfacer"} {
		a := &domain.Accessory{Name: name}
		if err := tx.Create(a).Error; err != nil {
			return err
		}
		accessories[name] = a
	}

	staff, bookers, err := seedUsers(tx)
	if err != nil {
		return err
	}
	_ = staff

	facilities, err := seedFacilities(tx, accessories)
	if err != nil {
		return err
	}

	return seedHistory(tx, facilities, bookers)
}

// nowFn is overridable in tests; production uses time.Now.
var nowFn = time.Now

// seedUsers creates the demo staff account plus a pool of resident and
// non-resident bookers (~70% resident, to drive the report split).
func seedUsers(tx *gorm.DB) (staff *domain.User, bookers []*domain.User, err error) {
	staff = &domain.User{Subject: "demo:admin", Email: "admin@rivermont.demo", Name: "Rivermont Admin", Role: domain.RoleAdmin, IsResident: true}
	primary := &domain.User{Subject: "demo:resident", Email: "resident@rivermont.demo", Name: "Jordan Rivers", Role: domain.RoleResident, IsResident: true}
	if err = tx.Create(staff).Error; err != nil {
		return
	}
	if err = tx.Create(primary).Error; err != nil {
		return
	}
	bookers = append(bookers, primary)

	names := []string{
		"Avery Chen", "Sam Okafor", "Priya Patel", "Liam Murphy", "Nadia Haddad",
		"Diego Torres", "Grace Kim", "Owen Byrne", "Mia Rossi", "Noah Schmidt",
		"Elena Popov", "Marcus Webb",
	}
	for i, name := range names {
		resident := i%10 < 7 // ~70% residents
		u := &domain.User{
			Subject: fmt.Sprintf("demo:booker%d", i), Email: fmt.Sprintf("booker%d@rivermont.demo", i),
			Name: name, Role: domain.RoleResident, IsResident: resident,
		}
		if err = tx.Create(u).Error; err != nil {
			return
		}
		bookers = append(bookers, u)
	}
	return staff, bookers, nil
}

func seedFacilities(tx *gorm.DB, accessories map[string]*domain.Accessory) ([]facilitySpec, error) {
	specs := []facilitySpec{
		{weight: 30, items: map[string]int{"Sound system": 1, "Wi-Fi": 1, "Chairs": 200, "Tables": 30, "Kitchen": 1, "Coffee maker": 2}, f: domain.Facility{
			Name: "Rivermont Community Hall", Capacity: 200, FeeCents: 15000, NonResidentFeeCents: 22500, DepositCents: 5000,
			Location: "120 Riverside Ave", Latitude: 44.2312, Longitude: -76.4860, ImageURL: "https://images.unsplash.com/photo-1517457373958-b7bdd4587205?w=800",
			Description:      "A large hall for weddings, markets, and community events, with an attached kitchen.",
			RequiresApproval: true, MinMinutes: 120, MaxMinutes: 600, BufferMinutes: 30, StepFreeAccess: true, AccessibleWashroom: true,
			BeforeInstructions: "Collect keys from the front desk. Tables are stacked in the rear closet.",
			AfterInstructions:  "Stack chairs, wipe tables, take waste to the bins outside, lock all doors.",
		}},
		{weight: 26, items: map[string]int{"Ice resurfacer": 1, "Sound system": 1, "Wi-Fi": 1}, f: domain.Facility{
			Name: "Rivermont Ice Arena", Capacity: 250, FeeCents: 12000, NonResidentFeeCents: 18000, DepositCents: 4000,
			Location: "85 Rink Rd", Latitude: 44.2340, Longitude: -76.4902, ImageURL: "https://images.unsplash.com/photo-1580692475446-c2fabbbb2069?w=800",
			Description:      "A full-size ice rink for hockey, skating, and tournaments.",
			RequiresApproval: true, MinMinutes: 60, MaxMinutes: 240, BufferMinutes: 30, StepFreeAccess: true, AccessibleWashroom: true,
			BeforeInstructions: "Check in at the arena office; skate aid available on request.",
			AfterInstructions:  "Clear the ice and benches; report any surface damage.",
		}},
		{weight: 22, items: map[string]int{}, f: domain.Facility{
			Name: "Cedar Playing Field", Capacity: 100, FeeCents: 0,
			Location: "Rivermont Park, Field 2", Latitude: 44.2280, Longitude: -76.4795, ImageURL: "https://images.unsplash.com/photo-1459865264687-595d652de67e?w=800",
			Description:      "A full-size sports field for soccer and community games. Free to book.",
			RequiresApproval: false, MinMinutes: 60, MaxMinutes: 240, BufferMinutes: 0, StepFreeAccess: true,
			BeforeInstructions: "Gates open 15 minutes before your slot.",
			AfterInstructions:  "Remove all equipment and litter; no cars on the grass.",
		}},
		{weight: 15, items: map[string]int{"Tables": 8, "Wi-Fi": 1}, f: domain.Facility{
			Name: "Willow Park Pavilion", Capacity: 40, FeeCents: 6000, NonResidentFeeCents: 9000, DepositCents: 2000,
			Location: "Rivermont Park, North Lawn", Latitude: 44.2295, Longitude: -76.4810, ImageURL: "https://images.unsplash.com/photo-1506905925346-21bda4d32df4?w=800",
			Description:      "A covered park pavilion for picnics, birthdays, and gatherings.",
			RequiresApproval: true, MinMinutes: 120, MaxMinutes: 480, BufferMinutes: 30, StepFreeAccess: true, AccessibleWashroom: true,
			BeforeInstructions: "Picnic tables are on site. Power outlets are by the north post.",
			AfterInstructions:  "Bag all waste and place it in the park bins.",
		}},
		{weight: 14, items: map[string]int{"Projector": 1, "Screen": 1, "Sound system": 1, "Wi-Fi": 1, "Chairs": 80}, f: domain.Facility{
			Name: "Assembly Room", Capacity: 80, FeeCents: 8000, NonResidentFeeCents: 12000, DepositCents: 2000,
			Location: "120 Riverside Ave, main floor", Latitude: 44.2311, Longitude: -76.4855, ImageURL: "https://images.unsplash.com/photo-1524178232363-1fb2b075b655?w=800",
			Description:      "A tiered assembly room for talks, AGMs, and presentations.",
			RequiresApproval: false, MinMinutes: 60, MaxMinutes: 300, BufferMinutes: 15, StepFreeAccess: true, AccessibleWashroom: true,
			BeforeInstructions: "AV desk is at the back; the access code is emailed the morning of.",
			AfterInstructions:  "Power down the AV desk and return chairs to rows.",
		}},
		{weight: 12, items: map[string]int{"Wi-Fi": 1, "Chairs": 20}, f: domain.Facility{
			Name: "Maple Meeting Room", Capacity: 20, FeeCents: 4000, NonResidentFeeCents: 6000,
			Location: "120 Riverside Ave, 2nd floor", Latitude: 44.2314, Longitude: -76.4858, ImageURL: "https://images.unsplash.com/photo-1497366216548-37526070297c?w=800",
			Description:      "A bright boardroom for meetings and workshops.",
			RequiresApproval: false, RequiresWaiver: true, MinMinutes: 60, MaxMinutes: 480, BufferMinutes: 15, StepFreeAccess: true, AccessibleWashroom: true,
			BeforeInstructions: "Access code is sent by email the morning of your booking.",
			AfterInstructions:  "Return the room to its default layout and switch off the display.",
		}},
		{weight: 10, items: map[string]int{"Sound system": 1, "Wi-Fi": 1}, f: domain.Facility{
			Name: "Dance Studio", Capacity: 30, FeeCents: 5000, NonResidentFeeCents: 7500,
			Location: "85 Rink Rd, studio B", Latitude: 44.2338, Longitude: -76.4899, ImageURL: "https://images.unsplash.com/photo-1508700115892-45ecd05ae2ad?w=800",
			Description:      "A mirrored studio with sprung floor for dance, yoga, and fitness classes.",
			RequiresApproval: false, MinMinutes: 60, MaxMinutes: 180, BufferMinutes: 15, StepFreeAccess: true, AccessibleWashroom: true,
			BeforeInstructions: "Indoor shoes only. Speakers connect over Bluetooth.",
			AfterInstructions:  "Wipe down mirrors and barres; stack any mats used.",
		}},
	}

	out := make([]facilitySpec, 0, len(specs)+2)
	var hallID string
	for _, spec := range specs {
		f := spec.f
		if err := tx.Create(&f).Error; err != nil {
			return nil, err
		}
		for name, qty := range spec.items {
			if err := tx.Create(&domain.FacilityAccessory{FacilityID: f.ID, AccessoryID: accessories[name].ID, Quantity: qty}).Error; err != nil {
				return nil, err
			}
		}
		if err := seedWeekdayHours(tx, f.ID); err != nil {
			return nil, err
		}
		spec.f = f // capture the generated ID
		if f.Name == "Rivermont Community Hall" {
			hallID = f.ID
		}
		out = append(out, spec)
	}

	halves, err := seedDividedHall(tx, accessories, hallID)
	if err != nil {
		return nil, err
	}
	return append(out, halves...), nil
}

// seedDividedHall splits the community hall into two bookable halves.
//
// This exists to make hierarchy-aware conflict detection visible: booking the
// whole hall must block both halves, and booking a half must block the hall
// (`facility.ConflictSet`). Without a parent/child pair in the data, that code
// path never runs outside the tests — which is exactly why the double-booking
// defect went unnoticed for so long.
func seedDividedHall(tx *gorm.DB, accessories map[string]*domain.Accessory, hallID string) ([]facilitySpec, error) {
	if hallID == "" {
		return nil, nil // the hall spec was renamed; nothing to divide
	}
	halves := []facilitySpec{
		{weight: 8, items: map[string]int{"Chairs": 100, "Wi-Fi": 1}, f: domain.Facility{
			Name: "Community Hall — North Half", Capacity: 100, FeeCents: 9000, NonResidentFeeCents: 13500, DepositCents: 3000,
			Location: "120 Riverside Ave", Latitude: 44.2312, Longitude: -76.4860,
			Description:      "Half of the community hall, divided by the retractable partition. Booking the whole hall books this too.",
			RequiresApproval: true, MinMinutes: 120, MaxMinutes: 600, BufferMinutes: 30, StepFreeAccess: true, AccessibleWashroom: true,
			BeforeInstructions: "The partition is drawn by staff before your slot — confirm at the front desk.",
			AfterInstructions:  "Stack chairs against the partition wall and wipe tables.",
		}},
		{weight: 8, items: map[string]int{"Chairs": 100, "Kitchen": 1, "Coffee maker": 2}, f: domain.Facility{
			Name: "Community Hall — South Half", Capacity: 100, FeeCents: 10000, NonResidentFeeCents: 15000, DepositCents: 3000,
			Location: "120 Riverside Ave", Latitude: 44.2312, Longitude: -76.4860,
			Description:      "Half of the community hall, with the attached kitchen. Booking the whole hall books this too.",
			RequiresApproval: true, MinMinutes: 120, MaxMinutes: 600, BufferMinutes: 30, StepFreeAccess: true, AccessibleWashroom: true,
			BeforeInstructions: "Kitchen keys are collected from the front desk with the hall keys.",
			AfterInstructions:  "Empty the fridge, run the dishwasher, and take waste to the outside bins.",
		}},
	}

	out := make([]facilitySpec, 0, len(halves))
	for _, spec := range halves {
		f := spec.f
		f.ParentID = &hallID
		if err := tx.Create(&f).Error; err != nil {
			return nil, err
		}
		for name, qty := range spec.items {
			if err := tx.Create(&domain.FacilityAccessory{FacilityID: f.ID, AccessoryID: accessories[name].ID, Quantity: qty}).Error; err != nil {
				return nil, err
			}
		}
		if err := seedWeekdayHours(tx, f.ID); err != nil {
			return nil, err
		}
		spec.f = f
		out = append(out, spec)
	}
	return out, nil
}

// seedWeekdayHours opens each facility 08:00–22:00, every day.
func seedWeekdayHours(tx *gorm.DB, facilityID string) error {
	const open, close = 8 * 60, 22 * 60
	for weekday := 0; weekday <= 6; weekday++ {
		if err := tx.Create(&domain.AvailabilityRule{FacilityID: facilityID, Weekday: weekday, OpenMinute: open, CloseMinute: close}).Error; err != nil {
			return err
		}
	}
	return nil
}
