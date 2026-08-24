package domain

import "time"

// CancellationPolicy is the refund and modification terms for a facility
// (§4.7, §4.9, and the §5 "Policy" entity).
//
// FacilityID nil means the municipality-wide default: the policy used by any
// facility without its own. An ice arena and a meeting room rarely share
// cancellation terms, so per-facility overrides are the point — but every
// facility must resolve to *some* policy, or a cancellation has no defined
// outcome.
type CancellationPolicy struct {
	Base
	FacilityID *string `gorm:"type:varchar(36);index" json:"facilityId,omitempty"`
	Name       string  `gorm:"type:varchar(120)" json:"name"`

	// NonRefundableCents is withheld from every refund (a booking/admin fee).
	// Applied after the tier percentage, and never more than the amount paid.
	NonRefundableCents int `json:"nonRefundableCents"`

	// ModificationCutoffHours is how close to the start a booking may still be
	// rescheduled. 0 means "any time before it starts".
	ModificationCutoffHours int `json:"modificationCutoffHours"`

	Tiers []RefundTier `gorm:"foreignKey:PolicyID" json:"tiers,omitempty"`
}

// RefundTier is one band of the policy: cancel at least HoursBefore ahead of the
// start and RefundPercent of the fee comes back. Tiers are evaluated from the
// largest HoursBefore down, so the first one the cancellation qualifies for
// wins; cancelling inside the smallest tier refunds nothing.
type RefundTier struct {
	Base
	PolicyID      string `gorm:"type:varchar(36);index" json:"policyId"`
	HoursBefore   int    `json:"hoursBefore"`
	RefundPercent int    `json:"refundPercent"` // 0–100
}

// Quote is what a cancellation would produce right now: the amount, and the
// reason in words. It is computed before anything is cancelled so the resident
// can be shown the consequence before confirming, and again by the cancel path
// so the figure quoted is the figure issued.
type Quote struct {
	PolicyName       string  `json:"policyName"`
	PaidCents        int     `json:"paidCents"`
	RefundCents      int     `json:"refundCents"`
	RefundPercent    int     `json:"refundPercent"`
	HoursUntilStart  float64 `json:"hoursUntilStart"`
	AppliedTierHours int     `json:"appliedTierHours"`
	Explanation      string  `json:"explanation"`
}

// Refundable reports whether cancelling now returns any money.
func (q Quote) Refundable() bool { return q.RefundCents > 0 }

// HoursUntil is the whole-hours gap between now and the booking start, negative
// once it has begun.
func HoursUntil(start, now time.Time) float64 {
	return start.Sub(now).Hours()
}
