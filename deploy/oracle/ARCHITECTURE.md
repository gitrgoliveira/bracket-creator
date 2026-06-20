# Oracle Cloud (OCI) Deployment Architecture

This document explains how the bracket-creator live tournament app is deployed on the
**Oracle Cloud Always Free tier**, what gets created, and what to expect. For step-by-step
deployment instructions, see [README.md](README.md).

> Cloud free-tier allowances change over time. The figures below were accurate as of June 2026 —
> confirm them against Oracle's current [Always Free](https://www.oracle.com/cloud/free/) page before
> you rely on them.

## What this deployment is for

The Always Free **Ampere A1** instance (2 OCPU / 12 GB RAM, with a 10 TB/month network allowance)
runs the tournament app **continuously, at no cost**, and comfortably handles **large events** —
including audiences of 1000+ concurrent live viewers. It is the recommended choice when you expect a
big crowd. For smaller club events you may also use the simpler GCP deployment (`deploy/gcp/`).

## Resources created

| Resource | Free-tier allowance (June 2026) | What this deployment uses |
|---|---|---|
| Compute | Ampere A1 (Arm), up to 2 OCPU / 12 GB RAM, Always Free | one instance: 2 OCPU / 12 GB |
| Block storage | 200 GB total, Always Free | boot volume holds the app and data |
| Network egress | 10 TB/month | effectively unlimited for this app |
| Networking | 1 virtual cloud network + public IP | one network, subnet, and a reserved public IP |

> Oracle's Always Free Ampere allowance is **2 OCPU / 12 GB** as of mid-June 2026 (reduced from an
> earlier 4 OCPU / 24 GB). The deployment requests 2 OCPU / 12 GB to stay within the free limit.

## Architecture note: Arm processors

Ampere A1 instances use **Arm (aarch64)** processors, so they run the Arm build of the application
image. The project publishes multi-architecture images (Arm and x86), so the instance simply pulls
the correct one — no special action is needed on your part.

## Topology

```
  Internet ──443/80──▶  Firewall: cloud Security List  +  on-host firewall (both required)
                              │
                   ┌──────────▼───────────┐  Ampere A1 (2 OCPU / 12 GB), Arm
                   │  Caddy  (port 443)   │  automatic HTTPS (Let's Encrypt)
                   │        │ proxy         │
                   │        ▼               │
                   │  tournament app :8080 │
                   └──────────┬───────────┘
                              │
                   /opt/tournament-data    (on the block volume; survives reboots)
```

The app runs in a container as a non-root user. Caddy sits in front of it, terminates HTTPS, and
forwards requests to the app. Live updates stream to browsers over Server-Sent Events (SSE), which
pass through Caddy unbuffered.

## Persistence

All tournament state is stored on the instance's block volume at `/opt/tournament-data`, mounted
into the container. This volume survives reboots and stop/start, so your tournament data is retained.
The container restarts automatically, so the app comes back on its own after a reboot.

## Networking and HTTPS

- **Automatic HTTPS** is handled by Caddy using Let's Encrypt — no manual certificate management.
- **Two firewall layers** must both allow web traffic, and the deployment configures both for you:
  1. The cloud-level **Security List** (opens ports 80, 443, and 22).
  2. The **on-host firewall** inside Oracle's Linux images, which blocks web ports by default — a
     common Oracle pitfall. The deployment opens and persists the necessary rules automatically.
- **DNS:** point an `A` record for your chosen hostname at the instance's public IP. Caddy needs the
  hostname to issue the certificate. The deployment uses a **reserved public IP** (included in Always
  Free), so the address stays stable across stop/start.

## Authentication

- By default the app uses the password stored in your tournament configuration.
- For public deployments you can enable **locked mode** (`LOCK_PASSWORD=true`) and supply a bcrypt
  password hash via `TOURNAMENT_PASSWORD_HASH`. These are written to a protected, root-owned file on
  the instance and are never stored in the container image. See the [README](README.md) for details.

## Capacity and scale

With 12 GB of RAM and a 10 TB monthly network allowance, this deployment serves large live audiences
comfortably; network egress is not a practical concern. `SSE_MAX_CLIENTS` (default 5000) bounds the
number of simultaneous live-update connections and can be adjusted in the configuration. For
exceptionally large events, validate capacity with a trial run at your expected audience size.

## Operations

- **Teardown:** `terraform destroy` removes everything. Running it also frees the Always Free
  Ampere capacity for future use.
- **Availability note:** Always Free Ampere capacity is occasionally unavailable in busy regions
  ("out of host capacity"). If that happens, retry or try a different availability domain — the
  README explains how.
- **Backups:** tournament data is small; you can back up the block volume or copy `tournament-data`
  elsewhere. The README includes commands.

## Provisioning summary

The module provisions the instance, network, firewall, and reserved IP, then automatically: installs
Docker, opens the on-host firewall, prepares the data directory, writes the app and Caddy
configuration, and starts the app by pulling the published image. Within a few minutes of
`terraform apply` the app is reachable over HTTPS at your hostname. Full instructions are in the
[README](README.md).
