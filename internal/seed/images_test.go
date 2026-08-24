package seed

import (
	"testing"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// imageOf reads back one facility's photo by name.
func imageOf(t *testing.T, db *gorm.DB, name string) string {
	t.Helper()
	var f domain.Facility
	if err := db.Where("name = ?", name).First(&f).Error; err != nil {
		t.Fatalf("load %q: %v", name, err)
	}
	return f.ImageURL
}

// A database seeded before the photos were brought in-house still points at
// Unsplash. Booting must repair it, or the facilities page keeps showing broken
// images on every deployment that is not freshly seeded.
func TestRelocateImagesRewritesSeededOffsiteURLs(t *testing.T) {
	db := testdb.New(t)
	old := "https://images.unsplash.com/photo-1580692475446-c2fabbbb2069?w=800"
	if err := db.Create(&domain.Facility{Name: "Rivermont Ice Arena", ImageURL: old}).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Transaction(relocateImages); err != nil {
		t.Fatal(err)
	}

	if got := imageOf(t, db, "Rivermont Ice Arena"); got != "facilities/ice-arena.jpg" {
		t.Fatalf("image not relocated: got %q", got)
	}
}

// The halves shipped with no photo at all, so they are matched by name — but
// only while empty, so a later re-run cannot undo an operator's choice.
func TestRelocateImagesFillsEmptyHalfPhotos(t *testing.T) {
	db := testdb.New(t)
	for _, name := range []string{"Community Hall — North Half", "Community Hall — South Half"} {
		if err := db.Create(&domain.Facility{Name: name}).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Transaction(relocateImages); err != nil {
		t.Fatal(err)
	}

	if got := imageOf(t, db, "Community Hall — North Half"); got != "facilities/hall-north.jpg" {
		t.Fatalf("north half: got %q", got)
	}
	if got := imageOf(t, db, "Community Hall — South Half"); got != "facilities/hall-south.jpg" {
		t.Fatalf("south half: got %q", got)
	}
}

// An operator who pointed a facility at the city's own CDN through the
// back-office must survive every subsequent boot. This is the case that makes
// the rewrite safe to run unconditionally.
func TestRelocateImagesLeavesOperatorChoicesAlone(t *testing.T) {
	db := testdb.New(t)
	custom := "https://cdn.rivermont.example/arena.jpg"
	if err := db.Create(&domain.Facility{Name: "Rivermont Ice Arena", ImageURL: custom}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Facility{Name: "Community Hall — North Half", ImageURL: custom}).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Transaction(relocateImages); err != nil {
		t.Fatal(err)
	}

	if got := imageOf(t, db, "Rivermont Ice Arena"); got != custom {
		t.Fatalf("overwrote operator URL: got %q", got)
	}
	if got := imageOf(t, db, "Community Hall — North Half"); got != custom {
		t.Fatalf("overwrote operator URL on half: got %q", got)
	}
}

// Area arrived after these rows were written, so an existing demo database has
// none — leaving the §4.3 area filter with an empty dropdown that reads as
// broken rather than unused.
func TestBackfillAreasFillsSeededFacilities(t *testing.T) {
	db := testdb.New(t)
	if err := db.Create(&domain.Facility{Name: "Rivermont Ice Arena"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Transaction(backfillAreas); err != nil {
		t.Fatal(err)
	}

	var f domain.Facility
	if err := db.Where("name = ?", "Rivermont Ice Arena").First(&f).Error; err != nil {
		t.Fatal(err)
	}
	if f.Area != "Rink Road" {
		t.Fatalf("area not backfilled: got %q", f.Area)
	}
}

// Staff who have already placed a facility keep their choice; the backfill runs
// on every boot, so it must only ever fill a gap.
func TestBackfillAreasLeavesStaffChoiceAlone(t *testing.T) {
	db := testdb.New(t)
	if err := db.Create(&domain.Facility{Name: "Rivermont Ice Arena", Area: "Ward 3"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Transaction(backfillAreas); err != nil {
		t.Fatal(err)
	}

	var f domain.Facility
	if err := db.Where("name = ?", "Rivermont Ice Arena").First(&f).Error; err != nil {
		t.Fatal(err)
	}
	if f.Area != "Ward 3" {
		t.Fatalf("overwrote staff choice: got %q", f.Area)
	}
}

// Every facility the seed creates must carry a photo — the placeholder is a
// fallback for bad data, not the intended look of the demo.
func TestSeededFacilitiesAllCarryLocalPhotos(t *testing.T) {
	db := testdb.New(t)
	if err := Run(db); err != nil {
		t.Fatal(err)
	}

	var facilities []domain.Facility
	if err := db.Find(&facilities).Error; err != nil {
		t.Fatal(err)
	}
	if len(facilities) == 0 {
		t.Fatal("seed created no facilities")
	}
	for _, f := range facilities {
		if f.ImageURL == "" {
			t.Errorf("%s has no photo", f.Name)
		}
		// An unplaced facility is invisible to the §4.3 area filter.
		if f.Area == "" {
			t.Errorf("%s has no area", f.Name)
		}
	}
}
