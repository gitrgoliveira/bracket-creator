// api_client_store.jsx: the ONE shared store of the outbox core (the api_client_*
// modules that send, settle and report the queued writes of this tab). A leaf:
// it imports only leaves (no module that acts on the queue), so no outbox module needs another to reach shared
// state, and the static imports among the outbox modules run one way, from the
// modules that act on the queue toward the queue (check-load-cycles.mjs guards it).
//
// Holds:
// - the state more than one outbox module reads or writes (`store`, and the
//   collections below), moved here from the module that happened to declare it;
// - `late`, the calls a lower module makes into a higher one (the queue tells the
//   flush to run again, a settled write tells the editors): a lower module may not
//   import a higher one, so the higher one `provide`s the function at load and the
//   lower one calls it as `late.name(...)`. A call before its module loaded throws
//   and names the function; the list below is the whole of them.
import { createMintedMembers } from './minted_members.jsx';
import { FETCH_TIMEOUT_MS } from './write_result.jsx';
import { _randomId } from './api_client_ids.jsx';

// Scalars the owner writes and others read, so a read is never stale.
export const store = {
    // Bumped by clearQueue() (logout / password_reset) or a park. An in-flight
    // _flushQueue loop snapshots this at the start and aborts if it changes
    // mid-loop, so a credential revocation can't keep sending queued writes with
    // the now-revoked password. Written by api_client_flush.jsx.
    queueGen: 0,
    // Set to true ONLY when a flush attempt ends with at least one true NETWORK
    // failure (fetch rejected: connection down). A non-2xx server response (e.g.
    // a transient 5xx/429) keeps the write queued for retry but is NOT "offline":
    // the network is up, so the pill stays "Syncing..." rather than "Offline".
    // Cleared back to false when a flush drains the queue or no network failure
    // occurs. Written by api_client_flush.jsx.
    offlineFlag: false,
    // Whether the last write queued (_commitEnqueue) reached browser storage. A
    // write held in memory alone is lost if the page goes, so its editor must not
    // tell the operator it is "saved on this device". Written by
    // api_client_enqueue.jsx.
    lastEnqueuePersisted: true,
    // A trigger that arrives while a flush runs sets this; the running loop reruns
    // once it finishes (api_client_flush.jsx, whose pass reads and clears it; the
    // tap sender sets it too).
    flushRequested: false,
};

// bc-evts: the queued entries being sent right now (_sendEntry). Every write is
// an outbox entry, stored before it is sent and leaving storage only once it
// settles, so a page going away mid-send leaves it for the next load; this set
// only stops a later write taking the place of one already out
// (_coalesceTarget). Never persisted.
export const _inFlight = new WeakSet();

// What is known of every member id this device (or another tab) minted: the
// ONE owner, minted_members.jsx (bc-evts stage 3, M1 a), shared through
// browser storage. Its storage is reached through this module, as every other
// read and write of browser storage is. Declared here, before the load-time
// flush.
export const _minted = createMintedMembers({
    storage: {
        get: (k) => localStorage.getItem(k),
        set: (k, v) => localStorage.setItem(k, v),
        remove: (k) => localStorage.removeItem(k),
    },
    timeoutMs: FETCH_TIMEOUT_MS,
    tab: _randomId(),
});

// The entries an operator discarded while their request was out (stage 3
// review 8, L1): the discard took them out of the queue at once, so nothing
// waits on them and the UI goes on, but what a discard undoes (a court or time
// overlay put back) waits for the request's end, which may be the server
// keeping the write. A member add's end is the owner's (_mintedAddAnswered).
export const _discardedWhileOut = new WeakSet();

// Per match: the newest derived match a score-events answer handed back (the
// broadcast copy, with markIds and scoreVersion).
export const _latestDerived = new Map();

// The latest `turnedAway` seen on any copy of each match, by base, so a tap
// whose own applied answer arrives AFTER the copy listing it (the finish's
// push overtook the answer) is told as it lands, from _noteLandedTaps, not
// left until the next copy of the match (stage 6 review 6). A copy with a
// lower scoreVersion than the one kept (an answer the server made before
// the finish) does not replace it. Forgotten with the shown matches.
// Each entry also keeps the newest `replacedResult` seen (bc-sseq FR8: the
// result of another device that the court's result replaced) as `replaced`,
// with the version of the copy that named it (`rv`) and the match's shiaijo
// (`court`), so a result whose own applied answer is noted after that copy is
// told as it is noted, and a heartbeat tells what a hidden tab left.
export const _turnedAwaySeen = new Map();

// bc-sseq FR8: the finished results (isFinishedResultWrite) this device sent
// that the server applied, by write id, so a copy of the match whose
// `replacedResult` names one tells its operator, once, that the court's result
// replaced it (api_client_taps.jsx, _tellReplaced). Kept in browser storage, so
// every tab of the device and the next page after a reload know them: a ring of
// APPLIED_RESULTS_MAX, the oldest going first, each kept a day. An entry is
// taken out before it is told, so it is told once per write id. Each names the
// tab that noted it (`tab`), so the device's one telling tab tells it
// (landed_taps.jsx `tellsFor`). Memory alone while storage refuses a write.
export const APPLIED_RESULTS_KEY = 'bc_applied_results_v1';
export const APPLIED_RESULTS_MAX = 200;
export const APPLIED_RESULT_KEEP_MS = 24 * 60 * 60 * 1000;
export function createAppliedResults({ storage, now = () => Date.now() } = {}) {
    let mem = [];
    // True while the last write did not reach storage: storage then lacks
    // this tab's own change, so memory is read instead.
    let memOnly = false;
    const valid = (e) => !!e && typeof e === 'object' && typeof e.id === 'string' && e.id !== '';
    const read = () => {
        if (memOnly) return mem.slice();
        try {
            const raw = storage.getItem(APPLIED_RESULTS_KEY);
            if (raw == null) return [];
            const list = JSON.parse(raw);
            return Array.isArray(list) ? list.filter(valid) : [];
        } catch (_e) {
            return mem.slice();
        }
    };
    const write = (list) => {
        mem = list;
        try {
            if (list.length > 0) storage.setItem(APPLIED_RESULTS_KEY, JSON.stringify(list));
            else storage.removeItem(APPLIED_RESULTS_KEY);
            memOnly = false;
        } catch (_e) {
            memOnly = true;
        }
    };
    const fresh = (list) => {
        const t = now();
        return list.filter((e) => !(Number(e.at) > 0) || t - Number(e.at) < APPLIED_RESULT_KEEP_MS);
    };
    return {
        /** Notes one applied result: {id (the write id), c, m, where, tab}. */
        note(rec) {
            if (!valid(rec)) return;
            const list = fresh(read()).filter((e) => e.id !== rec.id);
            list.push({ ...rec, at: Number(rec.at) || now() });
            write(list.slice(-APPLIED_RESULTS_MAX));
        },
        /** Whether a write id is noted. */
        has(id) {
            return read().some((e) => e.id === id);
        },
        /**
         * Takes the entry of write `id` out and returns it when `may(entry)`
         * allows, else null: the entry is left for a later copy or another tab.
         */
        take(id, may) {
            if (typeof id !== 'string' || !id) return null;
            const list = read();
            const i = list.findIndex((e) => e.id === id);
            if (i < 0) return null;
            const e = list[i];
            if (typeof may === 'function' && !may(e)) return null;
            list.splice(i, 1);
            write(fresh(list));
            return e;
        },
        /** Sign-out: takes out every entry `gone(entry)` names. */
        forget(gone) {
            const list = read();
            const kept = list.filter((e) => !gone(e));
            if (kept.length !== list.length) write(kept);
        },
    };
}
export const _appliedResults = createAppliedResults({
    storage: {
        getItem: (k) => globalThis.localStorage.getItem(k),
        setItem: (k, v) => globalThis.localStorage.setItem(k, v),
        removeItem: (k) => globalThis.localStorage.removeItem(k),
    },
});

const NOT_PROVIDED = (name) => () => {
    throw new Error(`api_client_store: late.${name} was called before the module that provides it loaded`);
};

// The calls a lower outbox module makes into a higher one, by the name the
// function has in its owner. Each is provided once, at load, by its owner:
//   api_client_flush.jsx     _rerunFlushSoon
//   api_client_enqueue.jsx   _outboxWrite, _withoutDownstreamConfirmation, _takeOverLegacyQueue
//   api_client_taps.jsx      _tellTurnedAwayIn, _dropToldHere, _publishPendingTaps
//   api_client_overlays.jsx  _noteOwnStatusAnswer, _revertLocalFields
//   api_client_members.jsx   _droppedOf, _mintedSweep, _learnDropped, _mintedPrune, _withListedMembersForgotten
//   api_client_clock.jsx     _relearnClockThrottled
export const late = Object.fromEntries([
    '_rerunFlushSoon',
    '_outboxWrite',
    '_tellTurnedAwayIn',
    '_noteOwnStatusAnswer',
    '_revertLocalFields',
    '_dropToldHere',
    '_publishPendingTaps',
    '_withoutDownstreamConfirmation',
    '_takeOverLegacyQueue',
    '_droppedOf',
    '_mintedSweep',
    '_learnDropped',
    '_mintedPrune',
    '_relearnClockThrottled',
    '_withListedMembersForgotten',
].map((name) => [name, NOT_PROVIDED(name)]));

/** Fills `late` with the functions a module owns; a name that is not listed above is a typo. */
export function provide(fns) {
    for (const [name, fn] of Object.entries(fns)) {
        if (!(name in late)) throw new Error(`api_client_store: late.${name} is not a listed call`);
        late[name] = fn;
    }
}
