package domain

import "time"

// Session is a server-side login session. The cookie carries the random,
// unguessable ID; storing sessions server-side makes them revocable and keeps
// no identity data client-side. Identity is refreshed from C2 at each login.
type Session struct {
	Base
	UserID    string    `gorm:"type:varchar(36);index" json:"userId"`
	User      *User     `gorm:"foreignKey:UserID" json:"-"`
	ExpiresAt time.Time `gorm:"index" json:"expiresAt"`
	// IDToken is the raw OIDC ID token from login, kept only to pass as
	// `id_token_hint` on RP-initiated logout (C2 needs it to know whose session
	// to end). Never leaves the server; dropped with the session at logout.
	IDToken string `gorm:"type:text" json:"-"`
}

// Expired reports whether the session is past its lifetime.
func (s Session) Expired(now time.Time) bool { return now.After(s.ExpiresAt) }
