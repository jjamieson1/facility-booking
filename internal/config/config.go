// Package config holds env-driven configuration for the facility-booking API.
package config

import (
	"os"
	"strings"
)

// Config is the resolved runtime configuration. Most fields have a sensible
// development default; the database is deliberately not one of them. FB_DB_DSN
// must be set explicitly — see the note on DBDSN.
type Config struct {
	Env       string // "dev" (default) or "prod"
	Addr      string // listen address, e.g. ":8086"
	BasePath  string // URL prefix the API is mounted under, e.g. "/facility-booking"
	AppOrigin string // public SPA origin, used for CORS and post-login redirect

	// DBDSN is the MariaDB/MySQL DSN and has **no default**. An app that falls
	// back to something when the DSN is missing or malformed is an app that can
	// boot healthy while writing to the wrong place — which is exactly what the
	// old SQLite fallback did. Empty here means db.Open refuses to start.
	DBDSN string

	SessionSecret string // HMAC key for the login-flow state cookie
	DataDir       string // where uploaded waiver docs are stored (outside any web root)

	// OIDC relying-party settings (identity delegated to C2).
	// OIDCIssuer is the identifier C2 stamps in tokens (the `iss` claim).
	// OIDCBaseURL is where the OIDC endpoints are actually reachable as JSON —
	// it can differ from the issuer in dev, where C2's web origin doesn't proxy
	// /oidc but the API origin does. Defaults to OIDCIssuer.
	OIDCIssuer       string
	OIDCBaseURL      string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	// OIDCPostLogoutRedirectURL is where C2 returns the browser after
	// RP-initiated logout. It must be registered with the C2 client as a
	// post_logout_redirect_uri or C2 rejects it.
	OIDCPostLogoutRedirectURL string

	// AdminEmails are promoted to the admin role on login — a demo convenience
	// so a known C2 account can reach the staff back-office without DB edits.
	AdminEmails []string

	// C2 admin-API lookup: C2 releases only `sub` over OIDC, so we fetch the
	// user's name/email from C2's identity API by subject at login. These are the
	// service credentials for that read-only lookup. Empty C2APIURL disables it.
	// C2PartnerOrigin is the C2 origin hosting the partner API ({origin}/partner)
	// — notifications today, the payment broker next. Defaults to the OIDC base
	// URL with a trailing "/oidc" trimmed, since the partner surface is a sibling
	// of the OIDC endpoints; set it explicitly when that guess is wrong.
	C2PartnerOrigin string
	// C2ApplicationID is this app's application id in C2 — the
	// service_provider_id on invoices, and the audience C2 stamps into payment
	// status tokens. It is NOT the OIDC client id; verifying a settlement
	// against the wrong audience would reject every callback. Empty disables the
	// payment callback entirely, which is the safe default: better to take no
	// settlements than to take unverified ones.
	C2ApplicationID string
	// C2PaymentCallbackURL is where C2 pushes signed settlement notices. It must
	// be publicly reachable; when empty, C2 sends nothing and reconciliation
	// falls back to polling the invoice, which C2 calls the source of truth.
	C2PaymentCallbackURL string
	// PaymentCurrency is the ISO-4217 currency invoices are raised in.
	PaymentCurrency string

	C2APIURL      string
	C2ServiceUser string
	C2ServicePass string

	// Central audit-logging service. Staff actions are mirrored to it (append-only,
	// tamper-evident). Empty AuditURL disables forwarding; token is optional.
	AuditURL   string
	AuditToken string

	// Service-card callout (C2 → this app): the public SPA base used to build
	// links back into the app for the card, and a static contact block shown on it.
	PublicAppURL string
	Contact      ServiceCardContact

	Seed bool // seed demo data on boot when the DB is empty (default true)
}

// ServiceCardContact is the static "contact us" block returned on the C2 service
// card callout. Demo defaults; override per-field with FB_CONTACT_*.
type ServiceCardContact struct {
	Address1   string
	City       string
	State      string
	PostalCode string
	Email      string
	Phone      string
}

// OIDCEnabled reports whether OIDC login is configured. When false, auth routes
// return 503 so the rest of the demo still runs without C2.
func (c Config) OIDCEnabled() bool {
	return c.OIDCIssuer != "" && c.OIDCClientID != "" && c.OIDCClientSecret != ""
}

// Load reads configuration from the environment, applying defaults. All keys
// are prefixed FB_ to avoid clashing with a co-located C2 instance.
func Load() Config {
	appOrigin := getenv("FB_APP_ORIGIN", "http://localhost:5180")
	return Config{
		Env:              getenv("FB_ENV", "dev"),
		Addr:             getenv("FB_ADDR", ":8086"),
		BasePath:         strings.TrimRight(getenv("FB_BASE_PATH", ""), "/"),
		AppOrigin:        appOrigin,
		DBDSN:            getenv("FB_DB_DSN", ""),
		SessionSecret:    getenv("FB_SESSION_SECRET", "dev-only-insecure-session-secret-change-me"),
		DataDir:          getenv("FB_DATA_DIR", "data"),
		OIDCIssuer:       getenv("FB_OIDC_ISSUER", ""),
		OIDCBaseURL:      getenv("FB_OIDC_BASE_URL", ""),
		OIDCClientID:     getenv("FB_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: getenv("FB_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  getenv("FB_OIDC_REDIRECT_URL", "http://localhost:5180/api/auth/callback"),
		// Default matches what scripts/register-c2-client.sh registers: the app
		// origin with a trailing slash.
		OIDCPostLogoutRedirectURL: getenv("FB_OIDC_POST_LOGOUT_REDIRECT_URL", appOrigin+"/"),
		AdminEmails:               splitList(getenv("FB_ADMIN_EMAILS", "admin@c2.local")),
		C2PartnerOrigin:           getenv("FB_C2_PARTNER_ORIGIN", partnerOriginFrom(getenv("FB_OIDC_BASE_URL", ""))),
		C2ApplicationID:           getenv("FB_C2_APPLICATION_ID", ""),
		C2PaymentCallbackURL:      getenv("FB_C2_PAYMENT_CALLBACK_URL", ""),
		PaymentCurrency:           getenv("FB_PAYMENT_CURRENCY", "CAD"),
		C2APIURL:                  strings.TrimRight(getenv("FB_C2_API_URL", ""), "/"),
		C2ServiceUser:             getenv("FB_C2_SERVICE_USER", ""),
		C2ServicePass:             getenv("FB_C2_SERVICE_PASS", ""),
		AuditURL:                  strings.TrimRight(getenv("FB_AUDIT_URL", ""), "/"),
		AuditToken:                getenv("FB_AUDIT_TOKEN", ""),
		PublicAppURL:              strings.TrimRight(getenv("FB_PUBLIC_APP_URL", appOrigin), "/"),
		Contact: ServiceCardContact{
			Address1:   getenv("FB_CONTACT_ADDRESS1", "215 Rivermont Way"),
			City:       getenv("FB_CONTACT_CITY", "Rivermont"),
			State:      getenv("FB_CONTACT_STATE", "ON"),
			PostalCode: getenv("FB_CONTACT_POSTAL", "K2P 1L4"),
			Email:      getenv("FB_CONTACT_EMAIL", "facilities@rivermont.ca"),
			Phone:      getenv("FB_CONTACT_PHONE", "+1 555-0142"),
		},
		Seed: getenv("FB_SEED", "true") != "false",
	}
}

// splitList parses a comma-separated env value into a trimmed, lowercased slice.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// partnerOriginFrom derives the C2 origin from the OIDC base URL. C2 mounts the
// partner API at {origin}/partner, a sibling of {origin}/oidc, so trimming the
// OIDC suffix gets there. Returns "" for an empty input, which leaves partner
// calls disabled rather than pointed somewhere arbitrary.
func partnerOriginFrom(oidcBaseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(oidcBaseURL), "/")
	if base == "" {
		return ""
	}
	return strings.TrimSuffix(base, "/oidc")
}

func (c Config) IsProd() bool { return c.Env == "prod" }

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
