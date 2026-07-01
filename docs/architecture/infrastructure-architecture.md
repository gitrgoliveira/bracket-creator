# Infrastructure architecture

How bracket-creator is built, packaged, deployed, and persisted. The whole product is a
**single self-contained Go binary** (web assets embedded) running behind a TLS proxy, with
tournament state on a plain disk; deployable from a laptop to a free-tier cloud VM.

> Related: [Software architecture](software-architecture.md) · [Network architecture](network-architecture.md)

## 1. Build & packaging pipeline

```mermaid
flowchart LR
    subgraph src["Source"]
        go["Go packages (cmd/, internal/)"]
        jsx["web-mobile/js/*.jsx (Preact)"]
        web["web/ (Excel form)"]
    end
    subgraph build["make go/build"]
        gen["go generate (glossary)"]
        esbuild["esbuild → web-mobile/dist/*.js"]
        embed["//go:embed web/* web-mobile/{index,css,dist,vendor}"]
        gobuild["go build → bin/bracket-creator"]
    end
    img["Container image<br/>ghcr.io/gitrgoliveira/bracket-creator-mobile-pdf:latest<br/>(+ LibreOffice for PDF)"]

    jsx --> esbuild --> embed
    web --> embed
    gen --> gobuild
    embed --> gobuild
    go --> gobuild
    gobuild --> img
```

- `dist/` and `vendor/` are build artifacts (gitignored except `keep` placeholders). esbuild
  regenerates `dist/` on every build, then `go:embed` bakes the served assets into the binary.
- The published image adds LibreOffice so the `print` PDF exports work in a container.
- **One artifact, no runtime asset directory**: the binary serves everything from its embedded FS.

## 2. Runtime composition (container + proxy + disk)

```mermaid
flowchart TB
    subgraph hostbox["Host (VM or any Docker host)"]
        subgraph caddyc["caddy:2-alpine"]
            caddy["Caddy<br/>:80 / :443 · auto-HTTPS"]
        end
        subgraph appc["app container (uid 65534, non-root)"]
            app["bracket-creator mobile-app :8080"]
        end
        vol[("./tournament-data → /tournament-data<br/>(host volume, owned by 65534)")]
        cvol[("caddy_data / caddy_config<br/>(certs)")]
    end
    inet["Internet :443/:80"] --> caddy
    caddy -->|reverse proxy| app
    app --> vol
    caddy --> cvol
```

- App runs as **non-root (uid 65534)**; the data volume must be owned by that uid or the app
  refuses to start. App port 8080 is `expose`d to the proxy only, never published to the host.
- `restart: unless-stopped` (compose) / auto-restart (cloud) brings the app back after reboots.

## 3. Deployment options

```mermaid
flowchart TD
    bin["bin/bracket-creator (single binary)"]
    bin --> bare["Bare: run directly<br/>PORT=8080 ./bracket-creator mobile-app<br/>(put any TLS proxy in front)"]
    bin --> compose["Docker Compose (deploy/docker/)<br/>app + Caddy, provider-agnostic"]
    bin --> gcp["GCP Always Free (deploy/gcp/)<br/>Terraform: e2-micro + Caddy"]
    bin --> oracle["Oracle Always Free (deploy/oracle/)<br/>Terraform: larger free tier (1000+ viewers)"]
```

| Target | What it is | Best for |
|---|---|---|
| **Bare binary** | run the binary, bring your own TLS proxy | local / dev / custom hosts |
| **Docker Compose** (`deploy/docker/`) | `app` + `caddy` services, host volume for data | self-managed VMs / on-prem |
| **GCP Always Free** (`deploy/gcp/`) | Terraform; `e2-micro` + firewall + persistent disk + Caddy auto-HTTPS | club / regional events (~≤50–300 viewers) |
| **Oracle Always Free** (`deploy/oracle/`) | Terraform; larger free allowance | large events (1000+ concurrent viewers) |

### Cloud topology (GCP Always-Free example)

```mermaid
flowchart TB
    dns["DNS A record → instance IP"] --> fw
    subgraph gcpproj["GCP project (free regions only: us-west1/central1/east1)"]
        fw["Firewall: allow 80, 443, 22"]
        subgraph vm["e2-micro (shared vCPU, 1 GB RAM, 24/7 free)"]
            caddy["Caddy :443 (Let's Encrypt)"]
            app["app container :8080"]
        end
        pd[("30 GB boot disk (free-tier cap)<br/>OS + Docker + image + /opt/tournament-data")]
    end
    fw --> caddy --> app --> pd
```

Terraform provisions the instance, network, and firewall, then installs Docker, prepares the
data dir, writes the app + Caddy config, and starts the app. Reachable over HTTPS within minutes
of `terraform apply`. `terraform destroy` removes everything (run it after the event).

### Venue connectivity: a four-court event

The cloud and host setup is only half the picture. On the venue floor, every operator console,
display screen, and spectator phone is a **browser** reaching that one app over the network.
A typical four-court (shiaijo A–D) layout:

```mermaid
flowchart TB
    subgraph floor["Venue floor (4 courts)"]
        subgraph cA["Court A"]
            opA["Operator console<br/>tablet / laptop (admin)"]
            dA["Display screen<br/>scoreboard / bracket"]
        end
        subgraph cB["Court B"]
            opB["Operator console"]
            dB["Display screen"]
        end
        subgraph cC["Court C"]
            opC["Operator console"]
            dC["Display screen"]
        end
        subgraph cD["Court D"]
            opD["Operator console"]
            dD["Display screen"]
        end
        spec["Spectator phones<br/>public viewer"]
    end

    net["Venue network<br/>router + Wi-Fi AP(s)<br/>wire operators where possible,<br/>dedicated AP for operators"]

    opA & opB & opC & opD --> net
    dA & dB & dC & dD --> net
    spec --> net

    net --> where{"Where does the app run?"}
    where -->|cloud| up["Internet uplink"] --> cloud["Caddy + app (cloud VM)"]
    where -->|on-prem| local["Local host on the LAN<br/>no internet needed · runs mobile-app"]
```

| Device | What it runs | Notes |
|---|---|---|
| Operator console (1 per court) | admin scoring SPA | tablet/desktop surface; authenticates with the tournament password; scores its own shiaijo |
| Display screen (1 per court, optional) | public display / scoreboard view | a browser at a display URL; read-only, no auth. **Preferred: drive it from the operator console's own machine** via an HDMI cable to a TV or monitor, so the board survives a Wi-Fi outage (see [Keep the court scoreboard alive on the same machine](#keep-the-court-scoreboard-alive-on-the-same-machine-hdmi) below). A standalone smart-TV browser or separate mini-PC also works but loses that offline path |
| Spectator phones | public viewer (mobile-first) | can be on cellular; they don't need venue Wi-Fi when the app is cloud-hosted |

**Per-client load.** Every console, display, and phone holds **one SSE stream** plus its REST
calls. A four-court event is roughly 4 operators + 4 displays + N spectators of concurrent SSE
clients, comfortably within `SSE_MAX_CLIENTS`, but every live update fans out to all of them
(see [Capacity & scaling](#5-capacity-scaling)).

**Two venue patterns:**

- **Cloud-hosted** (the cloud-hosted topology): venue devices reach the cloud app over the venue's internet
  uplink; spectators can use cellular and skip venue Wi-Fi entirely. Needs a working uplink for
  the operators and displays.
- **On-prem / local**: run the single `mobile-app` binary on a laptop or mini-PC **on the venue
  LAN**. Operators and displays hit it locally, so **scoring keeps working with no internet at
  all**. Put a local TLS proxy in front for secure-context features, or serve plain HTTP on the LAN.

**The network is the real fix.** Client resilience (offline write queue, SSE resync, silence
watchdog) keeps the app usable across blips. For a smooth event, **wire the operator consoles**
where you can, put operators on a **dedicated AP** separate from spectator guest Wi-Fi, prefer
the **on-prem** pattern when the venue's internet is unreliable, and **drive each court's display
from the operator's own machine over HDMI** so the scoreboard keeps moving even when the network
does not (next section).

### Keep the court scoreboard alive on the same machine (HDMI)

Each court's display screen can be rendered two ways, and the choice decides whether the
scoreboard freezes during a Wi-Fi outage:

- **Same machine as the operator console (recommended).** Connect a TV or monitor to the
  operator's laptop or mini-PC with an **HDMI cable**, extend the desktop, and open the court's
  display URL in a second browser window on that same machine. The operator console and the
  display board are then two tabs in the same browser on the same computer, so they share a
  private same-origin channel: every score the operator records reaches the board **directly,
  on the machine, with no network hop**. If the venue Wi-Fi drops mid-match, that court's
  scoreboard keeps updating from the operator's entries for as long as the scoring tab stays
  open. The board shows a small amber dot while it is running on this local feed (see
  [the scoreboard status dot](../user-guide/mobile-app.md#scoreboards-and-court-displays)).
- **Separate device (a smart-TV browser, or the display on its own mini-PC).** Simpler cabling,
  but the board only ever updates over the network, so a Wi-Fi outage freezes it until the link
  returns (the board then shows a red dot).

```mermaid
flowchart LR
    subgraph machine["One court machine (operator's laptop / mini-PC)"]
        op["Operator console tab<br/>(admin scoring)"]
        disp["Display board tab<br/>(scoreboard / bracket)"]
        op -. same-origin channel<br/>(no network) .-> disp
    end
    disp == HDMI cable ==> tv["Court TV / monitor"]
    op -->|writes, queued + synced<br/>when the link returns| net["Venue network / app server"]
```

This local hub needs no internet, no secure context, and no extra software, and it works in
every topology (cloud-hosted, on-prem, or bare-IP HTTP). It **complements** the network fixes
above rather than replacing them: the operator's writes are still queued locally and synced to
the server once the link returns, so the authoritative record stays correct. It is per machine
and per court. Reloading the **display** tab during an outage is fine: it cold-starts from the
operator tab's snapshot over the same channel, as long as an operator tab is still open on that
machine to answer (it only stays blank if none is). The genuine gap is reloading the **operator**
tab itself mid-outage, since it holds the court's working data while offline and would have
nothing to fetch from the down server.

## 4. Persistence model

```mermaid
flowchart LR
    app["mobile-app"] -->|durable write| files
    subgraph files["tournament-data/ (plain files on a persistent disk)"]
        t["tournament.md (YAML front-matter)"]
        c["competitions/&lt;id&gt;/config.md"]
        p["competitions/&lt;id&gt;/participants.csv · seeds.csv"]
        wal["WAL (crash recovery, replayed on startup)"]
    end
```

- **No database.** State is human-readable Markdown + CSV on disk. Multi-file changes are made
  durable through a write-ahead log replayed on startup. The disk survives reboots and stop/start.
- Backups are trivial: snapshot the disk or copy `tournament-data/` elsewhere.
- **Disk sizing is not about data volume.** Tournament state is tiny (KB–MB). The cloud disks
  (30 GB on GCP, 50 GB on Oracle) are the free-tier **boot-disk** allowances (they hold the OS,
  Docker, and the app image, with `tournament-data/` alongside). The module uses the free
  cap rather than provisioning a separate data disk.

## 5. Capacity & scaling

Live updates fan out to every viewer, so **egress is the limit**, not CPU/RAM.

```mermaid
flowchart LR
    a["≤ ~50 viewers"] --> g1["GCP free tier: comfortable"]
    b["~100–300 viewers"] --> g2["GCP free tier: watch egress (1 GB/mo)"]
    c["1000+ viewers"] --> o["Oracle deployment"]
```

Set a **billing budget alert** (for example, $1) on cloud deployments so you're warned if usage ever
exceeds the free allowance. `SSE_MAX_CLIENTS` bounds fan-out cost (default 5000; ~4–10 KB
resident per client).

## 6. Configuration (environment variables)

```mermaid
flowchart TB
    env["env / flags"] --> app["mobile-app startup"]
```

| Variable | Flag | Default | Purpose |
|---|---|---|---|
| `TOURNAMENT_DATA_DIR` | `-f/--folder` | `./tournament-data` | where state is stored |
| `PORT` | `-p/--port` | 8080 | listen port |
| `BIND_ADDRESS` | `-b/--bind` | localhost | listen address |
| `LOCK_PASSWORD` | `--lock-password` | false | enable locked (bcrypt) auth; disables reset endpoint |
| `TOURNAMENT_PASSWORD_HASH` | (none) | (none) | bcrypt hash for locked mode (root-owned, never in the image) |
| `SSE_MAX_CLIENTS` | (none) | 5000 | SSE subscriber cap |
| `ENABLE_TOURNAMENT_SCHEDULE` | (none) | off | feature flag for the schedule UI |

Generate the bcrypt hash with `bracket-creator hash-password`. In cloud deployments the secrets
are written to a protected, root-owned file on the instance, never baked into the container image.

## 7. Operational properties

- **Stateless app, stateful disk**: the container can be recreated freely. Only the data volume
  matters. Auto-restart + a persistent disk = self-healing after reboots.
- **Zero-dependency runtime**: no DB, no cache, no message broker. Only the binary, a TLS proxy,
  and a disk.
- **Graceful shutdown** (30s): lets in-flight writes finish and SSE goroutines exit cleanly before
  a container restart.
- **Teardown is one command** (`terraform destroy`), so no stray paid resources linger.
