# deploy/docker — provider-agnostic baseline

This directory is the **reusable core** for every bracket-creator cloud deploy.
The provider-specific Terraform modules (GCP, Oracle) render this compose +
Caddyfile onto a VM via cloud-init.  You can also use it directly on any
Linux machine with Docker installed.

## What's here

| File | Purpose |
|---|---|
| `docker-compose.yaml` | App + Caddy stack |
| `Caddyfile` | Automatic-HTTPS reverse proxy |
| `app.env.example` | Environment-variable template |

## Prerequisites

- Docker Engine + Docker Compose plugin installed on the host.
- A domain name (e.g. `tournament.example.com`) with an A record pointing at
  the host's public IP address.
- Ports 80 and 443 open in any host or cloud firewall (port 80 is required for
  the ACME HTTP-01 challenge that issues the TLS certificate).

## Quick start

```bash
# 1. Clone / copy these files to the host
git clone https://github.com/gitrgoliveira/bracket-creator
cd bracket-creator/deploy/docker

# 2. Edit Caddyfile — replace "tournament.example.com" with your real domain
nano Caddyfile

# 3. Create the data directory with the correct owner
#    The app container runs as uid 65534 (non-root scratch image).
#    Without this chown, the app cannot write to the data directory and exits.
sudo mkdir -p ./tournament-data
sudo chown -R 65534:65534 ./tournament-data

# 4. Create your app.env from the example (keep this file chmod 600)
cp app.env.example app.env
chmod 600 app.env
$EDITOR app.env   # fill in LOCK_PASSWORD / TOURNAMENT_PASSWORD_HASH

# 5. Start the stack
docker compose up -d

# 6. Verify — Caddy will obtain a Let's Encrypt cert automatically (may take
#    a few seconds on first boot).
curl -s https://tournament.example.com/health
```

## uid 65534 and volume ownership

The app image runs as **uid 65534** (`nonroot`).  The host-mounted
`./tournament-data` directory **must** be owned by that uid:

```bash
sudo chown -R 65534:65534 ./tournament-data
```

If you skip this step, the app cannot write to the data directory (a permission
error) and exits on first write. The app logs a clear diagnostic in this case —
check `docker compose logs app` if the container exits immediately.

## SSE and Caddy buffering

Caddy **streams** responses by default.  The live-score event stream (`/api/
events` SSE endpoint) requires streaming — do **not** add `flush_interval -1`
or any response-buffering directive to the Caddyfile.  The included Caddyfile
is correct as-is.

## Locking the admin password

Set `LOCK_PASSWORD=true` and `TOURNAMENT_PASSWORD_HASH=<bcrypt>` in `app.env`
for public deployments.  Generate the hash:

```bash
# Using htpasswd (apache2-utils / httpd-tools)
htpasswd -bnBC 12 "" 'MySecretPassword' | tr -d ':\n'

# Using the openssl fallback
openssl passwd -6 'MySecretPassword'   # SHA-512, also accepted
```

## Teardown

```bash
docker compose down
# To also remove the Caddy TLS data (certs):
docker compose down -v
```

Data in `./tournament-data` is a host bind-mount and is **not** removed by
`docker compose down` — back it up first if you want to keep tournament state.

## Updating the image

```bash
docker compose pull
docker compose up -d
```

Compose will replace only the containers whose image changed.
