#!/usr/bin/env bash
#
# deploy.sh — build and deploy Facility Booking (Rivermont Spaces) to production.
#
# Builds the Go API (linux, cgo-free) and the React SPA (Vite base
# "/facility-booking/"), pushes them to the server, installs/refreshes the
# systemd unit, and restarts the API. Follows skills/Security.md §7 (shared
# host): additive, loopback-bound service, Apache reverse-proxy, vhost that loads
# last. Local-first: nothing here runs until you choose to go live.
#
# Usage:
#   ./deployment/deploy.sh                 # gate + build + deploy + restart
#   ./deployment/deploy.sh --skip-build    # deploy the last build only
#   ./deployment/deploy.sh --with-vhost    # also install the vhost + reload httpd
#   ./deployment/deploy.sh --skip-tests    # ship past a failing suite (emergencies)
#   ./deployment/deploy.sh --allow-any-ref # skip the "HEAD == origin/main" check
#   SERVER=root@hosting ./deployment/deploy.sh
#
# SAFETY: there is no CI, so this script is the only gate between a broken
# working tree and production. It refuses to deploy unless
#   1. HEAD is exactly origin/main with a clean tree — deploy.sh builds from the
#      CURRENT working tree, so deploying a stale or dirty checkout silently
#      ships the wrong code;
#   2. build, vet and the full test suite pass. The suite runs against MariaDB
#      (the only supported database), so FB_TEST_MYSQL_DSN must be set — see
#      CLAUDE.md. ~75s.
# Each check has an override for a deliberate emergency, and each override is
# announced loudly in the output so it can't happen by accident.
#
# Requirements on the machine you run this from: bash, go, node/npm, ssh, rsync,
# scp, git, and a reachable MariaDB for the test gate.
#
set -euo pipefail

# ---- configuration (override via environment) ----
SERVER="${SERVER:-root@hosting}"
APP_DIR="${APP_DIR:-/app/facility-booking}"
WEB_DIR="${WEB_DIR:-/var/www/facility-booking}"
SERVICE="${SERVICE:-facility-booking-api}"
BIN_NAME="${BIN_NAME:-facility-booking-api}"
ENV_FILE="${ENV_FILE:-facility-booking.env}"
VHOST_FILE="${VHOST_FILE:-zzz-facility-booking.celestialtech.ca.conf}"
TARGET_ARCH="${TARGET_ARCH:-amd64}"      # server CPU arch: amd64 or arm64
HEALTH_PORT="${HEALTH_PORT:-8094}"       # must match FB_ADDR in the env file
VITE_BASE="${VITE_BASE:-/facility-booking/}"
DOMAIN="${DOMAIN:-facility-booking.celestialtech.ca}"

# ---- paths ----
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
BUILD_DIR="$HERE/build"

SKIP_BUILD=0
WITH_VHOST=0
SKIP_TESTS=0
ALLOW_ANY_REF=0
for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    --with-vhost) WITH_VHOST=1 ;;
    --skip-tests) SKIP_TESTS=1 ;;
    --allow-any-ref) ALLOW_ANY_REF=1 ;;
    -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; exit 2 ;;
  esac
done

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
warn() { printf '\n\033[1;33m!!  %s\033[0m\n' "$*" >&2; }

# ---------------------------------------------------------------------------
# 0a. Safety: only deploy origin/main, from a clean tree
# ---------------------------------------------------------------------------
# deploy.sh rebuilds the SPA + API from the CURRENT working tree, so deploying a
# stale branch or a dirty checkout silently ships code nobody reviewed — in the
# sibling project this reverted the whole frontend once. Uncommitted changes
# matter just as much as the wrong branch: they are in the binary but in no
# commit, so nothing on the server can be traced back to a reviewable state.
if [[ "$SKIP_BUILD" -eq 0 && "$ALLOW_ANY_REF" -eq 0 ]]; then
  log "Verifying the working tree is at origin/main and clean"
  git -C "$ROOT" fetch origin --quiet || { echo "git fetch failed" >&2; exit 1; }
  head_sha="$(git -C "$ROOT" rev-parse HEAD)"
  main_sha="$(git -C "$ROOT" rev-parse origin/main)"
  dirty="$(git -C "$ROOT" status --porcelain)"
  if [[ "$head_sha" != "$main_sha" || -n "$dirty" ]]; then
    cat >&2 <<EOF

Refusing to deploy.
  HEAD:        $head_sha
  origin/main: $main_sha
  uncommitted: $(printf '%s' "$dirty" | grep -c . || true) file(s)

deploy.sh builds from the current working tree, so this would ship code that is
not what origin/main says is deployed. Commit and push, then:
    git fetch origin && git checkout main && git pull
To deploy a non-main or dirty tree on purpose, pass --allow-any-ref.
EOF
    exit 1
  fi
elif [[ "$ALLOW_ANY_REF" -eq 1 ]]; then
  warn "--allow-any-ref: deploying whatever is in the working tree, reviewed or not."
fi

# ---------------------------------------------------------------------------
# 0b. Gate: build, vet, and the full test suite
# ---------------------------------------------------------------------------
# There is no CI. If this gate is skipped, nothing else checks the code before
# it serves the public.
if [[ "$SKIP_TESTS" -eq 0 ]]; then
  if [[ -z "${FB_TEST_MYSQL_DSN:-}" && -f "$ROOT/.env" ]]; then
    # Same pattern the project uses elsewhere: source .env without printing it.
    set -a; . "$ROOT/.env"; set +a
  fi
  if [[ -z "${FB_TEST_MYSQL_DSN:-}" ]]; then
    cat >&2 <<EOF

Refusing to deploy: FB_TEST_MYSQL_DSN is not set, so the test suite cannot run.

The suite runs against MariaDB — the only database this app supports — because a
suite that cannot exercise row locking and foreign keys proves nothing about the
booking path. Set it in .env (single-quoted; the DSN contains & and parentheses)
with NO database name:
    FB_TEST_MYSQL_DSN='facility_app:PASSWORD@tcp(127.0.0.1:3306)/?parseTime=true&loc=UTC&charset=utf8mb4'
Or pass --skip-tests to ship without this gate.
EOF
    exit 1
  fi
  log "Gate: go build"
  ( cd "$ROOT" && go build ./... )
  log "Gate: go vet"
  ( cd "$ROOT" && go vet ./... )
  log "Gate: go test against MariaDB (~75s)"
  ( cd "$ROOT" && go test ./... -p 4 )
  log "Gate passed"
else
  warn "--skip-tests: shipping without running build, vet or the test suite."
fi

# ---------------------------------------------------------------------------
# 1. Build
# ---------------------------------------------------------------------------
if [[ "$SKIP_BUILD" -eq 0 ]]; then
  log "Building API (linux/$TARGET_ARCH, cgo-free)"
  mkdir -p "$BUILD_DIR"
  # CGO_ENABLED=0: every driver in use is pure Go (gorm mysql), so the binary is
  # fully static and needs no libc on the server.
  ( cd "$ROOT"
    GOOS=linux GOARCH="$TARGET_ARCH" CGO_ENABLED=0 \
      go build -trimpath -ldflags "-s -w" -o "$BUILD_DIR/$BIN_NAME" ./cmd/server )

  log "Building web SPA (Vite base $VITE_BASE)"
  # api.ts derives its request base from Vite's BASE_URL, so setting VITE_BASE is
  # all that's needed — the SPA calls /facility-booking/api/... which Apache
  # strips to /api/... before the Go API sees it.
  ( cd "$ROOT/web"
    if [[ -f package-lock.json ]]; then npm ci; else npm install; fi
    VITE_BASE="$VITE_BASE" npm run build )
else
  log "Skipping build (--skip-build)"
  [[ -f "$BUILD_DIR/$BIN_NAME" ]] || { echo "No prior build at $BUILD_DIR/$BIN_NAME" >&2; exit 1; }
fi

# ---------------------------------------------------------------------------
# 2. Preconditions on the server
# ---------------------------------------------------------------------------
log "Preparing directories on $SERVER"
# data/ holds uploaded waivers — the only path the
# service writes to while the rest of the FS stays read-only (ProtectSystem=full).
ssh "$SERVER" "mkdir -p '$APP_DIR' '$APP_DIR/data' '$WEB_DIR'"

# The env file holds secrets and is managed by hand. Refuse to (re)start a
# service that has no config yet.
if ! ssh "$SERVER" "test -f '$APP_DIR/$ENV_FILE'"; then
  log "No $APP_DIR/$ENV_FILE on the server — uploading the template and stopping"
  scp "$HERE/$ENV_FILE.example" "$SERVER:$APP_DIR/$ENV_FILE.example"
  cat >&2 <<EOF

  The API config is missing. On the server:
      cp $APP_DIR/$ENV_FILE.example $APP_DIR/$ENV_FILE
      \$EDITOR $APP_DIR/$ENV_FILE      # set session secret, OIDC creds, DB, admins, ...
  then re-run this script.
EOF
  exit 1
fi

# The app now REQUIRES a MariaDB DSN and refuses to start without one (the
# SQLite fallback was removed — see CLAUDE.md). Catch that here, before we
# restart a working service into a crash loop, and give a better message than a
# failed health check would.
#
# grep only for the key and for a SQLite-era value; never print the line, since
# it contains the database password.
log "Checking the server's database configuration"
if ! ssh "$SERVER" "grep -qE '^[[:space:]]*FB_DB_DSN=' '$APP_DIR/$ENV_FILE'"; then
  cat >&2 <<EOF

Refusing to deploy: $APP_DIR/$ENV_FILE has no FB_DB_DSN.

MariaDB is required and there is no fallback, so the service would fail to start.
On the server, set:
    FB_DB_DSN=facility_app:PASSWORD@tcp(127.0.0.1:3306)/facility_booking?parseTime=true&loc=UTC&charset=utf8mb4
(create the database first with scripts/db-setup.sql), then re-run.
EOF
  exit 1
fi
# A pre-existing deployment will still carry the old SQLite settings. Those now
# stop the app from booting, so name them explicitly rather than letting the
# operator discover it from a crash loop.
if ssh "$SERVER" "grep -qE '^[[:space:]]*FB_DB_DRIVER=' '$APP_DIR/$ENV_FILE' || grep -qE '^[[:space:]]*FB_DB_DSN=.*\.db([[:space:]]|\$)' '$APP_DIR/$ENV_FILE'"; then
  cat >&2 <<EOF

Refusing to deploy: $APP_DIR/$ENV_FILE still carries SQLite-era database settings.

FB_DB_DRIVER no longer exists, and a path-style FB_DB_DSN (a .db file) is no
longer valid — this build is MariaDB-only. Migrating an existing deployment:
    1. Create the database + app user:  scripts/db-setup.sql
    2. Move any data you need out of the old SQLite file FIRST — this deploy
       will not read it, and nothing migrates it for you.
    3. Remove FB_DB_DRIVER and set a MariaDB FB_DB_DSN.
Then re-run.
EOF
  exit 1
fi

# ---------------------------------------------------------------------------
# 3. Deploy web assets
# ---------------------------------------------------------------------------
log "Syncing SPA to $WEB_DIR"
# --delete keeps the docroot clean, but never touch the ACME challenge dir.
rsync -az --delete --exclude '.well-known/' \
  "$ROOT/web/dist/" "$SERVER:$WEB_DIR/"

# ---------------------------------------------------------------------------
# 4. Deploy API binary (atomic rename avoids "text file busy")
# ---------------------------------------------------------------------------
log "Uploading API binary"
scp "$BUILD_DIR/$BIN_NAME" "$SERVER:$APP_DIR/$BIN_NAME.new"
ssh "$SERVER" "mv -f '$APP_DIR/$BIN_NAME.new' '$APP_DIR/$BIN_NAME' && chmod 0755 '$APP_DIR/$BIN_NAME'"

# ---------------------------------------------------------------------------
# 5. Install/refresh the systemd unit
# ---------------------------------------------------------------------------
log "Installing systemd unit"
scp "$HERE/$SERVICE.service" "$SERVER:/etc/systemd/system/$SERVICE.service"
ssh "$SERVER" "systemctl daemon-reload && systemctl enable '$SERVICE' >/dev/null 2>&1 || true"

# ---------------------------------------------------------------------------
# 6. (optional) Apache vhost
# ---------------------------------------------------------------------------
if [[ "$WITH_VHOST" -eq 1 ]]; then
  log "Installing Apache vhost + reloading httpd"
  # Named zzz-* so it loads LAST and never silently becomes the default vhost on
  # this shared host. configtest before reload; abort (leaving the running config
  # untouched) if it fails.
  scp "$HERE/$VHOST_FILE" "$SERVER:/etc/httpd/conf.d/$VHOST_FILE"
  ssh "$SERVER" "apachectl configtest && systemctl reload httpd"
fi

# ---------------------------------------------------------------------------
# 7. Restart + health check
# ---------------------------------------------------------------------------
log "Restarting $SERVICE"
ssh "$SERVER" "systemctl restart '$SERVICE'"

log "Health check"
# Check on loopback: the Go API serves /healthz at the root (Apache only proxies
# /facility-booking/api/, not /healthz), so this is the reliable probe.
if ssh "$SERVER" "sleep 2 && systemctl is-active --quiet '$SERVICE' && curl -fsS 'http://127.0.0.1:$HEALTH_PORT/healthz' >/dev/null"; then
  echo "OK — $SERVICE is active and healthy."
else
  echo "WARNING: health check failed. Inspect with:" >&2
  echo "    ssh $SERVER 'systemctl status $SERVICE --no-pager; journalctl -u $SERVICE -n 50 --no-pager'" >&2
  exit 1
fi

log "Deployed https://$DOMAIN/facility-booking/"
