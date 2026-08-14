package domain

import "time"

// EntitlementOutcome is what a provider decided.
type EntitlementOutcome string

const (
	EntitlementGranted EntitlementOutcome = "granted"
	EntitlementDenied  EntitlementOutcome = "denied"
)

// EntitlementDetermination is a provider's decision about one person and one
// entitlement type (§P2-5.11a). It replaces the `User.IsResident` boolean, which
// carried no provenance and never expired — people move.
//
// Provider + Ref are stored **together**: a bare reference is meaningless
// without knowing who issued it, and switching provider must invalidate every
// ref for that type rather than silently re-interpreting them.
//
// The evidence itself is deliberately not stored — the provider holds it. That
// keeps it out of this database and out of MFIPPA/FOIP access requests.
type EntitlementDetermination struct {
	Base
	UserID      string             `gorm:"type:varchar(36);index:idx_ent_user_type" json:"userId"`
	Type        string             `gorm:"type:varchar(40);index:idx_ent_user_type" json:"type"`
	Outcome     EntitlementOutcome `gorm:"type:varchar(20)" json:"outcome"`
	Category    string             `gorm:"type:varchar(60)" json:"category"` // e.g. "resident", "non-resident"
	Provider    string             `gorm:"type:varchar(60)" json:"provider"`
	Ref         string             `gorm:"type:varchar(200)" json:"ref"` // provider-scoped external reference
	EvaluatedAt time.Time          `json:"evaluatedAt"`
	ValidUntil  *time.Time         `json:"validUntil,omitempty"` // nil = no expiry
}

// Live reports whether this determination grants the entitlement at time now.
func (d EntitlementDetermination) Live(now time.Time) bool {
	return d.Outcome == EntitlementGranted && !d.Expired(now)
}

// Expired reports whether the validity window has passed.
func (d EntitlementDetermination) Expired(now time.Time) bool {
	return d.ValidUntil != nil && now.After(*d.ValidUntil)
}

// BookingEntitlement is the determination set stamped onto a booking at the
// moment it was priced (§P2-5.11a constraint 2 and 4). It is written once and
// never rewritten: a later enrolment applies to subsequent bookings, it does not
// reprice a completed one.
//
// This capture is load-bearing for reporting, not only for audit — a booking
// discounted to zero is otherwise indistinguishable from a free facility, and
// this is the only record that can answer "how much fee assistance did we
// provide this year?"
type BookingEntitlement struct {
	Base
	BookingID string             `gorm:"type:varchar(36);index" json:"bookingId"`
	Type      string             `gorm:"type:varchar(40)" json:"type"`
	Outcome   EntitlementOutcome `gorm:"type:varchar(20)" json:"outcome"`
	Category  string             `gorm:"type:varchar(60)" json:"category"`
	Provider  string             `gorm:"type:varchar(60)" json:"provider"`
	Ref       string             `gorm:"type:varchar(200)" json:"ref"`
}
