package domain

// Role is the app-local authorization role. Identity (who the user is) comes
// from C2 via OIDC; the role (what they may do here) is owned by this app.
type Role string

const (
	// RoleGuest is someone who booked without creating an account. A guest
	// session is a real session backed by a real user row (synthetic subject
	// `guest:<uuid>`), so ownership checks work unchanged — but it is *not* a
	// real account, and `auth.RequireAccount` rejects it.
	RoleGuest    Role = "guest"
	RoleResident Role = "resident"
	RoleStaff    Role = "staff"
	RoleAdmin    Role = "admin"
)

// RoleRank orders roles so promotions/demotions can be compared numerically.
// Higher is more privileged; an unknown role ranks as resident.
func RoleRank(r Role) int {
	switch r {
	case RoleAdmin:
		return 2
	case RoleStaff:
		return 1
	case RoleGuest:
		return -1
	default:
		return 0
	}
}

// ValidRole reports whether r is a role staff may assign. RoleGuest is
// deliberately excluded: it is conferred by booking as a guest, never granted,
// and an admin must not be able to demote an account holder into one.
func ValidRole(r Role) bool {
	return r == RoleResident || r == RoleStaff || r == RoleAdmin
}

// IsGuest reports whether this user booked without creating an account.
func (u User) IsGuest() bool { return u.Role == RoleGuest }

// User is a local projection of a C2 identity. Subject is the OIDC `sub` claim
// and is the stable link back to C2; no passwords are ever stored here.
type User struct {
	Base
	Subject string `gorm:"type:varchar(255);uniqueIndex" json:"-"`
	Email   string `gorm:"type:varchar(255);index" json:"email"`
	Name    string `gorm:"type:varchar(255)" json:"name"`
	Role    Role   `gorm:"type:varchar(20);default:resident" json:"role"`
	// Language is the citizen's preferred language for notifications ("en" or
	// "fr"). Canada requires both official languages (§4.11), and a notification
	// is the one place the app speaks to someone outside the UI — where the
	// header toggle cannot help. Set from the SPA's language toggle; defaults to
	// English when never set.
	Language string `gorm:"type:varchar(5);default:en" json:"language"`

	IsResident bool   `json:"isResident"`                                 // verified municipal resident (drives resident pricing)
	Address    string `gorm:"type:varchar(300)" json:"address,omitempty"` // captured during residency verification
}

// RoleGrant is a pending elevated-role assignment for an email that has not
// logged in yet. On that person's first C2 login it is applied to their new
// local user and then consumed. A user who already exists is promoted directly
// (no grant needed), so a grant only ever exists for a not-yet-seen email.
type RoleGrant struct {
	Base
	Email     string `gorm:"type:varchar(255);uniqueIndex" json:"email"`
	Role      Role   `gorm:"type:varchar(20)" json:"role"`
	InvitedBy string `gorm:"type:varchar(255)" json:"invitedBy"` // actor email, for the audit trail
}
