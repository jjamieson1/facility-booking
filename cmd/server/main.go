// Command server is the facility-booking API entrypoint. It wires
// config → DB (open + migrate + seed) → services → HTTP server.
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/jjamieson1/facility-booking/internal/auditlog"
	"github.com/jjamieson1/facility-booking/internal/auth"
	"github.com/jjamieson1/facility-booking/internal/booking"
	"github.com/jjamieson1/facility-booking/internal/brand"
	"github.com/jjamieson1/facility-booking/internal/c2"
	"github.com/jjamieson1/facility-booking/internal/calendar"
	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/db"
	"github.com/jjamieson1/facility-booking/internal/domain"
	"github.com/jjamieson1/facility-booking/internal/entitlement"
	"github.com/jjamieson1/facility-booking/internal/facility"
	"github.com/jjamieson1/facility-booking/internal/httpapi"
	"github.com/jjamieson1/facility-booking/internal/media"
	"github.com/jjamieson1/facility-booking/internal/notify"
	"github.com/jjamieson1/facility-booking/internal/payment"
	"github.com/jjamieson1/facility-booking/internal/policy"
	"github.com/jjamieson1/facility-booking/internal/reminders"
	"github.com/jjamieson1/facility-booking/internal/reports"
	"github.com/jjamieson1/facility-booking/internal/seed"
	"github.com/jjamieson1/facility-booking/internal/servicecard"
	"github.com/jjamieson1/facility-booking/internal/unpaid"
	"github.com/jjamieson1/facility-booking/internal/users"
	"github.com/jjamieson1/facility-booking/internal/waitlist"
	"github.com/jjamieson1/facility-booking/internal/waiver"
)

func main() {
	cfg := config.Load()

	gdb, err := db.Open(cfg)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}
	if cfg.Seed {
		if err := seed.Run(gdb); err != nil {
			log.Fatalf("startup: seed: %v", err)
		}
	}

	authSvc, err := auth.NewService(context.Background(), gdb, cfg)
	if err != nil {
		log.Printf("startup: OIDC disabled: %v", err) // app still runs without login
	} else if authSvc == nil {
		log.Printf("startup: OIDC not configured — set FB_OIDC_* to enable login")
	}

	// Notifications go through C2's partner API: C2 owns the citizen's inbox and
	// their channel preferences, so this app sends one message and C2 fans it out
	// to email/SMS on the channels they opted into (§4.10). With no partner
	// origin configured the app falls back to logging, which is what keeps the
	// demo runnable without C2.
	partner := c2.New(c2.Config{
		Origin:     cfg.C2PartnerOrigin,
		ClientID:   cfg.OIDCClientID,
		Secret:     cfg.OIDCClientSecret,
		AppBaseURL: cfg.PublicAppURL,
	})
	var notifier notify.Notifier = notify.NewLogNotifier()
	if partner.Configured() {
		notifier = notify.NewC2Notifier(gdb, partner)
	}
	// The municipality's identity, set before anything can serve a calendar
	// invite, a service card or a waiver template.
	brand.Set(cfg.BrandName, cfg.BrandShortName)

	auditRec := auditlog.New(cfg.AuditURL, cfg.AuditToken)

	mediaStore, err := media.NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("startup: media store: %v", err)
	}

	// Background reminders: nudge bookers within 24h of a confirmed booking,
	// scanning every minute (§4.10).
	go reminders.NewScheduler(gdb, notifier, 24*time.Hour, time.Minute).Run(context.Background())

	// Residency is decided by a provider against the municipal address roll —
	// never asserted by the client (§P2-5.11a). A remote provider (C2, a tax-roll
	// service) implements the same interface; only the roll's location differs.
	// Which gateway to charge through is an admin setting, resolved per request
	// so a change takes effect without a restart (§4.7).
	paymentSettings := payment.NewSettingsService(gdb, auditRec)
	// The C2 payment broker rides the same partner API and the same client
	// credentials as notifications — one client, not two. When the partner origin
	// is unset, selecting the broker yields a provider that fails every call
	// rather than one that quietly does nothing.
	paymentSettings.SetBroker(partner, cfg.C2PaymentCallbackURL, cfg.PaymentCurrency)

	entitlementSvc := entitlement.NewService(gdb, auditRec,
		entitlement.NewRollProvider(seed.MunicipalRoll(), 365*24*time.Hour))

	// Cancellation/refund terms (§4.7, §4.9): per facility with a
	// municipality-wide default, resolved at use rather than at construction.
	policySvc := policy.NewService(gdb)

	bookingSvc := booking.NewService(gdb, policySvc)
	waitlistSvc := waitlist.NewService(gdb, notifier)

	// Release bookings billed through a hosted gateway that were never paid
	// (§4.7). C2 settles out of band, so without this one unpaid request holds a
	// popular slot until its own start time — free denial-of-service on the
	// calendar. 24h by product decision; the freed slot opens the waitlist, which
	// is the reason for releasing early rather than at the start time.
	go unpaid.NewSweeper(gdb, notifier, 24*time.Hour, 15*time.Minute, func(b domain.Booking) {
		_, _ = waitlistSvc.NotifyFreed(context.Background(), b.FacilityID, b.StartsAt, b.EndsAt)
	}).Run(context.Background())

	// Background sweeper: expire waitlist entries whose slot has passed (they can
	// no longer free up), keeping the resident list and the C2 callout tidy.
	go waitlist.NewSweeper(waitlistSvc, 15*time.Minute).Run(context.Background())

	handler := httpapi.New(httpapi.Deps{
		Cfg:             cfg,
		Auth:            authSvc,
		Facility:        facility.NewService(gdb),
		Booking:         bookingSvc,
		Payment:         payment.NewService(gdb, paymentSettings.Provider),
		Reports:         reports.NewService(gdb),
		Waitlist:        waitlistSvc,
		Waiver:          waiver.NewService(gdb, mediaStore),
		Users:           users.NewService(gdb, auditRec),
		Calendar:        calendar.NewService(gdb, auditRec),
		Entitlements:    entitlementSvc,
		PaymentSettings: paymentSettings,
		Policy:          policySvc,
		DB:              gdb,
		ServiceCard:     servicecard.NewService(gdb, bookingSvc, waitlistSvc, cfg.PublicAppURL, cfg.Contact),
		Notifier:        notifier,
		Audit:           auditRec,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("facility-booking API listening on %s (env=%s db=mariadb oidc=%v audit=%v notify=%s)",
		cfg.Addr, cfg.Env, cfg.OIDCEnabled(), cfg.AuditURL != "", notifyMode(partner.Configured()))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// notifyMode names where notifications go, for the startup line.
func notifyMode(viaC2 bool) string {
	if viaC2 {
		return "c2"
	}
	return "log"
}
