// The flush: each pass over the queue, the per-match order and wait cycles, one
// entry's send and its answer, and the clock re-stamps.
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import {
    writeWasSuperseded, writeWasRefusedForClock, writeMatchClosed, writeDisplacedGroups, REP_BOUT_NOT_ADDED,
    REP_BOUT_NOT_REMOVED, isFinishedResultWrite,
} from './write_result.jsx';
// The offset lives in a leaf so a score editor can read the same clock (server_clock.jsx).
import { serverClockOffsetMs } from './server_clock.jsx';
import { DOC_VERSION_FIELD, tournamentRefusal, awardsRefusal } from './document_versions.jsx';
// bc-evts S2: the build this page runs against the server's, and the reload rule.
import { noteOutboxDrained } from './app_version.jsx';
import { mintedAddId } from './minted_members.jsx';
import {
    _refusalText, _refusalError, _reopenRefusal, _downstreamRefusalError, _courtBusyError,
    _leagueFinishedError, _lotsReplacedError, _replayRefusal, _serverKeepsRetrying, _doorValue,
    _reasonHumanError, _fetchJson, LINEUP_NOT_REMOVED,
} from './api_client_requests.jsx';
import {
    _learnServerClockOffset, _relearnClockThrottled, _serverNowMs, _stampAfterSeen, _perfAgeMs,
    CLOCK_SKEW_FAIL_REASON, _forgetRev, _afterSettledStamp,
} from './api_client_clock.jsx';
import {
    _isAllowedTerminalRequest, _replayBody, _notifyQueueAlert, _settledLeftStored, _persistQueue,
    _writeQueue, _dequeue, _baseOf, _byEntryOrder, _entriesFor, _claimRefusal,
} from './api_client_queue.jsx';
import { _CHECKIN_KINDS, _checkInAnswered, rebaseQueuedVersion } from './api_client_overlays.jsx';
import { TAP_BATCH_MAX, _tellTurnedAway, _sendTapBatch } from './api_client_taps.jsx';
import { _randomId } from './api_client_ids.jsx';
import { createBaseLease } from './base_sender_lease.jsx';
import {
    _tellRefusal, _notifyScoreSupersededEditor, _notifyScoreSupersededAlert, _heldForClosedMatch,
    _refusedByServer, _recomputeSyncStatus, SERVER_REJECTION_NOTICE_THRESHOLD, _endDiscardWhileOut,
} from './api_client_status.jsx';
import {
    _minted, TEAM_MEMBER_NOT_ADDED, TEAM_MEMBER_NOT_RENAMED, TEAM_MEMBER_NOT_CLEARED, _mintedAddAnswered,
    _mintedGate, _noteMintedOutHere, _endMintedOutHere,
} from './api_client_members.jsx';

// bc-evts: the queued entries being sent right now (_sendEntry). Every write is
// an outbox entry, stored before it is sent and leaving storage only once it
// settles, so a page going away mid-send leaves it for the next load; this set
// only stops a later write taking the place of one already out
// (_coalesceTarget). Never persisted.
export const _inFlight = new WeakSet();
// Set to true ONLY when a flush attempt ends with at least one true NETWORK
// failure (fetch rejected: connection down). A non-2xx server response (e.g. a
// transient 5xx/429) keeps the write queued for retry but is NOT "offline": the
// network is up, so the pill stays "Syncing…" rather than "Offline". Cleared
// back to false when a flush drains the queue or no network failure occurs.
export let _offlineFlag = false;
export function _afterDiscard() {
    _persistQueue();
    _recomputeSyncStatus();
    // A discarded write may have been holding back its match's later writes
    // (strict order, F1): send them now rather than on an unrelated trigger.
    if (_writeQueue.size > 0) {
        _resetFlushBackoff();
        _flushQueue();
    }
}

let _flushTimer = null;
const FLUSH_BACKOFF_MS = [500, 1000, 2000, 4000, 8000];
const ADOPTED_FLUSH_DELAY_MS = 300;
export let _flushAttempt = 0;
// Single-in-flight guard. _flushQueue's body awaits network I/O, so without a
// lock a second trigger (a rapid enqueue, an `online` event, or a backoff timer
// firing mid-flush) would start an OVERLAPPING loop iterating the same snapshot:
// duplicate PUTs and corrupted backoff state. Instead, a trigger that arrives
// while a flush is running sets _flushRequested; the running loop reruns once it
// finishes so newly-queued or still-failed writes are retried without overlap.
let _flushInProgress = false;
let _flushRequested = false;
// _launchIdleBases: while a pass runs, starts every base of the queue that is
// not out and did not end failing in it (set by _flushQueue, null otherwise;
// _wakeIdleBases asks it).
let _launchIdleBases = null;
// _basesOut: the bases a pass is flushing now. Every launch checks it. Passes
// run one at a time and each waits for all its turns, so today it always
// matches the pass's own `active`; it is kept so that a later change letting a
// pass end with a turn still out cannot flush one base twice at once.
const _basesOut = new Set();
// _baseLease: the tabs of one device share the outbox, and only the tab that
// holds a base's lease sends it (base_sender_lease.jsx), so one entry is sent
// once and a base's entries go in one tab's order. Storage is reached lazily,
// so a page whose storage throws sends as it always did.
export const _baseLease = createBaseLease({
    storage: {
        getItem: (k) => globalThis.localStorage.getItem(k),
        setItem: (k, v) => globalThis.localStorage.setItem(k, v),
        removeItem: (k) => globalThis.localStorage.removeItem(k),
    },
    tab: _randomId(),
});
// Bumped by clearQueue() (logout / password_reset). An in-flight _flushQueue loop
// snapshots this at the start and aborts if it changes mid-loop, so a credential
// revocation can't keep sending queued writes with the now-revoked password.
export let _queueGen = 0;

// The flush state above is written by the flush code alone; every other writer
// (an enqueue, another tab's write, the online event, a discard, and the
// sign-in, park and wipe doors) goes through these (bc-jsxs).
// _resetFlushBackoff: a fresh trigger. The backoff timer goes, and the next
// failure backs off from the first step again.
export function _resetFlushBackoff() {
    if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
    _flushAttempt = 0;
}
// _setOffline: whether the last pass ended for the connection (_offlineFlag);
// a park or a wipe clears it.
export function _setOffline(offline) {
    _offlineFlag = offline;
}
// _bumpQueueGen: a park or a wipe ends every pass in flight (_queueGen).
export function _bumpQueueGen() {
    _queueGen++;
}
// _requestFlushRerun: the pass running now runs again once it ends.
export function _requestFlushRerun() {
    _flushRequested = true;
}

// bc-cse: the queue's half of the clock-skew heal, shared by BOTH flush arms
// (terminal writes and running score writes) because the re-stamp rule is one
// rule: heal the offset, then rebuild this entry's stamp in the SERVER's frame
// from the moment the operator acted.
//
// AWAITED, not the throttled wrapper: the re-stamp is only correct once the new
// offset is in hand, and the learn is single-flighted so a concurrent
// background one is joined rather than duplicated.
//
// enqueuedAt is the LOCAL time of the operator's action, so enqueuedAt + offset
// is that action's time in the server's frame. Deliberately not _serverNowMs():
// the write must keep its real age and still LOSE to a genuinely newer result,
// rather than being made fresh by the act of retrying it. (The fallback cannot
// fire for an entry that came through enqueue or rehydrate - both guarantee a
// finite positive stamp - and exists so a hand-edited store yields a number,
// not NaN.)
//
// The wall-clock arithmetic above is only ONE of the two reconstructions, and on
// its own it assumes the local clock ticked monotonically while only the learned
// OFFSET was wrong. If the OS STEPPED the wall clock between enqueue and flush -
// the NTP-jump-on-reconnect case the 'online' relearn exists for - enqueuedAt was
// recorded in a frame the new offset does not describe, and a device that was 10
// minutes fast when the operator scored, then corrected on reconnect, would
// re-stamp 10 minutes into the server's future again and be dropped on the second
// refusal. _restampFor closes that WITHIN a session by cross-checking against a
// monotonic anchor and taking whichever reconstruction is older; see the safety
// argument stated there.
//
// RESIDUAL, unchanged and accepted: across a reload (or between two tabs) there
// is no comparable anchor - performance.now() is measured from a per-document
// origin - so a rehydrated entry falls back to wall-clock only and a clock step
// spanning the reload still ends in a drop. That loss is LOUD: the not-landed
// banner and the queue alert both fire and the operator re-enters, which is why
// the second refusal is terminal rather than looping. Clamping to serverNow
// would make it silent instead of loud AND wrong in the dangerous direction -
// a stale replay beating results recorded during the outage is the exact
// overwrite bc-lww1 exists to stop.
//
// Marks the entry so a SECOND refusal is terminal, and persists, so the mark
// survives a reload (see the skewRetried note on the descriptor shape).
// `base` is the entry's own (a check-in's `checkin:<comp>`, a court call's
// `announce:<id>`), never re-derived from its match.
async function _restampQueuedEntryForSkew(descriptor, base) {
    await _learnServerClockOffset();
    const enqueuedAt = Number.isFinite(descriptor.enqueuedAt) ? descriptor.enqueuedAt : Date.now();
    descriptor.payload.modifiedAt = _afterSettledStamp(base,
        _stampAfterSeen(_restampFor(enqueuedAt, descriptor.perfAtEnqueue), descriptor.seenModifiedAt));
    descriptor.skewRetried = true;
    _persistQueue();
}

// _resendForClock: a write the server refused for clock_skew (it logged
// nothing). The clock is learned again and the write re-stamped, every time.
// The first refusal resends at once; a refusal again on a stamp built from a
// relearned clock keeps the write queued and held (_keepForClock), retried on
// the backoff until the clock is right or an operator discards it. Never
// dropped (operator rule: nothing a device sends is dropped).
async function _resendForClock(key, descriptor, pass, alert) {
    const again = !!descriptor.skewRetried;
    await _restampQueuedEntryForSkew(descriptor, _baseOf(key));
    if (_writeQueue.get(key) !== descriptor) return 'settled'; // discarded meanwhile
    if (!again) {
        _flushRequested = true;
        return 'reclocked';
    }
    if (_keepForClock(descriptor, pass)) {
        _notifyQueueAlert({ kind: 'server_error', ...alert, detail: CLOCK_SKEW_FAIL_REASON });
        _recomputeSyncStatus();
    }
    return 'kept';
}

// _keepForClock: the ONE rule for a write or tap refused for clock_skew again
// after a re-stamp. It stays queued, reads as one the server keeps refusing
// (`clockRefused`, _refusedByServer: held, and discardable from its editor and
// the held-writes list), and the pass backs off. True the first time, so the
// caller tells the operator once.
export function _keepForClock(d, pass) {
    pass.anyFailed = true;
    if (d.clockRefused) return false;
    d.clockRefused = true;
    _persistQueue();
    return true;
}

// The re-stamp arithmetic, factored out so it can be tested without a queue and
// so the safety rule below has one statement rather than one per branch.
//
// Two independent reconstructions of "when did the operator act, in the server's
// frame", each with a failure mode the other does not share:
//
//   wall = enqueuedAt + offset     wrong if the OS STEPPED the clock between
//                                  enqueue and flush (enqueuedAt is then in a
//                                  frame the new offset does not describe)
//   perf = serverNow - trueAge     wrong if performance.now() FROZE while the
//                                  device was suspended (age understated, so
//                                  the write looks fresher than it is)
//
// Take the OLDER. That is not a tie-break, it is the whole safety argument: an
// over-old stamp can only make THIS write lose a comparison it might have won,
// which costs at worst a re-entry the operator is already being told about. An
// over-NEW stamp lets a stale replay beat a result recorded during the outage —
// silently, and that overwrite is the entire reason bc-lww1 exists. So where the
// two disagree we always prefer the direction whose failure is recoverable.
export function _restampFor(enqueuedAt, perfAtEnqueue) {
    const wall = enqueuedAt + serverClockOffsetMs();
    const ageMs = _perfAgeMs(perfAtEnqueue);
    // A negative age is not a clock step, it is two readings from different time
    // origins (a rehydrated entry that predates the strip, a hand-edited store).
    // There is no true age to recover, so fall back rather than invent one.
    if (ageMs === null || !(ageMs >= 0)) return wall;
    return Math.min(wall, _serverNowMs() - ageMs);
}

export async function _flushQueue() {
    if (_flushInProgress) { _flushRequested = true; _wakeIdleBases(); return; }
    _flushInProgress = true;
    // What this pass has to tell, counted across the whole call (the do/while
    // rerun included) and announced ONCE at the end, so a court reconnecting
    // with several held writes gets one alert stating how many, not N toasts
    // overwriting each other.
    //   superseded/lastSuperseded/matchClosed: writes kept in their match's
    //     history (writeMatchClosed: the alert then says to tap Correct).
    //   displaced: held finishes that landed and moved a LATER change of their
    //     match to its history (writeDisplacedGroups).
    //   sent: HELD writes that landed (bc-offl). A write is held only once a
    //     pass has left it unsettled (or it came back from storage), so an
    //     ordinary online Finish never reaches this count (operator decision
    //     2026-09-27).
    //   notApplied: taps the server did not apply (bc-evts, one alert a pass).
    const pass = {
        superseded: 0, lastSuperseded: null, matchClosed: false,
        displaced: 0, sent: 0, notApplied: 0, lastNotApplied: null, parkedTaps: 0,
        anyFailed: false, networkFailed: false, leaseSkipped: false,
    };
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
            pass.anyFailed = false;      // any failure → keep in queue + backoff
            pass.networkFailed = false;  // fetch rejected → connection down → "offline"
            pass.leaseSkipped = false;   // a base left to another tab: it says nothing of the connection
            const gen = _queueGen; // clearQueue() bumps this to cancel an in-flight flush
            // bc-evts F1 (reversing bc-mrgc C2): per match (base), in the ONE
            // replay order, _byEntryOrder, and strictly in order: the first entry
            // of a match that does not settle in this pass (offline, a 5xx,
            // parked, held by an open fetch, re-stamped for a clock refusal) ends
            // THAT match's turn; the other matches go on. A write made after
            // another on this device never reaches the server before it.
            // The matches go side by side, each strictly in its own order: one
            // match's slow or hanging request never holds another match's
            // writes for the pass. A base checks `gen` before each send, so
            // clearQueue() (logout / password_reset) stops every base before it
            // sends anything else with the revoked password. A write queued
            // while the pass runs, or a base freed by another's settle
            // (`after`), starts its base at once when that base is not out
            // (_launchIdleBases), so it never waits for another match's
            // hanging request; a base that ended failing is not started again
            // until the rerun, as before. One base's fault never cuts
            // another's turn short; the first is thrown once every base has
            // ended, as before.
            const active = new Map();
            const failedBases = new Set();
            const faults = [];
            // `closed` is set in the same step as the last base ends, so a wake
            // that runs before this pass's continuation starts nothing the pass
            // no longer waits for (the rerun picks it up).
            let closed = false;
            const launch = () => {
                if (closed || gen !== _queueGen) return;
                const byBase = new Map();
                for (const entry of [..._writeQueue.entries()].sort(_byEntryOrder)) {
                    const base = _baseOf(entry[0]);
                    if (!byBase.has(base)) byBase.set(base, []);
                    byBase.get(base).push(entry);
                }
                // Entries that wait on each other are stuck, not waiting (H1).
                _markWaitCycles(byBase);
                for (const [base, list] of byBase) {
                    if (active.has(base) || failedBases.has(base) || _basesOut.has(base)) continue;
                    // Another tab of the device is sending this base: it is not
                    // failing, and not held. The holder's wake after a turn that
                    // sent all it held reruns the flush, and the backoff covers
                    // a holder that failed or is gone.
                    if (!_baseLease.claim(base)) {
                        failedBases.add(base);
                        pass.leaseSkipped = true;
                        _askAgainWhenLeaseEnds(base);
                        continue;
                    }
                    active.set(base, true);
                    _basesOut.add(base);
                    (async () => {
                        let ended = '';
                        try {
                            ended = await _flushBase(base, list, pass, gen);
                            if (ended === 'kept') failedBases.add(base);
                        } catch (e) {
                            faults.push(e);
                            failedBases.add(base);
                        } finally {
                            active.delete(base);
                            _basesOut.delete(base);
                            _baseLease.release(base, ended === 'settled');
                            // A base that sent all it held, or whose entry was
                            // replaced while out (`restamped`), goes on with what
                            // is queued for it, in its order. A clock-skew
                            // re-stamp (`reclocked`) waits for the pass's rerun,
                            // as it always did: the clock it was re-stamped from
                            // is learned again there.
                            if ((ended === 'settled' || ended === 'restamped')
                                && [..._writeQueue.keys()].some((k) => _baseOf(k) === base)) launch();
                            if (active.size === 0 && !closed) {
                                closed = true;
                                _launchIdleBases = null;
                                allEnded();
                            }
                        }
                    })();
                }
            };
            let allEnded = () => {};
            const everyBaseEnded = new Promise((resolve) => { allEnded = resolve; });
            _launchIdleBases = launch;
            try {
                launch();
                if (active.size > 0) await everyBaseEnded;
            } finally {
                _launchIdleBases = null;
            }
            if (faults.length > 0) throw faults[0];
            _persistQueue();
            if (_writeQueue.size === 0) {
                _flushAttempt = 0;
                _offlineFlag = false;
                _recomputeSyncStatus();
            } else if (!pass.anyFailed && !pass.leaseSkipped) {
                // bc-qttl: entries remain but nothing failed this pass, which is the
                // all-parked case (a parked write is skipped, so it can neither
                // succeed nor fail). Republish so the pill leaves "Syncing…" for the
                // parked state, and schedule NO backoff timer: a parked write cannot
                // progress until resumeAfterAuth() or a discard.
                _offlineFlag = false;
                _recomputeSyncStatus();
            } else {
                // A pass that only left bases to other tabs learned nothing about
                // the connection: the flag stays as the last real pass set it.
                if (pass.anyFailed) _offlineFlag = pass.networkFailed;
                _recomputeSyncStatus();
                if (!_flushRequested) {
                    // Schedule a backoff retry only if no immediate rerun is already
                    // pending (a pending rerun re-attempts the failed writes now).
                    const delay = FLUSH_BACKOFF_MS[Math.min(_flushAttempt, FLUSH_BACKOFF_MS.length - 1)];
                    // Waiting for another tab's lease is not a failure: only a real
                    // failure climbs the backoff, so this tab's first network
                    // failure after a wait still retries at the first step.
                    if (pass.anyFailed) _flushAttempt++;
                    _flushTimer = setTimeout(_flushQueue, delay);
                }
            }
        } while (_flushRequested);
    } finally {
        _flushInProgress = false;
        // In the finally so the alert still reaches the operator if the pass
        // throws part-way: the drops it counts have already happened and their
        // queue entries are already gone, so this is the last chance to say so.
        // 'sent' goes FIRST: the toast keeps a visible error over a later
        // success, so a pass with both still ends on the error, and a pass with
        // no error shows the confirmation.
        if (pass.sent > 0) _notifyQueueAlert({ kind: 'sent', count: pass.sent, terminalCount: pass.sent });
        if (pass.displaced > 0) _notifyQueueAlert({ kind: 'displaced', count: pass.displaced, terminalCount: pass.displaced });
        _notifyScoreSupersededAlert(
            pass.superseded,
            pass.lastSuperseded ? pass.lastSuperseded.compID : undefined,
            pass.lastSuperseded ? pass.lastSuperseded.matchID : undefined,
            pass.matchClosed,
        );
        if (pass.notApplied > 0) {
            _notifyQueueAlert({
                kind: 'not_applied', count: pass.notApplied, terminalCount: 0,
                ...(pass.lastNotApplied ? { outcome: pass.lastNotApplied } : {}),
            });
        }
        if (pass.parkedTaps > 0) _notifyQueueAlert({ kind: 'taps_parked', count: pass.parkedTaps, terminalCount: 0 });
        // S2: a new build waits for the outbox to drain before it reloads.
        if (_outboxIdle()) noteOutboxDrained();
    }
}

// The outbox is idle when nothing is queued and no write is in flight.
export function _outboxIdle() {
    return _writeQueue.size === 0 && !_flushInProgress && !_settledLeftStored;
}

// _flushBase: one match's turn in a pass. Consecutive taps of ONE session go
// in one POST of at most TAP_BATCH_MAX; every other entry on its own. The
// first entry that does not settle ends the turn, and every entry the turn
// leaves behind is marked held (persisted), so its later landing is told.
async function _flushBase(base, list, pass, gen) {
    let i = 0;
    let outcome = 'settled';
    while (i < list.length) {
        if (gen !== _queueGen) return;
        const [key, d] = list[i];
        // Skip entries removed or replaced since the snapshot was taken.
        if (_writeQueue.get(key) !== d) { i++; continue; }
        // bc-qttl: a write parked on 401 carries the password captured at
        // enqueue time, so retrying it can only reproduce the same auth failure
        // until API.resumeAfterAuth() re-stamps it. A batch of taps the server
        // refused (400) is parked until an operator discards it (operator S5 b).
        // Each of these holds its match.
        if (d.authBlocked || d.parked) { outcome = 'kept'; break; }
        // A write that follows another match's entry (`after`) waits while that
        // entry is queued; once it settles or is discarded, this one goes and
        // the server judges it. Waiting is not failing: not marked held, and
        // the settle that frees it reruns the flush (_dequeue). Waiting on an
        // entry the server keeps refusing (a member add stuck on a 5xx, a
        // parked write) is held too (stage 3 review 3, F6), so this match's
        // editor shows that something it depends on is not getting through.
        // So is one on a wait cycle (H1, _markWaitCycles), which nothing frees.
        const waitsOn = _afterKeys(d).filter((k) => _writeQueue.has(k));
        if (waitsOn.length > 0) {
            outcome = d.cycled || waitsOn.some((k) => _blockedByHeld(k)) ? 'kept' : 'after';
            break;
        }
        // A write naming a member not known to exist waits the same way, and
        // one naming a member that never was goes without the id, or, for a
        // rename or a cleared name of that member, leaves unsent (the owner,
        // minted_members.jsx; its read, the add's answer or its timer reruns
        // the flush).
        const minted = _mintedGate(key, d);
        if (minted === 'wait') { outcome = 'after'; break; }
        if (minted === 'left') { i++; continue; }
        if (d.kind === 'evt') {
            const session = d.payload && d.payload.session;
            const batch = [];
            while (i < list.length && batch.length < TAP_BATCH_MAX) {
                const [k2, d2] = list[i];
                if (_writeQueue.get(k2) !== d2) { i++; continue; }
                if (d2.kind !== 'evt' || d2.authBlocked || d2.parked
                    || (d2.payload && d2.payload.session) !== session) break;
                // The batch ends at the first tap that waits (J1, stage 3
                // review 6): one on a wait cycle, still following an entry
                // that is queued, or naming a member not known to exist goes
                // in a later pass, after that entry.
                if (batch.length > 0 && (d2.cycled || _afterKeys(d2).some((k) => _writeQueue.has(k)))) break;
                const minted2 = _mintedGate(k2, d2);
                if (minted2 === 'left') { i++; continue; }
                if (minted2 === 'wait') break;
                batch.push(list[i]);
                i++;
            }
            if (batch.length === 0) { outcome = 'after'; break; }
            outcome = await _sendTapBatch(base, batch, pass);
        } else {
            outcome = await _sendEntry(key, d, pass);
            i++;
        }
        if (outcome !== 'settled') break;
    }
    if (outcome === 'settled') return outcome;
    let marked = false;
    for (const [key, d] of list) {
        if (_writeQueue.get(key) === d && !d.held && outcome === 'kept') { d.held = true; marked = true; }
    }
    // An entry of another base waiting on one just marked held is held with
    // it (F6): its own base may have had its turn earlier in this pass.
    if (marked && _markWaitersHeld()) marked = true;
    if (marked) _recomputeSyncStatus();
    return outcome;
}

// _blockedByHeld: whether the entry `key` names in `after` is stuck: held after
// the SERVER refused it (a 5xx, 429 or 403: `attempts`), parked, or waiting on
// a password, directly or through the entry it in turn waits on. One held only
// because the connection is down is not: the whole device is offline then, and
// waiting is not failing (outbox_running_and_after.test.jsx).
function _blockedByHeld(key, index = _queueIndex()) {
    return !!_blockerOf(key, _isStuckEntry, _newWalk(), index);
}

// _isStuckEntry: an entry that cannot go for now for a reason other than the
// connection: the server refused it (a 5xx, 429 or 403: `attempts`, once a pass
// has held it), the server keeps refusing it (_refusedByServer: parked, held
// for the clock, past the notice threshold), or waiting on a password.
function _isStuckEntry(d) {
    return !!d && (_refusedByServer(d) || !!d.authBlocked || !!d.cycled || (!!d.held && (Number(d.attempts) || 0) > 0));
}

// _markWaitCycles: marks `cycled` every entry on a wait cycle, and clears it
// on every other (stage 3 review 5, H1). An entry waits on what it names in
// `after` and on the entry before it in its own base (strict order), so a
// cycle of any length, across any number of matches, is entries none of which
// can ever be sent: nothing the server answers frees one. Such an entry reads
// as stuck (_isStuckEntry, _keepsFailing), so its match's banner names it
// and an operator's Discard breaks the cycle; the coalescing rule never makes
// one (_coalesceTarget), but a queue an earlier build stored can hold one.
// `byBase` is the pass's grouping, each base in replay order. Strongly
// connected components (Tarjan); returns whether any flag moved.
function _markWaitCycles(byBase) {
    const prev = new Map();
    for (const list of byBase.values()) {
        for (let i = 1; i < list.length; i++) prev.set(list[i][0], list[i - 1][0]);
    }
    const next = (k) => {
        const out = _afterKeys(_writeQueue.get(k)).filter((a) => _writeQueue.has(a));
        if (prev.has(k)) out.push(prev.get(k));
        return out;
    };
    const onCycle = new Set();
    const order = new Map();
    const low = new Map();
    const stack = [];
    const onStack = new Set();
    let n = 0;
    // Iterative, with an explicit stack of frames: a base's chain is as long as
    // its queue, so recursing once per entry overflowed the call stack on a
    // long single-match queue (J6, stage 3 review 6).
    const strong = (root) => {
        const frames = [];
        const open = (v) => {
            order.set(v, n); low.set(v, n); n++;
            stack.push(v); onStack.add(v);
            frames.push({ v, out: next(v), i: 0 });
        };
        open(root);
        while (frames.length > 0) {
            const f = frames[frames.length - 1];
            const v = f.v;
            if (f.i < f.out.length) {
                const w = f.out[f.i++];
                if (!order.has(w)) open(w);
                else if (onStack.has(w)) low.set(v, Math.min(low.get(v), order.get(w)));
                continue;
            }
            frames.pop();
            if (low.get(v) === order.get(v)) {
                const component = [];
                let w;
                do { w = stack.pop(); onStack.delete(w); component.push(w); } while (w !== v);
                if (component.length > 1 || f.out.includes(v)) component.forEach((k) => onCycle.add(k));
            }
            if (frames.length > 0) {
                const parent = frames[frames.length - 1].v;
                low.set(parent, Math.min(low.get(parent), low.get(v)));
            }
        }
    };
    for (const k of _writeQueue.keys()) if (!order.has(k)) strong(k);
    let changed = false;
    for (const [k, d] of _writeQueue) {
        if (!d) continue;
        if (onCycle.has(k) && !d.cycled) { d.cycled = true; changed = true; }
        else if (!onCycle.has(k) && d.cycled) { delete d.cycled; changed = true; }
    }
    return changed;
}

// _afterKeys: the queue keys an entry follows (`after`): one key, stored as a
// string, or several (a score write naming two teams' new members), stored as
// an array.
export function _afterKeys(d) {
    if (!d) return [];
    if (Array.isArray(d.after)) return d.after.filter((k) => typeof k === 'string' && k);
    return typeof d.after === 'string' && d.after ? [d.after] : [];
}

// _addAfter: makes the entry follow `key` as well as what it already follows.
export function _addAfter(d, key) {
    if (!d || typeof key !== 'string' || !key) return;
    const keys = _afterKeys(d);
    if (keys.includes(key)) return;
    keys.push(key);
    d.after = keys.length === 1 ? keys[0] : keys;
}

// _blockerOf: the key of the entry that holds `key` back and matches `stuck`:
// the entry itself, an earlier entry of its own base (strict order: it goes
// only after them, stage 3 review 4, G3), or anything an entry on that path
// follows (`after`), followed as far as it goes; '' when none.
export function _blockerOf(key, stuck, walk = _newWalk(), index = _queueIndex()) {
    if (!key || walk.seen.has(key)) return '';
    walk.seen.add(key);
    const d = _writeQueue.get(key);
    if (!d) return '';
    if (stuck(d, key)) return key;
    // Every earlier entry of a base is walked once per walk (stage 3 review
    // 5, H2): what an earlier entry waits on, a later one waits on too.
    const base = _baseOf(key);
    const list = index.byBase.get(base) || [];
    const at = index.pos.has(key) ? index.pos.get(key) : list.length;
    for (let i = walk.scanned.get(base) || 0; i < at; i++) {
        walk.scanned.set(base, i + 1);
        const found = _blockerOf(list[i][0], stuck, walk, index);
        if (found) return found;
    }
    for (const k of _afterKeys(d)) {
        const found = _blockerOf(k, stuck, walk, index);
        if (found) return found;
    }
    return '';
}

// _queueIndex / _newWalk: what one question about what holds writes back
// reads (stage 3 review 5, H2). The index is the queue grouped by base, each
// base in replay order with each key's place in it, built with ONE sort; a
// walk is the entries it has visited and how far into each base it has read,
// shared by every start of ONE question, which stops at the first blocker it
// finds (a fresh walk per question: an entry left on a found path was not
// fully read).
export function _queueIndex() {
    const byBase = new Map();
    for (const entry of [..._writeQueue.entries()].filter(([, d]) => d).sort(_byEntryOrder)) {
        const base = _baseOf(entry[0]);
        if (!byBase.has(base)) byBase.set(base, []);
        byBase.get(base).push(entry);
    }
    const pos = new Map();
    for (const list of byBase.values()) list.forEach(([k], i) => pos.set(k, i));
    return { byBase, pos };
}
export function _newWalk() {
    return { seen: new Set(), scanned: new Map() };
}

// _markWaitersHeld: marks held every queued entry that waits (`after`) on one
// that cannot go for now (_blockedByHeld), and every entry of its base behind
// it, as a failure of its own would. True when any was marked.
function _markWaitersHeld() {
    let marked = false;
    const blockedBases = new Set();
    const index = _queueIndex();
    for (const [key, d] of _writeQueue) {
        if (_afterKeys(d).some((k) => _writeQueue.has(k) && _blockedByHeld(k, index))) blockedBases.add(_baseOf(key));
    }
    if (blockedBases.size === 0) return false;
    for (const [key, d] of _writeQueue) {
        // Never an entry whose request is out: the matches flush side by
        // side, and one marked held while out would count as a held write
        // that landed (`pass.sent`) when its answer comes.
        if (!d.held && !_inFlight.has(d) && blockedBases.has(_baseOf(key))) { d.held = true; marked = true; }
    }
    return marked;
}

function _doorRefusal(d, status, body) {
    const b = body || {};
    const ineligible = b.error === 'ineligible_competitor' || b.error === 'already_ineligible';
    switch (d && d.kind) {
        case 'score':
            if (ineligible) return _reasonHumanError(b, 'Failed to record score');
            return _courtBusyError(b) || _downstreamRefusalError(b) || _leagueFinishedError(b) || _lotsReplacedError(b) || new Error(_refusalText(b, 'Failed to record score'));
        case 'decision':
            if (ineligible) return _reasonHumanError(b, 'Failed to record decision');
            return _downstreamRefusalError(b) || _leagueFinishedError(b) || _lotsReplacedError(b) || new Error(b.error || 'Failed to record decision');
        case 'reopen':
        case 'requeue':
            return _reopenRefusal(b);
        case 'revert':
            return _refusalError(b, 'Failed to send match back to queue');
        case 'kachinuki':
            return new Error(b.error || 'Failed to remove bout');
        case 'daihyosen':
            return new Error(_refusalText(b, d.method === 'DELETE' ? REP_BOUT_NOT_REMOVED : REP_BOUT_NOT_ADDED));
        case 'lineup':
            return _refusalError(b, d.payload && d.payload.matchId ? 'Failed to save match lineup' : 'Failed to save lineup');
        case 'court':
            return _refusalError(b, 'Failed to move match court');
        case 'time':
            return new Error(b.error || 'Failed to update match time');
        case 'member': {
            // R1: the member as it was comes back with the refusal, stamped
            // after the change it undoes, so an editor that showed the change
            // puts it back (mergeMembers keeps the later stamp).
            const err = _refusalError(b, d.method === 'POST' ? TEAM_MEMBER_NOT_ADDED
                : d.method === 'DELETE' ? TEAM_MEMBER_NOT_CLEARED : TEAM_MEMBER_NOT_RENAMED);
            if (d.was && typeof d.was === 'object') err.revertMember = { ...d.was, modifiedAt: _serverNowMs() };
            // Whether a refused add's member never was is the owner's to judge
            // (minted_members.jsx `answered`, told by _mintedAddAnswered: it
            // reads member_id_conflict as held, not refused), and the editors
            // hear it by `team-member-dropped`, never from the refusal (N6).
            return err;
        }
        case 'lineup-remove':
            return _refusalError(b, LINEUP_NOT_REMOVED);
        case 'reinstate':
            return new Error(b.error || 'Failed to reinstate competitor');
        case 'pool-ranks': {
            // A chusen's order that moves a qualifier out of a knockout match
            // already fought: the structured refusal attemptScoreWrite asks
            // the operator about, marked as a ranking for the dialog's copy.
            const refusal = _downstreamRefusalError(b);
            if (refusal && refusal.downstreamKnockoutPlayed) refusal.downstreamKnockoutPlayed.ranking = true;
            return refusal || new Error(b.error || 'Failed to override rank');
        }
        case 'checkin':
            return _refusalError(b, d.method === 'DELETE' ? 'The check-in was not taken back.' : 'The check-in was not saved.');
        case 'checkin-bulk':
            return _refusalError(b, 'The check-ins were not saved.');
        case 'announce':
            return _refusalError(b, 'The court call was not sent.');
        case 'announce-remove':
            return _refusalError(b, 'The court call was not taken down.');
        case 'announce-clear':
            return _refusalError(b, 'The court calls were not cleared.');
        case 'tournament':
            return tournamentRefusal(b, status);
        case 'awards':
            return awardsRefusal(b, status);
        default:
            return new Error(_refusalText(b, `HTTP ${status}`));
    }
}

// _sendEntry: one queued write (not a tap). 'settled' when it left the queue
// (sent, or refused for good), 'reclocked' when it was re-stamped for a clock
// refusal and goes again on the pass's rerun, 'restamped' when another copy
// took its key while it was out (a later write joined into it, or another
// tab's newer copy taken up from storage) and goes straight after it, 'kept' when it
// waits for a later pass.
// _answersMatchWrite: whether an entry sent with its own method and url is a
// MATCH write, whose answer can be a refusal body (`applied: false`, a clock
// refusal) read by the branch it gates in _sendEntry. A lineup save, a team
// member write, a lineup removal and a reinstatement answer their own records,
// never a match write's refusal shape; so do a check-in, a court call, its
// withdrawal, the clear and the two document saves (bc-obox).
const _NOT_MATCH_WRITE_KINDS = new Set([
    'lineup', 'lineup-remove', 'member', 'reinstate', 'pool-ranks',
    'checkin', 'checkin-bulk', 'announce', 'announce-remove', 'announce-clear', 'tournament', 'awards',
]);
function _answersMatchWrite(d) {
    return !!d && !!d.terminal && !_NOT_MATCH_WRITE_KINDS.has(d.kind);
}
// The doors outside a match whose body carries its stamp (`modifiedAt`), which
// the server judges for clock skew: a 200 {applied:false, reason:"clock_skew"}
// wrote nothing, and the entry is re-stamped and sent again (_resendForClock).
const _STAMPED_DOOR_KINDS = new Set(['checkin', 'checkin-bulk', 'announce', 'announce-clear']);
// The removals whose 404 is the outcome they asked for (nothing to take away).
export const _GONE_IS_DONE_KINDS = new Set(['lineup-remove', 'announce-remove']);

async function _sendEntry(key, descriptor, pass) {
    // Out of the queue AND out of storage at once, not at the end of the
    // pass: a page that goes away while a later entry is still out (a reload,
    // the tab closed) would leave this one stored, and the next load would
    // send it again. The door's promise settles with it (outcome).
    const leaveQueue = (outcome) => {
        if (!_dequeue(key, descriptor, outcome)) return false;
        _persistQueue();
        return true;
    };
    const { compID, matchID, payload, password, terminal, kind, method, url } = descriptor;
    // Running score writes (terminal=false) don't store method/url and
    // always PUT to the score endpoint. Terminal entries carry their own
    // method+url, which may have been rehydrated from (tamperable)
    // localStorage: validate against the allowlist and DROP anything outside
    // it rather than flush to an unexpected route or method.
    let effectiveMethod, effectiveUrl;
    if (terminal) {
        if (!_isAllowedTerminalRequest(method, url)) {
            console.warn(`[sync] dropping terminal ${kind || ''} write with disallowed method/url:`, method, url);
            // Undeliverable by construction: said by the editor open on this
            // match, else announced globally (_tellRefusal).
            _tellRefusal(
                { compID, matchID, kind, status: 0, reason: 'corrupted queue entry' },
                { kind: 'unreadable', count: 1, terminalCount: isFinishedResultWrite(descriptor) ? 1 : 0, compID, matchID },
            );
            leaveQueue({ error: new Error('corrupted queue entry') });
            return 'settled';
        }
        effectiveMethod = method;
        effectiveUrl = url;
    } else {
        effectiveMethod = 'PUT';
        effectiveUrl = `/api/competitions/${compID}/matches/${matchID}/score`;
    }
    let res, body;
    // A member add that goes out may be one the server keeps, whatever comes
    // of the request: the owner holds it out from this tab until its answer
    // (minted_members.jsx), every tab reading it so.
    const mintsId = mintedAddId(descriptor);
    if (mintsId) {
        _noteMintedOutHere(mintsId);
        _minted.sent(descriptor);
    }
    _inFlight.add(descriptor);
    try {
        // Bounded, the body read included (_fetchJson): this loop is
        // single-flight, so one replay whose body never completed would stop
        // every later flush. Past the deadline it is a network failure.
        ({ res, body } = await _fetchJson(effectiveUrl, {
            method: effectiveMethod,
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            // Sent as the entry holds it: a downstream confirmation rides only
            // on the in-memory copy of a write confirmed on this page
            // (_persistQueue never stores one). A tournament settings save
            // never carries a password (_replayBody).
            body: _replayBody(effectiveMethod, effectiveUrl, payload),
        }));
    } catch (err) {
        // fetch rejected (network down), or the request or its body was given
        // up at the deadline (_fetchJson).
        pass.anyFailed = true;
        pass.networkFailed = true;
        _inFlight.delete(descriptor);
        if (mintsId) _endMintedOutHere(mintsId);
        _mintedAddAnswered(key, descriptor, null, null, !!(err && err.timedOut));
        _endDiscardWhileOut(descriptor, null, null);
        return 'kept';
    } finally {
        _inFlight.delete(descriptor);
        if (mintsId) _endMintedOutHere(mintsId);
    }
    _mintedAddAnswered(key, descriptor, res, body || {});
    // Discarded while it was out (L1): the answer settles only what the
    // discard left waiting.
    if (_endDiscardWhileOut(descriptor, res, body)) return 'settled';
    body = body || {};
    if (res.ok) {
        // An answer that carries the match names the taps the server turned
        // away since their own answer: this device's are told.
        if (compID && matchID) _tellTurnedAway(compID, matchID, body.result || body);
        // A lineup change the server kept whose carried lineups could not all
        // follow it says so: told to the operator, never only logged (stage 6
        // review 8, m7). So does a score or decision write that started a
        // kachinuki match and kept its lineup (review 9, G1).
        if (typeof body.warning === 'string' && body.warning) {
            _notifyQueueAlert({ kind: 'lineup_warning', count: 1, terminalCount: 0, compID, matchID, detail: body.warning });
        }
        // Success (HTTP 200/201, including a write the server kept in the
        // match's history instead of applying, applied:false).
        if (_answersMatchWrite(descriptor)) {
            if (writeWasRefusedForClock(body)) {
                // bc-cse: NOT a supersede. The stamp this replay carries is in
                // the server's future; nothing newer won. Heal the offset and
                // RECONSTRUCT the stamp, then resend. Never dropped: the server
                // logged nothing, so a refusal again keeps it queued and held
                // (_keepForClock), one behaviour for every write and tap.
                return _resendForClock(key, descriptor, pass, {
                    count: 1, terminalCount: isFinishedResultWrite(descriptor) ? 1 : 0, compID, matchID,
                });
            }
            if (body.applied === false) {
                // bc-cse: our stamp lost either way, so re-check our clock frame.
                _relearnClockThrottled();
                if (_writeQueue.get(key) === descriptor) {
                    // bc-lww1: the operator's own entry, kept in the match's
                    // history, not applied. Said by the editor open on the
                    // match, else counted into the pass's one superseded alert.
                    const shown = _notifyScoreSupersededEditor(compID, matchID, body);
                    if (!shown) {
                        if (_heldForClosedMatch(body)) pass.matchClosed = true;
                        pass.superseded++;
                        pass.lastSuperseded = { compID, matchID };
                    }
                }
            }
        } else if (!terminal && kind === 'score') {
            // bc-cse MEDIUM-2: a RUNNING write settles with the server's answer
            // (its editor reads `settled`) and raises no alert of its own,
            // except the clock-skew refusal, where res.ok means nothing was
            // written: it is re-stamped and resent in its place, since the
            // entry may hold an edge-triggered kachinukiBoutFinal.
            if (writeWasRefusedForClock(body)) {
                return _resendForClock(key, descriptor, pass, { count: 1, terminalCount: 0, compID, matchID });
            }
            if (body.applied === false) {
                // Kept in the match's history, not applied: our stamp lost, so
                // re-check our clock frame. The editor that sent it reads the
                // answer from `settled`; no alert (an autosave's would repeat).
                _relearnClockThrottled();
            }
        } else if (_STAMPED_DOOR_KINDS.has(kind)) {
            // A check-in or a court call the server judged as stamped in its
            // future wrote nothing: re-stamped and sent again, as a match
            // write is (bc-obox).
            if (writeWasRefusedForClock(body)) {
                return _resendForClock(key, descriptor, pass, { count: 1, terminalCount: 0, compID, matchID });
            }
            // A check-in another desk's later tap superseded: the desk takes
            // the participant as stored, told once when it is not what the
            // tap asked (C2).
            if (_CHECKIN_KINDS.has(kind) && _writeQueue.get(key) === descriptor) _checkInAnswered(key, descriptor, body);
        }
        // A change turned away because the match was finished (writeMatchClosed)
        // is the operator's to correct, never routine noise, so it is said
        // whatever the write (a superseded Finish was counted above; never a
        // displaced answer: that finish was recorded).
        if (kind === 'score' && writeMatchClosed(body) && !(terminal && writeWasSuperseded(body))
            && _writeQueue.get(key) === descriptor
            && !_notifyScoreSupersededEditor(compID, matchID, body)) {
            pass.superseded++;
            pass.matchClosed = true;
            pass.lastSuperseded = { compID, matchID };
        }
        const wasHeld = !!descriptor.held;
        if (leaveQueue({ value: _doorValue(descriptor, body) })) {
            // A held result that landed (writeWasSuperseded is every
            // applied:false answer; those were announced above).
            // Counted as results only when it is one (isFinishedResultWrite, R5):
            // a held court move or lineup save that lands is no "result sent".
            if (isFinishedResultWrite(descriptor) && wasHeld && !writeWasSuperseded(body)) pass.sent++;
            if (isFinishedResultWrite(descriptor) && writeDisplacedGroups(body).length > 0) pass.displaced++;
            // A confirmed terminal score write needs no further rev tracking.
            if (terminal && kind === 'score') _forgetRev(compID, matchID);
            // A document save that landed: a save of it still queued at the
            // version this one was sent at takes the one it answered (G4b).
            const docField = DOC_VERSION_FIELD[kind];
            if (docField) rebaseQueuedVersion(_baseOf(key), docField, payload && payload[docField], body[docField]);
        } else if (_writeQueue.has(key)) {
            // A later write took this one's place while it was out (the same
            // key, _placeWrite): it goes before anything queued after it.
            _flushRequested = true;
            return 'restamped';
        }
        return 'settled';
    }
    if (res.status === 404 && _GONE_IS_DONE_KINDS.has(kind)) {
        // Nothing saved to remove: the outcome the removal asked for (R1). A
        // court call already gone (expired, or another console withdrew it)
        // is taken down too (bc-cdbl).
        leaveQueue({ value: true });
        return 'settled';
    }
    if (_serverKeepsRetrying(res.status)) {
        // Server is up but erroring: keep in queue and retry with backoff. NOT
        // "offline". 403 is never a bad credential on this server (that is
        // 401): it is the tournament record not usable yet, which an admin can
        // repair at any moment, so a plain retry is what discovers the repair.
        // bc-qttl: NEVER dropped here; on crossing the notice threshold we
        // announce once and the pill moves onto 'server-error'.
        const rejections = (Number(descriptor.attempts) || 0) + 1;
        descriptor.attempts = rejections;
        if (rejections === SERVER_REJECTION_NOTICE_THRESHOLD) {
            console.warn(`[sync] queued ${kind || 'running'} write still failing after ${rejections} server rejections (${res.status}); keeping it queued:`, body);
            _notifyQueueAlert({
                kind: 'server_error', count: 1, terminalCount: isFinishedResultWrite(descriptor) ? 1 : 0, compID, matchID,
                detail: body.reasonHuman || body.error || `HTTP ${res.status}`,
            });
            // Published at once (the failing count moved), so this match's
            // editor and the topbar's list offer the discard now.
            _recomputeSyncStatus();
        }
        pass.anyFailed = true;
        return 'kept';
    }
    if (res.status === 401) {
        // bc-qttl: an auth failure PARKS the write, it does not drop it; a
        // parked entry is skipped until API.resumeAfterAuth() re-stamps it, so
        // this branch deliberately does NOT set anyFailed.
        if (!descriptor.authBlocked) {
            console.warn(`[sync] parking queued ${kind || 'running'} write pending re-auth (${res.status}):`, body);
            descriptor.authBlocked = true;
            _notifyQueueAlert({
                kind: 'auth_required', count: 1, terminalCount: isFinishedResultWrite(descriptor) ? 1 : 0, compID, matchID,
                detail: body.reasonHuman || body.error || `HTTP ${res.status}`,
            });
        }
        return 'kept';
    }
    // Non-retryable: 400 validation, 413, a 409 conflict. A write the server
    // refused on its merits and would refuse identically forever, so it leaves
    // the queue (the one unconfirmed exit) and the refusal is REPORTED, never
    // taken as success (bc-mrgc phase 3): a decision's 409 too, since the
    // server answers an exact replay of a recorded decision as recorded
    // (engine.exactDecisionReplay). A caller that claims the refusal
    // (onRefusal, e.g. the downstream confirm in attemptScoreWrite) shows it
    // itself; otherwise the editor open on the match says it, else the global
    // alert does (_tellRefusal), with downstreamKnockoutPlayedQueueDrop's words for a knockout
    // correction whose later match was played meanwhile.
    console.warn(`[sync] queued ${kind || 'running'} write rejected (${res.status}):`, body);
    const err = _doorRefusal(descriptor, res.status, body);
    // A document save refused as made against an older copy: a save of the
    // same document queued behind it at the same version would be refused
    // the same way, so it leaves with it, and the operator is told once.
    const staleDocs = _staleDocumentSaves(key, descriptor, err);
    const claimed = [key, ...staleDocs.map(([k]) => k)].map((k) => _claimRefusal(k, err)).some(Boolean);
    if (!claimed) {
        const refusal = _replayRefusal(body, kind === 'decision' && res.status === 409 ? 'conflict (409)' : `HTTP ${res.status}`);
        const alert = {
            kind: 'rejected', count: 1, terminalCount: isFinishedResultWrite(descriptor) ? 1 : 0, compID, matchID,
            detail: refusal.reason,
            ...(refusal.sentence ? { sentence: true } : {}),
            // The document refusals have their own words (app.jsx
            // queueAlertMessage): open the settings and save again.
            ...(err.code && DOC_VERSION_FIELD[kind] ? { code: err.code } : {}),
        };
        // Said once: by the editor open on the match (a terminal write's
        // banner), else by the alert.
        if (terminal) _tellRefusal({ compID, matchID, kind, status: res.status, ...refusal }, alert);
        else _notifyQueueAlert(alert);
    }
    for (const [k, d] of staleDocs) _dequeue(k, d, { error: err });
    if (!leaveQueue({ error: err }) && _writeQueue.has(key)) {
        _flushRequested = true;
        return 'restamped';
    }
    return 'settled';
}

// _staleDocumentSaves: the saves of the same document still queued behind a
// document save refused as stale (tournament_changed, awards_changed) at the
// version it was refused for, or [] for any other refusal.
function _staleDocumentSaves(key, d, err) {
    const field = d && DOC_VERSION_FIELD[d.kind];
    if (!field || !err || err.code !== `${d.kind}_changed`) return [];
    const at = d.payload && d.payload[field];
    return _entriesFor(_baseOf(key)).filter(([k, e]) => k !== key && e && !_inFlight.has(e)
        && e.kind === d.kind && e.payload && e.payload[field] === at);
}

// _askAgainWhenLeaseEnds: a holder that is gone (its page closed without a
// pagehide, or crashed) wakes nobody, so this tab asks once the lease it found
// would have run out. One timer a base: a holder that is alive beats and the
// ask finds the lease held again, and a holder that finished woke this tab.
const _askingFor = new Set();
function _askAgainWhenLeaseEnds(base) {
    if (_askingFor.has(base)) return;
    _askingFor.add(base);
    setTimeout(() => {
        _askingFor.delete(base);
        if (_writeQueue.size > 0) _rerunFlushSoon();
    }, _baseLease.expiresIn(base) + 50);
}

// _flushAdoptedSoon: the flush for entries another tab made. Its storage event
// reaches this tab a moment BEFORE the lease that tab takes for the base it is
// about to send (base_sender_lease.jsx), so a flush made at once would find the
// base free and send it too. Waiting ADOPTED_FLUSH_DELAY_MS lets the lease show;
// a tab that is gone holds none, and its entries go once the wait is over.
export function _flushAdoptedSoon() {
    _resetFlushBackoff();
    setTimeout(() => { if (_writeQueue.size > 0) _flushQueue(); }, ADOPTED_FLUSH_DELAY_MS);
}

// _rerunFlushSoon: a flush now, or a rerun of the one under way.
export function _rerunFlushSoon() {
    if (_flushInProgress) { _flushRequested = true; _wakeIdleBases(); }
    else queueMicrotask(() => { if (_writeQueue.size > 0) _flushQueue(); });
}

// _wakeIdleBases: asks the running pass, in a microtask (never inside the
// enqueue or settle that asks), to start the bases a new or freed entry needs.
function _wakeIdleBases() {
    queueMicrotask(() => { if (_launchIdleBases) _launchIdleBases(); });
}
