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
export const _turnedAwaySeen = new Map();

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
