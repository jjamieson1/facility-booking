#!/usr/bin/env bash
#
# deploy.sh — build and ship facility-booking to the muni-demo QA server.
#
# Cross-compiles the Go API, builds the React SPA with the right base path,
# pushes both, refreshes the systemd unit and restarts the service. Run
# deploy/provision.sh once first; this script assumes the server is already
# set up and will say so if it is not.
#
# Usage:
#   ./deploy/deploy.sh                 # gate + build + ship + restart
#   ./deploy/deploy.sh --skip-build    # ship the last build again
#   ./deploy/deploy.sh --with-vhost    # also reinstall the Apache vhost
#   ./deploy/deploy.sh --skip-tests    # ship past a failing suite (emergencies)
#
# THE GATE: there is no CI, so this script is the only thing that checks the
# code before QA runs it. go build, go vet and the full test suite must pass.
# The suite runs against MariaDB/MySQL — FB_TEST_MYSQL_DSN must be set, and is
# read from .env if present (~75s with -p 4).
#
# Unlike a production deploy, ANY branch may ship here: that is what a QA
# server is for. A tree that is not clean at origin/main still warns loudly and
# names the commit it does not match, because the binary on the server is then
# traceable to nothing reviewable.
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
# shellcheck source=lib/common.sh
source "$HERE/lib/common.sh"

# ---- configuration (override via environment) ----
SERVER="${SERVER:-muni-demo}"
DOMAIN="${DOMAIN:-facility-booking.dev-pro.app}"
BASE_PATH="${BASE_PATH:-/facility-booking}"
APP_DIR="${APP_DIR:-/app/facility-booking}"
SERVICE="${SERVICE:-facility-booking}"
SVC_USER="${SVC_USER:-facility}"
PORT="${PORT:-8094}"
TARGET_ARCH="${TARGET_ARCH:-amd64}"
BUILD_DIR="$HERE/build"

SKIP_BUILD=0; WITH_VHOST=0; SKIP_TESTS=0
for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    --with-vhost) WITH_VHOST=1 ;;
    --skip-tests) SKIP_TESTS=1 ;;
    -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $arg" ;;
  esac
done

require_cmd ssh scp rsync git

# ---------------------------------------------------------------------------
# 1. Provenance — warn, do not refuse
# ---------------------------------------------------------------------------
# deploy.sh builds from the CURRENT working tree. On a QA box that is the
# point: you are here to try a branch. But whoever looks at the server later
# deserves to know the binary matches no commit, so say it plainly.
if [[ "$SKIP_BUILD" -eq 0 ]]; then
  branch="$(git -C "$ROOT" rev-parse --abbrev-ref HEAD)"
  head_sha="$(git -C "$ROOT" rev-parse --short HEAD)"
  dirty_count="$(git -C "$ROOT" status --porcelain | grep -c . || true)"
  if [[ "$dirty_count" -gt 0 ]]; then
    warn "Shipping a DIRTY tree: $dirty_count uncommitted file(s) on '$branch' ($head_sha).
    What lands on $DOMAIN will match no commit. Fine for QA; never for production."
  else
    step "Shipping '$branch' at $head_sha (clean)"
  fi
fi

# ---------------------------------------------------------------------------
# 2. Gate — build, vet, test
# ---------------------------------------------------------------------------
if [[ "$SKIP_TESTS" -eq 0 ]]; then
  if [[ -z "${FB_TEST_MYSQL_DSN:-}" && -f "$ROOT/.env" ]]; then
    set -a; . "$ROOT/.env"; set +a      # sourced, never echoed: it holds passwords
  fi
  [[ -n "${FB_TEST_MYSQL_DSN:-}" ]] || die "FB_TEST_MYSQL_DSN is not set, so the suite cannot run.

The tests run against a real MySQL/MariaDB because a suite that cannot exercise
row locking and foreign keys proves nothing about the booking path. Set it in
.env (single-quoted — the DSN contains & and parentheses) with NO database name:
    FB_TEST_MYSQL_DSN='facility_app:PASSWORD@tcp(127.0.0.1:3306)/?parseTime=true&loc=UTC&charset=utf8mb4'
Or pass --skip-tests to ship without the gate."

  log "Gate: go build"; ( cd "$ROOT" && go build ./... )
  log "Gate: go vet";   ( cd "$ROOT" && go vet ./... )
  log "Gate: go test (~75s)"; ( cd "$ROOT" && go test ./... -p 4 )
  log "Gate passed"
else
  warn "--skip-tests: shipping without running build, vet or the test suite.
    Nothing has checked this code."
fi

# ---------------------------------------------------------------------------
# 3. Build
# ---------------------------------------------------------------------------
if [[ "$SKIP_BUILD" -eq 0 ]]; then
  log "Building API (linux/$TARGET_ARCH, cgo-free)"
  mkdir -p "$BUILD_DIR"
  # CGO_ENABLED=0: the MySQL driver is pure Go, so this is a fully static
  # binary that needs nothing from the server's libc.
  ( cd "$ROOT" && GOOS=linux GOARCH="$TARGET_ARCH" CGO_ENABLED=0 \
      go build -trimpath -ldflags "-s -w" -o "$BUILD_DIR/$SERVICE" ./cmd/server )
  step "$(cd "$BUILD_DIR" && ls -lh "$SERVICE" | awk '{print $5}') binary"

  log "Building SPA (base $BASE_PATH/)"
  # web/src/lib/api.ts derives its request base from Vite's BASE_URL, so this
  # one variable is what makes the SPA call $BASE_PATH/api/... — which Apache
  # strips back to /api/... before the Go service sees it.
  ( cd "$ROOT/web"
    if [[ -f package-lock.json ]]; then npm ci; else npm install; fi
    VITE_BASE="$BASE_PATH/" npm run build )
else
  log "Skipping build (--skip-build)"
  [[ -x "$BUILD_DIR/$SERVICE" ]] || die "no prior build at $BUILD_DIR/$SERVICE"
fi

# ---------------------------------------------------------------------------
# 4. Preconditions on the server
# ---------------------------------------------------------------------------
log "Checking $SERVER"
remote "test -f '$APP_DIR/$SERVICE.env'" \
  || die "no $APP_DIR/$SERVICE.env on $SERVER — run ./deploy/provision.sh first.
This script will not start a service with no configuration."

# The app requires a MySQL/MariaDB DSN and refuses to boot without one. Catch
# that here rather than restarting a working service into a crash loop. Only
# the KEY is ever grepped — the line holds the database password.
remote "grep -qE '^[[:space:]]*FB_DB_DSN=' '$APP_DIR/$SERVICE.env'" \
  || die "$APP_DIR/$SERVICE.env has no FB_DB_DSN; the service would fail to start."
if remote "grep -qE '^[[:space:]]*FB_DB_DRIVER=' '$APP_DIR/$SERVICE.env'"; then
  die "$APP_DIR/$SERVICE.env still sets FB_DB_DRIVER, which no longer exists.
This build is MySQL/MariaDB only — remove it before deploying."
fi
step "server configuration looks sane"

# ---------------------------------------------------------------------------
# 5. Ship the SPA
# ---------------------------------------------------------------------------
log "Syncing SPA to $APP_DIR/web"
# --delete keeps the docroot clean, but the ACME challenge directory must
# survive or certificate renewal silently starts failing 60 days from now.
rsync -az --delete --exclude '.well-known/' \
  "$ROOT/web/dist/" "$SERVER:$APP_DIR/web/"

# ---------------------------------------------------------------------------
# 6. Ship the binary
# ---------------------------------------------------------------------------
log "Uploading API binary"
# Upload beside the target and rename: writing in place gives "text file busy"
# against the running process, and a half-copied binary would be started by the
# restart below.
scp -q "$BUILD_DIR/$SERVICE" "$SERVER:$APP_DIR/$SERVICE.new"
remote "cp -f '$APP_DIR/$SERVICE' '$APP_DIR/$SERVICE.prev' 2>/dev/null || true
        mv -f '$APP_DIR/$SERVICE.new' '$APP_DIR/$SERVICE'
        chown $SVC_USER:$SVC_USER '$APP_DIR/$SERVICE'
        chmod 0755 '$APP_DIR/$SERVICE'"
step "previous binary kept as $SERVICE.prev"

# ---------------------------------------------------------------------------
# 7. Unit, vhost, restart
# ---------------------------------------------------------------------------
log "Refreshing systemd unit"
STAGE="$(mktemp -d)"; trap 'rm -rf "$STAGE"' EXIT
render_template "$HERE/systemd/facility-booking.service.tmpl" "$STAGE/$SERVICE.service" \
  APP_DIR="$APP_DIR" SERVICE="$SERVICE" USER="$SVC_USER"
scp -q "$STAGE/$SERVICE.service" "$SERVER:/etc/systemd/system/$SERVICE.service"
remote "systemctl daemon-reload"

if [[ "$WITH_VHOST" -eq 1 ]]; then
  log "Reinstalling Apache vhost"
  render_template "$HERE/apache/facility-booking.conf.tmpl" "$STAGE/$SERVICE.conf" \
    DOMAIN="$DOMAIN" BASE_PATH="$BASE_PATH" APP_DIR="$APP_DIR" PORT="$PORT" SERVICE="$SERVICE"
  scp -q "$STAGE/$SERVICE.conf" "$SERVER:/etc/apache2/sites-available/$SERVICE.conf"
  # configtest before reload: a syntax error here would take down C2 and
  # parking too, not just this service.
  remote "apache2ctl configtest && systemctl reload apache2"
fi

log "Restarting $SERVICE"
remote "systemctl restart '$SERVICE'"

log "Health check"
# Probe on loopback. Apache only proxies $BASE_PATH/api/ and $BASE_PATH/healthz,
# so the Go service's own /healthz at its root is the direct, reliable check.
if remote "sleep 2; systemctl is-active --quiet '$SERVICE' && curl -fsS 'http://127.0.0.1:$PORT/healthz' >/dev/null"; then
  echo "OK — $SERVICE is active and healthy."
else
  warn "Health check FAILED. Inspect:
    ssh $SERVER 'systemctl status $SERVICE --no-pager; journalctl -u $SERVICE -n 50 --no-pager'
  Roll back:
    ssh $SERVER 'mv $APP_DIR/$SERVICE.prev $APP_DIR/$SERVICE && systemctl restart $SERVICE'"
  exit 1
fi

# The public path exercises Apache, TLS and the prefix strip together — the
# loopback check above passes even when the vhost is broken.
log "Public check"
code="$(curl -s -o /dev/null -w '%{http_code}' "https://$DOMAIN$BASE_PATH/healthz" || true)"
[[ "$code" == "200" ]] && step "https://$DOMAIN$BASE_PATH/healthz -> 200" \
  || warn "https://$DOMAIN$BASE_PATH/healthz -> $code (service is healthy on loopback; check the vhost)"

log "Deployed https://$DOMAIN$BASE_PATH/"
