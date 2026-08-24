# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Current state: Phases 1–6 built and verified end-to-end

Full stack works: config, DB (AutoMigrate + Rivermont seed), domain models, C2 OIDC login
with local sessions/roles, facility directory + CRUD, **parameter + date/time search (§4.3/§4.4)**,
per-day availability, a **public weekly/monthly availability calendar** (`/availability` →
`facility.Calendar` → `GET /api/facilities/{id}/calendar`; open/booked/blackout slots, click an
open slot to deep-link into the booking widget via `?start=&duration=`), booking with
**transactional double-booking prevention**, staff
approve/deny, cancel, my-bookings, mock-Stripe payment + refunds, `.ics` invite + city iCal
feed, log notifications, a utilization/revenue report, and the **staff back-office (§4.8):
facility create/edit + blackout/maintenance dates**. The React/Vite SPA covers the resident
flow (browse/search → book → pay), my-bookings, and staff Approvals/Manage/Reports. Verified in
a browser against a live C2 (sign in → book → pay) and by `go test ./...`.

Also built: **booking reschedule (§4.9)**, **pre-booking reminders (§4.10** — a background
`reminders` scheduler that nudges bookers within 24h, logged, idempotent via
`Booking.ReminderSentAt`), and two **§4.11 extras**: **recurring bookings** (weekly-repeating,
skips conflicting weeks, grouped by `Booking.RecurrenceID`) and a **waitlist** (join a taken
slot; cancel/deny of an overlapping booking notifies waiting residents once, via the `waitlist`
service). The public directory + availability already satisfy §4.11's "public availability
view" (no auth required).

All §4.11 extras are now built: **recurring**, **waitlist**, **map view** (Leaflet + OSM,
facility lat/long, `web/src/pages/MapView.tsx`), **resident verification + resident/non-resident
pricing** (`Facility.NonResidentFeeCents`, `Facility.FeeFor`; residency is now an *entitlement* —
see below), **waivers/insurance upload** (`internal/media` — magic-byte sniff, whitelist,
image re-encode, size/decompression caps, random name, stored 0644 under `FB_DATA_DIR` outside any
web root, served with `nosniff` + locking CSP + attachment; `internal/waiver` gates confirmation),
and **multilingual EN/FR** (`react-i18next`, `web/src/lib/i18n.ts`, header toggle). i18n now
covers **every page** — all UI strings go through `t()` against the `en`/`fr` bundles in
`i18n.ts`; add new strings there.

**Facility *content* is bilingual too** (FAC-12). Translations live in side tables
(`domain.FacilityTranslation`, `AccessoryTranslation`) rather than `NameFr`-style columns, which
would multiply every new translatable field by every language. The **base row holds the default
language** (English), so existing read paths are untouched and English readers cost no extra query.
`facility.Translate` overlays a language **per field**, so a facility translated except its
after-use instructions shows French everywhere it has French. The API reports what fell back
(`untranslated` on the facility response) and the SPA says so — §4.11's criterion is fall back *and
say so*, since silently serving English under a French heading is what makes a bilingual claim
untrue. **Location is deliberately not translatable**: a street address is the same in both
languages, and so are capacity and fees — the staff editor keeps them out of the language tabs.

Language is resolved most-specific-first: `?lang=` (what the SPA sends, following its own toggle) →
the signed-in user's stored `User.Language` → `Accept-Language` → English. An explicit `?lang=`
deliberately beats the stored preference: someone reading a page in one language should get that
page in that language. Notifications translate the facility name into each **recipient's** language
(`C2Notifier.translateFacility`), copying the facility so one recipient's language cannot leak into
another's message. Core v1 + the "further useful" list are complete; facility
*content* (names/descriptions/instructions) is stored in one language — a real bilingual
deployment would translate that data too.

The `seed` package generates a **deterministic year of history** (7 facilities + ~600 bookings +
payments, weighted toward popular facilities and recent months for a rising trend) so the
reporting dashboard is populated on first boot. Tune `historyBookings` in `internal/seed/seed.go`.
The **reports dashboard** (`internal/reports` → `/staff/reports`) supports month/quarter/year with
prev-period deltas: revenue, bookings, day-based utilization (booked days / open days), pending +
overdue, bookings-by-facility, top-spaces revenue+utilization, a 6-month utilization trend, and the
resident/non-resident split (from `Booking.Resident`, captured at booking time). The SPA renders it
as a stat-tile + bar + trend-line + table dashboard (single brand hue per the dataviz method).

**Residency is an entitlement, determined by a provider — never asserted by the client**
(`internal/entitlement`, `features-phase-2.md` §P2-5.11a). `POST /verify-residency` is **gone**: it
took an address from the request body and set `User.IsResident`, so anyone could self-declare and
take the resident rate. Now an `entitlement.Provider` (`Describe`/`Evaluate`/`Enrol`) decides;
`RollProvider` checks a submitted address against `seed.MunicipalRoll()`. Routes:
`GET /api/entitlements`, `GET /api/entitlements/{type}/descriptor` (the proving form renders from
this), `POST /api/entitlements/{type}/prove` (submits **evidence**, never an outcome).

Four things here are easy to get wrong and are covered by tests:

- **Resolve before the transaction.** `booking.Request` takes a `booking.Pricing` resolved by the
  handler; a provider callout inside the booking transaction would hold the double-booking row locks
  for the provider's latency. The same resolved set prices the quote and the charge.
- **Unreachable ≠ denied.** A provider outage serves the last-good cached determination while it is
  still valid (`Determination.Stale`), falling back to normal rates only when nothing usable is
  cached. Do *not* copy `internal/auditlog`'s best-effort pattern for anything that sets a price.
- **Determinations are stamped on the booking** (`domain.BookingEntitlement`), written once with it.
  A later enrolment applies to subsequent bookings; it never reprices a completed one.
- **Disclosure is per type, on the model.** Residency is `DisclosurePublic`; §5.8 subsidy is
  `DisclosureDiscreet` and must never reach a receipt or counter display. Unknown types default to
  discreet.

`User.IsResident` survives only as a read-only cache of the current determination (kept in step by
`syncResidentFlag`) so `Booking.Resident` and `reports.residentPct` keep working. The C2 login
`residency_status` claim still writes it too — a second writer to tidy when a C2 entitlement
provider lands.

**Calendar integration is modular and admin-selectable** (§4.6). `internal/calendar/provider.go`
holds a `Provider` interface (`Publish`/`Withdraw`/`BusyWindows`) plus a registry of `Module`
descriptors — each declaring its name, one-way vs two-way, its config fields, and the env var its
credentials come from. Modules: `ics` (today's one-way feed + invite, the default), `none`,
`google`, `microsoft`. An admin picks one at `/staff/calendar` (`GET /api/staff/calendar-settings`
staff-readable, `PUT` admin-only), stored as the singleton `domain.CalendarIntegration` row and
audited. **Selected vs effective**: the two-way modules aren't built yet, so selecting one records
the municipality's decision while the app keeps running the one-way default (`Settings.Effective` +
`FallbackNotes` say so in the UI) — a `pendingProvider` returns `ErrNotConnected` on every call so a
half-built integration can never silently swallow a booking. **Secrets are never form fields**: the
form takes only non-secret config (calendar id, tenant, time zone); credentials come from
`Module.SecretEnv`. Building out a real Google/Microsoft module means implementing `Provider` and
flipping `Available` — no other layer changes.

This is a **sales demo** ("Rivermont Spaces"), not a production municipal deployment. Payments are
still simulated: `internal/payment` has the module registry and admin form (FAC-2), but the Stripe
module itself is registered `Available: false` and no real gateway is wired.

### Notifications go through C2's partner API

`internal/c2` is the client for C2's **partner API** (`{origin}/partner`) — the machine-to-machine
surface, a sibling of `/api` (citizen sessions). `internal/notify.C2Notifier` uses it for every
booking event. **The payment broker (FAC-2) is the same API, same origin, same client credentials,
same consent gate — reuse this client, don't write a second one.**

Three properties that shape the code:

- **Consent-gated.** C2 sends nothing to a citizen who hasn't accepted this service's terms and
  answers **403**. That is an expected outcome (`c2.ErrNoConsent`), not a failure: don't retry, and
  note C2 audits denied attempts against our `client_id`.
- **We don't choose channels.** C2 always creates the in-app notification and fans out to
  email/SMS per the citizen's own preferences. We send one message with a `shortBody` for SMS —
  which must stand alone, since someone may read only that.
- **We send `sub`, never an email or name.** C2 resolves the person. A test asserts the request body
  carries no personal identifiers.

**Delivery is best-effort and must never disturb the thing it describes.** A failed notification
does not roll back a booking — the booking is the record, the message is a courtesy. Every send path
swallows its error into the log fallback.

**Two consequences worth knowing.** C2 carries **no attachments**, so the `.ics` invite travels as a
*link* to `/api/bookings/{id}/invite.ics` rather than a file. And **guests have no C2 identity**
(`guest:<uuid>` subjects, FAC-24), so they are skipped — expected, not an error.

**Language:** `User.Language` ("en"/"fr") is persisted via `PUT /api/me/language`, which the SPA's
header toggle calls for signed-in users. The browser toggle alone is not enough: notifications are
sent server-side, days later, often to another device. Templates live in `internal/notify/messages.go`
— add both languages or the recipient gets English.

**Staff routing is a placeholder.** "Needs approval" currently notifies every staff/admin account,
capped at `maxStaffFanout`. FAC-27's per-facility approvers replaces this with the right recipients.

**Set `FB_C2_PARTNER_ORIGIN` explicitly in dev — the derived default is wrong here.** It defaults to
`FB_OIDC_BASE_URL` minus `/oidc`, which in this setup is C2's **Vite web origin** (`:5173`). Vite
proxies `/oidc` and `/api` to C2's API but **not** `/partner`, so partner calls 404 against it. Use
C2's API directly:

```
FB_C2_PARTNER_ORIGIN=http://localhost:8088
```

Unlike OIDC — where the issuer origin must match what the browser sees — partner calls are
server-to-server and have no reason to traverse the browser proxy at all. (Probe to confirm: `:8088`
answers **401** on `/partner/notifications`, i.e. the route exists and wants credentials; `:5173`
answers 404.) In production, where the API is served under one origin, the derived default is right.

With no origin configured the app falls back to `LogNotifier`, which is what keeps it runnable
without C2. The startup line reports which: `notify=c2` or `notify=log`.

### Database: MariaDB only

**MariaDB is the only supported database, in dev, test and production. There is no fallback.**
`FB_DB_DSN` has no default: `db.Open` refuses to start without it, and `scripts/dev.sh` refuses to
launch the API. That is deliberate — a fallback lets a missing or malformed DSN produce an app that
boots healthy while writing bookings somewhere nobody backs up. `scripts/db-setup.sql` creates the
database and a least-privilege app user. Never echo the DSN; it carries the password.

A pure-Go SQLite fallback existed until Aug 2026 and was removed. It was not merely unused — it was
**actively harmful**: the SQLite suite passed while the booking path had a deadlock and a silent
double-booking, because `SELECT … FOR UPDATE` is a no-op there and foreign keys are not enforced.
Four test fixtures were also quietly relying on unenforced FKs. Do not reintroduce it.

**Tests run on MariaDB.** `internal/testdb.New(t)` is the single helper every package uses; it
fails the test with setup instructions if **`FB_TEST_MYSQL_DSN`** is unset, rather than skipping
(a skipped suite that reports success is the failure mode this whole change exists to remove). Each
call creates a throwaway `fbtest_<random>` database and drops it on cleanup, so packages stay
isolated in parallel — which is why `db-setup.sql` grants `` `fbtest\_%` `` on **developer machines
only**; never grant that in production.

Use **`go test ./... -p 4`**: about **75s**. Unbounded package parallelism (the default, one per
core) has ~15 packages creating and migrating databases at once and the run balloons to ~9 minutes
— that is contention on the server, not the cost of MariaDB.

### Ports (coexist with the user's other running projects)

facility-booking uses **API `:8086`, SPA `:5180`**. C2 owns `:8088` (API), `:5173` (web),
`:5175` (admin); `audit-logging` owns `:8080`. If a bind fails, never `pkill -f vite`/broad
kills — that stops the user's C2 SPAs; kill by port (`lsof -tnP -iTCP:<port> -sTCP:LISTEN`).
Restart C2's SPAs with `C2/scripts/dev.sh start` (it skips already-running ports).

### Commands

```bash
scripts/dev.sh start|stop|restart|status|logs   # manage API :8086 + SPA :5180 (pids/logs in .dev/)
set -a; . ./.env; set +a; go run ./cmd/server    # API only, foreground (needs .env for OIDC)
set -a; . ./.env; set +a; go test ./... -p 4   # tests (MariaDB required — see below)
go build ./...                           # build
cd web && npm run build                  # type-check (tsc -b) + production build
scripts/register-c2-client.sh            # (re)mint C2 OIDC client creds → .env
```

`scripts/dev.sh` builds the API to a binary and tracks PIDs in `.dev/`, so `stop`
only ever kills what it started — never C2's or another project's ports. To reset the demo data,
clear the rows in the MariaDB database and restart: the seed is idempotent and repopulates when
the facilities table is empty.

**MariaDB is required** — set `FB_DB_DSN` in `.env` (single-quoted: the DSN contains `&` and
parentheses, and an unquoted value silently truncates when the file is sourced). All
env vars are prefixed `FB_` (see README's config table) to avoid clashing with a co-located
C2 instance. `:8080` and Vite `:5173–5175` are often taken by the user's *other* projects —
if a bind fails, use `FB_ADDR=:8091` and `VITE_API_TARGET` rather than killing those processes.

### Key documents

- `requirements.md` — the PRD (v1 scope, personas, per-feature Given/When/Then acceptance
  criteria, data entities, non-functional requirements). Acceptance criteria = definition of done.
- `architecture.md` — the target stack and package layout (mostly realized; see below).
- `deploy.md` — production host, paths, URL (see Deployment).
- `Facility Booking Designs (standalone).html` — visual design reference, titled "Rivermont
  Spaces". The SPA takes **loose inspiration** from it with a shadcn/Tailwind kit, not a pixel match.
- `skills/` — role definitions that govern *how* to work here (see Working conventions).

### Decisions locked for this demo

- **DB**: MariaDB everywhere — dev, test and production. No SQLite, no fallback (see above).
- **Identity**: delegated to **C2 over OIDC** (Authorization Code + PKCE). Facility-booking is
  a relying party — it verifies the ID token against C2's JWKS, upserts a local `User` keyed by
  the C2 `sub`, and mints its own session cookie. Roles (resident/staff/admin) and residency are
  **local** to this app; `FB_ADMIN_EMAILS` promotes known accounts on login. The auth service
  builds endpoints from `FB_OIDC_BASE_URL` and verifies against `FB_OIDC_ISSUER` — these can
  differ, but in this dev setup both are `http://localhost:5173/oidc`. **This requires C2's web
  Vite config to proxy `/oidc` (and `/saml`) to its API** — added to `C2/web/vite.config.ts` so
  the issuer origin is consistent and the authorize round-trip sees C2's session cookie. Client
  registered via C2's `POST /api/federation/clients` (needs WRITE_FEDERATION) — see
  `scripts/register-c2-client.sh`.
- **C2 releases only `sub`** over OIDC (it gates name/email behind an app consent-policy chain
  the demo app doesn't have), so `internal/auth/c2.go` fetches name + primary email from C2's
  identity API (`GET /api/identities/{sub}` + `/emails`) at login, using read-only service creds
  (`FB_C2_API_URL`/`FB_C2_SERVICE_USER`/`FB_C2_SERVICE_PASS`). This is what populates the header
  name and drives `FB_ADMIN_EMAILS` promotion. The auth service still tries the standard userinfo
  endpoint first, so a properly-configured IdP wouldn't need the lookup.
- **Logout runs in both directions** (`C2-Integration-Guide.md` §6). *RP-initiated*: `POST
  /api/auth/logout` deletes the local session and returns `logoutUrl` — C2's `end_session`
  endpoint with `id_token_hint` (the raw ID token, stored on `domain.Session` at login solely
  for this) + `post_logout_redirect_uri` (`FB_OIDC_POST_LOGOUT_REDIRECT_URL`, which must be
  registered on the C2 client). The SPA **navigates** there rather than just invalidating its
  cache: clearing only the local session leaves the C2 SSO session alive, and the SPA's
  `RequireAuth` route guard would bounce the now-anonymous user into a silent re-login — the "logout
  does nothing" bug. *IdP-initiated*: `POST /api/auth/backchannel-logout` validates C2's
  `sub`-based logout token and drops every local session for that subject.
- **Payments**: a simulated Stripe (`MockProvider`) behind a `PaymentProvider` interface — a
  real-looking checkout with test card `4242…`, no keys, no charges. A `StripeProvider` stub
  drops in later without a schema change (`Payment.Provider`/`ProviderRef` already reserve room).
- **Calendar**: one-way `.ics` invite to the booker + a read-only iCal feed for the city. No
  Google/MS OAuth in v1.
- **Deploy**: local-first; `deployment/deploy.sh` is written but not run until you go live.

## Product in one line

A municipal facility-booking web app: residents browse/filter bookable spaces and request
bookings; staff approve requests, manage facilities/availability, and prevent double-bookings.
Confirmed bookings sync to the city calendar and send the booker an `.ics` invite.

## Target architecture (from architecture.md)

A React SPA talks to a Go REST API over JSON; the API persists to MySQL via GORM.

**Frontend** (`web/`): React 18 + TypeScript, built with Vite. `react-router-dom` for routing,
`@tanstack/react-query` for server state (queries/mutations + cache invalidation), Tailwind CSS
with a shadcn-style kit under `components/ui`. A thin `fetch` REST client in `web/src/lib/api.ts`.
Shipped as static assets served by Apache under the `/facility-booking/` base path.

**Backend** (Go, chi router, GORM/MySQL), layered by responsibility:
- `internal/domain` — GORM models, each embedding a shared `Base` (UUID id, timestamps, soft
  delete); an `AllModels()` list drives AutoMigrate.
- `internal/<area>` — service packages holding business logic (e.g. `auth`, `workflow`,
  `search`, `notifications`, `reports`). Each exposes `NewService(db, …)` and its own `Err*`
  sentinel errors.
- `internal/httpapi` — the chi router, request/response helpers, and per-area handlers that
  decode JSON, call services, and map sentinel errors to HTTP status codes.
- `cmd/server/main.go` — wires config → DB (open + AutoMigrate + idempotent seeds) →
  services → HTTP server. A `/healthz` endpoint backs the deploy health check.

**Auth**: HTTP-only session cookies for the web app; personal access tokens (`c2_pat_…`,
stored only as SHA-256 hashes) for API/MCP clients. Org roles enforced in the service layer;
authorization is always server-side.

There is deliberately **no "any authenticated user" middleware** — that is what silently hands a
guest session the whole API. Every non-public route picks one of three, and **when it isn't obvious,
pick the stricter one**:

- `auth.RequireSession` — any session, *including a guest* who booked without an account. For a
  booker acting on their own booking, where the handler's ownership check is the real protection.
- `auth.RequireAccount` — a real account only; a guest gets 403. For anything tied to a durable
  identity rather than one booking (residency/entitlements, waitlist).
- `auth.RequireRole(…)` — staff/admin. Admin implicitly satisfies staff; a guest satisfies nothing.

`internal/httpapi/route_access_test.go` classifies **every** registered route and fails the build if
a new one appears unclassified, plus asserts the behaviour (guest blocked from account routes,
anonymous 401, ownership checks identical for guest and account). Adding a route means adding a line
there — that is intentional.

> Note: some package/token names in `architecture.md` (`devlinks`, `architectures`, the
> `c2_pat_` prefix) appear carried over from a sibling project. Adapt names to this domain
> (facilities, bookings, availability) when scaffolding rather than copying verbatim.

## Deployment (from deploy.md + skills/Security.md §7)

- Production host: `root@hosting`. App URL: `https://facility-booking.celestialtech.ca/`.
- Apache serves the SPA and reverse-proxies `/facility-booking/api` to the Go service
  (stripping the `/facility-booking` prefix). Apache vhost configs live in
  `/etc/httpd/conf.d`; HTTPS via Let's Encrypt (certbot auto-renew).
- SPA static files go to `/var/www/facility-booking`; the API binary to `/app/facility-booking`.
- The API runs as a **systemd** unit behind Apache. `deployment/deploy.sh` (to be written)
  should cross-compile the Go API for linux, build the Vite SPA with base `/facility-booking/`,
  and rsync/scp both to the host.

**There is no CI — `deployment/deploy.sh` is the gate.** It refuses to deploy unless HEAD is exactly
`origin/main` with a clean tree (it builds from the working tree, so a dirty checkout ships code no
commit describes), `go build`/`go vet`/the full MariaDB suite pass, and the server's env file names a
MariaDB `FB_DB_DSN` with no SQLite-era leftovers. `--allow-any-ref` and `--skip-tests` override the
first two for emergencies and announce themselves loudly. Adding a route, a model or a migration
therefore means the suite must pass before anything reaches the public site.

**This is a shared host running other services — deploy additively.** Before changing anything,
do read-only recon (`httpd -S` for existing vhosts and the current default; `ss -ltnp` for a
free port; confirm DNS and existing certs). Name new vhost files to load **last**
(`zzz-<domain>.conf`) so you don't silently become the default vhost. `configtest` before every
reload with backup/rollback. Issue certs with `certbot certonly --webroot` (not `--apache`).
Run the service as a non-root user bound to loopback, reverse-proxied. Never invent or paste
production DB passwords; have the operator set them. Full checklist in `skills/Security.md` §7.

## Key correctness requirements

These are called out in `requirements.md` and are the ones easy to get wrong:

- **No double-booking under concurrency.** A confirmed booking must block that slot for
  everyone, including simultaneous requests. Enforce at the database level (transaction +
  constraint/locking), not just application checks.
- **Conflicts traverse the hierarchy.** A hall and its sub-spaces are the same physical space, so
  a booking on any of them occupies all of them. `facility.ConflictSet` returns a facility plus its
  ancestors and descendants (**not** siblings — those are separate spaces), and both
  `booking.loadWindow` and `facility.loadWindow` gather bookings across that set. The walk is
  depth-capped so a cyclic parent link can't hang a transaction holding locks. Keep the two
  `loadWindow`s in step: if the display path stops matching the booking path, the calendar offers
  slots that then fail on submit.
- **The booking transaction takes two different locks, and both are load-bearing.** This was got
  wrong twice, and only running on MariaDB revealed it.
  1. `lockFacilities` locks each row in the conflict set **one at a time, in sorted id order**. The
     facility rows are named mutexes: any two conflicting requests share at least one id, so they
     serialise, and a single fixed acquisition order means they can't deadlock. Ordering the
     *bookings* query with `ORDER BY` instead does **not** work — `ORDER BY` governs the result set,
     not InnoDB's lock acquisition, and `FOR UPDATE` over a sparse range also takes gap locks.
     Result: MariaDB error 1213, under ordinary concurrent load.
  2. The bookings query keeps its own `FOR UPDATE`. Under REPEATABLE READ a plain `SELECT` reads the
     transaction's **snapshot**, so a request that waited on the facility lock still wouldn't see
     the booking the winner just committed — and both would succeed. A locking read always sees the
     latest committed row. Removing this clause makes every concurrent request win.

  `TestConcurrentHierarchyRequests` covers both.
- **Cancellation and refunds follow a policy, and the quote is the charge.** `internal/policy`
  resolves a facility's own `CancellationPolicy`, else the municipality-wide default (`facility_id
  IS NULL`), else `policy.DefaultPolicy()` — a missing policy must never block a cancellation.
  Refund tiers are evaluated most-generous-first, percentages round **half up** (favouring the
  resident), a non-refundable charge comes off after the percentage, and the result is floored at 0
  and capped at the amount **paid** — not the fee, so an unpaid or free booking refunds nothing. The
  cancel path quotes **before** cancelling and issues **after** the transaction commits: quoting
  afterwards would price a booking that no longer exists, and calling the gateway inside the
  transaction would hold booking row locks for the provider's latency. A failed refund does not
  un-cancel the booking — it is audited as `booking.refund.failed` for staff, because the slot is
  already free. A **partial** refund leaves `Payment.Status` as `paid`: money is still held against
  that booking, and marking it refunded would misreport revenue.
- **Availability must reflect booking rules** — opening hours, min/max duration, blackout/
  maintenance dates, and per-facility buffer time between bookings.
- **Approval workflow**: facilities are either auto-confirm or require staff approval; bookings
  move through pending → confirmed/denied/cancelled, notifying booker and staff at each step.
- **Calendar sync**: confirming a booking sends an `.ics` invite; cancelling/changing withdraws
  or updates both the invite and the city calendar entry.
- **Accessibility (WCAG 2.1 AA)** and **responsive/mobile** are hard requirements for this
  public-sector app, not polish. Foundations are in: `<html lang>` synced to the EN/FR toggle,
  a skip-to-content link, `main`/`nav` landmarks, focus-visible rings on all interactive
  controls (`Button`, nav links, slot/waitlist buttons, lang toggle), `role="alert"`/`status`
  live regions on form errors/notices/spinners, accessible names on every control (verified: no
  images without alt, no unnamed buttons/inputs), and a slate-500 minimum on informational text
  for contrast. Keep this bar when adding UI. Remaining: a full contrast audit + mobile pass.
- **Auditable**: staff actions on bookings (approve, deny, refund, cancel, reschedule) write a
  local `domain.AuditLog` row **and** are mirrored to the central **audit-logging** service
  (`~/developement/audit-logging`, port 8080, append-only + tamper-evident hash chain) via
  `internal/auditlog` — best-effort, non-blocking, `app: "facility-booking"`. Configure with
  `FB_AUDIT_URL` (base URL; `/v1/logs` is appended) + optional `FB_AUDIT_TOKEN`; empty URL
  disables it. A staff **Audit log** screen (`/staff/audit` → `GET /api/staff/audit`, gated by
  `RequireRole`) reads events back from the central service via `auditlog.Recorder.List`.
- **Security headers**: `httpapi/security.go` middleware sets `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy`, and a `default-src 'none'` CSP on every API
  response; HSTS is added only when `FB_ENV=prod`. (The SPA's own headers are an Apache/deploy
  concern — see `skills/Security.md` §7.)

## Git workflow: a branch and a PR per ticket

From FAC-41 onward each ticket gets its own branch, commit and pull request — never commit straight
to `main`:

```bash
git checkout main && git pull
git checkout -b feat/FAC-NN-short-slug     # or fix/, chore/
# ... work, with the suite green ...
git commit                                  # subject starts "FAC-NN: "
git push -u origin feat/FAC-NN-short-slug
gh pr create                                # link the ticket; say what changed and why
```

`deployment/deploy.sh` refuses to deploy unless HEAD is exactly `origin/main` **and the tree is
clean**, so unmerged work cannot reach production — the PR is the path, not a formality.

PR bodies should carry what a reviewer cannot get from the diff: the failure the change prevents,
anything deliberately left out, and which claims were tested versus reasoned about. Ticket comments
in celestial-ticket remain the fuller record.

## Working conventions (skills/)

The `skills/` files define the personas and standards for this project — read them before
non-trivial work:

- `Developer-profile.md` — how to write code here: small, single-purpose functions (~<30 lines,
  shallow nesting via guard clauses), intention-revealing names, DRY without over-abstracting,
  tests alongside every change, secure-by-default. There's a self-review checklist at the end.
- `Code-Review.md` — the review bar (verdict `APPROVE` / `CHANGES REQUESTED`, must-fix vs. nit,
  cite file:line with a concrete suggested fix). A bug fix without a regression test is
  changes-requested.
- `Security.md` — the security review checklist: auth/sessions/cookies, authorization/IDOR,
  API hardening, OWASP Top 10, secrets, dependencies, the shared-server deploy rules (§7), and
  media/file-upload defenses (§7, second one) — sniff magic bytes, exclude SVG, re-encode
  images, store outside the web root, serve through the app with `nosniff` + locking CSP.
