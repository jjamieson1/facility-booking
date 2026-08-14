# Deployment

Builds and deploys Facility Booking (Rivermont Spaces) to the production server
(`root@hosting`, `facility-booking.celestialtech.ca`) behind Apache + Let's
Encrypt. **Local-first:** nothing here runs until you choose to go live.

## Layout on the server

| Path | Purpose |
|---|---|
| `/app/facility-booking/facility-booking-api` | the Go API binary (systemd service) |
| `/app/facility-booking/facility-booking.env` | API configuration + secrets (hand-managed) |
| `/app/facility-booking/data/` | uploaded waivers (only writable path) |
| `/var/www/facility-booking/` | the built SPA (Apache docroot) |
| `/etc/systemd/system/facility-booking-api.service` | the service unit |
| `/etc/httpd/conf.d/zzz-facility-booking.celestialtech.ca.conf` | the Apache vhost |

The API listens on **127.0.0.1:8094**; Apache terminates TLS and reverse-proxies
`/facility-booking/api/` → `http://127.0.0.1:8094/api/` (stripping the
`/facility-booking` prefix), and serves the SPA under `/facility-booking/`.

## One-time setup (shared host — do read-only recon first)

Per `skills/Security.md` §7, this host runs other services. Before touching it:

```bash
ssh root@hosting 'httpd -S'                 # existing vhosts + the current default
ssh root@hosting 'ss -ltnp | grep :8094'    # confirm the port is free
getent hosts facility-booking.celestialtech.ca   # confirm DNS -> this server
ssh root@hosting 'ls /etc/letsencrypt/live/facility-booking.celestialtech.ca' # cert?
```

Then:

1. **Config on the server** (the first deploy uploads the template and stops):
   ```bash
   ssh root@hosting
   cp /app/facility-booking/facility-booking.env.example /app/facility-booking/facility-booking.env
   $EDITOR /app/facility-booking/facility-booking.env   # session secret, OIDC creds, admins, ...
   ```
2. **OIDC client:** register this app in the production C2 (Authorization Code +
   PKCE) with redirect URL
   `https://facility-booking.celestialtech.ca/facility-booking/api/auth/callback`.
   Use the helper (it prompts for the admin password and prints the creds):
   ```bash
   C2_API_PROD=https://<c2-host> C2_ADMIN_USER=<admin> \
     scripts/register-c2-client-prod.sh
   ```
   Copy the printed `FB_OIDC_CLIENT_ID` / `FB_OIDC_CLIENT_SECRET` into the env
   file, along with `FB_OIDC_ISSUER` / `FB_OIDC_BASE_URL` (`https://<c2-host>/oidc`).
   Save the `C2_ORG_ID` / `C2_APP_ID` it reports so a re-run reuses the same
   org/app instead of creating duplicates.
3. **TLS cert** (webroot method, no autoconfig on a shared box):
   ```bash
   certbot certonly --webroot -w /var/www/facility-booking -d facility-booking.celestialtech.ca
   ```
   Stage the `:80` block of the vhost first so the ACME challenge can be served.

## Deploy

From your workstation (needs `go`, `node`/`npm`, `ssh`, `rsync`, `scp`):

```bash
./deployment/deploy.sh                 # build + deploy + restart
./deployment/deploy.sh --with-vhost    # also install the vhost and reload httpd
./deployment/deploy.sh --skip-build    # redeploy the last build without rebuilding
SERVER=root@hosting TARGET_ARCH=amd64 ./deployment/deploy.sh
```

Before it ships anything, `deploy.sh` refuses to continue unless:

- **HEAD is exactly `origin/main` and the tree is clean.** It builds from the
  working tree, so a stale branch or uncommitted changes would put code on the
  server that no commit describes. Override with `--allow-any-ref`.
- **`go build`, `go vet` and the full test suite pass** (~75s). The suite runs
  against MariaDB, so `FB_TEST_MYSQL_DSN` must be set — it is read from `.env`
  if present. Override with `--skip-tests`.
- **The server's env file names a MariaDB `FB_DB_DSN`**, and carries no
  SQLite-era settings. This build has no fallback database, so a stale
  `FB_DB_DRIVER=sqlite` or a `.db` path would restart a working service into a
  crash loop. Checked before the restart, not discovered from the health check.

There is no CI: this gate is the only thing between a broken tree and the public
site. Both overrides announce themselves loudly in the output.

What it does:

1. Cross-compiles the API for `linux/amd64`, **cgo-free** (every driver is pure
   Go → fully static binary; override arch with `TARGET_ARCH=arm64`).
2. Builds the SPA with Vite base `/facility-booking/` (`VITE_BASE`). The SPA
   derives its API base from that, so it calls `/facility-booking/api/...`.
3. Rsyncs the SPA to `/var/www/facility-booking` (never touching `.well-known/`).
4. Uploads the binary and swaps it in with an atomic rename.
5. Installs the systemd unit, `daemon-reload`, restarts, and health-checks
   `http://127.0.0.1:8094/healthz` **on loopback** (Apache only proxies
   `/facility-booking/api/`, so the external path isn't a valid health probe).

If `/app/facility-booking/facility-booking.env` is missing, the script uploads
the template and stops so it never starts the service with no configuration.

## Notes

- **Config vs code:** the env file is never overwritten by the deploy. Change it
  on the server and `systemctl restart facility-booking-api`.
- **The vhost loads last** (`zzz-*.conf`) so it can't silently become the shared
  host's default vhost. `apachectl configtest` runs before every reload; re-check
  `httpd -S` before and after.
- **Prefix stripping:** the vhost proxies with trailing slashes so `/facility-booking`
  is stripped before the Go API sees the request — the API keeps serving `/api/...`
  and `FB_BASE_PATH` stays empty. The SPA is a single-page app; `FallbackResource`
  returns `index.html` for client-side routes.
- **Database:** MariaDB is required; there is no fallback, and the app refuses to
  start without `FB_DB_DSN`. Run `scripts/db-setup.sql` (with a real password),
  set `FB_DB_DSN` in the env file, and add `After=mariadb.service` to the unit.
  Do **not** apply that script's `fbtest_%` grants here — they let the app user
  create and drop databases, and exist only for developer machines.
- **Hardening:** the unit runs with `ProtectSystem=full`, `ProtectHome=yes`,
  `PrivateTmp=yes`, `NoNewPrivileges=true`, and only `data/` is writable. To run
  as a dedicated non-root user, follow the commented `User=` block in the unit.
- **Rollback:** keep the previous binary (`cp facility-booking-api facility-booking-api.bak`
  before deploying); restore with `mv facility-booking-api.bak facility-booking-api &&
  systemctl restart facility-booking-api`.
- **Logs:** `journalctl -u facility-booking-api -f` on the server.
