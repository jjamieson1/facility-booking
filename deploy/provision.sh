#!/usr/bin/env bash
#
# provision.sh — one-time setup of facility-booking on the muni-demo QA server.
#
# Creates the service account, the directory tree, the database and its
# least-privilege user, the environment file, the TLS certificate, the Apache
# vhost and the systemd unit. Idempotent: re-running it is safe and changes
# nothing that already exists. Secrets it generates are written once and never
# regenerated, because rotating them silently would log every citizen out and
# break the C2 client.
#
# It does NOT ship the application. Run deploy/deploy.sh for that — provision
# installs and enables the unit, and starts it only once a binary is present.
#
# Usage:
#   FB_OIDC_CLIENT_ID=... FB_OIDC_CLIENT_SECRET=... FB_C2_APPLICATION_ID=... \
#     ./deploy/provision.sh
#   ./deploy/provision.sh --dry-run     # print the remote script, change nothing
#   ./deploy/provision.sh --skip-tls    # stop before certbot; print the command
#
# The three C2 values are required on a FIRST run only. Once the env file
# exists on the server it is left strictly alone, so later runs need nothing.
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
source "$HERE/lib/common.sh"

# ---- configuration (override via environment) ----
SERVER="${SERVER:-muni-demo}"           # ssh alias; see deploy.md
DOMAIN="${DOMAIN:-facility-booking.dev-pro.app}"
BASE_PATH="${BASE_PATH:-/facility-booking}"   # no trailing slash
APP_DIR="${APP_DIR:-/app/facility-booking}"
SERVICE="${SERVICE:-facility-booking}"
SVC_USER="${SVC_USER:-facility}"
PORT="${PORT:-8094}"
DB_NAME="${DB_NAME:-facility_booking}"
DB_USER="${DB_USER:-facility_app}"
CERT_EMAIL="${CERT_EMAIL:-jamie@celestialtech.ca}"
ACME_WEBROOT="${ACME_WEBROOT:-/var/www/html}"

DRY_RUN=0
SKIP_TLS=0
for arg in "$@"; do
  case "$arg" in
    --dry-run)  DRY_RUN=1 ;;
    --skip-tls) SKIP_TLS=1 ;;
    -h|--help)  grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $arg" ;;
  esac
done

require_cmd ssh scp sed

# ---------------------------------------------------------------------------
# 1. Recon — read-only, before anything is changed
# ---------------------------------------------------------------------------
# This is a SHARED host running C2, parking and the audit service. Every check
# here is one that, if skipped, could take down somebody else's service:
# binding a port that is in use, or issuing a certificate for a hostname that
# does not point at this machine and having Apache fail to reload.
log "Recon on $SERVER (read-only)"

remote "test -d /etc/apache2" || die "no /etc/apache2 on $SERVER — this script targets Ubuntu + apache2, not httpd/RHEL"

env_exists=0
if remote "test -f '$APP_DIR/$SERVICE.env'"; then
  env_exists=1
  step "env file already present — secrets will be preserved"
fi

port_owner="$(remote "ss -ltnpH 'sport = :$PORT' 2>/dev/null | head -1" || true)"
if [[ -n "$port_owner" ]]; then
  if [[ "$port_owner" == *"$SERVICE"* ]]; then
    step "port $PORT already held by $SERVICE (fine, it is ours)"
  else
    die "port $PORT is in use by something else:
    $port_owner
Pick a free port with PORT=<n> and set FB_ADDR to match."
  fi
else
  step "port $PORT is free"
fi

for m in proxy_http headers rewrite ssl; do
  remote "apache2ctl -M 2>/dev/null | grep -q ${m}_module" \
    || die "apache module ${m} is not enabled: ssh $SERVER 'a2enmod ${m} && systemctl reload apache2'"
done
step "apache modules present: proxy_http headers rewrite ssl"

remote "command -v mysql >/dev/null"   || die "mysql client not found on $SERVER"
remote "command -v certbot >/dev/null" || die "certbot not found on $SERVER"

# The certificate is issued over HTTP-01, so the hostname must already resolve
# to this machine. certbot's failure for this is opaque; this one is not.
server_ip="$(remote "curl -fsS -4 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print \$1}'" || true)"
resolved="$(dig +short "$DOMAIN" 2>/dev/null | tail -1 || true)"
if [[ -n "$resolved" && -n "$server_ip" && "$resolved" != "$server_ip" ]]; then
  warn "$DOMAIN resolves to $resolved but the server reports $server_ip.
    If that is a CNAME chain this may still be correct; certbot will be the judge."
else
  step "$DOMAIN resolves to $resolved"
fi

# ---------------------------------------------------------------------------
# 2. Render the configuration locally
# ---------------------------------------------------------------------------
log "Rendering templates"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
chmod 700 "$STAGE"

render_template "$HERE/apache/facility-booking-http.conf.tmpl" "$STAGE/$SERVICE-http.conf" \
  DOMAIN="$DOMAIN" SERVICE="$SERVICE"
render_template "$HERE/apache/facility-booking.conf.tmpl" "$STAGE/$SERVICE.conf" \
  DOMAIN="$DOMAIN" BASE_PATH="$BASE_PATH" APP_DIR="$APP_DIR" PORT="$PORT" SERVICE="$SERVICE"
render_template "$HERE/systemd/facility-booking.service.tmpl" "$STAGE/$SERVICE.service" \
  APP_DIR="$APP_DIR" SERVICE="$SERVICE" USER="$SVC_USER"
step "apache + systemd rendered"

# The env file is rendered only when the server has none. Regenerating the
# session secret would invalidate every live login; regenerating the database
# password would lock the running service out of its own data.
DB_PASSWORD=""
if [[ "$env_exists" -eq 0 ]]; then
  : "${FB_OIDC_CLIENT_ID:?set FB_OIDC_CLIENT_ID (from the C2 OIDC client) for a first provision}"
  : "${FB_OIDC_CLIENT_SECRET:?set FB_OIDC_CLIENT_SECRET (shown once when the client was created)}"
  : "${FB_C2_APPLICATION_ID:?set FB_C2_APPLICATION_ID (the C2 APPLICATION id, not the OIDC client id)}"

  DB_PASSWORD="$(secret 24)"
  render_template "$HERE/facility-booking.env.example" "$STAGE/$SERVICE.env" \
    APP_DIR="$APP_DIR" \
    SESSION_SECRET="$(secret 32)" \
    DB_PASSWORD="$DB_PASSWORD" \
    OIDC_CLIENT_ID="$FB_OIDC_CLIENT_ID" \
    OIDC_CLIENT_SECRET="$FB_OIDC_CLIENT_SECRET" \
    C2_APPLICATION_ID="$FB_C2_APPLICATION_ID"
  chmod 600 "$STAGE/$SERVICE.env"
  step "env file rendered with generated secrets"
fi

# ---------------------------------------------------------------------------
# 3. Build the remote script
# ---------------------------------------------------------------------------
# Everything that changes the server happens in one script, run once. Doing it
# as twenty separate ssh calls makes a partial failure much harder to reason
# about, and each call re-authenticates.
cat > "$STAGE/remote-provision.sh" <<REMOTE
#!/usr/bin/env bash
set -euo pipefail

APP_DIR='$APP_DIR'
SERVICE='$SERVICE'
SVC_USER='$SVC_USER'
DOMAIN='$DOMAIN'
PORT='$PORT'
DB_NAME='$DB_NAME'
DB_USER='$DB_USER'
DB_PASSWORD='$DB_PASSWORD'
CERT_EMAIL='$CERT_EMAIL'
ACME_WEBROOT='$ACME_WEBROOT'
SKIP_TLS='$SKIP_TLS'
ENV_EXISTS='$env_exists'
STAGE_DIR="\$(dirname "\$0")"

say() { printf '    %s\n' "\$*"; }

# ---- service account ----
if id -u "\$SVC_USER" >/dev/null 2>&1; then
  say "user \$SVC_USER exists"
else
  useradd --system --no-create-home --shell /usr/sbin/nologin "\$SVC_USER"
  say "created system user \$SVC_USER"
fi

# ---- directories ----
# data/ holds citizen-uploaded waivers and is the only writable path the unit
# allows. web/ is served by Apache and is replaced wholesale on each deploy.
mkdir -p "\$APP_DIR/data" "\$APP_DIR/web"
chown -R "\$SVC_USER:\$SVC_USER" "\$APP_DIR/data"
chmod 750 "\$APP_DIR/data"
say "directories ready under \$APP_DIR"

# ---- database ----
# Least privilege: enough for GORM's AutoMigrate to manage its own schema, and
# nothing else. No global grants, and explicitly none of the fbtest_% grants
# from scripts/db-setup.sql — those let the user create and drop databases and
# exist only so a developer machine can spin up throwaway test schemas.
if [ "\$ENV_EXISTS" = "0" ]; then
  mysql <<SQL
CREATE DATABASE IF NOT EXISTS \\\`\$DB_NAME\\\`
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '\$DB_USER'@'127.0.0.1' IDENTIFIED BY '\$DB_PASSWORD';
ALTER USER '\$DB_USER'@'127.0.0.1' IDENTIFIED BY '\$DB_PASSWORD';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX, REFERENCES, DROP
  ON \\\`\$DB_NAME\\\`.* TO '\$DB_USER'@'127.0.0.1';
FLUSH PRIVILEGES;
SQL
  say "database \$DB_NAME and user \$DB_USER ready"
else
  say "env file exists — leaving database credentials untouched"
fi

# ---- environment file ----
# Config is server state. A deploy ships code; it must never overwrite this.
if [ "\$ENV_EXISTS" = "0" ]; then
  install -o "\$SVC_USER" -g "\$SVC_USER" -m 600 "\$STAGE_DIR/\$SERVICE.env" "\$APP_DIR/\$SERVICE.env"
  say "wrote \$APP_DIR/\$SERVICE.env (0600, \$SVC_USER)"
else
  say "kept existing \$APP_DIR/\$SERVICE.env"
fi

# ---- TLS certificate ----
if [ -d "/etc/letsencrypt/live/\$DOMAIN" ]; then
  say "certificate for \$DOMAIN already exists"
elif [ "\$SKIP_TLS" = "1" ]; then
  say "--skip-tls: certificate NOT issued. Run:"
  say "    certbot certonly --webroot -w \$ACME_WEBROOT -d \$DOMAIN"
else
  # The real vhost cannot load without a certificate, and certbot cannot get a
  # certificate without a vhost answering on :80. The bootstrap vhost breaks
  # that circle: it serves the ACME challenge and 404s everything else, so the
  # hostname never briefly serves another site's content.
  install -m 644 "\$STAGE_DIR/\$SERVICE-http.conf" "/etc/apache2/sites-available/\$SERVICE-http.conf"
  a2ensite "\$SERVICE-http" >/dev/null
  apache2ctl configtest || { a2dissite "\$SERVICE-http" >/dev/null; exit 1; }
  systemctl reload apache2
  say "bootstrap HTTP vhost enabled"

  certbot certonly --webroot -w "\$ACME_WEBROOT" -d "\$DOMAIN" \
    --non-interactive --agree-tos -m "\$CERT_EMAIL"
  say "certificate issued for \$DOMAIN"

  a2dissite "\$SERVICE-http" >/dev/null
  rm -f "/etc/apache2/sites-available/\$SERVICE-http.conf"
fi

# ---- apache vhost ----
if [ -d "/etc/letsencrypt/live/\$DOMAIN" ]; then
  # Back up whatever is there so a bad configtest can be put back exactly.
  if [ -f "/etc/apache2/sites-available/\$SERVICE.conf" ]; then
    cp "/etc/apache2/sites-available/\$SERVICE.conf" "/etc/apache2/sites-available/\$SERVICE.conf.bak"
  fi
  install -m 644 "\$STAGE_DIR/\$SERVICE.conf" "/etc/apache2/sites-available/\$SERVICE.conf"
  a2ensite "\$SERVICE" >/dev/null
  if apache2ctl configtest; then
    systemctl reload apache2
    say "vhost installed and apache reloaded"
  else
    # Leave the RUNNING config untouched: apache has not been reloaded, so the
    # other services on this box are unaffected either way.
    if [ -f "/etc/apache2/sites-available/\$SERVICE.conf.bak" ]; then
      mv "/etc/apache2/sites-available/\$SERVICE.conf.bak" "/etc/apache2/sites-available/\$SERVICE.conf"
    else
      a2dissite "\$SERVICE" >/dev/null
      rm -f "/etc/apache2/sites-available/\$SERVICE.conf"
    fi
    echo "apache configtest FAILED — config rolled back, apache not reloaded" >&2
    exit 1
  fi
  rm -f "/etc/apache2/sites-available/\$SERVICE.conf.bak"
else
  say "no certificate yet — TLS vhost not installed"
fi

# ---- systemd unit ----
install -m 644 "\$STAGE_DIR/\$SERVICE.service" "/etc/systemd/system/\$SERVICE.service"
systemctl daemon-reload
systemctl enable "\$SERVICE" >/dev/null 2>&1 || true
say "systemd unit installed and enabled"

# Start only if a binary is actually there. On a first provision it is not, and
# starting would produce a crash loop that looks like a configuration fault.
if [ -x "\$APP_DIR/\$SERVICE" ]; then
  systemctl restart "\$SERVICE"
  sleep 2
  if systemctl is-active --quiet "\$SERVICE" && curl -fsS "http://127.0.0.1:\$PORT/healthz" >/dev/null; then
    say "service is active and healthy on 127.0.0.1:\$PORT"
  else
    echo "service did not come up healthy — journalctl -u \$SERVICE -n 50" >&2
    exit 1
  fi
else
  say "no binary at \$APP_DIR/\$SERVICE yet — run deploy/deploy.sh to ship one"
fi
REMOTE
chmod 700 "$STAGE/remote-provision.sh"

if [[ "$DRY_RUN" -eq 1 ]]; then
  log "--dry-run: the remote script below would run on $SERVER. Nothing was changed."
  sed 's/^/    /' "$STAGE/remote-provision.sh"
  exit 0
fi

# ---------------------------------------------------------------------------
# 4. Ship and run
# ---------------------------------------------------------------------------
# /root is mode 700, so the staged env file is never world-readable even for
# the moment it sits there.
log "Provisioning $SERVER"
REMOTE_STAGE="/root/.provision-$SERVICE.$$"
remote "mkdir -p '$REMOTE_STAGE' && chmod 700 '$REMOTE_STAGE'"
# shellcheck disable=SC2086
scp -q "$STAGE"/* "$SERVER:$REMOTE_STAGE/"
remote "chmod 600 '$REMOTE_STAGE/$SERVICE.env' 2>/dev/null || true"

set +e
remote "bash '$REMOTE_STAGE/remote-provision.sh'"
rc=$?
set -e
remote "rm -rf '$REMOTE_STAGE'"
[[ $rc -eq 0 ]] || die "provisioning failed (exit $rc)"

log "Provisioned https://$DOMAIN$BASE_PATH/"
cat <<EOF

Next:
  1. ./deploy/deploy.sh          ship the API and SPA, then start the service
  2. Ask the C2 admin to set the service-card callout URL to
       https://$DOMAIN$BASE_PATH/api/citizens/{sub}/status
  3. Set FB_AUDIT_TOKEN in $APP_DIR/$SERVICE.env once a key is issued, then
       ssh $SERVER 'systemctl restart $SERVICE'

Config lives at $APP_DIR/$SERVICE.env and is never touched by a deploy.
EOF
