package domain

import "time"

// BookingCondition is what staff attached to an approval (§4.5, §4.8): terms the
// resident must accept, an optional added fee, and an optional document beyond
// the facility's own waiver requirement.
//
// One row per booking. Re-approving replaces it, and the audit log carries the
// history — the condition set is the *current* contract, not a ledger.
//
// The three requirements are independent: a condition set may be terms only,
// terms plus a fee, a document only, or any combination. What they share is that
// none of them confirms the booking on its own.
type BookingCondition struct {
	Base
	BookingID string   `gorm:"type:varchar(36);uniqueIndex" json:"bookingId"`
	Booking   *Booking `gorm:"foreignKey:BookingID" json:"booking,omitempty"`

	// Terms are the conditions in the staff member's own words ("no amplified
	// music after 9pm"). Free text because the municipality's conditions are not
	// ours to enumerate.
	Terms string `gorm:"type:text" json:"terms"`

	// AdditionalFeeCents is levied on top of the booking's fee — a security
	// guard, extra cleaning. Added to Booking.FeeCents when the condition is
	// set, so every existing price, payment and report path keeps working
	// without learning about conditions.
	AdditionalFeeCents int `json:"additionalFeeCents"`

	// DocumentLabel names a document the resident must upload, beyond the
	// facility-level waiver. Empty means none required.
	DocumentLabel string `gorm:"type:varchar(200)" json:"documentLabel"`

	// SetByID is the staff member who imposed the conditions — §4.8 requires
	// this be attributable, and "who decided this" is the first question a
	// resident disputing a condition asks.
	SetByID string `gorm:"type:varchar(36);index" json:"setById"`
	SetBy   *User  `gorm:"foreignKey:SetByID" json:"setBy,omitempty"`

	// AcceptedAt records the resident agreeing to the terms. Nil until they do.
	// Stored rather than inferred, because "did they agree, and when" is the
	// question that matters if the condition is later enforced.
	AcceptedAt *time.Time `json:"acceptedAt,omitempty"`
}

// RequiresDocument reports whether a document upload is part of this condition set.
func (c BookingCondition) RequiresDocument() bool { return c.DocumentLabel != "" }

// Accepted reports whether the resident has agreed to the terms.
func (c BookingCondition) Accepted() bool { return c.AcceptedAt != nil }
