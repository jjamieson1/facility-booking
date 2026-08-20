package users

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/testdb"
)

func newService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := testdb.New(t)
	return NewService(db, auditlog.New("", "")), db
}

func mkUser(t *testing.T, db *gorm.DB, email string, role domain.Role) domain.User {
	t.Helper()
	u := domain.User{Subject: uuid.NewString(), Email: email, Name: email, Role: role}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func TestInviteExistingUserPromotes(t *testing.T) {
	svc, db := newService(t)
	admin := mkUser(t, db, "admin@x.com", domain.RoleAdmin)
	target := mkUser(t, db, "Bob@X.com", domain.RoleResident)

	// Case-insensitive match on the existing email.
	res, err := svc.Invite(context.Background(), "bob@x.com", domain.RoleAdmin, admin)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if !res.Applied || res.User == nil || res.User.Role != domain.RoleAdmin {
		t.Fatalf("res = %+v, want applied admin promotion", res)
	}
	var reloaded domain.User
	db.First(&reloaded, "id = ?", target.ID)
	if reloaded.Role != domain.RoleAdmin {
		t.Errorf("target role = %q, want admin", reloaded.Role)
	}
	// No grant should be created for an existing user.
	var grants int64
	db.Model(&domain.RoleGrant{}).Count(&grants)
	if grants != 0 {
		t.Errorf("grants = %d, want 0", grants)
	}
}

func TestInviteNewEmailCreatesGrant(t *testing.T) {
	svc, db := newService(t)
	admin := mkUser(t, db, "admin@x.com", domain.RoleAdmin)

	res, err := svc.Invite(context.Background(), "New.Person@x.com", domain.RoleStaff, admin)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if res.Applied || res.Grant == nil {
		t.Fatalf("res = %+v, want a pending grant", res)
	}
	if res.Grant.Email != "new.person@x.com" || res.Grant.Role != domain.RoleStaff {
		t.Errorf("grant = %+v, want normalized email + staff", res.Grant)
	}

	// Re-inviting the same email updates the grant in place (no duplicate).
	if _, err := svc.Invite(context.Background(), "new.person@x.com", domain.RoleAdmin, admin); err != nil {
		t.Fatal(err)
	}
	var grants []domain.RoleGrant
	db.Find(&grants)
	if len(grants) != 1 || grants[0].Role != domain.RoleAdmin {
		t.Errorf("grants = %+v, want one upgraded to admin", grants)
	}
}

func TestInviteRejectsBadInput(t *testing.T) {
	svc, db := newService(t)
	admin := mkUser(t, db, "admin@x.com", domain.RoleAdmin)
	if _, err := svc.Invite(context.Background(), "not-an-email", domain.RoleAdmin, admin); err != ErrInvalidEmail {
		t.Errorf("bad email err = %v, want ErrInvalidEmail", err)
	}
	if _, err := svc.Invite(context.Background(), "ok@x.com", domain.RoleResident, admin); err != ErrInvalidRole {
		t.Errorf("resident invite err = %v, want ErrInvalidRole", err)
	}
}

func TestSetRoleGuards(t *testing.T) {
	svc, db := newService(t)
	admin := mkUser(t, db, "admin@x.com", domain.RoleAdmin)

	// Self-demotion is refused.
	if _, err := svc.SetRole(context.Background(), admin.ID, domain.RoleResident, admin); err != ErrCannotDemoteSelf {
		t.Errorf("self-demote err = %v, want ErrCannotDemoteSelf", err)
	}
	// The last admin cannot be demoted by anyone.
	other := mkUser(t, db, "other@x.com", domain.RoleStaff)
	if _, err := svc.SetRole(context.Background(), admin.ID, domain.RoleStaff, other); err != ErrLastAdmin {
		t.Errorf("last-admin err = %v, want ErrLastAdmin", err)
	}
}

func TestSetRolePromoteAndRevoke(t *testing.T) {
	svc, db := newService(t)
	admin := mkUser(t, db, "admin@x.com", domain.RoleAdmin)
	bob := mkUser(t, db, "bob@x.com", domain.RoleResident)

	if _, err := svc.SetRole(context.Background(), bob.ID, domain.RoleAdmin, admin); err != nil {
		t.Fatal(err)
	}
	// With two admins now, revoking bob back to resident is allowed.
	u, err := svc.SetRole(context.Background(), bob.ID, domain.RoleResident, admin)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if u.Role != domain.RoleResident {
		t.Errorf("role = %q, want resident", u.Role)
	}

	// The action left an audit trail.
	var audits int64
	db.Model(&domain.AuditLog{}).Where("target_type = ?", "user").Count(&audits)
	if audits == 0 {
		t.Error("expected audit rows for role changes")
	}
}

func TestRevokeInvite(t *testing.T) {
	svc, db := newService(t)
	admin := mkUser(t, db, "admin@x.com", domain.RoleAdmin)
	res, err := svc.Invite(context.Background(), "pending@x.com", domain.RoleAdmin, admin)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeInvite(context.Background(), res.Grant.ID, admin); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	var n int64
	db.Model(&domain.RoleGrant{}).Count(&n)
	if n != 0 {
		t.Errorf("grants = %d, want 0", n)
	}
}
