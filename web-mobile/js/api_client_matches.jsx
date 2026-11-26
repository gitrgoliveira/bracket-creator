// The match API members: score and decision writes, the start check, court
// and time moves, send back to the queue, reopen, the representative bout,
// kachinuki bout removal and a match's history.
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

import { toBackendMatchResult } from './api_serializers.jsx';
import { isOffline, writeCourtNotHeld } from './write_result.jsx';
import {
    fetchWithTimeout, reopenBody, START_CHECK_TIMEOUT_MS, _serverKeepsRetrying, START_CHECK_NOT_FOUND_MESSAGE,
    START_CHECK_WRITE_KINDS, _reasonHumanError, _fetchJson,
} from './api_client_requests.jsx';
import { _serverNowMs, _seenOfScore, _stampPayload, _nextRev, _revSession } from './api_client_clock.jsx';
import { _revKey } from './api_client_queue.jsx';
import {
    START_BROADCAST_DROPS, _publishMatchPatch, _sideIdFields, _heldFlagCounts, _attachSendBackDisplayPutBack,
    _emitLocalMatchPatch,
} from './api_client_overlays.jsx';
import { _noteShownMatch, _noteMatchLabel } from './api_client_status.jsx';
import { _outboxWrite, _lastEntryKey, _daihyosenRequest } from './api_client_enqueue.jsx';
import { queuedWritesOf } from './api_client_outbox.jsx';
import { store } from './api_client_store.jsx';

export const matchesApi = {
    /**
     * Record a match score or status change.
     *
     * The patch passed as `result` may carry request-only `forceCourt: true`:
     * an organiser's confirmed start on a busy shiaijo; sent on the payload,
     * kept through the outbox, merged into a write that takes a queued one's
     * place, never broadcast to a display.
     *
     * @param {string} compID
     * @param {string} matchID
     * @param {object} result
     * @param {string} password
     * @param {object} [match]
     * @param {object} [opts]
     */
    async recordScore(compID, matchID, result, password, match, opts = {}) {
        _noteShownMatch(compID, match);
        const payload = toBackendMatchResult(result, match || result);
        if (result && result.forceCourt) {
            payload.forceCourt = true;
        }
        // Request-only like forceCourt: the organiser's "Change it anyway" on
        // a finished league's correction (write_result.jsx attemptScoreWrite).
        if (result && result.confirmFinished) {
            payload.confirmFinished = true;
        }
        // Request-only too: the organiser's "Replace the lots" on a result for
        // the drawn tie-breaker their lots settled.
        if (result && result.confirmLots) {
            payload.confirmLots = true;
        }
        // mp-y3nk: stamp in server-relative time for last-write-wins reconciliation.
        // Stamped when it is made (a running match's scoring is taps, each
        // stamped at its own tap, so no score write carries an older edit).
        // Never older than the match it was made against (bc-hlck), nor than
        // this device's last write of the match (_outboxWrite, _afterOwnStamp),
        // so the outbox's order and the stamps agree.
        const seen = _seenOfScore(result, match);
        _stampPayload(payload, _serverNowMs(), seen);

        // C2: stamp monotonic rev on running-status writes so the server's
        // rev-guard can hold out-of-order deliveries (e.g. from reconnect flush).
        // Completed writes do not need a rev: the guard is gated on status=running.
        const isRunning = payload?.status === 'running';
        if (isRunning) {
            payload.rev = _nextRev(compID, matchID);
            payload.revSession = _revSession;
        }

        // mp-9ukk Phase 2: broadcast helper. Publishes a match patch on the
        // court-local BroadcastChannel so the display tab can update without
        // a server round-trip during offline periods.
        //
        // The envelope mirrors the SSE match_updated shape including sideAId and
        // sideBId so the display tab's normalizeMatch can resolve participants by
        // UUID rather than falling back to name-key lookup (which can mis-resolve
        // same-name participants). winnerId is already in rest when present.
        // rev/revSession, and the groups the write names (`changed`, bc-mrgc),
        // are stripped so internal write-ordering metadata is not propagated
        // to the display tab.
        //
        // bc-evts stage 4 review 2 (G1): a start only starts. The server keeps
        // the score a sent-back match holds (engine.keepQueuedScore), so a
        // start-only write publishes no scoreline field at all (a key present
        // as undefined would still wipe the display's copy in the merge), and
        // states the flag counts the match it starts already holds: the
        // display reads a result naming a status that omits a count as 0
        // (mergeMatchPatch), which turned a kept 2-1 into 0-0. `startOnly` and
        // `forceCourt` (and `confirmFinished`) are request-only and stay off the display's copy.
        const _broadcastPatch = (fields) => {
            const {
                rev: _r, revSession: _rs, changed: _ch,
                writeId: _w, basis: _b, session: _s, forceDownstreamReopen: _f,
                startOnly, forceCourt: _fc, confirmFinished: _cf, confirmLots: _cl, ...rest
            } = fields || {};
            if (startOnly) {
                for (const k of START_BROADCAST_DROPS) delete rest[k];
                Object.assign(rest, _heldFlagCounts(match));
            }
            // Do not emit an unscoped (court-less) broadcast: a display can only
            // safely apply a patch it can attribute to its court, and the display
            // guard drops court-less messages anyway. In production every
            // recordScore caller passes the match (so court is set); the
            // court-less case is test-only (match=null), where no broadcast is
            // expected.
            _publishMatchPatch(compID, match, {
                id: matchID,
                ..._sideIdFields(match),
                ...rest,
            });
        };

        // bc-evts (the governing rule): EVERY score write is the outbox's, at
        // once, a running one (an autosave, Start match, Record bout) included:
        // stored in this tick on the match's base and sent strictly after what
        // the match already holds. A running write takes the place of the
        // match's last entry only when that is a running score write not yet
        // out (_coalesceTarget), claiming its groups and carrying a pending
        // kachinukiBoutFinal (_placeWrite). The caller carries on with the
        // pending answer; `settled` is the server's answer when it comes, and a
        // refusal nobody claims (opts.onRefusal) is told like a replay's.
        // `result.after` (request-only, like seenModifiedAt: toBackendMatchResult
        // copies named fields only) names another match's queued entry this
        // write must follow. The display overlay is the optimistic result.
        const answer = _outboxWrite({
            kind: 'score', compID, matchID, method: 'PUT',
            url: `/api/competitions/${compID}/matches/${matchID}/score`,
            payload, password, terminal: !isRunning, after: result && result.after,
            // `result.seenScoreVersion` (request-only, never sent): the server
            // copy the individual editor's board showed, the score-events
            // answer's when the board took it (m1); else the host's match.
            seen: result && result.seenScoreVersion !== undefined ? result.seenScoreVersion : match && match.scoreVersion,
            onRefusal: opts && opts.onRefusal,
        });
        // A write the court console's gate refused (court_control.jsx) was
        // never queued, so no display is shown it (bc-sseq FR9).
        if (!writeCourtNotHeld(answer)) _broadcastPatch(payload);
        return answer;
    },
    // T093–T095: kiken / fusenpai / fusensho / daihyosen: server auto-fills
    // scoreline and Winner from {decision, decisionBy, encho}. Body shape is
    // defined by mobileapp.DecisionRequest in handlers_decision.go; decisionBy
    // MUST be "shiro" or "aka", decisionReason is optional and ≤200 chars.
    // Response is the updated state.MatchResult.
    //
    // F1: bounded (12 s abort on stalled wifi), the body read included.
    // F5: on network failure / abort / 5xx / 429 the decision is enqueued as a
    // terminal write for durable re-delivery. 4xx (including a 409
    // on the DIRECT call) always throw: the score editor relies on a thrown 409
    // to show the refusal. A queued replay's 409 is reported too
    // (_flushQueue), never taken as success (bc-mrgc phase 3).
    async recordDecision(compID, matchID, body, password, opts = {}) {
        const decisionUrl = `/api/competitions/${compID}/matches/${matchID}/decision`;
        // Stamp in server-relative time, exactly as recordScore does (mp-y3nk),
        // when the operator acted, so a queued replay carries that time (a
        // match closed while still `scheduled` has no earlier write to inherit
        // one from, mp-jnvl). `seenModifiedAt` in the body is the match the
        // decision was recorded on (bc-hlck): it floors the stamp and is never
        // sent.
        const { seenModifiedAt: seen, ...rest } = body || {};
        const payload = _stampPayload({ ...rest }, _serverNowMs(), seen);
        // bc-evts: the outbox's, at once, behind this device's earlier writes
        // for the match. `opts.seen` is the score log line count the screen
        // showed (the match's scoreVersion): a correction over a finished match
        // carries the basis that says it saw the result (I1).
        return _outboxWrite({
            kind: 'decision', compID, matchID, method: 'POST', url: decisionUrl, payload, password,
            seen: opts && opts.seen, onRefusal: opts && opts.onRefusal,
        });
    },
    async updateMatchTime(compID, matchID, scheduledAt, password, opts = {}) {
        // bc-evts M5: an entry of the match, in its order, answered at once;
        // a refusal comes through `settled` or `opts.onRefusal`.
        // R4: shown at once, and `opts.was` (the time it had) put back on a refusal.
        const answer = _outboxWrite({
            kind: 'time', compID, matchID, method: 'PUT',
            url: `/api/competitions/${compID}/matches/${matchID}/time`,
            payload: { scheduledAt }, password, onRefusal: opts && opts.onRefusal, was: opts && opts.was,
        });
        if (!writeCourtNotHeld(answer)) _emitLocalMatchPatch(compID, matchID, { scheduledAt });
        return answer;
    },
    async moveMatchCourt(compID, matchID, court, password, opts = {}) {
        // bc-evts M5: an entry of the match, in its order, answered at once;
        // a refusal comes through `settled` or `opts.onRefusal`.
        // R4: shown at once (_emitLocalMatchPatch), and `opts.was` (the
        // court it had) is put back on a refusal.
        const payload = opts && opts.stopsRunning ? { court, stopsRunning: true } : { court };
        const answer = _outboxWrite({
            kind: 'court', compID, matchID, method: 'PUT',
            url: `/api/competitions/${compID}/matches/${matchID}/court`,
            payload, password, onRefusal: opts && opts.onRefusal, was: opts && opts.was,
        });
        if (writeCourtNotHeld(answer)) return answer;
        _emitLocalMatchPatch(compID, matchID, { court });
        if (opts && opts.stopsRunning && opts.match) {
            const oldCourt = (opts.was && opts.was.court) || opts.match.court || '';
            const oldMatch = { ...opts.match, court: oldCourt };
            _attachSendBackDisplayPutBack(compID, matchID, oldMatch, answer, court);
        }
        return answer;
    },
    async revertMatchToQueue(compID, matchID, password, opts = {}) {
        // bc-evts: the outbox's, with a JSON body carrying writeId, basis and
        // session; `settled` resolves true, or rejects with the refusal.
        const answer = _outboxWrite({
            kind: 'revert', compID, matchID, method: 'POST',
            url: `/api/competitions/${compID}/matches/${matchID}/revert-to-queue`,
            payload: {}, password, seen: opts && opts.seen, onRefusal: opts && opts.onRefusal,
        });
        // bc-stpf: `match`, the bout as the caller shows it (its court, sides
        // and flag counts), lets a display on this machine show the send back
        // at once, and the bout running again when it does not land.
        const match = opts && opts.match;
        if (match && !writeCourtNotHeld(answer)) {
            _attachSendBackDisplayPutBack(compID, matchID, match, answer);
        }
        return answer;
    },
    // bc-stpf (operator ruling 2026-10-12, option A): would starting `matchID`
    // be accepted once the bout running on `court` is sent back? A direct read
    // (GET .../start-check), never the outbox: it writes nothing and must never
    // be replayed. It waits at most START_CHECK_TIMEOUT_MS, the body read
    // included, and never throws. Answers {ok: true}; {refused: Error} for a
    // definite answer that the start would fail: a 409 (the Error carrying the
    // server's sentence as the start's own refusal does, _reasonHumanError), a
    // 401 (auth failure), a 404 (the server no longer holds the match or its
    // competition, so the start would 404 too) or another 4xx; or
    // {unknown: true} when there is no such answer (no answer in time, a
    // network failure, or an answer the outbox would retry on, _serverKeepsRetrying), on
    // which the court console asks the operator.
    async checkStart(compID, matchID, court, password) {
        const url = `/api/competitions/${encodeURIComponent(compID)}/matches/${encodeURIComponent(matchID)}/start-check?court=${encodeURIComponent(court || '')}`;
        try {
            const { res, body } = await _fetchJson(url, {
                method: 'GET',
                headers: password ? { 'X-Tournament-Password': password } : {},
            }, START_CHECK_TIMEOUT_MS);
            if (res.ok && body && body.ok === true) return { ok: true };
            if (res.status === 409) return { refused: _reasonHumanError(body || {}, 'Could not start the match') };
            if (res.status === 401) return { refused: new Error('The password was not accepted. Sign in again, then start the match.') };
            if (res.status === 404) return { refused: new Error(START_CHECK_NOT_FOUND_MESSAGE) };
            if (res.status >= 400 && !_serverKeepsRetrying(res.status)) {
                return { refused: _reasonHumanError(body || {}, 'Could not start the match') };
            }
            return { unknown: true };
        } catch (_e) {
            return { unknown: true };
        }
    },
    // bc-stpf: true when a start check's answer could not be trusted, so the
    // court console asks the operator without asking the server: the device
    // is offline (the browser says so, or the outbox's last pass failed on the
    // network), or the outbox still holds a write of `compID` the server has
    // not seen that moves who is fighting, where, or who is barred
    // (START_CHECK_WRITE_KINDS: a finish, a decision, a start, a send back, a
    // court move, a reinstatement), whose effect the check would miss. A tap is
    // not counted: it moves points, never a status or a bar, and taps leave in
    // a microtask. Nor is a lineup, a team member or a time write: none of
    // them changes a match's status, its court, its sides or a bar, so one held
    // or failing for a while does not make every pick ask the operator.
    startCheckUntrusted(compID) {
        if (isOffline()) return true;
        if (store.offlineFlag) return true;
        return queuedWritesOf(compID, START_CHECK_WRITE_KINDS) > 0;
    },
    // mp-gmcg: reopen a COMPLETED kachinuki team match, or (bc-tmfn) any
    // completed match a withdrawal decided: status back to running,
    // winner/decision cleared, fought bouts kept.
    //
    // The kachinuki Reopen sends NO REASON, deliberately (operator ruling). An
    // operator who ended a match BY MISTAKE at a shiaijo must be able to get
    // back into it in ONE TAP; making them justify the mistake before they may
    // undo it is friction at the worst possible moment, and ending the match
    // again asks for no reason either (operator ruling 2026-09-25: a match can
    // be reopened without any reason, and nothing is gated on that). Clear
    // withdrawal and reopen is one tap with no reason too; opts.reason stays
    // for any caller that has one to record. opts.force is the operator's confirmation after a
    // downstream_knockout_played refusal. opts.forceCourt is sent in the body beside reason
    // for an organiser's confirmed reopen on a busy shiaijo. The body stays a JSON object (never
    // absent) so a handler that binds JSON still parses the request.
    //
    // 400 = neither kachinuki nor withdrawal-decided; 409 = not completed / a
    // later match in progress or resolved by a bye / a later match with its
    // own result (downstream_knockout_played, parsed like the score path's,
    // so the caller confirms and retries with force) / another match is
    // already running on this match's court. The editor shows whichever came
    // back verbatim, so the unwrap keeps the server's own words. Resolves to
    // the server's { reopenedMatches: [{id, number, label}] }.
    //
    // TWO 409 SHAPES: most carry the sentence in `error`, but the court-busy
    // conflict reuses the score path's structured payload, where `error` is
    // the machine code "court_busy", the sentence is in `message`, and
    // court/matchId/compId identify the BLOCKING match. Prefer `message` so the
    // operator never reads a bare code, and carry the blocking match's identity
    // onto the thrown Error: without it the editor could only print the
    // conflict, and a busy court would be a dead end (kachinuki reopen is the
    // ONLY way to fix a bout log). The server broadcasts match_updated on
    // success.
    /**
     * Reopen a completed match.
     *
     * @param {string} compID
     * @param {string} matchID
     * @param {string} password
     * @param {object} [opts]
     * @param {string} [opts.reason=""]
     * @param {boolean} [opts.force=false]
     * @param {boolean} [opts.forceCourt=false] - sent in the body beside reason for an organiser's confirmed reopen on a busy shiaijo
     * @param {*} [opts.seen]
     * @param {Function} [opts.onRefusal]
     */
    async reopenMatch(compID, matchID, password, { reason = "", force = false, forceCourt = false, confirmFinished = false, confirmLots = false, seen, onRefusal } = {}) {
        // bc-evts: the outbox's; `settled` resolves the server body or rejects
        // with reopenFailureError's error (court_busy, downstream, ...).
        return _outboxWrite({
            kind: 'reopen', compID, matchID, method: 'POST',
            url: `/api/competitions/${compID}/matches/${matchID}/reopen`,
            payload: reopenBody(reason, force, forceCourt, confirmFinished, confirmLots), password, seen, onRefusal,
        });
    },
    // mp-gmcg (review A4): atomically requeue the match holding the court AND
    // reopen the target, in ONE server call under one court lock — replaces the
    // former two-call revert-then-reopen, which raced (a peer could take the
    // freed court between the calls). Surfaces the SAME court_busy detail shape
    // as reopenMatch, so the panel re-offers the remedy when a DIFFERENT match
    // has since taken the court. opts as reopenMatch's: the remedy carries the
    // same reason and confirmation the reopen it retries did.
    async requeueBlockerAndReopen(targetComp, targetMatch, blockerComp, blockerMatch, password, { reason = "", force = false, confirmFinished = false, confirmLots = false, seen, onRefusal, match, blockerLabel } = {}) {
        // bc-evts: the outbox's, on the TARGET match's base (the one reopened),
        // and ordered under the blocker's too (M5): it follows the blocker's
        // last queued write (`after`), and a write of the blocker made after it
        // follows it (`holds`, _heldBy), so neither overtakes the other.
        // `match` (the target as shown) and `blockerLabel` (the blocker as the
        // court_busy answer names it) name the two if either is held (H5).
        _noteShownMatch(targetComp, match);
        _noteMatchLabel(blockerComp, blockerMatch, blockerLabel);
        const blockerBase = _revKey(blockerComp, blockerMatch);
        return _outboxWrite({
            kind: 'requeue', compID: targetComp, matchID: targetMatch, method: 'POST',
            url: `/api/competitions/${targetComp}/matches/${targetMatch}/requeue-blocker-and-reopen`,
            payload: { blockerCompId: blockerComp, blockerMatchId: blockerMatch, ...reopenBody(reason, force, false, confirmFinished, confirmLots) },
            password, seen, onRefusal,
            after: _lastEntryKey(blockerBase), holds: [blockerBase],
        });
    },
    // mp-gmcg: remove a trailing UNSCORED kachinuki bout appended by mistake
    // ([Record bout] / [Add next bout]). Kachinuki-only; targets a numbered
    // bout, never a daihyosen. Returns the updated MatchResult (envelope
    // unwrapped, like removeDaihyosen). A 409 ("no unscored bout to remove" /
    // "match is not running") surfaces as the thrown Error's message.
    async removeKachinukiBout(compID, matchID, password, opts = {}) {
        // bc-evts: the outbox's; `settled` resolves the stored match the
        // server answered with (body.result), or rejects with the refusal.
        return _outboxWrite({
            kind: 'kachinuki', compID, matchID, method: 'DELETE',
            url: `/api/competitions/${compID}/matches/${matchID}/kachinuki-bout`,
            payload: {}, password, seen: opts && opts.seen, onRefusal: opts && opts.onRefusal,
        });
    },
    // bc-tmid pass 3: a team's squad, the actual people on it, lives in its
    // own per-competition store (team-members.yaml), keyed by the team's
    // participant id -- see internal/state/squad.go. Returns the whole
    // map ({ teamId: [{id, index, name}, …] }) since the lineup editor
    // needs its own team's list, not one member at a time.
    // bc-mrgc: a match's write history, oldest change first: every write that
    // reached the match, with the values of a change kept rather than applied
    // because a newer change to the same thing was recorded first. An
    // organiser read (main-gated in self-run); the score editors' History
    // disclosure (match_history_view.jsx) is its one reader.
    async fetchMatchHistory(compID, matchID, password) {
        const res = await fetchWithTimeout(`/api/competitions/${encodeURIComponent(compID)}/matches/${encodeURIComponent(matchID)}/history`, {
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load the match history");
        }
        const data = await res.json();
        return Array.isArray(data) ? data : [];
    },
    // T141: daihyosen (representative bout) appended after a knockout team
    // match ties on IV and PW. Server validates the tie + eligibility and
    // returns the updated MatchResult with a new SubMatchResult whose
    // decision="daihyosen" and Position=-1. The error codes (not_tied,
    // pool_match, insufficient_eligibility) are surfaced verbatim by the
    // caller: see TeamScoreEditorModal for the user-visible mapping.
    // Stamped like recordDecision (bc-dhas), so the add competes on
    // timestamps: see _daihyosenOutcome for what comes back.
    // seenModifiedAt (optional): the match as the sheet showed it (bc-hlck).
    async recordDaihyosen(compID, matchID, password, seenModifiedAt, opts = {}) {
        return _daihyosenRequest('POST', compID, matchID, password, seenModifiedAt, opts);
    },
    // T141: remove an unscored daihyosen placeholder from a knockout team match.
    // Returns the updated MatchResult on 200. Throws on 404 (no daihyosen or
    // match not found) or 409 (daihyosen already scored: clear scores first).
    // Stamped like the add, in a JSON body (the handler binds one on DELETE
    // too).
    async removeDaihyosen(compID, matchID, password, seenModifiedAt, opts = {}) {
        return _daihyosenRequest('DELETE', compID, matchID, password, seenModifiedAt, opts);
    },
};
