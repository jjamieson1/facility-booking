package domain

// CalendarIntegrationID is the fixed primary key of the single calendar-integration
// row. A constant id keeps the setting a true singleton: every write upserts the
// same row, so there is no way to end up with two competing configurations.
const CalendarIntegrationID = "calendar-integration"

// CalendarIntegration records which calendar module the municipality selected in
// the staff back-office (§4.6). Config holds that module's non-secret settings as
// a JSON object — credentials are never stored here; they come from the
// environment (see calendar.Module.SecretEnv).
type CalendarIntegration struct {
	Base
	Kind        string `gorm:"type:varchar(40)" json:"kind"`
	Config      string `gorm:"type:text" json:"-"` // JSON map[string]string
	UpdatedByID string `gorm:"type:varchar(36)" json:"updatedById,omitempty"`
}
