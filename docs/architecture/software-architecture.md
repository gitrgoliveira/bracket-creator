# Software architecture

How the bracket-creator codebase is organised: a single Go binary that is both a **CLI**
(Excel bracket generator) and a **live tournament web app** (the `mobile-app` server), plus a
Preact frontend compiled into the binary.

> Related: [Network architecture](network-architecture.md) · [Infrastructure architecture](infrastructure-architecture.md) · [Connection resilience](../dev-guide/connection-resilience.md)

## 1. System context

```mermaid
flowchart TB
    operator["Organiser / Operator<br/>(tablet or desktop)"]
    viewer["Competitors & spectators<br/>(phones)"]
    cli["CLI user<br/>(terminal)"]

    subgraph bin["bracket-creator — single Go binary"]
        cliCmds["CLI commands<br/>create-pools · create-playoffs · print"]
        serveCmd["serve<br/>(one-shot Excel web form)"]
        mobile["mobile-app<br/>(live tournament server)"]
    end

    excel["Excel .xlsx<br/>(formula-linked brackets)"]
    pdfs["PDF exports<br/>(tags, names, trees)"]
    state[("tournament-data/<br/>Markdown + CSV on disk")]

    cli --> cliCmds --> excel
    operator -->|HTTPS| mobile
    viewer -->|HTTPS + SSE| mobile
    mobile --> state
    mobile --> pdfs
    cliCmds --> pdfs
```

The same binary ships the CLI, the legacy one-shot Excel web form (`serve`), and the
real-time tournament app (`mobile-app`). All web assets are embedded at build time, so there is
nothing to install beyond the binary itself.

## 2. Command surface (Cobra CLI)

```mermaid
flowchart LR
    root["bracket-creator (root, Cobra)"]
    root --> cp["create-pools"]
    root --> cpp["create-playoffs"]
    root --> srv["serve — Excel web form :8080"]
    root --> ma["mobile-app — live server :8080"]
    root --> hp["hash-password — bcrypt for locked mode"]
    root --> pr["print — PDF generation"]
    root --> mn["man / version"]
```

Each command is an options struct with a `run()` method (`cmd/*.go`); `create-pools` and
`create-playoffs` share `cmd/shared.go`. `main.go` embeds the web assets and calls
`cmd.ExecuteWithResources(res)`.

## 3. Package layers

```mermaid
flowchart TD
    subgraph entry["Entry"]
        main["main.go<br/>//go:embed web/* web-mobile/*"]
        cmd["cmd/ (Cobra commands)"]
    end
    subgraph app["Application / orchestration"]
        mobileapp["internal/mobileapp<br/>Gin handlers · SSE hub · auth · middleware"]
        engine["internal/engine<br/>drives generation from state"]
        service["internal/service"]
    end
    subgraph domainlayer["Domain & logic"]
        helper["internal/helper<br/>PRIMARY logic: CSV, pools, trees, seeding, Excel cells"]
        domain["internal/domain<br/>clean models: Player, Pool, Match, Seed, Decision, TeamLineup"]
    end
    subgraph io["I/O & persistence"]
        state["internal/state<br/>file-backed store · WAL · per-comp locks"]
        excel["internal/excel<br/>workbook from scratch"]
        pdf["internal/pdf"]
        resources["internal/resources<br/>embedded FS"]
    end

    main --> cmd --> mobileapp & engine & service
    main --> resources
    mobileapp --> engine --> helper
    mobileapp --> state
    engine --> state
    service --> helper
    helper --> domain
    helper --> excel
    cmd --> pdf
```

**Dual domain model (in transition).** `internal/helper` is where the real algorithms live —
its types carry Excel coordinates (`sheetName`, `cell`) tightly coupled to output generation.
`internal/domain` holds clean models being phased in gradually. Don't confuse the two.

| Package | Responsibility |
|---|---|
| `cmd` | Cobra commands; each an options struct with `run()` |
| `internal/helper` | CSV parsing, pool/match generation, binary-tree brackets, seeding, Excel rendering |
| `internal/domain` | Clean models + canonical decision/lineup rules |
| `internal/engine` | Thin adapter that drives `helper` generation from a `state.Competition`; scoring, eligibility, kachinuki, schedule estimate |
| `internal/state` | File-backed store (`tournament.md`, `competitions/<id>/config.md`, `participants.csv`); transactions + write-ahead log; per-competition locks |
| `internal/excel` | Excel lifecycle + `NewFileFromScratch` |
| `internal/pdf` | PDF exports (LibreOffice-backed) |
| `internal/mobileapp` | Gin HTTP handlers, SSE hub, auth middleware, `safeGo` |
| `internal/resources` | Embedded web-asset management |

## 4. The `mobile-app` server at runtime

```mermaid
flowchart TB
    subgraph proc["mobile-app process (Gin + http.Server)"]
        mw["middleware.go<br/>X-Tournament-Password auth · body caps"]
        authsrc["auth_source.go<br/>PasswordVerifier (file | locked/bcrypt)"]
        handlers["handlers_*.go<br/>competition · match · participants · tournament<br/>decision · eligibility · lineup · schedule · reset · auth-config"]
        hub["hub.go — SSE hub<br/>seq stamping · 100-event replay ring · resync · heartbeat"]
        safego["safego.go<br/>panic-safe goroutines"]
        engine["engine adapter"]
        store[("state.Store<br/>WithTransaction + WAL")]
        embed["resources<br/>embedded SPA (dist/, css, vendor)"]
    end

    client["Browser SPA (Preact)"] -->|REST api calls| mw --> handlers
    mw --> authsrc
    client -->|SSE stream| hub
    handlers --> engine --> store
    handlers -->|broadcast| hub --> client
    client -->|static assets| embed
```

Server hardening (constants in `cmd/mobile_app.go`): `ReadHeaderTimeout 10s`, `ReadTimeout 30s`,
`IdleTimeout 120s`, `MaxHeaderBytes 1 MB`, **`WriteTimeout 0`** (SSE streams are infinite —
per-request cancellation runs via the request context), graceful shutdown 30s with
`Hub.Close` wired through `RegisterOnShutdown`. Every handler-spawned goroutine uses `safeGo`
(Gin Recovery only catches the request goroutine).

## 5. Write path — recording a score (ACID + broadcast)

```mermaid
sequenceDiagram
    autonumber
    participant SPA as Browser SPA
    participant MW as auth middleware
    participant H as handlers_match.go
    participant E as engine
    participant S as state.Store (WAL)
    participant HUB as SSE hub
    participant OTHERS as other clients

    SPA->>MW: PUT /api/competitions/:id/matches/:mid/score
    MW->>MW: verify X-Tournament-Password
    MW->>H: authorized
    H->>E: RecordMatchResultWithIneligibilityTx(...)
    E->>S: WithTransaction → stage writes (intents)
    S->>S: WAL commit → atomic write (fsync + rename) → done
    S-->>E: persisted
    E-->>H: result (+ eligibility status)
    H->>HUB: Broadcast(match_updated, ...)  [after persist]
    HUB-->>OTHERS: SSE event (id: seq)
    H-->>SPA: 200 result
```

Core invariant (project constitution): **persist then broadcast** — a write is durable
(`fsync` + atomic rename, WAL for multi-file changes) before the 200 and before any SSE
fan-out. Scoring is ACID; a legitimate operator change is never dropped.

## 6. Bracket generation (engine → helper)

```mermaid
flowchart LR
    start["POST /api/competitions/:id/start"] --> eng["engine.StartCompetition"]
    eng --> mode{"format?"}
    mode -->|pools + playoffs| pools["helper: greedy pools<br/>(dojo-conflict avoidance)<br/>court-aware seeding"]
    mode -->|playoffs only| tree["helper/tree.go<br/>binary tree (max 16/tree)<br/>StandardSeeding"]
    pools --> store[("state.Store")]
    tree --> store
    store --> excelOpt["Excel / PDF export (on demand)"]
```

## 7. Frontend (Preact SPA)

```mermaid
flowchart TB
    subgraph build["Build (make go/build)"]
        jsx["web-mobile/js/*.jsx (Preact, JSX)"] -->|esbuild classic transform| dist["web-mobile/dist/*.js"]
        dist -->|go embed| binemb["embedded in binary"]
    end
    subgraph runtime["In the browser"]
        api["api_client.jsx<br/>HTTP client · SSE consumer · offline write queue (outbox)"]
        appc["app.jsx (viewer)"]
        adminc["admin.jsx (operator console)"]
        patch["patch.jsx<br/>seq-gap detection (checkSeqGap)"]
        editors["admin_scoring_* · viewer_match · lineup editors"]
        api --> appc & adminc & editors
        patch --> appc & adminc
    end
```

The operator console is a tablet/desktop surface; the viewer is mobile-first. The client's
**offline write queue, SSE resume, and reconnect resilience** are documented separately in
[Connection resilience](../dev-guide/connection-resilience.md) and depicted in
[Network architecture](network-architecture.md).

## Key design rules (see also [`CLAUDE.md`](https://github.com/gitrgoliveira/bracket-creator/blob/main/CLAUDE.md) and [`DESIGN.md`](https://github.com/gitrgoliveira/bracket-creator/blob/main/DESIGN.md))

- **Persist before broadcast**; scoring is ACID; never drop a legitimate operator change.
- **Use layout/sheet constants** (`internal/helper/constants.go`), never string literals.
- **`errcheck` is enforced** in production code — propagate or log, never `_ =`.
- **Aka (Red) / Shiro (White) are positional**, distinguished by treatment, not hue alone.
