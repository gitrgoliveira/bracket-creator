// What this device shows before the server answers: own status moves, local
// match patches (court and time), check-ins, court calls and document versions.
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import { bridge as _bridge } from './court_bridge.jsx';
import { writeWasSuperseded } from './write_result.jsx';
import { createDocumentVersions } from './document_versions.jsx';
import { _isMatchAlreadyCompletedRefusal } from './api_client_requests.jsx';
import { _relearnClockThrottled } from './api_client_clock.jsx';
import {
    _revKey, _notifyQueueAlert, _persistQueue, _writeQueue, _queueShapeGen, _unorderedValue, _baseOf,
    _entryStamp, _byEntryOrder, _entriesFor, _claimRefusal,
} from './api_client_queue.jsx';
import { _tellTurnedAwayIn } from './api_client_taps.jsx';
import { _inFlight } from './api_client_flush.jsx';

// The scoreline fields a start-only write never publishes on the court bridge
// (bc-evts stage 4 review 2, G1): what engine.keepQueuedScore keeps from the
// stored match, plus the editors' `score` summary. _broadcastPatch is the one
// reader.
export const START_BROADCAST_DROPS = Object.freeze([
    'ipponsA', 'ipponsB', 'hansokuA', 'hansokuB', 'encho', 'subResults',
    'flagsA', 'flagsB', 'repPlayerA', 'repPlayerB', 'score',
]);

// Shared rules for court bridge match patches: a court-less match publishes
// nothing; the patch names the match sides ids; a status-only patch states
// the held flag counts because the display reads a result naming a status that
// omits a count as 0. `ifStatus`, when given, is the status the display's copy
// must hold for the patch to apply (applyPatchToTree).
export function _publishMatchPatch(compID, match, result, ifStatus) {
    const court = (match && match.court) || '';
    if (!court) return;
    _bridge.publish('patch', court, compID, ifStatus ? { result, ifStatus } : { result });
}

export function _sideIdFields(match) {
    return {
        ...(match && match.sideA && match.sideA.id ? { sideAId: match.sideA.id } : {}),
        ...(match && match.sideB && match.sideB.id ? { sideBId: match.sideB.id } : {}),
    };
}

export function _heldFlagCounts(match) {
    if (match && (match.flagsA != null || match.flagsB != null)) {
        return { flagsA: match.flagsA ?? 0, flagsB: match.flagsB ?? 0 };
    }
    return {};
}

// bc-stpf: a send back the outbox holds is told on the court's channel in the
// tick it is queued, as a start is (_broadcastPatch), so a display window on
// this machine shows the bout back in the queue at once (offline for the whole
// outage), and running again when the server refuses it or the operator
// discards it. Only the status moves: RevertMatchToQueue keeps the score and
// a running bout holds no verdict, so the patch names no scoreline field (a
// key present as undefined would wipe the display's copy), and it states the
// flag counts the match holds, because the display reads a result naming a
// status that omits a count as 0 (mergeMatchPatch). It carries no
// scoreVersion, so the put-back is never read as a stale running board
// (staleRunningOverlay). It names the status the bout moves from (`ifStatus`):
// a send back applies only to a bout the display holds running, and its
// put-back only to one it holds queued, so a display that has already taken
// another device's finish of the bout keeps showing it finished. A court-less
// match publishes nothing, as _broadcastPatch does.
function _publishSendBack(compID, matchID, match, status, court) {
    _publishMatchPatch(compID, match, {
        id: matchID,
        ..._sideIdFields(match),
        ..._heldFlagCounts(match),
        status,
        ...(court ? { court } : {}),
    }, status === 'scheduled' ? 'running' : 'scheduled');
}

// _attachSendBackDisplayPutBack publishes status "scheduled" to the local
// court bridge at enqueue, and puts it back "running" on a refusal or discard
// if the match is still running. When the refusal says the match is already
// completed, we do not tell the display it is running: the display takes the
// server copy at its next refetch. `movedTo`, given by a running court move,
// is the court the bout goes to: the "scheduled" patch names it, so the old
// court's display takes the bout off its queue at once (the display keeps a
// match's court when a patch names none, mergeMatchPatch), and the put-back
// names the court it came from. A send back gives none and names no court.
export function _attachSendBackDisplayPutBack(compID, matchID, match, answer, movedTo) {
    if (!match || !answer || !answer.settled) return;
    _publishSendBack(compID, matchID, match, 'scheduled', movedTo);
    const putBack = () => _publishSendBack(compID, matchID, match, 'running', movedTo && match.court);
    answer.settled.then(
        (body) => {
            if (body && body.discarded && !body.sentByAnotherTab) putBack();
        },
        (err) => {
            if (_isMatchAlreadyCompletedRefusal(err)) return;
            putBack();
        },
    );
}

// ---------------------------------------------------------------------------
// The status moves this device made (stage 6 review 8, M4, the screen rule:
// "The screens must display exactly what the operator sees or changes"): a
// send back to the queue (scheduled), a finish or a decision (completed), and
// a start or a reopen after them (running). ONE owner, read by the court
// console: the outbox's queued entries of the match, the last move in its
// replay order (so a reload, or the console mounted again, still shows a send
// back or a finish the outbox holds), else the last such move the server
// answered as landed, kept for the page, until the feed shows it. A refused or
// discarded move leaves the queue with no answer kept: the screen shows the
// server's copy again.
// ---------------------------------------------------------------------------
const _ownStatusAnswered = new Map(); // base -> { status, kind, payload, stamp }
const _ownStatusListeners = new Set();
let _ownStatusSig = '';

// _statusMoveOf: the status a queued entry moves its match to, or null.
function _statusMoveOf(d) {
    if (!d) return null;
    const p = d.payload || {};
    switch (d.kind) {
        case 'revert': return 'scheduled';
        case 'decision': return 'completed';
        case 'reopen': case 'requeue': return 'running';
        case 'score': case undefined: case null: case '':
            if (p.status === 'completed') return 'completed';
            if (p.status === 'running' && p.startOnly) return 'running';
            return null;
        default: return null;
    }
}

// _ownStatusMove: what the court console shows for match (compID, matchID)
// over the server's copy: { status, kind, payload, landed, stamp }, or null.
// `stamp` is the move's own (the server's answer when it named one): a feed
// copy stamped after it is a later change made elsewhere.
export function _ownStatusMove(compID, matchID) {
    const base = _revKey(compID, matchID);
    const moves = _statusEntriesByBase().get(base);
    if (moves && moves.length > 0) {
        const [, d] = moves.slice().sort(_byEntryOrder)[moves.length - 1];
        const last = { status: _statusMoveOf(d), kind: d.kind || 'score', payload: d.payload, landed: false, stamp: _entryStamp(d) };
        return last.status === 'running' ? null : last;
    }
    const answered = _ownStatusAnswered.get(base);
    return answered ? { ...answered, landed: true } : null;
}

// _statusEntriesByBase: the queued status moves, by base, made again only when
// the queue's entries changed (_queueShapeGen): the court console asks for every
// match it shows on every push, and a tap never is a status move (F7: each ask
// scanned and sorted the whole queue, about 1.7 ms a render at 2000 queued taps).
let _statusIndex = { gen: -1, byBase: new Map() };
export function _statusEntriesByBase() {
    if (_statusIndex.gen === _queueShapeGen) return _statusIndex.byBase;
    const byBase = new Map();
    for (const [k, d] of _writeQueue) {
        if (!d || d.kind === 'evt' || !_statusMoveOf(d)) continue;
        const b = _baseOf(k);
        if (!byBase.has(b)) byBase.set(b, []);
        byBase.get(b).push([k, d]);
    }
    _statusIndex = { gen: _queueShapeGen, byBase };
    return byBase;
}

// _noteOwnStatusAnswer: an entry left the queue (`outcome`, as _dequeue has
// it). A status move the server took is kept as answered; one it refused, or
// that was discarded or sent by another tab, keeps nothing.
export function _noteOwnStatusAnswer(key, d, outcome) {
    const status = _statusMoveOf(d);
    if (!status) return;
    const base = _baseOf(key);
    const v = outcome && 'value' in outcome ? outcome.value : undefined;
    if (v === undefined || v === null || _unorderedValue(v) || writeWasSuperseded(v)) return;
    if (status === 'running') {
        _ownStatusAnswered.delete(base);
        return;
    }
    const answered = Number(v && typeof v === 'object' && v.modifiedAt);
    const version = Number(v && typeof v === 'object' && v.scoreVersion);
    _ownStatusAnswered.set(base, {
        status, kind: d.kind || 'score', payload: d.payload,
        stamp: Number.isFinite(answered) && answered > 0 ? answered : _entryStamp(d),
        // The answer's score version: a feed copy past it is newer, whatever
        // its stamp (F7).
        version: Number.isFinite(version) ? version : null,
        compID: d.compID, matchID: d.matchID,
    });
}

// _publishOwnStatus: tells the listeners when any match's own status move
// changed (from _recomputeSyncStatus, which every queue change reaches).
export function _publishOwnStatus() {
    if (_ownStatusListeners.size === 0) return;
    const parts = [];
    for (const list of _statusEntriesByBase().values()) {
        for (const [k, d] of list) parts.push(`${k}=${_statusMoveOf(d)}@${_entryStamp(d)}`);
    }
    for (const [base, a] of _ownStatusAnswered) parts.push(`${base}~${a.status}@${a.stamp}`);
    const sig = parts.sort().join('|');
    if (sig === _ownStatusSig) return;
    _ownStatusSig = sig;
    for (const fn of _ownStatusListeners) {
        try { fn(); } catch (_e) { /* swallow */ }
    }
}

// The answered moves and their listeners are written by the own-status code
// alone; the shown-match labels, the wipe and the API doors go through these
// (bc-jsxs).
// _subscribeOwnStatusMoves: told whenever a status move changes (queued,
// answered, refused or discarded).
export function _subscribeOwnStatusMoves(fn) {
    _ownStatusListeners.add(fn);
    return () => _ownStatusListeners.delete(fn);
}
// _forgetOwnStatusAnswersWhere: the answered moves whose match key passes
// `test` go; whether any went.
export function _forgetOwnStatusAnswersWhere(test) {
    let moved = false;
    for (const k of _ownStatusAnswered.keys()) if (test(k)) { _ownStatusAnswered.delete(k); moved = true; }
    return moved;
}
// _clearOwnStatusAnswers: every answered move goes; whether any was held.
export function _clearOwnStatusAnswers() {
    const held = _ownStatusAnswered.size > 0;
    _ownStatusAnswered.clear();
    return held;
}
// _forgetOwnStatusMove: the feed shows the move, or a later change: the
// answered record goes.
export function _forgetOwnStatusMove(compID, matchID) {
    if (_ownStatusAnswered.delete(_revKey(compID, matchID))) _publishOwnStatus();
}
// _forgetOwnStatusMovesNotIn: the feed no longer holds the match (moved to
// another court, or gone with its draw): the answered record goes, so it never
// shows over a match made again under the same id (F7). `matches` is the feed
// the caller shows.
export function _forgetOwnStatusMovesNotIn(matches) {
    const held = new Set((matches || []).map((m) => m && _revKey(m.compId, m.id)));
    if (_forgetOwnStatusAnswersWhere((k) => !held.has(k))) _publishOwnStatus();
}

// ---------------------------------------------------------------------------
// bc-evts stage 3 review 2, R4: a court move and a time change show at once.
// ---------------------------------------------------------------------------
// They are outbox entries like every write, and what this device shows follows
// them as it follows a score write: the ONE owner is here. The new value is
// told to the local listeners in the tick the change is made
// (`subscribeLocalMatchPatch`: an SSE-shaped `match_updated`, `local: true`,
// that the hosts apply as they apply a push, applyPatch); every match a fetch
// answers carries the value a court or time entry still queued for it holds
// (`_overlayPendingFields`), so a refetch made before the change lands never
// shows the old one; and a refusal, or an operator's discard, puts back what
// the match had (`was`, which the caller hands over and the entry keeps), or
// the value an entry still queued after it holds.
const _LOCAL_FIELD = { court: 'court', time: 'scheduledAt' };
const _localPatchListeners = new Set();

export function subscribeLocalMatchPatch(cb) {
    if (typeof cb !== 'function') return () => {};
    _localPatchListeners.add(cb);
    return () => { _localPatchListeners.delete(cb); };
}

export function _emitLocalMatchPatch(compID, matchID, fields) {
    if (!compID || !matchID || !fields || Object.keys(fields).length === 0) return;
    const event = { type: 'match_updated', local: true, data: { competitionId: compID, matchId: matchID, result: { id: matchID, ...fields } } };
    for (const cb of _localPatchListeners) {
        try { cb(event); } catch (err) { console.error('local match patch listener failed:', err); }
    }
}

// The court and time entries still queued for a match hold, the later winning.
export function _pendingMatchFields(compID, matchID) {
    const out = {};
    for (const [, d] of _entriesFor(_revKey(compID, matchID))) {
        const f = _LOCAL_FIELD[d.kind];
        if (f && d.payload && f in d.payload) out[f] = d.payload[f];
    }
    return out;
}

// An entry left with a refusal or an operator's discard: the match shows what
// it had, unless a later entry of the same field is still queued. A check-in
// puts back what each of its participants had (`was`, per id) the same way.
export function _revertLocalFields(d) {
    if (d && _CHECKIN_KINDS.has(d.kind)) {
        _revertLocalCheckIns(d);
        return;
    }
    const f = d && _LOCAL_FIELD[d.kind];
    if (!f || !d.was || typeof d.was !== 'object' || !(f in d.was)) return;
    const still = _pendingMatchFields(d.compID, d.matchID);
    _emitLocalMatchPatch(d.compID, d.matchID, { [f]: f in still ? still[f] : d.was[f] });
}

// _overlayPendingFields: a fetched competition (a list item or a detail) with
// the court and time this device still has queued for its matches put on, and
// its queued check-ins (_overlayPendingCheckIns) and awards save
// (overlayPendingAwards).
export function _overlayPendingFields(comp, compID) {
    const id = compID || (comp && (comp.id || (comp.config && comp.config.id)));
    if (!comp || !id) return comp;
    // Every fetch of a competition passes here: a tap of this device it turned
    // away since its answer is told (_tellTurnedAway), after a reload too.
    _tellTurnedAwayIn(comp, id);
    return overlayPendingAwards(_overlayPendingCheckIns(_overlayPendingMatchFields(comp, id), id), id);
}

function _overlayPendingMatchFields(comp, id) {
    const byMatch = new Map();
    for (const d of _writeQueue.values()) {
        if (d && _LOCAL_FIELD[d.kind] && d.compID === id && d.matchID && !byMatch.has(d.matchID)) {
            byMatch.set(d.matchID, _pendingMatchFields(id, d.matchID));
        }
    }
    if (byMatch.size === 0) return comp;
    const put = (m) => (m && byMatch.has(m.id) ? { ...m, ...byMatch.get(m.id) } : m);
    const out = { ...comp };
    if (Array.isArray(comp.poolMatches)) out.poolMatches = comp.poolMatches.map(put);
    if (Array.isArray(comp.pools)) {
        out.pools = comp.pools.map((p) => (p && Array.isArray(p.matches) ? { ...p, matches: p.matches.map(put) } : p));
    }
    if (comp.bracket && Array.isArray(comp.bracket.rounds)) {
        out.bracket = { ...comp.bracket, rounds: comp.bracket.rounds.map((r) => (Array.isArray(r) ? r.map(put) : r)) };
        if (comp.bracket.thirdPlaceMatch) out.bracket.thirdPlaceMatch = put(comp.bracket.thirdPlaceMatch);
    }
    return out;
}

// ---------------------------------------------------------------------------
// bc-obox C1/C2: a check-in is an outbox entry, shown at once.
// ---------------------------------------------------------------------------
// A competition's check-ins go under one base, `checkin:<comp>`, so they are
// sent in the order they were tapped; each carries its stamp (`modifiedAt`),
// by which the server keeps the later tap whatever the arrival order. What the
// desk shows follows them as a court move does: the new value is told to the
// local listeners in the tick of the tap (`subscribeLocalParticipantPatch`,
// `{ type: 'checkin_updated', local: true, data: { competitionId,
// participants: [{ id, checkedIn }] } }`); every competition a fetch answers
// carries the check-ins still queued for it (_overlayPendingCheckIns); a
// refusal or a discard puts back what each participant had (`was`, per id),
// and a `superseded` answer puts the participant as stored (another desk's
// later tap), unless a later check-in of this device is still queued for them.
export const _CHECKIN_KINDS = new Set(['checkin', 'checkin-bulk']);
const _localParticipantListeners = new Set();

export function subscribeLocalParticipantPatch(cb) {
    if (typeof cb !== 'function') return () => {};
    _localParticipantListeners.add(cb);
    return () => { _localParticipantListeners.delete(cb); };
}

export function _checkInBase(compID) {
    return `checkin:${compID}`;
}

// _checkInsOf: what one check-in entry asks, per participant id: a single
// tap names its participant in the url (PUT checks in, DELETE takes it back),
// a bulk check-in checks in every id it names.
export function _checkInsOf(d) {
    const out = new Map();
    if (!d || !_CHECKIN_KINDS.has(d.kind)) return out;
    if (d.kind === 'checkin-bulk') {
        const ids = d.payload && Array.isArray(d.payload.participantIds) ? d.payload.participantIds : [];
        for (const pid of ids) if (typeof pid === 'string' && pid) out.set(pid, true);
        return out;
    }
    const m = String(d.url || '').match(/\/participants\/([^/]+)\/checkin$/);
    if (m) {
        try { out.set(decodeURIComponent(m[1]), d.method !== 'DELETE'); } catch (_e) { /* not this device's url */ }
    }
    return out;
}

// _pendingCheckIns: the check-ins still queued for a competition, per id, the
// later winning; `except` is an entry left out (the one being answered).
function _pendingCheckIns(compID, except) {
    const out = new Map();
    for (const [, d] of _entriesFor(_checkInBase(compID))) {
        if (d === except) continue;
        for (const [pid, v] of _checkInsOf(d)) out.set(pid, v);
    }
    return out;
}

export function _emitLocalCheckIns(compID, values) {
    const participants = [...values].filter(([id]) => id).map(([id, checkedIn]) => ({ id, checkedIn: !!checkedIn }));
    if (!compID || participants.length === 0) return;
    const event = { type: 'checkin_updated', local: true, data: { competitionId: compID, participants } };
    for (const cb of _localParticipantListeners) {
        try { cb(event); } catch (err) { console.error('local participant patch listener failed:', err); }
    }
}

// A check-in left with a refusal or a discard: each participant shows what it
// had, unless a later check-in of this device is still queued for them.
function _revertLocalCheckIns(d) {
    const was = d && d.was && typeof d.was === 'object' ? d.was : {};
    const still = _pendingCheckIns(d.compID, d);
    const out = [];
    for (const pid of _checkInsOf(d).keys()) {
        if (still.has(pid)) out.push([pid, still.get(pid)]);
        else if (Object.prototype.hasOwnProperty.call(was, pid)) out.push([pid, !!was[pid]]);
    }
    _emitLocalCheckIns(d.compID, out);
}

// _checkInAnswered: a check-in the server answered 200. A `superseded` answer
// (a later tap of another desk already changed that person) changed nothing:
// the desk takes the participant as stored, and when that is not what the tap
// asked the operator is told once, by a caller that claims it (onRefusal), else
// by the queue alert. A bulk answer names its superseded ids the same way.
export function _checkInAnswered(key, d, body) {
    const asked = _checkInsOf(d);
    const stored = d.kind === 'checkin-bulk'
        ? (Array.isArray(body && body.superseded) ? body.superseded : [])
        : (body && body.applied === false && body.reason === 'superseded' && body.participant ? [body.participant] : []);
    // Our stamp lost: the clock frame is checked again, as a match write's is.
    if (stored.length > 0) _relearnClockThrottled();
    const still = _pendingCheckIns(d.compID, d);
    const adopt = [];
    const lost = [];
    for (const p of stored) {
        if (!p || !p.id) continue;
        if (!still.has(p.id)) adopt.push([p.id, !!p.checkedIn]);
        if (asked.has(p.id) && asked.get(p.id) !== !!p.checkedIn) lost.push(p);
    }
    _emitLocalCheckIns(d.compID, adopt);
    if (lost.length === 0) return;
    const sentence = lost.length === 1
        ? `${lost[0].name || 'A competitor'} was changed on another desk: now ${lost[0].checkedIn ? 'checked in' : 'not checked in'}.`
        : `${lost.length} check-ins were changed on another desk. Check the list.`;
    const err = Object.assign(new Error(sentence), { reason: 'superseded', participants: lost });
    if (!_claimRefusal(key, err)) {
        _notifyQueueAlert({ kind: 'checkin_superseded', count: lost.length, terminalCount: 0, compID: d.compID, matchID: '', detail: sentence });
    }
}

// _overlayPendingCheckIns: a fetched competition with the check-ins this
// device still has queued for it put on its players (the list item's, the
// detail's config and each pool's).
function _overlayPendingCheckIns(comp, id) {
    const pending = _pendingCheckIns(id);
    if (pending.size === 0) return comp;
    const put = (p) => (p && p.id && pending.has(p.id) ? { ...p, checkedIn: pending.get(p.id) } : p);
    const out = { ...comp };
    if (Array.isArray(comp.players)) out.players = comp.players.map(put);
    if (comp.config && Array.isArray(comp.config.players)) out.config = { ...comp.config, players: comp.config.players.map(put) };
    if (Array.isArray(comp.pools)) {
        out.pools = comp.pools.map((p) => (p && Array.isArray(p.players) ? { ...p, players: p.players.map(put) } : p));
    }
    return out;
}

// _checkInLabel: where a check-in is, as the held-writes list names it.
export function _checkInLabel(name, count = 1) {
    if (typeof name === 'string' && name.trim()) return `Check-in: ${name.trim()}`;
    return count > 1 ? `Check-in: ${count} competitors` : '';
}

// ---------------------------------------------------------------------------
// bc-obox A1: court calls through the outbox, the device makes the id.
// ---------------------------------------------------------------------------
// A call and its withdrawal share the base `announce:<id>`, so they go in
// order; a clear has its own base. The server remembers withdrawn ids and the
// clear's floor, so the order between different calls does not matter, and a
// call the server keeps refusing holds back no other court's.
export const _ANNOUNCE_CLEAR_BASE = 'announce-clear';
export function _announceBase(id) {
    return `announce:${id}`;
}
export function _courtCallLabel(message) {
    const m = typeof message === 'string' ? message.trim() : '';
    if (!m) return 'Court call';
    return `Court call: "${m.length > 40 ? `${m.slice(0, 39)}…` : m}"`;
}

// The tournament settings and the awards are whole documents saved against a
// version (document_versions.jsx); it reaches the queue only through these.
const {
    currentDocVersion, rebaseQueuedVersion, overlayPendingTournament, overlayPendingAwards,
} = createDocumentVersions({
    entriesFor: (base) => _entriesFor(base),
    isInFlight: (d) => _inFlight.has(d),
    persistQueue: () => _persistQueue(),
});

export { currentDocVersion, rebaseQueuedVersion, overlayPendingTournament };
