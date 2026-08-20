package auth

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func TestValidateBackchannelLogoutClaims(t *testing.T) {
	okEvents := map[string]json.RawMessage{backchannelLogoutEvent: json.RawMessage(`{}`)}
	nonce := "not-allowed"

	cases := []struct {
		name   string
		claims backchannelLogoutClaims
		wantOK bool
	}{
		{
			name:   "valid claims",
			claims: backchannelLogoutClaims{Subject: "sub-1", Events: okEvents},
			wantOK: true,
		},
		{
			name:   "missing subject",
			claims: backchannelLogoutClaims{Events: okEvents},
		},
		{
			name:   "missing events",
			claims: backchannelLogoutClaims{Subject: "sub-1"},
		},
		{
			name:   "missing backchannel event",
			claims: backchannelLogoutClaims{Subject: "sub-1", Events: map[string]json.RawMessage{"other": json.RawMessage(`{}`)}},
		},
		{
			name:   "nonce not allowed",
			claims: backchannelLogoutClaims{Subject: "sub-1", Events: okEvents, Nonce: &nonce},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBackchannelLogoutClaims(tc.claims)
			if tc.wantOK && err != nil {
				t.Fatalf("validateBackchannelLogoutClaims() err = %v, want nil", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("validateBackchannelLogoutClaims() err = nil, want error")
			}
		})
	}
}

func TestCloseSessionsForSubject(t *testing.T) {
	db := testdb.New(t)
	svc := &Service{db: db, cfg: config.Config{}}

	u1 := domain.User{Subject: "sub-1", Email: "a@example.com", Name: "A"}
	u2 := domain.User{Subject: "sub-2", Email: "b@example.com", Name: "B"}
	if err := db.Create(&u1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&u2).Error; err != nil {
		t.Fatal(err)
	}

	expires := time.Now().Add(time.Hour)
	sessions := []domain.Session{
		{UserID: u1.ID, ExpiresAt: expires},
		{UserID: u1.ID, ExpiresAt: expires},
		{UserID: u2.ID, ExpiresAt: expires},
	}
	for i := range sessions {
		if err := db.Create(&sessions[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.CloseSessionsForSubject(context.Background(), "sub-1"); err != nil {
		t.Fatalf("CloseSessionsForSubject() err = %v", err)
	}

	var remainingSub1 int64
	if err := db.Model(&domain.Session{}).Where("user_id = ?", u1.ID).Count(&remainingSub1).Error; err != nil {
		t.Fatal(err)
	}
	if remainingSub1 != 0 {
		t.Fatalf("sub-1 sessions remaining = %d, want 0", remainingSub1)
	}

	var remainingSub2 int64
	if err := db.Model(&domain.Session{}).Where("user_id = ?", u2.ID).Count(&remainingSub2).Error; err != nil {
		t.Fatal(err)
	}
	if remainingSub2 != 1 {
		t.Fatalf("sub-2 sessions remaining = %d, want 1", remainingSub2)
	}
}
