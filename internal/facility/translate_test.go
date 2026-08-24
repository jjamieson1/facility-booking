package facility

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func seedTranslated(t *testing.T, db *gorm.DB) domain.Facility {
	t.Helper()
	f := domain.Facility{
		Name: "Community Hall", Description: "A large hall.",
		BeforeInstructions: "Collect keys.", AfterInstructions: "Lock up.",
		Location: "120 Riverside Ave",
	}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

// AC: switching to French shows French content.
func TestTranslateAppliesFrench(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	f := seedTranslated(t, db)
	db.Create(&domain.FacilityTranslation{
		FacilityID: f.ID, Language: domain.LangFR,
		Name: "Salle communautaire", Description: "Une grande salle.",
		BeforeInstructions: "Récupérez les clés.", AfterInstructions: "Verrouillez.",
	})

	got := f
	missing, err := svc.TranslateOne(context.Background(), domain.LangFR, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Salle communautaire" || got.Description != "Une grande salle." {
		t.Fatalf("not translated: %+v", got)
	}
	if len(missing) != 0 {
		t.Errorf("nothing should be reported missing, got %v", missing)
	}
	// The address is not translatable and must survive untouched.
	if got.Location != "120 Riverside Ave" {
		t.Errorf("location = %q — an address is not translated", got.Location)
	}
}

// AC: a missing field falls back to the default language AND says so, rather
// than showing a blank or passing English off as French.
func TestPartialTranslationFallsBackPerField(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	f := seedTranslated(t, db)
	db.Create(&domain.FacilityTranslation{
		FacilityID: f.ID, Language: domain.LangFR,
		Name: "Salle communautaire", // description and instructions left empty
	})

	got := f
	missing, err := svc.TranslateOne(context.Background(), domain.LangFR, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Salle communautaire" {
		t.Errorf("name should be French, got %q", got.Name)
	}
	if got.Description != "A large hall." {
		t.Errorf("description should fall back to English, got %q", got.Description)
	}
	if len(missing) != 3 {
		t.Fatalf("missing = %v, want the three untranslated fields reported", missing)
	}
}

// A facility with no translation at all reports every field as untranslated.
func TestUntranslatedFacilityIsReported(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	f := seedTranslated(t, db)

	got := f
	missing, err := svc.TranslateOne(context.Background(), domain.LangFR, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Community Hall" {
		t.Errorf("should fall back to English, got %q", got.Name)
	}
	if len(missing) != 4 {
		t.Errorf("missing = %v, want all four fields", missing)
	}
}

// English readers get the base row and pay for no extra lookups.
func TestDefaultLanguageIsUntouched(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	f := seedTranslated(t, db)
	db.Create(&domain.FacilityTranslation{FacilityID: f.ID, Language: domain.LangFR, Name: "Salle"})

	got := f
	missing, err := svc.TranslateOne(context.Background(), domain.LangEN, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Community Hall" || len(missing) != 0 {
		t.Fatalf("English reader should see the base row unchanged: %+v %v", got, missing)
	}
}

// Saving the default language writes the base row, not a second copy — two
// copies of the English text would drift with nothing saying which is right.
func TestSavingDefaultLanguageUpdatesTheBaseRow(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	f := seedTranslated(t, db)

	err := svc.SaveTranslation(context.Background(), domain.FacilityTranslation{
		FacilityID: f.ID, Language: domain.LangEN, Name: "Renamed Hall", Description: "Updated.",
	})
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := svc.Get(context.Background(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Name != "Renamed Hall" {
		t.Errorf("base row not updated: %q", reloaded.Name)
	}
	var extra int64
	db.Model(&domain.FacilityTranslation{}).Where("facility_id = ?", f.ID).Count(&extra)
	if extra != 0 {
		t.Errorf("%d translation rows written for the default language, want 0", extra)
	}
}

// Saving French twice updates rather than duplicating.
func TestSaveTranslationUpserts(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	f := seedTranslated(t, db)
	ctx := context.Background()

	for _, name := range []string{"Salle", "Salle communautaire"} {
		if err := svc.SaveTranslation(ctx, domain.FacilityTranslation{
			FacilityID: f.ID, Language: domain.LangFR, Name: name,
		}); err != nil {
			t.Fatal(err)
		}
	}
	var rows []domain.FacilityTranslation
	db.Find(&rows, "facility_id = ? AND language = ?", f.ID, domain.LangFR)
	if len(rows) != 1 {
		t.Fatalf("%d French rows, want 1 (upsert, not duplicate)", len(rows))
	}
	if rows[0].Name != "Salle communautaire" {
		t.Errorf("name = %q, want the latest value", rows[0].Name)
	}
}

// The editor sees every language, including the default read off the base row.
func TestTranslationsIncludeTheDefaultLanguage(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	f := seedTranslated(t, db)
	db.Create(&domain.FacilityTranslation{FacilityID: f.ID, Language: domain.LangFR, Name: "Salle"})

	out, err := svc.Translations(context.Background(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	langs := map[domain.Language]string{}
	for _, tr := range out {
		langs[tr.Language] = tr.Name
	}
	if langs[domain.LangEN] != "Community Hall" || langs[domain.LangFR] != "Salle" {
		t.Fatalf("editor tabs = %+v, want both languages", langs)
	}
}

// Accessory names are translated too — they appear on every facility page.
func TestAccessoryNamesAreTranslated(t *testing.T) {
	db := testdb.New(t)
	svc := NewService(db)
	f := seedTranslated(t, db)
	a := domain.Accessory{Name: "Projector"}
	db.Create(&a)
	db.Create(&domain.FacilityAccessory{FacilityID: f.ID, AccessoryID: a.ID, Quantity: 1})
	db.Create(&domain.AccessoryTranslation{AccessoryID: a.ID, Language: domain.LangFR, Name: "Projecteur"})

	loaded, err := svc.Get(context.Background(), f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TranslateOne(context.Background(), domain.LangFR, loaded); err != nil {
		t.Fatal(err)
	}
	if len(loaded.Accessories) != 1 || loaded.Accessories[0].Accessory.Name != "Projecteur" {
		t.Fatalf("accessory not translated: %+v", loaded.Accessories)
	}
}
