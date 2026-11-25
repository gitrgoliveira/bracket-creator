// Requests: fetchWithTimeout, _fetchJson and _directFetch, the refusal builders
// and the read helpers (normalizeViewerCompItem, lineupOrNull, backup requests).
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import { normalizeCompetitionDetail, normalizePlayer } from './api_serializers.jsx';
import {
    writeWasSuperseded, downstreamKnockoutPlayedQueueDrop, downstreamKnockoutRunningMessage,
    downstreamKnockoutRunningReopenMessage, downstreamKnockoutRunningQueueDrop, courtBusyMessage,
    FETCH_TIMEOUT_MS, WRITE_NOT_CONFIRMED, noConnectionError,
} from './write_result.jsx';
// bc-evts S2: the build this page runs against the server's, and the reload rule.
import { noteServerBuild } from './app_version.jsx';
import { late } from './api_client_store.jsx';
import { revealKeysHeader } from './reveal_key.jsx';

// ---------------------------------------------------------------------------
// Who this page reads as (bc-hnam)
// ---------------------------------------------------------------------------
// A competition can hide its competitors' names from the public pages; the
// server judges every public read by the credential it carries (the
// organiser's password, a phone's reveal keys, or nothing) and masks the
// answer for whoever may not see names. Which credential a read carries is
// decided HERE, by the route the page is on, never by the caller:
//   - "admin", an organiser signed in on an admin route: the password, so the
//     court console's feed, the team editor's detail and every other admin
//     read gets the real names it saves from;
//   - "viewer", the public pages: the reveal keys this phone holds
//     (reveal_key.jsx), and never the password, so an organiser who opens the
//     public page on the same laptop sees what the public sees;
//   - "display", the court boards and the stream overlay: nothing at all,
//     whatever the device holds, since a board is watched by the hall.
// app.jsx sets it as the route changes (setReadAudience) and keys the
// competitions it holds by readAudienceKey, so data read as one audience is
// never shown to another. The event stream asks its ticket with the same
// headers (api_client_events.jsx).
const READ_KINDS = new Set(['admin', 'viewer', 'display']);
const _audience = { kind: 'viewer', password: '', everyName: false };
let _audienceSignature = '';

function _readKind(kind) {
    return READ_KINDS.has(kind) ? kind : 'viewer';
}

// readAudienceKey: which audience a page on `kind` (with `password` when the
// organiser is signed in) reads as, for keying fetched state: "organiser",
// "display", "reveal:<the keys held>" or "public". The password itself is not
// in the key: a changed password reads the same names. `everyName` is true in
// a tournament that shows every name (tournamentShowsEveryName, a self-run
// one), where every audience reads the same names: the key is then always
// "public", so nothing fetched is read again as the route changes.
export function readAudienceKey(kind, password, everyName = false) {
    if (everyName) return 'public';
    const k = _readKind(kind);
    if (k === 'admin') return password ? 'organiser' : 'public';
    if (k === 'display') return 'display';
    const keys = revealKeysHeader();
    return keys ? `reveal:${keys}` : 'public';
}

// setReadAudience: the credential every read sends from now on. True when it
// differs from the one set before (another route, password or set of reveal
// keys): the caller then reopens the event stream, whose ticket was asked with
// the old one. Never true in a tournament that shows every name (`everyName`,
// see readAudienceKey): its stream needs no ticket and carries every name to
// every audience, so it stays open as it always did. Leaving such a tournament
// (a restore) is a change.
export function setReadAudience(kind, password, everyName = false) {
    _audience.kind = _readKind(kind);
    _audience.password = _audience.kind === 'admin' ? (password || '') : '';
    _audience.everyName = !!everyName;
    const keys = _audience.kind === 'viewer' ? revealKeysHeader() : '';
    const signature = `${_audience.kind}\n${_audience.password}\n${keys}\n${_audience.everyName}`;
    const changed = signature !== _audienceSignature;
    _audienceSignature = signature;
    return changed && !_audience.everyName;
}

// currentReadAudienceKey: the audience the reads sent now go as
// (readAudienceKey of the credential last set). A read whose answer lands
// after it moved is dropped by its caller (app.jsx load).
export function currentReadAudienceKey() {
    return readAudienceKey(_audience.kind, _audience.password, _audience.everyName);
}

// readsEveryName: whether the tournament shows every name (setReadAudience's
// everyName), where the event stream needs no ticket for any audience.
export function readsEveryName() {
    return _audience.everyName;
}

// readsAsOrganiser: whether this page's reads carry the organiser's password.
export function readsAsOrganiser() {
    return _audience.kind === 'admin' && !!_audience.password;
}

// CREDENTIAL_REFUSED: the `code` of the error a read throws when the server
// refused the organiser's password (HTTP 401 on an admin read, which a stale
// saved password draws now that every read is judged by its audience).
export const CREDENTIAL_REFUSED = 'credential_refused';

// onCredentialRefused: the app's one listener for a refused organiser password
// (app.jsx registers dropStaleCredential). Returns the function that takes it
// off again, which only removes this listener, never a later one.
let _credentialRefusedListener = null;
export function onCredentialRefused(listener) {
    _credentialRefusedListener = listener;
    return () => {
        if (_credentialRefusedListener === listener) _credentialRefusedListener = null;
    };
}

// noteRefusedCredential: a read answered `status`. When that is 401 on a page
// that read with the organiser's password, the app is told (the page drops the
// stale credential and asks for the password again, app.jsx
// dropStaleCredential) and the answer is true; the caller then throws an error
// carrying CREDENTIAL_REFUSED, so nothing reads it as "no tournament".
export function noteRefusedCredential(status) {
    if (status !== 401 || !readsAsOrganiser()) return false;
    if (_credentialRefusedListener) _credentialRefusedListener();
    return true;
}

// readFailure: the error a failed read throws. It carries CREDENTIAL_REFUSED
// when the server refused the organiser's password (noteRefusedCredential).
export function readFailure(status, serverMessage, what) {
    const e = new Error(serverMessage || `Failed to fetch ${what} (Status ${status})`);
    if (noteRefusedCredential(status)) e.code = CREDENTIAL_REFUSED;
    return e;
}

// readHeaders: the credential headers of a read from this page, {} for none.
export function readHeaders() {
    if (_audience.kind === 'admin') {
        return _audience.password ? { 'X-Tournament-Password': _audience.password } : {};
    }
    if (_audience.kind === 'display') return {};
    const keys = revealKeysHeader();
    return keys ? { 'X-Reveal-Key': keys } : {};
}

// ---------------------------------------------------------------------------
// F1: fetch with per-request timeout via AbortController
// ---------------------------------------------------------------------------
// fetchWithTimeout wraps the native fetch() with an AbortController so
// any stalled request is aborted after `ms` milliseconds (default 12 s).
// An abort is treated as a NETWORK FAILURE: the AbortError propagates as a
// rejected promise so callers enter the same catch branch as a TCP reset.
// The clearTimeout in finally ensures the alarm never fires after the
// request has already settled, avoiding a stale abort on a new controller.
// That settling is the HEADERS arriving: the abort covers nothing after it,
// so a request whose body must be bounded as well goes through _fetchJson.
// The deadline itself is FETCH_TIMEOUT_MS (write_result.jsx).
// ---------------------------------------------------------------------------

/**
 * fetch() with an automatic abort timeout.
 * @param {string} url
 * @param {RequestInit} opts
 * @param {number} [ms=FETCH_TIMEOUT_MS]
 * @returns {Promise<Response>}
 */
export function fetchWithTimeout(url, opts, ms = FETCH_TIMEOUT_MS) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), ms);
    return fetch(url, { ...opts, signal: controller.signal })
        .then((res) => { _noteBuildOf(res); return res; })
        .finally(() => clearTimeout(timer));
}

// S2: every answer names the server's build (X-App-Build); a different one
// than this page's asks for the reload (app_version.jsx owns the rule).
function _noteBuildOf(res) {
    try {
        const b = res && res.headers && typeof res.headers.get === 'function' ? res.headers.get('X-App-Build') : null;
        if (b) noteServerBuild(b);
    } catch (_e) { /* a stubbed response: nothing to read */ }
}

// _refusalText is the words a refused request is thrown with: the refusal's
// sentence (`message`: a finished match, the judges' decision on a
// representative bout) when it carries one, else its code (`error`), else the
// caller's fallback. The public page shows a thrown message as it is, so a
// path that threw the bare code showed "result_finalized" where the server had
// said what happened. The one owner of that choice.
export function _refusalText(body, fallback) {
    return (body && (body.message || body.error)) || fallback;
}

// _refusalError: the Error a refused request is thrown with, in _refusalText's
// words. A refusal that carries a sentence carries its code beside it, and the
// Error keeps that code, so a caller can tell one refusal from another without
// comparing words (the team editor and the Lineups page word a member who
// already has a name alike through it, lineup_resolver.jsx memberRefusalNote).
export function _refusalError(body, fallback) {
    const e = new Error(_refusalText(body, fallback));
    if (body && body.message && body.error) e.code = body.error;
    return e;
}

// reopenFailureError builds the Error thrown by a failed reopen. reopenMatch and
// requeueBlockerAndReopen surface the SAME structured shape (mp-gmcg review R8):
// the operator-facing `message`, the machine `code`, and the court/matchId/compId
// of the BLOCKING match, which useMatchReopen (admin_scoring_shared.jsx) reads
// to re-offer the requeue remedy when a court is busy. One builder so a new
// server field is threaded once, not per endpoint. Async: it awaits the body.
// _reopenRefusal: the Error a refused reopen (or requeue-and-reopen) answers
// with, built from the refusal body; the outbox rejects a reopen's `settled`
// with it, exactly what the direct call used to throw.
export function _reopenRefusal(err) {
    err = err || {};
    // A later knockout match already has its own result: the same refusal a
    // knockout correction gets, parsed by the same helper, so the caller asks
    // downstreamKnockoutPlayedRefusal(e) and confirms before retrying with force.
    // `reopen` marks it as met by a reopen rather than a corrected result, for
    // the dialog's copy (write_result.jsx), the way overridePoolRanks marks
    // `ranking`.
    //
    // bc-rawm: routed through the shared _downstreamRefusalError rather than
    // _downstreamKnockoutPlayedError alone, so a kachinuki reopen blocked by a
    // DOWNSTREAM MATCH STILL RUNNING (not yet played) gets a copy for THIS
    // being a reopen (bc-cse: `{ reopen: true }` selects
    // downstreamKnockoutRunningReopenMessage, "...then reopen this match
    // again" rather than the score path's "...then save this correction
    // again", which has no save step to retry here), instead of falling
    // through to the bare
    // "downstream_knockout_running" token below. The PLAYED shape ALSO marks
    // `reopen` on its own structured field below, for the confirm dialog's
    // copy; the running shape carries no `.downstreamKnockoutPlayed` to mark,
    // its message alone already says what to do about a match still being
    // fought.
    const downstream = _downstreamRefusalError(err, { reopen: true });
    if (downstream) {
        if (downstream.downstreamKnockoutPlayed) downstream.downstreamKnockoutPlayed.reopen = true;
        return downstream;
    }
    // The match belongs to a finished league: met by a reopen, which the
    // confirm words as one (write_result.jsx's leagueFinishedConfirm).
    const league = _leagueFinishedError(err);
    if (league) {
        league.leagueFinished.reopen = true;
        return league;
    }
    // The bout is a drawn tie-breaker the organiser's lots settled.
    const lots = _lotsReplacedError(err);
    if (lots) return lots;
    const e = new Error(_refusalText(err, "Failed to reopen match"));
    if (err.error) e.code = err.error;
    if (err.court) e.court = err.court;
    if (err.matchId) e.matchId = err.matchId;
    if (err.compId) e.compId = err.compId;
    // bc-rawm: the court-busy conflict reuses the score path's structured
    // court_busy payload (see recordScore below), which now carries `label`
    // -- the blocking match named the way the operator sees it. Carried
    // through so ReopenFeedback (admin_scoring_shared.jsx) can prefer it over
    // its own best-effort fetched label, and never fall back to printing the
    // raw matchId.
    if (err.label) e.label = err.label;
    return e;
}

// reopenBody is the optional half of both reopen requests: the audit reason,
// the downstream confirmation, the court-busy override, the confirmation of
// a finished league and the one that replaces entered lots, each sent only when set, so the one-tap kachinuki Reopen
// still posts {} exactly as before.
export function reopenBody(reason, force, forceCourt, confirmFinished, confirmLots) {
    const body = {};
    if (reason) body.reason = reason;
    if (force) body.forceDownstreamReopen = true;
    if (forceCourt) body.forceCourt = true;
    if (confirmFinished) body.confirmFinished = true;
    if (confirmLots) body.confirmLots = true;
    return body;
}

// normalizeViewerCompItem maps one {config, poolMatches, bracket} item from the
// aggregate GET /api/viewer/competitions or the court-scoped GET
// /api/viewer/court/:court/matches into the flattened, normalized competition shape
// the UI consumes (config fields hoisted, players + matches normalized). Shared
// so both endpoints stay in lockstep.
//
// `drawHidden` (bc-drpb) is a field of the config, not a sibling, so the
// `...c` spread carries it: the server stamps it on its copy of the
// competition for every audience. Read it only through drawHiddenFromPublic
// (draw_visibility.jsx).
export function normalizeViewerCompItem(item) {
    const c = item.config || item;
    return normalizeCompetitionDetail({
        ...c,
        poolMatches: item.poolMatches,
        bracket: item.bracket,
        // Located file failures for this competition, a SIBLING of config on
        // the wire because they are about the files, not about the record
        // inside one of them. Hoisted like poolMatches so the console can
        // render the notice off the flattened competition it already has.
        dataIssues: item.dataIssues,
        // bc-hnam: the server's mark that this item was built for a reader who
        // may not see this competition's names, also a sibling of config.
        // normalizeCompetitionDetail stamps every record's nameHidden from it,
        // so dropped here the aggregate and court-feed reads stamp nothing.
        namesHidden: item.namesHidden,
        // Whether this competition's participants.csv actually LOADED, also a
        // sibling of config for the same reason. Hoisted like dataIssues or it
        // is silently dropped. Absent (an older payload) is read as loaded by
        // rosterFullyLoaded, so this never makes an existing client quieter
        // than it was.
        rosterAvailable: item.rosterAvailable,
        // The server's per-competition slot minutes (handlers_viewer.go
        // stampSlotMinutes), also siblings of config. court_pace.jsx reads
        // them off the flattened competition; dropped here, no pace projects.
        slotMinutes: item.slotMinutes,
        knockoutSlotMinutes: item.knockoutSlotMinutes,
        // bc-pnum: the members map is ALSO a sibling of config on the wire,
        // where it is called `teamMembers` (present only for a team
        // competition, keyed by the team's participant id --
        // handlers_viewer.go's buildViewerCompetitionPayload). This line is
        // the ONE place it is renamed to the internal `squads`, so the wire
        // name appears on the right of the colon and the internal one on the
        // left. It must be hoisted exactly like poolMatches/bracket/dataIssues
        // above or it is silently dropped here: `c` (item.config) never
        // carries it, and
        // normalizeCompetitionDetail's `{...data}` spread only preserves
        // whatever this function hands it. Without this, TvDisplay and
        // StreamingOverlay (which consume this normalized shape via
        // tournament.competitions, app.jsx's `t.competitions = comps`) could
        // never see the squads the aggregate/court-feed payload now carries.
        squads: late._withListedMembersForgotten(item.teamMembers, c.id),
        players: (c.players || []).map(normalizePlayer),
    });
}

// bc-kcdg: parses the 409 downstream_knockout_played refusal (correcting a
// completed knockout match whose later round has already been played) into a
// thrown Error carrying the structured fields as `.downstreamKnockoutPlayed`,
// so a catcher asks write_result.jsx's downstreamKnockoutPlayedRefusal(err)
// rather than re-deriving the shape from the raw body. Returns null when the
// body isn't this refusal, so callers can `throw _downstreamKnockoutPlayedError(body) || new Error(...)`.
function _downstreamKnockoutPlayedError(body) {
    if (!body || body.error !== 'downstream_knockout_played') return null;
    const err = new Error(_refusalText(body, 'Failed to record score'));
    err.downstreamKnockoutPlayed = {
        matchId: body.matchId,
        blockingMatchId: body.blockingMatchId,
        // Every match the confirmation will clear, each as {id, number,
        // label}: the LABEL is what the operator is shown ("Match 3
        // (Final)", write_result.jsx matchLabel), the id only addresses it. One entry, except when a semifinal fed both the final
        // and the bronze match.
        blockingMatches: body.blockingMatches && body.blockingMatches.length
            ? body.blockingMatches
            : (body.blockingMatchId ? [{ id: body.blockingMatchId }] : []),
        displaced: body.displaced,
        // A POOL correction in a mixed competition names the qualifying
        // places it moves ({pool, rank, place, from, to, tied}); empty for a
        // knockout correction, which moves no pool place.
        qualifierChange: body.qualifierChange || [],
    };
    return err;
}

// Parses the 409 downstream_knockout_running refusal: a pool correction that
// would move a qualifier out of a knockout match being fought now, or a
// knockout correction that would change a side of one. Unlike the
// played refusal it is NOT confirmable, so it carries no confirm fields; the
// thrown Error's message is the operator's copy (write_result.jsx's
// downstreamKnockoutRunningMessage) rather than the bare code, which is what
// every `new Error(data.error)` fallback below would otherwise show. Returns
// null when the body is not this refusal. bc-cse: `opts.reopen` swaps in
// downstreamKnockoutRunningReopenMessage instead -- the score path's "then
// save this correction again" is wrong for a REOPEN, which has no save step to retry.
function _downstreamKnockoutRunningError(body, opts) {
    if (!body || body.error !== 'downstream_knockout_running') return null;
    const message = (opts && opts.reopen)
        ? downstreamKnockoutRunningReopenMessage(body.runningMatches)
        : downstreamKnockoutRunningMessage(body.runningMatches);
    const err = new Error(message);
    err.code = body.error;
    err.downstreamKnockoutRunning = { matchId: body.matchId, runningMatches: body.runningMatches || [] };
    return err;
}

// _downstreamRefusalError is the one parse every score/decision write throws
// through for a knockout-consequence 409: the confirmable played refusal, or
// the terminal running one.
export function _downstreamRefusalError(body, opts) {
    return _downstreamKnockoutPlayedError(body) || _downstreamKnockoutRunningError(body, opts);
}

// _courtBusyError (bc-rawm): the 409 court_busy refusal on a score write --
// this match's shiaijo is not free, a DIFFERENT match already holds it. Built
// into an operator sentence via write_result.jsx's courtBusyMessage rather
// than echoing the server's own `message` field, so the sentence names the
// blocking match the way the operator sees it (the server's `label`) and
// never falls back to printing the internal id the body also carries.
// Deliberately separate from _downstreamRefusalError: this is not a
// downstream-knockout refusal, and reopenFailureError's own court_busy
// handling (the "TWO 409 SHAPES" branch below) keeps using the server's raw
// `message` for its own Error, unaffected by this helper.
export function _courtBusyError(body) {
    if (!body || body.error !== 'court_busy') return null;
    const err = new Error(courtBusyMessage({ court: body.court, label: body.label }));
    err.code = body.error;
    if (body.court) err.court = body.court;
    if (body.matchId) err.matchId = body.matchId;
    if (body.compId) err.compId = body.compId;
    if (body.label) err.label = body.label;
    return err;
}

// _leagueFinishedError: the 409 league_finished refusal of a correction on a
// finished league (the score and decision doors), thrown with the server's
// sentence and its fields as `.leagueFinished` ({removedCourts}), which
// write_result.jsx's leagueFinishedRefusal reads for the confirm. A queued
// replay refused with it is told the server's sentence (_replayRefusal).
// null for any other body.
export function _leagueFinishedError(body) {
    if (!body || body.error !== 'league_finished') return null;
    const err = new Error(_refusalText(body, 'Failed to record result'));
    err.code = body.error;
    err.leagueFinished = { removedCourts: body.removedCourts || [] };
    return err;
}

// _lotsReplacedError: the 409 lots_replaced refusal of a result, a decision or a
// reopen over lots the organiser entered for a drawn tie-breaker, thrown with the
// server's sentence (the place and the order entered) and `.lotsReplaced`
// ({matchId}), which write_result.jsx's lotsReplacedRefusal reads for the
// confirm. null for any other body.
export function _lotsReplacedError(body) {
    if (!body || body.error !== 'lots_replaced') return null;
    const err = new Error(_refusalText(body, 'Failed to record result'));
    err.code = body.error;
    err.lotsReplaced = { matchId: body.matchId || '' };
    return err;
}

// _downstreamQueueDropCopy is what a DROPPED queued write is reported with when
// the server refused its replay for a knockout reason: the played refusal's
// queue copy (it cannot be confirmed from the flush loop, nobody is there), or
// the running refusal's. Both are write_result.jsx's words. bc-cse: also a
// court_busy refusal (_courtBusyError, whose message is courtBusyMessage's),
// so a queued write dropped for a busy shiaijo alerts the operator sentence
// rather than the bare "court_busy" token both drop sites below used to fall
// through to. null for any other body.
function _downstreamQueueDropCopy(body) {
    const played = _downstreamKnockoutPlayedError(body);
    if (played) return downstreamKnockoutPlayedQueueDrop(played.downstreamKnockoutPlayed);
    const running = _downstreamKnockoutRunningError(body);
    if (running) return downstreamKnockoutRunningQueueDrop(running.downstreamKnockoutRunning.runningMatches);
    const courtBusy = _courtBusyError(body);
    // A whole sentence that says what to do, so it is marked as one
    // (_replayRefusal): no full stop or advice is added after it.
    if (courtBusy) return { reason: courtBusy.message, sentence: true };
    return null;
}

// _replayRefusal: what a queued write the server refused on replay is reported
// with, in the shape the terminal failure and the queue alert carry: its
// `reason`, the drop copy's own `advice` where there is one, and `sentence`
// when the reason is the server's own sentence (`reasonHuman`, else
// `message`; the court_busy drop copy is marked the same way). A banner and
// the alert show such a sentence as it is
// (write_result.jsx notSavedText, app.jsx queueAlertMessage): it says what to
// do, so advice of theirs after it would contradict it. A bare code or HTTP
// status carries no mark and keeps their own words around it.
export function _replayRefusal(body, fallback) {
    const dropCopy = _downstreamQueueDropCopy(body);
    if (dropCopy) return dropCopy;
    const sentence = body.reasonHuman || body.message;
    if (sentence) return { reason: sentence, sentence: true };
    return { reason: body.error || fallback };
}

export function _isMatchAlreadyCompletedRefusal(err) {
    if (!err) return false;
    return err.code === 'match_completed';
}

// bc-stpf: how long the court console waits for GET .../start-check before a
// pick of another bout asks the operator instead (API.checkStart). The one
// bounded wait in front of an outbox write, and only on that pick path.
export const START_CHECK_TIMEOUT_MS = 1500;

// The answers the outbox keeps a write queued on and retries: the server is up
// but cannot take it yet (a 5xx, the rate limit's 429, or a 403, which on this
// server is never a bad credential (that is 401) but the tournament record not
// usable yet, which an admin can repair at any moment). The start check reads
// them as no answer, since a start queued on one is held, not refused.
export function _serverKeepsRetrying(status) {
    return status >= 500 || status === 429 || status === 403;
}

// bc-stpf: the start check's sentence for a 404: the server holds no such
// match (the draw was discarded or regenerated, or the competition removed,
// since the console's list was read), so a start would be refused too.
export const START_CHECK_NOT_FOUND_MESSAGE = "This match is no longer in the draw. Check the court's list.";

// bc-stpf: the outbox writes that can change what a start check judges: a
// match's status (a score write, which a finish or a start is, a decision, a
// representative bout, a kachinuki bout, a reopen, a send back, a requeue),
// its court, its sides (a pool-rank override seats other competitors in the
// knockout), or a competitor's bar (a decision, a reinstatement).
// API.startCheckUntrusted counts only these.
export const START_CHECK_WRITE_KINDS = new Set([
    'score', 'decision', 'daihyosen', 'kachinuki', 'reopen',
    'revert', 'requeue', 'court', 'reinstate', 'pool-ranks',
]);

// The value a door's `settled` resolves with: the shape the direct call used
// to return.
export function _doorValue(d, body) {
    switch (d && d.kind) {
        case 'revert': return true;
        case 'lineup-remove': return true;
        case 'announce-remove': return true;
        case 'kachinuki': return (body && body.result) ?? body;
        case 'daihyosen': return _daihyosenOutcome(body || {});
        default: return body;
    }
}

// The Error a door's `settled` rejects with: the one the direct call threw.
// A refusal that carries the operator's sentence, as an Error: the server's
// `reasonHuman`, else its raw reason or code. The score and decision doors'
// ineligible arms and the start check (API.checkStart) read it alike, so a
// start the check refuses shows the sentence the start itself would.
export function _reasonHumanError(b, fallback) {
    return new Error(b.reasonHuman || b.reason || b.error || fallback);
}

// parseSkippedCompetitionsHeader parses the server's X-Skipped-Competitions
// header value into [{id, reason}, ...]. The wire format is
// "<id>: <reason> | <id>: <reason>" (internal/mobileapp/handlers_print.go,
// skippedCompetitionsHeaderValue) -- entries joined by " | ", each split on
// the FIRST ": " so a reason that itself happens to contain ": " is not
// truncated. The delimiter is deliberately NOT "; ": the server's own
// sentinel reason strings contain semicolons (e.g. "...no static bracket to
// export; use the live standings view instead"), so splitting on "; " would
// tear a single entry's reason in two and silently drop the second half.
// The header carries the competition ID rather than its name (header values
// are latin-1 on the wire and a kendo competition name routinely holds
// non-ASCII characters), so callers resolve id -> display name themselves.
// Returns [] for an absent/empty header.
export function parseSkippedCompetitionsHeader(headerValue) {
    if (!headerValue) return [];
    return headerValue.split(' | ').reduce((out, entry) => {
        const sep = entry.indexOf(': ');
        if (sep === -1) return out;
        out.push({ id: entry.slice(0, sep), reason: entry.slice(sep + 2) });
        return out;
    }, []);
}

// Strict === false: a body that does not SAY it is unsaved is a lineup, so
// a stub/older-shape response with no `saved` field at all still resolves
// as a lineup rather than being swallowed. Shared by fetchTeamLineup and
// fetchLineupInForce.
const lineupOrNull = (body) => (body.saved === false ? null : body);

// What the daihyosen add or remove came back with. A refusal (superseded,
// clock_skew) is the 200 {applied: false} body itself, returned as it is for
// write_result.jsx's predicates, with the clock relearned as for any refusal.
// No _notifyScoreSuperseded: the team editor reports it from this result
// (notLandedBanner), and that broadcast would repaint a clock refusal with
// the superseded advice. Otherwise the match is unwrapped from its envelope
// ({ result: MatchResult }), as removeKachinukiBout does.
function _daihyosenOutcome(body) {
    if (writeWasSuperseded(body)) {
        late._relearnClockThrottled();
        return body;
    }
    return body.result ?? body;
}

// _requestTimedOut: the one error a request given up on at its deadline
// rejects with (_fetchJson), marked `timedOut` so a caller can tell it from a
// request that failed: the organiser's refresh after a write says nothing
// about one (admin.jsx refreshFailureToast).
function _requestTimedOut() {
    const e = new Error('the request was not answered in time');
    e.timedOut = true;
    return e;
}

// _fetchJson fetches and reads the body as JSON under ONE deadline, on ONE
// AbortController: fetchWithTimeout's abort ends when the headers arrive, and
// here the same controller stays armed until the body has been read. A body
// that stalls is therefore aborted too, which frees its connection, and the
// budget runs on a timer, so a step of the device's wall clock cannot cut it
// short. The body read is also raced against the abort itself, so the deadline
// holds whether or not a platform's json() rejects on it. Resolves to
// { res, body }; an unparseable body reads as {}, as `.json().catch(() =>
// ({}))` does everywhere else. Rejects on a network failure, or past the
// deadline with _requestTimedOut's error, however the abort surfaced.
export async function _fetchJson(url, opts, ms = FETCH_TIMEOUT_MS) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), ms);
    const aborted = new Promise((_resolve, reject) => {
        controller.signal.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
    });
    // Nothing else may observe this rejection: it only ends the race below,
    // whose catch reports every rejection after the abort as the one timeout.
    aborted.catch(() => {});
    try {
        const res = await Promise.race([fetch(url, { ...opts, signal: controller.signal }), aborted]);
        _noteBuildOf(res);
        const body = await Promise.race([
            Promise.resolve().then(() => res.json()).catch((e) => {
                if (controller.signal.aborted) throw e;
                return {};
            }),
            aborted,
        ]);
        return { res, body };
    } catch (e) {
        // Only the deadline aborts this controller, and the race can surface
        // that as fetch's AbortError or the body read's as well as its own.
        if (controller.signal.aborted) throw _requestTimedOut();
        throw e;
    } finally {
        clearTimeout(timer);
    }
}

// What a lineup removal leaves undone when it does not land.
export const LINEUP_NOT_REMOVED = 'The lineup was not removed';

// _readLineup: a lineup's GET, bounded like every sibling request (_fetchJson:
// the deadline covers the body), so a read the server never answers rejects with
// _requestTimedOut's error, which lineupReadFailure words, instead of leaving an
// editor on "Loading lineup…" until the browser gives up on the connection.
// `saved: false` reads as null (lineupOrNull). Shared by fetchTeamLineup and
// fetchLineupInForce. It reads as the page does (readHeaders): an admin route's
// editor gets the fighters' names.
export async function _readLineup(url) {
    const { res, body } = await _fetchJson(url, { headers: readHeaders() });
    if (!res.ok) throw readFailure(res.status, body.error || "Failed to load lineup");
    // An answer whose body could not be read comes back from _fetchJson as an empty
    // object, which cannot be told from one the server sent. A body with no keys is
    // refused here rather than read as no lineup, since an editor would then show an
    // empty lineup as the one the team fields.
    if (Object.keys(body).length === 0) throw new Error("The lineup could not be read. Check the connection and try again.");
    return lineupOrNull(body);
}

// _directFetch: the ONE door of a write that stays direct (not in the outbox).
// Plain fetch with no deadline of its own: a direct write can be the 64 MB
// import or the draw, which a fixed deadline would cut off while the server
// still works. A request that never left the device (fetch rejects with a
// TypeError, whatever the browser's own words) may or may not have reached
// the server, so it is turned into the one sentence WRITE_NOT_CONFIRMED
// (write_result.jsx), marked `noConnection`, so every caller shows it as it
// is. A read-only POST passes NOT_SENT_NO_CONNECTION as `sentence`. The
// original TypeError stays as the error's `cause`. An AbortError and any other
// rejection pass through untouched. So does a connection that drops after the
// headers arrived: the response's body reads (json, text, blob, arrayBuffer,
// formData) reject with a TypeError too, and get the same turn.
export async function _directFetch(url, init, sentence = WRITE_NOT_CONFIRMED) {
    const lost = (e) => {
        if (e instanceof TypeError) throw noConnectionError(sentence, e);
        throw e;
    };
    const res = await fetch(url, init).catch(lost);
    for (const read of ['json', 'text', 'blob', 'arrayBuffer', 'formData']) {
        if (typeof res[read] !== 'function') continue;
        const own = res[read].bind(res);
        res[read] = () => own().catch(lost);
    }
    return res;
}

// One fetch for the backup and restore endpoints: the password headers, and a
// refusal turned into an Error carrying the server's `code`. A confirmPassword
// of undefined sends no header. A POST that only reads (the restore plan) passes
// NOT_SENT_NO_CONNECTION as `sentence`; every other non-GET is a write.
export async function _backupRequest(url, failure, { method = 'GET', password, confirmPassword, body, sentence } = {}) {
    const headers = password ? { 'X-Tournament-Password': password } : {};
    if (confirmPassword !== undefined) headers['X-Confirm-Password'] = confirmPassword;
    const init = { method, headers, ...(body ? { body } : {}) };
    const res = await (method === 'GET' ? fetch(url, init) : _directFetch(url, init, sentence));
    if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        throw Object.assign(new Error(err.error || failure), { code: err.code, status: res.status });
    }
    return res;
}
export function _backupFilename(res) {
    // The server always names the archive: `attachment; filename="<name>.zip"`.
    return /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition'))[1];
}
// Publish draw and Unpublish (bc-drpb): POST /api/competitions/:id/publish-draw
// or /unpublish-draw, organiser-only, no body. A direct write like Generate and
// Discard draw, not an outbox door: two devices racing, the later arrival
// wins, and a competition already in the asked state answers 200 with nothing
// changed. Resolves the competition record as stored; a refusal (409
// `draw_not_ready`, `draw_visibility_self_run`) throws the server's sentence
// with its code on the Error, so the header can tell one from another.
async function _drawVisibilityWrite(id, password, publish) {
    const route = publish ? 'publish-draw' : 'unpublish-draw';
    const res = await _directFetch(`/api/competitions/${encodeURIComponent(id)}/${route}`, {
        method: 'POST',
        headers: { 'X-Tournament-Password': password },
    });
    if (!res.ok) {
        const body = (await res.json().catch(() => null)) || {};
        const e = new Error(body.error || (publish ? 'The draw was not published' : 'The draw was not hidden'));
        if (body.code) e.code = body.code;
        throw e;
    }
    return normalizeCompetitionDetail(await res.json());
}
export function publishDraw(id, password) {
    return _drawVisibilityWrite(id, password, true);
}
export function unpublishDraw(id, password) {
    return _drawVisibilityWrite(id, password, false);
}

// The plan and the restore take the same fields, decisions included: the plan
// applies them, so a copy name already used shows before the password.
export function _restoreForm({ file, from, only, undo, decisions }) {
    const fd = new FormData();
    if (file) fd.append('archive', file);
    if (from) fd.append('from', from);
    if (only) fd.append('only', only);
    if (undo === true) fd.append('undo', 'true');
    if (decisions) fd.append('decisions', typeof decisions === 'string' ? decisions : JSON.stringify(decisions));
    return fd;
}
