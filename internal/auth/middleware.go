package auth

import (
	"context"
	"net/http"

	"github.com/jjamieson1/facility-booking/internal/domain"
)

type ctxKey int

const userKey ctxKey = 0

// FromContext returns the authenticated user, or nil if the request is anonymous.
func FromContext(ctx context.Context) *domain.User {
	u, _ := ctx.Value(userKey).(*domain.User)
	return u
}

// Load attaches the current user to the request context when a valid session
// cookie is present. It never blocks anonymous requests — RequireSession and
// RequireAccount do that.
func (s *Service) Load(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := SessionIDFromRequest(r); id != "" {
			if u, err := s.UserForSession(r.Context(), id); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), userKey, u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Authenticated routes come in two kinds, and every route must pick one
// deliberately — there is no "any authenticated user" middleware, because that
// is exactly what silently hands a guest session the whole API.
//
//   - RequireSession — any session, including a guest who booked without an
//     account. Use for a booker acting on their own booking, where the handler's
//     own ownership check is what protects the data.
//   - RequireAccount — a real account only. Use for anything tied to a durable
//     identity rather than to one booking: residency and entitlements, or
//     standing relationships the municipality contacts over time.
//
// When it is not obvious which applies, use RequireAccount. Relaxing a route
// later is a one-line change; discovering that guests could reach it is an
// incident.

// RequireSession rejects anonymous requests with 401. Guest sessions pass.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) == nil {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAccount rejects anonymous requests with 401 and guest sessions with
// 403 — a guest is authenticated, so the failure is authorization, not identity.
func RequireAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		if u == nil {
			http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
			return
		}
		if u.IsGuest() {
			http.Error(w, `{"error":"a full account is required for this action"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole rejects users who don't hold one of the allowed roles with 403.
// Admin implicitly satisfies any staff requirement.
func RequireRole(roles ...domain.Role) func(http.Handler) http.Handler {
	allowed := make(map[domain.Role]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := FromContext(r.Context())
			if u == nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}
			if !allowed[u.Role] && u.Role != domain.RoleAdmin {
				http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IsStaff reports whether the user may act in a staff capacity.
func IsStaff(u *domain.User) bool {
	return u != nil && (u.Role == domain.RoleStaff || u.Role == domain.RoleAdmin)
}
