# GCP Deployment Architecture

This document explains how the bracket-creator live tournament app is deployed on the
**Google Cloud Always Free tier**, what gets created, and what to expect. For step-by-step
deployment instructions, see [README.md](README.md).

> Cloud free-tier allowances change over time. The figures below were accurate as of June 2026 ,
> confirm them against Google's current [Free Tier](https://cloud.google.com/free) page before you
> rely on them.

## What this deployment is for

The Always Free `e2-micro` instance runs the tournament app **continuously, at no cost**, and is a
great fit for **club and regional events**. Its monthly free network allowance (1 GB egress)
limits how many simultaneous live viewers it can serve, so for very large events (1000+ concurrent
viewers) we recommend the Oracle Always-Free deployment (`deploy/oracle/`) instead.

## Resources created

| Resource | Free-tier allowance | What this deployment uses |
|---|---|---|
| Compute | 1× `e2-micro` (shared vCPU, 1 GB RAM), Always Free | one instance, running 24/7 |
| Region | `us-west1`, `us-central1`, or `us-east1` only | one of these (enforced by the module) |
| Disk | 30 GB standard persistent disk | a single boot + data disk |
| Network egress | 1 GB/month (from North America) | your live-viewer ceiling: see [Capacity](#capacity-and-scale) |
| External IP | ephemeral included free | ephemeral by default; a stable static IP is optional |

> The deployment **only runs in the three free regions above.** Choosing another region would incur
> charges, so the module rejects any other value.

## Topology

```
  Internet ──443/80──▶  Firewall (allow 80, 443, 22)
                              │
                   ┌──────────▼───────────┐   e2-micro
                   │  Caddy  (port 443)   │   automatic HTTPS (Let's Encrypt)
                   │        │ proxy         │
                   │        ▼               │
                   │  tournament app :8080 │
                   └──────────┬───────────┘
                              │
                   /opt/tournament-data    (on the persistent disk; survives reboots)
```

The app runs in a container as a non-root user. Caddy sits in front of it, terminates HTTPS, and
forwards requests to the app. Live updates stream to browsers over Server-Sent Events (SSE), which
pass through Caddy unbuffered.

## Persistence

All tournament state is stored on the instance's persistent disk at `/opt/tournament-data`, mounted
into the container. This disk survives instance reboots and stop/start, so your tournament data is
retained. The container is configured to restart automatically, so the app comes back on its own
after a reboot.

## Networking and HTTPS

- **Automatic HTTPS** is handled by Caddy using Let's Encrypt: no manual certificate management.
- The firewall allows inbound HTTP (80), HTTPS (443), and SSH (22). For SSH you can restrict access
  to your own IP range.
- **DNS:** point an `A` record for your chosen hostname at the instance's public IP. Caddy needs the
  hostname to issue the certificate. If you use the default ephemeral IP, note that it can change if
  the instance is stopped and started: reserve a static IP if you need a stable address.

## Authentication

- By default the app uses the password stored in your tournament configuration.
- For public deployments you can enable **locked mode** (`LOCK_PASSWORD=true`) and supply a bcrypt
  password hash via `TOURNAMENT_PASSWORD_HASH`. These are written to a protected, root-owned file on
  the instance and are never stored in the container image. See the [README](README.md) for details.

## Capacity and scale

Live updates are delivered to every connected viewer, so **network egress is the practical limit**
on this tier, not CPU or memory. With 1 GB of free egress per month:

| Audience size | Suitability on GCP Always Free |
|---|---|
| Up to ~50 concurrent viewers | Comfortable |
| ~100–300 concurrent viewers | Possible, but watch your egress usage |
| 1000+ concurrent viewers | Use the Oracle deployment (`deploy/oracle/`) instead |

We recommend setting a **billing budget alert** (e.g. $1) so you're notified immediately if usage
ever exceeds the free allowance. The README covers how.

## Operations

- **Teardown:** `terraform destroy` removes everything. Run it when the event is over so no stray
  resources (such as a reserved static IP) remain.
- **Backups:** tournament data is small; you can snapshot the disk or copy `tournament-data`
  elsewhere. The README includes commands.
- **Monitoring:** keep an eye on the egress meter and your budget alert during busy events.

## Provisioning summary

The module provisions the instance, network, and firewall, then automatically: installs Docker,
prepares the data directory, writes the app and Caddy configuration, and starts the app. Within a
few minutes of `terraform apply` the app is reachable over HTTPS at your hostname. Full instructions
are in the [README](README.md).
