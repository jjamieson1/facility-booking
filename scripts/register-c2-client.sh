#!/usr/bin/env bash
# register-c2-client.sh — register facility-booking as an OIDC client in C2.
#
# Logs into C2 as the dev bootstrap admin, ensures a "City of Rivermont" org +
# "Rivermont Spaces" application exist, then registers the OIDC relying-party
# client and prints client_id + secret. Idempotent-ish: re-running creates a new
# client (C2 has no upsert here), so register once and store the creds in .env.
#
#   C2_API=http://localhost:8088 scripts/register-c2-client.sh
set -euo pipefail

C2_API="${C2_API:-http://localhost:8088}"
ADMIN_USER="${C2_ADMIN_USER:-admin}"
ADMIN_PASS="${C2_ADMIN_PASS:-admin12345}"
REDIRECT="${FB_OIDC_REDIRECT_URL:-http://localhost:5180/api/auth/callback}"
POST_LOGOUT="${FB_APP_ORIGIN:-http://localhost:5180}/"

JAR=$(mktemp)
trap 'rm -f "$JAR"' EXIT

curl -fsS -c "$JAR" -X POST "$C2_API/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" -o /dev/null
CSRF=$(awk '$6=="c2_csrf"{print $7}' "$JAR")

post() { curl -fsS -b "$JAR" -X POST "$C2_API$1" -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d "$2"; }
jid() { python3 -c "import sys,json;print(json.load(sys.stdin)['id'])"; }

OID=$(post /api/organizations '{"name":"City of Rivermont"}' | jid)
AID=$(post /api/applications "{\"name\":\"Rivermont Spaces\",\"organizationId\":\"$OID\"}" | jid)

post /api/federation/clients "{
  \"organizationId\":\"$OID\",\"applicationId\":\"$AID\",
  \"protocol\":\"OIDC\",\"name\":\"Rivermont Spaces (facility-booking)\",
  \"tokenEndpointAuthMethod\":\"client_secret_post\",\"pkceRequired\":true,
  \"redirectUris\":[\"$REDIRECT\"],\"postLogoutRedirectUris\":[\"$POST_LOGOUT\"],
  \"grantTypes\":[\"authorization_code\",\"refresh_token\"],
  \"responseTypes\":[\"code\"],\"allowedScopes\":[\"openid\",\"profile\",\"email\"]
}" | python3 -c "import sys,json; d=json.load(sys.stdin); print('FB_OIDC_CLIENT_ID='+d['clientId']); print('FB_OIDC_CLIENT_SECRET='+d.get('clientSecret',''))"

echo "# ^ copy these into .env"
