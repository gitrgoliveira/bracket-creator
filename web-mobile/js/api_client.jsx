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
// Empty-body methods (overridePoolRank, overrideBracketWinner,
// resetOverrides, updateMatchTime, moveMatchCourt, updateSchedule,
// deleteCompetition) deliberately return `true`
// rather than `res.json()` — calling res.json() on a 200/204 with no
// body throws SyntaxError per the Fetch spec, which used to surface as
// "alert: Unexpected end of JSON input" right after a successful save.
// See __tests__/api.test.jsx for the regression coverage.

import { normalizeCompetitionDetail, normalizePlayer, toBackendMatchResult, buildPlayerMetadata } from './api_serializers.jsx';

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

const _matchRevCounters = new Map(); // compID:matchID → int
function _nextRev(compID, matchID) {
    const key = _revKey(compID, matchID);
    const next = (_matchRevCounters.get(key) || 0) + 1;
    _matchRevCounters.set(key, next);
    return next;
}

// Per-page-load scoring-session id. Identifies one client session so the
// server's rev-guard can drop a single session's own out-of-order delivery
// (a reconnect flush). Different sessions are concurrent operators — the
// server treats those as last-write-wins. jsdom-safe fallback.
const _revSession = (typeof crypto !== 'undefined' && crypto.randomUUID)
    ? crypto.randomUUID()
    : `s${Math.random().toString(36).slice(2)}`;

// ---------------------------------------------------------------------------
// C2: Offline write queue
// ---------------------------------------------------------------------------
// Holds the latest pending "running" score write per match (last-write-wins).
// On any network failure the write is queued here and retried on reconnect.
// The queue never accumulates: a new write for the same match replaces any
// existing entry, so only the latest state is ever flushed.
//
// Sync-status state is published via _syncStatus / _syncListeners so the UI
// (SyncStatusPill) can render synced / syncing / offline without coupling to
// any Preact component tree.
// ---------------------------------------------------------------------------

/**
 * @typedef {'synced'|'syncing'|'offline'} SyncStatusValue
 */

/** @type {Map<string, {compID: string, matchID: string, payload: object, password: string}>} */
const _writeQueue = new Map(); // compID:matchID → pending write descriptor
let _syncStatus = /** @type {SyncStatusValue} */ ('synced');
const _syncListeners = new Set();

// Count of in-flight running writes (online path). Drives the syncing pill
// even when the queue is empty (i.e. the write succeeded and was removed from
// the queue but the fetch hasn't resolved yet).
let _inflightRunning = 0;
// Set to true ONLY when a flush attempt ends with at least one true NETWORK
// failure (fetch rejected — connection down). A non-2xx server response (e.g. a
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

/**
 * Recompute and publish the correct sync status from current state:
 *   offline  — _offlineFlag is set and the queue still has entries
 *   syncing  — any in-flight running write OR the queue is non-empty
 *   synced   — otherwise
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
// Single-in-flight guard. _flushQueue's body awaits network I/O, so without a
// lock a second trigger (a rapid enqueue, an `online` event, or a backoff timer
// firing mid-flush) would start an OVERLAPPING loop iterating the same snapshot
// — duplicate PUTs and corrupted backoff state. Instead, a trigger that arrives
// while a flush is running sets _flushRequested; the running loop reruns once it
// finishes so newly-queued or still-failed writes are retried without overlap.
let _flushInProgress = false;
let _flushRequested = false;

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
                _recomputeSyncStatus();
                continue;
            }
            _recomputeSyncStatus();
            // Snapshot the current entries. Each descriptor is the object reference
            // stored in the map at the time of the snapshot. After an await, we check
            // identity before deleting so a newer write (a fresh object literal set
            // under the same key by enqueueRunningWrite) is not accidentally removed.
            const entries = [..._writeQueue.entries()];
            let anyFailed = false;      // any failure → keep in queue + backoff
            let networkFailed = false;  // fetch rejected → connection down → "offline"
            for (const [key, descriptor] of entries) {
                const { compID, matchID, payload, password } = descriptor;
                try {
                    const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/score`, {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
                        body: JSON.stringify(payload),
                    });
                    if (res.ok) {
                        // Success (HTTP 200, including a stale {stale:true} no-op) — remove
                        // from queue only if no newer write has replaced this descriptor.
                        if (_writeQueue.get(key) === descriptor) _writeQueue.delete(key);
                    } else if (res.status >= 500 || res.status === 429) {
                        // Transient server error — server is up but erroring; keep in
                        // queue and retry with backoff, but this is NOT "offline".
                        anyFailed = true;
                    } else {
                        // Non-retryable response (4xx): 409 conflict (ineligible_competitor
                        // / court_busy / side_mismatch / result_finalized), 400 validation,
                        // 401/403 auth, 413, etc. This queued running autosave can never
                        // succeed, so discard rather than retry forever and wedge the pill
                        // on "Syncing…" — the operator's explicit Finish is authoritative.
                        // Log for devtools visibility.
                        res.json().then((body) => {
                            console.warn(`[sync] queued running write rejected (${res.status}):`, body);
                        }).catch(() => {});
                        if (_writeQueue.get(key) === descriptor) _writeQueue.delete(key);
                    }
                } catch (_) {
                    // fetch rejected — the network is down.
                    anyFailed = true;
                    networkFailed = true;
                }
            }
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
 * is replaced. Triggers an immediate flush attempt; on failure backs off and
 * retries automatically.
 */
function enqueueRunningWrite(compID, matchID, payload, password) {
    _writeQueue.set(_revKey(compID, matchID), { compID, matchID, payload, password });
    _recomputeSyncStatus();
    if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
    // A fresh user write resets the backoff counter so it doesn't inherit a
    // stale max-backoff delay from a prior failure run (mirrors the online handler).
    _flushAttempt = 0;
    _flushQueue();
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
// Shared ref-counted SSE singleton
// ---------------------------------------------------------------------------
// All subscribeToEvents callers share ONE EventSource connection. A Set of
// subscriber records ({callback, onStatus}) is maintained; when the last
// subscriber unsubscribes the source is closed and nulled out.
// ---------------------------------------------------------------------------

/** @type {EventSource|null} */
let _sharedSource = null;
/** @type {ReturnType<typeof setTimeout>|null} */
let _retryTimer = null;
/** @type {Set<{callback: Function, onStatus: Function|undefined}>} */
const _subscribers = new Set();

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
    // Guard: the 5s retry timer may fire after all subscribers have unsubscribed
    // (clearTimeout only cancels a pending callback — it cannot stop one that is
    // already queued in the event loop). No-op when no one is listening so we
    // don't open a zombie EventSource that can never be closed.
    if (_subscribers.size === 0) return;
    // Whoever calls this is establishing the connection NOW, so cancel any
    // pending reconnect timer — otherwise a retry queued by a previous onerror
    // can fire later and open a second EventSource (a leaked SSE connection).
    if (_retryTimer) {
        clearTimeout(_retryTimer);
        _retryTimer = null;
    }
    // Guard against double-connect: only one shared source may exist at a time.
    if (_sharedSource) return;
    // Bind handlers to THIS instance via a local const. Each handler ignores
    // events from a superseded source (`source !== _sharedSource`) so a stale
    // instance — should one ever fire after being replaced — can't close the
    // live connection or fan out a false status/message to current subscribers.
    const source = new EventSource('/api/events');
    _sharedSource = source;

    source.onopen = () => {
        if (source !== _sharedSource) return;
        _fanOutStatus('open');
    };

    source.onmessage = (event) => {
        if (source !== _sharedSource) return;
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
        _fanOutStatus('error');
        if (_subscribers.size > 0) {
            // Clear any prior timer before re-arming so repeated error events
            // can't queue multiple concurrent reconnects.
            if (_retryTimer) clearTimeout(_retryTimer);
            _retryTimer = setTimeout(_ensureConnected, 5000);
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
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch tournament (Status ${res.status})`);
        }
        return res.json();
    },
    // Public endpoint — returns {mode: "file"|"locked", resetEnabled: bool}.
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
    // Reset the tournament password. Unauthenticated by design — the
    // server enforces "is this endpoint enabled" via the verifier's
    // ResetEnabled() (locked mode returns 404). Throws on non-2xx so
    // the caller can surface the server's error message (including the
    // 404 "reset disabled" case if the SPA's cached authConfig was
    // stale).
    //
    // `originatorId` is a per-tab client ID echoed back on the SSE
    // password_reset event payload so the originating tab can ignore
    // its own broadcast and avoid clobbering the just-written
    // localStorage credential. Optional — when absent the server
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
        // Backend returns 204 No Content — calling .json() on an empty
        // body throws SyntaxError per the Fetch spec (same pattern as
        // overridePoolRank etc. above).
        return true;
    },
    // Set or rotate the elevated (destructive-ops) admin password (spec 004
    // / mp-e21). File mode only — locked mode 404s (credential is the
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
        return comps.map(item => {
            const c = item.config || item;
            const norm = {
                ...c,
                poolMatches: item.poolMatches,
                bracket: item.bracket,
                players: (c.players || []).map(normalizePlayer),
            };
            // Run normalization on the matches as well
            return normalizeCompetitionDetail(norm);
        });
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
    // env-var password to authorize even the initial POST — pass
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
                    // object without danGrade must be passed through unchanged — if we
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
    // Subscribe to the server's SSE event stream. `callback` is fired
    // for each parsed event. `onStatus` (optional) is fired with
    // 'open' when the EventSource transitions to open and 'error' when
    // it transitions away — used by the display surfaces (FR-011
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
            // No source yet — open one. The new subscriber keeps its optimistic
            // default until this source's onopen/onerror fires.
            _ensureConnected();
        } else if (_sharedSource.readyState === EventSource.OPEN) {
            // Source is already connected — replay 'open' to this subscriber.
            _emitStatus(record, 'open');
        }
        // Else the source exists but is still CONNECTING (a non-null source is
        // only ever CONNECTING or OPEN — we null it on close). Replay NOTHING
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
                if (_sharedSource) {
                    _sharedSource.close();
                    _sharedSource = null;
                }
            }
        };
    },
    async recordScore(compID, matchID, result, password, match) {
        const payload = toBackendMatchResult(result, match || result);

        // C2: stamp monotonic rev on running-status writes so the server's
        // rev-guard can drop out-of-order deliveries (e.g. from reconnect flush).
        // Completed writes do not need a rev — the guard is gated on status=running.
        const isRunning = payload?.status === 'running';
        if (!isRunning) {
            // A completed/terminal write supersedes any queued running autosave
            // for this match — drop it so a stale flush can't revert the result.
            if (_writeQueue.delete(_revKey(compID, matchID))) _recomputeSyncStatus();
            // Prune the per-match rev counter so it doesn't accumulate for the
            // page session across many matches/competitions.
            _matchRevCounters.delete(_revKey(compID, matchID));
        }
        if (isRunning) {
            payload.rev = _nextRev(compID, matchID);
            payload.revSession = _revSession;
            _inflightRunning++;
            _recomputeSyncStatus();
        }

        let res;
        try {
            try {
                res = await fetch(`/api/competitions/${compID}/matches/${matchID}/score`, {
                    method: 'PUT',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-Tournament-Password': password
                    },
                    body: JSON.stringify(payload)
                });
            } catch (_networkErr) {
                // Network failure (offline / no connection). For running-status writes,
                // queue for retry — last-write-wins semantics so only the latest state
                // is ever flushed. Completed writes are not queued because they MUST
                // be delivered; let callers handle the error with a toast.
                if (isRunning) {
                    enqueueRunningWrite(compID, matchID, payload, password);
                    // Return a synthetic "ok" shape so callers that ignore the return
                    // value (debounced autosave) don't crash. The queued write will
                    // deliver asynchronously via _flushQueue.
                    return { queued: true };
                }
                throw _networkErr;
            }
        } finally {
            if (isRunning) {
                _inflightRunning--;
                _recomputeSyncStatus();
            }
        }

        const data = await res.json().catch(() => ({}));
        if (res.ok) {
            // A stale running write is signalled by HTTP 200 {stale:true}
            // (the server's rev-guard no-ops it). Returned as-is; the
            // fire-and-forget autosave caller ignores the value.
            return data;
        }
        // A running write that failed with a RETRYABLE server error (5xx / 429)
        // is queued for offline-style retry. Without this the inflight counter
        // drops to 0 and the pill flips back to "Synced" even though the latest
        // score never reached the server (the autosave caller swallows the
        // throw). Queuing keeps the pill on syncing/offline and re-delivers the
        // latest state. Non-retryable 4xx are terminal (validation / conflict /
        // finalized) — they won't succeed on retry, so they surface to explicit
        // callers; the operator's authoritative Finish is the backstop.
        if (isRunning && (res.status >= 500 || res.status === 429)) {
            enqueueRunningWrite(compID, matchID, payload, password);
            return { queued: true };
        }
        // mp-dc52 Phase 3: the simultaneity gate returns 409 ineligible_competitor
        // with a human-readable reason; prefer reasonHuman, then reason, then code.
        if (data.error === "ineligible_competitor" || data.error === "already_ineligible") {
            throw new Error(data.reasonHuman || data.reason || data.error || "Failed to record score");
        }
        throw new Error(data.error || "Failed to record score");
    },
    // T093–T095: kiken / fusenpai / fusensho / daihyosen — server auto-fills
    // scoreline and Winner from {decision, decisionBy, encho}. Body shape is
    // defined by mobileapp.DecisionRequest in handlers_decision.go; decisionBy
    // MUST be "shiro" or "aka", decisionReason is optional and ≤200 chars.
    // Response is the updated state.MatchResult.
    async recordDecision(compID, matchID, body, password) {
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/decision`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to record decision");
        }
        return res.json();
    },
    // T098: competitor eligibility statuses produced by kiken/fusenpai
    // decisions. Endpoint is read-only and unauthenticated — same contract as
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
        const res = await fetch(`/api/competitions/${compID}/matches/${matchID}/override-winner`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ winnerName })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to override winner");
        }
        // Backend returns 200 with empty body — see overridePoolRank.
        return true;
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
        // Backend returns 204 No Content — .json() would reject.
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
    // TeamLineup for (compId, teamId, round) — 404 when no lineup has been
    // submitted yet, which the form treats as "blank, editable". PUT replaces
    // the lineup; the server rejects with 409 (ErrLineupLocked) once the
    // round's first match has gone running. DELETE clears the lineup so an
    // operator can revise pre-lock.
    async fetchTeamLineup(compID, teamId, round) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`);
        if (res.status === 404) return null;
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load lineup");
        }
        return res.json();
    },
    async putTeamLineup(compID, teamId, round, positions, password) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ teamId, competitionId: compID, round, positions })
        });
        if (!res.ok) {
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
    // place of the round key — successive encounters between the same
    // two teams each carry an independent, lockable lineup entry.
    // 404 → null (no lineup saved yet; form treats as blank/editable).
    // 409 ErrLineupLocked on PUT → the match is already running; surface
    // as a clear error (same pattern as round-scoped PUT).
    async fetchMatchLineup(compID, teamId, matchId) {
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`);
        if (res.status === 404) return null;
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load match lineup");
        }
        return res.json();
    },
    async putMatchLineup(compID, teamId, matchId, positions, password, force = false, reason = "") {
        if (force && !(reason && reason.trim())) {
            throw new Error("A change reason is required to override the lineup lock.");
        }
        const body = { teamId, competitionId: compID, matchId, positions, force };
        if (reason) body.changeReason = reason;
        const res = await fetch(`/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
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
    // caller — see TeamScoreEditorModal for the user-visible mapping.
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
    // match not found) or 409 (daihyosen already scored — clear scores first).
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
    // T190-T193 (US13 — Swiss format). Generate the next Swiss round.
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
    // T190-T193 (US13). Public endpoint — returns cumulative Swiss
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
    async toggleCheckIn(compID, pid, checkedIn, password) {
        const method = checkedIn ? 'PUT' : 'DELETE';
        const res = await fetch(`/api/competitions/${compID}/participants/${pid}/checkin`, {
            method,
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to ${checkedIn ? 'check in' : 'undo check-in'} participant`);
        }
        return res.json();
    },
    async addParticipant(compID, payload, password, adminPassword) {
        const res = await fetch(`/api/competitions/${compID}/participants`, {
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
        const res = await fetch(`/api/competitions/${compID}/participants/${pid}`, {
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
    // mp-scf: tournament branding — logo upload/delete.
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
    }
};

export { API, subscribeSyncStatus, enqueueRunningWrite };

if (typeof window !== 'undefined') {
    window.API = API;
    // C2: expose sync-status pub/sub so components loaded as window.* globals
    // (admin_scoring_modal.jsx, etc.) can subscribe without an ES import.
    window.subscribeSyncStatus = subscribeSyncStatus;
}
