// Sync status: the status and unsent-write listeners, terminal failures,
// superseded notices and held writes.
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import {
    writeWasRefusedForClock, writeMatchClosed, writeResultLocked, supersededBanner, isFinishedResultWrite,
    tapsShareReason,
} from './write_result.jsx';
import { scoreRowMatchLabel } from './pool_ids.jsx';
import { _revKey, _notifyQueueAlert, _writeQueue, _dequeue, _baseOf } from './api_client_queue.jsx';
import {
    _publishOwnStatus, _forgetOwnStatusAnswersWhere, _clearOwnStatusAnswers, _revertLocalFields,
    _CHECKIN_KINDS, _checkInsOf,
} from './api_client_overlays.jsx';
import { _discardedWhileOut, store, late, _latestDerived, _turnedAwaySeen } from './api_client_store.jsx';

let _syncStatus = /** @type {SyncStatusValue} */ ('synced');
const _syncListeners = new Set();

function _setSyncStatus(s) {
    if (_syncStatus === s) return;
    _syncStatus = s;
    for (const fn of _syncListeners) {
        try { fn(s); } catch (_e) { /* swallow */ }
    }
}

// Terminal-write FAILURE channel. When a queued terminal write (completed score /
// decision / lineup) is permanently discarded: a non-retryable 4xx on retry
// (409 conflict, 400 validation), a corrupt entry, or the mp-q8c6
// server-rejection cap (also applied to running writes): the write never landed.
// Neither auth-ish status is a terminal drop any more (bc-qttl): 401 PARKS the
// write pending re-auth and 403 RETRIES with backoff as a server-config state;
// see the 401/403 branches in _flushQueue below.
// Sync status alone returns to 'synced', which would let the editor
// clear its pending banner and look "saved". This channel lets surfaces show an
// explicit "not saved" state instead.
//
// Payload: {compID, matchID, kind, status, reason, advice?}. `advice` overrides
// the renderers' default "Re-enter the result and submit again." — see
// _notifyScoreSuperseded, the one case where re-entering is the wrong move.
const _terminalFailListeners = new Set();
export function subscribeTerminalWriteFailed(fn) {
    _terminalFailListeners.add(fn);
    return () => _terminalFailListeners.delete(fn);
}
function _notifyTerminalWriteFailed(info) {
    // Answers whether an editor open on that match took the refusal and shows
    // it (its listener returned true). That editor is then the one place the
    // operator is told, and the caller raises no queue alert repeating it
    // ("Only one behaviour allowed"); with none showing it, the alert is the
    // one tell (_tellRefusal).
    let shown = false;
    for (const fn of _terminalFailListeners) {
        try { if (fn(info) === true) shown = true; } catch (_e) { /* swallow */ }
    }
    return shown;
}

// _tellRefusal is the ONE rule for a write the server refused on replay: the
// editor open on its match says it (_notifyTerminalWriteFailed), else the
// queue alert does. Nothing is told twice and nothing goes untold.
export function _tellRefusal(info, alert) {
    if (_notifyTerminalWriteFailed(info)) return;
    _notifyQueueAlert(alert);
}

// The "did this write land?" predicates live in write_result.jsx, a leaf (its
// one import is the first_result_confirm.jsx leaf), so that admin_scoring_shared.jsx can import the same rule without
// importing THIS module (api_client is script-tagged; a module both
// script-tagged and ES-imported loads twice and splits its singleton write
// queue). EVERY consumer imports that leaf directly — this module included —
// so there is no `window` mirror of them to keep in step. See that file for why
// the rule has a single owner.

// bc-lww1: a COMPLETED score the server dropped under the timestamp
// last-write-wins guard (200 {"applied": false}). The operator entered a
// result, the server kept a NEWER one, and nothing they can see says so — the
// response used to echo their own payload back and the SSE broadcast carried
// the discarded values, so every surface agreed with the screen they were
// looking at.
//
// Announced on BOTH channels for the same reason the queued-409 path below uses
// both: _terminalFailListeners reaches the editor open on this match (so its
// pending banner resolves to "not saved" instead of clearing to look saved),
// while _notifyQueueAlert reaches an operator who has already moved to the next
// court.
//
// The `advice` is the load-bearing part and is why this is not just another
// terminal failure. Every other write failure ends in "re-enter the result", and
// here that is actively WRONG: re-entering re-stamps the write with the current
// clock, so it would beat the newer stored result and undo it. The operator has
// to look at what is recorded first.
//
// Running-status drops stay SILENT, as before: a superseded autosave is routine
// noise and the newer state is already what every surface shows.
// The EDITOR-targeted half: resolves the pending banner on the editor open on
// this match. One per match, never coalesced — each names a different match.
export function _notifyScoreSupersededEditor(compID, matchID, body) {
    // Copy owned by write_result.jsx (supersededBanner): the explicit-tap
    // call sites that submit status:"running" build this same banner state
    // directly from the awaited result (via notLandedBanner), since this
    // broadcast is deliberately silent for that status. Same strings, one
    // owner, so a future wording change cannot paste the copy anywhere a
    // fourth time. `body` is the server's answer: a change turned away
    // because the match was finished gets its own advice (writeMatchClosed).
    // Answers whether the editor open on the match shows it, in which case
    // the caller counts it into no superseded alert.
    return _notifyTerminalWriteFailed({ compID, matchID, kind: 'score', status: 200, ...supersededBanner(body) });
}

// The ALERT half: reaches an operator who has already moved on to another
// court. Split from the editor half so a flush pass can emit ONE alert carrying
// a real count, the way the 'unreadable' alert already does.
// Publishing count:1 per drop made queueAlertMessage's plural branch dead code
// and left a court that reconnected with several dropped results looking at a
// single toast reading "A result was not saved".
export function _notifyScoreSupersededAlert(count, compID, matchID, matchClosed = false) {
    if (count <= 0) return;
    _notifyQueueAlert({
        kind: 'superseded', count, terminalCount: count, compID, matchID,
        ...(matchClosed ? { matchClosed: true } : {}),
    });
}

// One message per match (decision q-f6-one-message). A pass notes every tap
// the server did not apply with its match (_noteNotAppliedTap, called by
// api_client_taps.jsx), and every match whose finished result it saw refused
// because the match was already closed, told or held (_noteRefusedResult);
// at its end _notifyNotAppliedTaps tells the taps the server did not apply,
// one alert per match, leaving out the taps of a match whose result the same
// pass refused that way: that refusal is the one message (a phone back online
// after another phone finished the match is told once, by its refused
// Finish). A result refused for another reason (a missing correction reason, a
// representative bout already there) says nothing about the taps, which keep
// their own alert. A tap id is unique only within its match (session and seq),
// so the match is never looked up from the id.
// Out of scope by design: a tap the server first answered as landed and a later
// copy of the match turned away is told on its own by _tellAway
// (api_client_taps.jsx), from a push or a fetch that has no pass to consult.
export function _noteNotAppliedTap(pass, base, d, outcome) {
    pass.notApplied++;
    pass.notAppliedTaps.push({ outcome, base, d });
}
// _refusedAsClosed: a finished result the server turned away because the match
// was already closed: held for it (_heldForClosedMatch) or refused 409
// result_finalized (the self-run judge's closed match).
function _refusedAsClosed(body) {
    return _heldForClosedMatch(body) || (!!body && body.error === 'result_finalized');
}
export function _noteRefusedResult(pass, key, d, body) {
    if (isFinishedResultWrite(d) && _refusedAsClosed(body)) pass.refusedResults.add(_baseOf(key));
}
export function _notifyNotAppliedTaps(pass) {
    const byMatch = new Map();
    for (const { outcome, base, d } of pass.notAppliedTaps) {
        if (pass.refusedResults.has(base)) continue;
        if (!byMatch.has(base)) byMatch.set(base, { d, outcomes: [] });
        byMatch.get(base).outcomes.push(outcome);
    }
    for (const { d, outcomes } of byMatch.values()) {
        const where = _whereOf(d);
        _notifyQueueAlert({
            kind: 'not_applied', count: outcomes.length, terminalCount: 0, compID: d.compID, matchID: d.matchID,
            ...(where ? { where } : {}),
            outcome: outcomes[outcomes.length - 1],
            ...(tapsShareReason(outcomes) ? { sameReason: true } : {}),
        });
    }
}

// _heldForClosedMatch: the answer turned this write's own change away because
// the match was finished, or because the next knockout round depends on its
// result. One remedy either way: Correct.
export function _heldForClosedMatch(body) {
    return writeMatchClosed(body) || writeResultLocked(body) !== null;
}

function _notifyScoreSuperseded(compID, matchID, body) {
    if (_notifyScoreSupersededEditor(compID, matchID, body)) return;
    _notifyScoreSupersededAlert(1, compID, matchID, _heldForClosedMatch(body));
}

// Both derived from the queue itself rather than kept as separate flags: a
// descriptor IS the record of its own state, so there is nothing to leave stale
// when an entry is superseded, confirmed, or resumed.
function _hasAuthBlockedQueued() {
    for (const d of _writeQueue.values()) { if (d && d.authBlocked) return true; }
    return false;
}
function _hasServerErroringQueued() {
    for (const d of _writeQueue.values()) {
        if (_keepsFailing(d)) return true;
    }
    return false;
}
// A held write the server has refused past the notice threshold: the one the
// pill reports as "Not saving" and the operator may discard
// (API.discardFailingHeldWrites).
export function _keepsFailing(d) {
    // A parked batch of taps the server refused (400) can only go by an
    // operator's discard, so it reads as failing at once; so does an entry on
    // a wait cycle (`cycled`, _markWaitCycles), which nothing else can free.
    return _refusedByServer(d) || (!!d && !!d.cycled);
}

// _refusedByServer: the entry is held because the SERVER keeps refusing it
// (a parked tap batch, or past the notice threshold), not because of what
// it waits on.
export function _refusedByServer(d) {
    return !!d && (!!d.parked || !!d.clockRefused || (Number(d.attempts) || 0) >= SERVER_REJECTION_NOTICE_THRESHOLD);
}

// _heldWriteItem: one held write as API.heldWrites() lists it.
export function _heldWriteItem(key, d) {
    return {
        key,
        compID: d.compID || '',
        matchID: d.matchID || '',
        kind: d.kind || 'score',
        terminal: !!d.terminal,
        keepsFailing: _keepsFailing(d),
        authBlocked: !!d.authBlocked,
        held: !!d.held,
        ...(d.parked ? { parked: true } : {}),
        // On a wait cycle (H1): stuck, but not because the server refuses it.
        ...(d.cycled ? { cycled: true } : {}),
        // Where it is (H5): the match's label or the team's name, or ''.
        where: _whereOf(d),
        ...(d.kind === 'lineup' && d.payload
            ? { teamId: d.payload.teamId || '', round: d.payload.round } : {}),
        // A check-in names its participants, so the list can name them.
        ...(_CHECKIN_KINDS.has(d.kind) ? { participantIds: [..._checkInsOf(d).keys()] } : {}),
    };
}

// What this device has shown of a match or a team, so a held write can be
// named by where it is (stage 3 review 5, H5): a match's label as the score
// list labels it (scoreRowMatchLabel), and each side's name by its id, which
// names a team's member and lineup writes. Learned from the match a door is
// handed (recordScore, recordTap, requeueBlockerAndReopen) and from the match
// an editor shows (API.noteShownMatch), and stamped on an entry as it is
// queued (`label`, persisted), so a reload still names it.
export const _shownMatchLabels = new Map();
const _shownTeamNames = new Map();

export function _noteShownMatch(compID, m) {
    if (!compID || !m || typeof m !== 'object' || !m.id) return;
    // Only a match that says its phase: a bracket id read as a pool id
    // ("m-3") would be labelled with the wrong number.
    let label = '';
    if (m.phase === 'pool' || m.phase === 'bracket') {
        try { label = scoreRowMatchLabel(m) || ''; } catch (_e) { label = ''; }
    }
    if (label) _shownMatchLabels.set(_revKey(compID, m.id), label);
    for (const side of [m.sideA, m.sideB]) {
        if (side && typeof side === 'object' && side.id && side.name) _shownTeamNames.set(`${compID}:${side.id}`, String(side.name));
    }
}

// _forgetShownMatches: a competition's draw was generated or discarded, or the
// competition deleted from this device, so its match ids may name other
// matches now (a redraw, or a competition made again under the same name,
// reuses them). The labels this
// device learned for them are forgotten; an entry keeps the label stamped on
// it as it was queued (J4, stage 3 review 6). Team names stay: a team keeps its
// id across draws.
export function _forgetShownMatches(compID) {
    if (!compID) return;
    const prefix = `${compID}:`;
    for (const k of _shownMatchLabels.keys()) if (k.startsWith(prefix)) _shownMatchLabels.delete(k);
    // The copies score-events answers handed back for its matches go too
    // (X4): a match id reused by the new draw must never show the old match's
    // scoreline on a board, nor have it recorded by a Finish.
    _forgetMatchCopiesOf(prefix);
    // And the status moves the server answered for them: a re-created match
    // must never show the old one finished, with its winner (F7).
    if (_forgetOwnStatusAnswersWhere((k) => k.startsWith(prefix))) _publishOwnStatus();
}

// _forgetAllShownMatches: the tournament's competitions may have been replaced
// (an import) or the client was told of a reset, so no learned match label is
// trusted (stage 3 review 7, K8). Another device's import is heard only as a
// `tournament_updated` push, which also follows other tournament-wide changes:
// a label is learned again as soon as its match is shown, and an entry keeps
// the label it was queued with.
export function _forgetAllShownMatches() {
    _shownMatchLabels.clear();
    _forgetAllMatchCopies();
    if (_clearOwnStatusAnswers()) _publishOwnStatus();
}

export function _noteMatchLabel(compID, matchID, label) {
    if (compID && matchID && typeof label === 'string' && label.trim()) _shownMatchLabels.set(_revKey(compID, matchID), label.trim());
}

// _teamIdOf: the team a team write is about (a member write's url, a lineup
// save's payload).
export function _teamIdOf(d) {
    if (d && d.payload && typeof d.payload.teamId === 'string' && d.payload.teamId) return d.payload.teamId;
    const m = String((d && d.url) || '').match(/\/teams\/([^/]+)\//);
    return m ? decodeURIComponent(m[1]) : '';
}

// _whereOf: where one queued write is, in the words above, or ''.
export function _whereOf(d) {
    if (!d) return '';
    if (typeof d.label === 'string' && d.label) return d.label;
    if (d.matchID) return _shownMatchLabels.get(_revKey(d.compID, d.matchID)) || '';
    const teamId = _teamIdOf(d);
    return teamId ? (_shownTeamNames.get(`${d.compID}:${teamId}`) || '') : '';
}
// The one discard of a held write (API.discardHeldWrite and the editors'
// API.discardFailingHeldWrites): only a write the server keeps refusing.
export function _discardIfFailing(key, d) {
    return _keepsFailing(d) && _dequeue(key, d, { value: { discarded: true }, byOperator: true });
}

/**
 * Recompute and publish the correct sync status from current state:
 *   offline       : store.offlineFlag is set and the queue still has entries
 *   auth-required : a queued write is parked awaiting re-authentication
 *   server-error  : a queued write has passed the server-rejection notice
 *                   threshold; it is STILL QUEUED and still being retried
 *   syncing       : any in-flight running write, or the queue (taps
 *                   included) is non-empty
 *   synced        : otherwise
 *
 * bc-qttl: the two middle states exist because writes are no longer discarded
 * when they keep failing. Without them a permanently-failing write would sit
 * under an ordinary "Syncing…" forever, which is the wedged-pill complaint that
 * originally motivated dropping the write (mp-q8c6). Reporting the state is the
 * fix; destroying the operator's result was not.
 *
 * Order is PRIORITY: the most actionable problem wins the pill. Offline outranks
 * both because a dead connection is the root cause an operator can act on first,
 * and neither re-auth nor a server retry can make progress until it is fixed.
 */
export function _recomputeSyncStatus() {
    _setSyncStatus(_deriveSyncStatus());
    _publishUnsentWrites();
    _publishOwnStatus();
}
function _deriveSyncStatus() {
    if (store.offlineFlag && _writeQueue.size > 0) return 'offline';
    if (_hasAuthBlockedQueued()) return 'auth-required';
    if (_hasServerErroringQueued()) return 'server-error';
    return _writeQueue.size > 0 ? 'syncing' : 'synced';
}

// bc-offl: the held-writes COUNT channel, for the admin topbar's indicator
// (admin_shell.jsx), which is mounted on every admin page and so still shows a
// held result after the editor that held it has closed. Separate from
// subscribeSyncStatus on purpose: that one is status-only and deduped by
// status, and its transition sequences are pinned (sync_queue.test.jsx).
//
// Published from _recomputeSyncStatus, which every queue mutation reaches
// (enqueue, the flush pass, rehydrate, the storage-event take-up, the page-hide
// keep, re-auth, park, clear), and ONLY when a count changed: a score editor
// enqueues a tap per scoring gesture, and an undeduped emit would fire per
// tap. Not from _persistQueue, which a rehydrate that dropped nothing
// never calls; subscribing also replays the queue as it is NOW, so a reload
// with a held queue shows its count at once.
//
// Scope: THIS tab's queue, which includes entries taken up from another tab's
// stored outbox (_onStoredQueueChanged). An edit still inside an editor's
// autosave window is not a held write and is not counted (the editor's own
// pill shows it as Syncing...), so the pill and the topbar can disagree for
// that moment; that is correct, not a drift to fix.
const _unsentListeners = new Set();
let _lastUnsent = { total: 0, terminal: 0, authBlocked: 0, failing: 0, held: 0 };
// `failing` counts the held writes past the server-error threshold
// (_keepsFailing), so a SECOND write crossing it is published even though the
// sync status is already "server-error" and no other count moves: the
// editors' notice and Discard and the topbar's list re-read on it.
export function _unsentCounts() {
    let total = 0, terminal = 0, authBlocked = 0, failing = 0, held = 0;
    for (const d of _writeQueue.values()) {
        if (!d) continue;
        // One item per entry, a tap included (D5).
        total++;
        // `terminal` here counts finished results (R5), never every entry
        // carrying its own method and url.
        if (isFinishedResultWrite(d)) terminal++;
        if (d.authBlocked) authBlocked++;
        if (_keepsFailing(d)) failing++;
        if (d.held) held++;
    }
    return { total, terminal, authBlocked, failing, held };
}
function _publishUnsentWrites() {
    const next = _unsentCounts();
    if (next.total === _lastUnsent.total && next.terminal === _lastUnsent.terminal
        && next.authBlocked === _lastUnsent.authBlocked && next.failing === _lastUnsent.failing
        && next.held === _lastUnsent.held) return;
    _lastUnsent = next;
    for (const fn of _unsentListeners) {
        try { fn({ ...next }); } catch (_e) { /* swallow */ }
    }
}
/**
 * Subscribe to the held-writes count ({total, terminal, authBlocked}). Replays
 * the current count on subscribe. Returns an unsubscribe function.
 */
export function subscribeUnsentWrites(fn) {
    _unsentListeners.add(fn);
    try { fn(_unsentCounts()); } catch (_e) { /* swallow */ }
    return () => _unsentListeners.delete(fn);
}

/**
 * Subscribe to sync-status changes. Returns an unsubscribe function.
 * @param {(status: SyncStatusValue) => void} fn
 * @returns {() => void}
 */
export function subscribeSyncStatus(fn) {
    _syncListeners.add(fn);
    // Replay current status so the subscriber can initialise its state.
    try { fn(_syncStatus); } catch (_e) { /* swallow */ }
    return () => _syncListeners.delete(fn);
}
// Per-entry SERVER-rejection (5xx/429) threshold. This used to be a DROP cap
// (mp-q8c6): after 10 rejections the entry was discarded, because a
// deterministic 5xx (a validation bug surfaced as HTTP 500, observed with an
// engi flag check during mp-n19y UAT) would otherwise retry forever, wedging
// the pill on "Syncing…" with no operator signal short of clearing
// localStorage.
//
// bc-qttl inverts that trade. Discarding the write "solved" the wedged pill by
// destroying the operator's result, which is the one outcome the outbox exists
// to prevent, and a persistently-erroring backend is exactly the case where a
// queued score matters most. So the entry now STAYS QUEUED and keeps retrying
// until it lands, and this constant became a NOTICE threshold: on
// crossing it we tell the operator (queue-alert 'server_error') and move the
// pill off "Syncing…" onto a distinct not-saving state. The wedged-pill
// complaint was really a SIGNALLING failure, and it is fixed by signalling.
//
// Network failures (fetch rejected: connection down / timeout) deliberately do
// NOT count: a real offline period is not the server refusing the write. At the
// 8s max backoff, 10 rejections ≈ 1 minute of a persistently-erroring server:
// long enough to ride out a restart behind the TLS proxy (transient 502/503)
// before bothering the operator.
export const SERVER_REJECTION_NOTICE_THRESHOLD = 10;

// _endDiscardWhileOut: the request of an entry discarded while out ended
// (`res`, or none: a network failure or the deadline). Answers true when it
// was one, which its answer then settles nothing of. A 2xx is the write kept:
// nothing is undone. Anything else undoes what the discard would have.
export function _endDiscardWhileOut(d, res, body) {
    if (!_discardedWhileOut.has(d)) return false;
    _discardedWhileOut.delete(d);
    const kept = !!res && ((res.ok && !writeWasRefusedForClock(body || {}))
        || (res.status === 404 && _GONE_IS_DONE_KINDS.has(d.kind)));
    if (!kept) _revertLocalFields(d);
    late._rerunFlushSoon();
    return true;
}

// The removals whose 404 is the outcome they asked for (nothing to take away).
export const _GONE_IS_DONE_KINDS = new Set(['lineup-remove', 'announce-remove']);

// _forgetMatchCopiesOf: the copies score-events answers handed back, and the
// turned-away lists seen, for the matches whose key starts with `prefix` (a
// competition whose draw was generated or discarded, _forgetShownMatches).
export function _forgetMatchCopiesOf(prefix) {
    for (const k of _latestDerived.keys()) if (k.startsWith(prefix)) _latestDerived.delete(k);
    for (const k of _turnedAwaySeen.keys()) if (k.startsWith(prefix)) _turnedAwaySeen.delete(k);
}

// _forgetAllMatchCopies: the same for every match (_forgetAllShownMatches).
export function _forgetAllMatchCopies() {
    _latestDerived.clear();
    _turnedAwaySeen.clear();
}
