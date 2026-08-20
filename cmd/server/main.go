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
	"github.com/jjamieson1/facility-booking/internal/calendar"
	"github.com/jjamieson1/facility-booking/internal/config"
	"github.com/jjamieson1/facility-booking/internal/db"
	"github.com/jjamieson1/facility-booking/internal/entitlement"
	"github.com/jjamieson1/facility-booking/internal/facility"
	"github.com/jjamieson1/facility-booking/internal/httpapi"
	"github.com/jjamieson1/facility-booking/internal/media"
	"github.com/jjamieson1/facility-booking/internal/notify"
	"github.com/jjamieson1/facility-booking/internal/payment"
	"github.com/jjamieson1/facility-booking/internal/reminders"
	"github.com/jjamieson1/facility-booking/internal/reports"
	"github.com/jjamieson1/facility-booking/internal/seed"
	"github.com/jjamieson1/facility-booking/internal/servicecard"
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

	notifier := notify.NewLogNotifier()
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

	entitlementSvc := entitlement.NewService(gdb, auditRec,
		entitlement.NewRollProvider(seed.MunicipalRoll(), 365*24*time.Hour))

	bookingSvc := booking.NewService(gdb)
	waitlistSvc := waitlist.NewService(gdb, notifier)

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
		ServiceCard:     servicecard.NewService(gdb, bookingSvc, waitlistSvc, cfg.PublicAppURL, cfg.Contact),
		Notifier:        notifier,
		Audit:           auditRec,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("facility-booking API listening on %s (env=%s db=mariadb oidc=%v audit=%v)",
		cfg.Addr, cfg.Env, cfg.OIDCEnabled(), cfg.AuditURL != "")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
