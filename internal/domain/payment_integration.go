package domain

// PaymentIntegrationID is the fixed primary key of the single payment-settings
// row. A constant id makes the setting a true singleton: every write upserts the
// same row, so there is no way to end up with two competing gateways configured.
const PaymentIntegrationID = "payment-integration"

// PaymentIntegration records which payment module the municipality selected
// (§4.7). Config holds that module's non-secret settings as a JSON object —
// API keys are never stored here; they come from the environment (see
// payment.Module.SecretEnv).
type PaymentIntegration struct {
	Base
	Kind        string `gorm:"type:varchar(40)" json:"kind"`
	Config      string `gorm:"type:text" json:"-"` // JSON map[string]string
	UpdatedByID string `gorm:"type:varchar(36)" json:"updatedById,omitempty"`
}
