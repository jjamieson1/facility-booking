# Rivermont Spaces — Facility Booking

A municipal facility-booking web app (sales demo). Residents browse and book
public spaces; staff approve requests and manage facilities. React + TypeScript
SPA over a Go (chi + GORM) REST API, persisting to **MariaDB/MySQL** — with a
**MariaDB** for storage.

Identity is delegated to the **C2** identity platform over OIDC (added in
Phase 2); no passwords are stored here.

> **Status:** Phases 1–6 built and verified end-to-end (browser: C2 login →
> book → pay). See the roadmap below.

## Run it locally

Requires a MariaDB database — create it with `scripts/db-setup.sql`, set `FB_DB_DSN`, and the app seeds demo data on first boot.
`.env` (gitignored) carries the C2 OIDC creds and the ports. Manage the whole
dev environment with one script (PIDs/logs/db under `.dev/`):

```bash
scripts/dev.sh start      # build + start API :8086 and SPA http://localhost:5180
scripts/dev.sh status     # what's running
scripts/dev.sh restart    # stop then start
scripts/dev.sh logs [api|web]   # tail logs
scripts/dev.sh stop       # stop both (only ever kills what it started)
```

Reset the demo data with `rm .dev/fb-dev.db` before the next `start`.

Or run them separately:

```bash
set -a; . ./.env; set +a
go run ./cmd/server          # API :8086
cd web && npm install && npm run dev   # SPA http://localhost:5180
```

Ports are chosen to coexist with a co-located C2 (`:8088/:5173/:5175`) and the
`audit-logging` project (`:8080`).

## Sign-in (C2 OIDC)

Login is delegated to the **C2** identity platform. To (re)mint client creds:

```bash
scripts/register-c2-client.sh    # logs in as C2 dev admin, prints FB_OIDC_CLIENT_ID/SECRET
```

**C2 dev note:** C2's web dev server must proxy `/oidc` to its API so the issuer
origin is consistent and the authorize round-trip sees the session cookie. Add
`"/oidc": apiTarget` (and `"/saml"`) to `C2/web/vite.config.ts`'s proxy and
restart C2's web SPA. `FB_ADMIN_EMAILS` (default `admin@c2.local`) promotes a
known account to the staff back-office on login.

## Use MariaDB

```bash
mariadb -u root -p < scripts/db-setup.sql      # set a password inside first
FB_DB_DRIVER=mysql \
FB_DB_DSN='facility_app:YOURPW@tcp(127.0.0.1:3306)/facility_booking?parseTime=true&loc=UTC&charset=utf8mb4' \
go run ./cmd/server
```

The app creates and migrates all tables itself; the SQL script only makes the
database and a least-privilege user.

## Smoke test

```bash
curl localhost:8080/healthz
curl localhost:8080/api/facilities
```

## Configuration (env, all prefixed FB_)

| Var | Default | Notes |
| --- | --- | --- |
| `FB_ENV` | `dev` | `prod` in production |
| `FB_ADDR` | `:8080` | API listen address |
| `FB_BASE_PATH` | `""` | URL prefix (prod: `/facility-booking`) |
| `FB_APP_ORIGIN` | `http://localhost:5173` | SPA origin for CORS |
| `FB_DB_DSN` | *(none — required)* | MariaDB DSN; the app refuses to start without it |
| `FB_SEED` | `true` | seed demo data when DB is empty |

## Layout

```
cmd/server/         API entrypoint (config → DB → seed → HTTP)
internal/
  config/           env-driven configuration
  db/               GORM open + AutoMigrate (MariaDB)
  domain/           GORM models (embed Base; AllModels())
  seed/             Rivermont demo data (idempotent)
  httpapi/          chi router, JSON helpers, handlers
web/                React 18 + TS + Vite + Tailwind SPA
scripts/            db-setup.sql, dev.sh
deployment/         deploy.sh (systemd + Apache; run when going live)
```

## Roadmap

- **Phase 1 — Skeleton ✅** DB + migrate + seed, public facility directory, SPA.
- **Phase 2 — Identity ✅** C2 OIDC login (Auth Code + PKCE), local session, roles.
- **Phase 3 — Directory + availability ✅** Facility CRUD, availability slots, filtered search.
- **Phase 4 — Booking + approval ✅** Requests, no-double-book (tested), my-bookings, cancel, audit.
- **Phase 5 — Confirm experience ✅** Mock-Stripe payment + refunds, `.ics` invite + iCal feed, log notifications.
- **Phase 6 — Reports ✅** Utilization + revenue summary.
- **Later (not built):** real Stripe (drop-in behind `PaymentProvider`), two-way
  calendar sync, recurring bookings, waitlist, map view, FR/EN i18n.

## Verified

The full path was driven in a browser against a live C2: sign in → C2 login →
consent → back to the app as a resident → book a free space (auto-confirmed) →
book a paid space → pay with the mock `4242…` card (and see `4000…0002`
declined). Backend logic (double-booking prevention, approval transitions,
availability) is covered by `go test ./...`.
# facility-booking
