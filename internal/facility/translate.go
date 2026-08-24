package facility

import (
	"context"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

// Translate overlays the requested language onto facilities, per field, and
// reports what had no translation.
//
// Called after loading rather than joined into every query: the base row is
// already the default language, so English readers — the majority in most
// municipalities — pay nothing, and the read paths stay unchanged.
func (s *Service) Translate(ctx context.Context, lang domain.Language, facilities ...*domain.Facility) (map[string][]string, error) {
	missing := map[string][]string{}
	if lang == domain.DefaultLanguage || len(facilities) == 0 {
		return missing, nil // base rows already hold the default language
	}

	ids := make([]string, 0, len(facilities))
	byID := make(map[string]*domain.Facility, len(facilities))
	for _, f := range facilities {
		if f == nil {
			continue
		}
		ids = append(ids, f.ID)
		byID[f.ID] = f
	}

	var rows []domain.FacilityTranslation
	if err := s.db.WithContext(ctx).
		Find(&rows, "language = ? AND facility_id IN ?", lang, ids).Error; err != nil {
		return nil, err
	}
	translated := map[string]bool{}
	for _, t := range rows {
		if f, ok := byID[t.FacilityID]; ok {
			f.ApplyTranslation(t)
			translated[t.FacilityID] = true
			if gaps := t.Untranslated(); len(gaps) > 0 {
				missing[t.FacilityID] = gaps
			}
		}
	}
	// A facility with no row at all is entirely untranslated — the reader is
	// seeing the default language throughout and should be told so.
	for id := range byID {
		if !translated[id] {
			missing[id] = []string{"name", "description", "beforeInstructions", "afterInstructions"}
		}
	}

	if err := s.translateAccessories(ctx, lang, facilities); err != nil {
		return nil, err
	}
	return missing, nil
}

// translateAccessories renames the accessory vocabulary in place. Accessories
// are shared across facilities, so this is one lookup for the whole set.
func (s *Service) translateAccessories(ctx context.Context, lang domain.Language, facilities []*domain.Facility) error {
	ids := map[string]bool{}
	for _, f := range facilities {
		if f == nil {
			continue
		}
		for _, fa := range f.Accessories {
			ids[fa.AccessoryID] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}

	var rows []domain.AccessoryTranslation
	if err := s.db.WithContext(ctx).
		Find(&rows, "language = ? AND accessory_id IN ?", lang, list).Error; err != nil {
		return err
	}
	names := make(map[string]string, len(rows))
	for _, t := range rows {
		if t.Name != "" {
			names[t.AccessoryID] = t.Name
		}
	}
	for _, f := range facilities {
		if f == nil {
			continue
		}
		for i := range f.Accessories {
			if n, ok := names[f.Accessories[i].AccessoryID]; ok {
				f.Accessories[i].Accessory.Name = n
			}
		}
	}
	return nil
}

// Translations returns every stored translation for a facility, for the staff
// editor. The default language is returned too, read off the base row, so the
// editor shows one consistent set of tabs rather than special-casing English.
func (s *Service) Translations(ctx context.Context, facilityID string) ([]domain.FacilityTranslation, error) {
	f, err := s.Get(ctx, facilityID)
	if err != nil {
		return nil, err
	}
	out := []domain.FacilityTranslation{{
		FacilityID: f.ID, Language: domain.DefaultLanguage,
		Name: f.Name, Description: f.Description,
		BeforeInstructions: f.BeforeInstructions, AfterInstructions: f.AfterInstructions,
	}}

	var rows []domain.FacilityTranslation
	if err := s.db.WithContext(ctx).
		Order("language asc").Find(&rows, "facility_id = ?", facilityID).Error; err != nil {
		return nil, err
	}
	return append(out, rows...), nil
}

// SaveTranslation upserts one language's text for a facility.
//
// Saving the default language writes the base row instead of a translation row,
// so there is exactly one place the English text lives. Two copies of it would
// drift, and nothing would say which was authoritative.
func (s *Service) SaveTranslation(ctx context.Context, t domain.FacilityTranslation) error {
	if _, err := s.Get(ctx, t.FacilityID); err != nil {
		return err
	}
	if t.Language == domain.DefaultLanguage {
		return s.db.WithContext(ctx).Model(&domain.Facility{}).
			Where("id = ?", t.FacilityID).
			Updates(map[string]any{
				"name":                t.Name,
				"description":         t.Description,
				"before_instructions": t.BeforeInstructions,
				"after_instructions":  t.AfterInstructions,
			}).Error
	}

	var existing []domain.FacilityTranslation
	if err := s.db.WithContext(ctx).Limit(1).
		Find(&existing, "facility_id = ? AND language = ?", t.FacilityID, t.Language).Error; err != nil {
		return err
	}
	if len(existing) == 1 {
		t.ID = existing[0].ID
		return s.db.WithContext(ctx).Model(&domain.FacilityTranslation{}).
			Where("id = ?", t.ID).
			Updates(map[string]any{
				"name":                t.Name,
				"description":         t.Description,
				"before_instructions": t.BeforeInstructions,
				"after_instructions":  t.AfterInstructions,
			}).Error
	}
	return s.db.WithContext(ctx).Create(&t).Error
}

// TranslateOne is the single-facility form of Translate.
func (s *Service) TranslateOne(ctx context.Context, lang domain.Language, f *domain.Facility) ([]string, error) {
	missing, err := s.Translate(ctx, lang, f)
	if err != nil {
		return nil, err
	}
	return missing[f.ID], nil
}
