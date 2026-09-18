# Deploying facility-booking to muni-demo (QA)

Ships **Rivermont Spaces** to `https://facility-booking.dev-pro.app/facility-booking/`
on the `muni-demo` QA server, behind Apache + Let's Encrypt, integrated with the
C2 instance running on the same box.

`provision.sh` sets the server up once. `deploy.sh` ships code, every time.

## The server

`muni-demo` is an ssh alias (see `deploy.md`) that connects as root. It is a
**shared host**: C2, Parking Pleasure and the audit service run here too, so
everything in this directory is additive and every Apache change runs
`configtest` before a reload — a syntax error would take those services down as
well, not just this one.

| Path | Purpose |
|---|---|
| `/app/facility-booking/facility-booking` | the Go API binary |
| `/app/facility-booking/facility-booking.env` | configuration + secrets (0600, `facility`) |
| `/app/facility-booking/data/` | uploaded waivers — the only writable path |
| `/app/facility-booking/web/` | the built SPA, served by Apache |
| `/etc/systemd/system/facility-booking.service` | the unit |
| `/etc/apache2/sites-available/facility-booking.conf` | the vhost |

The API listens on **127.0.0.1:8094**. Apache terminates TLS and proxies
`/facility-booking/api/` → `127.0.0.1:8094/api/`, stripping the prefix, which is
why `FB_BASE_PATH` is empty. Neighbours: 8090 audit, 8092 c2-api, 8093 parking.

This layout follows the parking app rather than `deployment/`'s older one: the
box is Ubuntu with apache2, not RHEL with httpd, and the SPA lives under `/app`
rather than `/var/www`.

## First time

```bash
FB_OIDC_CLIENT_ID=c2f_…  FB_OIDC_CLIENT_SECRET=…  FB_C2_APPLICATION_ID=… \
  ./deploy/provision.sh
./deploy/deploy.sh
```

`provision.sh` creates the `facility` system user, the directory tree, the
`facility_booking` database and a least-privilege user, the env file with
generated secrets, the TLS certificate (via a throwaway HTTP vhost that serves
only the ACME challenge), the real vhost, and the systemd unit — enabled, and
started as soon as a binary exists. It is idempotent; `--dry-run` prints the
remote script without touching anything.

The three C2 values come from registering the app in C2 (below) and are needed
on the **first** run only. After that the env file exists and is left alone.

## Every deploy

```bash
./deploy/deploy.sh                 # gate, build, ship, restart, health-check
./deploy/deploy.sh --with-vhost    # also reinstall the Apache vhost
./deploy/deploy.sh --skip-build    # re-ship the last build
./deploy/deploy.sh --skip-tests    # emergencies only; announces itself
```

`go build`, `go vet` and the full suite gate every deploy — there is no CI, so
this is the only thing that checks the code. The suite needs `FB_TEST_MYSQL_DSN`
(read from `.env`).

**Any branch may ship to QA.** A dirty or non-main tree warns loudly and names
the commit it does not match, rather than refusing: trying a branch is what this
server is for, but the binary then corresponds to nothing reviewable and you
should know that. Production would refuse.

## C2 registration

Done through the `dev-app-builder` MCP against the C2 on this box, in the
existing **City of Dev-Pro** organization:

| Object | Id |
|---|---|
| Consent policy | `dd64f660-4f5b-4058-9320-28917a02142a` |
| Application | `11f5e8ad-b4b8-45bb-94d8-fdd08d6d5ff5` → `FB_C2_APPLICATION_ID` |
| OIDC client | `c2f_084ddeb5bd5fac8a14d1393ba02342ab` |
| Service card | `98597462-5d3f-4ebb-b4f4-5072f97a3e59` |

The **application** id is what C2 stamps as the `aud` of every payment token —
not the OIDC client id, which would reject every settlement.

Redirect URI `…/facility-booking/api/auth/callback`, post-logout
`…/facility-booking/`; PKCE is required. The client is confidential because the
same credentials authenticate the partner API for notifications *and* billing.

Two things the MCP cannot do:

- **The service-card callout URL.** It takes `calloutApplicationId` but has no
  field for the URL, so a C2 admin must point the card at
  `https://facility-booking.dev-pro.app/facility-booking/api/citizens/{sub}/status`
  (parking uses `calloutAuthMode: signed_jwt`).
- **Consent scopes.** `list_consent_scopes` is empty on this C2 and parking's
  card previews with none, so there is nothing to grant.

`link_service_card_applications` returns an MCP protocol error — the server
replies with an empty content block — but the write lands. Verify with
`preview_service_card_consent` rather than trusting the error.

## Notes

- **Config is server state.** The env file is never overwritten by a deploy or a
  re-provision. Change it on the server and `systemctl restart facility-booking`.
- **Rollback:** each deploy keeps the previous binary as `facility-booking.prev`.
  `mv facility-booking.prev facility-booking && systemctl restart facility-booking`.
- **Logs:** `journalctl -u facility-booking -f`.
- **Certificate renewal** is certbot's own timer, webroot `/var/www/html` — the
  same mechanism parking uses. The SPA rsync excludes `.well-known/` so a deploy
  cannot break it.
- **Database:** MySQL 8 shared with C2 and parking. Same InnoDB semantics the
  booking path relies on (`SELECT … FOR UPDATE`, foreign keys, REPEATABLE READ).
  The app user has no global grants and **none** of the `fbtest_%` grants from
  `scripts/db-setup.sql`, which exist only for developer machines.
- **Reset the demo data:** clear the rows and restart; the seed repopulates when
  the facilities table is empty.
