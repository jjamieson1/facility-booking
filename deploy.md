# Deployment

facility-booking is deployed to the **muni-demo QA server** as
[https://facility-booking.dev-pro.app/facility-booking/](https://facility-booking.dev-pro.app/facility-booking/).

Everything needed lives in **`deploy/`** — `provision.sh` sets the server up
once, `deploy.sh` ships code every time after that. `deploy/README.md` is the
operational detail; this file is the orientation.

## Access to the VM

`ssh muni-demo` connects to the QA server as root, via an ssh config alias. Use
it for any provisioning, deployment or database task; the scripts address the
server by that alias and hold no user, host or key of their own.

## The server

Ubuntu 24.04. Apache terminates TLS and reverse-proxies to Go services bound to
loopback. **It is a shared host** — C2, Parking Pleasure and the audit service
run here too — so deploys are additive, Apache config is only ever reloaded
after `configtest`, and `deploy/provision.sh` refuses to take a port another
service already holds.

| | |
|---|---|
| Apache vhosts | `/etc/apache2/sites-available` (Ubuntu layout, **not** `/etc/httpd/conf.d`) |
| SPA | `/app/facility-booking/web` |
| API binary + config | `/app/facility-booking/` |
| Uploads | `/app/facility-booking/data` |
| Service | `facility-booking.service`, as the `facility` user, on `127.0.0.1:8094` |
| Database | MySQL 8, shared instance; database `facility_booking` |

Neighbouring ports, all loopback: 8090 audit, 8092 c2-api, 8093 parking.

HTTPS uses a Let's Encrypt certificate, renewed automatically by certbot's
timer over the `/var/www/html` webroot.

## C2 integration

The C2 instance on this same box (`https://muni-demo.dev-pro.app/c2`) provides
identity, notifications and payments. Registration is done with the
`dev-app-builder` MCP; ids and the two steps the MCP cannot perform are recorded
in `deploy/README.md`.

The **application** id is `FB_C2_APPLICATION_ID` — the audience C2 stamps on
payment tokens. It is not the OIDC client id, and using one for the other
rejects every settlement.

## Quick reference

```bash
./deploy/deploy.sh                    # gate, build, ship, restart, health-check
ssh muni-demo 'journalctl -u facility-booking -f'
ssh muni-demo 'systemctl restart facility-booking'   # after editing the env file
```

Configuration lives at `/app/facility-booking/facility-booking.env` on the
server and is **never** overwritten by a deploy.
