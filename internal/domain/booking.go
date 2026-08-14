package domain

import "time"

// BookingStatus is the lifecycle of a booking request.
type BookingStatus string

const (
	StatusPending   BookingStatus = "pending"   // awaiting staff approval
	StatusConfirmed BookingStatus = "confirmed" // holds the slot for everyone
	StatusDenied    BookingStatus = "denied"
	StatusCancelled BookingStatus = "cancelled"
)

// Booking is a reservation of a facility for a time window. Only Confirmed (and
// Pending, provisionally) bookings block a slot; the overlap check in the
// booking service enforces no-double-book including the facility's buffer.
type Booking struct {
	Base
	FacilityID string    `gorm:"type:varchar(36);index:idx_facility_time" json:"facilityId"`
	Facility   *Facility `gorm:"foreignKey:FacilityID" json:"facility,omitempty"`
	UserID     string    `gorm:"type:varchar(36);index" json:"userId"`
	User       *User     `gorm:"foreignKey:UserID" json:"user,omitempty"`

	StartsAt time.Time     `gorm:"index:idx_facility_time" json:"startsAt"`
	EndsAt   time.Time     `json:"endsAt"`
	Status   BookingStatus `gorm:"type:varchar(20);index;default:pending" json:"status"`

	Purpose    string `gorm:"type:varchar(500)" json:"purpose"`
	Attendance int    `json:"attendance"`
	Resident   bool   `json:"resident"` // booker's residency at booking time (drives the resident/non-resident report split)

	// RecurrenceID groups the occurrences of a recurring booking (§4.11); nil for
	// one-off bookings.
	RecurrenceID *string `gorm:"type:varchar(36);index" json:"recurrenceId,omitempty"`

	FeeCents int             `json:"feeCents"` // captured at booking time from the facility
	Payment  *Payment        `gorm:"foreignKey:BookingID" json:"payment,omitempty"`
	Waiver   *WaiverDocument `gorm:"foreignKey:BookingID" json:"waiver,omitempty"`

	// ReminderSentAt marks when the pre-booking reminder was sent, so the
	// scheduler sends it at most once. Nil until sent.
	ReminderSentAt *time.Time `json:"reminderSentAt,omitempty"`
}

// Active reports whether this booking should block its slot against others.
func (b Booking) Active() bool {
	return b.Status == StatusPending || b.Status == StatusConfirmed
}
