package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

const (
	sessionCookie = "fb_session"
	stateCookie   = "fb_login" // short-lived, carries OIDC state + PKCE verifier
)

// OpenSession creates a server-side session for the user and returns its id. The
// login's ID token rides along so logout can present it as `id_token_hint`.
func (s *Service) OpenSession(ctx context.Context, login Login) (string, error) {
	sess := domain.Session{
		UserID:    login.User.ID,
		ExpiresAt: time.Now().Add(sessionTTL),
		IDToken:   login.IDToken,
	}
	if err := s.db.WithContext(ctx).Create(&sess).Error; err != nil {
		return "", err
	}
	return sess.ID, nil
}

// UserForSession resolves the (unexpired) session's user, or an error.
func (s *Service) UserForSession(ctx context.Context, sessionID string) (*domain.User, error) {
	var sess domain.Session
	if err := s.db.WithContext(ctx).Preload("User").First(&sess, "id = ?", sessionID).Error; err != nil {
		return nil, err
	}
	if sess.Expired(time.Now()) || sess.User == nil {
		return nil, errors.New("auth: session expired")
	}
	return sess.User, nil
}

// CloseSession deletes a session (logout) and returns the ID token it carried,
// for the caller to hand C2 as `id_token_hint` on RP-initiated logout. A missing
// session is not an error — logout is idempotent.
func (s *Service) CloseSession(ctx context.Context, sessionID string) (string, error) {
	var sess domain.Session
	if err := s.db.WithContext(ctx).First(&sess, "id = ?", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if err := s.db.WithContext(ctx).Where("id = ?", sessionID).Delete(&domain.Session{}).Error; err != nil {
		return "", err
	}
	return sess.IDToken, nil
}

// CloseSessionsForSubject deletes all local sessions for a C2 subject.
// Back-channel logout tokens from C2 are subject-based (no sid), so the app
// must revoke every session tied to that identity.
func (s *Service) CloseSessionsForSubject(ctx context.Context, subject string) error {
	if strings.TrimSpace(subject) == "" {
		return nil
	}
	users := s.db.WithContext(ctx).Model(&domain.User{}).Select("id").Where("subject = ?", subject)
	return s.db.WithContext(ctx).Where("user_id IN (?)", users).Delete(&domain.Session{}).Error
}

// SetResidency is deliberately gone. It marked a user a resident from a
// submitted address, so anyone could self-declare and take the resident rate.
// Residency is now an entitlement determined by a provider — see
// internal/entitlement.

// --- cookies ---------------------------------------------------------------

func (s *Service) secure() bool { return s.cfg.IsProd() }

// SetSessionCookie writes the HttpOnly session cookie.
func (s *Service) SetSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure(),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
}

// ClearSessionCookie expires the session cookie.
func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: s.secure(), SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// SessionIDFromRequest reads the session cookie value.
func SessionIDFromRequest(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// loginState is the CSRF/PKCE material carried across the OIDC redirect in a
// signed cookie (no server storage needed for the brief round-trip).
type loginState struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r,omitempty"`
}

// NewLoginState builds login-flow state for SetStateCookie.
func NewLoginState(state, verifier, returnTo string) loginState {
	return loginState{State: state, Verifier: verifier, ReturnTo: returnTo}
}

// SetStateCookie stores signed login state for the 10-minute auth round-trip.
func (s *Service) SetStateCookie(w http.ResponseWriter, st loginState) error {
	payload, err := json.Marshal(st)
	if err != nil {
		return err
	}
	value := sign(s.cfg.SessionSecret, payload)
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: value, Path: "/", HttpOnly: true,
		Secure: s.secure(), SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	return nil
}

// ReadStateCookie verifies and decodes the login-state cookie.
func (s *Service) ReadStateCookie(r *http.Request) (loginState, error) {
	var st loginState
	c, err := r.Cookie(stateCookie)
	if err != nil {
		return st, errors.New("auth: missing login state")
	}
	payload, err := verify(s.cfg.SessionSecret, c.Value)
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(payload, &st)
}

// ClearStateCookie removes the login-state cookie after use.
func (s *Service) ClearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/", MaxAge: -1})
}

// sign returns base64(payload) + "." + base64(hmac), tamper-evident via secret.
func sign(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	b64 := base64.RawURLEncoding.EncodeToString
	return b64(payload) + "." + b64(mac.Sum(nil))
}

// verify checks the HMAC and returns the payload, or an error on tampering.
func verify(secret, token string) ([]byte, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("auth: malformed state token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("auth: bad state signature")
	}
	return payload, nil
}
