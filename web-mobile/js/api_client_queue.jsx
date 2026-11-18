// The write queue: its entries, keys and order, browser storage, settlement and the
// queue alerts.
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import { writeWasRefusedForClock, isFinishedResultWrite } from './write_result.jsx';
import { CLOCK_SKEW as TAP_CLOCK_SKEW } from './score_events.jsx';
import { mintedAddId } from './minted_members.jsx';
import { _randomId } from './api_client_ids.jsx';
import { _noteOwnStatusAnswer, _revertLocalFields } from './api_client_overlays.jsx';
import { _dropToldHere, _publishPendingTaps } from './api_client_taps.jsx';
import { _discardWhileOut } from './api_client_status.jsx';
import { _inFlight, _afterKeys, _rerunFlushSoon } from './api_client_flush.jsx';
import {
    _lastEnqueuePersisted, _withoutDownstreamConfirmation, _takeOverLegacyQueue,
} from './api_client_enqueue.jsx';
import { _minted, _droppedOf, _mintedSweep, _learnDropped, _mintedPrune } from './api_client_members.jsx';

// ---------------------------------------------------------------------------
// C2: Monotonic per-match revision counter
// ---------------------------------------------------------------------------
// Stamps "running" autosave writes so the server can drop out-of-order
// delivery from a reconnect flush. The counter is per (compID, matchID) and
// increases strictly monotonically within a page session. Completed writes
// do not use it (the guard is gated to running status on the server too).
// ---------------------------------------------------------------------------
/** Composite key for per-(compID, matchID) maps. Prevents collisions across competitions. */
export function _revKey(compID, matchID) { return `${compID}:${matchID}`; }

// ---------------------------------------------------------------------------
// C2 / F4 / F5: Offline write queue
// ---------------------------------------------------------------------------
// Holds every pending write, per match an ORDERED list (bc-mrgc phase 3,
// operator ruling 2026-10-03: "Nothing should be dropped. All events must be
// ordered."). On any network failure the write is queued here and retried on
// reconnect.
//
// One entry per write, each under its own key: `${base}${QUEUE_ENTRY_SEP}${id}`,
// where the BASE is the match the write is about (`compID:matchID` for a score
// or a decision, `lineup:…`). Each flush pass sends a base's entries
// strictly in the order of the stamp they carry (_entryStamp), bc-evts F1
// (which reversed bc-mrgc C2): the first entry of a base that does not settle
// in the pass (offline, a 5xx that keeps coming back, parked, held by an open
// fetch) ends THAT base's turn, so a write made after another on this device
// never reaches the server before it, and a new write made while an earlier
// one of its base is queued waits behind it. Bases go side by side, each in
// its own order, so one match's failing entry holds back no other match. The
// server still merges a match write group by group in stamp order and keeps
// whatever a newer change outranks in the match's history (bc-mrgc), so the
// order is a guarantee about this device, not the only thing deciding. The ONE
// coalescing rule (_coalesceTarget): a score write takes the place of the
// match's LAST entry only when that entry is a score write no newer than it,
// and a running write only of a running one, claiming the groups both changed
// (union, as bc-mrgc phase 2 does); a lineup save joins the queued save of the
// same lineup, so a lineup has ONE entry (lineup_save.jsx: the union of the
// positions the two changed, the later value standing where both changed one),
// because a lineup PUT carries no stamp the server could order it by. Nothing
// else replaces anything: a running write never replaces a queued Finish or
// a decision, and a decision and a score for the same match are two entries.
// A key persisted by an earlier build (one entry per match, keyed by the base
// itself) is a valid entry of this shape: its base is the key, so an old queue
// loads and replays as it is.
//
// F4: The queue is persisted to localStorage on every change so that a page
// reload or crash during a wifi gap does not lose unsaved scores/decisions.
// Nothing is dropped for its age: an old entry is sent like any other, and the
// server orders it by its stamp, applying what is still the latest change and
// keeping what a newer change outranks in the match's history (it used to be
// discarded after 12 h, announced, but gone).
//
// F5: Terminal writes (completed score, decision, lineup) use the same queue
// with additional descriptor fields: kind, terminal, method, url.
//
// Sync-status state is published via _syncStatus / _syncListeners so the UI
// (the topbar's held-writes count, the editors' pending banners) can follow
// synced / syncing / offline without coupling to any Preact component tree.
// ---------------------------------------------------------------------------

/**
 * @typedef {'synced'|'syncing'|'offline'|'auth-required'|'server-error'} SyncStatusValue
 */

/**
 * @typedef {'score'|'decision'|'lineup'} WriteKind
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
 *   enqueuedAt      : Date.now() at enqueue time (the outbox's "which copy is
 *                     newer" test, and the order of an entry with no stamp)
 *   perfAtEnqueue   : performance.now() at enqueue time (bc-cse). SESSION-LOCAL
 *                     and deliberately NOT persisted - it is measured from this
 *                     document's time origin, so it is meaningless once
 *                     rehydrated. Absent means the clock-skew re-stamp uses the
 *                     wall-clock reconstruction alone; see _restampFor.
 *   attempts        : count of server rejections (5xx/429/403) on flush retries;
 *                     absent until the first rejection. Network failures never
 *                     increment it. At SERVER_REJECTION_NOTICE_THRESHOLD the
 *                     operator is told; the entry is never dropped for it.
 *   skewRetried     : true once this entry has been re-stamped and retried after
 *                     a clock_skew refusal (bc-cse). ABSENT means not-yet-retried,
 *                     which is the safe default for an entry persisted by an
 *                     older build. A clock_skew refusal on a marked entry is
 *                     never a drop: the server logged nothing. The entry is
 *                     re-stamped again (a freshly learned offset) and kept
 *                     queued and held (`clockRefused`, _keepForClock), retried
 *                     on the backoff until it lands or an operator discards it.
 *   clockRefused    : true once a marked entry was refused for clock_skew again
 *                     (_keepForClock): it reads as a write the server keeps
 *                     refusing (_refusedByServer), so it is held and
 *                     discardable, and the operator was told once.
 *   authBlocked     : true once the server answered 401. The entry stays QUEUED
 *                     but is skipped by the flush loop, because it carries the
 *                     password it was enqueued with and can only reproduce the
 *                     same 401 until API.resumeAfterAuth() re-stamps it. Persisted
 *                     with the entry, so a parked write survives a reload and is
 *                     resumed when the operator next authenticates.
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
 *   skewRetried?: boolean,
 *   clockRefused?: boolean,
 * }} WriteDescriptor
 * `method`/`url` are present only on terminal entries; running entries omit them.
 */

// The outbox's own key (bc-evts stage 3 review 3, F4). It was 'bc_write_queue',
// which a tab of an older build open across the upgrade shares: that tab read
// this build's entries (tap events it does not know), rewrote them into its own
// score writes and pruned the shared key on a 4xx, so new taps went out of
// order or were lost. What an older build leaves under the old key is taken
// over (_takeOverLegacyQueue), on load and whenever an older tab writes it.
export const QUEUE_STORAGE_KEY = 'bc_write_queue_v2';
export const LEGACY_QUEUE_STORAGE_KEY = 'bc_write_queue';

// Defense-in-depth allowlist for outbox replay. Every entry the queue holds (a
// score write, a decision, a tap, a reopen, a send back, a court
// move, a time change, a lineup save or removal, a member add, rename or
// cleared name, a reinstatement, a chusen's order, a representative bout's add
// or remove, a kachinuki bout's removal, a check-in, a fighting-spirit awards
// save) is a PUT, POST or DELETE to /api/competitions/…, and a DELETE only for
// the doors named below, or one of the four doors named exactly outside it (a
// court call, its withdrawal, the clear, the tournament settings), so a
// tampered or corrupted stored entry can't be flushed into some other endpoint
// or HTTP method. Used by both rehydrate (load) and flush, kept in one place so
// the two checks can't drift apart.
const _ALLOWED_QUEUE_METHODS = ['PUT', 'POST', 'DELETE'];
export function _isAllowedTerminalRequest(method, url) {
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
    if (u.origin !== origin) return false;
    // bc-obox: the doors outside /api/competitions/… are named by their exact
    // path and method, never a prefix: a court call sent, a court call
    // withdrawn, every court call cleared, and the tournament settings saved.
    // POST /api/tournament/reset and /api/tournament/import sit beside them and
    // are never replayed.
    const outside = _OUTBOX_DOORS_OUTSIDE_COMPETITIONS.find(([re]) => re.test(u.pathname));
    if (outside) return outside[1] === method;
    if (!u.pathname.startsWith('/api/competitions/')) return false;
    // A DELETE is queued for five doors: the representative bout's remove, a
    // kachinuki bout's removal (bc-evts), a team member's name cleared, a
    // match's own lineup removed (review 2, R1) and a check-in taken back
    // (bc-obox). A reset of the pool-rank overrides is not one: no shipped
    // build queued it (only builds of this change between stage 3 reviews 11
    // and 12 did), so no stored entry needs to replay it, and the server's
    // DELETE overrides stays a repair door (stage 6 review 11, item 11 b).
    if (method === 'DELETE') {
        return /\/(daihyosen|kachinuki-bout)$/.test(u.pathname)
            || /\/teams\/[^/]+\/(members|match-lineups)\/[^/]+$/.test(u.pathname)
            || /^\/api\/competitions\/[^/]+\/participants\/[^/]+\/checkin$/.test(u.pathname);
    }
    return true;
}
// _replayBody: what a replayed entry sends. A save of the tournament settings
// never carries a password, whatever storage holds (a tampered entry must not
// rotate it): an empty one keeps the stored password. Judged on the request
// itself, not the entry's kind, which storage holds too.
export function _replayBody(method, url, payload) {
    if (method === 'PUT' && payload && typeof payload === 'object') {
        const origin = (typeof location !== 'undefined' && location.origin) || 'http://localhost';
        let path = '';
        try { path = new URL(url, origin).pathname; } catch (_e) { path = ''; }
        if (path === '/api/tournament') return JSON.stringify({ ...payload, password: '' });
    }
    return JSON.stringify(payload);
}
// The outbox doors outside /api/competitions/…, each [exact path, method].
const _OUTBOX_DOORS_OUTSIDE_COMPETITIONS = [
    [/^\/api\/tournament\/announce$/, 'POST'],
    [/^\/api\/announcements$/, 'DELETE'],
    [/^\/api\/announcements\/[^/]+$/, 'DELETE'],
    [/^\/api\/tournament$/, 'PUT'],
];

// ---------------------------------------------------------------------------
// F4: localStorage helpers (all accesses wrapped in try/catch for
// private-browsing / storage-quota safety: mirroring app.jsx pattern).
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// bc-qttl: queue-alert channel
// ---------------------------------------------------------------------------
// A queued write that will never reach the server must be ANNOUNCED, not
// silently discarded. Deliberately SEPARATE from _terminalFailListeners, which
// is addressed to one open editor (its subscribers filter on compID/matchID)
// and is therefore useless for the two cases that matter here: a drop detected
// during rehydration, when no editor has mounted yet, and a drop on a match the
// operator is not currently looking at.
//
// Alerts raised before any listener exists are BUFFERED and replayed to the
// first subscriber. This is load-bearing, not a nicety: the rehydrate IIFE runs
// at module load, long before App mounts, so without the buffer the rehydration
// alert (the whole point of bc-qttl) would publish into an empty Set and vanish
// exactly like the writes it is reporting.
//
// Alert shape: {kind, count, terminalCount, detail?, compID?, matchID?}
//   'unreadable'    dropped: corrupt/tampered entry that can never be replayed
//   'rejected'      the server permanently refused it (non-retryable 4xx)
//   'server_error'  the server keeps failing it; STILL QUEUED, still retrying
//   'auth_required' parked pending re-authentication; still queued
//   'storage_full'  the queue could not be persisted; a reload would lose it
//   'discarded'     dropped wholesale on credential revocation (password reset)
//   'sent'          GOOD news (bc-offl): held finished results landed in one
//                   flush; the one kind App renders as a success toast
//   'displaced'     held finished results landed and moved a later change of
//                   their match to its history; recorded, told once.
// ---------------------------------------------------------------------------
const _queueAlertListeners = new Set();
const _pendingQueueAlerts = [];
export function subscribeQueueAlert(fn) {
    _queueAlertListeners.add(fn);
    // Replay anything raised before this subscriber existed, then clear: the
    // buffer is a handoff to the first listener, not a durable log.
    if (_pendingQueueAlerts.length > 0) {
        const drained = _pendingQueueAlerts.splice(0, _pendingQueueAlerts.length);
        for (const alert of drained) {
            try { fn(alert); } catch (_e) { /* swallow */ }
        }
    }
    return () => _queueAlertListeners.delete(fn);
}
export function _notifyQueueAlert(alert) {
    if (_queueAlertListeners.size === 0) { _pendingQueueAlerts.push(alert); return; }
    for (const fn of _queueAlertListeners) {
        try { fn(alert); } catch (_e) { /* swallow */ }
    }
}

// Count of queued entries that represent a FINISHED result (completed score,
// decision, lineup). Alerts report this separately from the raw queue size
// because "3 results were lost" and "3 writes were lost" are different claims
// to an operator, and only the terminal ones are finished work.
export function _countTerminalQueued() {
    // Finished results only (isFinishedResultWrite, R5): an entry's `terminal`
    // says how it is sent, not that it is a result.
    let n = 0;
    for (const d of _writeQueue.values()) { if (isFinishedResultWrite(d)) n++; }
    return n;
}

// Latched so a wedged quota announces once per failure run rather than on every
// keystroke-driven autosave. Reset on the first successful persist.
let _storageFullAnnounced = false;

// True while browser storage still holds a write this tab settled, because
// the write that takes it out was refused: the outbox is not idle then
// (_outboxIdle), or a reload would read the write back and send it again
// (stage 3 review 9, P2). Cleared by the next persist storage takes.
export let _settledLeftStored = false;

/**
 * Persist the queue. Returns true when the queue's current state is safely on
 * disk, false when it could not be written.
 *
 * The stored queue is shared by every tab of this origin, so a persist MERGES
 * into what storage holds rather than overwriting it: this tab's entries are
 * written into it (a key both hold keeps the newer write), the entries this
 * tab settled since the last persist are taken out of it (_settled), and
 * anything else, another tab's queued writes, is left alone. A whole-map overwrite dropped the other tab's entries, and an
 * empty queue removed the key under them, so a result queued in one tab was
 * lost when another tab saved or reloaded (bc-sync). An emptied queue is
 * stored as "[]"; only clearQueue's wipe removes the key (P1, below).
 *
 * bc-qttl: the failure is no longer swallowed. A queue that cannot persist is a
 * queue that a reload will silently lose, which is precisely the durability
 * promise the outbox exists to make: with no size cap, a long offline run can
 * reach the localStorage quota, and every unsent result then died on the next
 * refresh with nothing shown to the operator.
 * @returns {boolean}
 */
export function _persistQueue() {
    // Storage still holds a write this tab settled: if the write that takes
    // it out is refused, a reload would read it back and send it again (P2).
    let settledStillStored = false;
    try {
        const merged = _storedQueue();
        for (const [k, at] of _settled) {
            const stored = merged.get(k);
            // A newer entry another tab wrote under the same key since is theirs.
            if (stored && !_enqueuedAfter(stored, at)) {
                merged.delete(k);
                settledStillStored = true;
            }
        }
        // A copy of an entry this tab settled earlier that another tab stored
        // back since, no newer, is the stale persist of a tab that had not seen
        // it leave: nothing holds it any more, so a reload would send it over
        // what came after it (one sender per base, base_sender_lease.jsx, no
        // longer leaves that tab to send and clear it itself).
        for (const [k, stored] of merged) {
            if (!_writeQueue.has(k) && _wasSettledHere(k, stored)) merged.delete(k);
        }
        // A key both hold keeps the newer write. This tab's copy can be one it
        // took from storage (on load, or from another tab), and writing it back
        // over a later write the other tab stored since, such as a queued
        // Finish replacing an autosave, would drop that write from storage:
        // lost, if that tab then closed or reloaded before sending it.
        for (const [k, d] of _writeQueue) {
            if (!_enqueuedAfter(merged.get(k), d.enqueuedAt)) merged.set(k, d);
        }
        // A settled member record nothing names any more is forgotten.
        _mintedPrune(merged);
        if (merged.size === 0) {
            // An emptied queue is stored as "[]", never removed: the other
            // tabs read the removal of the key as a wipe (clearQueue), which
            // leaves each its own writes, so a tab whose write this one sent
            // would keep it and send it again later over a newer one (stage 3
            // review 9, P1). "[]" tells them every write they shared is gone.
            // Only a key that is there: after clearQueue's wipe, a flush still
            // out must not store the queue again.
            if (localStorage.getItem(QUEUE_STORAGE_KEY) !== null) localStorage.setItem(QUEUE_STORAGE_KEY, '[]');
            _settled.clear();
            _settledLeftStored = false;
            _storageFullAnnounced = false;
            return true;
        }
        // perfAtEnqueue is SESSION-LOCAL and must not be serialized: it is
        // measured from this document's time origin, so persisting it would let
        // a reload (or a second tab reading the same key) subtract two readings
        // taken from different zeros and derive a nonsense age. Stripped here
        // rather than at the read side so there is one place to reason about,
        // and so an entry written by an older build behaves identically to a
        // rehydrated one: no anchor, wall-clock reconstruction only.
        // A downstream confirmation (forceDownstreamReopen) is the operator's
        // answer to a dialog on THIS page: it is sent, but never stored, so a
        // reload or another tab never replays a confirmation nobody gave.
        const entries = [...merged.entries()].map(
            ([k, d]) => [k, {
                ...d, perfAtEnqueue: undefined,
                ...(d.payload && d.payload.forceDownstreamReopen ? { payload: _withoutDownstreamConfirmation(d.payload) } : {}),
            }]
        );
        localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify(entries));
        // Only once the write has landed: a failed one must still prune these
        // entries next time, or a settled write would come back from storage.
        _settled.clear();
        _settledLeftStored = false;
        _storageFullAnnounced = false;
        return true;
    } catch (_e) {
        // What it took out stays in _settled for the next persist. While
        // storage still holds any of it the outbox is not idle (_outboxIdle),
        // so neither Reload nor the automatic reload goes and reads it back
        // (stage 3 review 9, P2).
        _settledLeftStored = settledStillStored;
        // Storage refused, but this tab holds nothing to store: nothing of
        // its can be lost, so it raises nothing, and answers stored unless a
        // write it settled is still there (L2, stage 3 review 8: a reload
        // must not wait on storage an empty outbox does not need).
        if (_writeQueue.size === 0) return !settledStillStored;
        if (!_storageFullAnnounced) {
            _storageFullAnnounced = true;
            _notifyQueueAlert({
                kind: 'storage_full',
                count: _writeQueue.size,
                terminalCount: _countTerminalQueued(),
                detail: 'browser storage is full',
            });
        }
        return false;
    }
}

/** @type {Map<string, WriteDescriptor>} */
export const _writeQueue = new Map(); // queue key → pending write descriptor
// Bumped wherever an entry enters, is replaced in, or leaves _writeQueue, so a
// view of the queue's entries (the status moves, _statusEntriesByBase) is made
// again only when they changed, never on every tap (stage 6 review 9, F7).
export let _queueShapeGen = 0;
// A tap never is a status move, so one entering or leaving the queue leaves
// the index as it is (stage 6 review 10, m5: every tap made it again).
function _shapesStatusIndex(d) {
    return !d || d.kind !== 'evt';
}
// _queueSet: an entry enters, or replaces one in, _writeQueue.
export function _queueSet(key, d) {
    const prev = _writeQueue.get(key);
    _writeQueue.set(key, d);
    if ((prev && _shapesStatusIndex(prev)) || _shapesStatusIndex(d)) _queueShapeGen++;
}

// Entries this tab has taken out of its queue since the last persist: queue
// key → the enqueuedAt of the entry removed (Infinity when it carried none).
// _persistQueue prunes them from storage, which it otherwise leaves to the
// tabs that wrote them.
export const _settled = new Map();

export function _markSettled(key, descriptor) {
    const stamped = descriptor && typeof descriptor.enqueuedAt === 'number' && Number.isFinite(descriptor.enqueuedAt);
    const at = stamped ? descriptor.enqueuedAt : Infinity;
    _settled.set(key, Math.max(_settled.get(key) ?? -Infinity, at));
    // An entry with no stamp is recorded as older than every stamped copy
    // (L5): it cannot be told from a later write stored under its key, which
    // Infinity would then refuse for the page's life.
    const prev = _settledHere.get(key);
    const here = Math.max(prev ? prev.at : -Infinity, stamped ? at : -Infinity);
    const now = Date.now();
    // Re-inserted, so the map runs in settle order and the sweep stops at the
    // first record still young.
    _settledHere.delete(key);
    _settledHere.set(key, { at: here, settledAt: now });
    for (const [k, r] of _settledHere) {
        if (now - r.settledAt <= SETTLED_HERE_KEEP_MS) break;
        _settledHere.delete(k);
    }
}

// Every entry this tab has taken out of its queue, as _settled records it,
// but kept past the persist that empties _settled: a copy another tab stores
// again under the same key, no newer, is one this tab already settled, and is
// never taken up again (K2, _wasSettledHere). Key -> { at: the newest
// enqueuedAt settled (-Infinity for an entry that carried none), settledAt }.
const _settledHere = new Map();

// How long a settle is remembered (L5, stage 3 review 8). A stale copy comes
// back when another tab that still held the entry writes the queue before it
// has seen it leave storage: at its next persist, which a live tab makes
// within moments and a tab the browser suspended makes when it wakes. An hour
// covers a suspended tablet woken within a session; past it the copy is taken
// up as any entry, which the server answers as the duplicate it is.
const SETTLED_HERE_KEEP_MS = 60 * 60 * 1000;

// _wasSettledHere: the stored copy `d` of `key` is one this tab settled.
export function _wasSettledHere(key, d) {
    const r = _settledHere.get(key);
    if (!r) return false;
    if (Date.now() - r.settledAt > SETTLED_HERE_KEEP_MS) {
        _settledHere.delete(key);
        return false;
    }
    return !_enqueuedAfter(d, r.at);
}

// _dequeue: the ONE way out of the queue. Removes `key`, only while it still
// holds `descriptor` when one is given (a newer write may have replaced it
// mid-flush), and marks it settled so the next persist drops it from storage
// too. A bare _writeQueue.delete would leave the stored copy for the merge to
// keep, and the write would be replayed on the next load.
// _unorderedValue: an entry left the queue with a value the server never
// ordered by its stamp: a discard, or a clock refusal (a tap's outcome, or a
// write's answer).
export function _unorderedValue(v) {
    if (!v || typeof v !== 'object') return false;
    return v.discarded === true || v.outcome === TAP_CLOCK_SKEW || writeWasRefusedForClock(v);
}

export function _dequeue(key, descriptor, outcome) {
    const d = _writeQueue.get(key);
    if (!d || (descriptor && d !== descriptor)) return false;
    _writeQueue.delete(key);
    if (_shapesStatusIndex(d)) _queueShapeGen++;
    // A tap no longer queued here is no longer on a board (minor 2).
    if (d.kind === 'evt' && d.payload && d.payload.id) _dropToldHere(d.payload.id);
    _markSettled(key, d);
    _noteOwnStatusAnswer(key, d, outcome);
    if (outcome && outcome.byOperator && _inFlight.has(d)) {
        // Discarded while its request is out: the server may still keep it,
        // so what a discard undoes waits for that request to end (L1,
        // _endDiscardWhileOut); a member add's request end is the owner's
        // answer (_mintedAddAnswered).
        _discardWhileOut(d);
    } else if (outcome && (outcome.error || outcome.byOperator)) {
        // A court move or time change the server refused, or an operator
        // discarded, no longer shows (R4). Never for an entry another tab sent.
        _revertLocalFields(d);
        // A member add that leaves unanswered (a refusal was answered as it
        // came, _mintedAddAnswered): the owner says what is known of its id.
        if (mintedAddId(d)) {
            if (_minted.discarded(d) === 'dropped') _learnDropped([_droppedOf(d)]);
            else _mintedSweep(d);
        }
    }
    // The server answered it: a clock-skew re-stamp of a later write of the
    // match is never stamped below it (_afterSettledStamp). Not a discard (of
    // a write held for the clock, say): the server never ordered it, and its
    // stamp may be in the server's future, so it would floor every later
    // re-stamp of the match into the future too.
    if (outcome && 'value' in outcome && !_unorderedValue(outcome.value)) {
        const base = _baseOf(key);
        const s = _entryStamp(d);
        if (s > (_lastSettledStamp.get(base) || 0)) _lastSettledStamp.set(base, s);
    }
    // Every entry that leaves settles its door's promise: with the answer the
    // flush passes, else as discarded (a discard vs another tab sending it).
    _settleKey(key, outcome || { value: { discarded: true, sentByAnotherTab: true } });
    // Inside a batch the follow-ups below are done once for the batch (K6).
    if (_dequeueBatch) {
        if (d.kind === 'evt') _dequeueBatch.bases.add(_baseOf(key));
        _dequeueBatch.keys.add(key);
        return true;
    }
    if (d.kind === 'evt') _publishPendingTaps(_baseOf(key));
    _kickFollowersOf(new Set([key]));
    return true;
}

// _wipeQueue: the logout wipe's half of the queue (clearQueue). A hard wipe,
// every tab's entries included: the credential they carry is revoked, so
// nothing of the merge's is kept.
export function _wipeQueue() {
    _writeQueue.clear(); _queueShapeGen++;
    _settled.clear();
    // The wipe takes them out of storage too (P2).
    _settledLeftStored = false;
    _settledHere.clear();
    _removeSharedAnswers();
}

// While many entries leave at once (a tap batch settling, _sendTapBatch;
// another tab's removals, _onStoredQueueChanged), the follow-ups each _dequeue
// would do over the whole queue (publishing its match's pending taps, kicking a
// write that follows it) are gathered here and done once, so a batch costs
// one walk of the queue, not one per entry (stage 3 review 7, K6).
export let _dequeueBatch = null;

export function _openDequeueBatch() {
    if (!_dequeueBatch) _dequeueBatch = { bases: new Set(), keys: new Set() };
}

export function _closeDequeueBatch() {
    const b = _dequeueBatch;
    _dequeueBatch = null;
    if (!b) return;
    for (const base of b.bases) _publishPendingTaps(base);
    if (b.keys.size > 0) _kickFollowersOf(b.keys);
}

// _kickFollowersOf: an entry another match's write follows (`after`) has
// left: that write goes now, in this flush (a rerun) or in one kicked for it.
function _kickFollowersOf(keys) {
    for (const e of _writeQueue.values()) {
        if (_afterKeys(e).some((k) => keys.has(k))) {
            _rerunFlushSoon();
            return;
        }
    }
}

// What storage holds now, keyed. Unreadable storage reads as empty, so the
// persist writes this tab's queue alone, which is what it always did; the
// rehydrate is where a corrupt queue is counted and reported. Items without a
// key are left out; every other stored descriptor is kept verbatim for the
// next rehydrate to judge.
export function _storedQueue() {
    try {
        return _parseStoredQueue(localStorage.getItem(QUEUE_STORAGE_KEY));
    } catch (_e) {
        return new Map();
    }
}

// The answer of an entry, kept for the other tabs. A tab that sends an entry
// another tab queued (it held the base's lease) settles it here; the tab that
// made it, and any tab still holding a copy, only sees the entry leave storage
// (or not at all: a stale persist writes a copy back, K2), so a maker would
// settle its caller as `discarded`, never told a refusal nor reverting a court
// or time change the server refused, and a tab whose `storage` events lag
// would send the entry again. So the sender stores the answer under the entry's
// own key BEFORE it takes the entry out of storage (_shareAnswer), and a tab
// that finds one for its copy settles the entry with it instead of sending
// (_sharedAnswerFor: when it drops its copy as another tab's removal reaches it,
// and once it holds a base's lease, _settleAnsweredElsewhere). An outcome is as
// `_dequeue` takes it, `{ value }` or `{ error: { message, props } }`. Records
// older than ANSWER_KEEP_MS are swept as one is written, and the logout wipe
// removes them (_wipeQueue). Not for taps, which settle through their own
// outcomes.
export const ANSWER_STORAGE_PREFIX = 'bc_entry_answer_v1:';
const ANSWER_KEEP_MS = 60_000;
const ANSWER_SWEEP_MS = 30_000;
let _answersSweptAt = 0;
// Names this tab's own records, which it never reads back.
const _answerTab = _randomId();

function _answerKeys() {
    try {
        return Object.keys(localStorage).filter((k) => k.startsWith(ANSWER_STORAGE_PREFIX));
    } catch (_e) {
        return [];
    }
}

function _sweepAnswers(now) {
    if (now - _answersSweptAt < ANSWER_SWEEP_MS) return;
    _answersSweptAt = now;
    for (const k of _answerKeys()) {
        let at = 0;
        try { at = JSON.parse(localStorage.getItem(k)).at; } catch (_e) { /* unreadable: swept */ }
        if (!(now - at < ANSWER_KEEP_MS)) {
            try { localStorage.removeItem(k); } catch (_e) { /* unreadable storage */ }
        }
    }
}

export function _removeSharedAnswers() {
    for (const k of _answerKeys()) {
        try { localStorage.removeItem(k); } catch (_e) { /* unreadable storage */ }
    }
}

// _shareAnswer: the answer of an entry, stored for the other tabs. Anything
// that cannot be stored is left: the maker then settles its caller as
// discarded, as before.
export function _shareAnswer(key, d, outcome) {
    if (d.kind === 'evt' || !outcome || outcome.byOperator) return;
    try {
        const now = Date.now();
        _sweepAnswers(now);
        const shared = 'error' in outcome
            ? { error: { message: String(outcome.error && outcome.error.message), props: { ...outcome.error } } }
            : { value: outcome.value };
        localStorage.setItem(ANSWER_STORAGE_PREFIX + key, JSON.stringify({
            at: now, tab: _answerTab, enqueuedAt: d.enqueuedAt, outcome: JSON.parse(JSON.stringify(shared)),
        }));
    } catch (_e) { /* storage refused or the answer is not plain data */ }
}

// _sharedAnswerFor: the answer another tab stored for this tab's copy `d` of
// `key`, as `_dequeue` takes an outcome; undefined when there is none.
export function _sharedAnswerFor(key, d) {
    let a;
    try {
        a = JSON.parse(localStorage.getItem(ANSWER_STORAGE_PREFIX + key));
    } catch (_e) {
        return undefined;
    }
    if (!a || a.tab === _answerTab || a.enqueuedAt !== d.enqueuedAt || !a.outcome) return undefined;
    const { error, value } = a.outcome;
    if (error) return { error: Object.assign(new Error(error.message), error.props) };
    return { value };
}

// _settleAnsweredElsewhere: the entries of `base` that another tab already
// sent are settled with its answer, never sent again. Asked once this tab holds
// the base's lease, which the other tab gave up after it stored the answer.
// Returns whether any entry was settled.
export function _settleAnsweredElsewhere(base) {
    let settled = false;
    for (const [k, d] of _writeQueue) {
        if (_baseOf(k) !== base || _inFlight.has(d)) continue;
        const answer = _sharedAnswerFor(k, d);
        if (!answer) continue;
        if (answer.error) _claimRefusal(k, answer.error);
        if (_dequeue(k, d, answer)) settled = true;
    }
    if (settled) _persistQueue();
    return settled;
}

// The stored queue's serialized form (a `storage` event carries it as a
// string), keyed, with the same leniency as _storedQueue.
export function _parseStoredQueue(raw) {
    try {
        const entries = raw ? JSON.parse(raw) : [];
        if (!Array.isArray(entries)) return new Map();
        return new Map(entries.filter((e) => Array.isArray(e) && e.length >= 2 && e[0]));
    } catch (_e) {
        return new Map();
    }
}

// _enqueuedAfter: was queue entry `d` queued after time `at`? The shared
// outbox's one "which write is newer" test. An entry with no numeric stamp is
// never the newer one.
export function _enqueuedAfter(d, at) {
    return !!d && typeof d.enqueuedAt === 'number' && typeof at === 'number' && d.enqueuedAt > at;
}

// ---------------------------------------------------------------------------
// bc-mrgc phase 3: per match, an ordered list of pending writes.
// ---------------------------------------------------------------------------
// The separator between an entry's base (the match it is about) and its own
// id. A control character, so no competition, match, team or round id can
// contain it, and a key persisted by an earlier build (no separator) is its
// own base.
export const QUEUE_ENTRY_SEP = '\u001f';
let _entrySeq = 0;

// _baseOf: the match (or lineup) a queue key is about.
export function _baseOf(key) {
    const i = typeof key === 'string' ? key.indexOf(QUEUE_ENTRY_SEP) : -1;
    return i < 0 ? key : key.slice(0, i);
}


// _poolRanksBase: the queue base of a competition's pool-rank writes (a
// chusen's order, overridePoolRanks; stage 3 review 11, Y7), so they are sent
// strictly in the order they were made. They belong to no match.
export function _poolRanksBase(compID) {
    return `pool-ranks:${compID}`;
}

// _newEntryKey: a fresh key for one more write about `base`. Unique across
// tabs (the stored outbox is shared) and across reloads.
export function _newEntryKey(base) {
    _entrySeq++;
    return `${base}${QUEUE_ENTRY_SEP}${Date.now().toString(36)}.${_entrySeq.toString(36)}.${_randomId().slice(0, 8)}`;
}

// _entryStamp: the order an entry replays in: the stamp its payload carries
// (when the operator made the change, server-relative), or, for a write with
// none (a lineup), when it was queued.
export function _entryStamp(d) {
    const p = d && d.payload;
    const s = Number(p && p.modifiedAt);
    if (Number.isFinite(s) && s > 0) return s;
    // A tap carries its stamp as `stamp`; a door whose body has no stamp of
    // its own (reopen, requeue, send back, a kachinuki bout removal) keeps its
    // place in its match's order on the descriptor, in server time like the
    // rest, never the local wall clock.
    const t = Number(p && p.stamp);
    if (Number.isFinite(t) && t > 0) return t;
    const o = Number(d && d.sortStamp);
    if (Number.isFinite(o) && o > 0) return o;
    return Number(d && d.enqueuedAt) || 0;
}

// _byEntryOrder: the ONE replay order. By stamp, then by when it was queued,
// then by key, so it is total and the same in every tab.
export function _byEntryOrder([ka, a], [kb, b]) {
    return (_entryStamp(a) - _entryStamp(b))
        || ((Number(a && a.enqueuedAt) || 0) - (Number(b && b.enqueuedAt) || 0))
        || (ka < kb ? -1 : ka > kb ? 1 : 0);
}

// _entriesFor: a base's pending writes, in replay order.
export function _entriesFor(base) {
    const out = [];
    for (const [k, d] of _writeQueue) {
        if (d && _baseOf(k) === base) out.push([k, d]);
    }
    return out.sort(_byEntryOrder);
}
// Per match: the stamp of the newest write of this device the server
// answered (an entry that left the queue with an answer). A clock-skew
// re-stamp is never below it (_afterSettledStamp).
export const _lastSettledStamp = new Map();
// Per queue key: the promise a door handed out and the refusal claims of the
// callers. In memory only (never persisted): a function does not survive a
// reload, and a rehydrated entry has nobody waiting on it.
export const _entryHooks = new Map();

// _openSettlement: the `settled` promise of a queue key. A write that takes
// the place of a queued one (_placeWrite reuses its key) shares its promise,
// so the first caller learns the answer of the write that replaced its own.
export function _openSettlement(key, onRefusal) {
    let h = _entryHooks.get(key);
    if (!h) {
        let resolve, reject;
        const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
        // A caller that never awaits it must not raise an unhandled rejection;
        // one that does still receives the rejection.
        promise.catch(() => {});
        h = { promise, resolve, reject, claims: [] };
        _entryHooks.set(key, h);
    }
    if (typeof onRefusal === 'function') h.claims.push(onRefusal);
    return h.promise;
}
export function _settleKey(key, outcome) {
    const h = _entryHooks.get(key);
    if (!h) return;
    _entryHooks.delete(key);
    if (outcome && 'error' in outcome) h.reject(outcome.error);
    else h.resolve(outcome ? outcome.value : undefined);
}
// _claimRefusal: does a caller take this refusal on itself (onRefusal
// returned true)? Then the flush raises no alert for it.
export function _claimRefusal(key, err) {
    const h = _entryHooks.get(key);
    if (!h) return false;
    let claimed = false;
    for (const fn of h.claims) {
        // A claim that throws has not shown the refusal: the outbox says it.
        try { if (fn(err) === true) claimed = true; } catch (e) { console.error(`[sync] a refusal handler for ${key} threw:`, e); }
    }
    return claimed;
}

// The pending answer every outbox door gives: the write is the outbox's now.
export function _pendingAnswer(key, settled) {
    const answer = { applied: true, pending: true, key, settled };
    if (!_lastEnqueuePersisted) answer.persisted = false;
    return answer;
}

// _parkStoredEntries: blank the password on every STORED entry and park it
// (authBlocked), another tab's included, for a revoked credential
// (parkQueueForReauth). The merge keeps what this tab did not write, so
// persisting this tab's scrubbed queue alone would leave the revoked
// plaintext password in storage under the other tab's entries.
export function _parkStoredEntries() {
    // What an older build still holds under the old key carries the revoked
    // password too: taken over first, so it is parked with the rest (F4).
    try { _takeOverLegacyQueue(localStorage.getItem(LEGACY_QUEUE_STORAGE_KEY)); } catch (_e) { /* unreadable storage */ }
    const stored = _storedQueue();
    if (stored.size === 0) return;
    for (const d of stored.values()) {
        if (!d || typeof d !== 'object') continue;
        d.password = '';
        d.authBlocked = true;
    }
    try {
        localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify([...stored.entries()]));
    } catch (_e) { /* unwritable storage: the persist that follows reports it */ }
}
