// Minted team members: the writes that add, rename or clear one, and the gate
// every write naming a minted id passes (minted_members.jsx owns the record).
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import { FETCH_TIMEOUT_MS } from './write_result.jsx';
import {
    createMintedMembers, mintedAddId, mintedIdsNamed, mintedMemberUrlId, mintedProofCovers, mintedStrip,
} from './minted_members.jsx';
import { _randomId } from './api_client_ids.jsx';
import { _fetchJson } from './api_client_requests.jsx';
import { _serverNowMs } from './api_client_clock.jsx';
import {
    _notifyQueueAlert, _persistQueue, _writeQueue, _wasSettledHere, _dequeue, _dequeueBatch,
    _openDequeueBatch, _closeDequeueBatch, _storedQueue, _entriesFor,
} from './api_client_queue.jsx';
import { _teamIdOf, _recomputeSyncStatus } from './api_client_status.jsx';
import { _inFlight, _queueGen, _addAfter, _rerunFlushSoon } from './api_client_flush.jsx';
import { _outboxWrite, _lineupKey } from './api_client_enqueue.jsx';

// _withListedMembersForgotten: a members map the server answered, after
// telling the owner every member it holds exists (minted_members.jsx).
export function _withListedMembersForgotten(teams, competitionId) {
    // A member judged never to have been that the list holds: told (N4).
    for (const m of _minted.listed(teams, competitionId)) _announceKeptMember(m);
    return teams;
}

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
// The members reads the owner asked for that are out (by member id), the ids
// of the adds this tab has a request out for, and the one timer that reruns
// the flush when a write the owner holds back may go.
const _mintedReads = new Set();
const _mintedOutHere = new Set();
// The adds this tab sent or discarded whose end is not known, by id, with the
// password to read their team's members with (_mintedSweep).
const _mintedReadPw = new Map();
let _mintedTimer = null;
let _mintedTimerAt = 0;
// Y4 (stage 3 review 11): the ids whose read the server refused for its
// password (401/403), parked until the next sign-in (resumeAfterAuth), never
// counted toward the re-read bound nor asked again with that password; and
// whether a revoked password has paused every read (parkQueueForReauth).
const _mintedAuthParked = new Set();
let _mintedPaused = false;
const MINTED_READ_AUTH_REFUSED = Symbol('minted read refused for its password');

// The member ids this tab has said are gone (_announceDroppedMember): one
// `team-member-dropped` per id, whether this tab proved it or heard of it from
// another.
const _announcedDropped = new Set();
// The member ids this tab has told the operator the server kept after all
// (_announceKeptMember): one alert per id.
const _announcedKept = new Set();

// What a team member write leaves undone when the server does not answer it.
export const TEAM_MEMBER_NOT_ADDED = 'The team member was not added';
export const TEAM_MEMBER_NOT_RENAMED = 'The team member was not renamed';
export const TEAM_MEMBER_NOT_CLEARED = 'The team member\'s name was not cleared';

// _memberWrite: a team member's POST (add), PUT (rename) or DELETE (cleared name), as an
// outbox entry (R1). `payload` is the JSON body, none for a clear; `member` the member as
// this device shows it once the write is made.
export function _memberWrite({ compID, teamId, method, url, payload, password, member, opts }) {
    // bc-evts stage 3 review 2, R1: a team member write is an outbox entry
    // under the team's own base (the starting lineup's, so a starting lineup
    // save made after it follows it), answered at once and never awaited. The
    // member comes back as this device will show it (`member`, merged by its
    // stamp, mergeMembers): `settled` (not enumerable, so a list holding the
    // member never carries it) resolves with the member as the server stored
    // it, or rejects with the refusal, whose `revertMember` puts the member as
    // it was back, stamped later than the change it undoes.
    const answer = _outboxWrite({
        kind: 'member', compID, matchID: '', method, url, payload, password,
        onRefusal: opts && opts.onRefusal,
        base: _membersBase(compID, teamId),
        ...(opts && opts.was ? { was: opts.was } : {}),
    });
    const shown = { ...member };
    Object.defineProperty(shown, 'settled', { value: answer.settled, enumerable: false });
    Object.defineProperty(shown, 'pending', { value: true, enumerable: false });
    if (answer.persisted === false) Object.defineProperty(shown, 'persisted', { value: false, enumerable: false });
    return shown;
}

// _lastMemberEntryKey: the key of the team's last queued member write, '' when none.
function _lastMemberEntryKey(compID, teamId) {
    return _lastMemberEntryKeyOfBase(_membersBase(compID, teamId));
}

// _lastMemberEntryKeyOfBase: the same answer for a base already in hand (the
// ONE meaning of "the team's last member write", J3, stage 3 review 6).
function _lastMemberEntryKeyOfBase(base) {
    const members = _entriesFor(base).filter(([, d]) => d.kind === 'member');
    return members.length ? members[members.length - 1][0] : '';
}

// _droppedOf: the member add `d`'s id with the competition and team the
// owner holds for it, for the strip and the event.
export function _droppedOf(d) {
    const id = mintedAddId(d);
    const r = _minted.recordOf(id);
    return { id, c: r ? r.c : (d.compID || ''), t: r ? r.t : _teamIdOf(d) };
}

// _mintedAddAnswered: tells the owner (minted_members.jsx) what came of a
// member add's request: `res`/`body`, or none: no connection, or no answer by
// the deadline (`timedOut`: a server still at work may keep the add after it,
// so the owner waits a grace before it asks). Called before the entry leaves
// the queue, so another tab that hears of the queue changing already finds
// what is known of the id.
export function _mintedAddAnswered(key, d, res, body, timedOut = false) {
    if (!mintedAddId(d)) return;
    const status = res ? res.status : 0;
    let outcome = 'refused';
    if (!res) outcome = timedOut ? 'timeout' : 'retry';
    else if (res.ok) outcome = 'kept';
    else if (status >= 500 || status === 429 || status === 403 || status === 401) outcome = 'retry';
    else if ((body && body.error) === 'member_id_conflict') outcome = 'held';
    const verdict = _minted.answered(d, outcome, _writeQueue.get(key) === d);
    if (verdict === 'dropped') _learnDropped([_droppedOf(d)]);
    else if (verdict === 'contradicted') _announceKeptMember(_droppedOf(d));
    else if (outcome !== 'kept') _mintedSweep(d);
}

// _announceKeptMember: a member this device (or another tab) treated as not
// added, and so changed the writes naming it, turned out to be held by the
// server (a members list holds it, or its add's request was kept after all).
// The operator is told once per id, never left to find the member's names and
// lineups changed under them (stage 3 review 10, N4).
export function _announceKeptMember({ id, c }) {
    if (!id || _announcedKept.has(id)) return;
    _announcedKept.add(id);
    _notifyQueueAlert({ kind: 'member_kept', count: 1, terminalCount: 0, compID: c || '' });
}

// _announceUnsentMemberChange: a rename or a cleared name of a member that
// never was left the queue unsent (it settles as discarded, which the editors
// do not show): the operator is told it was not sent (N4).
function _announceUnsentMemberChange(d) {
    _notifyQueueAlert({ kind: 'member_unsent', count: 1, terminalCount: 0, compID: (d && d.compID) || '' });
}

// _mintedSweep: the reads this tab owes for the adds it sent or discarded
// (`d`, one more to remember) whose end is not known, made as soon as the
// owner allows, with no write waiting on them, so the editors hear soon that a
// member never was; a later one is timed (_rerunFlushAt).
export function _mintedSweep(d = null) {
    if (d && mintedAddId(d)) _mintedReadPw.set(mintedAddId(d), d.password || '');
    // A revoked password parks every read until the new sign-in (Y4).
    if (_mintedPaused || _mintedReadPw.size === 0) return;
    // Deleting while iterating a Map is safe: each key is visited once.
    for (const id of _mintedReadPw.keys()) {
        const r = _minted.recordOf(id);
        if (!r || r.s === 'dropped' || r.s === 'kept') _mintedReadPw.delete(id);
    }
    const due = _minted.due([..._mintedReadPw.keys()].filter((id) => !_mintedAuthParked.has(id)), _mintedCtx());
    for (const r of due.reads) _readMintedMembers(r, _mintedReadPw.get(r.id));
    if (due.until) _rerunFlushAt(due.until);
}

// _mintedAddQueued: this tab, or the stored queue, still holds the add that
// mints `id`, which then still decides what becomes of it. A stored copy of
// one this tab took out itself is no longer queued (_wasSettledHere): the
// next store takes it out.
function _mintedAddQueued(id) {
    for (const d of _writeQueue.values()) if (mintedAddId(d) === id) return true;
    try {
        for (const [k, d] of _storedQueue()) if (mintedAddId(d) === id && !_wasSettledHere(k, d)) return true;
    } catch (_e) { /* storage unreadable: what this tab holds is all it knows */ }
    return false;
}

// _mintedCtx: what this tab tells the owner of the queue it holds.
function _mintedCtx() {
    return {
        addQueued: _mintedAddQueued, addOutHere: (id) => _mintedOutHere.has(id), teamOf: _teamIdOf,
        // A read the server refused for its password, parked until the next
        // sign-in (Y4): the writes go as made meanwhile, as after a failed read.
        readParked: (id) => _mintedAuthParked.has(id),
    };
}

// _mintedGate: what the owner says of the write `d` (queued under `key`)
// before it is sent: 'go'; 'wait', while a member it names is not known to
// exist (the owner's read, the add's answer or its timer reruns the flush); or
// 'left', for a rename or a cleared name of a member that never was, taken out
// of the queue unsent (the editors were told the member is gone). A member
// that never was comes off the write here, whichever tab queued it.
export function _mintedGate(key, d) {
    const g = _minted.gate(d, _mintedCtx());
    for (const r of g.reads) _readMintedMembers(r, d.password);
    if (g.leave && !_inFlight.has(d)) {
        if (_dequeue(key, d, { value: { discarded: true } })) {
            _persistQueue();
            _announceUnsentMemberChange(d);
        }
        return 'left';
    }
    let stripped = false;
    for (const id of g.strip) if (mintedStrip(d, id)) stripped = true;
    if (stripped) _persistQueue();
    if (g.wait) {
        if (g.until) _rerunFlushAt(g.until);
        return 'wait';
    }
    return 'go';
}

// _rerunFlushAt: a flush at `at` (the owner's time for a write it holds back),
// one timer for the earliest.
function _rerunFlushAt(at) {
    if (_mintedTimer !== null && _mintedTimerAt <= at) return;
    if (_mintedTimer !== null) clearTimeout(_mintedTimer);
    _mintedTimerAt = at;
    _mintedTimer = setTimeout(() => {
        _mintedTimer = null;
        _mintedSweep();
        _rerunFlushSoon();
    }, Math.max(0, at - Date.now()) + 1);
}

// The minted-member state above is written by the members code alone; the
// send and the sign-in, park and wipe doors go through these (bc-jsxs).
// _noteMintedOutHere / _endMintedOutHere: this tab's request for the add that
// mints `id` went out, and ended (_sendEntry).
export function _noteMintedOutHere(id) {
    _mintedOutHere.add(id);
}
export function _endMintedOutHere(id) {
    _mintedOutHere.delete(id);
}
// _cancelMintedTimer: the timed rerun goes (a park, a wipe).
function _cancelMintedTimer() {
    if (_mintedTimer !== null) { clearTimeout(_mintedTimer); _mintedTimer = null; }
}
// _resumeMintedReads: a new sign-in (resumeAfterAuth).
// The reads the owner asked for carry the password the add had, which
// the server may have refused since: an organiser's reads take the new
// one (a self-run participant's carries none and stays so), and one the
// refusal left unread is asked for at its time (N5).
// A new sign-in asks again every read this tab owes, whatever the
// earlier tries (Y4, stage 3 review 11): the ones refused for the old
// password were parked, and failures counted under it may have been
// its own, so the bound starts again.
export function _resumeMintedReads(password) {
    _mintedPaused = false;
    if (password) {
        for (const [id, pw] of _mintedReadPw) if (pw) _mintedReadPw.set(id, password);
        _mintedAuthParked.clear();
        _minted.readsResumed([..._mintedReadPw.keys()]);
        _mintedSweep();
    }
}
// _pauseMintedReads: a revoked password (parkQueueForReauth). The members
// reads too: none goes with the revoked password until the new sign-in (Y4).
export function _pauseMintedReads() {
    _mintedPaused = true;
    _cancelMintedTimer();
}
// _wipeMintedRecords: the logout wipe (clearQueue). What is known of the
// minted members goes with the queue it is for.
export function _wipeMintedRecords() {
    _minted.clear();
    _mintedReadPw.clear();
    _mintedAuthParked.clear();
    _mintedPaused = false;
    _cancelMintedTimer();
}

// _readMintedMembers: the read of the team's members the owner asked for,
// with the waiting write's password, the header sent even when empty (the
// public page sends it empty, never omits it). A self-run participant's write
// carries none, and the members door is main-gated, so that read is the
// public competition payload, which carries the same `teamMembers` (L3). A
// read refused (a stale organiser password's 401 included, a 403 counted as a
// failed try) or failed proves
// nothing: the owner keeps the id unknown, and the writes go as they were made.
function _readMintedMembers({ id, c, t }, password) {
    // A read refused for its password waits for a new sign-in, as does every
    // read while a revoked password is parked (Y4, stage 3 review 11).
    if (_mintedReads.has(id) || _mintedPaused || _mintedAuthParked.has(id)) return;
    _mintedReads.add(id);
    // This tab asked, so it asks again if the read fails (_mintedSweep),
    // however the member came to it (an add taken from another tab's queue).
    if (!_mintedReadPw.has(id)) _mintedReadPw.set(id, password || '');
    const gen = _queueGen;
    const url = password ? `/api/competitions/${c}/team-members` : `/api/viewer/competitions/${c}`;
    _fetchJson(url, { headers: { 'X-Tournament-Password': password || '' } }).then(({ res, body }) => {
        // Refused for the password (a stale organiser sign-in): no failed try.
        // A 403 is not that: on this server it means the tournament record is
        // not usable yet and is retried (_sendEntry), so it is a failed try
        // below (Z4).
        if (res.status === 401) return MINTED_READ_AUTH_REFUSED;
        const teams = res.ok && body && body.teamMembers;
        if (!teams || typeof teams !== 'object') throw new Error(`no member list (HTTP ${res.status})`);
        return Array.isArray(teams[t]) ? teams[t] : [];
    }).catch((err) => {
        console.warn(`[sync] could not read team ${t}'s members to learn whether a member added from this device was kept; the writes naming it go as they are:`, err);
        return null;
    }).then((list) => {
        // A logout's wipe since: nothing of this read is kept.
        if (gen !== _queueGen) return;
        if (list === MINTED_READ_AUTH_REFUSED) {
            // Not counted toward the re-read bound, and not asked again with
            // the password the server refused: parked until resumeAfterAuth.
            console.warn(`[sync] the server refused this device's password for team ${t}'s members; asked again after the next sign-in`);
            _mintedAuthParked.add(id);
            return;
        }
        if (_minted.readEnded(id, list, _mintedCtx()) === 'dropped') _learnDropped([{ id, c, t }]);
        // A read that failed is asked again later (backed off, bounded by the
        // owner): the timer for it is set here, whether or not a write names
        // the member (N5).
        else if (list === null) _mintedSweep();
    }).finally(() => {
        _mintedReads.delete(id);
        _rerunFlushSoon();
    });
}

// _learnDropped: members now known never to have been, proved here or by
// another tab (the stored records say so). This tab tells its editors once
// per id (`team-member-dropped`), takes the id off its own queued writes the
// proof covers, and takes out unsent a rename or a cleared name of one.
export function _learnDropped(list) {
    let changed = false;
    // Inside a batch already open, its close does the follow-ups.
    const ownBatch = !_dequeueBatch;
    if (ownBatch) _openDequeueBatch();
    try {
        for (const { id, c, t } of list) {
            if (!id) continue;
            _announceDroppedMember({ competitionId: c, teamId: t, id });
            for (const [k, e] of _writeQueue) {
                if (!mintedProofCovers({ c, t }, e, _teamIdOf(e))) continue;
                if (mintedMemberUrlId(e) === id) {
                    if (!_inFlight.has(e) && _dequeue(k, e, { value: { discarded: true } })) {
                        changed = true;
                        _announceUnsentMemberChange(e);
                    }
                } else if (mintedStrip(e, id)) {
                    changed = true;
                }
            }
        }
    } finally {
        if (ownBatch) _closeDequeueBatch();
    }
    if (changed) {
        _persistQueue();
        _recomputeSyncStatus();
    }
    _rerunFlushSoon();
}

// _mintedPrune: a settled record nothing names any more (no entry of the queue
// as stored, this tab's included, names or mints its id) is forgotten once it
// has been kept a while. The stored copies of other tabs' writes are NOT
// stripped of an id proved never to have been: every send goes through
// _mintedGate first (_flushBase, for the first entry and per tap in a batch),
// in whichever tab or page loads the entry, so the strip would change nothing
// on the wire (stage 3 review 10, N2).
export function _mintedPrune(merged) {
    const named = new Set();
    const adds = new Set();
    for (const e of merged.values()) {
        for (const id of mintedIdsNamed(e)) named.add(id);
        const add = mintedAddId(e);
        if (add) adds.add(add);
    }
    _minted.prune(named, adds);
}

// _announceDroppedMember: the `team-member-dropped` event, once per id per
// tab, whether this tab proved the member never was or heard it from another,
// so the editors take the member off their lists and the id off their
// positions and picks (`mergeMembers` itself never removes a member).
function _announceDroppedMember({ competitionId, teamId, id }) {
    if (!id || _announcedDropped.has(id)) return;
    _announcedDropped.add(id);
    if (typeof window !== 'undefined' && typeof window.dispatchEvent === 'function' && typeof CustomEvent === 'function') {
        window.dispatchEvent(new CustomEvent('team-member-dropped', {
            detail: { competitionId: competitionId || '', teamId: teamId || '', memberId: id },
        }));
    }
}

// _followMintedMembers: a write naming a member id whose add is still queued
// or out (the owner's record, minted_members.jsx) follows that team's last
// member write (`after`), so the member exists when the write reaches the
// server (stage 3 review 4, G1), and a write waiting on an add the server
// keeps refusing is held with it (F6). The ONE place for every door: a lineup
// save, a score write's representative pick or a numbered row's fighter, a
// tap alike, and the one place a match's lineup save follows its team's
// member writes at all (K4). A write on the team's own base already goes
// after it, in strict order. What the add's end proves is the owner's gate.
export function _followMintedMembers(base, descriptor) {
    // A match's lineup save follows every member write of its team, a rename
    // or a cleared name too, not only an add (stage 3 review 7, K4): it may
    // place a member by the name a queued rename gives them, which the server
    // reads only once the rename has landed.
    if (descriptor && descriptor.kind === 'lineup' && descriptor.payload && descriptor.payload.matchId) {
        const teamId = _teamIdOf(descriptor);
        if (teamId && _membersBase(descriptor.compID, teamId) !== base) {
            _addAfter(descriptor, _lastMemberEntryKey(descriptor.compID, teamId));
        }
    }
    for (const id of mintedIdsNamed(descriptor)) {
        const r = _minted.recordOf(id);
        if (!r || (r.s !== 'queued' && r.s !== 'out')) continue;
        const addBase = _membersBase(r.c, r.t);
        if (addBase === base) continue;
        _addAfter(descriptor, _lastMemberEntryKeyOfBase(addBase));
    }
}

// _membersBase: the outbox base of a team's member writes, the base of its
// starting lineup: a starting lineup save made after a member write follows
// it, and a match's lineup save names its last entry in `after` (R1).
function _membersBase(compID, teamId) {
    return _lineupKey(compID, teamId, { round: 0 });
}

// _newClientId: an id this device mints for a team member or a court call, a
// lowercase UUID v4 (the server's state.IsClientUUID).
export function _newClientId() {
    return _randomId();
}

// The member as this device shows it after its own write: stamped now, in
// server time, so it stands over the copy the lists hold until the server's
// own copy comes (stamped when it lands).
export function _shownMember(member, fields) {
    return { ...member, ...fields, modifiedAt: _serverNowMs() };
}
