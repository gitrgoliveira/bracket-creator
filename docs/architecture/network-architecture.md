# Network architecture

How traffic flows between browsers and the tournament app: HTTPS termination at a reverse
proxy, plain HTTP to the app, real-time updates over Server-Sent Events (SSE), and the
client-side resilience that keeps it working on flaky venue Wi-Fi.

> Related: [Software architecture](software-architecture.md) · [Infrastructure architecture](infrastructure-architecture.md)

## 1. Edge topology

The app speaks **plain HTTP**. A TLS-terminating reverse proxy (**Caddy**, automatic
Let's Encrypt) sits in front and streams SSE through unbuffered.

```mermaid
flowchart TB
    subgraph net["Venue / Internet"]
        op["Operator tablets"]
        vw["Viewer phones"]
    end

    subgraph host["Host (VM / container host)"]
        caddy["Caddy<br/>:80 redirect · :443 HTTPS<br/>auto TLS · SSE unbuffered"]
        app["bracket-creator mobile-app<br/>:8080 HTTP, internal only"]
        disk[("tournament-data/<br/>persistent disk")]
    end

    op -->|HTTPS 443 + SSE| caddy
    vw -->|HTTPS 443 + SSE| caddy
    caddy -->|HTTP reverse_proxy| app
    app --> disk
```

| Port | Where | Purpose |
|---|---|---|
| 443 | Caddy (public) | HTTPS for REST + SSE |
| 80 | Caddy (public) | ACME challenge; redirect to 443 |
| 8080 | app (internal) | plain HTTP; **never published directly** (Caddy proxies it) |
| 22 | host (optional) | SSH (restrict to your IP) |

> **Proxy must stream, not buffer.** SSE is a long-lived response. The Caddyfiles deliberately avoid `flush_interval` and response-buffering directives, because those would break the real-time event stream.
> (In production HTTPS comes from the proxy, so browser secure-context features work even though
> the app itself serves plain HTTP.)

## 2. Protocols on the wire

Two channels share the one HTTPS origin:

```mermaid
flowchart TB
    subgraph client["Browser SPA"]
        rest["REST calls<br/>fetch() JSON"]
        sse["1 long-lived SSE stream<br/>EventSource('/api/events')"]
    end
    subgraph server["mobile-app"]
        api["/api/* handlers<br/>(request/response)"]
        hub["SSE hub<br/>(fan-out)"]
    end
    rest -->|REST calls + auth header| api
    api -.->|after persist| hub
    hub ==>|SSE id + data frames| sse
```

- **REST**: score, decision, and lineup writes, config, and participants. Every scoring tap (a
  point, a foul, an overtime period, an engi flag, a team bout's tie or fusensho, a fighter named
  for a bout, a representative pick, the representative bout's judges' decision) is an event sent
  to `POST /api/competitions/{id}/matches/{mid}/score-events`, in a batch of at most 64 from one
  device session. The server appends each event to the match's event log, replays the log in the
  order the taps were made, and answers 200 with one outcome per event. The handler refuses the
  batch as a whole with 401 (a wrong password, checked before the body is read), 400 (a malformed
  batch) or 404 (an unknown competition or match).
  The answer also lists the `gaps` (the sequence numbers of that session's taps the server never
  received) and carries the match as stored, whose `eventSeqs` (per device session, the highest
  tap number the log holds) and `turnedAway` (each device tap the log holds that the server does
  not apply now, with its outcome) every later copy of the match carries too. Finishes,
  decisions, starts, reopens, sends back to the queue and corrections are whole writes, such as
  `PUT /api/competitions/{id}/matches/{mid}/score`; they also enter the match's log, and every
  device stamps them with the time they were made. Only the rule-bound writes (the taps, the
  whole writes of a match, lineups, team members, reinstatements and pool rank orders) and
  check-in, court calls, the tournament settings and the fighting-spirit awards go through the
  device's outbox (§4); other setup edits, such as the password change, competition settings,
  the roster, seeds, the draw and import, are direct calls that are not kept for later. Auth is
  handled through the `X-Tournament-Password` header (two modes, §5; self-run mode allows
  unauthenticated scoring). Every response carries the `X-App-Build` header naming the git
  commit of the server build (or `unknown`), allowing clients to detect when a new server build
  is running.
- **SSE**: one stream per client carrying `match_updated`, `competition_started`,
  `competition_completed`, `competitor_status_updated`, `participants_updated`,
  `tournament_updated`, `schedule_updated`, `lineup_updated`, `draw_generated`, `draw_discarded`,
  `swiss_round_generated`, `announcement`, `password_reset` and `bracket_quarantined`, plus the
  resilience control events `resync_required` and `heartbeat`. Every real event is stamped with a
  monotonic `seq` written as the SSE `id:` line, so the browser's `Last-Event-ID` advances
  automatically. A tap does not push by itself: the server replays the match and pushes the
  resulting `match_updated`. For a running match these pushes are held to at most four a second,
  with a final push carrying the newest state; any other push goes at once.

## 3. Real-time delivery & reconnect (SSE hub)

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser
    participant C as Caddy
    participant H as SSE hub

    B->>C: GET /api/events
    C->>H: proxied
    H-->>B: ": open" comment
    H-->>B: hello heartbeat
    H-->>B: id: N, data: event
    B--xH: connection drops
    B->>C: ?lastEventId=N
    C->>H: proxied
    H-->>B: ": open" + hello
    alt gap in the ring
        H-->>B: replay N+1 to head
    else ring rolled past N
        H-->>B: resync_required
    end
    H-->>B: heartbeat every 15 s
```

The steps, by their numbers in the diagram:

1. The browser opens the stream with `EventSource`.
2. Caddy passes it through unbuffered.
3. The hub writes an `: open` comment at once.
4. Then the hello frame (described below), with no `id:` line.
5. Each real event goes out as it happens, stamped with `seq` N as its `id:` line; the hub
   keeps the last 200 in a ring.
6. A Wi-Fi blip drops the connection.
7. The browser reconnects with a new `EventSource`, sending the last id it saw as
   `?lastEventId=N`.
8. Caddy passes it through.
9. Every connection, a reconnect included, opens with the comment and the hello.
10. When the ring still holds every event after N, the hub replays them.
11. When it does not (the ring rolled past N, or the server restarted and `seq` reset), the
    hub sends `resync_required` and the browser refetches everything.
12. A heartbeat goes out every 15 s.

- **Hello**: every connection, a first one or a reconnect, opens with an `: open` comment and
  then a hello frame, `{"type":"heartbeat","hello":true,"nowMs":<unix ms>,"build":"<commit>"}`,
  before any replay or `resync_required`. It carries no `id:` line and uses no `seq`, so a
  client takes it as liveness only; `build` lets a tab running an older bundle see that the
  server changed.

- **Replay ring**: `DefaultHistorySize` (200) recent events. The last id the client saw replays
  the gap. The client reconnects by opening a new `EventSource`, which sends no `Last-Event-ID`
  header, so it sends that id as the `lastEventId` query parameter (the header wins if both
  arrive).
- **`resync_required`**: emitted when a gap-free replay is impossible (ring eviction or a
  server restart that reset `seq`). The client resets its `lastSeq` and full-refetches. Emitted
  **without** an `id:` line when head seq is 0 so it can't force `Last-Event-ID` to "0".
- **Observable heartbeat**: a real `{"type":"heartbeat","nowMs":<unix ms>}` frame (no `id:`)
  every 15s, so the client can tell "quiet" from "dead". `nowMs` is the server clock at send time. It is a tripwire for a client whose own clock has drifted, not a source to set the clock from. A one-way push carries no round trip to correct against, so a delayed frame would drag the client's clock backwards. A client that sees a large divergence relearns the
  offset from the time endpoint instead.
- **Per-client buffered channel**; a stalled client that can't drain is dropped (non-blocking
  send). Subscriber cap `SSE_MAX_CLIENTS` (default 5000).

## 4. Client resilience on flaky Wi-Fi

The client treats the link as unreliable by default. The following flows show how.

Two patterns carry scoring. On the server each match is **event sourced**: the match's score
log holds the taps and the whole writes made to it, and the match shown to everyone is rebuilt by
replaying that log in the order the changes were made. On the device a **transactional outbox**
holds every tap and every write: it is stored in the browser before anything is sent, so a score
is never only in the memory of a page. The outbox sends a match's entries in the order they were
made, until the server confirms each, and the confirmation is idempotent, because a tap carries
its own id and a repeated id is recognised and logged once.

There is deliberately **no message broker** between them. The queue that matters is on the
tablet, because the tablet is the part that goes offline; a broker in the venue or the cloud
would hold nothing the tablet does not already hold. The order that matters is when each tap was
made, which the replay restores from the tap's own stamp, so a queue that preserved arrival
order would add nothing. And the app is one binary that keeps its data in files, so a broker
would be a second system to install, secure and keep running at a tournament for no gain.

Only a confirmed write ever empties the queue. A write that cannot be confirmed is never
dropped in silence: it is either kept and retried, parked until the operator can fix the
cause, or, when the server refuses it outright, removed with a visible notice naming the
reason, or discarded by the operator from the match's score editor after a confirm, when
the server keeps refusing it. A write is never removed for its age: however long it waited,
it is sent, and the server orders it by when it was made. Entries of one match are sent
strictly in the order they were made, so one that does not land holds back that match's later
entries and no other match's.

The cycle for one match's taps:

```mermaid
sequenceDiagram
    autonumber
    participant Op as Operator
    participant Ob as Outbox
    participant S as Server

    Op->>Ob: tap
    Ob-->>Op: shown pending
    Ob->>S: POST score-events
    alt offline, 5xx, 429, 403
        S--xOb: no answer or error
    else 401
        S-->>Ob: invalid password
    else other 4xx
        S-->>Ob: batch refused
    else 200
        S-->>Ob: outcomes, gaps, match
        opt gaps
            Ob->>S: missing taps again
        end
        S-->>Ob: SSE match_updated
    end
    Ob-->>Op: turned-away tap told
```

The steps, by their numbers in the diagram:

1. The operator taps. The tap carries its own id, the device session, a sequence number in
   that session and the time it was made. The outbox stores it in `localStorage`, shared by
   every tab, before anything is sent.
2. The board shows the tap at once, as pending.
3. The outbox sends a match's taps strictly in the order they were made, consecutive taps of
   one device session in one batch of at most 64.
4. Offline, or a 5xx, 429 or 403: the taps are kept and sent again later. That is safe,
   because a repeated id is logged once.
5. 401: the batch is parked until the operator signs in again.
6. Any other 4xx: the batch is parked, never dropped, and holds back that match's later
   entries until the operator discards it.
7. 200: the server appends each new tap to the match's log, replays the log by stamp, and
   answers with one outcome per tap, the `gaps` and the match as stored. Each tap settles on
   its own outcome:
    - **applied, recorded or duplicate**: leaves the queue quietly, and the outbox notes in
      browser storage that it landed.
    - **clock_skew** (the device's clock is too far ahead; the server logged nothing): the
      tap is re-stamped under the same id. The first re-stamp is sent again at once; a tap
      refused again after a re-stamp stays queued and held, with a notice, and is never
      dropped.
    - **any other outcome** (`matchClosed`, `noTarget`, `beforeBaseline`, a self-run limit):
      leaves the queue, and the operator is told in the editor and in one alert per pass.
    - **no outcome at all**: whether it was applied is unknown, so the tap is kept with its
      id and sequence number and sent again exactly as it went.
8. `gaps` names the taps of this session the server never received; the outbox sends the
   ones it still holds again at once.
9. The tap's own answer is not what other screens see: the server broadcasts
   `match_updated` on the SSE stream (§3), to this device and every other. For a running
   match the server's broadcast coalescer holds these pushes to one per 250 ms per match, the last one carrying the
   newest state.
10. That copy, like every later copy of the match (a push or a fetch), lists `turnedAway`. A
    tap the device saw land and later sees turned away is told to its operator once.

The flush of the whole queue, taps and whole writes alike:

```mermaid
flowchart TB
    submit["Tap, Finish,<br/>decision or lineup"] --> q["Stored in<br/>the outbox first"]
    online["browser 'online' event,<br/>page load, retry timer"] --> flush
    q --> flush["Flush pass"]
    flush -->|offline| q
    flush -->|2xx| ans{"Answer"}
    ans -->|"applied,<br/>recorded"| done["Leaves<br/>the queue"]
    ans -->|"applied:<br/>false"| notapplied["Leaves,<br/>'Not applied'"]
    ans -->|"other tap<br/>outcome"| told["Leaves,<br/>operator told"]
    ans -->|clock_skew| restamp["Re-stamped,<br/>same id"]
    restamp -->|first time| flush
    restamp -->|again| clockheld["Held,<br/>notice"]
```

When the server refuses the request itself:

```mermaid
flowchart TB
    flush["Flush pass"] -->|refused| status{"Status"}
    status -->|"5xx, 429,<br/>403"| retrying["Kept,<br/>retried"]
    status -->|401| parked["Parked,<br/>sign in"]
    status -->|"404 on a<br/>removal"| gone["Done,<br/>already gone"]
    status -->|"other<br/>4xx"| other{"Taps or<br/>whole write?"}
    retrying -->|"operator<br/>discards"| discarded["Discarded"]
    parked -->|signs in| again["Sent again"]
    other -->|taps| parkedtaps["Parked,<br/>never dropped"]
    other -->|"whole<br/>write"| refused["Discarded,<br/>notice"]
```

- **Stored first**: every tap or write is stored in the outbox before it is sent, and the
  board shows it at once.
- **Pass**: each owner's entries go strictly in the order made, taps in batches of up to 64;
  each request times out after 12 s, and a failed pass backs off to at most 8 s. A `gaps`
  list in a tap answer has the missing taps sent again at once.
- **2xx**: a whole write that applied, or was already recorded, leaves the queue (a write
  that had been held is answered with a notice). One the server answered `applied: false`
  (superseded by a newer change) leaves the queue too, with a "Not applied" notice, never a
  "saved". A tap's outcomes and `gaps` are settled as in the list above.
- **clock_skew**: a whole write refused for the device's clock (`applied: false`,
  `reason: "clock_skew"`) is handled exactly as a tap is: re-stamped under the same id and
  sent again at once, and refused again, held with a notice and never dropped.
- **5xx, 429 and 403**: kept and retried, holding back only that owner's later entries.
  After 10 consecutive refusals the header count reads "Not saving" and the match's editor
  offers **Discard held result**, which removes that write alone after a confirm.
- **401**: parked, no more retries, "Sign in to save"; signing in sends it again.
- **404 on a removal** (a match lineup's removal, a court call's withdrawal): the thing it
  removes is already gone, which is what the write asked for, so it counts as done.
- **Other 4xx**: a batch of taps is parked, never dropped, and holds back that match's later
  entries until the operator discards it; a whole write (400, 413, a generic 409) can never
  succeed, so it is discarded with a notice naming the match and the reason.

Not every write is an outbox entry. The rule-bound writes, the ones the server orders and judges
against what the match or the team already holds, are, and so are check-in, court calls, the
tournament settings and the fighting-spirit awards; other setup edits are direct calls:

```mermaid
flowchart TB
    rule["Rule-bound writes<br/>(listed below)"] --> ob["Outbox<br/>answered pending at once"]
    other["Check-in · court calls<br/>tournament settings<br/>fighting-spirit awards"] --> ob
    ob --> order["Sent per owner<br/>strictly in the order made"]
    order --> srv["Server"]
    setup["Setup edits<br/>(listed below)"] --> direct["Direct REST call<br/>not kept for later"]
    direct --> srv
```

- **Rule-bound writes** (outbox): taps; score writes (start, record bout, finish,
  correction); decisions; reopen, send back to the queue, queue it and reopen; court move and
  time change; a representative bout added or removed; a kachinuki bout removed; a match
  lineup saved or removed; a starting lineup saved; a team member added, renamed or cleared;
  a reinstatement; a pool rank order (chusen).
- **Also in the outbox**: check-in (one competitor or many); a court call announced,
  withdrawn or cleared; the tournament settings; the fighting-spirit awards.
- **Owner**: entries are ordered per match, or per team, competitor, competition, court call,
  the clear of every court call, or the tournament settings. The outbox lives in
  `localStorage` and is shared by every tab.
- **Setup edits** (direct): the password change, competition settings, the roster and seeds,
  the draw, a competition's start and completion, Swiss rounds, league tie-breaks, sponsors,
  the logo upload and import. Each is sent at once, and an error goes straight back to the
  page.

The Participants page saves the whole competitor list in one call (a typed seed, a row moved, a
shuffle, seeds cleared or read from a file, a pasted list applied), while registration desks
check competitors in and add them at the same time. Two rules keep each desk's change:

- **A list save never writes a check-in.** Every competitor keeps the check-in the server holds
  for them, whatever the list says. Only the check-in calls check a competitor in or out.
- **A list save names the list it was built from** (`rosterVersion`, which every competition
  answer carrying the list includes). When another desk has added, removed, renamed, moved or seeded a
  competitor since, the server refuses the save with 409 `roster_changed`, writes nothing, and
  answers with the list as it stands. The page shows that list at once, applies the
  operator's change to it again (the seed, the move, the shuffle) and sends it straight away,
  so both desks' changes land. A pasted list is the one exception: it would replace a list the
  operator never saw, so the page saves nothing, says the list changed on another device and
  keeps the pasted list in the box for one tap to apply it again. A check-in does not count as a
  change of the list.

```mermaid
flowchart TB
    arm["Watchdog armed<br/>on connect"] --> silent{"Silent<br/>for 35 s?"}
    silent -->|no| arm
    silent -->|yes| reconnect["Close + reconnect<br/>backoff + jitter"]
    back["browser 'online'<br/>event"] --> rco["reconnectEvents()"]
    vis["Tab visible<br/>again"] --> rc["reconnectEvents()<br/>+ refetch"]
    reconnect --> reopen["Stream reopens<br/>after a loss"]
    rco --> reopen
    reopen --> rf["Refetch<br/>(jittered)"]
```

The SSE liveness checks: a watchdog armed when the stream connects (not only on `onopen`)
closes and reopens a stream that has carried no message or heartbeat for 35 s. The browser's
`online` event and a `visibilitychange` to visible both force a reconnect; a tab shown again
also refetches at once, and any stream that reopens after a loss refetches, jittered.

Key client mechanisms (all in `web-mobile/js/api_client.jsx` + consumers):

| Concern | Mechanism |
|---|---|
| Half-open / stalled sockets | 12s write timeouts, 35s SSE silence watchdog (armed at connect, not only `onopen`) |
| Reconnect storms | exponential backoff + jitter (vs. a fixed delay) |
| Lost writes | durable outbox in `localStorage`, retried with no age limit, each owner's writes sent strictly in order; refer to [The outbox](#the-outbox) |
| Order of writes | nothing is thrown away; changes apply in the order they were made, group by group, and what loses is kept in the match's history; refer to [Order of writes](#order-of-writes) |
| Edits not yet sent | a score editor holds nothing back on a running match. Every tap (a point, a foul, an overtime period, a flag, a bout's tie or fusensho, a fighter named for a bout, a representative pick, the representative bout's judges' decision) goes into the outbox the moment it is made, in the order it was made, and the board shows it at once, as the server's match with the device's pending taps on top. There is no autosave and no save delay, and nothing to lose by closing the editor, moving to another match, hiding the tab or reloading the page. A tap carries the time it was made, not the time it is sent, so it never outranks a result another device recorded after it. The exceptions are a few entries that are not taps: on a running match a Mark draw, a hantei pick and a removed withdrawal are held in the editor until Finish (or until a decision carries them). On a match that is not running (queued, or finished and being corrected) the editor holds everything the operator enters as a draft, sent by Start, Finish or Save correction. Leaving with entries not yet sent, by closing the editor, moving to another match or leaving the page, asks first with one warning, "Your unsaved entries will be discarded. <action> anyway?", with **Discard entries** and **Keep entries**, and nothing is sent until the operator answers |
| Taps the server does not apply | a tap is answered one by one. An applied, already recorded or repeated tap leaves the queue quietly. A tap refused for a clock too far ahead is stamped again and sent at once under the same id; refused again after that, it stays queued and held, with a notice, and is never dropped. A tap the answer gives no outcome for is kept and sent again as it went. Any other tap the server answered without applying (the match is finished, `matchClosed`; the mark it names is not there, `noTarget`; made before a result that never saw it, `beforeBaseline`; a self-run limit) leaves the queue and is told to the operator in the editor and in one alert per pass. The answer also lists the `gaps`, the taps of the device's session the server never received, and the outbox sends the ones it still holds again at once. Every copy of the match names, per device session, the highest tap number its log holds (`eventSeqs`), so a screen never counts a pending tap twice, and lists the device taps the server does not apply now (`turnedAway`): a tap first answered as applied can be turned away later, when a finish that never saw it arrives after it, and the device that made it tells its operator once, from the newest copy of the match it has seen, in one tab. Two tabs reading the same tap at the same moment can each tell it, so the notice may come twice, but it is never lost. A tap sent while the match is neither running nor finished is not logged and is told the same way. A tap on a finished match is logged and answered `matchClosed`. A whole batch the server refuses with a 4xx other than 401 is parked instead of dropped |
| Server errors (5xx / 429), and 403 | write stays queued and keeps retrying until it lands, holding back only that owner's later entries; after 10 consecutive rejections the operator gets a notice and the header count reads "Not saving", and the match's score editor offers **Discard held result**, which discards that write and nothing else after a confirm. The held-writes count in the admin topbar opens a list of every held write (score updates, results, decisions, lineups, check-ins, court calls, tournament settings, fighting-spirit awards), where any write that keeps failing can be discarded the same way, one at a time; a write only waiting for the connection cannot be discarded there. The public page shows the same held-writes count in its bottom-left corner (only while a write is held) and opens the same list. On this server 403 is never a bad credential (that is 401): it means the tournament is not configured yet, or is missing its password. Only an admin fixing the server state clears it, so signing in again cannot help and the write keeps retrying instead of parking |
| Invalid credential (401) | the one 4xx a retry can fix: the write is parked, not discarded, stops retrying, and shows "Sign in to save"; signing in again re-sends it with the new credential, together with every other queued write still carrying the refused password (one queued after it, before its own 401 came back), so one sign-in is enough. A write queued without a password (a competitor's in a self-run event) is never given one |
| Other non-retryable 4xx (400 validation, 413, generic 409) | a whole write is discarded (it can never succeed on retry), and the operator always gets a visible notice naming the match and the server's reason. A batch of taps the server refuses with a 4xx other than 401 (a 400, say) is not discarded: it is parked, stays out of the board and the display, and holds back that match's later entries until the operator discards it from the editor or the held-writes list, with one alert |
| Missed events | replay from the last event id the client saw (`?lastEventId=`), `checkSeqGap` on every event → scoped refetch; `resync_required`. A stream that reopens after it was lost also refetches everything, jittered over 2s so a venue's devices do not all ask at once: replay does not resend an event the page received whose refetch then failed because the device could not fetch |
| Back online | the browser's `online` event forces a reconnect, and the reopened stream refetches as above (the stream can outlive the outage, so it is not left to report a loss itself) |
| Tab resume | `visibilitychange` → force reconnect + refetch |
| False success | terminal writes show pending / parked / still-retrying / failure state, never a false "saved"; a write dropped because the stored entry was corrupt also raises a visible notice at page load, never a silent loss. The admin topbar counts this tab's held writes after the connection status ("Offline: 1 result not sent", "Sending 1 result…", "Not saving: 1 result"), and the public page shows the count in its bottom-left corner while a write is held, so a held result stays visible after the editor that held it closes, and the connection status reads "Reconnecting…" while held writes fail for network reasons. When held finished results land, one success toast per flush says so ("1 finished result sent."). Every write passes through the outbox, so a result that lands at once raises none of these (the success toast appears only for results that had been held) |
| Storage full | if the browser can't persist the queue (storage quota exhausted), that is surfaced to the operator rather than swallowed: an alert, and the queued write answers `persisted: false`, so its score editor says "Not sent yet: keep this page open until the connection returns." instead of "saved on this device" |
| Credential change | queue cleared on logout (the operator is asked to confirm first if writes are still unsent). On `password_reset` the queued writes are parked instead: the old password is removed from every stored write, every tab's included, and they are re-sent once the operator signs in again (no stale-password retries) |

### The outbox

The outbox is stored in `localStorage` and survives a tab refresh; each write in it is retried until it lands, with no age limit. Every tap, every rule-bound write, and every check-in, court call, tournament settings save and awards save is stored in the outbox in the moment it is made, before anything is sent, so the browser never holds a score only in memory.

Each entry belongs to one owner: a match, or a team (a starting lineup, a team member added, renamed or cleared), a competitor (a reinstatement) or a competition (a pool rank order, its check-ins, its awards), a court call (a clear of every court call has a queue of its own), or the tournament settings, and a flush pass sends the entries of each strictly in the order they were made (their stamp, then the order they were stored). One that does not land (offline, a 5xx that keeps coming back, a sign-in it waits for, or an entry it must follow) ends its turn for the pass and holds back the later entries of the same match, team, competitor, competition, court call or the tournament settings, so a write made after another on the same device never reaches the server first. The others go on.

Consecutive taps of one device session go as one batch of at most 64. A tap is idempotent by its event id: sent twice, it is logged once and answered as a duplicate, so a lost answer is harmless.

A court move, a time change, a lineup save, a match lineup's removal, a reinstatement, a pool rank order and a team member's rename or cleared name are taken in the order they arrive, so each carries a write id, and the server answers a late duplicate as recorded without applying it again. A team member's add carries the id the device made for the member, so the same add sent again is answered with the member as stored. A reopen, a send back to the queue, a queue-it-and-reopen, a kachinuki bout's removal and a representative bout's add or removal each carry a write id too, which the match's score log keeps, so a late duplicate of any of them is also answered as recorded. A lineup save's positions are still ordered by when each was changed (`changedAt`), and a match lineup's removal by when it was made (`removedAt`), so a late copy of an earlier save never puts an older value back.

A later running update never takes the place of a queued Finish or decision, and a decision and a score for the same match are both kept; a tap never folds into anything. Only a score write folds into the score write queued just before it, when that one has not been sent (a running update into a running update, a Finish into either, as when the editor re-sends a held Finish), naming every group either changed. A lineup save folds too: it joins a queued save of the same lineup, which then names every position either changed, with the later value where both changed one. A tournament settings save or an awards save that has not been sent yet takes the place of a queued save of the same document.

A check-in carries the tap's stamp and the later tap wins (`superseded` otherwise). A court call carries the id the device made, so a resend is answered `recorded`. A settings or awards save carries the version it was made from: a stale one is refused 409 with the current copy, and one whose body is already the stored document is answered 200. A refusal on replay is always reported, never taken as success.

Every tab of the app shares the stored outbox. A tab saving its queue merges into it: it adds its own writes, keeps the newer copy where two tabs hold the same entry, and removes only the writes it settled. Each tab also takes up the writes another tab stores, so a write queued in a tab that then closes is still sent by a tab left open. A write two tabs hold can be sent by both, which is harmless, because the server orders writes by their stamps and recognises a repeated id (refer to [Order of writes](#order-of-writes))

### Order of writes

Nothing a device sends is thrown away, and changes are put in the order they were made (the write's or the tap's own stamp), never the order they arrive.

A device stamps a write with its estimate of the server's time, and never earlier than the match it was made against: two devices' estimates can differ by up to the 5 s the server accepts, so a change made a moment after a write stamped by a device running ahead would otherwise read as older than the write it changed. A tap is stamped with the same rule, against the match as it was when the operator tapped, so a change another device makes afterwards never moves it. The stamp is therefore the later of the device's time and the stamp of the match as the operator saw it plus 1 ms, never more than 4 s past the device's time.

Taps are replayed by their stamps on the server, and a whole write names the groups of the match it changes (`changed`: `points`, `result`, `encho`, `flags`, `rep`, each side's representative pick, `repPickA` and `repPickB`, or one bout row, `bout:<position>`). The score editor works this out by comparing what it sends with the match it was showing. The server applies each named group only if the write is not older than the last change applied to that group, and never touches a group the write does not name, so two devices changing different things both land whatever order they arrive in. A named group a newer change outranks is not applied: its value is kept in the match's history (`GET /api/competitions/:id/matches/:mid/history`), which the score editors show under **History**. A write applied in part answers with `heldGroups`; one none of which applied answers `{"applied": false, "reason": "superseded"}` with the same list, and the editor says "Not applied" rather than "Not saved".

Once a finish or a decision is recorded, a tap or a scoring write made after it is not applied either: the finish stands, the change is kept in the history, and the answer adds `heldReason: "match_closed"` (a tap's outcome is `matchClosed`), on which the editor tells the operator that the match is finished and to tap Correct. Arrival order does not change the outcome. A tap that reached the server before a finish made earlier is applied at first; when the finish arrives, the tap is moved to the history (an entry of its own, at its own time) and the finish is answered applied, naming it in `displacedGroups` with `heldReason: "match_closed"` and no `heldGroups`; the editor says "Saved" and what was moved, and a held finish replayed this way raises one information alert.

The first recorded result stands: a later finish or decision from a device that did not see the result in force is held in the same way, whichever arrived first, and so is one that would replace a knockout result after the next match was played or started (`heldReason` `downstream_knockout_played` or `downstream_knockout_running`). A representative pick made after its representative bout was removed is moved to the history the same way when the removal arrives, and that answer names it in `displacedGroups` with no `heldReason`. Where the outbox folds a queued running update into a later one, the later one names every group either changed.

The same holds for the writes that are not score sheets: an older save the same device had already followed with a newer one (an out-of-order delivery after a reconnect) is kept whole in the history as an "older revision of this board"; an engi result is ordered by when it was saved, its flags and the winner they decide kept together. A queued decision or finish whose first send landed but whose answer was lost is answered as recorded when it is sent again: the same write under the same stamp changes nothing, so it is not asked for a correction reason and nothing is written to the match; the match's history gains a line saying the write arrived and changed nothing

## 5. Authentication on the network

```mermaid
flowchart TB
    req["Request to a<br/>password-gated route"] --> conf{"Tournament<br/>configured?"}
    conf -->|no| f403a["403 not configured"]
    conf -->|yes| selfrun{"Self-run, and not an<br/>organiser route?"}
    selfrun -->|yes| ok["Handler"]
    selfrun -->|no| empty{"File mode, stored<br/>password empty?"}
    empty -->|yes| f403b["403 misconfigured"]
    empty -->|no| verifier{"PasswordVerifier"}
    verifier -->|"file: plaintext<br/>locked: bcrypt"| match{"Matches?"}
    match -->|yes| ok
    match -->|"no, or missing"| f401["401 invalid password"]
```

- **Not configured**: before a tournament exists (or, in file mode, while it is still the
  blank default), every gated request is 403 except the write that creates it. In file mode
  that write needs no password; in locked mode it must carry the password the hash matches.
- **Self-run mode**: routes outside the organiser-only set (scoring, starting a match and the
  rest of the play a participant drives; self-run has no check-in) pass without the password check; the organiser's setup,
  corrections and destructive routes stay gated.
- **Empty stored password** (file mode): 403 rather than 401, because the server state is
  wrong, not the request.
- **Wrong or missing password**: 401. A 403 is therefore never a bad credential, which is why
  the outbox retries it instead of parking it (§4).

- **File mode** (default): plaintext compare against `tournament.md`. `POST /api/tournament/reset`
  (for a forgotten admin password) answers only a caller on the machine running the server,
  judged on the TCP peer address; any other caller gets 403 `reset_local_only`.
- **Locked mode** (`--lock-password` / `LOCK_PASSWORD=true` + `TOURNAMENT_PASSWORD_HASH`): bcrypt
  compare; reset endpoint returns 404. `GET /api/auth-config` reports the mode to the SPA.

## 6. Server-side network hardening (`cmd/mobile_app.go`)

| Setting | Value | Why |
|---|---|---|
| `ReadHeaderTimeout` | 10s | slowloris-header defense |
| `ReadTimeout` | 30s | slow-body defense (still allows multi-MB CSV import) |
| `IdleTimeout` | 120s | bounds fd commitment per idle keep-alive |
| `WriteTimeout` | **0** | SSE is infinite; cancellation through the request context |
| `MaxHeaderBytes` | 1 MB | header-bomb defense |
| Body cap (admin JSON) | 1 MB | `MaxBodyBytes` middleware → 413 |
| Body cap (`/tournament/import`) | 64 MB | matches multipart CSV import |
| Graceful shutdown | 30s | `Hub.Close` through `RegisterOnShutdown` |

## 7. Scale limit = egress

Because every real-time update is fanned out to **every** connected viewer, **network egress is the
practical ceiling**, not CPU/RAM. Refer to [Infrastructure architecture](infrastructure-architecture.md#5-capacity-scaling)
for per-tier audience guidance (for example, GCP free tier compared with Oracle for large events).
