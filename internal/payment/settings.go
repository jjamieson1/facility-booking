package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// DefaultKind is what the app charges through until a municipality chooses
// otherwise: the simulated gateway, which needs no keys.
const DefaultKind = KindMock

// Settings is the current payment integration as the API reports it.
//
// Selected is what the municipality picked; Effective is what the app will
// actually charge through. They differ when a module has been selected but is
// not built yet — the choice is recorded without silently breaking payments.
type Settings struct {
	Selected      Kind              `json:"selected"`
	Effective     Kind              `json:"effective"`
	Config        map[string]string `json:"config"`
	Connected     bool              `json:"connected"`
	UpdatedByID   string            `json:"updatedById,omitempty"`
	FallbackNotes string            `json:"fallbackNotes,omitempty"`
}

// SettingsService reads and writes the municipality's payment-module choice.
type SettingsService struct {
	db    *gorm.DB
	audit auditlog.Recorder
}

// NewSettingsService constructs the payment settings service.
func NewSettingsService(db *gorm.DB, audit auditlog.Recorder) *SettingsService {
	return &SettingsService{db: db, audit: audit}
}

// Get returns the current settings, defaulting to the simulated gateway.
func (s *SettingsService) Get(ctx context.Context) (Settings, error) {
	// Find (not First) so "nothing chosen yet" is an empty result rather than a
	// logged ErrRecordNotFound on every read.
	var rows []domain.PaymentIntegration
	if err := s.db.WithContext(ctx).Limit(1).Find(&rows, "id = ?", domain.PaymentIntegrationID).Error; err != nil {
		return Settings{}, err
	}
	if len(rows) == 0 {
		return settingsFor(DefaultKind, nil, ""), nil
	}
	row := rows[0]
	kind := Kind(row.Kind)
	if _, ok := ModuleFor(kind); !ok {
		// A row naming a module this build no longer registers: fall back rather
		// than fail, so an unknown value can never take payments down.
		return settingsFor(DefaultKind, nil, row.UpdatedByID), nil
	}
	return settingsFor(kind, decodeConfig(row.Config), row.UpdatedByID), nil
}

// Set records the chosen module after validating it against that module's
// declared fields, and audits the change — switching the gateway a municipality
// takes money through is exactly the kind of action §6 requires logged.
func (s *SettingsService) Set(ctx context.Context, kind Kind, config map[string]string, actor domain.User) (Settings, error) {
	mod, ok := ModuleFor(kind)
	if !ok {
		return Settings{}, ErrUnknownKind
	}
	clean, err := validateConfig(mod, config)
	if err != nil {
		return Settings{}, err
	}

	encoded, err := json.Marshal(clean)
	if err != nil {
		return Settings{}, err
	}
	row := domain.PaymentIntegration{
		Base:        domain.Base{ID: domain.PaymentIntegrationID},
		Kind:        string(kind),
		Config:      string(encoded),
		UpdatedByID: actor.ID,
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return Settings{}, err
	}

	detail := fmt.Sprintf("payment module set to %s (%s)", mod.Name, kind)
	_ = s.db.WithContext(ctx).Create(&domain.AuditLog{
		ActorID: actor.ID, Action: "payment.settings.update",
		TargetType: "payment", TargetID: string(kind), Detail: detail,
	}).Error
	if s.audit != nil {
		s.audit.Record(auditlog.Event{
			Action: "payment.settings.update", ActorID: actor.ID, ActorEmail: actor.Email,
			TargetType: "payment", TargetID: string(kind), Message: detail,
		})
	}
	return settingsFor(kind, clean, actor.ID), nil
}

// Provider constructs the module the app should actually charge through. A
// selected-but-unbuilt module falls back to the default so the payment path
// keeps working.
func (s *SettingsService) Provider(ctx context.Context) Provider {
	set, err := s.Get(ctx)
	if err != nil {
		p, _ := New(DefaultKind, nil)
		return p
	}
	p, err := New(set.Effective, set.Config)
	if err != nil {
		p, _ = New(DefaultKind, nil)
	}
	return p
}

func settingsFor(kind Kind, config map[string]string, updatedBy string) Settings {
	if config == nil {
		config = map[string]string{}
	}
	mod, _ := ModuleFor(kind)
	set := Settings{
		Selected:    kind,
		Effective:   kind,
		Config:      config,
		Connected:   mod.Available,
		UpdatedByID: updatedBy,
	}
	if !mod.Available {
		set.Effective = DefaultKind
		set.FallbackNotes = fmt.Sprintf(
			"%s is recorded as the municipality's choice but is not connected yet; payments continue through the simulated gateway until that module is built. Do not take real payments in this state.",
			mod.Name)
	}
	return set
}

func validateConfig(mod Module, in map[string]string) (map[string]string, error) {
	allowed := make(map[string]bool, len(mod.Fields))
	for _, f := range mod.Fields {
		allowed[f.Key] = true
	}
	clean := map[string]string{}
	for k, v := range in {
		if !allowed[k] {
			return nil, fmt.Errorf("%w: %s", ErrUnknownField, k)
		}
		if v = strings.TrimSpace(v); v != "" {
			clean[k] = v
		}
	}
	for _, f := range mod.Fields {
		if f.Required && clean[f.Key] == "" {
			return nil, fmt.Errorf("%w: %s", ErrMissingField, f.Label)
		}
	}
	return clean, nil
}

func decodeConfig(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
