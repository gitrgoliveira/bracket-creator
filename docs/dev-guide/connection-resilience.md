# Connection resilience: strategies & why we chose ours

> Design note backing the **mobile-app flaky-wifi hardening** work (bead `mp-gpra`).
> Captures the landscape of offline-resilient real-time techniques, the alternatives we
> surveyed, and why the hand-rolled approach in `web-mobile/` + `internal/mobileapp/hub.go`
> is the right fit for this project's constraints. Researched 2026-06.

## The problem

Kendo tournaments run on saturated / half-open venue Wi-Fi. Two things must survive a flaky
link without losing a legitimate operator change (a constitution requirement) or showing
silently-stale data:

1. **Durable client writes** — a score/decision/lineup submitted during a blip must not be lost.
2. **Real-time resume** — when the SSE stream drops and reconnects, the client must not miss
   events or go silently stale.

## What bracket-creator does (and why it's the textbook pattern)

The two halves map directly onto the two canonical solutions:

| Concern | Pattern | Where in the code |
|---|---|---|
| Durable writes | **Outbox pattern** — a persisted local queue of mutations drained by a background worker, retried with backoff | `web-mobile/js/api_client.jsx` (`_writeQueue`, persisted to `localStorage`) |
| Real-time resume | **SSE `Last-Event-ID` replay** — server keeps a ring of recent events and replays from the client's last id; an explicit `resync_required` covers gaps the ring can't | `internal/mobileapp/hub.go` + `web-mobile/js/patch.jsx` (`checkSeqGap`) |

The classic failure these guard against is well documented: *"the browser sends `Last-Event-ID`
automatically on reconnect, but if the server ignores it and resubscribes fresh, events emitted
during the gap are lost."* Our `applyPatchOrdered` + `resync_required` handling closes that.

### Idempotency: by design, not by key

The outbox pattern normally pairs the queue with **idempotency keys** so a retry can't
double-apply. We instead rely on **idempotency-by-design**, which is verified, not assumed:

- completed-score advancement is a deterministic slot SET (proven in `internal/engine/scoring_idempotency_test.go`);
- decisions are 409-lock-guarded server-side.

An idempotency key (the Stripe model) is the canonical alternative and would be the move *if* we
ever wanted silent background auto-retry of terminal writes. Today terminal writes surface an
explicit pending / failure state to the operator instead.

## Alternatives surveyed (and why not, for this project)

Constraints that rule most of these out: **single Go binary, file-backed state (Markdown/CSV),
no extra infrastructure or paid services, YAGNI.**

| Option | What it is | Verdict here |
|---|---|---|
| **TanStack Query** persisted offline mutations | Library: persisted mutation queue, retry-in-order, resume | Closest off-the-shelf to our queue. Notably hits the **same limitation we did** — *"functions can't be serialized; register a default mutation fn to resume"* (our "`pendingFnRef` can't be restored from the serialized queue"). React-first (Preact needs `preact/compat`); low ROI to swap in. |
| **Mercure** (`github.com/dunglas/mercure`) | Production-grade **Go** SSE hub implementing exactly our `Last-Event-ID` protocol | Could replace `hub.go`, but adds an AGPL component/service to a deliberately single-binary app. Rejected. |
| **Service Worker Background Sync API** | Browser OS replays queued POSTs even after the tab closes | More robust than our `setInterval`/`online` flush, but **Chromium-only** (no Safari/Firefox as of 2026) and we vetoed service workers (secure-context dependency + no separate artifact). |
| **Hosted realtime** (Ably / Pusher / Socket.IO connection-state-recovery) | Managed "retain ~2 min history, replay missed on reconnect" | Same idea as our event ring, but external paid dependency — violates the no-infra constraint. |
| **Local-first sync engines** (Replicache / ElectricSQL / PowerSync) | Bidirectional sync engines over Postgres/SQLite | Require a database + sync service incompatible with our file-backed store. Over-engineering vs YAGNI. |
| **CRDTs** (Yjs / Automerge) | Conflict-free replicated data types for collaborative editing | For *collaborative editing*, which we don't have. Industry guidance: *"only add a CRDT if you have a real collaborative-editing requirement; most offline-first apps just need queued writes that sync."* That's us. |

## Verdict

Keep the hand-rolled **outbox + SSE-resume**; it matches the industry-standard pattern and fits
the single-binary / file-backed / no-infra constraints. The one pattern worth holding in reserve
is the **idempotency key**, should we ever want silent background auto-retry of terminal writes
instead of today's operator-visible pending/failure state.

## Sources

- AWS — [Transactional outbox pattern](https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html)
- [Offline-first: outbox, idempotency & conflict resolution](https://www.educba.com/offline-first/)
- TanStack Query — [Mutations (offline / persisted)](https://tanstack.com/query/latest/docs/framework/react/guides/mutations)
- [Server-Sent Events: the complete guide](https://codelit.io/blog/sse-server-sent-events-guide) · [WebSocket.org — SSE vs WebSocket](https://websocket.org/comparisons/sse/)
- Mercure — [spec](https://mercure.rocks/spec) · [Go hub](https://pkg.go.dev/github.com/dunglas/mercure)
- [Ably — connection state recovery](https://faqs.ably.com/connection-state-recovery) · [Socket.IO — connection state recovery](https://socket.io/docs/v4/connection-state-recovery)
- [ElectricSQL vs PowerSync vs Replicache](https://queryplane.com/blog/electricsql-vs-powersync-vs-replicache/) · [Why a team moved away from CRDTs for sync](https://powersync.com/blog/why-cinapse-moved-away-from-crdts-for-sync)
- MDN — [Offline and background operation (Background Sync)](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Offline_and_background_operation)
