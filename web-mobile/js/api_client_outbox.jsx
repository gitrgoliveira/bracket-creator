// The outbox's API members: what this device holds queued (by kind, held
// writes, unsent counts) and the discards, taps and their board, the editor's
// own status moves, local match and participant patches, the court-move
// queries, and the sign-in park, resume and clear.
//
// A piece of the API object (bc-jsxs): api_client.jsx spreads every piece into
// `API` (api_client_tournament.jsx, api_client_competitions.jsx,
// api_client_matches.jsx, api_client_teams.jsx, api_client_outbox.jsx, and the
// two event members in api_client_events.jsx), so `window.API` keeps one shape.
// Declaration-only: at load it builds its object and runs nothing else, so the
// import cycles between the outbox's pieces stay safe (check-load-cycles.mjs).
// A member another member calls is a named function of the module that owns it,
// called directly, never through `API`. Never import api_client.jsx from here
// (it would run the outbox twice).

import { isFinishedResultWrite } from './write_result.jsx';
import { _serverNowMs, _stampAfterSeen, _afterOwnStamp } from './api_client_clock.jsx';
import {
    _revKey, QUEUE_STORAGE_KEY, LEGACY_QUEUE_STORAGE_KEY, _notifyQueueAlert, _countTerminalQueued,
    _persistQueue, _writeQueue, _wipeQueue, _baseOf, _byEntryOrder, _entriesFor, _entryHooks, _settleKey,
    _parkStoredEntries,
} from './api_client_queue.jsx';
import {
    _ownStatusMove, _subscribeOwnStatusMoves, _clearOwnStatusAnswers, _forgetOwnStatusMove,
    _forgetOwnStatusMovesNotIn, subscribeLocalMatchPatch, _pendingMatchFields, _CHECKIN_KINDS,
    subscribeLocalParticipantPatch, _checkInBase, _checkInsOf,
} from './api_client_overlays.jsx';
import {
    _eventSession, _derivedListeners, _pendingTapListeners, _tapOutcomeListeners, _pruneToldOnBoard,
    _nextTapSeq, _claimToldHere, _forgetTurnedAwayAndTold, _forgetDerivedCopies, _queueBoardTold, _listenOn,
    _pendingTapsFor, _publishPendingTaps, _told, _forgetLandedTaps,
} from './api_client_taps.jsx';
import {
    _keepsFailing, _refusedByServer, _heldWriteItem, _noteShownMatch, _discardIfFailing, _recomputeSyncStatus,
    _unsentCounts,
} from './api_client_status.jsx';
import {
    _afterDiscard, _resetFlushBackoff, _setOffline, _bumpQueueGen, _flushQueue, _blockerOf, _queueIndex,
    _newWalk,
} from './api_client_flush.jsx';
import { _commitEnqueue } from './api_client_enqueue.jsx';
import { _resumeMintedReads, _pauseMintedReads, _wipeMintedRecords } from './api_client_members.jsx';
import { _latestDerived, store } from './api_client_store.jsx';

// bc-obox C1/T1: the ONE reader of "this competition still has queued
// writes of these kinds": how many entries this device's outbox holds for
// `compID` whose kind is one of `kinds` (an array or a Set; a write an
// older build stored with no kind is a score write). Pure: it asks nothing
// about the connection. `compID` null counts every competition's, and the
// writes that belong to none (the tournament settings, `''`). Re-read it on
// subscribeUnsentWrites, which is told whenever an entry joins or leaves.
// The draw waits for API.CHECK_IN_WRITE_KINDS; a move for ['time'].
export function queuedWritesOf(compID, kinds) {
    const set = kinds instanceof Set ? kinds : new Set(Array.isArray(kinds) ? kinds : []);
    let n = 0;
    for (const d of _writeQueue.values()) {
        if (!d || !set.has(d.kind || 'score')) continue;
        if (compID === null || compID === undefined || d.compID === compID) n++;
    }
    return n;
}

// heldWriteState: both answers the editors' banner reads, from ONE read
// of the queue (stage 3 review 5, H2: the two asked apart scanned and
// sorted it at every step of the walk): `keepsFailing`, whether a write
// of the match or one it waits on keeps failing, and `blocker`, that
// write when it is another base's (heldWriteBlocker), else null.
function heldWriteState(compID, matchID) {
    const base = _revKey(compID, matchID);
    const index = _queueIndex();
    const own = index.byBase.get(base) || [];
    // The match's own write is the one the server keeps refusing: that
    // is what the banner says, and what its Discard discards.
    if (own.some(([, d]) => _refusedByServer(d))) return { keepsFailing: true, blocker: null };
    // Otherwise the first write of ANOTHER base it waits on that keeps
    // failing, walked through its own entries (one of them on a wait
    // cycle, H1, is passed through to the write of the other match on it).
    const stuck = (d, key) => _baseOf(key) !== base && _keepsFailing(d);
    const walk = _newWalk();
    for (const [key] of own) {
        const found = _blockerOf(key, stuck, walk, index);
        if (found) return { keepsFailing: true, blocker: _heldWriteItem(found, _writeQueue.get(found)) };
    }
    return { keepsFailing: own.some(([, d]) => _keepsFailing(d)), blocker: null };
}

// heldWriteBlocker: the write of ANOTHER base this match's writes wait on
// that the server keeps refusing (stage 3 review 4, G2: a member add a
// lineup save or a representative pick follows), as a heldWrites() item;
// null when the match's own writes keep failing (those are its own to
// say), or when nothing it waits on does. Found through what each entry
// follows and what its own base sends before it (_blockerOf).
function heldWriteBlocker(compID, matchID) {
    return heldWriteState(compID, matchID).blocker;
}

export const outboxApi = {
    queuedWritesOf,
    // bc-obox C1: what this device's queued check-ins for `compID` found on
    // their rows, per participant id: the `was` of the EARLIEST queued entry
    // naming the id, what the row showed before this device touched it (the
    // check-in as last fetched or pushed). An id no queued entry names is not
    // in the map; one whose entry carries no `was` for it reads false. Pure,
    // like queuedWritesOf; the draw's "now anyway" confirm counts off it
    // (serverHeldCheckIns, qualifier_preview.jsx).
    queuedCheckInsWere(compID) {
        const out = new Map();
        for (const [, d] of _entriesFor(_checkInBase(compID))) {
            const was = d && d.was && typeof d.was === 'object' ? d.was : {};
            for (const pid of _checkInsOf(d).keys()) {
                if (!out.has(pid)) out.set(pid, Object.prototype.hasOwnProperty.call(was, pid) && !!was[pid]);
            }
        }
        return out;
    },
    CHECK_IN_WRITE_KINDS: Object.freeze([..._CHECKIN_KINDS]),
    // -------------------------------------------------------------------------
    // F5: hasPendingTerminalWrite: returns true when the queue holds a TERMINAL
    // entry for the given (compID, matchID) score/decision key.
    // The score editor uses this to show a "not yet saved / retry" indicator
    // when a completed score or decision is waiting for connectivity.
    // Exposed as API.hasPendingTerminalWrite / window.API.hasPendingTerminalWrite.
    // -------------------------------------------------------------------------
    hasPendingTerminalWrite(compID, matchID) {
        // A finished result of the match (isFinishedResultWrite, R5): the
        // editors' held-result banner reads it, so a queued court move, time
        // change or lineup save of the match is not one.
        return _entriesFor(_revKey(compID, matchID)).some(([, d]) => isFinishedResultWrite(d));
    },
    // The way past a held write the server keeps refusing (a 5xx on every
    // retry, past the server-error notice threshold). It holds back no other
    // write, but it is retried for as long as the page is open, keeps the
    // pill on "Not saving" and is counted by the logout gate, so the operator
    // can discard it, and only it, from the match's editor after a confirm
    // (HeldWriteDiscard, admin_scoring_shared.jsx). Every other write held for
    // the match, and every other match's, is left alone.
    // Whether this device still holds ANY write for the match (a running
    // update, a result, a decision): the editors' pending banner stays while
    // one does and goes when none is left, landed or discarded
    // (useMatchHeldWrite, admin_scoring_shared.jsx).
    hasHeldWrite(compID, matchID) {
        // Only a write that already failed a pass (or came back from storage)
        // is held: a Finish on its first send is not "Not sent yet" (bc-evts).
        return _entriesFor(_revKey(compID, matchID)).some(([, d]) => d.held);
    },
    // bc-evts S3a: one scoring tap, as an event for the score-events door.
    // SYNCHRONOUS: the tap is in the persisted outbox when this returns, and
    // the send is kicked a microtask later, so taps made in one tick go in one
    // batch. `ev` is a score_events.jsx makeEvent() shape (kind, pos, side,
    // letter, target, value, memberId, name); `match` is the match as shown
    // (its modifiedAt floors the stamp, bc-hlck). Never coalesced: each tap is
    // its own entry (_coalesceTarget names only score and lineup writes).
    recordTap(compID, matchID, ev, password, match) {
        _noteShownMatch(compID, match);
        const base = _revKey(compID, matchID);
        const seq = _nextTapSeq(base);
        const id = `${_eventSession}.${seq}`;
        const seen = Number(match && match.modifiedAt) || 0;
        const stamp = _afterOwnStamp(base, _stampAfterSeen(_serverNowMs(), seen));
        const payload = { ...ev, id, session: _eventSession, seq, stamp };
        const descriptor = {
            compID, matchID, payload, password: password || '', kind: 'evt', terminal: false,
            method: 'POST', url: `/api/competitions/${compID}/matches/${matchID}/score-events`,
            enqueuedAt: Date.now(),
            ...(seen > 0 ? { seenModifiedAt: seen } : {}),
        };
        // Through the one enqueue (_commitEnqueue), the flush kicked in a
        // microtask so the taps of one tick go in one batch.
        _commitEnqueue(base, descriptor, null, { nextTick: true });
        const answer = { id, stamp, seq };
        if (!store.lastEnqueuePersisted) answer.persisted = false;
        return answer;
    },
    // The taps this device still holds for a match, in send order (event
    // payloads), for an editor's board and the same-device TV overlay.
    pendingTaps(compID, matchID) {
        return _pendingTapsFor(_revKey(compID, matchID));
    },
    subscribePendingTaps(compID, matchID, fn) {
        return _listenOn(_pendingTapListeners, _revKey(compID, matchID), fn);
    },
    // The newest derived match a score-events answer handed back (broadcast
    // copy with markIds and scoreVersion; kept by the larger scoreVersion).
    latestDerivedMatch(compID, matchID) {
        return _latestDerived.get(_revKey(compID, matchID)) || null;
    },
    subscribeDerivedMatch(compID, matchID, fn) {
        return _listenOn(_derivedListeners, _revKey(compID, matchID), fn);
    },
    // A tap the server did not apply: fn({ id, outcome, reason, message }).
    subscribeTapOutcome(compID, matchID, fn) {
        return _listenOn(_tapOutcomeListeners, _revKey(compID, matchID), fn);
    },
    // tellPendingTapRefused: the board found a pending tap of this device the
    // server will not apply (a tap on a bout the match's copy shows removed,
    // applyPending's BOUT_REMOVED): told once, now, to the match's editor and
    // as one 'not_applied' alert, and not again when the server's answer for
    // that tap arrives (stage 6 review 7). Never waits: told after the render.
    tellPendingTapRefused(compID, matchID, outcome, tap) {
        if (!compID || !matchID || !outcome || !outcome.id || !_claimToldHere(outcome.id)) return;
        // This tab's editor hears it; the operator's alert is raised once per
        // device (the first board to tell it), one alert for the taps told
        // together (F4).
        const first = _told.tell(compID, matchID, outcome.id);
        _queueBoardTold(compID, matchID, outcome, tap, first);
    },
    // noteShownMatch: the match an editor shows, so a held write of it, or of
    // one of its teams, is named by where it is (H5, _whereOf).
    noteShownMatch(compID, match) {
        _noteShownMatch(compID, match);
    },
    heldWriteKeepsFailing(compID, matchID) {
        return heldWriteState(compID, matchID).keepsFailing;
    },
    heldWriteState,
    heldWriteBlocker,
    discardFailingHeldWrites(compID, matchID) {
        let discarded = 0;
        // What holds the match back from another base (G2), when its own
        // writes are not the ones failing; asked before anything goes.
        const blocker = heldWriteBlocker(compID, matchID);
        for (const [key, d] of _entriesFor(_revKey(compID, matchID))) {
            if (_discardIfFailing(key, d)) discarded++;
        }
        if (blocker && _discardIfFailing(blocker.key, _writeQueue.get(blocker.key))) discarded++;
        if (discarded > 0) _afterDiscard();
        return discarded;
    },
    // Every write held on this device, in the order it replays, for the admin
    // topbar's held-writes list (HeldWritesPanel, held_writes_panel.jsx). It is where
    // a held write with no editor to discard it from is reached: a running
    // autosave or a lineup save the server keeps refusing.
    // Each item names what it is (kind, terminal; for a lineup its team and
    // round), whether it keeps failing, and the key discardHeldWrite takes. A
    // write still sending, or only waiting for the connection, is listed
    // without a way to discard it: it will land on its own.
    heldWrites() {
        const all = [..._writeQueue.entries()].filter(([, d]) => !!d).sort(_byEntryOrder);
        return all.map(([key, d]) => _heldWriteItem(key, d));
    },
    // The version banner's Reload stores the outbox once more before the
    // page goes (S2); every entry is stored as it is made anyway.
    persistQueue() {
        return _persistQueue();
    },
    // Discards ONE held write, by the key heldWrites gave, and only while the
    // server keeps refusing it (a write waiting for the connection is never
    // discarded through here). Returns whether it was.
    discardHeldWrite(key) {
        const ok = _discardIfFailing(key, _writeQueue.get(key));
        if (ok) _afterDiscard();
        return ok;
    },
    // mp-gpra (security): clearQueue: drop all queued writes (in-memory + the
    // persisted queue, an older build's key included) and reset sync state. Called on logout /
    // password_reset so a stale plaintext password and any pending writes don't
    // linger in localStorage past credential revocation: bringing the stored queue
    // to the same lifecycle as bc_password. Pending offline writes are discarded:
    // on a password_reset they would 401 on retry anyway, and logout is explicit.
    /**
     * bc-qttl: how much unconfirmed operator work the queue is still holding.
     * Read by the logout gate, which must not let a sign-out silently destroy
     * results that never reached the server.
     * @returns {{total: number, terminal: number, authBlocked: number}}
     */
    unsentWrites() {
        return _unsentCounts();
    },
    // R4: the court moves and time changes this device makes, told in the tick
    // they are made and again when a refusal puts the old value back.
    // The status move this device made to a match that the court console
    // shows over the server's copy (_ownStatusMove, M4), and a subscription
    // told whenever one changes (queued, answered, refused or discarded).
    ownStatusMove(compID, matchID) {
        return _ownStatusMove(compID, matchID);
    },
    subscribeOwnStatusMoves(fn) {
        return _subscribeOwnStatusMoves(fn);
    },
    // The feed shows the move, or a later change: the answered record goes.
    forgetOwnStatusMove(compID, matchID) {
        _forgetOwnStatusMove(compID, matchID);
    },
    // The feed no longer holds the match (_forgetOwnStatusMovesNotIn).
    forgetOwnStatusMovesNotIn(matches) {
        _forgetOwnStatusMovesNotIn(matches);
    },
    subscribeLocalMatchPatch(cb) {
        return subscribeLocalMatchPatch(cb);
    },
    // bc-obox C1: a check-in this device makes, told in the tick it is made,
    // and again when a refusal or a discard puts back what the row had or a
    // superseded answer puts the participant as stored: { type:
    // 'checkin_updated', local: true, data: { competitionId, participants:
    // [{ id, checkedIn }] } }.
    subscribeLocalParticipantPatch(cb) {
        return subscribeLocalParticipantPatch(cb);
    },
    // Whether a court move of this match is still queued here: a feed that
    // shows the match on that court (the overlay above) is not yet the server's.
    courtMoveQueued(compID, matchID) {
        return 'court' in _pendingMatchFields(compID, matchID);
    },
    // Whether a move that stops this running match is still queued here (one
    // already out included): the server stops it with that move, so a later
    // move finds it waiting, and the copy shown still says running.
    stoppingCourtMoveQueued(compID, matchID) {
        return _entriesFor(_revKey(compID, matchID))
            .some(([, d]) => d.kind === 'court' && d.payload && d.payload.stopsRunning === true);
    },
    /**
     * bc-qttl: un-park writes held on a 401 and retry them with a freshly
     * authenticated credential, and give that credential to every other
     * queued write still carrying the password the server refused.
     *
     * Necessary because each descriptor captures the password it was enqueued
     * with, so a parked entry retried as-is can only reproduce the same 401
     * forever. Re-stamping is the whole point: it is what turns "parked" into a
     * state the operator can actually clear, and therefore what makes parking a
     * better answer than dropping the write.
     *
     * The rejection counter is reset too: those failures were against the old
     * credential and say nothing about whether the new one will be refused.
     * @param {string} password the newly authenticated password
     * @returns {number} how many parked writes were resumed
     */
    resumeAfterAuth(password) {
        // The passwords the server refused. A write queued after the parked
        // one, while it was offline say, carries the same stale password
        // without having been answered yet: it gets the new one too, or it
        // would meet the same 401 and ask for a second sign-in. An EMPTY
        // password is never treated as stale on an unparked entry: a self-run
        // participant's write carries none and must never be sent with the
        // organiser's (a parked entry has its password blanked and resumes on
        // its own flag anyway).
        const stale = new Set();
        for (const d of _writeQueue.values()) {
            if (d && d.authBlocked && d.password) stale.add(d.password);
        }
        let resumed = 0;
        for (const d of _writeQueue.values()) {
            if (!d) continue;
            const parked = !!d.authBlocked;
            if (!parked && !(password && d.password && stale.has(d.password))) continue;
            d.authBlocked = false;
            if (password) d.password = password;
            d.attempts = 0;
            if (parked) resumed++;
        }
        // The members reads go again, with the new password (_resumeMintedReads).
        _resumeMintedReads(password);
        if (resumed > 0) {
            _persistQueue();
            _resetFlushBackoff();
            _recomputeSyncStatus();
            _flushQueue();
        }
        return resumed;
    },
    /**
     * bc-qttl: the credential was revoked out from under this tablet (a
     * password_reset broadcast by another admin). Scrub the dead password from
     * every queued write and PARK them, instead of destroying results nobody
     * here chose to discard.
     *
     * This keeps both properties that matter. The security intent of calling
     * clearQueue() on this path was that a stale plaintext password must not
     * linger in localStorage past revocation, and emptying the password field
     * achieves exactly that: the password was the secret, the operator's match
     * results never were. The writes stay queued and unsendable (authBlocked)
     * until resumeAfterAuth() stamps the new credential, which is precisely the
     * state a 401 would have put them in anyway.
     *
     * The interactive logout path still uses clearQueue(), because there the
     * operator is asked first and explicitly chooses to discard.
     * @returns {number} how many writes were parked
     */
    parkQueueForReauth() {
        // Bump the generation FIRST so an in-flight _flushQueue loop cannot send
        // the rest of its snapshot with the now-revoked password.
        _bumpQueueGen();
        let parked = 0;
        for (const d of _writeQueue.values()) {
            if (!d) continue;
            d.password = '';
            d.authBlocked = true;
            parked++;
        }
        _resetFlushBackoff();
        _pauseMintedReads();
        _setOffline(false);
        // Every stored entry too, whether or not this tab holds any.
        _parkStoredEntries();
        // The landed-tap record and the copies seen stay: the same operator
        // signs in again, and is told of a tap turned away meanwhile (stage 6
        // review 8, m10). The board's notices stay for the taps still queued
        // here, to be answered after the sign-in; the rest go (m12).
        _pruneToldOnBoard();
        if (parked > 0) {
            _persistQueue();
            _notifyQueueAlert({ kind: 'auth_required', count: parked, terminalCount: _countTerminalQueued() });
        }
        _recomputeSyncStatus();
        return parked;
    },
    clearQueue() {
        // bc-qttl: announce what is being destroyed. This is a credential
        // revocation, so these writes genuinely cannot be delivered (the password
        // they carry is dead), but "cannot be delivered" is never a licence to
        // discard an operator's results in silence. The interactive logout path
        // asks BEFORE reaching here; this covers the involuntary route, a
        // password_reset broadcast from another admin, where nobody at this
        // tablet chose anything.
        const discarding = _countTerminalQueued();
        const discardingTotal = _writeQueue.size;
        _forgetTurnedAwayAndTold();
        _clearOwnStatusAnswers();
        if (discardingTotal > 0) {
            _notifyQueueAlert({ kind: 'discarded', count: discardingTotal, terminalCount: discarding });
        }
        // Bump the generation FIRST so an in-flight _flushQueue loop (mid-await on
        // a network write) aborts instead of sending the rest of its snapshot with
        // the now-revoked password.
        _bumpQueueGen();
        _wipeQueue();
        for (const k of _entryHooks.keys()) _settleKey(k, { value: { discarded: true } });
        for (const b of _pendingTapListeners.keys()) _publishPendingTaps(b);
        try { localStorage.removeItem(QUEUE_STORAGE_KEY); } catch (_e) { /* ignore */ }
        // An older build's queue carries the same revoked password (F4).
        try { localStorage.removeItem(LEGACY_QUEUE_STORAGE_KEY); } catch (_e) { /* ignore */ }
        _wipeMintedRecords();
        // The copies score-events answers handed back go with the session
        // they were answered to (X4).
        _forgetDerivedCopies();
        // The landed-tap record goes with the operator: this tab's taps and
        // those of tabs no longer alive (stage 6 reviews 7 and 8).
        _forgetLandedTaps();
        _resetFlushBackoff();
        _setOffline(false);
        _recomputeSyncStatus();
    },
};
