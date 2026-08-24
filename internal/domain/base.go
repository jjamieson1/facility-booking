// Package domain holds the GORM models and the AllModels list that drives
// AutoMigrate. Every model embeds Base, giving it a UUID primary key,
// created/updated timestamps, and soft-delete.
package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base is embedded by every model. The ID is a UUID string generated before
// create so it works identically on SQLite and MySQL (neither needs a DB
// default expression).
type Base struct {
	ID        string         `gorm:"type:varchar(36);primaryKey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate assigns a UUID when the caller hasn't set one.
func (b *Base) BeforeCreate(*gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}

// AllModels is the full list of tables to AutoMigrate. Add every new model here.
func AllModels() []any {
	return []any{
		&User{},
		&RoleGrant{},
		&Session{},
		&Facility{},
		&Accessory{},
		&FacilityAccessory{},
		&AvailabilityRule{},
		&Blackout{},
		&Booking{},
		&Payment{},
		&RefundObligation{},
		&PaymentTransaction{},
		&WaitlistEntry{},
		&WaiverDocument{},
		&AuditLog{},
		&CalendarIntegration{},
		&PaymentIntegration{},
		&CancellationPolicy{},
		&RefundTier{},
		&FacilityTranslation{},
		&AccessoryTranslation{},
		&EntitlementDetermination{},
		&BookingEntitlement{},
	}
}
