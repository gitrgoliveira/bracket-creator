// Taps: the event session and seq, landed and told taps, turned-away telling,
// pending taps and the tap batch send.
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import { tapOutcomeQuiet } from './write_result.jsx';
import { CLOCK_SKEW as TAP_CLOCK_SKEW } from './score_events.jsx';
import { createLandedTaps } from './landed_taps.jsx';
import { createToldTaps } from './told_taps.jsx';
import { _randomId } from './api_client_ids.jsx';
import { _replayRefusal, _serverKeepsRetrying, _fetchJson } from './api_client_requests.jsx';
import {
    _learnServerClockOffset, _stampAfterSeen, CLOCK_SKEW_FAIL_REASON, _afterSettledStamp, _restampFor,
} from './api_client_clock.jsx';
import {
    _revKey, _isAllowedTerminalRequest, _notifyQueueAlert, _persistQueue, _writeQueue, _dequeue,
    _openDequeueBatch, _closeDequeueBatch, _baseOf, _entriesFor, _keepForClock,
} from './api_client_queue.jsx';
import {
    _shownMatchLabels, _whereOf, _recomputeSyncStatus, SERVER_REJECTION_NOTICE_THRESHOLD, _noteNotAppliedTap,
} from './api_client_status.jsx';
import { _latestDerived, _turnedAwaySeen, store, provide } from './api_client_store.jsx';

// ---------------------------------------------------------------------------
// bc-evts stage 3: the outbox. EVERY rule-bound write (a tap, a Finish, a
// decision, a reopen, a send back, the representative bout's
// add and remove, a kachinuki bout removal) is put in the persisted queue in
// the SAME TICK it is made and the caller carries on at once: nothing in front
// of an editor waits on the network (operator rule, 2026-10-16). Each match's
// entries are then sent strictly in order (_flushQueue, F1), so a write never
// overtakes one made before it on this device.
// ---------------------------------------------------------------------------
// The device's EVENT session: random per page load. Not _revSession, which
// orders running snapshot writes: a tap's id is `${session}.${seq}` and the
// server counts the session's seqs to report the taps it never got (gaps).
export const _eventSession = _randomId();
export const TAP_BATCH_MAX = 64;
export const BASIS_OWN_MAX = 1024;
// Per match (base): the last seq this session queued, and the last stamp this
// device gave any write of that match, so a later write is never stamped
// before an earlier one (the replay orders by stamp).
export const _tapSeq = new Map();
export const _derivedListeners = new Map();
export const _pendingTapListeners = new Map();
export const _tapOutcomeListeners = new Map();
// The pending taps the board already told as not applied (tellPendingTapRefused)
// are the DEVICE's record, told_taps.jsx (`_told`, made beside the landed-tap
// record below; stage 6 review 9, F4): the tab whose batch the answer is knows
// what another tab's board told. `_toldHere` is what this tab's own listeners
// were told, so a board re-render tells them nothing twice.
const _toldHere = new Set();
// _pruneToldOnBoard keeps only the ids of taps this tab still holds queued:
// one no longer queued (answered, or taken by another tab) has nothing left
// to tell (stage 6 review 8, m12).
function _queuedTapIds() {
    const queued = new Set();
    for (const d of _writeQueue.values()) {
        if (d && d.kind === 'evt' && d.payload && d.payload.id) queued.add(d.payload.id);
    }
    return queued;
}
export function _pruneToldOnBoard() {
    const queued = _queuedTapIds();
    for (const id of _toldHere) if (!queued.has(id)) _toldHere.delete(id);
    _told.prune((id) => queued.has(id));
}
// _forgetToldHere: the device's record no longer holds these taps (another
// tab settled them, or the sweep took them out). A tap this tab still holds
// queued stays told here until it leaves this tab's queue (_dequeue): the
// tab that settled it stores the record before the queue, so a board render
// between the two still shows it pending and told it again (stage 6 review
// 11, minor 2).
function _forgetToldHere(ids) {
    let queued = null;
    for (const id of ids) {
        if (!_toldHere.has(id)) continue;
        if (!queued) queued = _queuedTapIds();
        if (!queued.has(id)) _toldHere.delete(id);
    }
}

// The tap state above (the seqs, the copies seen, the taps told here) is
// written by the tap code alone; the queue, the shown-match labels and the
// tap, sign-out and told doors go through these (bc-jsxs).
// _nextTapSeq: the next seq of this session's taps of the match (recordTap).
export function _nextTapSeq(base) {
    const seq = (_tapSeq.get(base) || 0) + 1;
    _tapSeq.set(base, seq);
    return seq;
}
// _claimToldHere: false when this tab's listeners were already told of the
// tap, else it is noted as told now (tellPendingTapRefused).
export function _claimToldHere(id) {
    if (_toldHere.has(id)) return false;
    _toldHere.add(id);
    return true;
}
// _dropToldHere: a tap that left this tab's queue is no longer on a board
// (_dequeue, minor 2).
export function _dropToldHere(id) {
    _toldHere.delete(id);
}
// _forgetTurnedAwayAndTold / _forgetDerivedCopies: the sign-out (clearQueue).
export function _forgetTurnedAwayAndTold() {
    _turnedAwaySeen.clear();
    _toldHere.clear();
    _told.forget();
}
export function _forgetDerivedCopies() {
    _latestDerived.clear();
}

// _onToldTapsStored: another tab changed a match's told-tap record. A notice
// it took back takes this tab's note down too (F4), and when this is the tab
// that told it (or a shown tab, that one hidden or gone) the operator is told
// "applied after all" here, once for the device (stage 6 review 10, m1: only
// the tab whose batch found it applied said so, and the tab whose board had
// said "Not applied" just lost its note). An entry the other tab took out is
// forgotten here too (m7), and the taps it took back are one alert per match.
export function _onToldTapsStored(key, oldValue, newValue) {
    const alerts = new Map();
    for (const t of _told.onStored(key, oldValue, newValue)) {
        _toldHere.delete(t.id);
        const base = _revKey(t.compID, t.matchID);
        _emitOn(_tapOutcomeListeners, base, { id: t.id, outcome: 'applied', takenBack: true });
        if (_landed.tellsFor(_told.tellerOf(t.compID, t.matchID, t.id)) && _told.claimAlert(t.compID, t.matchID, t.id)) {
            const a = alerts.get(base) || { compID: t.compID, matchID: t.matchID, count: 0 };
            a.count++;
            alerts.set(base, a);
        }
    }
    _forgetToldHere(_told.goneOnStored(key, oldValue, newValue).map((t) => t.id));
    for (const [base, a] of alerts) {
        _notifyQueueAlert({
            kind: 'tap_applied', count: a.count, terminalCount: 0, compID: a.compID, matchID: a.matchID,
            where: _shownMatchLabels.get(base) || '',
        });
    }
}
// _notTurnedAway: the newest copy of the match seen names a version and does
// not list the tap turned away. A `duplicate` answer says only that the tap's
// id is already in the log, where a tap turned away is logged too, so it takes
// a board's notice back only on this word (stage 6 review 9, F3).
function _notTurnedAway(base, id) {
    const seen = _turnedAwaySeen.get(base);
    return !!seen && Number.isFinite(seen.v) && !(seen.away && Object.prototype.hasOwnProperty.call(seen.away, id));
}
// The board's notices raised in one tick go out as ONE alert per match (F4:
// a removal reaching a board with several pending taps on the bout gave one
// identical toast per tap).
const _boardToldBatch = new Map();
export function _queueBoardTold(compID, matchID, outcome, tap, alert) {
    const base = _revKey(compID, matchID);
    let b = _boardToldBatch.get(base);
    if (!b) {
        b = { compID, matchID, outcomes: [], alerted: [] };
        _boardToldBatch.set(base, b);
        queueMicrotask(() => {
            _boardToldBatch.delete(base);
            for (const o of b.outcomes) _emitOn(_tapOutcomeListeners, base, o);
            if (b.alerted.length > 0) {
                const [first] = b.alerted;
                _notifyQueueAlert({
                    kind: 'not_applied', count: b.alerted.length, terminalCount: 0, compID, matchID,
                    outcome: first.outcome, where: _shownMatchLabels.get(base) || '',
                    tap: b.alerted.length === 1 ? first.tap : null,
                });
            }
        });
    }
    b.outcomes.push(outcome);
    if (alert) b.alerted.push({ outcome, tap: tap || null });
}

export function _listenOn(map, base, fn) {
    if (typeof fn !== 'function') return () => {};
    let set = map.get(base);
    if (!set) { set = new Set(); map.set(base, set); }
    set.add(fn);
    return () => {
        set.delete(fn);
        if (set.size === 0 && map.get(base) === set) map.delete(base);
    };
}
function _emitOn(map, base, value) {
    const set = map.get(base);
    if (!set) return;
    for (const fn of set) {
        // One listener that throws must not stop the others hearing it.
        try { fn(value); } catch (e) { console.error(`[sync] a listener for ${base} threw:`, e); }
    }
}

// _tellTapNotQueued (bc-sseq FR9): a tap the court console's write gate
// refused before it was queued (_commitEnqueue). A tap is fire and forget,
// so its editor hears of it where it hears of every tap outcome
// (subscribeTapOutcome), with the gate's reason, worded by tapNotAppliedText
// and shown beside the board. Its seq is given back when it was the last one
// handed out, so the server never waits for a tap that was never sent.
export function _tellTapNotQueued(base, payload, reason) {
    const id = payload && payload.id;
    if (!id) return;
    const seq = Number(payload.seq);
    if (payload.session === _eventSession && seq > 0 && _tapSeq.get(base) === seq) _tapSeq.set(base, seq - 1);
    _emitOn(_tapOutcomeListeners, base, { id, outcome: 'notQueued', reason });
}

// The taps this device holds for a match, in the order they are sent.
export function _tapEntriesFor(base) {
    return _entriesFor(base).filter(([, d]) => d.kind === 'evt');
}
export function _pendingTapsFor(base) {
    // A parked tap (the server refused its batch) is not counted on the board
    // or the TV: it is held until an operator discards it, and the server
    // will never apply it as it stands.
    return _tapEntriesFor(base).filter(([, d]) => !d.parked).map(([, d]) => ({ ...d.payload }));
}
export function _publishPendingTaps(base) {
    if (!_pendingTapListeners.has(base)) return;
    _emitOn(_pendingTapListeners, base, _pendingTapsFor(base));
}

// _noteDerived keeps the newest derived match per base, by scoreVersion (it
// only grows), and tells whoever follows that match.
function _noteDerived(base, match) {
    if (!match || typeof match !== 'object') return;
    const prev = _latestDerived.get(base);
    const v = Number(match.scoreVersion) || 0;
    if (prev && (Number(prev.scoreVersion) || 0) > v) return;
    _latestDerived.set(base, match);
    _emitOn(_derivedListeners, base, match);
}

// The taps this device made that the server answered as landed (applied,
// recorded, duplicate), by event id, kept in browser storage (landed_taps.jsx,
// one record per match) so every tab of the device and the next page after a
// reload know them (stage 6 review 5).
// Such a tap can be turned away LATER: a finish that never saw it, or one
// stamped before it, reaches the server after it (rule D5, the closed-match
// rule). Its own answer said it landed, so nothing else tells this device: the
// server names it in the match's `turnedAway` on every copy of the match (a
// push, a fetch, an answer), and `_tellTurnedAway` tells the operator once,
// for a tap of its own only, whatever moved it. A tap answered not-applied at
// once is never recorded here, since its own answer already said so. Kept a
// day, at most LANDED_TAPS_PER_MATCH of a match; a tap landed before browser storage
// was cleared is not known after it.
// The record itself has ONE owner, landed_taps.jsx: one small record per
// match (a batch's answer touches its own match's only), each entry naming the
// tab that noted it, so one tab of the device tells it (stage 6 review 7).
// Storage is reached lazily, so a page whose storage throws keeps it in memory.
const _landedStorage = {
    getItem: (k) => globalThis.localStorage.getItem(k),
    setItem: (k, v) => globalThis.localStorage.setItem(k, v),
    removeItem: (k) => globalThis.localStorage.removeItem(k),
    // Every key storage holds, so the record's index can be healed (m10).
    keys: () => {
        const ls = globalThis.localStorage;
        if (typeof ls.length === 'number' && typeof ls.key === 'function') {
            const out = [];
            for (let i = 0; i < ls.length; i++) out.push(ls.key(i));
            return out;
        }
        return Object.keys(ls);
    },
};
export const _landed = createLandedTaps({ storage: _landedStorage, tab: _randomId() });
export const _told = createToldTaps({ storage: _landedStorage, tab: _landed.tab });
// _landedBeat keeps this tab alive in the record, and tells the taps it may
// tell now from the newest copy of each match it has seen: a tab that died
// without its pagehide holds its taps until its heartbeat is old, and the
// copy that listed them may have come meanwhile, with no later one to come
// (stage 6 review 8, m11).
export function _landedBeat() {
    _landed.beat();
    // The told-tap record's sweep (m7): what it takes out is forgotten here.
    _forgetToldHere(_told.beat());
    for (const seen of _turnedAwaySeen.values()) {
        if (seen && seen.away && seen.compID && seen.matchID) _tellAway(seen.compID, seen.matchID, seen.away);
    }
}
// _forgetLandedTaps: on a sign-out the record goes with the operator who made
// the taps, so the next operator is never told of them: this tab's entries and
// those of tabs no longer alive, never another live tab's (stage 6 review 8,
// m10). A revoked password's park keeps it: the same operator signs in again.
export function _forgetLandedTaps() {
    _landed.forget();
}
// _noteLandedTaps records the taps of one match the server answered as landed.
function _noteLandedTaps(compID, matchID, descs) {
    if (!descs.length) return;
    const recs = [];
    for (const d of descs) {
        const p = d && d.payload;
        if (!p || !p.id) continue;
        recs.push({ id: p.id, where: _whereOf(d), kind: p.kind, side: p.side, pos: p.pos, letter: p.letter });
    }
    _landed.note(compID, matchID, recs);
    // The newest copy of the match seen before these answers may list them.
    const seen = _turnedAwaySeen.get(_revKey(compID, matchID));
    if (seen && seen.away) _tellAway(compID, matchID, seen.away);
}
function _rememberTurnedAway(compID, matchID, match) {
    if (!match || typeof match !== 'object') return;
    const base = _revKey(compID, matchID);
    const raw = match.turnedAway;
    const away = raw && typeof raw === 'object' && Object.keys(raw).length > 0 ? raw : null;
    const prev = _turnedAwaySeen.get(base);
    const v = match.scoreVersion;
    const keep = (seen) => _turnedAwaySeen.set(base, { ...seen, compID, matchID });
    if (!Number.isFinite(v)) {
        // A copy that names no version cannot be ordered: it never replaces
        // a list kept, and its list is kept only while none is (stage 6
        // review 7: one listing nothing wiped the list).
        if (away && !(prev && prev.away)) keep({ v: prev ? prev.v : NaN, away });
        return;
    }
    if (prev && !Number.isFinite(prev.v) && prev.away && !away) {
        // A list from a copy that named no version cannot be ordered against
        // this one: it stays, under this version.
        keep({ v, away: prev.away });
        return;
    }
    if (prev && Number.isFinite(prev.v)) {
        // Every copy with a version is remembered, one that lists nothing
        // included: a newer copy that no longer lists a tap (the first
        // result it was turned away for was displaced) is the newest word.
        if (v < prev.v || (v === prev.v && !away)) return;
    }
    keep({ v, away });
}
// _tellTurnedAway reads one copy of a match: each tap of this device the
// server now lists as turned away is told once, to the match's open editor
// (subscribeTapOutcome) and as one 'not_applied' alert naming the match and
// the tap, then forgotten. One tab tells it: the tab that noted it while its
// page is shown, else the shown tab with the lowest id (landed_taps.jsx
// `claim`, a marker read back before telling). Accepted: two tabs whose
// copies of the record are both behind each other's marker can still both
// tell it; the notice is said twice, never lost.
export function _tellTurnedAway(compID, matchID, match) {
    if (!compID || !matchID) return;
    const base = _revKey(compID, matchID);
    _rememberTurnedAway(compID, matchID, match);
    // Told from the newest copy seen, never from an older one.
    const seen = _turnedAwaySeen.get(base);
    if (seen && seen.away) _tellAway(compID, matchID, seen.away);
}
function _tellAway(compID, matchID, away) {
    if (!compID || !matchID || !away || typeof away !== 'object') return;
    const ids = Object.keys(away);
    if (ids.length === 0) return;
    // Taken out of the record before it is told: one notice per tap.
    const told = _landed.claim(compID, matchID, ids).map((t) => ({ ...t, outcome: away[t.id] }));
    if (told.length === 0) return;
    const base = _revKey(compID, matchID);
    const where = told.map((t) => t.rec.where).find(Boolean) || _shownMatchLabels.get(base) || '';
    for (const t of told) _emitOn(_tapOutcomeListeners, base, { id: t.id, outcome: t.outcome });
    const last = told[told.length - 1];
    const tap = { kind: last.rec.kind, side: last.rec.side, pos: last.rec.pos, letter: last.rec.letter };
    _notifyQueueAlert({
        kind: 'not_applied', count: told.length, terminalCount: 0, compID, matchID,
        outcome: { id: last.id, outcome: last.outcome }, where, tap, turnedAway: true,
    });
}
// _tellTurnedAwayIn reads every match a fetched competition holds.
export function _tellTurnedAwayIn(comp, compID) {
    if (!comp || !compID) return;
    // Every copy that names a version is remembered (a newer one that lists
    // nothing ends what an older one listed), and one with a list is told.
    const look = (m) => {
        if (m && m.id && (m.turnedAway || Number.isFinite(m.scoreVersion))) _tellTurnedAway(compID, m.id, m);
    };
    for (const m of comp.poolMatches || []) look(m);
    for (const p of comp.pools || []) for (const m of (p && p.matches) || []) look(m);
    const b = comp.bracket;
    if (b) {
        for (const r of b.rounds || []) for (const m of (Array.isArray(r) ? r : [])) look(m);
        look(b.thirdPlaceMatch);
    }
}

// _sendTapBatch: consecutive taps of one session for one match, in one POST
// to the score-events door. Each event settles on its own outcome: a quiet
// one (applied, recorded, duplicate) leaves the queue; a clock_skew one is
// re-stamped once and goes again with the same id; any other outcome leaves
// the queue and is told (one 'not_applied' alert a pass, and to the match's
// open editor through subscribeTapOutcome). A 400 for the batch PARKS its taps
// (operator S5 b); 5xx and network keep them; 401 parks for re-auth.
export async function _sendTapBatch(base, batch, pass) {
    const [, first] = batch[0];
    const { compID, matchID, password, url } = first;
    const session = first.payload && first.payload.session;
    if (!_isAllowedTerminalRequest('POST', url)) {
        let dequeued = false;
        _openDequeueBatch();
        try {
            for (const [k, d] of batch) {
                if (_dequeue(k, d, { value: { discarded: true } })) dequeued = true;
            }
        } finally {
            _closeDequeueBatch();
        }
        if (dequeued) _persistQueue();
        _notifyQueueAlert({ kind: 'unreadable', count: batch.length, terminalCount: 0, compID, matchID });
        return 'settled';
    }
    let res, body;
    try {
        ({ res, body } = await _fetchJson(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify({ session, events: batch.map(([, d]) => d.payload) }),
        }));
    } catch (_) {
        pass.anyFailed = true;
        pass.networkFailed = true;
        return 'kept';
    }
    body = body || {};
    if (res.ok) {
        if (body.match) _noteDerived(base, body.match);
        const byId = new Map((Array.isArray(body.outcomes) ? body.outcomes : []).map((o) => [o && o.id, o]));
        let result = 'settled';
        // Taps the server provably did not log (refused clock_skew, re-stamped
        // below): each may need a fresh seq (_reseqUnheldTap).
        const unheld = [];
        // Every settled tap leaves the queue in this loop, and the stored
        // queue is written once after it, not once per tap (J2, stage 3
        // review 6): a long queue drained a tap at a time was quadratic.
        let dequeued = false;
        // The follow-ups of the taps that leave are done once for the batch,
        // as it closes (K6).
        const landed = [];
        // Taps a board told as not applied that the server applied: one alert.
        const takenBack = [];
        // Taps held for the clock for the first time: told once, below.
        let clockHeld = 0;
        // The answer's own copy of the match first, so a duplicate is judged
        // on the newest word of what was turned away (F3).
        _rememberTurnedAway(compID, matchID, body.match);
        _openDequeueBatch();
        try {
            for (const [k, d] of batch) {
                if (_writeQueue.get(k) !== d) continue;
                const o = byId.get(d.payload && d.payload.id);
                if (!o) {
                    // No outcome: whether it was applied is unknown, so it may be
                    // logged. It keeps its id AND its seq (R6) and is sent again on
                    // the backoff exactly as it went, like a write a 5xx left queued.
                    pass.anyFailed = true;
                    if (result === 'settled') result = 'kept';
                    continue;
                }
                if (o.outcome === TAP_CLOCK_SKEW) {
                    // The taps settled so far leave storage before the clock is
                    // read (K2): while it is out, another tab's write to the
                    // queue would otherwise hand them back to this tab.
                    if (dequeued) { _persistQueue(); dequeued = false; }
                    _closeDequeueBatch();
                    // Never dropped: the server logged nothing. Re-stamped each
                    // time; refused again after a re-stamp, it stays queued and
                    // held (_keepForClock), as every write does.
                    const again = !!d.skewRetried;
                    await _restampQueuedTapForSkew(d);
                    unheld.push([k, d]);
                    if (again) {
                        if (_keepForClock(d, pass)) clockHeld++;
                        result = 'kept';
                    } else {
                        store.flushRequested = true;
                        if (result === 'settled') result = 'reclocked';
                    }
                    continue;
                }
                if (result !== 'settled') continue; // keep strict order behind it
                if (_dequeue(k, d, { value: o })) dequeued = true;
                const toldOnBoard = _told.told(compID, matchID, o.id);
                _toldHere.delete(o.id);
                if (tapOutcomeQuiet(o) && !toldOnBoard) {
                    landed.push(d);
                } else if (tapOutcomeQuiet(o) && (o.outcome !== 'duplicate' || _notTurnedAway(base, o.id))) {
                    // The board told it not applied from a copy behind the
                    // server (its bout shown removed, already picked again
                    // there): the server applied it, so the notice is taken
                    // back on every tab and the operator told once (stage 6
                    // review 8, m12; review 9, F3/F4).
                    // The alert goes to the tab that told it, or a shown tab
                    // when that one is hidden or gone (m1): that tab hears of
                    // it through the storage event (_onToldTapsStored).
                    landed.push(d);
                    const teller = _told.tellerOf(compID, matchID, o.id);
                    if (_told.takeBack(compID, matchID, o.id) && _landed.tellsFor(teller)
                        && _told.claimAlert(compID, matchID, o.id)) takenBack.push(d);
                    _emitOn(_tapOutcomeListeners, base, { id: o.id, outcome: 'applied', takenBack: true });
                } else if (toldOnBoard) {
                    // Told by the board as the removal reached it: once. A
                    // duplicate the newest copy still lists turned away is
                    // such a tap answered again (F3).
                    _told.settle(compID, matchID, o.id);
                } else {
                    _noteNotAppliedTap(pass, base, first, o);
                    _emitOn(_tapOutcomeListeners, base, {
                        id: o.id, outcome: o.outcome, reason: o.reason, message: o.message,
                    });
                }
            }
        } finally {
            _closeDequeueBatch();
        }
        if (clockHeld > 0) {
            _notifyQueueAlert({
                kind: 'server_error', count: clockHeld, terminalCount: 0, compID, matchID, taps: true,
                detail: CLOCK_SKEW_FAIL_REASON,
            });
        }
        if (takenBack.length > 0) {
            _notifyQueueAlert({
                kind: 'tap_applied', count: takenBack.length, terminalCount: 0, compID, matchID,
                where: _whereOf(takenBack[0]) || _shownMatchLabels.get(base) || '',
            });
        }
        // Landed: told later if a result that never saw them turns them away.
        // The answer's own copy of the match is remembered first, so a tap it
        // no longer lists is never told from an older copy (stage 6 review 7).
        _rememberTurnedAway(compID, matchID, body.match);
        _noteLandedTaps(compID, matchID, landed);
        _tellTurnedAway(compID, matchID, body.match);
        const heldSeqs = (body.match && body.match.eventSeqs) || {};
        let reseq = false;
        for (const [k, d] of unheld) {
            if (_writeQueue.get(k) === d && _reseqUnheldTap(base, d, heldSeqs)) reseq = true;
        }
        if (dequeued || reseq) _persistQueue();
        if (reseq) _publishPendingTaps(base);
        // S6 b: the server is missing taps of this session for the match; send
        // the ones the outbox still holds at once (a duplicate is harmless).
        if (Array.isArray(body.gaps) && body.gaps.length > 0) {
            const missing = new Set(body.gaps.map(Number));
            const held = _tapEntriesFor(base).some(([, d]) => d.payload && d.payload.session === session
                && missing.has(Number(d.payload.seq)));
            if (held) store.flushRequested = true;
        }
        _recomputeSyncStatus();
        return result;
    }
    if (_serverKeepsRetrying(res.status)) {
        let crossed = false;
        for (const [, d] of batch) {
            d.attempts = (Number(d.attempts) || 0) + 1;
            if (d.attempts === SERVER_REJECTION_NOTICE_THRESHOLD) crossed = true;
        }
        if (crossed) {
            _notifyQueueAlert({
                kind: 'server_error', count: batch.length, terminalCount: 0, compID, matchID, taps: true,
                detail: body.reasonHuman || body.error || `HTTP ${res.status}`,
            });
            _recomputeSyncStatus();
        }
        pass.anyFailed = true;
        return 'kept';
    }
    if (res.status === 401) {
        let fresh = false;
        for (const [, d] of batch) { if (!d.authBlocked) { d.authBlocked = true; fresh = true; } }
        if (fresh) {
            _notifyQueueAlert({
                kind: 'auth_required', count: batch.length, terminalCount: 0, compID, matchID, taps: true,
                detail: body.reasonHuman || body.error || `HTTP ${res.status}`,
            });
        }
        return 'kept';
    }
    // 400 (a shape fault) or another refusal for the whole batch: PARKED, not
    // dropped (operator S5 b). The taps stay in the held-writes list, hold
    // back the match's later writes, and go only when an operator discards
    // them (they count as failing, so HeldWriteDiscard and HeldWritesPanel
    // offer it).
    console.warn(`[sync] parking ${batch.length} tap(s) the server refused (${res.status}):`, body);
    const refusal = _replayRefusal(body, `HTTP ${res.status}`);
    for (const [, d] of batch) d.parked = { status: res.status, reason: refusal.reason };
    pass.parkedTaps += batch.length;
    _persistQueue();
    // A parked tap is not counted on the board or the TV (_pendingTapsFor).
    _publishPendingTaps(base);
    _recomputeSyncStatus();
    return 'kept';
}

// A tap refused for clock skew keeps its id and is re-stamped once, by the
// same take-the-older rule as a queued write (_restampFor), never before the
// match it was made against.
async function _restampQueuedTapForSkew(d) {
    await _learnServerClockOffset();
    const enqueuedAt = Number.isFinite(d.enqueuedAt) ? d.enqueuedAt : Date.now();
    d.payload.stamp = _afterSettledStamp(_revKey(d.compID, d.matchID),
        _stampAfterSeen(_restampFor(enqueuedAt, d.perfAtEnqueue), d.seenModifiedAt));
    d.skewRetried = true;
    _persistQueue();
}

// _reseqUnheldTap (M3): the ONE rule for a queued tap the server answered
// without logging it: a clock-skew refusal, the one answer that provably logged
// nothing (R6: a tap the answer names no outcome for may be logged, so it keeps
// its seq and is resent as it went). A match copy names, per device session, the highest seq its score log
// holds (`eventSeqs`), and a screen reads a tap at or below it as already in
// the copy (applyPending, score_events.jsx). When a later tap of the same
// session landed, that number already covers this one, so the board would drop
// a tap the server does not hold. Such a tap takes the session's next seq for
// the match, above every seq the server or this outbox holds for it, under the
// same id: the server keys duplicates by id, and the writes that saw the tap
// name it by id (`basis.own`), so containment is unchanged. Exact both ways: it
// counts as held only once the server logs it. Returns whether it moved.
function _reseqUnheldTap(base, d, heldSeqs) {
    const p = d && d.payload;
    if (!p || !p.session || !(Number(p.seq) > 0)) return false;
    const held = Number(heldSeqs && heldSeqs[p.session]) || 0;
    if (held < Number(p.seq)) return false;
    let top = held;
    for (const [, e] of _tapEntriesFor(base)) {
        if (e.payload && e.payload.session === p.session) top = Math.max(top, Number(e.payload.seq) || 0);
    }
    if (p.session === _eventSession) top = Math.max(top, _tapSeq.get(base) || 0);
    p.seq = top + 1;
    if (p.session === _eventSession) _tapSeq.set(base, p.seq);
    return true;
}

// _noteOwnTapSeq: a tap of THIS tab's session taken up from storage moves the
// tab's seq counter for its match past it, so the next tap never reuses the
// seq (R10: another tab may have re-seqd it, _reseqUnheldTap).
export function _noteOwnTapSeq(key, d) {
    const p = d && d.kind === 'evt' && d.payload;
    if (!p || p.session !== _eventSession || !(Number(p.seq) > 0)) return;
    const base = _baseOf(key);
    if (Number(p.seq) > (_tapSeq.get(base) || 0)) _tapSeq.set(base, Number(p.seq));
}

// _adoptStoredTapSeq: the stored copy of a tap this tab holds names a higher
// seq under the same id: another tab re-seqd it. Taken into this tab's copy
// (and its seq counter, _noteOwnTapSeq). True when the copy changed.
export function _adoptStoredTapSeq(key, held, stored) {
    const hp = held && held.kind === 'evt' && held.payload;
    const sp = stored && stored.kind === 'evt' && stored.payload;
    if (!hp || !sp || hp.id !== sp.id || !(Number(sp.seq) > Number(hp.seq))) return false;
    hp.seq = Number(sp.seq);
    _noteOwnTapSeq(key, held);
    _publishPendingTaps(_baseOf(key));
    return true;
}

// The calls lower modules make into this one (api_client_store.jsx).
provide({ _tellTurnedAwayIn, _dropToldHere, _publishPendingTaps });
