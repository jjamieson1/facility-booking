#!/usr/bin/env bash
#
# Local dev orchestrator for facility-booking (Rivermont Spaces): brings up the
# Go API + React SPA and tears them down cleanly.
#
#   scripts/dev.sh start     # build + start API and SPA
#   scripts/dev.sh stop      # stop everything started by this script
#   scripts/dev.sh restart   # stop then start
#   scripts/dev.sh status     # show what's running
#   scripts/dev.sh logs [api|web]   # tail logs (default: all)
#
# Config comes from .env (OIDC creds + ports) if present. Ports (override via
# env): API from FB_ADDR (default :8086), SPA WEB_PORT=5180 — chosen to coexist
# with C2 (:8088/:5173/:5175) and audit-logging (:8080). The API runs on MariaDB;
# set FB_DB_DSN in .env (single-quoted) or start_api refuses to launch. PIDs and
# logs live under .dev/ (gitignored).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEV="$ROOT/.dev"
mkdir -p "$DEV"

# Load .env (OIDC creds, optional port/driver overrides) if present.
if [ -f "$ROOT/.env" ]; then set -a; . "$ROOT/.env"; set +a; fi

API_PORT="${FB_ADDR:-:8086}"; API_PORT="${API_PORT#:}"
WEB_PORT="${WEB_PORT:-5180}"
API_TARGET="http://localhost:${API_PORT}"

# ---- helpers ---------------------------------------------------------------

pidfile() { echo "$DEV/$1.pid"; }
logfile() { echo "$DEV/$1.log"; }

is_running() { # name -> 0 if a live process is tracked
  local f; f="$(pidfile "$1")"
  [ -f "$f" ] && kill -0 "$(cat "$f")" 2>/dev/null
}

port_in_use() { # port -> 0 only if something is LISTENING (ignore client sockets)
  lsof -iTCP:"${1}" -sTCP:LISTEN -t >/dev/null 2>&1
}

ensure_deps() { # dir -> npm install if node_modules missing
  if [ ! -d "$1/node_modules" ]; then
    echo ">> installing deps in ${1#$ROOT/} ..."
    (cd "$1" && npm install >/dev/null 2>&1)
  fi
}

# ---- start / stop ----------------------------------------------------------

start_api() {
  if is_running api; then echo ">> api already running (pid $(cat "$(pidfile api)"))"; return; fi
  if port_in_use "$API_PORT"; then
    echo "!! port ${API_PORT} already in use by another process — skipping api (set FB_ADDR= to change)"; return
  fi
  echo ">> building api ..."
  (cd "$ROOT" && go build -o "$DEV/fb-api" ./cmd/server)
  # MariaDB is required — there is no fallback. Fail here with something
  # actionable rather than letting the API exit a second later.
  if [ -z "${FB_DB_DSN:-}" ]; then
    echo "!! FB_DB_DSN is not set. MariaDB is required." >&2
    echo "   Create it once:  mariadb -u root -p < scripts/db-setup.sql" >&2
    echo "   Then add FB_DB_DSN to .env (single-quoted — the DSN contains & and parentheses)." >&2
    return 1
  fi
  # Never echo the DSN: it carries the database password.
  echo ">> starting api on :${API_PORT} (mariadb, oidc=${FB_OIDC_CLIENT_ID:+on})"
  # Build a clean binary and run it directly so the PID we track is the real
  # process (go run would fork a child we couldn't reliably stop). OIDC and
  # other FB_* vars come from the sourced .env.
  FB_ADDR=":${API_PORT}" FB_DB_DSN="$FB_DB_DSN" \
    nohup "$DEV/fb-api" >"$(logfile api)" 2>&1 &
  local pid=$!
  echo "$pid" >"$(pidfile api)"
  disown "$pid" 2>/dev/null || true
}

start_web() {
  if is_running web; then echo ">> web already running (pid $(cat "$(pidfile web)"))"; return; fi
  if port_in_use "$WEB_PORT"; then
    echo "!! port ${WEB_PORT} already in use by another process — skipping web (set WEB_PORT= to change)"; return
  fi
  ensure_deps "$ROOT/web"
  echo ">> starting web on :${WEB_PORT} (proxying /api → ${API_TARGET})"
  ( cd "$ROOT/web" && VITE_API_TARGET="$API_TARGET" exec nohup ./node_modules/.bin/vite \
      --port "$WEB_PORT" --strictPort ) >"$(logfile web)" 2>&1 &
  local pid=$!
  echo "$pid" >"$(pidfile web)"
  disown "$pid" 2>/dev/null || true
}

stop_one() { # name
  local name="$1" f; f="$(pidfile "$name")"
  if [ -f "$f" ]; then
    local pid; pid="$(cat "$f")"
    if kill -0 "$pid" 2>/dev/null; then
      echo ">> stopping $name (pid $pid)"
      kill "$pid" 2>/dev/null || true
    fi
    rm -f "$f"
  fi
}

cmd_start() {
  start_api
  start_web
  sleep 1
  echo ""
  echo "  API   http://localhost:${API_PORT}/healthz"
  echo "  App   http://localhost:${WEB_PORT}/"
  echo ""
  echo "Logs: scripts/dev.sh logs   |   Stop: scripts/dev.sh stop"
}

cmd_stop() {
  # Only ever kills processes THIS script started (tracked by pid file). We never
  # blindly free a port — it could belong to C2 or another unrelated project.
  stop_one web
  stop_one api
  echo ">> stopped"
}

cmd_status() {
  for s in api web; do
    if is_running "$s"; then
      printf "  %-4s running (pid %s)\n" "$s" "$(cat "$(pidfile "$s")")"
    else
      printf "  %-4s stopped\n" "$s"
    fi
  done
}

cmd_logs() {
  local which="${1:-all}"
  case "$which" in
    api) tail -f "$(logfile api)" ;;
    web) tail -f "$(logfile web)" ;;
    all) tail -f "$DEV"/api.log "$DEV"/web.log ;;
    *)   echo "usage: dev.sh logs [api|web]"; exit 1 ;;
  esac
}

case "${1:-}" in
  start)   cmd_start ;;
  stop)    cmd_stop ;;
  restart) cmd_stop; cmd_start ;;
  status)  cmd_status ;;
  logs)    shift; cmd_logs "${1:-all}" ;;
  *) echo "usage: scripts/dev.sh {start|stop|restart|status|logs}"; exit 1 ;;
esac
