#!/usr/bin/env bash
# register-c2-client-prod.sh — register facility-booking as an OIDC client in the
# PRODUCTION C2 identity platform.
#
# Same flow as scripts/register-c2-client.sh (login -> org -> application ->
# federation client) but hardened for a live identity provider:
#   - never falls back to dev credentials or localhost; the prod C2 URL and an
#     admin user are REQUIRED and the URL must be https://;
#   - the admin password is read from the environment or prompted for with
#     `read -s` — never hardcoded, defaulted, or echoed;
#   - reuses an existing organization/application when you pass their ids, so a
#     re-run doesn't create duplicates (C2 has no upsert for clients);
#   - shows what it will do and asks for confirmation before mutating C2.
#
# Required environment:
#   C2_API_PROD=https://<c2-host>     # base URL of the production C2 API
#   C2_ADMIN_USER=<admin username>    # a C2 admin holding WRITE_FEDERATION
#   C2_ADMIN_PASS=<admin password>    # optional — prompted for if unset
#
# Optional (reuse an existing org/app on a re-run instead of creating new ones):
#   C2_ORG_ID=<organization id>
#   C2_APP_ID=<application id>
#
# Optional overrides (default to the production deployment values):
#   FB_OIDC_REDIRECT_URL   FB_APP_ORIGIN
#
# Usage:
#   C2_API_PROD=https://c2.example C2_ADMIN_USER=alice scripts/register-c2-client-prod.sh
#   scripts/register-c2-client-prod.sh --yes     # skip the confirmation prompt
set -euo pipefail

C2_API="${C2_API_PROD:-}"
ADMIN_USER="${C2_ADMIN_USER:-}"
ADMIN_PASS="${C2_ADMIN_PASS:-}"
ORG_ID="${C2_ORG_ID:-}"
APP_ID="${C2_APP_ID:-}"
REDIRECT="${FB_OIDC_REDIRECT_URL:-https://facility-booking.celestialtech.ca/facility-booking/api/auth/callback}"
APP_ORIGIN="${FB_APP_ORIGIN:-https://facility-booking.celestialtech.ca}"
POST_LOGOUT="$APP_ORIGIN/facility-booking/"

ASSUME_YES=0
[[ "${1:-}" == "--yes" ]] && ASSUME_YES=1

die() { echo "error: $*" >&2; exit 1; }

# ---- guards: refuse to run with dev creds or against localhost ----
[[ -n "$C2_API" ]]     || die "set C2_API_PROD to the production C2 base URL (e.g. https://c2.example)"
[[ "$C2_API" == https://* ]] || die "C2_API_PROD must be https:// for production"
case "$C2_API" in
  *localhost*|*127.0.0.1*) die "C2_API_PROD points at localhost — refusing (this is the PROD script; use register-c2-client.sh for dev)";;
esac
[[ -n "$ADMIN_USER" ]] || die "set C2_ADMIN_USER to a C2 admin with WRITE_FEDERATION"
[[ "$ADMIN_USER" == "admin" && -z "${ALLOW_DEFAULT_ADMIN:-}" ]] && \
  die "C2_ADMIN_USER is the dev bootstrap 'admin' — set a real prod admin (or ALLOW_DEFAULT_ADMIN=1 if that is genuinely correct)"
if [[ -z "$ADMIN_PASS" ]]; then
  read -rs -p "C2 admin password for $ADMIN_USER: " ADMIN_PASS; echo
  [[ -n "$ADMIN_PASS" ]] || die "no password provided"
fi

command -v python3 >/dev/null || die "python3 is required (used to parse JSON responses)"

# ---- confirm before touching the identity provider ----
echo "About to register an OIDC client in PRODUCTION C2:"
echo "  C2 API      : $C2_API"
echo "  admin user  : $ADMIN_USER"
echo "  redirect URI: $REDIRECT"
echo "  post-logout : $POST_LOGOUT"
[[ -n "$ORG_ID" ]] && echo "  reuse org   : $ORG_ID"
[[ -n "$APP_ID" ]] && echo "  reuse app   : $APP_ID"
if [[ "$ASSUME_YES" -ne 1 ]]; then
  read -r -p "Proceed? [y/N] " ans
  [[ "$ans" == "y" || "$ans" == "Y" ]] || die "aborted"
fi

# ---- log in (session + double-submit CSRF cookie) ----
JAR=$(mktemp); trap 'rm -f "$JAR"' EXIT
curl -fsS -c "$JAR" -X POST "$C2_API/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" -o /dev/null \
  || die "login failed — check the admin credentials and C2 URL"
CSRF=$(awk '$6=="c2_csrf"{print $7}' "$JAR")
[[ -n "$CSRF" ]] || die "no c2_csrf cookie after login (unexpected C2 response)"

post() { curl -fsS -b "$JAR" -X POST "$C2_API$1" -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d "$2"; }
jid()  { python3 -c "import sys,json;print(json.load(sys.stdin)['id'])"; }

# ---- organization + application (created once, then reused via C2_ORG_ID/APP_ID) ----
if [[ -z "$ORG_ID" ]]; then
  ORG_ID=$(post /api/organizations '{"name":"City of Rivermont"}' | jid) \
    || die "create organization failed (a 'City of Rivermont' org may already exist — pass C2_ORG_ID to reuse it)"
  echo "created organization: $ORG_ID"
fi
if [[ -z "$APP_ID" ]]; then
  APP_ID=$(post /api/applications "{\"name\":\"Rivermont Spaces\",\"organizationId\":\"$ORG_ID\"}" | jid) \
    || die "create application failed (pass C2_APP_ID to reuse an existing one)"
  echo "created application: $APP_ID"
fi

# ---- the OIDC relying-party client ----
post /api/federation/clients "{
  \"organizationId\":\"$ORG_ID\",\"applicationId\":\"$APP_ID\",
  \"protocol\":\"OIDC\",\"name\":\"Rivermont Spaces (facility-booking)\",
  \"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":true,
  \"redirectUris\":[\"$REDIRECT\"],\"postLogoutRedirectUris\":[\"$POST_LOGOUT\"],
  \"grantTypes\":[\"authorization_code\",\"refresh_token\"],
  \"responseTypes\":[\"code\"],\"allowedScopes\":[\"openid\",\"profile\",\"email\"]
}" | python3 -c "import sys,json; d=json.load(sys.stdin); print(); print('FB_OIDC_CLIENT_ID='+d['clientId']); print('FB_OIDC_CLIENT_SECRET='+d.get('clientSecret',''))" \
  || die "client registration failed — does '$ADMIN_USER' hold WRITE_FEDERATION?"

cat <<EOF

# ^ The client secret is shown ONCE. Copy both lines into
#   /app/facility-booking/facility-booking.env on the server, and also set the
#   matching issuer/base URL:
#     FB_OIDC_ISSUER=$C2_API/oidc
#     FB_OIDC_BASE_URL=$C2_API/oidc
#   Keep these for a future re-run (reuses this org/app instead of duplicating):
#     C2_ORG_ID=$ORG_ID   C2_APP_ID=$APP_ID
#   Then on the server: systemctl restart facility-booking-api
#
# Re-running this script WITHOUT C2_ORG_ID/C2_APP_ID mints a brand-new client
# (C2 has no upsert). Revoke stale clients in the C2 admin UI if you re-register.
EOF
