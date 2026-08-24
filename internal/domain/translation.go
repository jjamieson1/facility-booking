package domain

import "strings"

// Language is one of the municipality's official languages. Canada requires
// English and French (§4.11); the set is deliberately closed, because a
// translation in a language the app cannot serve is worse than none — it looks
// stored but never reaches anyone.
type Language string

const (
	LangEN Language = "en"
	LangFR Language = "fr"
)

// DefaultLanguage is the language the base columns on Facility and Accessory
// hold, and the fallback when a translation is missing.
const DefaultLanguage = LangEN

// NormalizeLanguage maps anything user- or header-supplied onto a language the
// app can actually serve, defaulting to English.
func NormalizeLanguage(s string) Language {
	switch {
	case strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "fr"):
		return LangFR
	default:
		return LangEN
	}
}

// FacilityTranslation holds one facility's text in one non-default language.
//
// A side table rather than `NameFr`/`DescriptionFr` columns: the column-per-
// language shape multiplies every new translatable field by every language, and
// makes "which fields lack a French version" a query over columns rather than
// rows. The base Facility row keeps the default-language text, so every existing
// read path still works untouched.
//
// Location is deliberately absent — a street address is not translated.
type FacilityTranslation struct {
	Base
	FacilityID         string   `gorm:"type:varchar(36);index:idx_ftrans,unique" json:"facilityId"`
	Language           Language `gorm:"type:varchar(5);index:idx_ftrans,unique" json:"language"`
	Name               string   `gorm:"type:varchar(200)" json:"name"`
	Description        string   `gorm:"type:text" json:"description"`
	BeforeInstructions string   `gorm:"type:text" json:"beforeInstructions"`
	AfterInstructions  string   `gorm:"type:text" json:"afterInstructions"`
}

// AccessoryTranslation is the same for the accessory vocabulary ("Projector",
// "Chairs"), which appears on every facility page.
type AccessoryTranslation struct {
	Base
	AccessoryID string   `gorm:"type:varchar(36);index:idx_atrans,unique" json:"accessoryId"`
	Language    Language `gorm:"type:varchar(5);index:idx_atrans,unique" json:"language"`
	Name        string   `gorm:"type:varchar(120)" json:"name"`
}

// Untranslated lists the fields with no value in this translation, so the API
// can tell a reader which text they are seeing in the other language rather
// than silently serving English under a French heading.
func (t FacilityTranslation) Untranslated() []string {
	var missing []string
	for name, value := range map[string]string{
		"name":               t.Name,
		"description":        t.Description,
		"beforeInstructions": t.BeforeInstructions,
		"afterInstructions":  t.AfterInstructions,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

// ApplyTranslation overlays a translation onto a facility, field by field.
//
// Falling back per field rather than per record matters: a facility translated
// except for its after-use instructions should show French everywhere it has
// French, not revert wholesale to English because one field is missing.
func (f *Facility) ApplyTranslation(t FacilityTranslation) {
	if v := strings.TrimSpace(t.Name); v != "" {
		f.Name = v
	}
	if v := strings.TrimSpace(t.Description); v != "" {
		f.Description = v
	}
	if v := strings.TrimSpace(t.BeforeInstructions); v != "" {
		f.BeforeInstructions = v
	}
	if v := strings.TrimSpace(t.AfterInstructions); v != "" {
		f.AfterInstructions = v
	}
}
