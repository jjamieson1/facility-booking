// Package users manages the app-local authorization roles. Identity comes from
// C2 over OIDC; who may administer this app is decided here. Admins can promote
// or revoke other users, and invite an email that hasn't logged in yet — a
// pending grant applied on that person's first login.
package users

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

var (
	ErrInvalidRole      = errors.New("users: invalid role")
	ErrInvalidEmail     = errors.New("users: invalid email")
	ErrNotFound         = errors.New("users: not found")
	ErrCannotDemoteSelf = errors.New("users: you cannot lower your own role")
	ErrLastAdmin        = errors.New("users: cannot remove the last administrator")
)

// Service owns role reads and mutations, writing an audit trail for each change.
type Service struct {
	db    *gorm.DB
	audit auditlog.Recorder
}

func NewService(db *gorm.DB, audit auditlog.Recorder) *Service {
	return &Service{db: db, audit: audit}
}

// ListElevated returns users holding a staff or admin role, newest first.
func (s *Service) ListElevated(ctx context.Context) ([]domain.User, error) {
	users := []domain.User{}
	err := s.db.WithContext(ctx).
		Where("role IN ?", []domain.Role{domain.RoleStaff, domain.RoleAdmin}).
		Order("created_at DESC").
		Find(&users).Error
	return users, err
}

// ListInvites returns pending role grants (emails not yet logged in).
func (s *Service) ListInvites(ctx context.Context) ([]domain.RoleGrant, error) {
	grants := []domain.RoleGrant{}
	err := s.db.WithContext(ctx).Order("created_at DESC").Find(&grants).Error
	return grants, err
}

// InviteResult reports how an invite was resolved: an existing user promoted now,
// or a pending grant recorded for a first login.
type InviteResult struct {
	Applied bool              `json:"applied"`
	User    *domain.User      `json:"user,omitempty"`
	Grant   *domain.RoleGrant `json:"grant,omitempty"`
}

// Invite elevates the account for email to role. If that email already has a
// local user it is promoted immediately; otherwise a pending grant is stored and
// applied when they first sign in. Only elevated roles (staff/admin) are invitable.
func (s *Service) Invite(ctx context.Context, email string, role domain.Role, actor domain.User) (*InviteResult, error) {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return nil, ErrInvalidEmail
	}
	if !domain.ValidRole(role) || role == domain.RoleResident {
		return nil, ErrInvalidRole
	}

	var u domain.User
	err := s.db.WithContext(ctx).Where("LOWER(email) = ?", email).First(&u).Error
	switch {
	case err == nil:
		if domain.RoleRank(u.Role) < domain.RoleRank(role) {
			if e := s.db.WithContext(ctx).Model(&u).Update("role", role).Error; e != nil {
				return nil, e
			}
			s.record(ctx, actor, "user.role.grant", "user", u.ID, u.Email+" → "+string(role))
			u.Role = role
		}
		return &InviteResult{Applied: true, User: &u}, nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}

	// No user yet — upsert a pending grant keyed by email.
	grant := domain.RoleGrant{Email: email, Role: role, InvitedBy: actor.Email}
	if e := s.db.WithContext(ctx).
		Where("email = ?", email).
		Assign(map[string]any{"role": role, "invited_by": actor.Email}).
		FirstOrCreate(&grant).Error; e != nil {
		return nil, e
	}
	s.record(ctx, actor, "user.invite", "invite", grant.ID, email+" → "+string(role))
	return &InviteResult{Grant: &grant}, nil
}

// SetRole changes an existing user's role (promotion or revocation). It refuses
// to let an admin lower their own role or remove the final administrator, so the
// app can never be locked out of administration.
func (s *Service) SetRole(ctx context.Context, userID string, role domain.Role, actor domain.User) (*domain.User, error) {
	if !domain.ValidRole(role) {
		return nil, ErrInvalidRole
	}
	var u domain.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", userID).Error; err != nil {
		return nil, ErrNotFound
	}
	if u.Role == role {
		return &u, nil
	}
	demotion := domain.RoleRank(role) < domain.RoleRank(u.Role)
	if u.ID == actor.ID && demotion {
		return nil, ErrCannotDemoteSelf
	}
	if u.Role == domain.RoleAdmin && role != domain.RoleAdmin && s.adminCount(ctx) <= 1 {
		return nil, ErrLastAdmin
	}

	prev := u.Role
	if err := s.db.WithContext(ctx).Model(&u).Update("role", role).Error; err != nil {
		return nil, err
	}
	u.Role = role
	s.record(ctx, actor, "user.role.set", "user", u.ID, u.Email+": "+string(prev)+" → "+string(role))
	return &u, nil
}

// RevokeInvite deletes a pending grant that hasn't been claimed yet.
func (s *Service) RevokeInvite(ctx context.Context, id string, actor domain.User) error {
	var g domain.RoleGrant
	if err := s.db.WithContext(ctx).First(&g, "id = ?", id).Error; err != nil {
		return ErrNotFound
	}
	if err := s.db.WithContext(ctx).Delete(&g).Error; err != nil {
		return err
	}
	s.record(ctx, actor, "user.invite.revoke", "invite", g.ID, g.Email)
	return nil
}

func (s *Service) adminCount(ctx context.Context) int64 {
	var n int64
	s.db.WithContext(ctx).Model(&domain.User{}).Where("role = ?", domain.RoleAdmin).Count(&n)
	return n
}

// record writes the durable local audit row and best-effort mirrors it to the
// central audit-logging service.
func (s *Service) record(ctx context.Context, actor domain.User, action, targetType, targetID, detail string) {
	_ = s.db.WithContext(ctx).Create(&domain.AuditLog{
		ActorID: actor.ID, Action: action, TargetType: targetType, TargetID: targetID, Detail: detail,
	}).Error
	if s.audit != nil {
		s.audit.Record(auditlog.Event{
			Action: action, ActorID: actor.ID, ActorEmail: actor.Email,
			TargetType: targetType, TargetID: targetID, Message: detail,
		})
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// validEmail is a deliberately loose check — C2 is the identity authority, this
// only guards against obvious typos before storing a grant.
func validEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	return at > 0 && strings.IndexByte(email[at:], '.') > 0
}
