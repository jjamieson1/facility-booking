// Package httpapi builds the chi router: middleware, health check, and the
// per-area handlers mounted under {BasePath}/api.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gorm.io/gorm"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/booking"
	"github.com/jjamieson1/facility-booking/internal/calendar"
	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/entitlement"
	"github.com/jjamieson1/facility-booking/internal/facility"
	"github.com/jjamieson1/facility-booking/internal/notify"
	"github.com/jjamieson1/facility-booking/internal/payment"
	"github.com/jjamieson1/facility-booking/internal/policy"
	"github.com/jjamieson1/facility-booking/internal/reports"
	"github.com/jjamieson1/facility-booking/internal/servicecard"
	"github.com/jjamieson1/facility-booking/internal/users"
	"github.com/jjamieson1/facility-booking/internal/waitlist"
	"github.com/jjamieson1/facility-booking/internal/waiver"
)

// Deps carries the config and constructed services the handlers need.
type Deps struct {
	Cfg             config.Config
	Auth            *auth.Service // may be nil when OIDC is not configured
	Facility        *facility.Service
	Booking         *booking.Service
	Payment         *payment.Service
	Reports         *reports.Service
	Waitlist        *waitlist.Service
	Waiver          *waiver.Service
	Users           *users.Service
	Calendar        *calendar.Service
	Entitlements    *entitlement.Service
	PaymentSettings *payment.SettingsService
	Policy          *policy.Service
	DB              *gorm.DB
	ServiceCard     *servicecard.Service
	Notifier        notify.Notifier
	Audit           auditlog.Recorder
}

// New builds the top-level handler. The API mounts under {BasePath}/api; a bare
// /healthz is exposed at the root for the deploy health check.
func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders(d.Cfg.IsProd()))
	r.Use(corsMiddleware(d.Cfg.AppOrigin))
	if d.Auth != nil {
		r.Use(d.Auth.Load) // attach current user when a session cookie is present
	}

	r.Get("/healthz", healthz)
	if d.Cfg.BasePath != "" {
		r.Get(d.Cfg.BasePath+"/healthz", healthz)
	}

	fac := facilityHandler{svc: d.Facility}
	bk := bookingHandler{bookings: d.Booking, facilities: d.Facility, payments: d.Payment, waitlist: d.Waitlist, waiver: d.Waiver, entitlements: d.Entitlements, policies: d.Policy, notifier: d.Notifier, audit: d.Audit}
	wv := waiverHandler{svc: d.Waiver}
	rep := reportHandler{svc: d.Reports}
	pmt := paymentHandler{svc: d.Payment}
	ua := userAdminHandler{svc: d.Users}
	sc := serviceCardHandler{auth: d.Auth, svc: d.ServiceCard}
	aud := auditHandler{svc: d.Audit}
	cal := calendarSettingsHandler{svc: d.Calendar}
	ent := entitlementHandler{svc: d.Entitlements}
	psh := paymentSettingsHandler{svc: d.PaymentSettings}
	pol := policyHandler{policies: d.Policy, bookings: d.Booking}
	langh := languageHandler{db: d.DB}
	ah := authHandler{svc: d.Auth, appOrigin: d.Cfg.AppOrigin}

	r.Route(d.Cfg.BasePath+"/api", func(api chi.Router) {
		api.Route("/auth", ah.routes)

		// Public directory + availability.
		api.Get("/facilities", fac.list)
		api.Get("/facilities/{id}", fac.get)
		api.Get("/facilities/{id}/availability", fac.availability)
		api.Get("/facilities/{id}/calendar", fac.calendar)
		api.Get("/facilities/{id}/cancellation-policy", pol.facilityPolicy)

		// City calendar feed (public read-only iCal).
		api.Get("/calendar.ics", bk.feed)

		// Downloadable example waiver template (public).
		api.Get("/waiver-template.pdf", wv.template)

		// C2 service-card callout (server-to-server; authenticated by a C2-issued
		// JWT, not a session cookie — so it sits with the public routes).
		api.Get("/citizens/{sub}/status", sc.status)

		// Booker actions on their own booking. RequireSession, so a guest who
		// booked without an account can still complete and manage that booking —
		// each handler's ownership check is what protects the data.
		api.Group(func(pr chi.Router) {
			pr.Use(auth.RequireSession)
			pr.Post("/bookings", bk.create)
			pr.Put("/me/language", langh.setLanguage)
			pr.Get("/bookings/mine", bk.mine)
			pr.Get("/bookings/{id}", bk.get)
			pr.Get("/bookings/{id}/invite.ics", bk.invite)
			pr.Get("/bookings/{id}/refund-quote", pol.refundQuote)
			pr.Post("/bookings/{id}/cancel", bk.cancel)
			pr.Post("/bookings/{id}/pay", bk.pay)
			// A facility requiring a waiver cannot be confirmed without one, so a
			// guest must be able to upload and retrieve their own.
			pr.Post("/bookings/{id}/waiver", wv.upload)
			pr.Get("/bookings/{id}/waiver", wv.download)
		})

		// Actions tied to a durable identity rather than to a single booking.
		api.Group(func(ar chi.Router) {
			ar.Use(auth.RequireAccount)
			// Entitlements (residency today, fee assistance next). The client
			// submits evidence; the provider decides. Nothing here accepts an
			// outcome from the caller.
			ar.Get("/entitlements", ent.mine)
			ar.Get("/entitlements/{type}/descriptor", ent.descriptor)
			ar.Post("/entitlements/{type}/prove", ent.prove)
			// Ambiguous, so held at the stricter level: moving a booking re-runs
			// availability and pricing, and is easier to reason about once the
			// booker has a durable identity.
			ar.Post("/bookings/{id}/reschedule", bk.reschedule)
			// A waitlist is a standing relationship the city contacts later, which
			// presumes an account to contact.
			ar.Post("/facilities/{id}/waitlist", bk.joinWaitlist)
			ar.Get("/waitlist/mine", bk.myWaitlist)
			ar.Delete("/waitlist/{id}", bk.leaveWaitlist)
		})

		// Staff/admin back-office.
		api.Group(func(sr chi.Router) {
			sr.Use(auth.RequireRole(domain.RoleStaff, domain.RoleAdmin))
			sr.Get("/staff/bookings/pending", bk.pending)
			sr.Post("/staff/bookings/{id}/approve", bk.approve)
			sr.Post("/staff/bookings/{id}/deny", bk.deny)
			sr.Post("/staff/bookings/{id}/refund", bk.refund)
			sr.Post("/staff/facilities", fac.create)
			sr.Put("/staff/facilities/{id}", fac.update)
			sr.Delete("/staff/facilities/{id}", fac.remove)
			sr.Get("/staff/facilities/{id}/blackouts", fac.listBlackouts)
			sr.Post("/staff/facilities/{id}/blackouts", fac.addBlackout)
			sr.Delete("/staff/facilities/{id}/blackouts/{blackoutId}", fac.removeBlackout)
			sr.Get("/staff/reports/summary", rep.summary)
			sr.Get("/staff/payments", pmt.ledger)
			sr.Get("/staff/audit", aud.log)
			// Read-only for staff: which calendar and payment modules the city runs on.
			sr.Get("/staff/calendar-settings", cal.get)
			sr.Get("/staff/payment-settings", psh.get)
		})

		// Admin-only: user/role management.
		api.Group(func(admin chi.Router) {
			admin.Use(auth.RequireRole(domain.RoleAdmin))
			admin.Get("/staff/users", ua.list)
			admin.Post("/staff/users/invite", ua.invite)
			admin.Put("/staff/users/{id}/role", ua.setRole)
			admin.Delete("/staff/users/invites/{id}", ua.revokeInvite)
			// Changing the city's calendar integration is a system-level setting.
			admin.Put("/staff/calendar-settings", cal.update)
			// Changing the gateway the city takes money through, more so.
			admin.Put("/staff/payment-settings", psh.update)
		})
	})
	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// corsMiddleware allows the SPA origin with credentials. A single explicit
// origin (no wildcard) is required because requests carry the session cookie.
func corsMiddleware(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
