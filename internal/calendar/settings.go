package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

var (
	// ErrUnknownField rejects a config key the selected module doesn't define,
	// so a typo'd setting fails loudly instead of being silently stored.
	ErrUnknownField = errors.New("calendar: unknown configuration field")
	// ErrMissingField reports a required config value the admin left blank.
	ErrMissingField = errors.New("calendar: required configuration field is missing")
)

// DefaultKind is what the app runs with until a municipality chooses otherwise:
// the one-way behaviour that has always shipped.
const DefaultKind = KindICS

// Settings is the current calendar integration as the API reports it.
//
// Selected is what the municipality picked; Effective is what the app is
// actually running. They differ when a two-way module has been selected but is
// not built or credentialed yet — the choice is recorded (which is the point of
// the admin form) while behaviour falls back to the one-way default rather than
// silently dropping bookings.
type Settings struct {
	Selected      Kind              `json:"selected"`
	Effective     Kind              `json:"effective"`
	Config        map[string]string `json:"config"`
	Connected     bool              `json:"connected"`
	UpdatedByID   string            `json:"updatedById,omitempty"`
	FallbackNotes string            `json:"fallbackNotes,omitempty"`
}

// Service reads and writes the municipality's calendar-module choice.
type Service struct {
	db    *gorm.DB
	audit auditlog.Recorder
}

// NewService constructs the calendar settings service.
func NewService(db *gorm.DB, audit auditlog.Recorder) *Service {
	return &Service{db: db, audit: audit}
}

// Get returns the current settings, defaulting to the one-way ICS module when
// nothing has been chosen yet.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	// Find (not First) so the common "nothing chosen yet" case is an empty result
	// rather than a logged ErrRecordNotFound on every read.
	var rows []domain.CalendarIntegration
	if err := s.db.WithContext(ctx).Limit(1).Find(&rows, "id = ?", domain.CalendarIntegrationID).Error; err != nil {
		return Settings{}, err
	}
	if len(rows) == 0 {
		return settingsFor(DefaultKind, nil, ""), nil
	}
	row := rows[0]
	kind := Kind(row.Kind)
	if _, ok := ModuleFor(kind); !ok {
		// A row naming a module this build no longer registers: fall back rather
		// than fail, so an unknown value can never take the app down.
		return settingsFor(DefaultKind, nil, row.UpdatedByID), nil
	}
	return settingsFor(kind, decodeConfig(row.Config), row.UpdatedByID), nil
}

// Set records the chosen module after validating it against that module's
// declared fields, and writes an audit trail — changing how the city calendar is
// fed is exactly the kind of staff action §6 requires logged.
func (s *Service) Set(ctx context.Context, kind Kind, config map[string]string, actor domain.User) (Settings, error) {
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
	row := domain.CalendarIntegration{
		Base:        domain.Base{ID: domain.CalendarIntegrationID},
		Kind:        string(kind),
		Config:      string(encoded),
		UpdatedByID: actor.ID,
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return Settings{}, err
	}

	s.record(ctx, actor, kind, mod)
	return settingsFor(kind, clean, actor.ID), nil
}

// Provider constructs the module the app should actually use. A selected but
// unbuilt two-way module falls back to the default so bookings keep working.
func (s *Service) Provider(ctx context.Context) Provider {
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

// settingsFor assembles the reported settings for a chosen kind, resolving what
// the app can actually run.
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
			"%s is recorded as the municipality's choice but is not connected yet; the app continues to publish one-way (.ics invite + city feed) until that module is built.",
			mod.Name)
	}
	return set
}

// validateConfig drops blank values, rejects keys the module doesn't declare,
// and enforces its required fields.
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

// record writes the durable local audit row and mirrors it to the central
// audit-logging service, matching how other staff actions are recorded.
func (s *Service) record(ctx context.Context, actor domain.User, kind Kind, mod Module) {
	detail := fmt.Sprintf("calendar module set to %s (%s)", mod.Name, kind)
	_ = s.db.WithContext(ctx).Create(&domain.AuditLog{
		ActorID: actor.ID, Action: "calendar.settings.update",
		TargetType: "calendar", TargetID: string(kind), Detail: detail,
	}).Error
	if s.audit != nil {
		s.audit.Record(auditlog.Event{
			Action: "calendar.settings.update", ActorID: actor.ID, ActorEmail: actor.Email,
			TargetType: "calendar", TargetID: string(kind), Message: detail,
		})
	}
}
