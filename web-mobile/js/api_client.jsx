// HTTP client for the mobile-app API.
//
// Split out of api.jsx (T006 / NFR-006). The serializer helpers
// (normalizeMatch, normalizeCompetitionDetail, etc.) live in
// api_serializers.jsx and are imported here for use on responses.
//
// Public function signatures intentionally mirror the original API
// object so importers (admin.jsx, viewer.jsx) keep working unchanged
// when they go through the api.jsx re-export shim.
//
// Auth: mutations send an `X-Tournament-Password` header. Read endpoints
// (/api/viewer/*) are unauthenticated.
//
// Error contract: every method that throws does so with `new Error(msg)`
// where `msg` is the server-reported `error` field if present, else a
// per-method default string. Callers decide whether to .catch and toast.
//
// overrideBracketWinner is the exception: it returns { applied } parsed from
// the 200 body (mp-y3nk), so the caller can tell a landed write from a stale
// reconnect replay the server dropped.
//
// Empty-body methods (overridePoolRank,
// resetOverrides, updateMatchTime, moveMatchCourt, updateSchedule,
// deleteCompetition) deliberately return `true`
// rather than `res.json()`: calling res.json() on a 200/204 with no
// body throws SyntaxError per the Fetch spec, which used to surface as
// "alert: Unexpected end of JSON input" right after a successful save.
// See __tests__/api.test.jsx for the regression coverage.
//
// Wifi-hardening additions (mp-gpra):
//   F1: fetchWithTimeout: per-request AbortController, 12 s default.
//   F2: SSE silence watchdog: reconnects if no message for 35 s (>2× heartbeat).
//   F3: SSE reconnect exponential backoff + jitter (1–30 s, reset on open).
//   F4: Write queue persisted to localStorage (6 h TTL, try/catch safe).
//   F5: Terminal writes (decision, completed score, lineup) durable via queue.

import { normalizeCompetitionDetail, normalizePlayer, toBackendMatchResult, buildPlayerMetadata } from './api_serializers.jsx';
import { bridge as _bridge } from './court_bridge.jsx';

// ---------------------------------------------------------------------------
// F1: fetch with per-request timeout via AbortController
// ---------------------------------------------------------------------------
// fetchWithTimeout wraps the native fetch() with an AbortController so
// any stalled request is aborted after `ms` milliseconds (default 12 s).
// An abort is treated as a NETWORK FAILURE: the AbortError propagates as a
// rejected promise so callers enter the same catch branch as a TCP reset.
// The clearTimeout in finally ensures the alarm never fires after the
// request has already settled, avoiding a stale abort on a new controller.
// ---------------------------------------------------------------------------

/**
 * fetch() with an automatic abort timeout.
 * @param {string} url
 * @param {RequestInit} opts
 * @param {number} [ms=12000]
 * @returns {Promise<Response>}
 */
function fetchWithTimeout(url, opts, ms = 12000) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), ms);
    return fetch(url, { ...opts, signal: controller.signal }).finally(() => clearTimeout(timer));
}

// normalizeViewerCompItem maps one {config, poolMatches, bracket} item from the
// aggregate GET /api/viewer/competitions or the court-scoped GET
// /api/viewer/court/:court/matches into the flattened, normalized competition shape
// the UI consumes (config fields hoisted, players + matches normalized). Shared
// so both endpoints stay in lockstep.
function normalizeViewerCompItem(item) {
    const c = item.config || item;
    return normalizeCompetitionDetail({
        ...c,
        poolMatches: item.poolMatches,
        bracket: item.bracket,
        players: (c.players || []).map(normalizePlayer),
    });
}

// ---------------------------------------------------------------------------
// C2: Monotonic per-match revision counter
// ---------------------------------------------------------------------------
// Stamps "running" autosave writes so the server can drop out-of-order
// delivery from a reconnect flush. The counter is per (compID, matchID) and
// increases strictly monotonically within a page session. Completed writes
// do not use it (the guard is gated to running status on the server too).
// ---------------------------------------------------------------------------

/** Composite key for per-(compID, matchID) maps. Prevents collisions across competitions. */
function _revKey(compID, matchID) { return `${compID}:${matchID}`; }

// ---------------------------------------------------------------------------
// mp-y3nk: server-clock offset for timestamp reconciliation.
//
// Writes (score / override-winner) are stamped in SERVER-RELATIVE time so an
// offline court's changes reconcile against other courts by ONE clock, not each
// tablet's local clock. The offset is learned from GET /api/time and refreshed
// on reconnect. Until the first successful learn, the offset is 0 (writes carry
// approximately local time); the server treats an unstamped/legacy 0 as
// arrival-order, and a small offset error only matters for genuinely concurrent
// same-field edits, so the degradation is graceful.
// ---------------------------------------------------------------------------
let _serverClockOffsetMs = 0;
async function _learnServerClockOffset() {
    try {
        const t0 = Date.now();
        const res = await fetchWithTimeout('/api/time', {}, 5000);
        if (!res.ok) return;
        const body = await res.json();
        const t1 = Date.now();
        if (typeof body.nowMs !== 'number') return;
        // Net out the round-trip: estimate the server clock at the response
        // arrival by adding half the RTT to the reported time.
        _serverClockOffsetMs = (body.nowMs + Math.round((t1 - t0) / 2)) - t1;
    } catch (_e) {
        // Offline / timeout: keep the last known offset (0 on first ever call).
    }
}
// _serverNowMs returns the current time in the server's clock frame. Never
// negative-guarded: callers only compare relative order, so a monotonic-ish
// value is what matters.
function _serverNowMs() { return Date.now() + _serverClockOffsetMs; }

const _matchRevCounters = new Map(); // compID:matchID → int
function _nextRev(compID, matchID) {
    const key = _revKey(compID, matchID);
    const next = (_matchRevCounters.get(key) || 0) + 1;
    _matchRevCounters.set(key, next);
    return next;
}

// Per-page-load scoring-session id. Identifies one client session so the
// server's rev-guard can drop a single session's own out-of-order delivery
// (a reconnect flush). Different sessions are concurrent operators: the
// server treats those as last-write-wins. jsdom-safe fallback.
const _revSession = (typeof crypto !== 'undefined' && crypto.randomUUID)
    ? crypto.randomUUID()
    : `s${Math.random().toString(36).slice(2)}`;

// ---------------------------------------------------------------------------
// C2 / F4 / F5: Offline write queue
// ---------------------------------------------------------------------------
// Holds the latest pending write per queue key (last-write-wins for running
// autosaves; terminal writes supersede running ones for the same match).
// On any network failure the write is queued here and retried on reconnect.
//
// F4: The queue is persisted to localStorage on every change so that a page
// reload or crash during a wifi gap does not lose unsaved scores/decisions.
// Entries older than QUEUE_TTL_MS (6 h) are dropped on rehydration.
//
// F5: Terminal writes (completed score, decision, lineup) use the same queue
// with additional descriptor fields: kind, terminal, method, url.
// A terminal entry for a key SUPERSEDES any running entry for the same key.
//
// Sync-status state is published via _syncStatus / _syncListeners so the UI
// (SyncStatusPill) can render synced / syncing / offline without coupling to
// any Preact component tree.
// ---------------------------------------------------------------------------

/**
 * @typedef {'synced'|'syncing'|'offline'} SyncStatusValue
 */

/**
 * @typedef {'score'|'decision'|'lineup'|'override'} WriteKind
 */

/**
 * Descriptor shape for every queue entry:
 *   compID, matchID: for identity / rev-counter management (scores/decisions)
 *   payload         : request body (object, will be JSON-stringified)
 *   password        : X-Tournament-Password header value
 *   kind            : 'score' | 'decision' | 'lineup'
 *   terminal        : true for completed / decision / lineup writes
 *   method          : HTTP method string ('PUT' | 'POST')
 *   url             : same-origin request path, e.g. /api/competitions/… (for replay in _flushQueue)
 *   enqueuedAt      : Date.now() at enqueue time (for TTL eviction on reload)
 *   attempts        : count of server 5xx/429 rejections on flush retries (mp-q8c6
 *                     retry cap); absent until the first rejection. Network
 *                     failures never increment it.
 *
 * Running score descriptors set terminal=false and do not use method/url
 * (they are always PUT to the score endpoint: _flushQueue hard-codes that
 * path for backward compat with the running path).
 *
 * @typedef {{
 *   compID: string,
 *   matchID: string,
 *   payload: object,
 *   password: string,
 *   kind: WriteKind,
 *   terminal: boolean,
 *   method?: string,
 *   url?: string,
 *   enqueuedAt: number,
 *   attempts?: number,
 * }} WriteDescriptor
 * `method`/`url` are present only on terminal entries; running entries omit them.
 */

const QUEUE_STORAGE_KEY = 'bc_write_queue';
const QUEUE_TTL_MS = 6 * 60 * 60 * 1000; // 6 hours

// Defense-in-depth allowlist for terminal-write replay. The queue only ever
// holds score/decision/lineup writes, which are ALWAYS PUT/POST to
// /api/competitions/…: so a tampered or corrupted bc_write_queue entry can't be
// flushed into some other endpoint or HTTP method. Used by both rehydrate (load)
// and flush, kept in one place so the two checks can't drift apart.
const _ALLOWED_QUEUE_METHODS = ['PUT', 'POST'];
function _isAllowedTerminalRequest(method, url) {
    if (!_ALLOWED_QUEUE_METHODS.includes(method) || typeof url !== 'string' || !url) return false;
    // Defense-in-depth: localStorage is tamperable. Resolve the URL against our own
    // origin and validate the NORMALIZED same-origin pathname. A naive
    // startsWith('/api/competitions/') would let a dot-segment payload such as
    // "/api/competitions/../../api/admin/secrets" pass while fetch normalizes it to
    // "/api/admin/secrets" on the wire. The URL parse also rejects absolute,
    // cross-origin, and protocol-relative ("//evil.com/…") URLs.
    const origin = (typeof location !== 'undefined' && location.origin) || 'http://localhost';
    let u;
    try {
        u = new URL(url, origin);
    } catch (_e) {
        return false;
    }
    return u.origin === origin && u.pathname.startsWith('/api/competitions/');
}

// ---------------------------------------------------------------------------
// F4: localStorage helpers (all accesses wrapped in try/catch for
// private-browsing / storage-quota safety: mirroring app.jsx pattern).
// ---------------------------------------------------------------------------

function _persistQueue() {
    try {
        // Remove the key (rather than writing "[]") when the queue is empty: avoids
        // storage churn and, critically, prevents an in-flight flush that reaches
        // _persistQueue() after clearQueue() from re-creating an empty bc_write_queue,
        // which would undermine the credential-revocation intent of removing it.
        if (_writeQueue.size === 0) {
            localStorage.removeItem(QUEUE_STORAGE_KEY);
            return;
        }
        const entries = [..._writeQueue.entries()];
        localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify(entries));
    } catch (_e) { /* quota / private-browsing: silently skip */ }
}

/** @type {Map<string, WriteDescriptor>} */
const _writeQueue = new Map(); // queue key → pending write descriptor
let _syncStatus = /** @type {SyncStatusValue} */ ('synced');
const _syncListeners = new Set();

// Count of in-flight running writes (online path). Drives the syncing pill
// even when the queue is empty (i.e. the write succeeded and was removed from
// the queue but the fetch hasn't resolved yet).
let _inflightRunning = 0;
// Set to true ONLY when a flush attempt ends with at least one true NETWORK
// failure (fetch rejected: connection down). A non-2xx server response (e.g. a
// transient 5xx/429) keeps the write queued for retry but is NOT "offline": the
// network is up, so the pill stays "Syncing…" rather than "Offline". Cleared
// back to false when a flush drains the queue or no network failure occurs.
let _offlineFlag = false;

function _setSyncStatus(s) {
    if (_syncStatus === s) return;
    _syncStatus = s;
    for (const fn of _syncListeners) {
        try { fn(s); } catch (_e) { /* swallow */ }
    }
}

// Terminal-write FAILURE channel. When a queued terminal write (completed score /
// decision / lineup) is permanently discarded: a non-retryable 4xx on retry
// (409 conflict, 400 validation, 401/403 auth), a corrupt entry, or the mp-q8c6
// server-rejection cap (also applied to running writes): the write never landed.
// Sync status alone returns to 'synced', which would let the editor
// clear its pending banner and look "saved". This channel lets surfaces show an
// explicit "not saved" state instead. Payload: {compID, matchID, kind, status, reason}.
const _terminalFailListeners = new Set();
function subscribeTerminalWriteFailed(fn) {
    _terminalFailListeners.add(fn);
    return () => _terminalFailListeners.delete(fn);
}
function _notifyTerminalWriteFailed(info) {
    for (const fn of _terminalFailListeners) {
        try { fn(info); } catch (_e) { /* swallow */ }
    }
}

// Bracket-resync channel. When a queued override-winner assertion the server
// LWW-dropped returns 200 {"applied": false} and emits NO SSE broadcast, any
// optimistic local bracket advance from it is stale. This channel asks listeners
// (e.g. AdminShiaijo) to refetch. This is a BENIGN supersede: do NOT reuse
// _terminalFailListeners, which surfaces a "not saved" ERROR state. Payload:
// {compID, matchID, reason}.
const _bracketResyncListeners = new Set();
function subscribeBracketResync(fn) {
    _bracketResyncListeners.add(fn);
    return () => _bracketResyncListeners.delete(fn);
}
function _notifyBracketResync(info) {
    for (const fn of _bracketResyncListeners) {
        try { fn(info); } catch (_e) { /* swallow */ }
    }
}

/**
 * Recompute and publish the correct sync status from current state:
 *   offline  : _offlineFlag is set and the queue still has entries
 *   syncing  : any in-flight running write OR the queue is non-empty
 *   synced   : otherwise
 */
function _recomputeSyncStatus() {
    if (_offlineFlag && _writeQueue.size > 0) { _setSyncStatus('offline'); return; }
    _setSyncStatus((_inflightRunning > 0 || _writeQueue.size > 0) ? 'syncing' : 'synced');
}

/**
 * Subscribe to sync-status changes. Returns an unsubscribe function.
 * @param {(status: SyncStatusValue) => void} fn
 * @returns {() => void}
 */
function subscribeSyncStatus(fn) {
    _syncListeners.add(fn);
    // Replay current status so the subscriber can initialise its state.
    try { fn(_syncStatus); } catch (_e) { /* swallow */ }
    return () => _syncListeners.delete(fn);
}

let _flushTimer = null;
const FLUSH_BACKOFF_MS = [500, 1000, 2000, 4000, 8000];
let _flushAttempt = 0;
// mp-q8c6: per-entry cap on SERVER rejections (5xx/429). A deterministic 5xx
// (e.g. a validation bug surfaced as HTTP 500, observed with an engi flag
// check during mp-n19y UAT) would otherwise retry forever: the entry poisons
// the queue for the rest of the session, the pill wedges on "Syncing…", and
// the operator gets no signal short of clearing localStorage. After this many
// consecutive server rejections the entry is dropped and surfaced through the
// write-failed channel instead. Network failures (fetch rejected: connection
// down / timeout) deliberately do NOT count: a real offline period must keep
// writes queued for the full QUEUE_TTL_MS, not silently shed them. At the 8s
// max backoff, 10 rejections ≈ 1 minute of a persistently-erroring server:
// long enough to ride out a restart behind the TLS proxy (transient 502/503),
// short enough that a poisoned write can't wedge the queue for hours.
const MAX_QUEUE_SERVER_REJECTIONS = 10;
// Single-in-flight guard. _flushQueue's body awaits network I/O, so without a
// lock a second trigger (a rapid enqueue, an `online` event, or a backoff timer
// firing mid-flush) would start an OVERLAPPING loop iterating the same snapshot:
// duplicate PUTs and corrupted backoff state. Instead, a trigger that arrives
// while a flush is running sets _flushRequested; the running loop reruns once it
// finishes so newly-queued or still-failed writes are retried without overlap.
let _flushInProgress = false;
let _flushRequested = false;
// Bumped by clearQueue() (logout / password_reset). An in-flight _flushQueue loop
// snapshots this at the start and aborts if it changes mid-loop, so a credential
// revocation can't keep sending queued writes with the now-revoked password.
let _queueGen = 0;

async function _flushQueue() {
    if (_flushInProgress) { _flushRequested = true; return; }
    _flushInProgress = true;
    try {
        do {
            _flushRequested = false;
            // Clear any pending backoff timer so a rerun never leaves an orphaned
            // timer that would later trigger a redundant flush.
            if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }

            if (_writeQueue.size === 0) {
                _offlineFlag = false;
                _persistQueue();
                _recomputeSyncStatus();
                continue;
            }
            _recomputeSyncStatus();
            // Snapshot the current entries. Each descriptor is the object reference
            // stored in the map at the time of the snapshot. After an await, we check
            // identity before deleting so a newer write (a fresh object literal set
            // under the same key by enqueueRunningWrite) is not accidentally removed.
            const entries = [..._writeQueue.entries()];
            const gen = _queueGen; // clearQueue() bumps this to cancel an in-flight flush
            let anyFailed = false;      // any failure → keep in queue + backoff
            let networkFailed = false;  // fetch rejected → connection down → "offline"
            for (const [key, descriptor] of entries) {
                // If clearQueue() ran during a prior await (logout / password_reset →
                // credential revocation), abort before sending anything else with the
                // now-revoked password/header.
                if (gen !== _queueGen) break;
                // Skip entries removed or superseded since the snapshot was taken.
                if (_writeQueue.get(key) !== descriptor) continue;
                const { compID, matchID, payload, password, terminal, kind, method, url } = descriptor;
                // Running score writes (terminal=false) don't store method/url and
                // always PUT to the score endpoint. Terminal entries carry their own
                // method+url, which may have been rehydrated from (tamperable)
                // localStorage: validate against the allowlist (PUT/POST to
                // /api/competitions/…) and DROP anything outside it rather than flush
                // to an unexpected route or method.
                let effectiveMethod, effectiveUrl;
                if (terminal) {
                    if (!_isAllowedTerminalRequest(method, url)) {
                        console.warn(`[sync] dropping terminal ${kind || ''} write with disallowed method/url:`, method, url);
                        _notifyTerminalWriteFailed({ compID, matchID, kind, status: 0, reason: 'corrupted queue entry' });
                        if (_writeQueue.get(key) === descriptor) _writeQueue.delete(key);
                        continue;
                    }
                    effectiveMethod = method;
                    effectiveUrl = url;
                } else {
                    effectiveMethod = 'PUT';
                    effectiveUrl = `/api/competitions/${compID}/matches/${matchID}/score`;
                }
                try {
                    const res = await fetchWithTimeout(effectiveUrl, {
                        method: effectiveMethod,
                        headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
                        body: JSON.stringify(payload),
                    });
                    if (res.ok) {
                        // Success (HTTP 200/201, including a stale {stale:true} no-op): remove
                        // from queue only if no newer write has replaced this descriptor.
                        if (terminal && kind === 'override') {
                            // A queued feeder-winner assertion the server LWW-dropped returns
                            // 200 {"applied": false} and emits NO broadcast, so any optimistic
                            // local advance from it is stale. Ask listeners to refetch. This is a
                            // benign supersede, NOT a write failure, so do NOT use the terminal-fail
                            // channel (which surfaces a "not saved" error).
                            const body = await res.json().catch(() => ({}));
                            if (body && body.applied === false) {
                                _notifyBracketResync({ compID, matchID, reason: 'lww_dropped' });
                            }
                        }
                        if (_writeQueue.get(key) === descriptor) {
                            _writeQueue.delete(key);
                            // A confirmed terminal score write needs no further rev
                            // tracking: drop its counter (mirrors recordScore's online
                            // completed path) so _matchRevCounters doesn't grow for the
                            // tab's lifetime now that completed writes are queued.
                            if (terminal && kind === 'score') _matchRevCounters.delete(_revKey(compID, matchID));
                        }
                    } else if (res.status >= 500 || res.status === 429) {
                        // Transient server error: server is up but erroring; keep in
                        // queue and retry with backoff, but this is NOT "offline".
                        // Bounded (mp-q8c6): a DETERMINISTIC rejection (a server bug
                        // answering 500 to a write it will never accept) must not
                        // retry forever. Count server rejections on the descriptor
                        // (persisted with it) and drop + surface once the cap is hit.
                        // The identity checks above compare object references, so
                        // mutating the live descriptor is safe: a superseding write
                        // installs a fresh object and naturally resets the count.
                        const rejections = (Number(descriptor.attempts) || 0) + 1;
                        if (rejections >= MAX_QUEUE_SERVER_REJECTIONS) {
                            const body = await res.json().catch(() => ({}));
                            console.warn(`[sync] dropping queued ${kind || 'running'} write after ${rejections} server rejections (${res.status}):`, body);
                            _notifyTerminalWriteFailed({ compID, matchID, kind, status: res.status, reason: body.reasonHuman || body.error || `HTTP ${res.status} (gave up after ${rejections} attempts)` });
                            if (_writeQueue.get(key) === descriptor) _writeQueue.delete(key);
                        } else {
                            descriptor.attempts = rejections;
                            anyFailed = true;
                        }
                    } else if (terminal && kind === 'decision' && res.status === 409) {
                        // F5: decision-locked-as-success for queued terminal decision entries.
                        //
                        // When a decision POST was sent by the operator but the network dropped
                        // before the response arrived, the request may have landed on the server
                        // (decision already recorded) even though the client never saw a 2xx.
                        // On the subsequent queued retry the server returns 409 with
                        // error="decision_locked" or "already_ineligible": both indicate our
                        // earlier (lost-response) attempt succeeded. Any other 409
                        // (unexpected) can never succeed on retry either: so both
                        // cases discard the entry; only the unexpected one warns.
                        //
                        // IMPORTANT: this lost-response rule applies ONLY to queued retries
                        // inside _flushQueue. The direct recordDecision call path always throws
                        // on 409 so the score editor's force-retry prompt still fires.
                        const body = await res.json().catch(() => ({}));
                        if (body.error !== 'decision_locked' && body.error !== 'already_ineligible') {
                            console.warn(`[sync] queued decision write rejected (409):`, body);
                            _notifyTerminalWriteFailed({ compID, matchID, kind, status: 409, reason: body.reasonHuman || body.error || 'conflict (409)' });
                        }
                        if (_writeQueue.get(key) === descriptor) _writeQueue.delete(key);
                    } else {
                        // Non-retryable response (4xx other than decision-locked above):
                        // 400 validation, 401/403 auth, 413, generic 409 conflict, etc.
                        // This queued write can never succeed, so discard rather than
                        // retry forever and wedge the pill on "Syncing…". For TERMINAL
                        // writes, surface an explicit failure so the editor shows a
                        // "not saved" state instead of silently clearing to "saved".
                        const body = await res.json().catch(() => ({}));
                        console.warn(`[sync] queued ${kind || 'running'} write rejected (${res.status}):`, body);
                        if (terminal) {
                            _notifyTerminalWriteFailed({ compID, matchID, kind, status: res.status, reason: body.reasonHuman || body.error || `HTTP ${res.status}` });
                        }
                        if (_writeQueue.get(key) === descriptor) _writeQueue.delete(key);
                    }
                } catch (_) {
                    // fetch rejected (network down) or aborted by fetchWithTimeout.
                    anyFailed = true;
                    networkFailed = true;
                }
            }
            _persistQueue();
            if (_writeQueue.size === 0) {
                _flushAttempt = 0;
                _offlineFlag = false;
                _recomputeSyncStatus();
            } else if (anyFailed) {
                _offlineFlag = networkFailed;
                _recomputeSyncStatus();
                if (!_flushRequested) {
                    // Schedule a backoff retry only if no immediate rerun is already
                    // pending (a pending rerun re-attempts the failed writes now).
                    const delay = FLUSH_BACKOFF_MS[Math.min(_flushAttempt, FLUSH_BACKOFF_MS.length - 1)];
                    _flushAttempt++;
                    _flushTimer = setTimeout(_flushQueue, delay);
                }
            }
        } while (_flushRequested);
    } finally {
        _flushInProgress = false;
    }
}

/**
 * Enqueue a running-status write for offline-resilient delivery.
 * Last-write-wins per matchId: any older pending write for the same match
 * is replaced (including any terminal entry: a superseded terminal means
 * the operator sent a newer running update after a failed finish, which
 * is handled by the caller's logic; this path keeps them consistent).
 * Triggers an immediate flush attempt; on failure backs off and retries.
 */
// Shared post-enqueue tail for both queue paths: persist, republish sync status,
// reset the backoff counter (a fresh user write must not inherit a stale
// max-backoff delay from a prior failure run), and kick a flush.
function _commitEnqueue(key, descriptor) {
    _writeQueue.set(key, descriptor);
    _persistQueue();
    _recomputeSyncStatus();
    if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
    _flushAttempt = 0;
    _flushQueue();
}

/**
 * Enqueue a running-status write for offline-resilient delivery.
 * Last-write-wins per matchId. Running writes always PUT to the score endpoint,
 * so method/url are omitted: _flushQueue reconstructs that path for terminal=false.
 */
function enqueueRunningWrite(compID, matchID, payload, password) {
    _commitEnqueue(_revKey(compID, matchID), {
        compID, matchID, payload, password,
        kind: 'score', terminal: false,
        enqueuedAt: Date.now(),
    });
}

/**
 * F5: Enqueue a terminal write for offline-resilient delivery.
 * Terminal entries (completed score, decision, lineup) supersede any running
 * entry for the same key: last-write-wins, terminal takes priority.
 * @param {string} key        - Queue key (use _revKey or lineup key)
 * @param {WriteKind} kind    - 'score' | 'decision' | 'lineup'
 * @param {string} method     - HTTP method ('PUT' | 'POST')
 * @param {string} url        - Same-origin request path (e.g. /api/competitions/…)
 * @param {object} payload    - Request body object
 * @param {string} password   - X-Tournament-Password value
 * @param {string} compID     - Competition ID (for identity tracking)
 * @param {string} matchID    - Match ID (for identity tracking)
 */
function _enqueueTerminalWrite(key, kind, method, url, payload, password, compID, matchID) {
    _commitEnqueue(key, {
        compID, matchID, payload, password,
        kind, terminal: true,
        method, url,
        enqueuedAt: Date.now(),
    });
}

// Flush the queue whenever the browser comes back online.
// Remove-then-add so re-evaluating this module (tests with resetModules, hot
// reload, multi-bundle inclusion) replaces the listener instead of stacking up
// orphaned ones bound to stale module state. The handler reference is parked on
// `window` (which survives module re-eval) so the prior one can be detached.
if (typeof window !== 'undefined') {
    if (window.__bcOnlineFlushHandler) {
        window.removeEventListener('online', window.__bcOnlineFlushHandler);
    }
    const onlineHandler = () => {
        if (_writeQueue.size > 0) {
            _flushAttempt = 0;
            if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
            _flushQueue();
        }
    };
    window.__bcOnlineFlushHandler = onlineHandler;
    window.addEventListener('online', onlineHandler);
}

// ---------------------------------------------------------------------------
// F4: Rehydrate write queue from localStorage on module load
// ---------------------------------------------------------------------------
// Parse any entries persisted by a previous page session. Drop stale entries
// (older than QUEUE_TTL_MS = 6 h) so we never replay hours-old scores into
// a tournament that has moved on. Valid entries are added to _writeQueue and
// an immediate flush is triggered so they are delivered as soon as the page
// has network. All localStorage access is wrapped in try/catch for
// private-browsing / quota safety.
// ---------------------------------------------------------------------------

// mp-y3nk: learn the server-clock offset at load so the first writes are already
// stamped in the server's frame (refreshed again on each SSE (re)connect).
if (typeof fetch === 'function') { _learnServerClockOffset(); }

;(function _rehydrateQueue() {
    try {
        const raw = localStorage.getItem(QUEUE_STORAGE_KEY);
        if (!raw) return;
        const entries = JSON.parse(raw);
        if (!Array.isArray(entries)) return;
        const now = Date.now();
        let anyLoaded = false;
        for (const item of entries) {
            // Defensive: a single malformed element (corrupt/tampered storage) must
            // not throw and abort the whole rehydrate: destructuring a non-array in
            // the for-of header would do exactly that, dropping ALL valid queued
            // writes. Validate the tuple shape first and skip bad items individually.
            if (!Array.isArray(item) || item.length < 2) continue;
            const [key, descriptor] = item;
            if (!key || !descriptor || typeof descriptor !== 'object') continue;
            // Drop entries without a valid timestamp or older than TTL. enqueuedAt
            // must be a finite positive number: a tampered/corrupt entry could set
            // it to a non-numeric truthy value (e.g. a string), making
            // (now - enqueuedAt) NaN so the TTL comparison is false and an
            // arbitrarily old write slips through. Validate the type first.
            if (typeof descriptor.enqueuedAt !== 'number'
                || !Number.isFinite(descriptor.enqueuedAt)
                || descriptor.enqueuedAt <= 0
                || (now - descriptor.enqueuedAt) > QUEUE_TTL_MS) continue;
            // Defense-in-depth: localStorage is tamperable. Reject terminal entries
            // whose method/url fall outside the queue allowlist (PUT/POST to
            // /api/competitions/…) before they can ever reach fetch on replay.
            if (descriptor.terminal && !_isAllowedTerminalRequest(descriptor.method, descriptor.url)) continue;
            // Only restore if not already superseded by a same-session write.
            if (!_writeQueue.has(key)) {
                _writeQueue.set(key, descriptor);
                anyLoaded = true;
            }
        }
        if (anyLoaded) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    } catch (_e) { /* private-browsing / malformed JSON: silently skip */ }
}());

// ---------------------------------------------------------------------------
// Shared ref-counted SSE singleton
// ---------------------------------------------------------------------------
// All subscribeToEvents callers share ONE EventSource connection. A Set of
// subscriber records ({callback, onStatus}) is maintained; when the last
// subscriber unsubscribes the source is closed and nulled out.
//
// F2: SSE silence watchdog: if no message (including heartbeat) arrives for
// 35 s (>2× the server's 15 s heartbeat interval, with margin for slow courts)
// the connection is assumed stale and is torn down for a fresh reconnect.
// _lastActivityAt is updated on every onopen and every onmessage.
//
// F3: Reconnect uses exponential backoff + jitter (1–30 s) instead of a fixed
// 5 s delay. _reconnectAttempt increments on each onerror and resets to 0 in
// onopen, so a stable connection always reconnects fast after the first error.
// ---------------------------------------------------------------------------

/** @type {EventSource|null} */
let _sharedSource = null;
/** @type {ReturnType<typeof setTimeout>|null} */
let _retryTimer = null;
/** @type {Set<{callback: Function, onStatus: Function|undefined}>} */
const _subscribers = new Set();

// F2: timestamp of the last received SSE activity (open or message).
let _lastActivityAt = 0;
/** @type {ReturnType<typeof setInterval>|null} */
let _watchdogTimer = null;
const SSE_WATCHDOG_INTERVAL_MS = 10000;  // poll every 10 s
const SSE_SILENCE_THRESHOLD_MS = 35000; // reconnect if silent for >35 s

// F3: reconnect backoff state.
let _reconnectAttempt = 0;

/** Compute the next reconnect delay: exponential backoff 1–30 s plus up to 1 s of jitter. */
function _reconnectDelay() {
    return Math.min(30000, 1000 * Math.pow(2, _reconnectAttempt)) + Math.random() * 1000;
}

/** Arm the silence watchdog. Idempotent: clears any prior interval first. */
function _armWatchdog() {
    _disarmWatchdog();
    _watchdogTimer = setInterval(() => {
        // If the source is gone already (e.g. onerror cleared it) do nothing: 
        // the reconnect timer will re-arm the watchdog when the new source opens.
        if (!_sharedSource) return;
        if (Date.now() - _lastActivityAt > SSE_SILENCE_THRESHOLD_MS) {
            // Silence detected: tear down the stale source and reconnect.
            _sharedSource.close();
            _sharedSource = null;
            _fanOutStatus('error');
            if (_subscribers.size > 0) {
                if (_retryTimer) { clearTimeout(_retryTimer); }
                _retryTimer = setTimeout(_ensureConnected, _reconnectDelay());
                _reconnectAttempt++;
            }
            // Watchdog re-arms itself after _ensureConnected → onopen.
            _disarmWatchdog();
        }
    }, SSE_WATCHDOG_INTERVAL_MS);
}

/** Disarm the silence watchdog (call when no subscribers remain). */
function _disarmWatchdog() {
    if (_watchdogTimer !== null) { clearInterval(_watchdogTimer); _watchdogTimer = null; }
}

function _emitStatus(sub, s) {
    if (typeof sub.onStatus === 'function') {
        try { sub.onStatus(s); } catch (err) { console.error('SSE status callback failed:', err); }
    }
}

function _fanOutStatus(s) {
    for (const sub of _subscribers) {
        _emitStatus(sub, s);
    }
}

function _ensureConnected() {
    // Guard: the retry timer may fire after all subscribers have unsubscribed
    // (clearTimeout only cancels a pending callback: it cannot stop one that is
    // already queued in the event loop). No-op when no one is listening so we
    // don't open a zombie EventSource that can never be closed.
    if (_subscribers.size === 0) return;
    // Whoever calls this is establishing the connection NOW, so cancel any
    // pending reconnect timer: otherwise a retry queued by a previous onerror
    // can fire later and open a second EventSource (a leaked SSE connection).
    if (_retryTimer) {
        clearTimeout(_retryTimer);
        _retryTimer = null;
    }
    // Guard against double-connect: only one shared source may exist at a time.
    if (_sharedSource) return;
    // Bind handlers to THIS instance via a local const. Each handler ignores
    // events from a superseded source (`source !== _sharedSource`) so a stale
    // instance: should one ever fire after being replaced: can't close the
    // live connection or fan out a false status/message to current subscribers.
    const source = new EventSource('/api/events');
    _sharedSource = source;

    // F2: arm the watchdog at connect time, not just in onopen: a CONNECTING
    // stall (half-open / captive / saturated WiFi where onopen, and sometimes
    // even onerror, never fire) is the exact silent-staleness this watchdog must
    // catch. Stamp activity now so the 35s timer measures from the attempt.
    _lastActivityAt = Date.now();
    _armWatchdog();

    source.onopen = () => {
        if (source !== _sharedSource) return;
        // F2: record activity and arm the watchdog on every (re)connect.
        _lastActivityAt = Date.now();
        _armWatchdog();
        // F3: a successful open resets the backoff counter so the next error
        // reconnects fast (starting from 1 s) rather than inheriting a long delay.
        _reconnectAttempt = 0;
        // mp-y3nk: (re)learn the server-clock offset on every connect so writes
        // stamped after a reconnect stay in the server's frame.
        _learnServerClockOffset();
        _fanOutStatus('open');
    };

    source.onmessage = (event) => {
        if (source !== _sharedSource) return;
        // F2: every message (including heartbeat {type:"heartbeat"}) resets the
        // silence watchdog. No seq field on heartbeats: just update the timestamp.
        _lastActivityAt = Date.now();
        let parsed;
        try {
            parsed = JSON.parse(event.data);
        } catch (err) {
            console.error('Error parsing SSE event:', err);
            return;
        }
        for (const sub of _subscribers) {
            try { sub.callback(parsed); } catch (err) { console.error('SSE callback failed:', err); }
        }
    };

    source.onerror = () => {
        if (source !== _sharedSource) return;
        source.close();
        _sharedSource = null;
        // F2: disarm the watchdog: the source is gone; a new one will re-arm it.
        _disarmWatchdog();
        _fanOutStatus('error');
        if (_subscribers.size > 0) {
            // Clear any prior timer before re-arming so repeated error events
            // can't queue multiple concurrent reconnects.
            if (_retryTimer) clearTimeout(_retryTimer);
            // F3: exponential backoff + jitter instead of fixed 5 s.
            _retryTimer = setTimeout(_ensureConnected, _reconnectDelay());
            _reconnectAttempt++;
        }
    };
}

// adminHdr returns the X-Admin-Password header object for the elevated
// (destructive-ops) password (spec 004 / mp-e21), or an empty object when no
// admin password is supplied. Destructive methods spread it into their
// headers so a request can carry BOTH X-Tournament-Password (main) and
// X-Admin-Password (elevated). When the gate is inactive on the server
// (file mode, no admin pw set) callers pass "" and the header is omitted.
function adminHdr(adminPassword) {
    return adminPassword ? { 'X-Admin-Password': adminPassword } : {};
}

const API = {
    async fetchTournament() {
        const res = await fetch('/api/viewer/tournament');
        // "No tournament configured yet" is a normal bootstrap state, not an
        // error: return null so callers open the create-tournament gate quietly
        // instead of logging a console error. Newer servers signal it with a 200
        // and a null JSON body (handled by the res.json() return below); older
        // servers used a 404 (handled here). Genuine failures (5xx, network)
        // still throw below.
        if (res.status === 404) {
            return null;
        }
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch tournament (Status ${res.status})`);
        }
        return res.json();
    },
    // Public endpoint: returns {mode: "file"|"locked", resetEnabled: bool}.
    // Mounted on App() boot to decide whether to render the "Forgot
    // password?" link in AuthModal and whether the /reset SPA route
    // should show a form or an "operator-disabled" message. Always
    // resolves (never rejects): non-2xx responses, network failures, and
    // JSON parse errors all fall back to the file-mode default so a
    // fresh-deploy SPA pointed at an older server without this endpoint
    // still works, and any transport error doesn't break sign-in.
    async fetchAuthConfig() {
        try {
            const res = await fetch('/api/auth-config');
            if (!res.ok) {
                return { mode: 'file', resetEnabled: true };
            }
            return await res.json();
        } catch {
            return { mode: 'file', resetEnabled: true };
        }
    },
    async fetchVersion() {
        try {
            const res = await fetch('/api/version');
            if (!res.ok) {
                return null;
            }
            return await res.json();
        } catch {
            return null;
        }
    },
    // Reset the tournament password. Unauthenticated by design: the
    // server enforces "is this endpoint enabled" via the verifier's
    // ResetEnabled() (locked mode returns 404). Throws on non-2xx so
    // the caller can surface the server's error message (including the
    // 404 "reset disabled" case if the SPA's cached authConfig was
    // stale).
    //
    // `originatorId` is a per-tab client ID echoed back on the SSE
    // password_reset event payload so the originating tab can ignore
    // its own broadcast and avoid clobbering the just-written
    // localStorage credential. Optional: when absent the server
    // broadcasts an empty originator and ALL tabs log out.
    async resetPassword(newPassword, originatorId) {
        const body = { password: newPassword };
        if (originatorId) body.originatorId = originatorId;
        const res = await fetch('/api/tournament/reset', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to reset password (Status ${res.status})`);
            e.status = res.status;
            throw e;
        }
        // Backend returns 204 No Content: calling .json() on an empty
        // body throws SyntaxError per the Fetch spec (same pattern as
        // overridePoolRank etc. above).
        return true;
    },
    // Set or rotate the elevated (destructive-ops) admin password (spec 004
    // / mp-e21). File mode only: locked mode 404s (credential is the
    // TOURNAMENT_ADMIN_PASSWORD_HASH env var). `password` is the MAIN
    // tournament password (gates the endpoint via AuthMiddleware).
    // `currentAdminPassword` is required only when an admin password is
    // already set (rotation); on first-time set (TOFU) the server ignores it.
    // The endpoint never echoes any password back.
    async setAdminPassword(newPassword, currentAdminPassword, password) {
        const body = { newPassword };
        if (currentAdminPassword) body.currentPassword = currentAdminPassword;
        const res = await fetch('/api/auth/admin-password', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to set admin password (Status ${res.status})`);
            e.status = res.status;
            throw e;
        }
        return res.json();
    },
    async fetchCompetitions() {
        const res = await fetch('/api/viewer/competitions');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competitions (Status ${res.status})`);
        }
        const comps = await res.json();
        if (!Array.isArray(comps)) return comps;
        return comps.map(normalizeViewerCompItem);
    },

    // fetchCourtMatches returns the court-scoped match feed: only the
    // competitions that have a real match physically on `court` right now, each
    // with its full {config, poolMatches, bracket} payload (same per-comp shape
    // as fetchCompetitions). The operator console sources its cross-competition
    // court view from this instead of the whole-tournament aggregate. Returns a
    // normalized competitions array so callers can run tournamentMatches() over
    // { competitions } exactly as they do for the aggregate.
    async fetchCourtMatches(court) {
        const res = await fetch(`/api/viewer/court/${encodeURIComponent(court)}/matches`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch court matches (Status ${res.status})`);
        }
        const data = await res.json();
        const comps = Array.isArray(data.competitions) ? data.competitions : [];
        return comps.map(normalizeViewerCompItem);
    },
    async fetchCompetitionDetails(id) {
        const res = await fetch(`/api/viewer/competitions/${id}`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competition details (Status ${res.status})`);
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // Bootstrap a fresh tournament. In file mode the call is
    // unauthenticated (the AuthMiddleware's uninitialized-bootstrap
    // branch lets it through). In locked mode the server requires the
    // env-var password to authorize even the initial POST: pass
    // `authPassword` (typed by the operator into the locked-mode
    // CreateTournament form) so we can attach it as
    // X-Tournament-Password. The handler discards the body's
    // `password` field in locked mode but the header is what gets
    // verified.
    async createTournament(config, authPassword) {
        const headers = { 'Content-Type': 'application/json' };
        if (authPassword) {
            headers['X-Tournament-Password'] = authPassword;
        }
        const res = await fetch('/api/tournament', {
            method: 'POST',
            headers,
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create tournament");
        }
        return res.json();
    },
    async updateTournament(config, password) {
        const res = await fetch('/api/tournament', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update tournament");
        }
        return res.json();
    },
    async updateCompetition(id, config, password, adminPassword) {
        // Go's domain.Player uses json:"metadata" (array); the JS layer uses the
        // friendlier danGrade string. Convert before encoding so the backend can
        // unmarshal the dan grade into Player.Metadata.
        let body = config;
        if (config.players) {
            body = {
                ...config,
                players: config.players.map((player) => {
                    // Only transform players that have been through normalizePlayer
                    // (which always adds an explicit danGrade key). A Go-sourced player
                    // object without danGrade must be passed through unchanged: if we
                    // destructure it, rest would be empty and metadata[0] would be lost.
                    if (!('danGrade' in player)) return player;
                    const { danGrade, metadata: _m, ...p } = player;
                    const metadata = buildPlayerMetadata(danGrade, player.metadata);
                    return metadata === undefined ? p : { ...p, metadata };
                }),
            };
        }
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password,
                // Roster-bearing updates (non-null players) hit the server's
                // elevated-password gate (spec 004); the caller threads the
                // admin password for those. Settings-only saves omit it.
                ...adminHdr(adminPassword)
            },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update competition");
        }
        return res.json();
    },
    async createCompetition(config, password) {
        const res = await fetch('/api/competitions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create competition");
        }
        return res.json();
    },
    // exportPDFs POSTs to /api/print/:type (admin-gated) and returns the
    // response as a Blob (a ZIP of the produced PDFs). `type` is one of
    // registration|names|tags|pools-trees|full-bracket|all. Throws with the
    // server's error message on non-2xx (e.g. 503 when LibreOffice is absent,
    // 422 when no pages were produced). Generation runs LibreOffice and can
    // take 30–60s, so callers should show a busy state.
    async exportPDFs(type, password) {
        const res = await fetch(`/api/print/${type}`, {
            method: 'POST',
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to generate PDFs");
        }
        return res.blob();
    },
    // exportResults GETs the RESULTS-populated workbook (played scores,
    // standings, winners, and decision suffixes written as literal values)
    // from the admin-gated /api/competitions/:id/export-results endpoint. This
    // is distinct from AdminExport's "Download .xlsx", which posts to the public
    // /create path and yields a BLANK formula template. Returns an .xlsx Blob;
    // throws the server's error message on non-2xx.
    async exportResults(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/export-results`, {
            method: 'GET',
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to export results");
        }
        return res.blob();
    },
    async startCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}/start`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to start competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    async generateDraw(id, password) {
        const res = await fetch(`/api/competitions/${id}/generate-draw`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to generate draw");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    async discardDraw(id, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}/draw`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to discard draw");
        }
    },
    // completeCompetition: POST /competitions/:id/complete. The only trigger
    // for a bracket-based competition (playoffs, or mixed once its knockout is
    // running) to reach status "completed": MaybeAutoCompletePools only
    // auto-transitions the League format, so a finished bracket otherwise sits
    // in "pools"/"playoffs" forever and the public viewer's Awards tab (gated
    // on status === "completed") never becomes reachable. 400 when the
    // competition isn't in "pools" or "playoffs" status (already completed, or
    // not started yet), or a naginata 3rd-place match is still unscored; 404
    // when the competition doesn't exist. Elevated-gated (irreversible), so it
    // takes the admin password like discardDraw.
    async completeCompetition(id, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}/complete`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to complete competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // Subscribe to the server's SSE event stream. `callback` is fired
    // for each parsed event. `onStatus` (optional) is fired with
    // 'open' when the EventSource transitions to open and 'error' when
    // it transitions away: used by the display surfaces (FR-011
    // scenario 4 / T063) to render a reconnect indicator when the
    // browser is between connections. Existing call sites that omit
    // the second arg are unaffected.
    //
    // All callers share ONE module-level EventSource (ref-counted). The
    // source is opened on the first subscribe and closed when the last
    // subscriber unsubscribes. A subscriber joining an already-OPEN source
    // receives an immediate onStatus('open') replay so it starts in the
    // correct connected state without waiting for the next onopen event.
    subscribeToEvents(callback, onStatus) {
        const record = { callback, onStatus };
        _subscribers.add(record);

        if (_sharedSource === null) {
            // No source yet: open one. The new subscriber keeps its optimistic
            // default until this source's onopen/onerror fires.
            _ensureConnected();
        } else if (_sharedSource.readyState === EventSource.OPEN) {
            // Source is already connected: replay 'open' to this subscriber.
            _emitStatus(record, 'open');
        }
        // Else the source exists but is still CONNECTING (a non-null source is
        // only ever CONNECTING or OPEN: we null it on close). Replay NOTHING
        // and let the bound onopen/onerror fan-out deliver the first
        // authoritative status, exactly as the first subscriber experiences.
        // Replaying 'error' here would flash a false "Reconnecting…" on admin
        // load, where AdminTopbar/AdminDashboard subscribe mid-handshake.

        // Idempotent unsubscribe: guard with a `done` flag so double-calling
        // never double-decrements the ref count.
        let done = false;
        return () => {
            if (done) return;
            done = true;
            _subscribers.delete(record);
            if (_subscribers.size === 0) {
                clearTimeout(_retryTimer);
                _retryTimer = null;
                // F2: disarm the watchdog when no subscribers remain: no point
                // in polling for silence when no one is listening.
                _disarmWatchdog();
                if (_sharedSource) {
                    _sharedSource.close();
                    _sharedSource = null;
                }
            }
        };
    },
    async recordScore(compID, matchID, result, password, match) {
        const payload = toBackendMatchResult(result, match || result);
        // mp-y3nk: stamp in server-relative time for last-write-wins reconciliation.
        payload.modifiedAt = _serverNowMs();

        // C2: stamp monotonic rev on running-status writes so the server's
        // rev-guard can drop out-of-order deliveries (e.g. from reconnect flush).
        // Completed writes do not need a rev: the guard is gated on status=running.
        const isRunning = payload?.status === 'running';
        if (isRunning) {
            payload.rev = _nextRev(compID, matchID);
            payload.revSession = _revSession;
            _inflightRunning++;
            _recomputeSyncStatus();
        }

        // mp-9ukk Phase 2: broadcast helper. Publishes a match patch on the
        // court-local BroadcastChannel so the display tab can update without
        // a server round-trip during offline periods.
        //
        // The envelope mirrors the SSE match_updated shape including sideAId and
        // sideBId so the display tab's normalizeMatch can resolve participants by
        // UUID rather than falling back to name-key lookup (which can mis-resolve
        // same-name participants). winnerId is already in rest when present.
        // rev/revSession are stripped so internal write-ordering metadata is not
        // propagated to the display tab.
        const _broadcastPatch = (fields) => {
            const { rev: _r, revSession: _rs, ...rest } = fields || {};
            const court = (match && match.court) || '';
            // Do not emit an unscoped (court-less) broadcast: a display can only
            // safely apply a patch it can attribute to its court, and the display
            // guard drops court-less messages anyway. In production every
            // recordScore caller passes the match (so court is set); the
            // court-less case is test-only (match=null), where no broadcast is
            // expected.
            if (!court) return;
            _bridge.publish('patch', court, compID, {
                result: {
                    id: matchID,
                    ...(match && match.sideA && match.sideA.id ? { sideAId: match.sideA.id } : {}),
                    ...(match && match.sideB && match.sideB.id ? { sideBId: match.sideB.id } : {}),
                    ...rest,
                },
            });
        };

        const scoreUrl = `/api/competitions/${compID}/matches/${matchID}/score`;
        let res;
        try {
            try {
                // F1: use fetchWithTimeout (12 s) so a stalled wifi request doesn't
                // block the UI indefinitely. An abort is treated as a network failure.
                res = await fetchWithTimeout(scoreUrl, {
                    method: 'PUT',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-Tournament-Password': password
                    },
                    body: JSON.stringify(payload)
                });
            } catch (_networkErr) {
                // Network failure or timeout abort. For running-status writes,
                // queue for retry: last-write-wins semantics so only the latest state
                // is ever flushed.
                // F5: completed (terminal) writes are now also queued on transient
                // failures so they are not silently lost on a wifi gap.
                if (isRunning) {
                    enqueueRunningWrite(compID, matchID, payload, password);
                    // mp-9ukk: broadcast even on enqueue so the display tab gets
                    // the optimistic overlay while the server is unreachable.
                    _broadcastPatch(payload);
                    // Return a DISCRIMINATED { queued: true } result. The write was
                    // NOT confirmed by the server: only enqueued for async delivery
                    // via _flushQueue (last-write-wins). Fire-and-forget callers
                    // (debounced autosave) ignore the value. Any caller that awaits a
                    // running write as a HARD PREREQUISITE for a dependent request
                    // MUST branch on `.queued` and treat it as "not yet persisted"
                    // rather than success: see the daihyosen pre-save in
                    // admin_scoring_modal.jsx, which aborts on a queued save so it
                    // never runs recordDaihyosen against stale server-side scores.
                    return { queued: true };
                }
                // F5: completed score: enqueue as terminal and return {queued:true}.
                // A terminal entry supersedes any running entry for the same key.
                _enqueueTerminalWrite(
                    _revKey(compID, matchID), 'score', 'PUT', scoreUrl,
                    payload, password, compID, matchID
                );
                // mp-9ukk: broadcast the terminal write for offline display update.
                _broadcastPatch(payload);
                return { queued: true };
            }
        } finally {
            if (isRunning) {
                _inflightRunning--;
                _recomputeSyncStatus();
            }
        }

        const data = await res.json().catch(() => ({}));
        if (res.ok) {
            if (!isRunning) {
                // A completed/terminal write that the server CONFIRMED supersedes
                // any queued running autosave for this match: drain it now (and
                // prune the per-match rev counter) so a stale flush can't later
                // revert the finalized result. Deferred from pre-flight on purpose:
                // if the completed PUT fails (e.g. Finish while offline) we KEEP the
                // last queued running snapshot so it can still flush when the
                // connection returns, instead of dropping the operator's scores.
                if (_writeQueue.delete(_revKey(compID, matchID))) {
                    _persistQueue();
                    _recomputeSyncStatus();
                }
                _matchRevCounters.delete(_revKey(compID, matchID));
            }
            // A stale running write is signalled by HTTP 200 {stale:true}
            // (the server's rev-guard no-ops it). Do not broadcast it: pushing
            // a superseded running score to the display would overwrite the
            // newer state already there. Return as-is; the fire-and-forget
            // autosave caller ignores the value.
            // mp-9ukk: echo the operator's patch to the court display
            // peer-to-peer so the board keeps updating when the SSE link is
            // slow or down. This is an OPTIMISTIC overlay, not the
            // authoritative source: the request payload already carries the
            // display-critical fields (id, side ids, winner, scores, status)
            // in the same envelope every other broadcast path uses, and the
            // fully server-normalized state arrives via SSE (or the reconnect
            // full refetch), which replaces the overlay. Broadcasting the
            // request payload (not the server `data`) keeps one consistent
            // shape across the confirmed and offline paths. Skip a rejected
            // stale write above so a superseded score is never pushed.
            if (data && data.stale) return data;
            _broadcastPatch(payload);
            return data;
        }
        // A running write that failed with a RETRYABLE server error (5xx / 429)
        // is queued for offline-style retry. Without this the inflight counter
        // drops to 0 and the pill flips back to "Synced" even though the latest
        // score never reached the server (the autosave caller swallows the
        // throw). Queuing keeps the pill on syncing/offline and re-delivers the
        // latest state.
        if (isRunning && (res.status >= 500 || res.status === 429)) {
            enqueueRunningWrite(compID, matchID, payload, password);
            // mp-9ukk: broadcast the queued state for offline display update.
            _broadcastPatch(payload);
            // Discriminated { queued: true }: not server-confirmed; see the
            // network-error branch above for the full caller contract.
            return { queued: true };
        }
        // F5: completed score, transient server error: enqueue as terminal.
        // 4xx on completed scores are non-retryable and throw (below).
        if (!isRunning && (res.status >= 500 || res.status === 429)) {
            _enqueueTerminalWrite(
                _revKey(compID, matchID), 'score', 'PUT', scoreUrl,
                payload, password, compID, matchID
            );
            // mp-9ukk: broadcast the queued terminal state for offline display.
            _broadcastPatch(payload);
            return { queued: true };
        }
        // mp-dc52 Phase 3: the simultaneity gate returns 409 ineligible_competitor
        // with a human-readable reason; prefer reasonHuman, then reason, then code.
        if (data.error === "ineligible_competitor" || data.error === "already_ineligible") {
            throw new Error(data.reasonHuman || data.reason || data.error || "Failed to record score");
        }
        throw new Error(data.error || "Failed to record score");
    },
    // T093–T095: kiken / fusenpai / fusensho / daihyosen: server auto-fills
    // scoreline and Winner from {decision, decisionBy, encho}. Body shape is
    // defined by mobileapp.DecisionRequest in handlers_decision.go; decisionBy
    // MUST be "shiro" or "aka", decisionReason is optional and ≤200 chars.
    // Response is the updated state.MatchResult.
    //
    // F1: uses fetchWithTimeout (12 s abort on stalled wifi).
    // F5: on network failure / abort / 5xx / 429 the decision is enqueued as a
    // terminal write for durable re-delivery. 4xx (including 409 decision_locked
    // on the DIRECT call) always throw: the score editor relies on a thrown 409
    // to show its force-retry prompt. The decision-locked-as-success rule (409
    // treated as success) applies ONLY to queued retries inside _flushQueue.
    async recordDecision(compID, matchID, body, password) {
        const decisionUrl = `/api/competitions/${compID}/matches/${matchID}/decision`;
        let res;
        try {
            // F1: abort after 12 s; AbortError propagates as network failure.
            res = await fetchWithTimeout(decisionUrl, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Tournament-Password': password
                },
                body: JSON.stringify(body)
            });
        } catch (_networkErr) {
            // F5: network failure or timeout: enqueue as terminal for retry.
            _enqueueTerminalWrite(
                _revKey(compID, matchID), 'decision', 'POST', decisionUrl,
                body, password, compID, matchID
            );
            return { queued: true };
        }
        if (!res.ok) {
            // F5: transient server error: enqueue for retry.
            if (res.status >= 500 || res.status === 429) {
                _enqueueTerminalWrite(
                    _revKey(compID, matchID), 'decision', 'POST', decisionUrl,
                    body, password, compID, matchID
                );
                return { queued: true };
            }
            // 4xx (including 409 decision_locked): throw immediately so the UI
            // can surface the error. The decision-locked-as-success rule is ONLY
            // for queued retries in _flushQueue, not for direct calls.
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to record decision");
        }
        return res.json();
    },
    // T098: competitor eligibility statuses produced by kiken/fusenpai
    // decisions. Endpoint is read-only and unauthenticated: same contract as
    // the other /api/competitions/:id/* GETs surfaced to the viewer.
    async fetchCompetitorStatuses(compID) {
        const res = await fetch(`/api/competitions/${compID}/competitor-status`);
        if (!res.ok) throw new Error("Failed to load competitor statuses");
        const data = await res.json();
        return data.statuses || [];
    },
    async reinstateCompetitor(compID, playerID, password) {
        const res = await fetch(`/api/competitions/${compID}/competitors/${playerID}/reinstate`, {
            method: 'POST',
            headers: {
                'X-Tournament-Password': password
            }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to reinstate competitor");
        }
        return res.json();
    },
    async overridePoolRank(compID, poolID, playerName, rank, password) {
        const res = await fetch(`/api/competitions/${compID}/pools/${poolID}/override-rank`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ playerName, rank })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to override rank");
        }
        // Backend returns 200 with empty body. Calling .json() on an
        // empty body throws SyntaxError per the Fetch spec, which would
        // surface to the user as an alert("Failed: Unexpected end of
        // JSON input") right after a successful save.
        return true;
    },
    async overrideBracketWinner(compID, matchID, winnerName, password) {
        const url = `/api/competitions/${compID}/matches/${matchID}/override-winner`;
        // mp-y3nk: stamp in server-relative time for last-write-wins reconciliation.
        const payload = { winnerName, modifiedAt: _serverNowMs() };
        let res;
        try {
            // fetchWithTimeout so a stalled request is treated as offline rather
            // than hanging the "Run now" flow.
            res = await fetchWithTimeout(url, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Tournament-Password': password
                },
                body: JSON.stringify(payload)
            });
        } catch (_networkErr) {
            // mp-y3nk Phase 4: offline / timeout. An operator's feeder-winner
            // assertion (the "Run now" recovery) must survive an update outage
            // like a score does, so queue it as a terminal write. It replays on
            // reconnect; the server then propagates it into the dependent final's
            // sides. Keyed distinctly from score writes ("override:…") so an
            // assertion and a score for the same match id never collide under the
            // queue's last-write-wins-per-key rule. On a later non-retryable 4xx
            // (e.g. the feeder resolved differently server-side) _flushQueue
            // surfaces it via the terminal-write-failed channel rather than
            // silently applying, preserving bracket integrity.
            _enqueueTerminalWrite(`override:${compID}:${matchID}`, 'override', 'PUT', url, payload, password, compID, matchID);
            return { queued: true };
        }
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to override winner");
        }
        // Backend replies 200 {"applied": <bool>} (mp-y3nk). applied=false means
        // the timestamp guard dropped this assertion because a newer/equal result
        // already exists for the feeder, so the server kept a different outcome and
        // the caller must NOT trust its optimistic pick. An older server (or any
        // absent body) yields {} here; default applied=true so back-compat callers
        // keep advancing exactly as before.
        const body = await res.json().catch(() => ({}));
        return { applied: body.applied !== false };
    },
    async resetOverrides(compID, password, adminPassword) {
        const res = await fetch(`/api/competitions/${compID}/overrides`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to reset overrides");
        }
        // Backend returns 204 No Content: .json() would reject.
        return true;
    },
    async updateMatchTime(compID, matchID, scheduledAt, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/time`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ scheduledAt })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update match time");
        }
        return true;
    },
    async estimateSchedule(args, password, signal) {
        const params = new URLSearchParams();
        Object.entries(args).forEach(([k, v]) => {
            if (v !== undefined && v !== null && v !== "" && !Number.isNaN(v)) {
                params.append(k, v);
            }
        });
        const res = await fetch(`/api/schedule/estimate?${params.toString()}`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to estimate schedule");
        }
        return res.json();
    },
    async estimateCompetitionSchedule(compID, password, signal) {
        const res = await fetch(`/api/competitions/${compID}/schedule/estimate`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to estimate schedule");
        }
        return res.json();
    },
    // Court (shiaijo) clashes between this competition and every other one
    // (same day, shared court, overlapping time windows). Returns a possibly
    // empty array of ClashWarning objects. Non-blocking: surfaced as a warning.
    async getScheduleClashes(compID, password, signal) {
        const res = await fetch(`/api/competitions/${compID}/schedule/clashes`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to check schedule clashes");
        }
        return res.json();
    },
    async moveMatchCourt(compID, matchID, court, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/court`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ court })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to move match court");
        }
        return true;
    },
    async revertMatchToQueue(compID, matchID, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/revert-to-queue`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to send match back to queue");
        }
        return true;
    },
    async updateSchedule(compID, entries, password) {
        const res = await fetch(`/api/competitions/${compID}/schedule`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(entries)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update schedule");
        }
        return true;
    },
    async importCompetitions(formData, password, adminPassword) {
        const res = await fetch('/api/tournament/import', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) },
            body: formData
        });
        if (!res.ok) {
            const body = await res.json().catch(() => ({}));
            throw new Error(body.error || 'Import failed');
        }
        return res.json();
    },
    async deleteCompetition(id, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to delete competition");
        }
        return true;
    },
    // Returns the updated competition JSON (including the new `status`) so
    // callers can apply it to local state immediately instead of waiting for
    // the SSE refresh / tournament refetch to land.
    async invalidateCompetition(id, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}/invalidate`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password, ...adminHdr(adminPassword) }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to invalidate competition");
        }
        return res.json();
    },
    // Replace the full fighting-spirit awards list for a competition.
    // awards: array of { title, recipientName, recipientDojo? }.
    // Requires X-Admin-Password only when the elevated gate is active
    // (locked mode); in file mode the gate is optional. adminPassword is
    // sent when provided and ignored server-side when the gate is off.
    async updateCompetitionAwards(id, awards, password, adminPassword) {
        const res = await fetch(`/api/competitions/${id}/awards`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password,
                ...adminHdr(adminPassword)
            },
            body: JSON.stringify({ fightingSpiritAwards: awards })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update competition awards");
        }
        return res.json();
    },
    // T129/T130: per-round team lineups (FR-040). GET returns the persisted
    // TeamLineup for (compId, teamId, round): 404 when no lineup has been
    // submitted yet, which the form treats as "blank, editable". PUT replaces
    // the lineup. DELETE clears it so an operator can revise.
    // opts.fallback: best-effort resolution for match-scoring surfaces: when
    // the exact round has no lineup the server falls back to the closest
    // saved round (highest <= requested, else highest overall) instead of
    // 404. The lineup EDITOR must NOT pass this: 404 means "blank, editable".
    async fetchTeamLineup(compID, teamId, round, opts) {
        const qs = opts && opts.fallback ? "?fallback=best" : "";
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}${qs}`);
        if (res.status === 404) return null;
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load lineup");
        }
        return res.json();
    },
    async putTeamLineup(compID, teamId, round, positions, password) {
        const lineupUrl = `/api/competitions/${compID}/teams/${teamId}/lineups/${round}`;
        const lineupBody = { teamId, competitionId: compID, round, positions };
        // F5: lineup queue key is distinct from score/decision keys so a lineup
        // write doesn't collide with a concurrent score write for the same match.
        const lineupKey = `lineup:${compID}:${teamId}:${round}`;
        let res;
        try {
            // F1: abort after 12 s on stalled wifi.
            res = await fetchWithTimeout(lineupUrl, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Tournament-Password': password
                },
                body: JSON.stringify(lineupBody)
            });
        } catch (_networkErr) {
            // F5: network failure or timeout: enqueue as terminal.
            _enqueueTerminalWrite(
                lineupKey, 'lineup', 'PUT', lineupUrl,
                lineupBody, password, compID, ''
            );
            return { queued: true };
        }
        if (!res.ok) {
            // F5: transient server error: enqueue for retry.
            if (res.status >= 500 || res.status === 429) {
                _enqueueTerminalWrite(
                    lineupKey, 'lineup', 'PUT', lineupUrl,
                    lineupBody, password, compID, ''
                );
                return { queued: true };
            }
            // 4xx: throw immediately (400 validation, etc.).
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to save lineup");
        }
        return res.json();
    },
    async deleteTeamLineup(compID, teamId, round, password) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok && res.status !== 404) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to delete lineup");
        }
        return true;
    },
    // mp-825 / mp-bkg: per-match lineup endpoints. Match ID takes the
    // place of the round key: successive encounters between the same
    // two teams each carry an independent lineup entry.
    // 404 → null (no lineup saved yet; form treats as blank/editable).
    async fetchMatchLineup(compID, teamId, matchId) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`);
        if (res.status === 404) return null;
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load match lineup");
        }
        return res.json();
    },
    async putMatchLineup(compID, teamId, matchId, positions, password) {
        const matchLineupBody = { teamId, competitionId: compID, matchId, positions };
        const matchLineupUrl = `/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`;
        // F5: per-match lineup key: distinct from round-scoped lineups.
        const matchLineupKey = `lineup:${compID}:${teamId}:match:${matchId}`;
        let res;
        try {
            // F1: abort after 12 s on stalled wifi.
            res = await fetchWithTimeout(matchLineupUrl, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                    'X-Tournament-Password': password
                },
                body: JSON.stringify(matchLineupBody)
            });
        } catch (_networkErr) {
            // F5: network failure or timeout: enqueue as terminal.
            _enqueueTerminalWrite(
                matchLineupKey, 'lineup', 'PUT', matchLineupUrl,
                matchLineupBody, password, compID, matchId
            );
            return { queued: true };
        }
        if (!res.ok) {
            // F5: transient server error: enqueue for retry.
            if (res.status >= 500 || res.status === 429) {
                _enqueueTerminalWrite(
                    matchLineupKey, 'lineup', 'PUT', matchLineupUrl,
                    matchLineupBody, password, compID, matchId
                );
                return { queued: true };
            }
            // 4xx: throw immediately (400 validation, etc.).
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to save match lineup");
        }
        return res.json();
    },
    async deleteMatchLineup(compID, teamId, matchId, password) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok && res.status !== 404) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to delete match lineup");
        }
        return true;
    },
    // T141: daihyosen (representative bout) appended after a knockout team
    // match ties on IV and PW. Server validates the tie + eligibility and
    // returns the updated MatchResult with a new SubMatchResult whose
    // decision="daihyosen" and Position=-1. The error codes (not_tied,
    // pool_match, insufficient_eligibility) are surfaced verbatim by the
    // caller: see TeamScoreEditorModal for the user-visible mapping.
    async recordDaihyosen(compID, matchID, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/daihyosen`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to add daihyosen");
        }
        return res.json();
    },
    // T141: remove an unscored daihyosen placeholder from a knockout team match.
    // Returns the updated MatchResult on 200. Throws on 404 (no daihyosen or
    // match not found) or 409 (daihyosen already scored: clear scores first).
    async removeDaihyosen(compID, matchID, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/daihyosen`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to remove daihyosen");
        }
        // The handler responds with an envelope ({ result: MatchResult }); unwrap
        // it so the return value matches the docstring ("the updated MatchResult").
        const body = await res.json().catch(() => ({}));
        return body.result ?? body;
    },
    // T190-T193 (US13: Swiss format). Generate the next Swiss round.
    // Backend pre-conditions: format=swiss; all matches in the current
    // round must be completed; swissCurrentRound must be < swissRounds.
    // Returns { round, matches, swissCurrentRound } on 201, throws on
    // 4xx/5xx. 409 with code="round_incomplete" surfaces a friendly
    // "current round still has incomplete matches" message in the UI;
    // the caller checks `e.code === "round_incomplete"` to branch.
    async swissGenerateRound(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/swiss/generate-round`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            // Preserve the structured 409 payload (code + round) on the
            // thrown Error so the caller can show a precise message. The
            // generic .message stays as the server-reported error string.
            const e = new Error(err.error || "Failed to generate Swiss round");
            if (err.code) e.code = err.code;
            if (err.round !== undefined) e.round = err.round;
            throw e;
        }
        return res.json();
    },
    // T190-T193 (US13). Public endpoint: returns cumulative Swiss
    // standings ranked by wins > points > head-to-head. Returns []
    // when the competition hasn't started yet. Each entry is a
    // state.PlayerStanding (same shape used by pool standings).
    async swissStandings(compID) {
        const res = await fetch(`/api/competitions/${compID}/swiss/standings`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load Swiss standings");
        }
        return res.json();
    },
    async leagueStandings(compID) {
        const res = await fetch(`/api/competitions/${compID}/league/standings`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load league standings");
        }
        return res.json();
    },
    async toggleCheckIn(compID, pid, checkedIn, password) {
        const method = checkedIn ? 'PUT' : 'DELETE';
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/participants/${encodeURIComponent(pid)}/checkin`, {
            method,
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to ${checkedIn ? 'check in' : 'undo check-in'} participant`);
        }
        return res.json();
    },
    // Bulk check-in for one competition. participantIds is an array of pids as
    // built by checkinPid: a stable UUID, or the composite "name|dojo" key for
    // legacy UUID-less rows (the server resolves either). Returns { checkedIn,
    // alreadyCheckedIn, notFound }. Used by the Registration desk's "check in a
    // whole dojo" action.
    async bulkCheckIn(compID, participantIds, password) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/participants/checkin-bulk`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify({ participantIds })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to check in participants');
        }
        return res.json();
    },
    async addParticipant(compID, payload, password, adminPassword) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/participants`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password, ...adminHdr(adminPassword) },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to add participant');
        }
        return res.json();
    },
    async replaceParticipant(compID, pid, payload, password, adminPassword) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/participants/${encodeURIComponent(pid)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password, ...adminHdr(adminPassword) },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to replace participant');
        }
        return res.json();
    },
    async sendAnnouncement(message, durationMinutes, password) {
        const res = await fetch('/api/tournament/announce', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ message, durationMinutes })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to send announcement (Status ${res.status})`);
        }
        return res.json();
    },
    async fetchAnnouncements() {
        const res = await fetch('/api/tournament/announcements');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch announcements (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteAnnouncement(id, password) {
        const res = await fetch(`/api/announcements/${encodeURIComponent(id)}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete announcement (Status ${res.status})`);
        }
    },
    async clearAnnouncements(password) {
        const res = await fetch('/api/announcements', {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to clear announcements (Status ${res.status})`);
        }
    },
    // mp-c38: sponsor logo upload (multipart) and delete.
    async uploadSponsor({ file, name, link, password }) {
        const fd = new FormData();
        fd.append('name', name);
        if (link) fd.append('link', link);
        fd.append('file', file);
        const res = await fetch('/api/sponsors', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: fd,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to upload sponsor (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteSponsor(index, password) {
        const res = await fetch(`/api/sponsors/${index}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete sponsor (Status ${res.status})`);
        }
    },
    // mp-scf: tournament branding: logo upload/delete.
    async uploadBrandingLogo({ file, password }) {
        const fd = new FormData();
        fd.append('file', file);
        const res = await fetch('/api/branding/logo', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: fd,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to upload logo (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteBrandingLogo(password) {
        const res = await fetch('/api/branding/logo', {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete logo (Status ${res.status})`);
        }
    },

    // Phase 3b (mp-8rc9): league tie-breaker operator API.
    //
    // leagueTiebreakCandidates: GET /competitions/:id/league-tiebreak/candidates
    // Returns { candidates: [{teamNames, minPosition, maxPosition}], finalized: bool }.
    async leagueTiebreakCandidates(compID) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak/candidates`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to load league tie-breaker candidates (${res.status})`);
        }
        return res.json();
    },

    // chusenCandidates: GET /competitions/:id/chusen-candidates
    // Consequential team-pool ties the daihyosen could not settle; the operator
    // resolves each by chusen (drawing lots), recorded via overridePoolRank.
    async chusenCandidates(compID, password) {
        // Lives in the admin-gated competition router, so it needs the
        // tournament password header (unlike the public league candidates GET).
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/chusen-candidates`, {
            headers: { 'X-Tournament-Password': password || '' }
        });
        if (!res.ok) {
            // Surface the server-provided error + status so auth/config failures
            // are diagnosable from the admin UI, matching the other helpers.
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to load chusen candidates (${res.status})`);
        }
        const data = await res.json();
        return data.candidates || [];
    },

    // leagueTiebreakGenerate: POST /competitions/:id/league-tiebreak
    // Body: { teamNames: string[] }: the tied group to break the tie.
    // Returns { matches: MatchResult[] } on 201.
    // Throws on 400 (invalid group), 409 (matches already exist).
    async leagueTiebreakGenerate(compID, teamNames, password) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify({ teamNames }),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to generate league tie-breaker (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },

    // leagueTiebreakRemove: DELETE /competitions/:id/league-tiebreak
    // Body: { teamNames: string[] }: the tied group whose unscored matches to remove.
    // Returns { deleted: number } on 200.
    // Throws on 404 (no matches found), 409 (any match already scored).
    async leagueTiebreakRemove(compID, teamNames, password) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak`, {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify({ teamNames }),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to remove league tie-breaker matches (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },

    // leagueTiebreakFinalize: POST /competitions/:id/league-tiebreak/finalize
    // Accepts the current standings as final without a tie-breaker. Sets the
    // LeagueTiebreakFinalized flag so MaybeAutoCompletePools can transition.
    // Returns { finalized: true } on 200.
    // Throws on 404 (comp not found), 409 (already complete).
    async leagueTiebreakFinalize(compID, password) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak/finalize`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to finalise league standings (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },

    // -------------------------------------------------------------------------
    // F2/F3: reconnectEvents: force a fresh SSE connection.
    // Closes the current shared source (if any), resets the reconnect backoff
    // counter, and calls _ensureConnected(). Safe to call repeatedly: the
    // double-connect guard in _ensureConnected() prevents duplicate sources.
    // Exposed as API.reconnectEvents / window.API.reconnectEvents so other
    // modules loaded as window globals can reach it without an ES import.
    // -------------------------------------------------------------------------
    reconnectEvents() {
        if (_sharedSource) {
            _sharedSource.close();
            _sharedSource = null;
            _disarmWatchdog();
        }
        if (_retryTimer) { clearTimeout(_retryTimer); _retryTimer = null; }
        _reconnectAttempt = 0;
        _ensureConnected();
    },

    // -------------------------------------------------------------------------
    // F5: hasPendingTerminalWrite: returns true when the queue holds a TERMINAL
    // entry for the given (compID, matchID) score/decision key.
    // The score editor uses this to show a "not yet saved / retry" indicator
    // when a completed score or decision is waiting for connectivity.
    // Exposed as API.hasPendingTerminalWrite / window.API.hasPendingTerminalWrite.
    // -------------------------------------------------------------------------
    hasPendingTerminalWrite(compID, matchID) {
        const descriptor = _writeQueue.get(_revKey(compID, matchID));
        return !!(descriptor && descriptor.terminal);
    },

    // mp-gpra (security): clearQueue: drop all queued writes (in-memory + the
    // persisted bc_write_queue) and reset sync state. Called on logout /
    // password_reset so a stale plaintext password and any pending writes don't
    // linger in localStorage past credential revocation: bringing bc_write_queue
    // to the same lifecycle as bc_password. Pending offline writes are discarded:
    // on a password_reset they would 401 on retry anyway, and logout is explicit.
    clearQueue() {
        // Bump the generation FIRST so an in-flight _flushQueue loop (mid-await on
        // a network write) aborts instead of sending the rest of its snapshot with
        // the now-revoked password.
        _queueGen++;
        _writeQueue.clear();
        try { localStorage.removeItem(QUEUE_STORAGE_KEY); } catch (_e) { /* ignore */ }
        if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
        _flushAttempt = 0;
        _offlineFlag = false;
        _recomputeSyncStatus();
    },
};

export { API, subscribeSyncStatus, subscribeTerminalWriteFailed, subscribeBracketResync, enqueueRunningWrite };

if (typeof window !== 'undefined') {
    window.API = API;
    // C2: expose sync-status pub/sub so components loaded as window.* globals
    // (admin_scoring_modal.jsx, etc.) can subscribe without an ES import.
    window.subscribeSyncStatus = subscribeSyncStatus;
    // mp-gpra: terminal-write failure pub/sub: lets the score editor show an
    // explicit "not saved" state when a queued terminal write is permanently dropped.
    window.subscribeTerminalWriteFailed = subscribeTerminalWriteFailed;
    // mp-y3nk: bracket-resync pub/sub: signals AdminShiaijo to refetch when a
    // queued override the server LWW-dropped leaves stale optimistic bracket state.
    window.subscribeBracketResync = subscribeBracketResync;
    // mp-gpra: reconnectEvents and hasPendingTerminalWrite are on window.API
    // (via the API object above): no separate window assignments needed.
}
