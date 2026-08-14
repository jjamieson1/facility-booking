package auth

import (
	"context"
	"testing"

	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func residencyTestService(t *testing.T) *Service {
	t.Helper()
	db := testdb.New(t)
	return &Service{db: db, cfg: config.Config{}}
}

// A residency_status of "active" makes the upserted user a resident (so booking
// charges the resident rate); other/absent statuses do not.
func TestUpsertUserResidencyClaim(t *testing.T) {
	cases := []struct {
		name         string
		status       string
		wantResident bool
	}{
		{name: "active status", status: "active", wantResident: true},
		{name: "active mixed case", status: "Active", wantResident: true},
		{name: "inactive status", status: "expired", wantResident: false},
		{name: "empty status", status: "", wantResident: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := residencyTestService(t)
			c := claims{Subject: "sub-1", Email: "r@example.com", Name: "R", ResidencyStatus: tc.status}
			u, err := svc.upsertUser(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if u.IsResident != tc.wantResident {
				t.Fatalf("IsResident = %v, want %v", u.IsResident, tc.wantResident)
			}
		})
	}
}

// An authoritative C2 residency claim is honoured on a returning user, in both
// directions; a login carrying no residency claim leaves the flag untouched.
func TestUpsertUserResidencyRefresh(t *testing.T) {
	svc := residencyTestService(t)
	ctx := context.Background()
	sub := "sub-refresh"

	// First login: active → resident.
	if u, err := svc.upsertUser(ctx, claims{Subject: sub, Email: "r@example.com", Name: "R", ResidencyStatus: "active"}); err != nil {
		t.Fatal(err)
	} else if !u.IsResident {
		t.Fatal("first login: want resident")
	}

	// Residency lapses: an inactive claim demotes the user.
	if u, err := svc.upsertUser(ctx, claims{Subject: sub, Email: "r@example.com", Name: "R", ResidencyStatus: "expired"}); err != nil {
		t.Fatal(err)
	} else if u.IsResident {
		t.Fatal("after lapse: want non-resident")
	}

	// Residency is re-established by an entitlement determination (which is the
	// only writer now that self-attestation is gone); a later login carrying no
	// claim must not clobber it.
	if err := svc.db.Model(&domain.User{}).
		Where("id = ?", subjectUserID(t, svc, sub)).Update("is_resident", true).Error; err != nil {
		t.Fatal(err)
	}
	if u, err := svc.upsertUser(ctx, claims{Subject: sub, Email: "r@example.com", Name: "R"}); err != nil {
		t.Fatal(err)
	} else if !u.IsResident {
		t.Fatal("no claim: residency flag should be preserved")
	}
}

func subjectUserID(t *testing.T, svc *Service, sub string) string {
	t.Helper()
	var u domain.User
	if err := svc.db.Where("subject = ?", sub).First(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u.ID
}
