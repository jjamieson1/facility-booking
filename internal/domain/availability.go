package domain

import "time"

// AvailabilityRule defines a facility's regular opening hours for one weekday.
// Times are minutes-since-midnight in the facility's local timezone (0..1439),
// which keeps them trivially comparable without date arithmetic.
type AvailabilityRule struct {
	Base
	FacilityID  string `gorm:"type:varchar(36);index" json:"facilityId"`
	Weekday     int    `json:"weekday"`    // 0=Sunday .. 6=Saturday (Go time.Weekday)
	OpenMinute  int    `json:"openMinute"` // e.g. 8*60 = 08:00
	CloseMinute int    `json:"closeMinute"`
}

// Blackout marks a facility unavailable for a date/time range (maintenance,
// closure). Overlapping this range blocks booking regardless of opening hours.
type Blackout struct {
	Base
	FacilityID string    `gorm:"type:varchar(36);index" json:"facilityId"`
	StartsAt   time.Time `gorm:"index" json:"startsAt"`
	EndsAt     time.Time `json:"endsAt"`
	Reason     string    `gorm:"type:varchar(300)" json:"reason"`
}
