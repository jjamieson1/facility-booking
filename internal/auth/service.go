// Package auth is the OIDC relying-party integration with C2. It delegates
// identity to C2 (login happens there), then keeps a local server-side session
// and a local User row keyed by the C2 subject. Roles are owned by this app.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
)

// ErrNotConfigured is returned when OIDC settings are absent.
var ErrNotConfigured = errors.New("auth: OIDC not configured")

// ErrInvalidLogoutToken is returned when a back-channel logout token is
// malformed or fails required claim checks.
var ErrInvalidLogoutToken = errors.New("auth: invalid logout token")

const backchannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"

// Service performs the OIDC dance and manages sessions.
type Service struct {
	db            *gorm.DB
	cfg           config.Config
	verifier      *oidc.IDTokenVerifier
	userinfoURL   string
	endSessionURL string    // OIDC RP-initiated logout endpoint at C2
	postLogoutURL string    // where C2 sends the browser back after logout
	c2            *c2Client // profile lookup by subject (C2 releases only `sub`)
	oauth         oauth2.Config
}

// NewService builds the relying party. Rather than rely on discovery (C2's dev
// web origin doesn't proxy /oidc), it derives the endpoints from OIDCBaseURL —
// the origin that serves the OIDC API as JSON — while verifying ID tokens
// against OIDCIssuer, the identifier C2 stamps in the `iss` claim. Returns
// (nil, nil) when OIDC is not configured so the app still boots.
func NewService(ctx context.Context, db *gorm.DB, cfg config.Config) (*Service, error) {
	if !cfg.OIDCEnabled() {
		return nil, nil
	}
	base := strings.TrimRight(cfg.OIDCBaseURL, "/")
	if base == "" {
		base = strings.TrimRight(cfg.OIDCIssuer, "/")
	}

	keySet := oidc.NewRemoteKeySet(ctx, base+"/keys")
	verifier := oidc.NewVerifier(cfg.OIDCIssuer, keySet, &oidc.Config{ClientID: cfg.OIDCClientID})

	return &Service{
		db:            db,
		cfg:           cfg,
		verifier:      verifier,
		userinfoURL:   base + "/userinfo",
		endSessionURL: base + "/end_session",
		postLogoutURL: cfg.OIDCPostLogoutRedirectURL,
		c2:            newC2Client(cfg.C2APIURL, cfg.C2ServiceUser, cfg.C2ServicePass),
		oauth: oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  base + "/authorize",
				TokenURL: base + "/oauth/token",
			},
			RedirectURL: cfg.OIDCRedirectURL,
			Scopes:      []string{oidc.ScopeOpenID, "profile", "email", "residency"},
		},
	}, nil
}

// AuthCodeURL builds the authorize URL for the login redirect, binding the
// state and a PKCE challenge derived from verifier.
func (s *Service) AuthCodeURL(state, verifier string) string {
	return s.oauth.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("max_age", "3600"),
	)
}

// residencyStatusActive is the residency_status claim value C2 asserts for a
// verified current resident. It selects the resident booking rate.
const residencyStatusActive = "active"

// claims are the subset of OIDC claims this app consumes.
type claims struct {
	Subject         string `json:"sub"`
	Email           string `json:"email"`
	Name            string `json:"name"`
	GivenName       string `json:"given_name"`
	FamilyName      string `json:"family_name"`
	ResidencyStatus string `json:"residency_status"` // "active" ⇒ resident pricing
}

// residencyAsserted reports whether C2 released a residency_status claim. When it
// is present the app honours it (active → resident, anything else → not) rather
// than leaving the local flag to self-attestation.
func (c claims) residencyAsserted() bool {
	return strings.TrimSpace(c.ResidencyStatus) != ""
}

// isActiveResident reports whether residency_status is "active".
func (c claims) isActiveResident() bool {
	return strings.EqualFold(strings.TrimSpace(c.ResidencyStatus), residencyStatusActive)
}

// Login is the outcome of a completed OIDC round-trip: the local user plus the
// raw ID token, which the caller stores on the session for logout's
// `id_token_hint`.
type Login struct {
	User    *domain.User
	IDToken string
}

// Complete exchanges the code for tokens, verifies the ID token, and upserts the
// local user. It returns the login so the caller can open a session.
func (s *Service) Complete(ctx context.Context, code, verifier string) (Login, error) {
	tok, err := s.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Login{}, fmt.Errorf("auth: token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return Login{}, errors.New("auth: no id_token in response")
	}
	idToken, err := s.verifier.Verify(ctx, rawID)
	if err != nil {
		return Login{}, fmt.Errorf("auth: verify id_token: %w", err)
	}
	var c claims
	if err := idToken.Claims(&c); err != nil {
		return Login{}, fmt.Errorf("auth: parse claims: %w", err)
	}
	if c.Subject == "" {
		return Login{}, errors.New("auth: id_token missing subject")
	}
	// Try the standard OIDC userinfo endpoint first. C2, however, gates profile/
	// email release behind an app consent policy and returns only `sub`, so fall
	// back to looking the identity up in C2's admin API by subject. Both are
	// best-effort — identity is already established by the verified ID token.
	s.mergeUserinfo(ctx, tok, &c)
	if (c.Name == "" || c.Email == "") && s.c2 != nil {
		p := s.c2.Profile(ctx, c.Subject)
		if c.Name == "" {
			c.Name = p.Name
		}
		if c.Email == "" {
			c.Email = p.Email
		}
	}
	u, err := s.upsertUser(ctx, c)
	if err != nil {
		return Login{}, err
	}
	return Login{User: u, IDToken: rawID}, nil
}

// LogoutURL is C2's RP-initiated logout URL (OIDC RP-Initiated Logout 1.0 /
// C2-Integration-Guide §6.1). Clearing only the local session leaves the C2
// session and this client's C2 tokens alive, so the user stays SSO'd and is
// silently signed back in on the next protected page. idToken is passed as
// `id_token_hint` — without it C2 cannot tell whose session to end. Returns ""
// when OIDC is not configured, so callers fall back to a local-only logout.
func (s *Service) LogoutURL(idToken string) string {
	if s == nil || s.endSessionURL == "" {
		return ""
	}
	q := url.Values{"client_id": {s.cfg.OIDCClientID}}
	if idToken != "" {
		q.Set("id_token_hint", idToken)
	}
	if s.postLogoutURL != "" {
		q.Set("post_logout_redirect_uri", s.postLogoutURL)
	}
	return s.endSessionURL + "?" + q.Encode()
}

// mergeUserinfo calls the OIDC userinfo endpoint with the access token and fills
// any profile fields the ID token didn't carry. Failures are non-fatal: the ID
// token already established identity. The subject is never overwritten.
func (s *Service) mergeUserinfo(ctx context.Context, tok *oauth2.Token, c *claims) {
	if s.userinfoURL == "" || tok.AccessToken == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.userinfoURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("auth: userinfo fetch failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("auth: userinfo status %d", resp.StatusCode)
		return
	}
	var ui claims
	if err := json.NewDecoder(resp.Body).Decode(&ui); err != nil {
		return
	}
	if c.Email == "" {
		c.Email = ui.Email
	}
	if c.Name == "" {
		c.Name = ui.Name
	}
	if c.GivenName == "" {
		c.GivenName = ui.GivenName
	}
	if c.FamilyName == "" {
		c.FamilyName = ui.FamilyName
	}
	if c.ResidencyStatus == "" {
		c.ResidencyStatus = ui.ResidencyStatus
	}
}

// upsertUser finds the user by C2 subject or creates one, refreshing profile
// fields on each login. The role is never overwritten here — it is owned locally
// (a returning admin stays an admin).
func (s *Service) upsertUser(ctx context.Context, c claims) (*domain.User, error) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = strings.TrimSpace(c.GivenName + " " + c.FamilyName)
	}

	// The role to (at least) hold on this login: the static admin allowlist plus
	// any pending grant an admin created for this email in the management screen.
	desired := domain.RoleResident
	if s.isAdminEmail(c.Email) {
		desired = domain.RoleAdmin
	}
	grantRole, grantID, hasGrant := s.pendingGrant(ctx, c.Email)
	if hasGrant && domain.RoleRank(grantRole) > domain.RoleRank(desired) {
		desired = grantRole
	}

	var u domain.User
	err := s.db.WithContext(ctx).Where("subject = ?", c.Subject).First(&u).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		u = domain.User{Subject: c.Subject, Email: c.Email, Name: name, Role: desired}
		if c.residencyAsserted() {
			u.IsResident = c.isActiveResident()
		}
		if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		// Refresh profile; promote if the desired role is higher, but never
		// demote a role already granted locally.
		updates := map[string]any{"email": c.Email, "name": name}
		if domain.RoleRank(desired) > domain.RoleRank(u.Role) {
			updates["role"] = desired
			u.Role = desired
		}
		// Honour a fresh residency assertion from C2 (authoritative over the
		// self-attested flag); absent a claim, leave the existing value alone.
		if c.residencyAsserted() {
			resident := c.isActiveResident()
			updates["is_resident"] = resident
			u.IsResident = resident
		}
		u.Email, u.Name = c.Email, name
		if err := s.db.WithContext(ctx).Model(&u).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	// Consume the grant once the user actually holds (at least) its role.
	if hasGrant && domain.RoleRank(u.Role) >= domain.RoleRank(grantRole) {
		_ = s.db.WithContext(ctx).Delete(&domain.RoleGrant{}, "id = ?", grantID).Error
	}
	return &u, nil
}

// pendingGrant looks up an unclaimed role grant for an email (case-insensitive).
func (s *Service) pendingGrant(ctx context.Context, email string) (domain.Role, string, bool) {
	var g domain.RoleGrant
	e := strings.ToLower(strings.TrimSpace(email))
	if err := s.db.WithContext(ctx).Where("LOWER(email) = ?", e).First(&g).Error; err != nil {
		return "", "", false
	}
	return g.Role, g.ID, true
}

// isAdminEmail reports whether the email is in the configured admin allowlist.
func (s *Service) isAdminEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, a := range s.cfg.AdminEmails {
		if a == email {
			return true
		}
	}
	return false
}

// VerifyServiceToken verifies a service-to-service bearer JWT (the C2 service-card
// callout) against C2's JWKS — RS256 signature, issuer, audience (== our
// client_id), and expiry, the same checks applied to ID tokens at login — and
// returns the subject claim on success.
func (s *Service) VerifyServiceToken(ctx context.Context, raw string) (string, error) {
	if s == nil || s.verifier == nil {
		return "", ErrNotConfigured
	}
	tok, err := s.verifier.Verify(ctx, raw)
	if err != nil {
		return "", err
	}
	return tok.Subject, nil
}

type backchannelLogoutClaims struct {
	Subject string                     `json:"sub"`
	Events  map[string]json.RawMessage `json:"events"`
	Nonce   *string                    `json:"nonce"`
	JTI     string                     `json:"jti"`
	Issued  int64                      `json:"iat"`
}

// VerifyBackchannelLogoutToken validates an OIDC back-channel logout token and
// returns the subject whose local sessions must be revoked.
func (s *Service) VerifyBackchannelLogoutToken(ctx context.Context, raw string) (string, error) {
	if s == nil || s.verifier == nil {
		return "", ErrNotConfigured
	}
	tok, err := s.verifier.Verify(ctx, raw)
	if err != nil {
		return "", ErrInvalidLogoutToken
	}
	var c backchannelLogoutClaims
	if err := tok.Claims(&c); err != nil {
		return "", ErrInvalidLogoutToken
	}
	if err := validateBackchannelLogoutClaims(c); err != nil {
		return "", err
	}
	return c.Subject, nil
}

func validateBackchannelLogoutClaims(c backchannelLogoutClaims) error {
	if strings.TrimSpace(c.Subject) == "" {
		return ErrInvalidLogoutToken
	}
	if c.Nonce != nil {
		return ErrInvalidLogoutToken
	}
	if c.Events == nil {
		return ErrInvalidLogoutToken
	}
	if _, ok := c.Events[backchannelLogoutEvent]; !ok {
		return ErrInvalidLogoutToken
	}
	return nil
}

// sessionTTL is how long a login session lasts.
const sessionTTL = 12 * time.Hour
