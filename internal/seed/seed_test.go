package seed

import (
	"testing"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

// A fresh database must come up fully seeded in one boot — facilities, the
// cancellation policy, AND the French content.
//
// This has a test because the ordering was wrong once: translations ran before
// the facilities existed, so a new deployment had English-only content until
// someone restarted it. Nothing failed; the French was just missing.
func TestRunSeedsTranslationsOnFirstBoot(t *testing.T) {
	db := testdb.New(t)
	if err := Run(db); err != nil {
		t.Fatal(err)
	}

	var facilities int64
	db.Model(&domain.Facility{}).Count(&facilities)
	if facilities == 0 {
		t.Fatal("no facilities seeded")
	}

	var french int64
	db.Model(&domain.FacilityTranslation{}).Where("language = ?", domain.LangFR).Count(&french)
	if french == 0 {
		t.Fatal("first boot produced no French content — translations ran before the facilities existed")
	}
	if french != facilities {
		t.Errorf("%d French translations for %d facilities — every seeded facility should have one", french, facilities)
	}

	var accessories int64
	db.Model(&domain.AccessoryTranslation{}).Where("language = ?", domain.LangFR).Count(&accessories)
	if accessories == 0 {
		t.Error("accessory vocabulary was not translated")
	}

	var policies int64
	db.Model(&domain.CancellationPolicy{}).Where("facility_id IS NULL").Count(&policies)
	if policies != 1 {
		t.Errorf("%d municipal default policies, want exactly 1", policies)
	}
}

// Re-running must not duplicate anything, and must not overwrite text staff
// have edited.
func TestRunIsIdempotentAndDoesNotOverwriteEdits(t *testing.T) {
	db := testdb.New(t)
	if err := Run(db); err != nil {
		t.Fatal(err)
	}

	// Staff correct a translation by hand.
	var tr domain.FacilityTranslation
	if err := db.First(&tr, "language = ?", domain.LangFR).Error; err != nil {
		t.Fatal(err)
	}
	edited := "Nom corrigé par le personnel"
	db.Model(&domain.FacilityTranslation{}).Where("id = ?", tr.ID).Update("name", edited)

	before := counts(t, db)
	if err := Run(db); err != nil {
		t.Fatal(err)
	}
	if after := counts(t, db); after != before {
		t.Errorf("re-running the seed changed counts: %+v → %+v", before, after)
	}

	var reloaded domain.FacilityTranslation
	db.First(&reloaded, "id = ?", tr.ID)
	if reloaded.Name != edited {
		t.Errorf("the seed overwrote a staff edit: %q", reloaded.Name)
	}
}

type seedCounts struct{ facilities, translations, accessoryTranslations, policies int64 }

func counts(t *testing.T, db *gorm.DB) seedCounts {
	t.Helper()
	var c seedCounts
	db.Model(&domain.Facility{}).Count(&c.facilities)
	db.Model(&domain.FacilityTranslation{}).Count(&c.translations)
	db.Model(&domain.AccessoryTranslation{}).Count(&c.accessoryTranslations)
	db.Model(&domain.CancellationPolicy{}).Count(&c.policies)
	return c
}
