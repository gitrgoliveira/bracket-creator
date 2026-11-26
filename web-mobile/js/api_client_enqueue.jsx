// Enqueue: building and tagging a write, coalescing, committing it to the queue,
// and the other tabs' and older builds' queues taken up.
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import { unionChanged } from './match_groups.jsx';
import { joinQueuedLineupSave } from './lineup_save.jsx';
import { DOC_VERSION_FIELD } from './document_versions.jsx';
import { mintedAddId } from './minted_members.jsx';
import {
    _serverNowMs, _perfNow, _payloadSeen, _stampPayload, _carryPayloadSeen, _afterOwnStamp, _noteOwnStamp,
} from './api_client_clock.jsx';
import {
    _revKey, LEGACY_QUEUE_STORAGE_KEY, _isAllowedTerminalRequest, _persistQueue, _writeQueue, _queueSet,
    _settled, _markSettled, _wasSettledHere, _dequeue, _openDequeueBatch, _closeDequeueBatch, _parseStoredQueue,
    _enqueuedAfter, QUEUE_ENTRY_SEP, _baseOf, _newEntryKey, _entryStamp, _entriesFor, _openSettlement,
    _pendingAnswer, _sharedAnswerFor, _claimRefusal, _afterKeys, _addAfter, _lineupKey,
} from './api_client_queue.jsx';
import {
    _eventSession, BASIS_OWN_MAX, _tapSeq, _tapEntriesFor, _publishPendingTaps, _noteOwnTapSeq,
    _adoptStoredTapSeq, _tellTapNotQueued,
} from './api_client_taps.jsx';
import { COURT_NOT_HELD, courtNotHeldAnswer, courtNotHeldError } from './write_result.jsx';
import { _whereOf, _recomputeSyncStatus } from './api_client_status.jsx';
import { _resetFlushBackoff, _flushQueue, _flushAdoptedSoon } from './api_client_flush.jsx';
import { _followMintedMembers } from './api_client_members.jsx';
import { _inFlight, _minted, store, provide, late } from './api_client_store.jsx';
import { consoleWriteRefusal, courtOfControlStamp } from './court_control.jsx';

// _takenFromLegacy: every entry this tab took over from the key an older build
// uses (_takeOverLegacyQueue), its key to the `enqueuedAt` it was taken with,
// kept for the tab's life so none is taken over, and sent, twice (G5). A later
// write an older build stores under the same key (before bc-mrgc phase 3 a key
// named only its match) is newer, and is taken.
const _takenFromLegacy = new Map();

// Descriptors this tab took up from another tab's write to the stored queue
// (_onStoredQueueChanged) rather than queued itself.
const _adopted = new WeakSet();
const WRITE_ID_MAX = 128;
let _writeIdSeq = 0;
// A microtask flush is already kicked for the taps of this tick.
let _tapFlushKicked = false;

function _newWriteId() {
    _writeIdSeq++;
    return `${_eventSession}.w${_writeIdSeq.toString(36)}`.slice(0, WRITE_ID_MAX);
}

// _basisFor: what a rule-bound write was made against (S3d). `seen` is the
// score log line count the screen showed (the match's scoreVersion, or the
// board's, seenScoreVersion); `own` the ids of this device's taps for the match
// still queued or in flight; `seq` the last seq this session queued for it,
// so the server's seq arm counts the device's own taps (operator S8 b).
function _basisFor(base, seen) {
    // `seen` is what the caller's screen showed, said by the caller (bc-evts
    // stage 3 review 11, m1). The copy a score-events answer handed back
    // (_latestDerived) is NOT counted here: it counts only through a board
    // that took it, the individual editor, which says so itself
    // (seenScoreVersion), so a write never claims it saw a result its screen
    // did not show.
    const s = Number(seen) > 0 ? Number(seen) : 0;
    const own = _tapEntriesFor(base).map(([, d]) => d.payload && d.payload.id).filter(Boolean);
    return {
        seen: s,
        own: own.length > BASIS_OWN_MAX ? own.slice(own.length - BASIS_OWN_MAX) : own,
        seq: _tapSeq.get(base) || 0,
    };
}

// _tagWrite: writeId (kept on the payload, so a resend reuses it), basis and
// session on a rule-bound write's payload.
function _tagWrite(payload, base, seen) {
    if (!payload.writeId) payload.writeId = _newWriteId();
    payload.session = _eventSession;
    payload.basis = _basisFor(base, seen);
    return payload;
}

// _outboxWrite: the ONE way a rule-bound door hands a write over. Builds the
// descriptor synchronously, tags it (writeId, basis, session), stamps its
// place in the match's order, enqueues it durably and kicks the flush.
export function _outboxWrite({ kind, compID, matchID, method, url, payload, password, seen, onRefusal, terminal = true, after, base: ownBase, holds, was, label }) {
    // `base`: the queue base when it is not the match's own (a team's starting
    // lineup, `lineup:…`).
    const base = ownBase || _revKey(compID, matchID);
    _tagWrite(payload, base, seen);
    // A running score write (terminal false) carries no method/url: it is
    // always PUT to /score (_sendEntry), the shape older builds queued too.
    const descriptor = terminal
        ? { compID, matchID, payload, password, kind, terminal: true, method, url, enqueuedAt: Date.now() }
        : _runningDescriptor(compID, matchID, payload, password, Date.now());
    // `after`: the queue key of ANOTHER match's entry this write must follow
    // (the court console's start after its send back). Persisted with the
    // entry; the flush holds this one back while that entry is still queued.
    if (typeof after === 'string' && after) descriptor.after = after;
    // `holds`: the other matches (bases) this write also belongs to (a send
    // back to the queue frees its blocker's court): a later write of one of
    // them follows it (_commitEnqueue). Persisted.
    if (Array.isArray(holds) && holds.length > 0) descriptor.holds = holds.filter((b) => typeof b === 'string' && b);
    // `was`: what a court move or time change replaces, or what each
    // participant of a check-in had, put back on a refusal (R4,
    // _revertLocalFields). Persisted.
    if (was && typeof was === 'object') descriptor.was = was;
    // `label`: where the write is, named by the door when no match or team
    // names it (a check-in, a court call, a document; _whereOf). Persisted.
    if (typeof label === 'string' && label) descriptor.label = label;
    const stamped = Number(payload.modifiedAt);
    if (Number.isFinite(stamped) && stamped > 0) payload.modifiedAt = _afterOwnStamp(base, stamped);
    else descriptor.sortStamp = _afterOwnStamp(base, _serverNowMs());
    // A lineup save names when each position it changes was changed, in the
    // server's clock frame: a change made before a kachinuki match started
    // reaches the lineup the match started with whenever it arrives
    // (engine.FollowLineupSave). The queue's join keeps each position's own.
    if (kind === 'lineup' && Array.isArray(payload.changed) && !payload.changedAt && descriptor.sortStamp) {
        payload.changedAt = Object.fromEntries(payload.changed.map((k) => [k, descriptor.sortStamp]));
    }
    // A removal names when it was made too, and the server orders it by that
    // stamp against the saves of the same lineup, like every change (stage 6
    // review 8, m1): one made before a save another device made later never
    // takes that save away, whichever arrives first.
    if (kind === 'lineup-remove' && !payload.removedAt && descriptor.sortStamp) {
        payload.removedAt = descriptor.sortStamp;
    }
    let settled = null;
    const key = _commitEnqueue(base, descriptor, (k) => { settled = _openSettlement(k, onRefusal); });
    if (key === null) return _refusedAtDoor(descriptor);
    return _pendingAnswer(key, settled);
}

// _refusedAtDoor: the answer of a write the court console's gate refused
// (_placeWrite): never queued, so nothing will settle it. It carries a
// `settled` that rejects with the same refusal at once, so a caller that
// follows it learns of the refusal as of any other (a send back's display
// put-back, a member's revert), and a court move, time change or check-in
// the door showed at once is put back (`was`) once the door has shown it.
function _refusedAtDoor(descriptor) {
    const settled = Promise.reject(courtNotHeldError());
    settled.catch(() => {});
    queueMicrotask(() => late._revertLocalFields(descriptor));
    return { ...courtNotHeldAnswer(), settled };
}

// _lastEntryKey: the queue key of a base's last entry in replay order, or ''.
export function _lastEntryKey(base) {
    const list = _entriesFor(base);
    return list.length ? list[list.length - 1][0] : '';
}

// _heldBy: the key of the newest queued entry of ANOTHER base that names this
// base in its `holds`, or ''. A write of this base made while one is queued
// follows it (`after`), so it never overtakes a write that belongs to both.
function _heldBy(base) {
    let key = '';
    let at = -Infinity;
    for (const [k, d] of _writeQueue) {
        if (!d || !Array.isArray(d.holds) || !d.holds.includes(base) || _baseOf(k) === base) continue;
        const s = _entryStamp(d);
        if (s >= at) { at = s; key = k; }
    }
    return key;
}

// _isFollowed: whether some queued entry names `key` in its `after`.
export function _isFollowed(key) {
    for (const e of _writeQueue.values()) if (_afterKeys(e).includes(key)) return true;
    return false;
}

// _coalesceTarget: the one entry a new write may take the place of, or null
// when it is appended as an entry of its own. A SCORE write coalesces only
// into its match's LAST entry, only when that entry is a score write too (the
// same board's state, which the newer write restates, claiming its groups
// through the union), and only when the new write is not older than it. A
// RUNNING write takes the place of a running one only: never of a queued
// Finish or correction. A Finish or correction may take the place of either
// (a later Finish, or the editor's Retry re-sending the queued one, is the
// same result restated, not a second one). A LINEUP save joins the queued save
// of the same lineup (the base names the lineup): _placeWrite makes the one
// entry say what both changed. A decision is never replaced,
// and nothing coalesces across one, nor into an entry already being sent.
function _coalesceTarget(base, descriptor) {
    if (!descriptor) return null;
    const list = _entriesFor(base);
    const last = list.length ? list[list.length - 1] : null;
    if (!last) return null;
    const [lastKey, prev] = last;
    if (!prev) return null;
    // Never an entry another queued write follows (`after`: a send back
    // follows the blocker's last write, stage 3 review 5, H1). A write taking
    // its place would carry everything the new write follows, the send back
    // itself among them (_heldBy), and the two would wait on each other for
    // good; and what follows the entry would then also wait on a write made
    // after it. The new write is queued behind it instead, in order.
    if (_isFollowed(lastKey)) return null;
    // A lineup save joins a queued save of the SAME lineup (team, and match or
    // round) when it is the base's last entry: a match's base holds both
    // teams' lineups and the match's other writes, and a save never moves
    // ahead of a write made before it. One already out is joined too: the
    // joined entry takes its key, so its answer does not settle it and the
    // flush sends the union straight after (a position sent twice lands once).
    if (descriptor.kind === 'lineup') return _sameLineup(prev, descriptor) ? last : null;
    // Never a score entry already being sent: its answer settles it, and a
    // write made since goes after it (_inFlight, set by _sendEntry).
    if (_inFlight.has(prev)) return null;
    // A whole-document save (the tournament settings, a competition's awards)
    // restates the document, so it takes the place of a queued save of the
    // same document not yet out (bc-obox G4b).
    if (DOC_VERSION_FIELD[descriptor.kind]) return prev.kind === descriptor.kind ? last : null;
    if (descriptor.kind !== 'score') return null;
    if (prev.kind !== 'score') return null;
    if (prev.terminal && !descriptor.terminal) return null;
    if (_entryStamp(descriptor) < _entryStamp(prev)) return null;
    return last;
}

// _migratedLegacyLineup: the key a save of a MATCH's lineup an older build
// queued under its own `lineup:…:match:…` base is moved to, or null for any
// other entry (R9). Moved onto the match's base when it loads (rehydrate) or
// is taken up from another tab, it is sent in the match's order with the
// match's other writes, and a new save of the lineup joins it there
// (_coalesceTarget) as it joins any queued save of the same lineup.
export function _migratedLegacyLineup(key, descriptor) {
    const base = _baseOf(key);
    const p = descriptor && descriptor.kind === 'lineup' && descriptor.payload;
    if (!p || !p.matchId || !descriptor.compID) return null;
    if (base !== _lineupKey(descriptor.compID, p.teamId, { matchId: p.matchId })) return null;
    // Deterministic, so two tabs moving the same entry make one entry.
    const id = String(key).split(QUEUE_ENTRY_SEP).join('.');
    return `${_revKey(descriptor.compID, p.matchId)}${QUEUE_ENTRY_SEP}legacy.${id}`;
}

// _sameLineup: two lineup entries save the same lineup (a probe names only
// the team, matchId and round it asks about).
function _sameLineup(a, b) {
    const pa = (a && a.kind === 'lineup' && a.payload) || null;
    const pb = (b && b.payload) || null;
    if (!pa || !pb || pa.teamId !== pb.teamId) return false;
    return String(pa.matchId || '') === String(pb.matchId || '')
        && String(pa.round ?? '') === String(pb.round ?? '');
}

// _queuedLineupSave: whether a save of this lineup (a team's lineup for a match,
// or its round) is still in the outbox, sent or not yet answered. Every lineup
// save is an outbox entry (bc-evts M5): a match's in its match's order, a
// starting lineup under its own `lineup:` base, and a save made while the
// lineup's save is the base's last entry joins it (_coalesceTarget,
// _placeWrite), each position keeping its own `changedAt`, by which the server
// orders it (decision L1). The server's copy lacks a queued save, so this is
// what keeps a read answered meanwhile from putting the old lineup back over
// the pick the team sheet shows (matchLineupSaveQueued). A removal does not
// wait on it: it is an entry of the same base, sent after the save and ordered
// by its own stamp (`removedAt`), so the queued save never undoes it.
export function _queuedLineupSave(compID, teamId, { matchId, round } = {}) {
    const probe = { payload: { teamId, ...(matchId ? { matchId } : { round }) } };
    // A match's lineup is an entry of its match (bc-evts M5; one an older
    // build queued under its own base is moved there as it loads, R9).
    const bases = matchId
        ? [_revKey(compID, matchId)]
        : [_lineupKey(compID, teamId, { round })];
    return bases.some((b) => _entriesFor(b).some(([, d]) => _sameLineup(d, probe)));
}

// _storedEntryProblem: why a stored queue entry cannot be replayed
// ('unreadable'), or null when it can. The checks a rehydrate applies, stated
// once so taking up another tab's entries (_onStoredQueueChanged) applies them
// too. An entry is never refused for its age (bc-mrgc phase 3): the server
// orders it by its stamp and keeps what a newer change outranks in the
// match's history, so sending it loses nothing and discarding it would.
export function _storedEntryProblem(descriptor) {
    if (!descriptor || typeof descriptor !== 'object') return 'unreadable';
    // enqueuedAt must be a finite positive number: it orders the entry and
    // tells two copies of it apart (_enqueuedAfter), and a tampered/corrupt
    // value (a string, NaN) would make both comparisons silently false.
    if (typeof descriptor.enqueuedAt !== 'number'
        || !Number.isFinite(descriptor.enqueuedAt)
        || descriptor.enqueuedAt <= 0) return 'unreadable';
    // Defense-in-depth: localStorage is tamperable. Reject terminal entries
    // whose method/url fall outside the queue allowlist (PUT/POST to
    // /api/competitions/…) before they can ever reach fetch on replay.
    if ((descriptor.terminal || descriptor.kind === 'evt')
        && !_isAllowedTerminalRequest(descriptor.method, descriptor.url)) return 'unreadable';
    // A tap is sent to the score-events door and nowhere else.
    if (descriptor.kind === 'evt' && (descriptor.method !== 'POST'
        || !String(descriptor.url).split('?')[0].endsWith('/score-events'))) return 'unreadable';
    return null;
}

// Shared post-enqueue tail for every queue path: place the write in its
// match's list (_placeWrite: coalesced into the last running score write, or
// an entry of its own), persist, republish sync status, reset the backoff
// counter (a fresh user write must not inherit a stale max-backoff delay from
// a prior failure run), and kick a flush. Returns the entry's key, or null when
// the court console's gate refused the write (_placeWrite): nothing queued.
// The ONE enqueue: every outbox entry, a tap included, is stored here.
// `nextTick`: kick the flush in a microtask instead of now, so the taps of one
// tick go in one batch (recordTap).
export function _commitEnqueue(base, descriptor, beforeFlush, { nextTick = false } = {}) {
    // bc-cse: the monotonic half of the clock-skew re-stamp. performance.now()
    // is immune to wall-clock steps, so the difference between this reading and
    // one taken at flush time is the write's TRUE age even if the OS corrects
    // the clock in between. Deliberately NOT persisted (see _persistQueue): the
    // value is relative to THIS document's time origin, so a reading rehydrated
    // from a previous session, or written by another tab, would be arithmetic on
    // two different zeros. An entry without it simply falls back to the
    // wall-clock reconstruction, which is what every entry did before.
    descriptor.perfAtEnqueue = _perfNow();
    // bc-hlck: what the write was made against, so a re-stamped replay keeps
    // the floor its first stamp had. Persisted (it is a server-time stamp).
    if (_payloadSeen.has(descriptor.payload)) descriptor.seenModifiedAt = _payloadSeen.get(descriptor.payload);
    // Where the write is, as this device names it (H5, _whereOf). Persisted.
    if (!descriptor.label) {
        const where = _whereOf(descriptor);
        if (where) descriptor.label = where;
    }
    // A write of a match another match's queued write also belongs to (a send
    // back to the queue frees this match's court) follows that write.
    const holder = _heldBy(base);
    if (holder) _addAfter(descriptor, holder);
    const key = _placeWrite(base, descriptor);
    // Refused by the court console's gate: nothing is queued. A tap's editor
    // is told through the tap-outcome channel (recordTap reads nothing back).
    if (key === null) {
        if (descriptor.kind === 'evt') _tellTapNotQueued(base, descriptor.payload, COURT_NOT_HELD);
        return null;
    }
    // Which shiaijo the write's court control was given for (court_control.jsx):
    // persisted with the entry, so the tablet keeps the control while any tab
    // still holds it (the release on pagehide).
    const ccCourt = courtOfControlStamp(descriptor.payload && descriptor.payload.courtControl);
    if (ccCourt) descriptor.courtControlCourt = ccCourt;
    // A write naming a member this device minted follows that member's add,
    // whatever the write (G1): read on the payload as placed, a union with the
    // write it took the place of.
    _followMintedMembers(base, descriptor);
    // An add's id is known to the owner before the add is stored, so a tab
    // that takes the add up already finds it queued.
    if (mintedAddId(descriptor)) _minted.minted(descriptor);
    _queueSet(key, descriptor);
    // Read BEFORE the flush is kicked: the flush's synchronous part can
    // persist again, and it is THIS write's persist the answer reports.
    store.lastEnqueuePersisted = _persistQueue();
    // The door's promise exists before anything can settle it.
    if (typeof beforeFlush === 'function') beforeFlush(key);
    _recomputeSyncStatus();
    if (descriptor.kind === 'evt') _publishPendingTaps(base);
    const kick = () => {
        _resetFlushBackoff();
        _flushQueue();
    };
    if (!nextTick) kick();
    else if (!_tapFlushKicked) {
        _tapFlushKicked = true;
        queueMicrotask(() => { _tapFlushKicked = false; kick(); });
    }
    return key;
}

// _placeWrite: the key a new write about `base` goes under. When it coalesces
// (_coalesceTarget), it takes the queued entry's key and carries what that
// write said that a later write must not lose: the groups a score write
// changed (_claimingQueuedGroups), a confirmed forceCourt if either carried it,
// and, for a running write, a pending kachinukiBoutFinal (enqueueRunningWrite's
// comment says why); a lineup save the positions the queued save changed
// (joinQueuedLineupSave). Otherwise it is a new entry, after the others in
// replay order. null when the court console's gate refuses the write.
function _placeWrite(base, descriptor) {
    // bc-sseq FR9: the ONE write gate. While the court console on screen does
    // not run its shiaijo, a new write is refused here, never queued (null).
    // The withdrawal of a court call is cleanup the new holder wants too.
    if (descriptor.kind !== 'announce-remove' && consoleWriteRefusal()) return null;
    // A tap is never merged; its key names its event id.
    if (descriptor.kind === 'evt') return `${base}${QUEUE_ENTRY_SEP}evt:${descriptor.payload.id}`;
    const target = _coalesceTarget(base, descriptor);
    if (!target) return _newEntryKey(base);
    const [key, prev] = target;
    // The write it takes the place of was made against a match at least as
    // old: the later floor stands for both.
    if (Number(prev.seenModifiedAt) > (Number(descriptor.seenModifiedAt) || 0)) {
        descriptor.seenModifiedAt = prev.seenModifiedAt;
    }
    // A document save keeps the version the queued save it replaces was made
    // against: the change another device made since that save is refused, not
    // written over, whichever of the two screens the operator saved from.
    const docField = DOC_VERSION_FIELD[descriptor.kind];
    if (docField) {
        descriptor.payload = { ...descriptor.payload, [docField]: prev.payload ? prev.payload[docField] : descriptor.payload[docField] };
        // The organiser's confirmation that the new public-names default is
        // applied to every competition stands for the save that takes its
        // place only while that save sends the same default: a confirmed save
        // applies its default to every competition, so a later, different
        // default was never shown in a list and must be asked about again.
        if (prev.payload && prev.payload.confirmPublicNames
            && prev.payload.defaultPublicNames === descriptor.payload.defaultPublicNames) {
            descriptor.payload.confirmPublicNames = true;
        }
        for (const k of _afterKeys(prev)) _addAfter(descriptor, k);
        return key;
    }
    let payload = descriptor.kind === 'lineup'
        ? joinQueuedLineupSave(prev.payload, descriptor.payload)
        : _claimingQueuedGroups(prev, descriptor.payload);
    if (!descriptor.terminal && prev.payload && prev.payload.kachinukiBoutFinal
        && payload && !payload.kachinukiBoutFinal) {
        payload = { ...payload, kachinukiBoutFinal: true };
    }
    if ((prev.payload && prev.payload.forceCourt) || (descriptor.payload && descriptor.payload.forceCourt)) {
        payload = { ...payload, forceCourt: true };
    }
    // The organiser's confirmed correction of a finished league stands for the
    // write that takes its place, like a confirmed forceCourt.
    if ((prev.payload && prev.payload.confirmFinished) || (descriptor.payload && descriptor.payload.confirmFinished)) {
        payload = { ...payload, confirmFinished: true };
    }
    // So does their confirmation that the bout's result replaces the lots.
    if ((prev.payload && prev.payload.confirmLots) || (descriptor.payload && descriptor.payload.confirmLots)) {
        payload = { ...payload, confirmLots: true };
    }
    // Whatever the replaced write had to follow, this one follows too.
    for (const k of _afterKeys(prev)) _addAfter(descriptor, k);
    descriptor.payload = payload;
    return key;
}

/**
 * Enqueue a running-status write for offline-resilient delivery. It takes the
 * place of the match's last queued write only when that is a running score
 * write too (_coalesceTarget); otherwise it is queued after what is there.
 * Running writes always PUT to the score endpoint, so method/url are omitted:
 * _flushQueue reconstructs that path for terminal=false.
 *
 * kachinukiBoutFinal is an edge-triggered COMMAND (the operator pressed
 * "Record bout": the server must run kachinuki advancement once), while the
 * rest of the payload is level-triggered state that coalescing may fold
 * freely. If the queued entry it takes the place of carries the flag and the
 * new write does not (a later autosave while still offline), the flag is
 * carried forward (_placeWrite), otherwise the reconnect flush would deliver
 * the score but silently skip advancement and the winner-stays sequence would
 * stall until the operator pressed Record bout again. Safe to re-deliver:
 * MaybeAdvanceKachinuki bails when the last numbered bout has no outcome or
 * the match is completed.
 */
export function enqueueRunningWrite(compID, matchID, payload, password) {
    _commitEnqueue(_revKey(compID, matchID), _runningDescriptor(compID, matchID, payload, password, Date.now()));
}

// _claimingQueuedGroups (bc-mrgc): a score write that takes the place of a
// queued score write for the same match carries the later state, so it must
// also claim every group the one it replaces changed, or that change would
// never be applied: the server applies only the groups a write names
// (match_groups.jsx). Returns the payload with `changed` the union of both
// (unionChanged: no list on either side means every group, and stays so), or
// the payload untouched when there is no queued score write. A COPY, never a
// mutation: the live fetch and _broadcastPatch share the caller's object.
function _claimingQueuedGroups(prev, payload) {
    if (!prev || prev.kind !== 'score' || !prev.payload || !payload) return payload;
    const changed = unionChanged(prev.payload.changed, payload.changed);
    const { changed: _own, ...rest } = payload;
    return changed === undefined ? rest : { ...rest, changed };
}

// The queue entry for a running score write.
function _runningDescriptor(compID, matchID, payload, password, enqueuedAt) {
    return {
        compID, matchID, payload, password,
        kind: 'score', terminal: false,
        enqueuedAt,
    };
}

// _withoutDownstreamConfirmation is the ONE owner of "a queued write NEVER
// carries the operator's downstream confirmation". A "Proceed anyway" answered
// one specific refusal, naming the knockout matches that stood then; a replay
// minutes or hours later would reopen whatever stands by then, which nobody
// confirmed. So a replay that still meets the refusal gets the 409 and is
// dropped with its queue copy (downstreamKnockoutPlayedQueueDrop).
//
// Applied at BOTH ends of the queue: _persistQueue, so the stored copy
// (and localStorage) never holds it, and _flushQueue, the one door EVERY
// replay goes through, so an entry that already holds it cannot send it
// either. That second call is not redundant: an entry queued by an older
// bundle, before the enqueue strip existed, is rehydrated from localStorage
// verbatim and replayed by this bundle's flush loop. Returns a COPY, never a
// mutation: the live fetch and _broadcastPatch share the caller's object.
export function _withoutDownstreamConfirmation(payload) {
    const { forceDownstreamReopen: _confirmation, ...rest } = payload || {};
    // The copy keeps what the write was floored by (bc-hlck, _stampPayload).
    _carryPayloadSeen(payload, rest);
    return rest;
}

/**
 * F5: Enqueue a terminal write for offline-resilient delivery.
 * A terminal SCORE write (a Finish, a correction) takes the place of the
 * match's last queued write when that is a score write (running, or a Finish
 * the editor's Retry is re-sending), claiming its groups (_coalesceTarget,
 * _placeWrite); a lineup save joins the queued save of its lineup; every
 * other terminal write (a decision) is queued after what is there
 * and replays in order. Nothing queued is ever replaced by it otherwise.
 * @param {string} key        - Queue BASE: the match the write is about (use
 *                              _revKey, or the lineup base)
 * @param {WriteKind} kind    - 'score' | 'decision' | 'lineup'
 * @param {string} method     - HTTP method ('PUT' | 'POST')
 * @param {string} url        - Same-origin request path (e.g. /api/competitions/…)
 * @param {object} payload    - Request body object
 * @param {string} password   - X-Tournament-Password value
 * @param {string} compID     - Competition ID (for identity tracking)
 * @param {string} matchID    - Match ID (for identity tracking)
 */
// bc-sync: another tab changed the stored queue. Each tab sends only what it
// holds, so without this a write queued in a tab that then closed (while
// offline, say) waited in storage for the next page load, while the tabs
// still open read "Synced". `storage` fires in every OTHER tab when one writes
// the key, so each tab follows the others:
//   - an entry it does not hold, or holds an older copy of, is taken up
//     (after the rehydrate's checks) and sent like its own;
//   - its copy of an entry the writer took out of storage (in the old value,
//     gone from the new one, same enqueuedAt) is dropped: the merge only ever
//     removes what a tab settled (landed, refused, or dropped on load), so
//     there is nothing left to send;
//   - a removed key (the writer's queue went empty, or was wiped at logout)
//     drops only the entries taken up this way; this tab's own are written
//     back by its next persist.
// A write two tabs hold is sent by the tab that holds its base's lease
// (base_sender_lease.jsx); a send both make anyway is harmless: the server
// takes an equal stamp again and refuses an older one (last-write-wins).
export function _onStoredQueueChanged(oldRaw, newRaw) {
    const before = _parseStoredQueue(oldRaw);
    const after = newRaw === null ? null : _parseStoredQueue(newRaw);
    let changed = false, adopted = false, migrated = false, dropped = false;
    // Another tab's batch can take many entries out at once: their follow-ups
    // are done once (K6). What a member add another tab took out proved is in
    // the owner's records, which that tab wrote first (minted_members.jsx).
    _openDequeueBatch();
    try {
        for (const [k, d] of _writeQueue) {
            const gone = after === null
                ? _adopted.has(d)
                : !after.has(k) && before.get(k)?.enqueuedAt === d.enqueuedAt;
            // An entry whose request this tab has out is settled by that
            // request's own answer, never as sent by another tab: a refusal
            // the other tab drew (a court_busy its host cannot ask about)
            // still reaches the caller here, whose host may ask (Start
            // anyway) or say it. A sign-out's wipe (after === null) still
            // takes it at once.
            if (gone && after !== null && _inFlight.has(d)) continue;
            if (!gone) continue;
            // The tab that sent it left its answer: the caller here reads it as
            // it would have had it from its own request (_sharedAnswerFor).
            const answer = after === null ? undefined : _sharedAnswerFor(k, d);
            if (answer && answer.error) _claimRefusal(k, answer.error);
            if (_dequeue(k, d, answer)) changed = dropped = true;
        }
    } finally {
        _closeDequeueBatch();
    }
    // This tab's own stale persist may have put a dropped entry back in storage
    // while it had not seen it leave: the persist takes it out again.
    if (dropped) _persistQueue();
    for (let [k, d] of after || []) {
        const held = _writeQueue.get(k);
        if (held && !_enqueuedAfter(d, held.enqueuedAt)) {
            // The same copy, but another tab may have re-seqd the tap
            // (_reseqUnheldTap, R10): take its seq, or this tab would send the
            // old one and its next tap could reuse the new one.
            if (_adoptStoredTapSeq(k, held, d)) changed = true;
            continue;
        }
        // A copy of an entry this tab already settled, written back by a tab
        // that still held it: sent already, never taken up again (K2).
        if (!held && _wasSettledHere(k, d)) continue;
        if (_storedEntryProblem(d)) continue;
        if (d.payload && d.payload.forceDownstreamReopen) d.payload = _withoutDownstreamConfirmation(d.payload);
        // An older build's match lineup goes onto its match's base (R9).
        const moved = _migratedLegacyLineup(k, d);
        if (moved) {
            _markSettled(k, d);
            migrated = true;
            if (_writeQueue.has(moved)) continue;
            k = moved;
        }
        _queueSet(k, d);
        _noteOwnStamp(k, d);
        _noteOwnTapSeq(k, d);
        _adopted.add(d);
        changed = adopted = true;
    }
    if (migrated) _persistQueue();
    if (!changed) return;
    _recomputeSyncStatus();
    if (adopted) _flushAdoptedSoon();
}

// _takeOverLegacyQueue: takes over the entries an older build stored under the
// old key (`raw`, its serialized list), F4. Each is taken as the rehydrate
// takes a stored one (held, checked, an old match lineup moved onto its match),
// unless this tab holds that key already, and stored under this build's key;
// once that has landed the old key is handed back EMPTY. Empty, not removed: a
// tab of an older build drops every entry of its own that the stored list no
// longer holds (as sent by another tab), so it stops sending what was taken
// over, where a removal only makes it drop what it took up from other tabs.
// An entry it has out at that moment can be sent by both, which the server
// answers as it answers any resend. The new owner sends them in the one replay
// order of their match (_byEntryOrder). If this build's key cannot be written,
// the old key is left as it is, so nothing is lost. True when any was taken.
export function _takeOverLegacyQueue(raw) {
    const stored = _parseStoredQueue(raw);
    if (stored.size === 0) return false;
    let taken = false;
    for (const [key, d] of stored) {
        if (!d || typeof d !== 'object' || _storedEntryProblem(d)) continue;
        const at = _migratedLegacyLineup(key, d) || key;
        if (_writeQueue.has(at)) continue;
        // G5: an entry this tab already took over is never taken again: an
        // older tab that has not yet read the "[]" handed back stores its list
        // again, and the entry it still holds is one this tab has sent (or an
        // operator discarded). Nor one this tab settled since its last persist.
        if (_takenFromLegacy.has(at) && !_enqueuedAfter(d, _takenFromLegacy.get(at))) continue;
        if (_settled.has(at) && !_enqueuedAfter(d, _settled.get(at))) continue;
        d.held = true;
        if (d.payload && d.payload.forceDownstreamReopen) d.payload = _withoutDownstreamConfirmation(d.payload);
        _queueSet(at, d);
        _takenFromLegacy.set(at, typeof d.enqueuedAt === 'number' ? d.enqueuedAt : Infinity);
        _noteOwnStamp(at, d);
        _noteOwnTapSeq(at, d);
        taken = true;
    }
    if (!_persistQueue()) return taken;
    try { localStorage.setItem(LEGACY_QUEUE_STORAGE_KEY, '[]'); } catch (_e) { /* the next load tries again */ }
    return taken;
}

// The daihyosen add (POST) or remove (DELETE), stamped like recordDecision,
// an entry of its match's outbox. A refusal shows the server's own sentence
// when it sends one (_refusalText), e.g. a finished match's.
export function _daihyosenRequest(method, compID, matchID, password, seenModifiedAt, opts = {}) {
    // bc-evts: the representative bout's add (POST) and remove (DELETE) are
    // the outbox's like every rule-bound write. `settled` resolves the stored
    // match (_daihyosenOutcome: a refusal body when it was held), or rejects
    // with the not-added/not-removed sentence (_doorRefusal).
    return _outboxWrite({
        kind: 'daihyosen', compID, matchID, method,
        url: `/api/competitions/${compID}/matches/${matchID}/daihyosen`,
        payload: _stampPayload({}, _serverNowMs(), seenModifiedAt), password,
        seen: opts && opts.seen, onRefusal: opts && opts.onRefusal,
    });
}

// The calls lower modules make into this one (api_client_store.jsx).
provide({ _outboxWrite, _withoutDownstreamConfirmation, _takeOverLegacyQueue });
