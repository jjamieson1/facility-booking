package domain

import "time"

// WaitlistEntry is a resident's request to be notified if a taken slot frees up
// (§4.11). When a booking overlapping the window is cancelled or denied, matching
// entries are notified and stamped NotifiedAt so they're told at most once.
type WaitlistEntry struct {
	Base
	FacilityID string     `gorm:"type:varchar(36);index" json:"facilityId"`
	Facility   *Facility  `gorm:"foreignKey:FacilityID" json:"facility,omitempty"`
	UserID     string     `gorm:"type:varchar(36);index" json:"userId"`
	StartsAt   time.Time  `json:"startsAt"`
	EndsAt     time.Time  `json:"endsAt"`
	NotifiedAt *time.Time `json:"notifiedAt,omitempty"`
}
