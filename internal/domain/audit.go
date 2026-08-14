package domain

// AuditLog records staff actions on bookings (approve, deny, refund, …) for
// accountability, per the non-functional requirements. It is append-only.
type AuditLog struct {
	Base
	ActorID    string `gorm:"type:varchar(36);index" json:"actorId"` // staff/admin user
	Action     string `gorm:"type:varchar(60);index" json:"action"`  // e.g. "booking.approve"
	TargetType string `gorm:"type:varchar(60)" json:"targetType"`    // e.g. "booking"
	TargetID   string `gorm:"type:varchar(36);index" json:"targetId"`
	Detail     string `gorm:"type:text" json:"detail"`
}
