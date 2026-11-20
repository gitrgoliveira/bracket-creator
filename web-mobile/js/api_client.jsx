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
// Empty-body methods (overridePoolRanks,
// updateMatchTime, moveMatchCourt,
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
//   F4: Write queue persisted to localStorage (no age limit, try/catch safe).
//   F5: Terminal writes (decision, completed score, lineup) durable via queue.

import {
    writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock, SUPERSEDED_REASON, SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE, isFinishedResultWrite,
} from './write_result.jsx';
// bc-evts S2: the build this page runs against the server's, and the reload rule.
import { setOutboxIdleProbe, setBeforeReload } from './app_version.jsx';
import { LANDED_TAB_BEAT_MS } from './landed_taps.jsx';
import { TOLD_TAPS_PREFIX } from './told_taps.jsx';
import { MINTED_MEMBERS_KEY } from './minted_members.jsx';
import { normalizeViewerCompItem, START_CHECK_TIMEOUT_MS } from './api_client_requests.jsx';
import {
    _clockOffsetLearned, _learnServerClockOffset, _relearnClockThrottled, _noteOwnStamp, _restampFor,
} from './api_client_clock.jsx';
import {
    QUEUE_STORAGE_KEY, LEGACY_QUEUE_STORAGE_KEY, subscribeQueueAlert, _notifyQueueAlert, _persistQueue,
    _writeQueue, _queueSet, _markSettled,
} from './api_client_queue.jsx';
import { _statusEntriesByBase } from './api_client_overlays.jsx';
import { _onToldTapsStored, _landed, _landedBeat } from './api_client_taps.jsx';
import {
    subscribeTerminalWriteFailed, _recomputeSyncStatus, subscribeUnsentWrites, subscribeSyncStatus,
} from './api_client_status.jsx';
import {
    _resetFlushBackoff, _flushQueue, _outboxIdle, _rerunFlushSoon, _wakeLeaseWaiters, _baseLease,
} from './api_client_flush.jsx';
import { BASE_LEASE_WAKE_KEY } from './base_sender_lease.jsx';
import {
    _migratedLegacyLineup, _storedEntryProblem, enqueueRunningWrite, _withoutDownstreamConfirmation,
    _onStoredQueueChanged, _takeOverLegacyQueue,
} from './api_client_enqueue.jsx';
import { _announceKeptMember, _learnDropped } from './api_client_members.jsx';
import { eventsApi } from './api_client_events.jsx';
import { tournamentApi } from './api_client_tournament.jsx';
import { competitionsApi } from './api_client_competitions.jsx';
import { matchesApi } from './api_client_matches.jsx';
import { teamsApi } from './api_client_teams.jsx';
import { outboxApi } from './api_client_outbox.jsx';
import { _minted } from './api_client_store.jsx';
export { subscribeLocalMatchPatch, subscribeLocalParticipantPatch } from './api_client_overlays.jsx';

if (typeof setInterval === 'function') {
    const beat = setInterval(_landedBeat, LANDED_TAB_BEAT_MS);
    if (beat && typeof beat.unref === 'function') beat.unref();
}
if (typeof window !== 'undefined' && typeof window.addEventListener === 'function') {
    // A page that goes gives up the bases it was sending, so another tab sends
    // them at once (base_sender_lease.jsx).
    window.addEventListener('pagehide', () => { _landed.leave(); _baseLease.leave(); });
}
// A hidden page's timers slow or stop: it beats as it is hidden and shown, so
// the other tabs read it alive for longer while hidden (stage 6 review 9, F6),
// and a page shown again tells at once what it may tell now, a hidden tab's
// taps included (stage 6 review 10, M1).
if (typeof document !== 'undefined' && typeof document.addEventListener === 'function') {
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'hidden') _landed.beat({ hidden: true });
        else {
            _landed.beat({ hidden: false });
            _landedBeat();
        }
    });
}

// Remove-then-add for the same reason as the online handler below.
if (typeof window !== 'undefined') {
    if (window.__bcStoredQueueHandler) window.removeEventListener('storage', window.__bcStoredQueueHandler);
    const onStorage = (e) => {
        if (e.key === QUEUE_STORAGE_KEY) _onStoredQueueChanged(e.oldValue, e.newValue);
        // Another tab sent all it held of a base: send what is left of it, if this
        // tab was turned away from it.
        if (e.key === BASE_LEASE_WAKE_KEY) _wakeLeaseWaiters();
        if (typeof e.key === 'string' && e.key.startsWith(TOLD_TAPS_PREFIX)) _onToldTapsStored(e.key, e.oldValue, e.newValue);
        // Another tab learned something of a minted member: a member that
        // never was comes off this tab's writes, and a write held back for one
        // may now go (minted_members.jsx).
        if (e.key === MINTED_MEMBERS_KEY) {
            const { dropped, kept, unblocks } = _minted.onStored(e.oldValue, e.newValue);
            for (const m of kept) _announceKeptMember(m);
            if (dropped.length > 0) _learnDropped(dropped);
            else if (unblocks) _rerunFlushSoon();
        }
        // An older tab wrote what it queues under the old key: take it over (F4).
        if (e.key === LEGACY_QUEUE_STORAGE_KEY && e.newValue && _takeOverLegacyQueue(e.newValue)) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    };
    window.__bcStoredQueueHandler = onStorage;
    window.addEventListener('storage', onStorage);
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
        // bc-cse: reconnect is also the moment an OS is most likely to have
        // NTP-jumped this device's clock (a tablet that slept through a wifi gap
        // resyncs on the way back). The SSE onopen relearn only helps if SSE
        // actually opens, which on a court that has just come back is neither
        // immediate nor guaranteed - so relearn here too, and do it BEFORE the
        // flush and regardless of whether anything is queued: the writes at risk
        // are the live ones the operator makes in the next few seconds.
        // Throttled, so a flapping connection costs one GET per 5s at worst.
        _relearnClockThrottled();
        if (_writeQueue.size > 0) {
            _resetFlushBackoff();
            _flushQueue();
        }
    };
    window.__bcOnlineFlushHandler = onlineHandler;
    window.addEventListener('online', onlineHandler);
}

// ---------------------------------------------------------------------------
// F4: Rehydrate write queue from localStorage on module load
// ---------------------------------------------------------------------------
// Parse any entries persisted by a previous page session, whatever their age
// (bc-mrgc phase 3: the server orders an old write by its stamp and keeps what
// a newer change outranks in the match's history, so nothing is dropped for
// being old). A queue persisted by an earlier build (one entry per match, keyed
// by the match itself) loads as it is: its key is its own base (_baseOf).
// Valid entries are added to _writeQueue and an immediate flush is triggered
// so they are delivered as soon as the page has network. All localStorage access is wrapped in try/catch for
// private-browsing / quota safety.
// ---------------------------------------------------------------------------

// mp-y3nk: learn the server-clock offset at load so the first writes are already
// stamped in the server's frame (refreshed again on each SSE (re)connect).
//
// bc-cse: retry until the FIRST learn succeeds. A single fire-and-forget attempt
// leaves the offset at 0 for the whole session when the load-time GET fails and
// no SSE connection ever opens - and that precondition ("SSE never opens") is
// exactly why there is no other trigger to lean on. Backoff 5s -> 10s -> 20s ->
// 40s, capped at 60s, indefinitely: one tiny GET per minute is the steady state,
// and it stops the moment anything (here or the SSE-connect relearn) succeeds.
//
// The `typeof fetch` guard wraps the WHOLE chain, not just the first attempt, so
// a host without fetch schedules no timers at all. Note it does NOT keep the
// chain out of the unit suite: jsdom + node give `fetch` here, so the poll runs
// in any test that imports this module (see the clock-poll note in
// __tests__/sync_queue.test.jsx, whose fetch counts are scoped because of it).
const CLOCK_LEARN_RETRY_BASE_MS = 5000;
const CLOCK_LEARN_RETRY_MAX_MS = 60000;
if (typeof fetch === 'function') {
    (function _learnClockUntilSuccess(delayMs) {
        _learnServerClockOffset().finally(() => {
            if (_clockOffsetLearned) return;
            setTimeout(() => {
                // Re-check on FIRE, not just on schedule: an SSE-connect learn or a
                // supersede relearn can land while this timer is pending.
                if (_clockOffsetLearned) return;
                _learnClockUntilSuccess(Math.min(delayMs * 2, CLOCK_LEARN_RETRY_MAX_MS));
            }, delayMs);
        });
    })(CLOCK_LEARN_RETRY_BASE_MS);
}

;(function _rehydrateQueue() {
    try {
        const raw = localStorage.getItem(QUEUE_STORAGE_KEY);
        if (!raw) return;
        const entries = JSON.parse(raw);
        if (!Array.isArray(entries)) return;
        let anyLoaded = false;
        // bc-qttl: every drop below is COUNTED and reported. These entries are
        // finished operator work that never reached the server, and the previous
        // code discarded them with a bare `continue` - no log, no signal, nothing
        // the operator could ever notice. Counted terminal separately from
        // running, because "3 results were lost" and "3 writes were lost" are
        // different claims and only the terminal ones are a finished result.
        // The one class left is a corrupt entry: nothing is dropped for its age
        // (bc-mrgc phase 3).
        let unreadable = 0, unreadableTerminal = 0, migrated = false;
        // A dropped entry is marked settled, so the persist below takes it out
        // of storage: the merge otherwise keeps what this tab did not write.
        const dropUnreadable = (key, d) => {
            unreadable++;
            if (isFinishedResultWrite(d)) unreadableTerminal++;
            if (key) _markSettled(key, d);
        };
        for (const item of entries) {
            // Defensive: a single malformed element (corrupt/tampered storage) must
            // not throw and abort the whole rehydrate: destructuring a non-array in
            // the for-of header would do exactly that, dropping ALL valid queued
            // writes. Validate the tuple shape first and skip bad items individually.
            if (!Array.isArray(item) || item.length < 2) { dropUnreadable(null, null); continue; }
            const [key, descriptor] = item;
            if (!key || !descriptor || typeof descriptor !== 'object') { dropUnreadable(key, null); continue; }
            if (_storedEntryProblem(descriptor)) { dropUnreadable(key, descriptor); continue; }
            // Only restore if not already superseded by a same-session write.
            if (!_writeQueue.has(key)) {
                // A write a previous page left behind has already waited: it
                // is held, and its landing is told (the 'sent' alert).
                descriptor.held = true;
                // A confirmation an older build stored is never replayed: it
                // answered a dialog on a page that is gone.
                if (descriptor.payload && descriptor.payload.forceDownstreamReopen) {
                    descriptor.payload = _withoutDownstreamConfirmation(descriptor.payload);
                }
                // A match lineup an older build queued under its own base goes
                // onto its match's (R9); the old key leaves storage below.
                const moved = _migratedLegacyLineup(key, descriptor);
                const at = moved || key;
                if (moved) { _markSettled(key, descriptor); migrated = true; }
                if (!_writeQueue.has(at)) {
                    _queueSet(at, descriptor);
                    _noteOwnStamp(at, descriptor);
                    anyLoaded = true;
                }
            }
        }
        // Persist the pruned queue whenever anything was dropped. This is what makes
        // the alert below fires ONCE rather than on every reload forever: rehydrate
        // never used to write back, so a dropped entry stayed in localStorage
        // untouched, and each refresh would re-read, re-drop and re-announce it.
        // With the queue now empty _persistQueue() stores it as "[]".
        if (unreadable > 0 || migrated) _persistQueue();
        if (unreadable > 0) {
            console.warn(`[sync] discarded ${unreadable} unreadable queued write(s) on rehydration`);
            _notifyQueueAlert({ kind: 'unreadable', count: unreadable, terminalCount: unreadableTerminal });
        }
        if (anyLoaded) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    } catch (_e) { /* private-browsing / malformed JSON: silently skip */ }
}());

// After this build's own queue: what an older build left under the old key
// (F4). A key both hold keeps this build's entry.
;(function _takeOverLegacyQueueOnLoad() {
    try {
        if (_takeOverLegacyQueue(localStorage.getItem(LEGACY_QUEUE_STORAGE_KEY))) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    } catch (_e) { /* private-browsing: nothing to take over */ }
}());

// The members live in their domain pieces (bc-jsxs), each a plain object of
// methods: the tournament (api_client_tournament.jsx), the competitions
// (api_client_competitions.jsx), the matches (api_client_matches.jsx), the
// teams (api_client_teams.jsx), the outbox (api_client_outbox.jsx) and the
// event stream (api_client_events.jsx). No two share a key
// (api_client_pieces.test.jsx), so the spread loses none.
const API = {
    ...tournamentApi,
    ...competitionsApi,
    ...matchesApi,
    ...teamsApi,
    ...outboxApi,
    ...eventsApi,
};

export {
    API, START_CHECK_TIMEOUT_MS, subscribeSyncStatus, subscribeTerminalWriteFailed, subscribeQueueAlert,
    subscribeUnsentWrites,
    enqueueRunningWrite,
    writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock,
    SUPERSEDED_REASON, SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE,
    // Exported for tests only: the take-the-older rule is a safety property, and
    // reaching it through a full offline/refuse/reconnect fixture would test the
    // queue rather than the arithmetic. No production consumer outside this file.
    _restampFor,
    // Exported for tests only: whether a tap makes the status-move index again
    // shows only in its identity (stage 6 review 10, m5).
    _statusEntriesByBase,
    normalizeViewerCompItem,
};

// S2: a new build reloads the page only once the outbox is idle, and the
// queue is persisted first.
setOutboxIdleProbe(_outboxIdle);
setBeforeReload(() => { _persistQueue(); });

if (typeof window !== 'undefined') {
    window.API = API;
    // C2: expose sync-status pub/sub so components loaded as window.* globals
    // (admin_scoring_modal.jsx, etc.) can subscribe without an ES import.
    window.subscribeSyncStatus = subscribeSyncStatus;
    // bc-offl: the held-writes count, for the admin topbar (admin_shell.jsx is
    // script-tagged and cannot ES-import this script-tagged module).
    window.subscribeUnsentWrites = subscribeUnsentWrites;
    // mp-gpra: terminal-write failure pub/sub: lets the score editor show an
    // explicit "not saved" state when a queued terminal write is permanently dropped.
    window.subscribeTerminalWriteFailed = subscribeTerminalWriteFailed;
    window.subscribeQueueAlert = subscribeQueueAlert;
    // No `window` mirror for the not-landed predicates or their banner copy:
    // every consumer imports write_result.jsx directly. That module is
    // import-only (never script-tagged), so even a script-tagged surface can
    // import it without the double-module-eval hazard the mirrors existed to
    // dodge.
    // mp-gpra: reconnectEvents and hasPendingTerminalWrite are on window.API
    // (via the API object above): no separate window assignments needed.
}
