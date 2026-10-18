// HTTP client for the mobile-app API.
//
// Split out of api.jsx (T006 / NFR-006). The serializer helpers
// (normalizeMatch, normalizeCompetitionDetail, etc.) live in
// api_serializers.jsx and are imported here for use on responses.
//
// Public function signatures intentionally mirror the original API
// object so importers (admin.jsx, viewer.jsx) keep working unchanged
// when they go through the api.jsx re-export shim.
//
// Auth: mutations send an `X-Tournament-Password` header. Read endpoints
// (/api/viewer/*) are unauthenticated.
//
// Error contract: every method that throws does so with `new Error(msg)`
// where `msg` is the server-reported `error` field if present, else a
// per-method default string. Callers decide whether to .catch and toast.
//
// overrideBracketWinner is the exception: it returns { applied } parsed from
// the 200 body (mp-y3nk), so the caller can tell a landed write from a stale
// reconnect replay the server dropped.
//
// Empty-body methods (overridePoolRanks,
// updateMatchTime, moveMatchCourt,
// deleteCompetition) deliberately return `true`
// rather than `res.json()`: calling res.json() on a 200/204 with no
// body throws SyntaxError per the Fetch spec, which used to surface as
// "alert: Unexpected end of JSON input" right after a successful save.
// See __tests__/api.test.jsx for the regression coverage.
//
// Wifi-hardening additions (mp-gpra):
//   F1: fetchWithTimeout: per-request AbortController, 12 s default.
//   F2: SSE silence watchdog: reconnects if no message for 35 s (>2× heartbeat).
//   F3: SSE reconnect exponential backoff + jitter (1–30 s, reset on open).
//   F4: Write queue persisted to localStorage (no age limit, try/catch safe).
//   F5: Terminal writes (decision, completed score, lineup) durable via queue.

import { normalizeCompetitionDetail, normalizePlayer, toBackendMatchResult, buildPlayerMetadata } from './api_serializers.jsx';
import { unionChanged } from './match_groups.jsx';
import { joinQueuedLineupSave } from './lineup_save.jsx';
import { bridge as _bridge } from './court_bridge.jsx';
// The offset lives in a leaf so a score editor can read the same clock (server_clock.jsx).
import { serverNowMs, serverClockOffsetMs, setServerClockOffsetMs } from './server_clock.jsx';
import {
    writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock, writeMatchClosed, writeResultLocked, writeDisplacedGroups, supersededBanner,
    SUPERSEDED_REASON, SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE, CLOCK_SKEW_UNHEALED_ADVICE,
    downstreamKnockoutPlayedQueueDrop, downstreamKnockoutRunningMessage, downstreamKnockoutRunningReopenMessage,
    downstreamKnockoutRunningQueueDrop, courtBusyMessage,
    FETCH_TIMEOUT_MS, REP_BOUT_NOT_ADDED, REP_BOUT_NOT_REMOVED, tapOutcomeQuiet,
    isFinishedResultWrite,
} from './write_result.jsx';
// bc-evts S2: the build this page runs against the server's, and the reload rule.
import { noteServerBuild, setOutboxIdleProbe, setBeforeReload, noteOutboxDrained } from './app_version.jsx';
import { CLOCK_SKEW as TAP_CLOCK_SKEW } from './score_events.jsx';
import { scoreRowMatchLabel } from './pool_ids.jsx';
import { createLandedTaps, LANDED_TAB_BEAT_MS } from './landed_taps.jsx';
import {
    createMintedMembers, mintedAddId, mintedIdsNamed, mintedMemberUrlId, mintedProofCovers, mintedStrip,
    MINTED_MEMBERS_KEY,
} from './minted_members.jsx';

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
function fetchWithTimeout(url, opts, ms = FETCH_TIMEOUT_MS) {
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
function _refusalText(body, fallback) {
    return (body && (body.message || body.error)) || fallback;
}

// _refusalError: the Error a refused request is thrown with, in _refusalText's
// words. A refusal that carries a sentence carries its code beside it, and the
// Error keeps that code, so a caller can tell one refusal from another without
// comparing words (the team editor and the Lineups page word a member who
// already has a name alike through it, lineup_resolver.jsx memberRefusalNote).
function _refusalError(body, fallback) {
    const e = new Error(_refusalText(body, fallback));
    if (body && body.message && body.error) e.code = body.error;
    return e;
}

// reopenFailureError builds the Error thrown by a failed reopen. reopenMatch and
// requeueBlockerAndReopen surface the SAME structured shape (mp-gmcg review R8):
// the operator-facing `message`, the machine `code`, and the court/matchId/compId
// of the BLOCKING match, which applyReopenFailure (admin_scoring_team.jsx) reads
// to re-offer the requeue remedy when a court is busy. One builder so a new
// server field is threaded once, not per endpoint. Async: it awaits the body.
// _reopenRefusal: the Error a refused reopen (or requeue-and-reopen) answers
// with, built from the refusal body; the outbox rejects a reopen's `settled`
// with it, exactly what the direct call used to throw.
function _reopenRefusal(err) {
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

// reopenBody is the optional half of both reopen requests: the audit reason
// and the downstream confirmation, each sent only when set, so the one-tap
// kachinuki Reopen still posts {} exactly as before.
function reopenBody(reason, force) {
    const body = {};
    if (reason) body.reason = reason;
    if (force) body.forceDownstreamReopen = true;
    return body;
}

// normalizeViewerCompItem maps one {config, poolMatches, bracket} item from the
// aggregate GET /api/viewer/competitions or the court-scoped GET
// /api/viewer/court/:court/matches into the flattened, normalized competition shape
// the UI consumes (config fields hoisted, players + matches normalized). Shared
// so both endpoints stay in lockstep.
function normalizeViewerCompItem(item) {
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
        // Whether this competition's participants.csv actually LOADED, also a
        // sibling of config for the same reason. Hoisted like dataIssues or it
        // is silently dropped. Absent (an older payload) is read as loaded by
        // rosterFullyLoaded, so this never makes an existing client quieter
        // than it was.
        rosterAvailable: item.rosterAvailable,
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
        squads: _withListedMembersForgotten(item.teamMembers, c.id),
        players: (c.players || []).map(normalizePlayer),
    });
}

// _withListedMembersForgotten: a members map the server answered, after
// telling the owner every member it holds exists (minted_members.jsx).
function _withListedMembersForgotten(teams, competitionId) {
    // A member judged never to have been that the list holds: told (N4).
    for (const m of _minted.listed(teams, competitionId)) _announceKeptMember(m);
    return teams;
}

// ---------------------------------------------------------------------------
// C2: Monotonic per-match revision counter
// ---------------------------------------------------------------------------
// Stamps "running" autosave writes so the server can drop out-of-order
// delivery from a reconnect flush. The counter is per (compID, matchID) and
// increases strictly monotonically within a page session. Completed writes
// do not use it (the guard is gated to running status on the server too).
// ---------------------------------------------------------------------------

/** Composite key for per-(compID, matchID) maps. Prevents collisions across competitions. */
function _revKey(compID, matchID) { return `${compID}:${matchID}`; }

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
function _downstreamRefusalError(body, opts) {
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
function _courtBusyError(body) {
    if (!body || body.error !== 'court_busy') return null;
    const err = new Error(courtBusyMessage({ court: body.court, label: body.label }));
    err.code = body.error;
    if (body.court) err.court = body.court;
    if (body.matchId) err.matchId = body.matchId;
    if (body.compId) err.compId = body.compId;
    if (body.label) err.label = body.label;
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
function _replayRefusal(body, fallback) {
    const dropCopy = _downstreamQueueDropCopy(body);
    if (dropCopy) return dropCopy;
    const sentence = body.reasonHuman || body.message;
    if (sentence) return { reason: sentence, sentence: true };
    return { reason: body.error || fallback };
}

// ---------------------------------------------------------------------------
// mp-y3nk: server-clock offset for timestamp reconciliation.
//
// Writes (score / override-winner) are stamped in SERVER-RELATIVE time so an
// offline court's changes reconcile against other courts by ONE clock, not each
// tablet's local clock. The offset is learned from GET /api/time and refreshed
// on reconnect. Until the first successful learn, the offset is 0 (writes carry
// approximately local time); the server treats an unstamped/legacy 0 as
// arrival-order, and a small offset error only matters for genuinely concurrent
// same-field edits, so the degradation is graceful.
// ---------------------------------------------------------------------------
// bc-cse: has ANY learn ever succeeded? The load-time retry chain below stops
// scheduling once this flips, and it never flips back: a later failed refresh
// keeps the last good offset rather than reopening the retry chain.
let _clockOffsetLearned = false;
// bc-cse (clock-skew hardening): SINGLE-FLIGHT. Every learn goes through here,
// so a caller that must have a fresh offset RIGHT NOW (the clock_skew recovery
// paths below, which await this) can never race a background learn into two
// concurrent GETs, and never has to invent its own dedupe. A joiner gets the
// in-flight learn's promise: /api/time always answers with the CURRENT server
// time, so an already-outstanding request is exactly as good as a new one.
// Deliberately does NOT open the throttle window (_clockRelearnLastAt): that
// window belongs to the throttled wrapper, and stamping it here would let the
// load-time learn silence the very first supersede-driven relearn.
let _clockLearnInFlight = null;
function _learnServerClockOffset() {
    if (_clockLearnInFlight) return _clockLearnInFlight;
    _clockLearnInFlight = _learnServerClockOffsetOnce().finally(() => { _clockLearnInFlight = null; });
    return _clockLearnInFlight;
}
async function _learnServerClockOffsetOnce() {
    try {
        const t0 = Date.now();
        const res = await fetchWithTimeout('/api/time', {}, 5000);
        if (!res.ok) return;
        const body = await res.json();
        const t1 = Date.now();
        if (typeof body.nowMs !== 'number') return;
        // Net out the round-trip: estimate the server clock at the response
        // arrival by adding half the RTT to the reported time.
        setServerClockOffsetMs((body.nowMs + Math.round((t1 - t0) / 2)) - t1);
        _clockOffsetLearned = true;
    } catch (_e) {
        // Offline / timeout: keep the last known offset (0 on first ever call).
    }
}

// bc-cse: relearn the offset when something says this device's clock frame may
// be wrong. Four triggers today: a superseded write (the original), a heartbeat
// whose nowMs diverges from our frame, the browser 'online' event, and a write
// the server refused outright for clock skew.
//
// A 200 {"applied": false} is the ONE signal that this device's stamps may be
// running behind the server's frame (see the mp-y3nk block above for what the
// offset is and how it is normally learned). Without this, a device whose
// load-time learn failed and which never opens SSE keeps an offset of 0 for the
// whole session: the operator is told to check the recorded result and re-enter
// it, and the re-entry loses to the same uncorrected skew. Relearning here makes
// that advice ACTIONABLE — the next manual entry is stamped in the server's
// frame, so it is judged on its real recency instead of being refused for a
// skew the operator cannot see or fix. It is not a guarantee of winning: a
// genuinely newer stored result should still beat the re-entry, and does.
//
// It deliberately does NOT touch replays: queued payloads are stamped at enqueue
// time and replay verbatim (their staleness is real), so only writes stamped
// AFTER the relearn benefit.
//
// Throttled so a reconnect flush that surfaces several superseded drops
// back-to-back collapses them into a single GET. The bound is a RATE, not a
// per-flush cap: the 5s window is measured from the previous learn's
// COMPLETION, and a queued write may take up to fetchWithTimeout's 12s abort,
// so a flush of slow drops can legally fire one GET per ~5s. That ceiling is
// the intended one - a drop landing after the window really is fresh evidence
// that this device's clock frame is still wrong, and one tiny GET per 5s costs
// nothing next to re-losing the operator's result.
const CLOCK_RELEARN_THROTTLE_MS = 5000;
let _clockRelearnInFlight = false;
let _clockRelearnLastAt = 0;
function _relearnClockThrottled() {
    if (_clockRelearnInFlight) return;
    if (_clockRelearnLastAt !== 0 && (Date.now() - _clockRelearnLastAt) < CLOCK_RELEARN_THROTTLE_MS) return;
    _clockRelearnInFlight = true;
    _learnServerClockOffset().finally(() => {
        _clockRelearnInFlight = false;
        _clockRelearnLastAt = Date.now();
    });
}
// _serverNowMs returns the current time in the server's clock frame. Never
// negative-guarded: callers only compare relative order, so a monotonic-ish
// value is what matters.
function _serverNowMs() { return serverNowMs(); }

// Monotonic reading, or null where performance.now() is unavailable (older
// embedded webviews, and the non-browser imports the test harness does). Null
// simply means "no monotonic anchor", which every caller already handles: the
// wall-clock reconstruction is the fallback.
function _perfNow() {
    return (typeof performance !== 'undefined' && typeof performance.now === 'function')
        ? performance.now()
        : null;
}

// bc-hlck: a write is never stamped older than the result it was made against
// (operator request 2026-10-03). The stamp is this device's estimate of the
// server's time, and two devices' estimates can differ by up to the 5 s the
// server accepts: a device running ahead stamps its write later than real
// time, and a second device that sees that write and changes it a moment
// later would stamp its change EARLIER than the write it changed, so the
// server would keep the change in the history instead of applying it.
//
// _stampAfterSeen is the ONE rule (a hybrid logical clock): this device's
// stamp, or the stamp of the match as the operator saw it plus one, whichever
// is later. Wall-clock order stands wherever the clocks agree (an offline
// court's genuinely later action still wins); where they disagree, a change
// still lands after what it changed. The floor stops HLC_FLOOR_MAX_AHEAD_MS
// past this device's own time, inside the 5 s the server accepts
// (modifiedAtRefuseSkewMs), so chasing a stamp seen far ahead can never get
// the write refused for clock skew; past that, the write is ordered by its
// own time as before.
//
// What "as seen" is belongs to the caller: the match the operator acted on
// (`match.modifiedAt` for a tap, `seenModifiedAt` in a decision's body or
// a representative bout's or a hand-set winner's argument, or on a score
// write's patch), never the match at the send: a tap made before another
// device finished the match stays older than that finish (operator ruling
// 2026-09-27). 0 or absent: no floor.
const HLC_FLOOR_MAX_AHEAD_MS = 4000;
function _stampAfterSeen(stamp, seen) {
    const s = Number(seen);
    if (!Number.isFinite(s) || s <= 0 || stamp > s) return stamp;
    return Math.max(stamp, Math.min(s + 1, _serverNowMs() + HLC_FLOOR_MAX_AHEAD_MS));
}
// What a score write was made against: the `seenModifiedAt` its patch carries
// (even 0: the editor saw no stamp), else the match handed in.
function _seenOfScore(result, match) {
    if (result && result.seenModifiedAt !== undefined) return Number(result.seenModifiedAt) || 0;
    return Number(match && match.modifiedAt) || 0;
}
// The seen stamp a payload was floored by, kept beside it (never sent) so the
// queue can record it on the entry (_commitEnqueue, seenModifiedAt) and a
// re-stamped replay keeps the floor (_restampQueuedEntryForSkew).
const _payloadSeen = new WeakMap();
function _stampPayload(payload, stamp, seen) {
    payload.modifiedAt = _stampAfterSeen(stamp, seen);
    const s = Number(seen);
    if (Number.isFinite(s) && s > 0) _payloadSeen.set(payload, s);
    return payload;
}

// _perfAgeMs: whole milliseconds since a monotonic reading, or null when there
// is none (no reading, or no performance.now()). Rounded UP, for two reasons:
// the server reads modifiedAt as an int64 and refuses a fraction with a 400,
// and a stamp built as "server now minus this age" then never lands later than
// the moment it stands for, which is the safe direction (see _restampFor).
function _perfAgeMs(reading) {
    const now = _perfNow();
    return (now === null || !Number.isFinite(reading)) ? null : Math.ceil(now - reading);
}

// ---------------------------------------------------------------------------
// bc-cse: clock-skew REFUSAL (the server half of this change)
// ---------------------------------------------------------------------------
// A write whose modifiedAt lands more than the server's margin (5s) in ITS
// future is not stored at all: the server answers 200
// {"applied": false, "reason": "clock_skew", "serverNowMs": …}. That is a
// different animal from a plain supersede. A supersede means "someone else's
// newer result won" and the operator must go and LOOK at what is recorded; a
// clock_skew refusal means "your stamp is nonsense" and nothing about the
// tournament state has changed - the same write, re-stamped in the server's
// frame, is exactly what should be stored. So this client heals and retries
// rather than telling the operator to check anything. (The heal-and-retry is
// the SCORE path's: recordDecision deliberately only reports — see the note at
// its own applied:false branch.)
//
// `reason` absent, or 'superseded', keeps the pre-existing behaviour verbatim.
//
// The TEST for that shape is writeWasRefusedForClock (write_result.jsx), asked
// wherever a parsed body is on hand, for the same reason writeWasSuperseded is:
// a response test re-derived per call site is a response test that drifts. This
// constant remains for the two places that are not testing a body - the wire
// label on a bracket-resync notification, and the override path, which has
// already collapsed the body down to a boolean.
const CLOCK_SKEW_REASON = 'clock_skew';
// The operator-facing copy lives in write_result.jsx beside the SUPERSEDED
// pair, not here: it is a VERDICT string, and the whole point of this response
// is that its verdict is the opposite of the superseded one. A local copy of it
// drifted almost immediately - it kept the superseded advice ("check what is
// recorded") under a comment claiming it named the clock - which is exactly the
// failure one owner prevents. This module keeps only the wire value it
// compares, and the one reason string that has no consumer outside the queue.
const CLOCK_SKEW_FAIL_REASON = 'the device clock could not be reconciled with the server';

// Heartbeat tripwire threshold. Deliberately BELOW the server's 5s refusal
// margin: the tripwire is meant to fire and heal while writes are still being
// accepted, not after the first result has already been refused.
const CLOCK_TRIPWIRE_MS = 3000;

// The scoreline fields a start-only write never publishes on the court bridge
// (bc-evts stage 4 review 2, G1): what engine.keepQueuedScore keeps from the
// stored match, plus the editors' `score` summary. _broadcastPatch is the one
// reader.
const START_BROADCAST_DROPS = Object.freeze([
    'ipponsA', 'ipponsB', 'hansokuA', 'hansokuB', 'encho', 'subResults',
    'flagsA', 'flagsB', 'repPlayerA', 'repPlayerB', 'score',
]);

const _matchRevCounters = new Map(); // compID:matchID → int
function _nextRev(compID, matchID) {
    const key = _revKey(compID, matchID);
    const next = (_matchRevCounters.get(key) || 0) + 1;
    _matchRevCounters.set(key, next);
    return next;
}

// Per-page-load scoring-session id. Identifies one client session so the
// server's rev-guard can drop a single session's own out-of-order delivery
// (a reconnect flush). Different sessions are concurrent operators: the
// server treats those as last-write-wins. jsdom-safe fallback.
const _revSession = (typeof crypto !== 'undefined' && crypto.randomUUID)
    ? crypto.randomUUID()
    : `s${Math.random().toString(36).slice(2)}`;

// ---------------------------------------------------------------------------
// C2 / F4 / F5: Offline write queue
// ---------------------------------------------------------------------------
// Holds every pending write, per match an ORDERED list (bc-mrgc phase 3,
// operator ruling 2026-10-03: "Nothing should be dropped. All events must be
// ordered."). On any network failure the write is queued here and retried on
// reconnect.
//
// One entry per write, each under its own key: `${base}${QUEUE_ENTRY_SEP}${id}`,
// where the BASE is the match the write is about (`compID:matchID` for a score
// or a decision, `override:…`, `lineup:…`). Each flush pass sends the entries
// in the order of the stamp they carry (_entryStamp), and each entry on its
// own: one that does not land (offline, a 5xx that keeps coming back) holds
// back nothing after it, and a new write goes straight to the server whatever
// is queued. The server, not arrival order, decides the result: it merges a
// match write group by group in stamp order and keeps whatever a newer change
// outranks in the match's history (bc-mrgc), so a write that waited behind a
// failing one gained nothing and lost the court every write after it. The ONE
// coalescing rule (_coalesceTarget): a score write takes the place of the
// match's LAST entry only when that entry is a score write no newer than it,
// and a running write only of a running one, claiming the groups both changed
// (union, as bc-mrgc phase 2 does); a lineup save joins the queued save of the
// same lineup, so a lineup has ONE entry (lineup_save.jsx: the union of the
// positions the two changed, the later value standing where both changed one),
// because a lineup PUT carries no stamp the server could order it by. Nothing
// else replaces anything: a running write never replaces a queued Finish or
// a decision, and a decision and a score for the same match are two entries.
// A key persisted by an earlier build (one entry per match, keyed by the base
// itself) is a valid entry of this shape: its base is the key, so an old queue
// loads and replays as it is.
//
// F4: The queue is persisted to localStorage on every change so that a page
// reload or crash during a wifi gap does not lose unsaved scores/decisions.
// Nothing is dropped for its age: an old entry is sent like any other, and the
// server orders it by its stamp, applying what is still the latest change and
// keeping what a newer change outranks in the match's history (it used to be
// discarded after 12 h, announced, but gone).
//
// F5: Terminal writes (completed score, decision, lineup) use the same queue
// with additional descriptor fields: kind, terminal, method, url.
//
// Sync-status state is published via _syncStatus / _syncListeners so the UI
// (SyncStatusPill) can render synced / syncing / offline without coupling to
// any Preact component tree.
// ---------------------------------------------------------------------------

/**
 * @typedef {'synced'|'syncing'|'offline'|'auth-required'|'server-error'} SyncStatusValue
 */

/**
 * @typedef {'score'|'decision'|'lineup'|'override'} WriteKind
 */

/**
 * Descriptor shape for every queue entry:
 *   compID, matchID: for identity / rev-counter management (scores/decisions)
 *   payload         : request body (object, will be JSON-stringified)
 *   password        : X-Tournament-Password header value
 *   kind            : 'score' | 'decision' | 'lineup'
 *   terminal        : true for completed / decision / lineup writes
 *   method          : HTTP method string ('PUT' | 'POST')
 *   url             : same-origin request path, e.g. /api/competitions/… (for replay in _flushQueue)
 *   enqueuedAt      : Date.now() at enqueue time (the outbox's "which copy is
 *                     newer" test, and the order of an entry with no stamp)
 *   perfAtEnqueue   : performance.now() at enqueue time (bc-cse). SESSION-LOCAL
 *                     and deliberately NOT persisted - it is measured from this
 *                     document's time origin, so it is meaningless once
 *                     rehydrated. Absent means the clock-skew re-stamp uses the
 *                     wall-clock reconstruction alone; see _restampFor.
 *   attempts        : count of server rejections (5xx/429/403) on flush retries;
 *                     absent until the first rejection. Network failures never
 *                     increment it. At SERVER_REJECTION_NOTICE_THRESHOLD the
 *                     operator is told; the entry is never dropped for it.
 *   skewRetried     : true once this entry has been re-stamped and retried after
 *                     a clock_skew refusal (bc-cse). ABSENT means not-yet-retried,
 *                     which is the safe default for an entry persisted by an
 *                     older build. A second clock_skew refusal on a marked entry
 *                     drops it rather than retrying a third time: the stamp is
 *                     reconstructed by _restampFor (the older of the wall-clock
 *                     and monotonic estimates),
 *                     so if that is still refused the device's frame cannot be
 *                     reconciled and further attempts would poison-loop.
 *                     A reload landing between the re-stamp and the retry
 *                     rehydrates the entry with the flag already set, so its
 *                     first refusal in the new session drops it with no retry
 *                     at all: conservative by design, since the persisted stamp
 *                     was already built from a learned offset.
 *   authBlocked     : true once the server answered 401. The entry stays QUEUED
 *                     but is skipped by the flush loop, because it carries the
 *                     password it was enqueued with and can only reproduce the
 *                     same 401 until API.resumeAfterAuth() re-stamps it. Persisted
 *                     with the entry, so a parked write survives a reload and is
 *                     resumed when the operator next authenticates.
 *
 * Running score descriptors set terminal=false and do not use method/url
 * (they are always PUT to the score endpoint: _flushQueue hard-codes that
 * path for backward compat with the running path).
 *
 * @typedef {{
 *   compID: string,
 *   matchID: string,
 *   payload: object,
 *   password: string,
 *   kind: WriteKind,
 *   terminal: boolean,
 *   method?: string,
 *   url?: string,
 *   enqueuedAt: number,
 *   attempts?: number,
 *   skewRetried?: boolean,
 * }} WriteDescriptor
 * `method`/`url` are present only on terminal entries; running entries omit them.
 */

// The outbox's own key (bc-evts stage 3 review 3, F4). It was 'bc_write_queue',
// which a tab of an older build open across the upgrade shares: that tab read
// this build's entries (tap events it does not know), rewrote them into its own
// score writes and pruned the shared key on a 4xx, so new taps went out of
// order or were lost. What an older build leaves under the old key is taken
// over (_takeOverLegacyQueue), on load and whenever an older tab writes it.
const QUEUE_STORAGE_KEY = 'bc_write_queue_v2';
const LEGACY_QUEUE_STORAGE_KEY = 'bc_write_queue';

// Defense-in-depth allowlist for terminal-write replay. The queue only ever
// holds score/decision/lineup writes, which are ALWAYS PUT/POST to
// /api/competitions/…: so a tampered or corrupted bc_write_queue entry can't be
// flushed into some other endpoint or HTTP method. Used by both rehydrate (load)
// and flush, kept in one place so the two checks can't drift apart.
const _ALLOWED_QUEUE_METHODS = ['PUT', 'POST', 'DELETE'];
function _isAllowedTerminalRequest(method, url) {
    if (!_ALLOWED_QUEUE_METHODS.includes(method) || typeof url !== 'string' || !url) return false;
    // Defense-in-depth: localStorage is tamperable. Resolve the URL against our own
    // origin and validate the NORMALIZED same-origin pathname. A naive
    // startsWith('/api/competitions/') would let a dot-segment payload such as
    // "/api/competitions/../../api/admin/secrets" pass while fetch normalizes it to
    // "/api/admin/secrets" on the wire. The URL parse also rejects absolute,
    // cross-origin, and protocol-relative ("//evil.com/…") URLs.
    const origin = (typeof location !== 'undefined' && location.origin) || 'http://localhost';
    let u;
    try {
        u = new URL(url, origin);
    } catch (_e) {
        return false;
    }
    if (u.origin !== origin || !u.pathname.startsWith('/api/competitions/')) return false;
    // A DELETE is queued for five doors: the representative bout's remove, a
    // kachinuki bout's removal (bc-evts), a team member's name cleared, a
    // match's own lineup removed (review 2, R1) and a competition's pool-rank
    // overrides reset (review 11, Y7).
    if (method === 'DELETE') {
        return /\/(daihyosen|kachinuki-bout)$/.test(u.pathname)
            || /^\/api\/competitions\/[^/]+\/overrides$/.test(u.pathname)
            || /\/teams\/[^/]+\/(members|match-lineups)\/[^/]+$/.test(u.pathname);
    }
    return true;
}

// ---------------------------------------------------------------------------
// F4: localStorage helpers (all accesses wrapped in try/catch for
// private-browsing / storage-quota safety: mirroring app.jsx pattern).
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// bc-qttl: queue-alert channel
// ---------------------------------------------------------------------------
// A queued write that will never reach the server must be ANNOUNCED, not
// silently discarded. Deliberately SEPARATE from _terminalFailListeners, which
// is addressed to one open editor (its subscribers filter on compID/matchID)
// and is therefore useless for the two cases that matter here: a drop detected
// during rehydration, when no editor has mounted yet, and a drop on a match the
// operator is not currently looking at. Same reasoning that split
// _notifyBracketResync out rather than overloading the terminal-fail channel.
//
// Alerts raised before any listener exists are BUFFERED and replayed to the
// first subscriber. This is load-bearing, not a nicety: the rehydrate IIFE runs
// at module load, long before App mounts, so without the buffer the rehydration
// alert (the whole point of bc-qttl) would publish into an empty Set and vanish
// exactly like the writes it is reporting.
//
// Alert shape: {kind, count, terminalCount, detail?, compID?, matchID?}
//   'unreadable'    dropped: corrupt/tampered entry that can never be replayed
//   'rejected'      the server permanently refused it (non-retryable 4xx)
//   'server_error'  the server keeps failing it; STILL QUEUED, still retrying
//   'auth_required' parked pending re-authentication; still queued
//   'storage_full'  the queue could not be persisted; a reload would lose it
//   'discarded'     dropped wholesale on credential revocation (password reset)
//   'sent'          GOOD news (bc-offl): held finished results landed in one
//                   flush; the one kind App renders as a success toast
//   'displaced'     held finished results landed and moved a later change of
//                   their match to its history; recorded, told once.
// ---------------------------------------------------------------------------
const _queueAlertListeners = new Set();
const _pendingQueueAlerts = [];
function subscribeQueueAlert(fn) {
    _queueAlertListeners.add(fn);
    // Replay anything raised before this subscriber existed, then clear: the
    // buffer is a handoff to the first listener, not a durable log.
    if (_pendingQueueAlerts.length > 0) {
        const drained = _pendingQueueAlerts.splice(0, _pendingQueueAlerts.length);
        for (const alert of drained) {
            try { fn(alert); } catch (_e) { /* swallow */ }
        }
    }
    return () => _queueAlertListeners.delete(fn);
}
function _notifyQueueAlert(alert) {
    if (_queueAlertListeners.size === 0) { _pendingQueueAlerts.push(alert); return; }
    for (const fn of _queueAlertListeners) {
        try { fn(alert); } catch (_e) { /* swallow */ }
    }
}

// Count of queued entries that represent a FINISHED result (completed score,
// decision, lineup). Alerts report this separately from the raw queue size
// because "3 results were lost" and "3 writes were lost" are different claims
// to an operator, and only the terminal ones are finished work.
function _countTerminalQueued() {
    // Finished results only (isFinishedResultWrite, R5): an entry's `terminal`
    // says how it is sent, not that it is a result.
    let n = 0;
    for (const d of _writeQueue.values()) { if (isFinishedResultWrite(d)) n++; }
    return n;
}

// Latched so a wedged quota announces once per failure run rather than on every
// keystroke-driven autosave. Reset on the first successful persist.
let _storageFullAnnounced = false;

// True while browser storage still holds a write this tab settled, because
// the write that takes it out was refused: the outbox is not idle then
// (_outboxIdle), or a reload would read the write back and send it again
// (stage 3 review 9, P2). Cleared by the next persist storage takes.
let _settledLeftStored = false;

/**
 * Persist the queue. Returns true when the queue's current state is safely on
 * disk, false when it could not be written.
 *
 * The stored queue is shared by every tab of this origin, so a persist MERGES
 * into what storage holds rather than overwriting it: this tab's entries are
 * written into it (a key both hold keeps the newer write), the entries this
 * tab settled since the last persist are taken out of it (_settled), and
 * anything else, another tab's queued writes, is left alone. A whole-map overwrite dropped the other tab's entries, and an
 * empty queue removed the key under them, so a result queued in one tab was
 * lost when another tab saved or reloaded (bc-sync). An emptied queue is
 * stored as "[]"; only clearQueue's wipe removes the key (P1, below).
 *
 * bc-qttl: the failure is no longer swallowed. A queue that cannot persist is a
 * queue that a reload will silently lose, which is precisely the durability
 * promise the outbox exists to make: with no size cap, a long offline run can
 * reach the localStorage quota, and every unsent result then died on the next
 * refresh with nothing shown to the operator.
 * @returns {boolean}
 */
function _persistQueue() {
    // Storage still holds a write this tab settled: if the write that takes
    // it out is refused, a reload would read it back and send it again (P2).
    let settledStillStored = false;
    try {
        const merged = _storedQueue();
        for (const [k, at] of _settled) {
            const stored = merged.get(k);
            // A newer entry another tab wrote under the same key since is theirs.
            if (stored && !_enqueuedAfter(stored, at)) {
                merged.delete(k);
                settledStillStored = true;
            }
        }
        // A key both hold keeps the newer write. This tab's copy can be one it
        // took from storage (on load, or from another tab), and writing it back
        // over a later write the other tab stored since, such as a queued
        // Finish replacing an autosave, would drop that write from storage:
        // lost, if that tab then closed or reloaded before sending it.
        for (const [k, d] of _writeQueue) {
            if (!_enqueuedAfter(merged.get(k), d.enqueuedAt)) merged.set(k, d);
        }
        // A settled member record nothing names any more is forgotten.
        _mintedPrune(merged);
        if (merged.size === 0) {
            // An emptied queue is stored as "[]", never removed: the other
            // tabs read the removal of the key as a wipe (clearQueue), which
            // leaves each its own writes, so a tab whose write this one sent
            // would keep it and send it again later over a newer one (stage 3
            // review 9, P1). "[]" tells them every write they shared is gone.
            // Only a key that is there: after clearQueue's wipe, a flush still
            // out must not store the queue again.
            if (localStorage.getItem(QUEUE_STORAGE_KEY) !== null) localStorage.setItem(QUEUE_STORAGE_KEY, '[]');
            _settled.clear();
            _settledLeftStored = false;
            _storageFullAnnounced = false;
            return true;
        }
        // perfAtEnqueue is SESSION-LOCAL and must not be serialized: it is
        // measured from this document's time origin, so persisting it would let
        // a reload (or a second tab reading the same key) subtract two readings
        // taken from different zeros and derive a nonsense age. Stripped here
        // rather than at the read side so there is one place to reason about,
        // and so an entry written by an older build behaves identically to a
        // rehydrated one: no anchor, wall-clock reconstruction only.
        // A downstream confirmation (forceDownstreamReopen) is the operator's
        // answer to a dialog on THIS page: it is sent, but never stored, so a
        // reload or another tab never replays a confirmation nobody gave.
        const entries = [...merged.entries()].map(
            ([k, d]) => [k, {
                ...d, perfAtEnqueue: undefined,
                ...(d.payload && d.payload.forceDownstreamReopen ? { payload: _withoutDownstreamConfirmation(d.payload) } : {}),
            }]
        );
        localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify(entries));
        // Only once the write has landed: a failed one must still prune these
        // entries next time, or a settled write would come back from storage.
        _settled.clear();
        _settledLeftStored = false;
        _storageFullAnnounced = false;
        return true;
    } catch (_e) {
        // What it took out stays in _settled for the next persist. While
        // storage still holds any of it the outbox is not idle (_outboxIdle),
        // so neither Reload nor the automatic reload goes and reads it back
        // (stage 3 review 9, P2).
        _settledLeftStored = settledStillStored;
        // Storage refused, but this tab holds nothing to store: nothing of
        // its can be lost, so it raises nothing, and answers stored unless a
        // write it settled is still there (L2, stage 3 review 8: a reload
        // must not wait on storage an empty outbox does not need).
        if (_writeQueue.size === 0) return !settledStillStored;
        if (!_storageFullAnnounced) {
            _storageFullAnnounced = true;
            _notifyQueueAlert({
                kind: 'storage_full',
                count: _writeQueue.size,
                terminalCount: _countTerminalQueued(),
                detail: 'browser storage is full',
            });
        }
        return false;
    }
}

/** @type {Map<string, WriteDescriptor>} */
const _writeQueue = new Map(); // queue key → pending write descriptor

// Entries this tab has taken out of its queue since the last persist: queue
// key → the enqueuedAt of the entry removed (Infinity when it carried none).
// _persistQueue prunes them from storage, which it otherwise leaves to the
// tabs that wrote them.
const _settled = new Map();

// What is known of every member id this device (or another tab) minted: the
// ONE owner, minted_members.jsx (bc-evts stage 3, M1 a), shared through
// browser storage. Its storage is reached through this module, as every other
// read and write of browser storage is. Declared here, before the load-time
// flush.
const _minted = createMintedMembers({
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
// _takenFromLegacy: every entry this tab took over from the key an older build
// uses (_takeOverLegacyQueue), its key to the `enqueuedAt` it was taken with,
// kept for the tab's life so none is taken over, and sent, twice (G5). A later
// write an older build stores under the same key (before bc-mrgc phase 3 a key
// named only its match) is newer, and is taken.
const _takenFromLegacy = new Map();

// Descriptors this tab took up from another tab's write to the stored queue
// (_onStoredQueueChanged) rather than queued itself.
const _adopted = new WeakSet();

function _markSettled(key, descriptor) {
    const stamped = descriptor && typeof descriptor.enqueuedAt === 'number' && Number.isFinite(descriptor.enqueuedAt);
    const at = stamped ? descriptor.enqueuedAt : Infinity;
    _settled.set(key, Math.max(_settled.get(key) ?? -Infinity, at));
    // An entry with no stamp is recorded as older than every stamped copy
    // (L5): it cannot be told from a later write stored under its key, which
    // Infinity would then refuse for the page's life.
    const prev = _settledHere.get(key);
    const here = Math.max(prev ? prev.at : -Infinity, stamped ? at : -Infinity);
    const now = Date.now();
    // Re-inserted, so the map runs in settle order and the sweep stops at the
    // first record still young.
    _settledHere.delete(key);
    _settledHere.set(key, { at: here, settledAt: now });
    for (const [k, r] of _settledHere) {
        if (now - r.settledAt <= SETTLED_HERE_KEEP_MS) break;
        _settledHere.delete(k);
    }
}

// Every entry this tab has taken out of its queue, as _settled records it,
// but kept past the persist that empties _settled: a copy another tab stores
// again under the same key, no newer, is one this tab already settled, and is
// never taken up again (K2, _wasSettledHere). Key -> { at: the newest
// enqueuedAt settled (-Infinity for an entry that carried none), settledAt }.
const _settledHere = new Map();

// How long a settle is remembered (L5, stage 3 review 8). A stale copy comes
// back when another tab that still held the entry writes the queue before it
// has seen it leave storage: at its next persist, which a live tab makes
// within moments and a tab the browser suspended makes when it wakes. An hour
// covers a suspended tablet woken within a session; past it the copy is taken
// up as any entry, which the server answers as the duplicate it is.
const SETTLED_HERE_KEEP_MS = 60 * 60 * 1000;

// _wasSettledHere: the stored copy `d` of `key` is one this tab settled.
function _wasSettledHere(key, d) {
    const r = _settledHere.get(key);
    if (!r) return false;
    if (Date.now() - r.settledAt > SETTLED_HERE_KEEP_MS) {
        _settledHere.delete(key);
        return false;
    }
    return !_enqueuedAfter(d, r.at);
}

// _dequeue: the ONE way out of the queue. Removes `key`, only while it still
// holds `descriptor` when one is given (a newer write may have replaced it
// mid-flush), and marks it settled so the next persist drops it from storage
// too. A bare _writeQueue.delete would leave the stored copy for the merge to
// keep, and the write would be replayed on the next load.
// _unorderedValue: an entry left the queue with a value the server never
// ordered by its stamp: a discard, or a clock refusal (a tap's outcome, or a
// write's answer).
function _unorderedValue(v) {
    if (!v || typeof v !== 'object') return false;
    return v.discarded === true || v.outcome === TAP_CLOCK_SKEW || writeWasRefusedForClock(v);
}

function _dequeue(key, descriptor, outcome) {
    const d = _writeQueue.get(key);
    if (!d || (descriptor && d !== descriptor)) return false;
    _writeQueue.delete(key);
    _markSettled(key, d);
    _noteOwnStatusAnswer(key, d, outcome);
    if (outcome && outcome.byOperator && _inFlight.has(d)) {
        // Discarded while its request is out: the server may still keep it,
        // so what a discard undoes waits for that request to end (L1,
        // _endDiscardWhileOut); a member add's request end is the owner's
        // answer (_mintedAddAnswered).
        _discardWhileOut(d);
    } else if (outcome && (outcome.error || outcome.byOperator)) {
        // A court move or time change the server refused, or an operator
        // discarded, no longer shows (R4). Never for an entry another tab sent.
        _revertLocalFields(d);
        // A member add that leaves unanswered (a refusal was answered as it
        // came, _mintedAddAnswered): the owner says what is known of its id.
        if (mintedAddId(d)) {
            if (_minted.discarded(d) === 'dropped') _learnDropped([_droppedOf(d)]);
            else _mintedSweep(d);
        }
    }
    // The server answered it: a clock-skew re-stamp of a later write of the
    // match is never stamped below it (_afterSettledStamp). Not a discard, and
    // not a write dropped after a second clock refusal: the server never
    // ordered those, and that stamp is in its future, so it would floor every
    // later re-stamp of the match into the future too.
    if (outcome && 'value' in outcome && !_unorderedValue(outcome.value)) {
        const base = _baseOf(key);
        const s = _entryStamp(d);
        if (s > (_lastSettledStamp.get(base) || 0)) _lastSettledStamp.set(base, s);
    }
    // Every entry that leaves settles its door's promise: with the answer the
    // flush passes, else as discarded (a discard, another tab sending it).
    _settleKey(key, outcome || { value: { discarded: true } });
    // Inside a batch the follow-ups below are done once for the batch (K6).
    if (_dequeueBatch) {
        if (d.kind === 'evt') _dequeueBatch.bases.add(_baseOf(key));
        _dequeueBatch.keys.add(key);
        return true;
    }
    if (d.kind === 'evt') _publishPendingTaps(_baseOf(key));
    _kickFollowersOf(new Set([key]));
    return true;
}

// While many entries leave at once (a tap batch settling, _sendTapBatch;
// another tab's removals, _onStoredQueueChanged), the follow-ups each _dequeue
// would do over the whole queue (publishing its match's pending taps, kicking a
// write that follows it) are gathered here and done once, so a batch costs
// one walk of the queue, not one per entry (stage 3 review 7, K6).
let _dequeueBatch = null;

function _openDequeueBatch() {
    if (!_dequeueBatch) _dequeueBatch = { bases: new Set(), keys: new Set() };
}

function _closeDequeueBatch() {
    const b = _dequeueBatch;
    _dequeueBatch = null;
    if (!b) return;
    for (const base of b.bases) _publishPendingTaps(base);
    if (b.keys.size > 0) _kickFollowersOf(b.keys);
}

// _kickFollowersOf: an entry another match's write follows (`after`) has
// left: that write goes now, in this flush (a rerun) or in one kicked for it.
function _kickFollowersOf(keys) {
    for (const e of _writeQueue.values()) {
        if (_afterKeys(e).some((k) => keys.has(k))) {
            _rerunFlushSoon();
            return;
        }
    }
}

// What storage holds now, keyed. Unreadable storage reads as empty, so the
// persist writes this tab's queue alone, which is what it always did; the
// rehydrate is where a corrupt queue is counted and reported. Items without a
// key are left out; every other stored descriptor is kept verbatim for the
// next rehydrate to judge.
function _storedQueue() {
    try {
        return _parseStoredQueue(localStorage.getItem(QUEUE_STORAGE_KEY));
    } catch (_e) {
        return new Map();
    }
}

// The stored queue's serialized form (a `storage` event carries it as a
// string), keyed, with the same leniency as _storedQueue.
function _parseStoredQueue(raw) {
    try {
        const entries = raw ? JSON.parse(raw) : [];
        if (!Array.isArray(entries)) return new Map();
        return new Map(entries.filter((e) => Array.isArray(e) && e.length >= 2 && e[0]));
    } catch (_e) {
        return new Map();
    }
}

// _enqueuedAfter: was queue entry `d` queued after time `at`? The shared
// outbox's one "which write is newer" test. An entry with no numeric stamp is
// never the newer one.
function _enqueuedAfter(d, at) {
    return !!d && typeof d.enqueuedAt === 'number' && typeof at === 'number' && d.enqueuedAt > at;
}

// ---------------------------------------------------------------------------
// bc-mrgc phase 3: per match, an ordered list of pending writes.
// ---------------------------------------------------------------------------
// The separator between an entry's base (the match it is about) and its own
// id. A control character, so no competition, match, team or round id can
// contain it, and a key persisted by an earlier build (no separator) is its
// own base.
const QUEUE_ENTRY_SEP = '\u001f';
let _entrySeq = 0;

// _baseOf: the match (or lineup, or override target) a queue key is about.
function _baseOf(key) {
    const i = typeof key === 'string' ? key.indexOf(QUEUE_ENTRY_SEP) : -1;
    return i < 0 ? key : key.slice(0, i);
}


// _poolRanksBase: the queue base of a competition's pool-rank writes (a
// chusen's order, overridePoolRanks; stage 3 review 11, Y7), so they are sent
// strictly in the order they were made. They belong to no match.
function _poolRanksBase(compID) {
    return `pool-ranks:${compID}`;
}

// _newEntryKey: a fresh key for one more write about `base`. Unique across
// tabs (the stored outbox is shared) and across reloads.
function _newEntryKey(base) {
    _entrySeq++;
    return `${base}${QUEUE_ENTRY_SEP}${Date.now().toString(36)}.${_entrySeq.toString(36)}.${Math.random().toString(36).slice(2, 8)}`;
}

// _entryStamp: the order an entry replays in: the stamp its payload carries
// (when the operator made the change, server-relative), or, for a write with
// none (a lineup), when it was queued.
function _entryStamp(d) {
    const p = d && d.payload;
    const s = Number(p && p.modifiedAt);
    if (Number.isFinite(s) && s > 0) return s;
    // A tap carries its stamp as `stamp`; a door whose body has no stamp of
    // its own (reopen, requeue, send back, a kachinuki bout removal) keeps its
    // place in its match's order on the descriptor, in server time like the
    // rest, never the local wall clock.
    const t = Number(p && p.stamp);
    if (Number.isFinite(t) && t > 0) return t;
    const o = Number(d && d.sortStamp);
    if (Number.isFinite(o) && o > 0) return o;
    return Number(d && d.enqueuedAt) || 0;
}

// _byEntryOrder: the ONE replay order. By stamp, then by when it was queued,
// then by key, so it is total and the same in every tab.
function _byEntryOrder([ka, a], [kb, b]) {
    return (_entryStamp(a) - _entryStamp(b))
        || ((Number(a && a.enqueuedAt) || 0) - (Number(b && b.enqueuedAt) || 0))
        || (ka < kb ? -1 : ka > kb ? 1 : 0);
}

// _entriesFor: a base's pending writes, in replay order.
function _entriesFor(base) {
    const out = [];
    for (const [k, d] of _writeQueue) {
        if (d && _baseOf(k) === base) out.push([k, d]);
    }
    return out.sort(_byEntryOrder);
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
function _ownStatusMove(compID, matchID) {
    const base = _revKey(compID, matchID);
    let last = null;
    for (const [, d] of _entriesFor(base)) {
        const status = _statusMoveOf(d);
        if (status) last = { status, kind: d.kind || 'score', payload: d.payload, landed: false, stamp: _entryStamp(d) };
    }
    if (last) return last.status === 'running' ? null : last;
    const answered = _ownStatusAnswered.get(base);
    return answered ? { ...answered, landed: true } : null;
}

// _noteOwnStatusAnswer: an entry left the queue (`outcome`, as _dequeue has
// it). A status move the server took is kept as answered; one it refused, or
// that was discarded or sent by another tab, keeps nothing.
function _noteOwnStatusAnswer(key, d, outcome) {
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
    _ownStatusAnswered.set(base, {
        status, kind: d.kind || 'score', payload: d.payload,
        stamp: Number.isFinite(answered) && answered > 0 ? answered : _entryStamp(d),
    });
}

// _publishOwnStatus: tells the listeners when any match's own status move
// changed (from _recomputeSyncStatus, which every queue change reaches).
function _publishOwnStatus() {
    if (_ownStatusListeners.size === 0) return;
    const parts = [];
    for (const [k, d] of _writeQueue) {
        const status = _statusMoveOf(d);
        if (status) parts.push(`${k}=${status}`);
    }
    for (const [base, a] of _ownStatusAnswered) parts.push(`${base}~${a.status}@${a.stamp}`);
    const sig = parts.sort().join('|');
    if (sig === _ownStatusSig) return;
    _ownStatusSig = sig;
    for (const fn of _ownStatusListeners) {
        try { fn(); } catch (_e) { /* swallow */ }
    }
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

function _emitLocalMatchPatch(compID, matchID, fields) {
    if (!compID || !matchID || !fields || Object.keys(fields).length === 0) return;
    const event = { type: 'match_updated', local: true, data: { competitionId: compID, matchId: matchID, result: { id: matchID, ...fields } } };
    for (const cb of _localPatchListeners) {
        try { cb(event); } catch (err) { console.error('local match patch listener failed:', err); }
    }
}

// The court and time entries still queued for a match hold, the later winning.
function _pendingMatchFields(compID, matchID) {
    const out = {};
    for (const [, d] of _entriesFor(_revKey(compID, matchID))) {
        const f = _LOCAL_FIELD[d.kind];
        if (f && d.payload && f in d.payload) out[f] = d.payload[f];
    }
    return out;
}

// An entry left with a refusal or an operator's discard: the match shows what
// it had, unless a later entry of the same field is still queued.
function _revertLocalFields(d) {
    const f = d && _LOCAL_FIELD[d.kind];
    if (!f || !d.was || typeof d.was !== 'object' || !(f in d.was)) return;
    const still = _pendingMatchFields(d.compID, d.matchID);
    _emitLocalMatchPatch(d.compID, d.matchID, { [f]: f in still ? still[f] : d.was[f] });
}

// _overlayPendingFields: a fetched competition (a list item or a detail) with
// the court and time this device still has queued for its matches put on.
function _overlayPendingFields(comp, compID) {
    const id = compID || (comp && (comp.id || (comp.config && comp.config.id)));
    if (!comp || !id) return comp;
    // Every fetch of a competition passes here: a tap of this device it turned
    // away since its answer is told (_tellTurnedAway), after a reload too.
    _tellTurnedAwayIn(comp, id);
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
// bc-evts stage 3: the outbox. EVERY rule-bound write (a tap, a Finish, a
// decision, a reopen, a send back, override-winner, the representative bout's
// add and remove, a kachinuki bout removal) is put in the persisted queue in
// the SAME TICK it is made and the caller carries on at once: nothing in front
// of an editor waits on the network (operator rule, 2026-10-16). Each match's
// entries are then sent strictly in order (_flushQueue, F1), so a write never
// overtakes one made before it on this device.
// ---------------------------------------------------------------------------

function _randomId() {
    if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID();
    return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}
// The device's EVENT session: random per page load. Not _revSession, which
// orders running snapshot writes: a tap's id is `${session}.${seq}` and the
// server counts the session's seqs to report the taps it never got (gaps).
const _eventSession = _randomId();
const TAP_BATCH_MAX = 64;
const BASIS_OWN_MAX = 1024;
const WRITE_ID_MAX = 128;
let _writeIdSeq = 0;
// A microtask flush is already kicked for the taps of this tick.
let _tapFlushKicked = false;
// Per match (base): the last seq this session queued, and the last stamp this
// device gave any write of that match, so a later write is never stamped
// before an earlier one (the replay orders by stamp).
const _tapSeq = new Map();
const _lastStampFor = new Map();
// Per match: the stamp of the newest write of this device the server
// answered (an entry that left the queue with an answer). A clock-skew
// re-stamp is never below it (_afterSettledStamp).
const _lastSettledStamp = new Map();
// Per match: the newest derived match a score-events answer handed back (the
// broadcast copy, with markIds and scoreVersion).
const _latestDerived = new Map();
const _derivedListeners = new Map();
const _pendingTapListeners = new Map();
const _tapOutcomeListeners = new Map();
// The pending taps the board already told as not applied (tellPendingTapRefused),
// whose answer then tells nothing more: one notice per tap.
const _toldOnBoard = new Set();
// Per queue key: the promise a door handed out and the refusal claims of the
// callers. In memory only (never persisted): a function does not survive a
// reload, and a rehydrated entry has nobody waiting on it.
const _entryHooks = new Map();

function _newWriteId() {
    _writeIdSeq++;
    return `${_eventSession}.w${_writeIdSeq.toString(36)}`.slice(0, WRITE_ID_MAX);
}

// _afterOwnStamp: this device's next stamp for the match, never at or before
// the last one it gave a write of that match.
function _afterOwnStamp(base, stamp) {
    const last = _lastStampFor.get(base) || 0;
    const s = stamp > last ? stamp : last + 1;
    _lastStampFor.set(base, s);
    return s;
}

// _noteOwnStamp: a write of this device for the match that this page did not
// stamp (left queued by a previous page, or taken up from another tab) still
// counts as one it gave: a later write is stamped after it (M1). The clock
// offset is not stored, so after a reload the frame can read a little behind
// the one the stored write was stamped in.
function _noteOwnStamp(key, d) {
    const s = _entryStamp(d);
    if (!(s > 0)) return;
    const base = _baseOf(key);
    if (s > (_lastStampFor.get(base) || 0)) _lastStampFor.set(base, s);
}

// _afterSettledStamp: the stamp a clock-skew re-stamp gives a queued write,
// never at or before a write of its match the server already answered, so a
// re-stamped write never lands before one made before it. Floored by what
// LANDED, not by _lastStampFor: that holds the later writes queued behind this
// one too, and the skewed stamp itself, and a re-stamp above them would move
// this write behind them, or back into the future. The later writes are still
// counted from it.
function _afterSettledStamp(base, stamp) {
    const settled = _lastSettledStamp.get(base) || 0;
    const s = stamp > settled ? stamp : settled + 1;
    if (s > (_lastStampFor.get(base) || 0)) _lastStampFor.set(base, s);
    return s;
}

function _listenOn(map, base, fn) {
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

// The taps this device holds for a match, in the order they are sent.
function _tapEntriesFor(base) {
    return _entriesFor(base).filter(([, d]) => d.kind === 'evt');
}
function _pendingTapsFor(base) {
    // A parked tap (the server refused its batch) is not counted on the board
    // or the TV: it is held until an operator discards it, and the server
    // will never apply it as it stands.
    return _tapEntriesFor(base).filter(([, d]) => !d.parked).map(([, d]) => ({ ...d.payload }));
}
function _publishPendingTaps(base) {
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
const _landed = createLandedTaps({ storage: _landedStorage, tab: _randomId() });
// _landedBeat keeps this tab alive in the record, and tells the taps it may
// tell now from the newest copy of each match it has seen: a tab that died
// without its pagehide holds its taps until its heartbeat is old, and the
// copy that listed them may have come meanwhile, with no later one to come
// (stage 6 review 8, m11).
function _landedBeat() {
    _landed.beat();
    for (const seen of _turnedAwaySeen.values()) {
        if (seen && seen.away && seen.compID && seen.matchID) _tellAway(seen.compID, seen.matchID, seen.away);
    }
}
if (typeof setInterval === 'function') {
    const beat = setInterval(_landedBeat, LANDED_TAB_BEAT_MS);
    if (beat && typeof beat.unref === 'function') beat.unref();
}
if (typeof window !== 'undefined' && typeof window.addEventListener === 'function') {
    window.addEventListener('pagehide', () => _landed.leave());
}
// _forgetLandedTaps: on a sign-out the record goes with the operator who made
// the taps, so the next operator is never told of them: this tab's entries and
// those of tabs no longer alive, never another live tab's (stage 6 review 8,
// m10). A revoked password's park keeps it: the same operator signs in again.
function _forgetLandedTaps() {
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
// The latest `turnedAway` seen on any copy of each match, by base, so a tap
// whose own applied answer arrives AFTER the copy listing it (the finish's
// push overtook the answer) is told as it lands, from _noteLandedTaps, not
// left until the next copy of the match (stage 6 review 6). A copy with a
// lower scoreVersion than the one kept (an answer the server made before
// the finish) does not replace it. Forgotten with the shown matches.
const _turnedAwaySeen = new Map();
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
// the tap, then forgotten. Accepted: two tabs of the device reading copies
// of the match at the same moment can both find the tap in the shared
// landed record before either removes it, and both tell it.
function _tellTurnedAway(compID, matchID, match) {
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
function _tellTurnedAwayIn(comp, compID) {
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

// _openSettlement: the `settled` promise of a queue key. A write that takes
// the place of a queued one (_placeWrite reuses its key) shares its promise,
// so the first caller learns the answer of the write that replaced its own.
function _openSettlement(key, onRefusal) {
    let h = _entryHooks.get(key);
    if (!h) {
        let resolve, reject;
        const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
        // A caller that never awaits it must not raise an unhandled rejection;
        // one that does still receives the rejection.
        promise.catch(() => {});
        h = { promise, resolve, reject, claims: [] };
        _entryHooks.set(key, h);
    }
    if (typeof onRefusal === 'function') h.claims.push(onRefusal);
    return h.promise;
}
function _settleKey(key, outcome) {
    const h = _entryHooks.get(key);
    if (!h) return;
    _entryHooks.delete(key);
    if (outcome && 'error' in outcome) h.reject(outcome.error);
    else h.resolve(outcome ? outcome.value : undefined);
}
// _claimRefusal: does a caller take this refusal on itself (onRefusal
// returned true)? Then the flush raises no alert for it.
function _claimRefusal(key, err) {
    const h = _entryHooks.get(key);
    if (!h) return false;
    let claimed = false;
    for (const fn of h.claims) {
        // A claim that throws has not shown the refusal: the outbox says it.
        try { if (fn(err) === true) claimed = true; } catch (e) { console.error(`[sync] a refusal handler for ${key} threw:`, e); }
    }
    return claimed;
}

// The pending answer every outbox door gives: the write is the outbox's now.
function _pendingAnswer(key, settled) {
    const answer = { applied: true, pending: true, key, settled };
    if (!_lastEnqueuePersisted) answer.persisted = false;
    return answer;
}

// _outboxWrite: the ONE way a rule-bound door hands a write over. Builds the
// descriptor synchronously, tags it (writeId, basis, session), stamps its
// place in the match's order, enqueues it durably and kicks the flush.
function _outboxWrite({ kind, compID, matchID, method, url, payload, password, seen, onRefusal, terminal = true, after, base: ownBase, holds, was }) {
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
    // `was`: what a court move or time change replaces, put back on a refusal
    // (R4, _revertLocalFields). Persisted.
    if (was && typeof was === 'object') descriptor.was = was;
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
    let settled = null;
    const key = _commitEnqueue(base, descriptor, (k) => { settled = _openSettlement(k, onRefusal); });
    return _pendingAnswer(key, settled);
}

// _lastEntryKey: the queue key of a base's last entry in replay order, or ''.
function _lastEntryKey(base) {
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
function _isFollowed(key) {
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
// entry say what both changed. A decision or an override is never replaced,
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
function _migratedLegacyLineup(key, descriptor) {
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
// _placeWrite), since a lineup PUT carries no stamp the server could order two
// saves by. It is what holds back removing the lineup, which the queued save
// would undo, and what keeps a read from putting the old lineup back over the
// pick the sheet shows.
function _queuedLineupSave(compID, teamId, { matchId, round } = {}) {
    const probe = { payload: { teamId, ...(matchId ? { matchId } : { round }) } };
    // A match's lineup is an entry of its match (bc-evts M5; one an older
    // build queued under its own base is moved there as it loads, R9).
    const bases = matchId
        ? [_revKey(compID, matchId)]
        : [_lineupKey(compID, teamId, { round })];
    return bases.some((b) => _entriesFor(b).some(([, d]) => _sameLineup(d, probe)));
}

// _lineupKey: the queue BASE of a lineup: a match's own lineup (`matchId`), or a
// team's Lineups-page round (round 0 is its starting lineup). The one owner of
// the key: both lineup saves and queuedLineupSave name a lineup by it.
function _lineupKey(compID, teamId, { matchId, round }) {
    return matchId ? `lineup:${compID}:${teamId}:match:${matchId}` : `lineup:${compID}:${teamId}:${round}`;
}

// _storedEntryProblem: why a stored queue entry cannot be replayed
// ('unreadable'), or null when it can. The checks a rehydrate applies, stated
// once so taking up another tab's entries (_onStoredQueueChanged) applies them
// too. An entry is never refused for its age (bc-mrgc phase 3): the server
// orders it by its stamp and keeps what a newer change outranks in the
// match's history, so sending it loses nothing and discarding it would.
function _storedEntryProblem(descriptor) {
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

// _parkStoredEntries: blank the password on every STORED entry and park it
// (authBlocked), another tab's included, for a revoked credential
// (parkQueueForReauth). The merge keeps what this tab did not write, so
// persisting this tab's scrubbed queue alone would leave the revoked
// plaintext password in storage under the other tab's entries.
function _parkStoredEntries() {
    // What an older build still holds under the old key carries the revoked
    // password too: taken over first, so it is parked with the rest (F4).
    try { _takeOverLegacyQueue(localStorage.getItem(LEGACY_QUEUE_STORAGE_KEY)); } catch (_e) { /* unreadable storage */ }
    const stored = _storedQueue();
    if (stored.size === 0) return;
    for (const d of stored.values()) {
        if (!d || typeof d !== 'object') continue;
        d.password = '';
        d.authBlocked = true;
    }
    try {
        localStorage.setItem(QUEUE_STORAGE_KEY, JSON.stringify([...stored.entries()]));
    } catch (_e) { /* unwritable storage: the persist that follows reports it */ }
}
let _syncStatus = /** @type {SyncStatusValue} */ ('synced');
const _syncListeners = new Set();

// bc-evts: the queued entries being sent right now (_sendEntry). Every write is
// an outbox entry, stored before it is sent and leaving storage only once it
// settles, so a page going away mid-send leaves it for the next load; this set
// only stops a later write taking the place of one already out
// (_coalesceTarget). Never persisted.
const _inFlight = new WeakSet();
// Set to true ONLY when a flush attempt ends with at least one true NETWORK
// failure (fetch rejected: connection down). A non-2xx server response (e.g. a
// transient 5xx/429) keeps the write queued for retry but is NOT "offline": the
// network is up, so the pill stays "Syncing…" rather than "Offline". Cleared
// back to false when a flush drains the queue or no network failure occurs.
let _offlineFlag = false;

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
function subscribeTerminalWriteFailed(fn) {
    _terminalFailListeners.add(fn);
    return () => _terminalFailListeners.delete(fn);
}
function _notifyTerminalWriteFailed(info) {
    for (const fn of _terminalFailListeners) {
        try { fn(info); } catch (_e) { /* swallow */ }
    }
}

// The "did this write land?" predicates live in write_result.jsx, a leaf with
// no imports, so that admin_scoring_shared.jsx can import the same rule without
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
// court. NOT _notifyBracketResync, whose whole point is that an override
// supersede is BENIGN: an override is an assertion about a feeder the server may
// safely ignore, whereas this is the operator's own entered result being dropped.
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
function _notifyScoreSupersededEditor(compID, matchID, body) {
    // Copy owned by write_result.jsx (supersededBanner): the explicit-tap
    // call sites that submit status:"running" build this same banner state
    // directly from the awaited result (via notLandedBanner), since this
    // broadcast is deliberately silent for that status. Same strings, one
    // owner, so a future wording change cannot paste the copy anywhere a
    // fourth time. `body` is the server's answer: a change turned away
    // because the match was finished gets its own advice (writeMatchClosed).
    _notifyTerminalWriteFailed({ compID, matchID, kind: 'score', status: 200, ...supersededBanner(body) });
}

// The ALERT half: reaches an operator who has already moved on to another
// court. Split from the editor half so a flush pass can emit ONE alert carrying
// a real count, the way the 'unreadable' alert already does.
// Publishing count:1 per drop made queueAlertMessage's plural branch dead code
// and left a court that reconnected with several dropped results looking at a
// single toast reading "A result was not saved".
function _notifyScoreSupersededAlert(count, compID, matchID, matchClosed = false) {
    if (count <= 0) return;
    _notifyQueueAlert({
        kind: 'superseded', count, terminalCount: count, compID, matchID,
        ...(matchClosed ? { matchClosed: true } : {}),
    });
}

// _heldForClosedMatch: the answer turned this write's own change away because
// the match was finished, or because the next knockout round depends on its
// result. One remedy either way: Correct.
function _heldForClosedMatch(body) {
    return writeMatchClosed(body) || writeResultLocked(body) !== null;
}

function _notifyScoreSuperseded(compID, matchID, body) {
    _notifyScoreSupersededEditor(compID, matchID, body);
    _notifyScoreSupersededAlert(1, compID, matchID, _heldForClosedMatch(body));
}

// Bracket-resync channel. When a queued override-winner assertion the server
// LWW-dropped returns 200 {"applied": false} and emits NO SSE broadcast, any
// optimistic local bracket advance from it is stale. This channel asks listeners
// (e.g. AdminShiaijo) to refetch. This is a BENIGN supersede: do NOT reuse
// _terminalFailListeners, which surfaces a "not saved" ERROR state. Payload:
// {compID, matchID, reason}.
const _bracketResyncListeners = new Set();
function subscribeBracketResync(fn) {
    _bracketResyncListeners.add(fn);
    return () => _bracketResyncListeners.delete(fn);
}
function _notifyBracketResync(info) {
    for (const fn of _bracketResyncListeners) {
        try { fn(info); } catch (_e) { /* swallow */ }
    }
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
function _keepsFailing(d) {
    // A parked batch of taps the server refused (400) can only go by an
    // operator's discard, so it reads as failing at once; so does an entry on
    // a wait cycle (`cycled`, _markWaitCycles), which nothing else can free.
    return _refusedByServer(d) || (!!d && !!d.cycled);
}

// _refusedByServer: the entry is held because the SERVER keeps refusing it
// (a parked tap batch, or past the notice threshold), not because of what
// it waits on.
function _refusedByServer(d) {
    return !!d && (!!d.parked || (Number(d.attempts) || 0) >= SERVER_REJECTION_NOTICE_THRESHOLD);
}

// _heldWriteItem: one held write as API.heldWrites() lists it.
function _heldWriteItem(key, d) {
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
    };
}

// What this device has shown of a match or a team, so a held write can be
// named by where it is (stage 3 review 5, H5): a match's label as the score
// list labels it (scoreRowMatchLabel), and each side's name by its id, which
// names a team's member and lineup writes. Learned from the match a door is
// handed (recordScore, recordTap, requeueBlockerAndReopen) and from the match
// an editor shows (API.noteShownMatch), and stamped on an entry as it is
// queued (`label`, persisted), so a reload still names it.
const _shownMatchLabels = new Map();
const _shownTeamNames = new Map();

function _noteShownMatch(compID, m) {
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
function _forgetShownMatches(compID) {
    if (!compID) return;
    const prefix = `${compID}:`;
    for (const k of _shownMatchLabels.keys()) if (k.startsWith(prefix)) _shownMatchLabels.delete(k);
    // The copies score-events answers handed back for its matches go too
    // (X4): a match id reused by the new draw must never show the old match's
    // scoreline on a board, nor have it recorded by a Finish.
    for (const k of _latestDerived.keys()) if (k.startsWith(prefix)) _latestDerived.delete(k);
    for (const k of _turnedAwaySeen.keys()) if (k.startsWith(prefix)) _turnedAwaySeen.delete(k);
}

// _forgetAllShownMatches: the tournament's competitions may have been replaced
// (an import) or the client was told of a reset, so no learned match label is
// trusted (stage 3 review 7, K8). Another device's import is heard only as a
// `tournament_updated` push, which also follows other tournament-wide changes:
// a label is learned again as soon as its match is shown, and an entry keeps
// the label it was queued with.
function _forgetAllShownMatches() {
    _shownMatchLabels.clear();
    _latestDerived.clear();
    _turnedAwaySeen.clear();
}

function _noteMatchLabel(compID, matchID, label) {
    if (compID && matchID && typeof label === 'string' && label.trim()) _shownMatchLabels.set(_revKey(compID, matchID), label.trim());
}

// _teamIdOf: the team a team write is about (a member write's url, a lineup
// save's payload).
function _teamIdOf(d) {
    if (d && d.payload && typeof d.payload.teamId === 'string' && d.payload.teamId) return d.payload.teamId;
    const m = String((d && d.url) || '').match(/\/teams\/([^/]+)\//);
    return m ? decodeURIComponent(m[1]) : '';
}

// _whereOf: where one queued write is, in the words above, or ''.
function _whereOf(d) {
    if (!d) return '';
    if (typeof d.label === 'string' && d.label) return d.label;
    if (d.matchID) return _shownMatchLabels.get(_revKey(d.compID, d.matchID)) || '';
    const teamId = _teamIdOf(d);
    return teamId ? (_shownTeamNames.get(`${d.compID}:${teamId}`) || '') : '';
}
// The one discard of a held write (API.discardHeldWrite and the editors'
// API.discardFailingHeldWrites): only a write the server keeps refusing.
function _discardIfFailing(key, d) {
    return _keepsFailing(d) && _dequeue(key, d, { value: { discarded: true }, byOperator: true });
}
function _afterDiscard() {
    _persistQueue();
    _recomputeSyncStatus();
    // A discarded write may have been holding back its match's later writes
    // (strict order, F1): send them now rather than on an unrelated trigger.
    if (_writeQueue.size > 0) {
        if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
        _flushAttempt = 0;
        _flushQueue();
    }
}

/**
 * Recompute and publish the correct sync status from current state:
 *   offline       : _offlineFlag is set and the queue still has entries
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
function _recomputeSyncStatus() {
    _setSyncStatus(_deriveSyncStatus());
    _publishUnsentWrites();
    _publishOwnStatus();
}
function _deriveSyncStatus() {
    if (_offlineFlag && _writeQueue.size > 0) return 'offline';
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
function _unsentCounts() {
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
function subscribeUnsentWrites(fn) {
    _unsentListeners.add(fn);
    try { fn(_unsentCounts()); } catch (_e) { /* swallow */ }
    return () => _unsentListeners.delete(fn);
}

/**
 * Subscribe to sync-status changes. Returns an unsubscribe function.
 * @param {(status: SyncStatusValue) => void} fn
 * @returns {() => void}
 */
function subscribeSyncStatus(fn) {
    _syncListeners.add(fn);
    // Replay current status so the subscriber can initialise its state.
    try { fn(_syncStatus); } catch (_e) { /* swallow */ }
    return () => _syncListeners.delete(fn);
}

let _flushTimer = null;
const FLUSH_BACKOFF_MS = [500, 1000, 2000, 4000, 8000];
let _flushAttempt = 0;
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
const SERVER_REJECTION_NOTICE_THRESHOLD = 10;
// Single-in-flight guard. _flushQueue's body awaits network I/O, so without a
// lock a second trigger (a rapid enqueue, an `online` event, or a backoff timer
// firing mid-flush) would start an OVERLAPPING loop iterating the same snapshot:
// duplicate PUTs and corrupted backoff state. Instead, a trigger that arrives
// while a flush is running sets _flushRequested; the running loop reruns once it
// finishes so newly-queued or still-failed writes are retried without overlap.
let _flushInProgress = false;
let _flushRequested = false;
// Bumped by clearQueue() (logout / password_reset). An in-flight _flushQueue loop
// snapshots this at the start and aborts if it changes mid-loop, so a credential
// revocation can't keep sending queued writes with the now-revoked password.
let _queueGen = 0;

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
async function _restampQueuedEntryForSkew(descriptor) {
    await _learnServerClockOffset();
    const enqueuedAt = Number.isFinite(descriptor.enqueuedAt) ? descriptor.enqueuedAt : Date.now();
    descriptor.payload.modifiedAt = _afterSettledStamp(_revKey(descriptor.compID, descriptor.matchID),
        _stampAfterSeen(_restampFor(enqueuedAt, descriptor.perfAtEnqueue), descriptor.seenModifiedAt));
    descriptor.skewRetried = true;
    _persistQueue();
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
function _restampFor(enqueuedAt, perfAtEnqueue) {
    const wall = enqueuedAt + serverClockOffsetMs();
    const ageMs = _perfAgeMs(perfAtEnqueue);
    // A negative age is not a clock step, it is two readings from different time
    // origins (a rehydrated entry that predates the strip, a hand-edited store).
    // There is no true age to recover, so fall back rather than invent one.
    if (ageMs === null || !(ageMs >= 0)) return wall;
    return Math.min(wall, _serverNowMs() - ageMs);
}

async function _flushQueue() {
    if (_flushInProgress) { _flushRequested = true; return; }
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
        anyFailed: false, networkFailed: false,
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
            const gen = _queueGen; // clearQueue() bumps this to cancel an in-flight flush
            // bc-evts F1 (reversing bc-mrgc C2): per match (base), in the ONE
            // replay order, _byEntryOrder, and strictly in order: the first entry
            // of a match that does not settle in this pass (offline, a 5xx,
            // parked, held by an open fetch, re-stamped for a clock refusal) ends
            // THAT match's turn; the other matches go on. A write made after
            // another on this device never reaches the server before it.
            const byBase = new Map();
            for (const entry of [..._writeQueue.entries()].sort(_byEntryOrder)) {
                const base = _baseOf(entry[0]);
                if (!byBase.has(base)) byBase.set(base, []);
                byBase.get(base).push(entry);
            }
            // Entries that wait on each other are stuck, not waiting (H1).
            _markWaitCycles(byBase);
            for (const [base, list] of byBase) {
                // If clearQueue() ran during a prior await (logout / password_reset →
                // credential revocation), abort before sending anything else with the
                // now-revoked password/header.
                if (gen !== _queueGen) break;
                await _flushBase(base, list, pass, gen);
            }
            _persistQueue();
            if (_writeQueue.size === 0) {
                _flushAttempt = 0;
                _offlineFlag = false;
                _recomputeSyncStatus();
            } else if (!pass.anyFailed) {
                // bc-qttl: entries remain but nothing failed this pass, which is the
                // all-parked case (a parked write is skipped, so it can neither
                // succeed nor fail). Republish so the pill leaves "Syncing…" for the
                // parked state, and schedule NO backoff timer: a parked write cannot
                // progress until resumeAfterAuth() or a discard.
                _offlineFlag = false;
                _recomputeSyncStatus();
            } else {
                _offlineFlag = pass.networkFailed;
                _recomputeSyncStatus();
                if (!_flushRequested) {
                    // Schedule a backoff retry only if no immediate rerun is already
                    // pending (a pending rerun re-attempts the failed writes now).
                    const delay = FLUSH_BACKOFF_MS[Math.min(_flushAttempt, FLUSH_BACKOFF_MS.length - 1)];
                    _flushAttempt++;
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
function _outboxIdle() {
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
    if (outcome === 'settled') return;
    let marked = false;
    for (const [key, d] of list) {
        if (_writeQueue.get(key) === d && !d.held && outcome === 'kept') { d.held = true; marked = true; }
    }
    // An entry of another base waiting on one just marked held is held with
    // it (F6): its own base may have had its turn earlier in this pass.
    if (marked && _markWaitersHeld()) marked = true;
    if (marked) _recomputeSyncStatus();
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
// has held it), parked, or waiting on a password.
function _isStuckEntry(d) {
    return !!d && (!!d.parked || !!d.authBlocked || !!d.cycled || (!!d.held && (Number(d.attempts) || 0) > 0));
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
function _afterKeys(d) {
    if (!d) return [];
    if (Array.isArray(d.after)) return d.after.filter((k) => typeof k === 'string' && k);
    return typeof d.after === 'string' && d.after ? [d.after] : [];
}

// _addAfter: makes the entry follow `key` as well as what it already follows.
function _addAfter(d, key) {
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
function _blockerOf(key, stuck, walk = _newWalk(), index = _queueIndex()) {
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
function _queueIndex() {
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
function _newWalk() {
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
        if (!d.held && blockedBases.has(_baseOf(key))) { d.held = true; marked = true; }
    }
    return marked;
}

// The value a door's `settled` resolves with: the shape the direct call used
// to return.
function _doorValue(d, body) {
    switch (d && d.kind) {
        case 'override':
            return {
                applied: !!body && body.applied !== false, reason: body && body.reason, message: body && body.message,
                ...(body && Array.isArray(body.heldGroups) ? { heldGroups: body.heldGroups } : {}),
            };
        case 'revert': return true;
        case 'lineup-remove': return true;
        case 'kachinuki': return (body && body.result) ?? body;
        case 'daihyosen': return _daihyosenOutcome(body || {});
        default: return body;
    }
}

// The Error a door's `settled` rejects with: the one the direct call threw.
function _doorRefusal(d, status, body) {
    const b = body || {};
    const ineligible = b.error === 'ineligible_competitor' || b.error === 'already_ineligible';
    switch (d && d.kind) {
        case 'score':
            if (ineligible) return new Error(b.reasonHuman || b.reason || b.error || 'Failed to record score');
            return _courtBusyError(b) || _downstreamRefusalError(b) || new Error(_refusalText(b, 'Failed to record score'));
        case 'decision':
            if (ineligible) return new Error(b.reasonHuman || b.reason || b.error || 'Failed to record decision');
            return _downstreamRefusalError(b) || new Error(b.error || 'Failed to record decision');
        case 'override':
            return _downstreamRefusalError(b) || new Error(b.error || 'Failed to override winner');
        case 'reopen':
        case 'requeue':
            return _reopenRefusal(b);
        case 'revert':
            return new Error(b.error || 'Failed to send match back to queue');
        case 'kachinuki':
            return new Error(b.error || 'Failed to remove bout');
        case 'daihyosen':
            return new Error(_refusalText(b, d.method === 'DELETE' ? REP_BOUT_NOT_REMOVED : REP_BOUT_NOT_ADDED));
        case 'lineup':
            return _refusalError(b, d.payload && d.payload.matchId ? 'Failed to save match lineup' : 'Failed to save lineup');
        case 'court':
            return new Error(b.error || 'Failed to move match court');
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
        default:
            return new Error(_refusalText(b, `HTTP ${status}`));
    }
}

// _sendEntry: one queued write (not a tap). 'settled' when it left the queue
// (sent, or refused for good), 'restamped' when it was re-stamped for a clock
// refusal and goes again at once, 'kept' when it waits for a later pass.
// _answersMatchWrite: whether an entry sent with its own method and url is a
// MATCH write, whose answer can be a refusal body (`applied: false`, a clock
// refusal) read by the branch it gates in _sendEntry. A lineup save, a team
// member write, a lineup removal and a reinstatement answer their own records,
// never a match write's refusal shape.
const _NOT_MATCH_WRITE_KINDS = new Set(['lineup', 'lineup-remove', 'member', 'reinstate', 'pool-ranks']);
function _answersMatchWrite(d) {
    return !!d && !!d.terminal && !_NOT_MATCH_WRITE_KINDS.has(d.kind);
}

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
            _notifyTerminalWriteFailed({ compID, matchID, kind, status: 0, reason: 'corrupted queue entry' });
            // Undeliverable by construction: announced globally as well, since
            // the editor-scoped channel only reaches an operator on this match.
            _notifyQueueAlert({ kind: 'unreadable', count: 1, terminalCount: isFinishedResultWrite(descriptor) ? 1 : 0, compID, matchID });
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
        _mintedOutHere.add(mintsId);
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
            // (_persistQueue never stores one).
            body: JSON.stringify(payload),
        }));
    } catch (err) {
        // fetch rejected (network down), or the request or its body was given
        // up at the deadline (_fetchJson).
        pass.anyFailed = true;
        pass.networkFailed = true;
        _inFlight.delete(descriptor);
        if (mintsId) _mintedOutHere.delete(mintsId);
        _mintedAddAnswered(key, descriptor, null, null, !!(err && err.timedOut));
        _endDiscardWhileOut(descriptor, null, null);
        return 'kept';
    } finally {
        _inFlight.delete(descriptor);
        if (mintsId) _mintedOutHere.delete(mintsId);
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
        // Success (HTTP 200/201, including a write the server kept in the
        // match's history instead of applying, applied:false).
        if (_answersMatchWrite(descriptor)) {
            if (writeWasRefusedForClock(body)) {
                // bc-cse: NOT a supersede. The stamp this replay carries is in
                // the server's future; nothing newer won. Heal the offset and
                // RECONSTRUCT the stamp, then retry once.
                if (descriptor.skewRetried) {
                    // Refused a second time on a stamp built from a freshly
                    // learned offset: this device's frame cannot be reconciled.
                    // Dropped loudly, on both channels.
                    console.warn(`[sync] dropping queued ${kind} write refused twice for clock skew (serverNowMs=${body.serverNowMs}); the device clock cannot be reconciled`);
                    _notifyTerminalWriteFailed({ compID, matchID, kind, status: 200, reason: CLOCK_SKEW_FAIL_REASON, advice: CLOCK_SKEW_UNHEALED_ADVICE });
                    _notifyQueueAlert({
                        kind: 'rejected', count: 1, terminalCount: isFinishedResultWrite(descriptor) ? 1 : 0, compID, matchID,
                        detail: CLOCK_SKEW_FAIL_REASON,
                    });
                    // An override additionally leaves an optimistic local advance
                    // standing on a write that will never land: ask for a refetch.
                    if (kind === 'override') _notifyBracketResync({ compID, matchID, reason: CLOCK_SKEW_REASON });
                    leaveQueue({ value: _doorValue(descriptor, body) });
                    return 'settled';
                }
                await _restampQueuedEntryForSkew(descriptor);
                // Left QUEUED, with an immediate rerun of the loop, so the
                // corrected write goes out now rather than on a backoff timer.
                _flushRequested = true;
                return 'restamped';
            }
            if (body.applied === false) {
                // bc-cse: our stamp lost either way, so re-check our clock frame.
                _relearnClockThrottled();
                if (_writeQueue.get(key) === descriptor) {
                    if (kind === 'override') {
                        // A feeder-winner ASSERTION the server held: any optimistic
                        // local advance from it is stale, so ask listeners to
                        // refetch; told in the pass's one superseded alert.
                        _notifyBracketResync({ compID, matchID, reason: 'lww_dropped' });
                    } else {
                        // bc-lww1: the operator's own entry, kept in the match's
                        // history, not applied. The editor-targeted half fires per
                        // match; the alert half is counted for the pass.
                        _notifyScoreSupersededEditor(compID, matchID, body);
                        if (_heldForClosedMatch(body)) pass.matchClosed = true;
                    }
                    pass.superseded++;
                    pass.lastSuperseded = { compID, matchID };
                }
            }
        } else if (!terminal && kind === 'score') {
            // bc-cse MEDIUM-2: a RUNNING write settles with the server's answer
            // (its editor reads `settled`) and raises no alert of its own,
            // except the clock-skew refusal, where res.ok means nothing was
            // written: it is re-stamped and resent in its place, since the
            // entry may hold an edge-triggered kachinukiBoutFinal.
            const refusedForClock = writeWasRefusedForClock(body);
            if (refusedForClock && !descriptor.skewRetried) {
                await _restampQueuedEntryForSkew(descriptor);
                _flushRequested = true;
                return 'restamped';
            }
            if (refusedForClock) {
                console.warn(`[sync] dropping queued running write for ${matchID} refused twice for clock skew (serverNowMs=${body.serverNowMs}); a pending kachinuki advancement may need re-pressing`);
            } else if (body.applied === false) {
                // Kept in the match's history, not applied: our stamp lost, so
                // re-check our clock frame. The editor that sent it reads the
                // answer from `settled`; no alert (an autosave's would repeat).
                _relearnClockThrottled();
            }
        }
        // A change turned away because the match was finished (writeMatchClosed)
        // is the operator's to correct, never routine noise, so it is said
        // whatever the write (a superseded Finish was counted above; never a
        // displaced answer: that finish was recorded).
        if (kind === 'score' && writeMatchClosed(body) && !(terminal && writeWasSuperseded(body))
            && _writeQueue.get(key) === descriptor) {
            _notifyScoreSupersededEditor(compID, matchID, body);
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
            if (terminal && kind === 'score') _matchRevCounters.delete(_revKey(compID, matchID));
        } else if (_writeQueue.has(key)) {
            // A later write took this one's place while it was out (the same
            // key, _placeWrite): it goes before anything queued after it.
            _flushRequested = true;
            return 'restamped';
        }
        return 'settled';
    }
    if (res.status === 404 && kind === 'lineup-remove') {
        // Nothing saved to remove: the outcome the removal asked for (R1).
        leaveQueue({ value: true });
        return 'settled';
    }
    if (res.status >= 500 || res.status === 429 || res.status === 403) {
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
    // itself; otherwise the editor-scoped channel and the global alert carry
    // it, with downstreamKnockoutPlayedQueueDrop's words for a knockout
    // correction whose later match was played meanwhile.
    console.warn(`[sync] queued ${kind || 'running'} write rejected (${res.status}):`, body);
    const err = _doorRefusal(descriptor, res.status, body);
    if (!_claimRefusal(key, err)) {
        const refusal = _replayRefusal(body, kind === 'decision' && res.status === 409 ? 'conflict (409)' : `HTTP ${res.status}`);
        if (terminal) _notifyTerminalWriteFailed({ compID, matchID, kind, status: res.status, ...refusal });
        _notifyQueueAlert({
            kind: 'rejected', count: 1, terminalCount: isFinishedResultWrite(descriptor) ? 1 : 0, compID, matchID,
            detail: refusal.reason,
            ...(refusal.sentence ? { sentence: true } : {}),
        });
    }
    if (!leaveQueue({ error: err }) && _writeQueue.has(key)) {
        _flushRequested = true;
        return 'restamped';
    }
    return 'settled';
}

// _sendTapBatch: consecutive taps of one session for one match, in one POST
// to the score-events door. Each event settles on its own outcome: a quiet
// one (applied, recorded, duplicate) leaves the queue; a clock_skew one is
// re-stamped once and goes again with the same id; any other outcome leaves
// the queue and is told (one 'not_applied' alert a pass, and to the match's
// open editor through subscribeTapOutcome). A 400 for the batch PARKS its taps
// (operator S5 b); 5xx and network keep them; 401 parks for re-auth.
async function _sendTapBatch(base, batch, pass) {
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
                if (o.outcome === TAP_CLOCK_SKEW && !d.skewRetried) {
                    // The taps settled so far leave storage before the clock is
                    // read (K2): while it is out, another tab's write to the
                    // queue would otherwise hand them back to this tab.
                    if (dequeued) { _persistQueue(); dequeued = false; }
                    _closeDequeueBatch();
                    await _restampQueuedTapForSkew(d);
                    _flushRequested = true;
                    if (result === 'settled') result = 'restamped';
                    unheld.push([k, d]);
                    continue;
                }
                if (result !== 'settled') continue; // keep strict order behind it
                if (_dequeue(k, d, { value: o })) dequeued = true;
                if (tapOutcomeQuiet(o)) {
                    landed.push(d);
                } else if (_toldOnBoard.delete(o.id)) {
                    // Told by the board as the removal reached it: once.
                } else {
                    pass.notApplied++;
                    pass.lastNotApplied = o;
                    _emitOn(_tapOutcomeListeners, base, {
                        id: o.id, outcome: o.outcome, reason: o.reason, message: o.message,
                    });
                }
            }
        } finally {
            _closeDequeueBatch();
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
            if (held) _flushRequested = true;
        }
        _recomputeSyncStatus();
        return result;
    }
    if (res.status >= 500 || res.status === 429 || res.status === 403) {
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

// Shared post-enqueue tail for every queue path: place the write in its
// match's list (_placeWrite: coalesced into the last running score write, or
// an entry of its own), persist, republish sync status, reset the backoff
// counter (a fresh user write must not inherit a stale max-backoff delay from
// a prior failure run), and kick a flush. Returns the entry's key.
// The ONE enqueue: every outbox entry, a tap included, is stored here.
// `nextTick`: kick the flush in a microtask instead of now, so the taps of one
// tick go in one batch (recordTap).
function _commitEnqueue(base, descriptor, beforeFlush, { nextTick = false } = {}) {
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
    // A write naming a member this device minted follows that member's add,
    // whatever the write (G1): read on the payload as placed, a union with the
    // write it took the place of.
    _followMintedMembers(base, descriptor);
    // An add's id is known to the owner before the add is stored, so a tab
    // that takes the add up already finds it queued.
    if (mintedAddId(descriptor)) _minted.minted(descriptor);
    _writeQueue.set(key, descriptor);
    // Read BEFORE the flush is kicked: the flush's synchronous part can
    // persist again, and it is THIS write's persist the answer reports.
    _lastEnqueuePersisted = _persistQueue();
    // The door's promise exists before anything can settle it.
    if (typeof beforeFlush === 'function') beforeFlush(key);
    _recomputeSyncStatus();
    if (descriptor.kind === 'evt') _publishPendingTaps(base);
    const kick = () => {
        if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
        _flushAttempt = 0;
        _flushQueue();
    };
    if (!nextTick) kick();
    else if (!_tapFlushKicked) {
        _tapFlushKicked = true;
        queueMicrotask(() => { _tapFlushKicked = false; kick(); });
    }
    return key;
}

// Whether the last write queued (_commitEnqueue) reached browser storage.
// A write held in memory alone is lost if the page goes, so its
// editor must not tell the operator it is "saved on this device".
let _lastEnqueuePersisted = true;

// _placeWrite: the key a new write about `base` goes under. When it coalesces
// (_coalesceTarget), it takes the queued entry's key and carries what that
// write said that a later write must not lose: the groups a score write
// changed (_claimingQueuedGroups) and, for a running write, a pending
// kachinukiBoutFinal (enqueueRunningWrite's comment says why); a lineup save
// the positions the queued save changed (joinQueuedLineupSave). Otherwise it is
// a new entry, after the others in replay order.
function _placeWrite(base, descriptor) {
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
    let payload = descriptor.kind === 'lineup'
        ? joinQueuedLineupSave(prev.payload, descriptor.payload)
        : _claimingQueuedGroups(prev, descriptor.payload);
    if (!descriptor.terminal && prev.payload && prev.payload.kachinukiBoutFinal
        && payload && !payload.kachinukiBoutFinal) {
        payload = { ...payload, kachinukiBoutFinal: true };
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
function enqueueRunningWrite(compID, matchID, payload, password) {
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
function _withoutDownstreamConfirmation(payload) {
    const { forceDownstreamReopen: _confirmation, ...rest } = payload || {};
    // The copy keeps what the write was floored by (bc-hlck, _stampPayload).
    if (_payloadSeen.has(payload)) _payloadSeen.set(rest, _payloadSeen.get(payload));
    return rest;
}

/**
 * F5: Enqueue a terminal write for offline-resilient delivery.
 * A terminal SCORE write (a Finish, a correction) takes the place of the
 * match's last queued write when that is a score write (running, or a Finish
 * the editor's Retry is re-sending), claiming its groups (_coalesceTarget,
 * _placeWrite); a lineup save joins the queued save of its lineup; every
 * other terminal write (a decision, an override) is queued after what is there
 * and replays in order. Nothing queued is ever replaced by it otherwise.
 * @param {string} key        - Queue BASE: the match the write is about (use
 *                              _revKey, or the lineup / override base)
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
// A write two tabs hold can be sent by both, which is harmless: the server
// takes an equal stamp again and refuses an older one (last-write-wins).
function _onStoredQueueChanged(oldRaw, newRaw) {
    const before = _parseStoredQueue(oldRaw);
    const after = newRaw === null ? null : _parseStoredQueue(newRaw);
    let changed = false, adopted = false, migrated = false;
    // Another tab's batch can take many entries out at once: their follow-ups
    // are done once (K6). What a member add another tab took out proved is in
    // the owner's records, which that tab wrote first (minted_members.jsx).
    _openDequeueBatch();
    try {
        for (const [k, d] of _writeQueue) {
            const gone = after === null
                ? _adopted.has(d)
                : !after.has(k) && before.get(k)?.enqueuedAt === d.enqueuedAt;
            if (gone && _dequeue(k, d)) changed = true;
        }
    } finally {
        _closeDequeueBatch();
    }
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
        _writeQueue.set(k, d);
        _noteOwnStamp(k, d);
        _noteOwnTapSeq(k, d);
        _adopted.add(d);
        changed = adopted = true;
    }
    if (migrated) _persistQueue();
    if (!changed) return;
    _recomputeSyncStatus();
    if (adopted) {
        _flushAttempt = 0;
        if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
        _flushQueue();
    }
}

// _noteOwnTapSeq: a tap of THIS tab's session taken up from storage moves the
// tab's seq counter for its match past it, so the next tap never reuses the
// seq (R10: another tab may have re-seqd it, _reseqUnheldTap).
function _noteOwnTapSeq(key, d) {
    const p = d && d.kind === 'evt' && d.payload;
    if (!p || p.session !== _eventSession || !(Number(p.seq) > 0)) return;
    const base = _baseOf(key);
    if (Number(p.seq) > (_tapSeq.get(base) || 0)) _tapSeq.set(base, Number(p.seq));
}

// _adoptStoredTapSeq: the stored copy of a tap this tab holds names a higher
// seq under the same id: another tab re-seqd it. Taken into this tab's copy
// (and its seq counter, _noteOwnTapSeq). True when the copy changed.
function _adoptStoredTapSeq(key, held, stored) {
    const hp = held && held.kind === 'evt' && held.payload;
    const sp = stored && stored.kind === 'evt' && stored.payload;
    if (!hp || !sp || hp.id !== sp.id || !(Number(sp.seq) > Number(hp.seq))) return false;
    hp.seq = Number(sp.seq);
    _noteOwnTapSeq(key, held);
    _publishPendingTaps(_baseOf(key));
    return true;
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
function _takeOverLegacyQueue(raw) {
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
        _writeQueue.set(at, d);
        _takenFromLegacy.set(at, typeof d.enqueuedAt === 'number' ? d.enqueuedAt : Infinity);
        _noteOwnStamp(at, d);
        _noteOwnTapSeq(at, d);
        taken = true;
    }
    if (!_persistQueue()) return taken;
    try { localStorage.setItem(LEGACY_QUEUE_STORAGE_KEY, '[]'); } catch (_e) { /* the next load tries again */ }
    return taken;
}

// Remove-then-add for the same reason as the online handler below.
if (typeof window !== 'undefined') {
    if (window.__bcStoredQueueHandler) window.removeEventListener('storage', window.__bcStoredQueueHandler);
    const onStorage = (e) => {
        if (e.key === QUEUE_STORAGE_KEY) _onStoredQueueChanged(e.oldValue, e.newValue);
        // Another tab learned something of a minted member: a member that
        // never was comes off this tab's writes, and a write held back for one
        // may now go (minted_members.jsx).
        if (e.key === MINTED_MEMBERS_KEY) {
            const { dropped, kept, unblocks } = _minted.onStored(e.oldValue, e.newValue);
            for (const m of kept) _announceKeptMember(m);
            if (dropped.length > 0) _learnDropped(dropped);
            else if (unblocks) _rerunFlushSoon();
        }
        // An older tab wrote what it queues under the old key: take it over (F4).
        if (e.key === LEGACY_QUEUE_STORAGE_KEY && e.newValue && _takeOverLegacyQueue(e.newValue)) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    };
    window.__bcStoredQueueHandler = onStorage;
    window.addEventListener('storage', onStorage);
}

// Flush the queue whenever the browser comes back online.
// Remove-then-add so re-evaluating this module (tests with resetModules, hot
// reload, multi-bundle inclusion) replaces the listener instead of stacking up
// orphaned ones bound to stale module state. The handler reference is parked on
// `window` (which survives module re-eval) so the prior one can be detached.
if (typeof window !== 'undefined') {
    if (window.__bcOnlineFlushHandler) {
        window.removeEventListener('online', window.__bcOnlineFlushHandler);
    }
    const onlineHandler = () => {
        // bc-cse: reconnect is also the moment an OS is most likely to have
        // NTP-jumped this device's clock (a tablet that slept through a wifi gap
        // resyncs on the way back). The SSE onopen relearn only helps if SSE
        // actually opens, which on a court that has just come back is neither
        // immediate nor guaranteed - so relearn here too, and do it BEFORE the
        // flush and regardless of whether anything is queued: the writes at risk
        // are the live ones the operator makes in the next few seconds.
        // Throttled, so a flapping connection costs one GET per 5s at worst.
        _relearnClockThrottled();
        if (_writeQueue.size > 0) {
            _flushAttempt = 0;
            if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
            _flushQueue();
        }
    };
    window.__bcOnlineFlushHandler = onlineHandler;
    window.addEventListener('online', onlineHandler);
}

// ---------------------------------------------------------------------------
// F4: Rehydrate write queue from localStorage on module load
// ---------------------------------------------------------------------------
// Parse any entries persisted by a previous page session, whatever their age
// (bc-mrgc phase 3: the server orders an old write by its stamp and keeps what
// a newer change outranks in the match's history, so nothing is dropped for
// being old). A queue persisted by an earlier build (one entry per match, keyed
// by the match itself) loads as it is: its key is its own base (_baseOf).
// Valid entries are added to _writeQueue and an immediate flush is triggered
// so they are delivered as soon as the page has network. All localStorage access is wrapped in try/catch for
// private-browsing / quota safety.
// ---------------------------------------------------------------------------

// mp-y3nk: learn the server-clock offset at load so the first writes are already
// stamped in the server's frame (refreshed again on each SSE (re)connect).
//
// bc-cse: retry until the FIRST learn succeeds. A single fire-and-forget attempt
// leaves the offset at 0 for the whole session when the load-time GET fails and
// no SSE connection ever opens - and that precondition ("SSE never opens") is
// exactly why there is no other trigger to lean on. Backoff 5s -> 10s -> 20s ->
// 40s, capped at 60s, indefinitely: one tiny GET per minute is the steady state,
// and it stops the moment anything (here or the SSE-connect relearn) succeeds.
//
// The `typeof fetch` guard wraps the WHOLE chain, not just the first attempt, so
// a host without fetch schedules no timers at all. Note it does NOT keep the
// chain out of the unit suite: jsdom + node give `fetch` here, so the poll runs
// in any test that imports this module (see the clock-poll note in
// __tests__/sync_queue.test.jsx, whose fetch counts are scoped because of it).
const CLOCK_LEARN_RETRY_BASE_MS = 5000;
const CLOCK_LEARN_RETRY_MAX_MS = 60000;
if (typeof fetch === 'function') {
    (function _learnClockUntilSuccess(delayMs) {
        _learnServerClockOffset().finally(() => {
            if (_clockOffsetLearned) return;
            setTimeout(() => {
                // Re-check on FIRE, not just on schedule: an SSE-connect learn or a
                // supersede relearn can land while this timer is pending.
                if (_clockOffsetLearned) return;
                _learnClockUntilSuccess(Math.min(delayMs * 2, CLOCK_LEARN_RETRY_MAX_MS));
            }, delayMs);
        });
    })(CLOCK_LEARN_RETRY_BASE_MS);
}

;(function _rehydrateQueue() {
    try {
        const raw = localStorage.getItem(QUEUE_STORAGE_KEY);
        if (!raw) return;
        const entries = JSON.parse(raw);
        if (!Array.isArray(entries)) return;
        let anyLoaded = false;
        // bc-qttl: every drop below is COUNTED and reported. These entries are
        // finished operator work that never reached the server, and the previous
        // code discarded them with a bare `continue` - no log, no signal, nothing
        // the operator could ever notice. Counted terminal separately from
        // running, because "3 results were lost" and "3 writes were lost" are
        // different claims and only the terminal ones are a finished result.
        // The one class left is a corrupt entry: nothing is dropped for its age
        // (bc-mrgc phase 3).
        let unreadable = 0, unreadableTerminal = 0, migrated = false;
        // A dropped entry is marked settled, so the persist below takes it out
        // of storage: the merge otherwise keeps what this tab did not write.
        const dropUnreadable = (key, d) => {
            unreadable++;
            if (isFinishedResultWrite(d)) unreadableTerminal++;
            if (key) _markSettled(key, d);
        };
        for (const item of entries) {
            // Defensive: a single malformed element (corrupt/tampered storage) must
            // not throw and abort the whole rehydrate: destructuring a non-array in
            // the for-of header would do exactly that, dropping ALL valid queued
            // writes. Validate the tuple shape first and skip bad items individually.
            if (!Array.isArray(item) || item.length < 2) { dropUnreadable(null, null); continue; }
            const [key, descriptor] = item;
            if (!key || !descriptor || typeof descriptor !== 'object') { dropUnreadable(key, null); continue; }
            if (_storedEntryProblem(descriptor)) { dropUnreadable(key, descriptor); continue; }
            // Only restore if not already superseded by a same-session write.
            if (!_writeQueue.has(key)) {
                // A write a previous page left behind has already waited: it
                // is held, and its landing is told (the 'sent' alert).
                descriptor.held = true;
                // A confirmation an older build stored is never replayed: it
                // answered a dialog on a page that is gone.
                if (descriptor.payload && descriptor.payload.forceDownstreamReopen) {
                    descriptor.payload = _withoutDownstreamConfirmation(descriptor.payload);
                }
                // A match lineup an older build queued under its own base goes
                // onto its match's (R9); the old key leaves storage below.
                const moved = _migratedLegacyLineup(key, descriptor);
                const at = moved || key;
                if (moved) { _markSettled(key, descriptor); migrated = true; }
                if (!_writeQueue.has(at)) {
                    _writeQueue.set(at, descriptor);
                    _noteOwnStamp(at, descriptor);
                    anyLoaded = true;
                }
            }
        }
        // Persist the pruned queue whenever anything was dropped. This is what makes
        // the alert below fires ONCE rather than on every reload forever: rehydrate
        // never used to write back, so a dropped entry stayed in localStorage
        // untouched, and each refresh would re-read, re-drop and re-announce it.
        // With the queue now empty _persistQueue() stores it as "[]".
        if (unreadable > 0 || migrated) _persistQueue();
        if (unreadable > 0) {
            console.warn(`[sync] discarded ${unreadable} unreadable queued write(s) on rehydration`);
            _notifyQueueAlert({ kind: 'unreadable', count: unreadable, terminalCount: unreadableTerminal });
        }
        if (anyLoaded) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    } catch (_e) { /* private-browsing / malformed JSON: silently skip */ }
}());

// After this build's own queue: what an older build left under the old key
// (F4). A key both hold keeps this build's entry.
;(function _takeOverLegacyQueueOnLoad() {
    try {
        if (_takeOverLegacyQueue(localStorage.getItem(LEGACY_QUEUE_STORAGE_KEY))) {
            _recomputeSyncStatus();
            _flushQueue();
        }
    } catch (_e) { /* private-browsing: nothing to take over */ }
}());

// ---------------------------------------------------------------------------
// Shared ref-counted SSE singleton
// ---------------------------------------------------------------------------
// All subscribeToEvents callers share ONE EventSource connection. A Set of
// subscriber records ({callback, onStatus}) is maintained; when the last
// subscriber unsubscribes the source is closed and nulled out.
//
// F2: SSE silence watchdog: if no message (including heartbeat) arrives for
// 35 s (>2× the server's 15 s heartbeat interval, with margin for slow courts)
// the connection is assumed stale and is torn down for a fresh reconnect.
// _lastActivityAt is updated on every onopen and every onmessage.
//
// F3: Reconnect uses exponential backoff + jitter (1–30 s) instead of a fixed
// 5 s delay. _reconnectAttempt increments on each onerror and resets to 0 in
// onopen, so a stable connection always reconnects fast after the first error.
// ---------------------------------------------------------------------------

/** @type {EventSource|null} */
let _sharedSource = null;
/** @type {ReturnType<typeof setTimeout>|null} */
let _retryTimer = null;
/** @type {Set<{callback: Function, onStatus: Function|undefined}>} */
const _subscribers = new Set();

// The last event id the stream delivered (the server's seq, from its `id:`
// line). A reconnect opens a NEW EventSource, which sends no Last-Event-ID
// header, so the id rides on the URL and the hub replays every event after
// it (or answers resync_required when it cannot). A resync_required with no
// id (a restarted server that has sent nothing yet) resets it, since the
// browser keeps the old id when a frame carries none.
let _lastEventId = '';
function _noteEventId(id, parsed) {
    if (parsed && parsed.type === 'resync_required' && !(parsed.seq > 0)) {
        _lastEventId = '';
        return;
    }
    if (id) _lastEventId = id;
}
function _eventsUrl() {
    return _lastEventId ? `/api/events?lastEventId=${encodeURIComponent(_lastEventId)}` : '/api/events';
}

// F2: timestamp of the last received SSE activity (open or message).
let _lastActivityAt = 0;
/** @type {ReturnType<typeof setInterval>|null} */
let _watchdogTimer = null;
const SSE_WATCHDOG_INTERVAL_MS = 10000;  // poll every 10 s
const SSE_SILENCE_THRESHOLD_MS = 35000; // reconnect if silent for >35 s

// F3: reconnect backoff state.
let _reconnectAttempt = 0;

/** Compute the next reconnect delay: exponential backoff 1–30 s plus up to 1 s of jitter. */
function _reconnectDelay() {
    return Math.min(30000, 1000 * Math.pow(2, _reconnectAttempt)) + Math.random() * 1000;
}

/** Arm the silence watchdog. Idempotent: clears any prior interval first. */
function _armWatchdog() {
    _disarmWatchdog();
    _watchdogTimer = setInterval(() => {
        // If the source is gone already (e.g. onerror cleared it) do nothing: 
        // the reconnect timer will re-arm the watchdog when the new source opens.
        if (!_sharedSource) return;
        if (Date.now() - _lastActivityAt > SSE_SILENCE_THRESHOLD_MS) {
            // Silence detected: tear down the stale source and reconnect.
            _sharedSource.close();
            _sharedSource = null;
            _fanOutStatus('error');
            if (_subscribers.size > 0) {
                if (_retryTimer) { clearTimeout(_retryTimer); }
                _retryTimer = setTimeout(_ensureConnected, _reconnectDelay());
                _reconnectAttempt++;
            }
            // Watchdog re-arms itself after _ensureConnected → onopen.
            _disarmWatchdog();
        }
    }, SSE_WATCHDOG_INTERVAL_MS);
}

/** Disarm the silence watchdog (call when no subscribers remain). */
function _disarmWatchdog() {
    if (_watchdogTimer !== null) { clearInterval(_watchdogTimer); _watchdogTimer = null; }
}

function _emitStatus(sub, s) {
    if (typeof sub.onStatus === 'function') {
        try { sub.onStatus(s); } catch (err) { console.error('SSE status callback failed:', err); }
    }
}

function _fanOutStatus(s) {
    for (const sub of _subscribers) {
        _emitStatus(sub, s);
    }
}

function _ensureConnected() {
    // Guard: the retry timer may fire after all subscribers have unsubscribed
    // (clearTimeout only cancels a pending callback: it cannot stop one that is
    // already queued in the event loop). No-op when no one is listening so we
    // don't open a zombie EventSource that can never be closed.
    if (_subscribers.size === 0) return;
    // Whoever calls this is establishing the connection NOW, so cancel any
    // pending reconnect timer: otherwise a retry queued by a previous onerror
    // can fire later and open a second EventSource (a leaked SSE connection).
    if (_retryTimer) {
        clearTimeout(_retryTimer);
        _retryTimer = null;
    }
    // Guard against double-connect: only one shared source may exist at a time.
    if (_sharedSource) return;
    // Bind handlers to THIS instance via a local const. Each handler ignores
    // events from a superseded source (`source !== _sharedSource`) so a stale
    // instance: should one ever fire after being replaced: can't close the
    // live connection or fan out a false status/message to current subscribers.
    const source = new EventSource(_eventsUrl());
    _sharedSource = source;

    // F2: arm the watchdog at connect time, not just in onopen: a CONNECTING
    // stall (half-open / captive / saturated WiFi where onopen, and sometimes
    // even onerror, never fire) is the exact silent-staleness this watchdog must
    // catch. Stamp activity now so the 35s timer measures from the attempt.
    _lastActivityAt = Date.now();
    _armWatchdog();

    source.onopen = () => {
        if (source !== _sharedSource) return;
        // F2: record activity and arm the watchdog on every (re)connect.
        _lastActivityAt = Date.now();
        _armWatchdog();
        // F3: a successful open resets the backoff counter so the next error
        // reconnects fast (starting from 1 s) rather than inheriting a long delay.
        _reconnectAttempt = 0;
        // mp-y3nk: (re)learn the server-clock offset on every connect so writes
        // stamped after a reconnect stay in the server's frame.
        _learnServerClockOffset();
        _fanOutStatus('open');
    };

    source.onmessage = (event) => {
        if (source !== _sharedSource) return;
        // F2: every message (including heartbeat {type:"heartbeat"}) resets the
        // silence watchdog. No seq field on heartbeats: just update the timestamp.
        _lastActivityAt = Date.now();
        let parsed;
        try {
            parsed = JSON.parse(event.data);
        } catch (err) {
            console.error('Error parsing SSE event:', err);
            return;
        }
        // bc-cse: heartbeat clock TRIPWIRE. The 15s heartbeat carries the
        // server's nowMs, which makes it a free, continuous check on whether this
        // device is still in the server's frame - so a clock that jumps mid-event
        // (an NTP correction, an operator "fixing" the tablet's time) is caught
        // within one heartbeat instead of at the next refused result.
        //
        // It is a DETECTOR, NEVER A SOURCE. nowMs is never assigned into the
        // offset, because a one-way push carries no round-trip to correct for:
        // the frame arrives some unknown latency after it was stamped, and on the
        // congested court wifi this exists for, that latency is exactly when it is
        // largest. Applying it directly would push this device's frame BACKWARDS
        // by that latency - manufacturing the behind-the-clock skew the whole
        // mechanism is here to prevent. So the tripwire only ever asks for a real
        // GET /api/time, whose RTT-halving math is the one thing entitled to move
        // the offset. A spurious trip costs one throttled GET; a corrupted offset
        // costs the operator's data.
        //
        // Threshold sits below the server's 5s refusal margin (see
        // CLOCK_TRIPWIRE_MS) so the heal happens before writes start bouncing.
        if (parsed && parsed.type === 'heartbeat' && typeof parsed.nowMs === 'number') {
            if (Math.abs(parsed.nowMs - _serverNowMs()) > CLOCK_TRIPWIRE_MS) {
                _relearnClockThrottled();
            }
        }
        // S2: the stream's first frame names the server's build.
        if (parsed && parsed.type === 'heartbeat' && parsed.hello) noteServerBuild(parsed.build);
        if (parsed && (parsed.type === 'draw_generated' || parsed.type === 'draw_discarded')) {
            _forgetShownMatches(parsed.data && parsed.data.competitionId);
        }
        if (parsed && (parsed.type === 'tournament_updated' || parsed.type === 'password_reset')) _forgetAllShownMatches();
        // A push of a match names the taps the server turned away since their
        // answer: this device's are told (_tellTurnedAway).
        if (parsed && parsed.type === 'match_updated' && parsed.data && parsed.data.result) {
            const r = parsed.data.result;
            _tellTurnedAway(parsed.data.competitionId, parsed.data.matchId || r.id, r);
        }
        _noteEventId(event.lastEventId, parsed);
        for (const sub of _subscribers) {
            try { sub.callback(parsed); } catch (err) { console.error('SSE callback failed:', err); }
        }
    };

    source.onerror = () => {
        if (source !== _sharedSource) return;
        source.close();
        _sharedSource = null;
        // F2: disarm the watchdog: the source is gone; a new one will re-arm it.
        _disarmWatchdog();
        _fanOutStatus('error');
        if (_subscribers.size > 0) {
            // Clear any prior timer before re-arming so repeated error events
            // can't queue multiple concurrent reconnects.
            if (_retryTimer) clearTimeout(_retryTimer);
            // F3: exponential backoff + jitter instead of fixed 5 s.
            _retryTimer = setTimeout(_ensureConnected, _reconnectDelay());
            _reconnectAttempt++;
        }
    };
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
function parseSkippedCompetitionsHeader(headerValue) {
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
        _relearnClockThrottled();
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
async function _fetchJson(url, opts, ms = FETCH_TIMEOUT_MS) {
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

// The daihyosen add (POST) or remove (DELETE), stamped like recordDecision,
// an entry of its match's outbox. A refusal shows the server's own sentence
// when it sends one (_refusalText), e.g. a finished match's.
function _daihyosenRequest(method, compID, matchID, password, seenModifiedAt, opts = {}) {
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

// What a lineup removal leaves undone when it does not land.
const LINEUP_NOT_REMOVED = 'The lineup was not removed';

// _readLineup: a lineup's GET, bounded like every sibling request (_fetchJson:
// the deadline covers the body), so a read the server never answers rejects with
// _requestTimedOut's error, which lineupReadFailure words, instead of leaving an
// editor on "Loading lineup…" until the browser gives up on the connection.
// `saved: false` reads as null (lineupOrNull). Shared by fetchTeamLineup and
// fetchLineupInForce.
async function _readLineup(url) {
    const { res, body } = await _fetchJson(url, {});
    if (!res.ok) throw new Error(body.error || "Failed to load lineup");
    // An answer whose body could not be read comes back from _fetchJson as an empty
    // object, which cannot be told from one the server sent. A body with no keys is
    // refused here rather than read as no lineup, since an editor would then show an
    // empty lineup as the one the team fields.
    if (Object.keys(body).length === 0) throw new Error("The lineup could not be read. Check the connection and try again.");
    return lineupOrNull(body);
}

// What a team member write leaves undone when the server does not answer it.
const TEAM_MEMBER_NOT_ADDED = 'The team member was not added';
const TEAM_MEMBER_NOT_RENAMED = 'The team member was not renamed';
const TEAM_MEMBER_NOT_CLEARED = 'The team member\'s name was not cleared';

// _memberWrite: a team member's POST (add), PUT (rename) or DELETE (cleared name), as an
// outbox entry (R1). `payload` is the JSON body, none for a clear; `member` the member as
// this device shows it once the write is made.
function _memberWrite({ compID, teamId, method, url, payload, password, member, opts }) {
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
function _droppedOf(d) {
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
function _mintedAddAnswered(key, d, res, body, timedOut = false) {
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
function _announceKeptMember({ id, c }) {
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
function _mintedSweep(d = null) {
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
function _mintedGate(key, d) {
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
function _learnDropped(list) {
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
function _mintedPrune(merged) {
    const named = new Set();
    const adds = new Set();
    for (const e of merged.values()) {
        for (const id of mintedIdsNamed(e)) named.add(id);
        const add = mintedAddId(e);
        if (add) adds.add(add);
    }
    _minted.prune(named, adds);
}

// The entries an operator discarded while their request was out (stage 3
// review 8, L1): the discard took them out of the queue at once, so nothing
// waits on them and the UI goes on, but what a discard undoes (a court or time
// overlay put back) waits for the request's end, which may be the server
// keeping the write. A member add's end is the owner's (_mintedAddAnswered).
const _discardedWhileOut = new WeakSet();

// _discardWhileOut: `d` was discarded while out.
function _discardWhileOut(d) {
    _discardedWhileOut.add(d);
}

// _endDiscardWhileOut: the request of an entry discarded while out ended
// (`res`, or none: a network failure or the deadline). Answers true when it
// was one, which its answer then settles nothing of. A 2xx is the write kept:
// nothing is undone. Anything else undoes what the discard would have.
function _endDiscardWhileOut(d, res, body) {
    if (!_discardedWhileOut.has(d)) return false;
    _discardedWhileOut.delete(d);
    const kept = !!res && ((res.ok && !writeWasRefusedForClock(body || {}))
        || (res.status === 404 && d.kind === 'lineup-remove'));
    if (!kept) _revertLocalFields(d);
    _rerunFlushSoon();
    return true;
}

// _rerunFlushSoon: a flush now, or a rerun of the one under way.
function _rerunFlushSoon() {
    if (_flushInProgress) _flushRequested = true;
    else queueMicrotask(() => { if (_writeQueue.size > 0) _flushQueue(); });
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
function _followMintedMembers(base, descriptor) {
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

// _newMemberId: a member id this device mints, a lowercase UUID v4 (the
// server's state.IsClientMemberID).
function _newMemberId() {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID().toLowerCase();
    const hex = (n) => Array.from({ length: n }, () => Math.floor(Math.random() * 16).toString(16)).join('');
    const variant = (8 + Math.floor(Math.random() * 4)).toString(16);
    return `${hex(8)}-${hex(4)}-4${hex(3)}-${variant}${hex(3)}-${hex(12)}`;
}

// The member as this device shows it after its own write: stamped now, in
// server time, so it stands over the copy the lists hold until the server's
// own copy comes (stamped when it lands).
function _shownMember(member, fields) {
    return { ...member, ...fields, modifiedAt: _serverNowMs() };
}

const API = {
    async fetchTournament() {
        const res = await fetch('/api/viewer/tournament');
        // "No tournament configured yet" is a normal bootstrap state, not an
        // error: return null so callers open the create-tournament gate quietly
        // instead of logging a console error. Newer servers signal it with a 200
        // and a null JSON body (handled by the res.json() return below); older
        // servers used a 404 (handled here). Genuine failures (5xx, network)
        // still throw below.
        if (res.status === 404) {
            return null;
        }
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch tournament (Status ${res.status})`);
        }
        return res.json();
    },
    // Public endpoint: returns {mode: "file"|"locked", resetEnabled: bool}.
    // Mounted on App() boot to decide whether to render the "Forgot
    // password?" link in AuthModal and whether the /reset SPA route
    // should show a form or an "operator-disabled" message. Always
    // resolves (never rejects): non-2xx responses, network failures, and
    // JSON parse errors all fall back to the file-mode default so a
    // fresh-deploy SPA pointed at an older server without this endpoint
    // still works, and any transport error doesn't break sign-in.
    async fetchAuthConfig() {
        try {
            const res = await fetch('/api/auth-config');
            if (!res.ok) {
                return { mode: 'file', resetEnabled: true };
            }
            return await res.json();
        } catch {
            return { mode: 'file', resetEnabled: true };
        }
    },
    async fetchVersion() {
        try {
            const res = await fetch('/api/version');
            if (!res.ok) {
                return null;
            }
            return await res.json();
        } catch {
            return null;
        }
    },
    // Reset the admin password. Unauthenticated by design: the
    // server enforces "is this endpoint enabled" via the verifier's
    // ResetEnabled() (locked mode returns 404). In file mode it answers
    // only a request made on the server machine (loopback peer and
    // loopback Host), else 403 reset_local_only. Throws on non-2xx so
    // the caller can surface the server's error message (including the
    // 404 "reset disabled" case if the SPA's cached authConfig was
    // stale).
    //
    // `originatorId` is a per-tab client ID echoed back on the SSE
    // password_reset event payload so the originating tab can ignore
    // its own broadcast and avoid clobbering the just-written
    // localStorage credential. Optional: when absent the server
    // broadcasts an empty originator and ALL tabs log out.
    async resetPassword(newPassword, originatorId) {
        const body = { password: newPassword };
        if (originatorId) body.originatorId = originatorId;
        const res = await fetch('/api/tournament/reset', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to reset password (Status ${res.status})`);
            e.status = res.status;
            // The refusal code (reset_local_only) lets the form word the
            // case; absent on a bare error.
            if (err.code) e.code = err.code;
            throw e;
        }
        // A reset: no learned match label is trusted (K8).
        _forgetAllShownMatches();
        // Backend returns 204 No Content: calling .json() on an empty
        // body throws SyntaxError per the Fetch spec (same pattern as
        // overridePoolRanks etc. above).
        return true;
    },
    // `bounded` gives up after FETCH_TIMEOUT_MS, the body read included
    // (_fetchJson), instead of waiting on a connection that never answers.
    // The organiser's refresh after a write asks for it (admin.jsx
    // refreshCompsBestEffort): the score editor that made the write waits on
    // that refresh, and it is best-effort. Other callers wait as long as the
    // aggregate takes, which can be long for a large tournament on a slow link.
    async fetchCompetitions({ bounded = false } = {}) {
        if (bounded) {
            const { res, body } = await _fetchJson('/api/viewer/competitions', {});
            if (!res.ok) throw new Error(body.error || `Failed to fetch competitions (Status ${res.status})`);
            // _fetchJson reads an unreadable body as {}: never hand that on as
            // the competitions.
            if (!Array.isArray(body)) throw new Error('Failed to fetch competitions (unreadable answer)');
            return body.map((c) => _overlayPendingFields(normalizeViewerCompItem(c)));
        }
        const res = await fetch('/api/viewer/competitions');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competitions (Status ${res.status})`);
        }
        const comps = await res.json();
        if (!Array.isArray(comps)) return comps;
        // A court or time this device still has queued shows (R4).
        return comps.map((c) => _overlayPendingFields(normalizeViewerCompItem(c)));
    },

    // fetchCourtMatches returns the court-scoped match feed: only the
    // competitions that have a real match physically on `court` right now, each
    // with its full {config, poolMatches, bracket} payload (same per-comp shape
    // as fetchCompetitions). The operator console sources its cross-competition
    // court view from this instead of the whole-tournament aggregate. Returns a
    // normalized competitions array so callers can run tournamentMatches() over
    // { competitions } exactly as they do for the aggregate.
    async fetchCourtMatches(court) {
        const res = await fetch(`/api/viewer/court/${encodeURIComponent(court)}/matches`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch court matches (Status ${res.status})`);
        }
        const data = await res.json();
        const comps = Array.isArray(data.competitions) ? data.competitions : [];
        return comps.map((c) => _overlayPendingFields(normalizeViewerCompItem(c)));
    },
    async fetchCompetitionDetails(id) {
        const res = await fetch(`/api/viewer/competitions/${id}`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch competition details (Status ${res.status})`);
        }
        const data = await res.json();
        return _overlayPendingFields(normalizeCompetitionDetail(data), id);
    },
    // Bootstrap a fresh tournament. In file mode the call is
    // unauthenticated (the AuthMiddleware's uninitialized-bootstrap
    // branch lets it through). In locked mode the server requires the
    // env-var password to authorize even the initial POST: pass
    // `authPassword` (typed by the operator into the locked-mode
    // CreateTournament form) so we can attach it as
    // X-Tournament-Password. The handler discards the body's
    // `password` field in locked mode but the header is what gets
    // verified.
    async createTournament(config, authPassword) {
        const headers = { 'Content-Type': 'application/json' };
        if (authPassword) {
            headers['X-Tournament-Password'] = authPassword;
        }
        const res = await fetch('/api/tournament', {
            method: 'POST',
            headers,
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create tournament");
        }
        return res.json();
    },
    async updateTournament(config, password) {
        const res = await fetch('/api/tournament', {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update tournament");
        }
        return res.json();
    },
    async updateCompetition(id, config, password) {
        // Go's domain.Player uses json:"metadata" (array); the JS layer uses the
        // friendlier danGrade string. Convert before encoding so the backend can
        // unmarshal the dan grade into Player.Metadata.
        let body = config;
        if (config.players) {
            body = {
                ...config,
                players: config.players.map((player) => {
                    // Only transform players that have been through normalizePlayer
                    // (which always adds an explicit danGrade key). A Go-sourced player
                    // object without danGrade must be passed through unchanged: if we
                    // destructure it, rest would be empty and metadata[0] would be lost.
                    if (!('danGrade' in player)) return player;
                    const { danGrade, metadata: _m, ...p } = player;
                    const metadata = buildPlayerMetadata(danGrade, player.metadata);
                    return metadata === undefined ? p : { ...p, metadata };
                }),
            };
        }
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(body)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update competition");
        }
        return res.json();
    },
    async createCompetition(config, password) {
        const res = await fetch('/api/competitions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify(config)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to create competition");
        }
        return res.json();
    },
    // exportPDFs POSTs to /api/print/:type (admin-gated) and returns
    // { blob, skipped }: `blob` is a ZIP of the produced PDFs, `skipped` is
    // the parsed X-Skipped-Competitions header as [{id, reason}, ...] (empty
    // array when the header is absent, i.e. nothing was skipped). `type` is
    // one of registration|names|tags|pools-trees|full-bracket|all.
    //
    // Throws with the server's error message on non-2xx (e.g. 503 when
    // LibreOffice is absent, 422 when every competition was skipped or no
    // pages were produced); the thrown Error also carries a `skipped` array
    // (from the JSON body's `skipped` field, e.g. the all-skipped 422 case),
    // empty when the body carries none. Generation runs LibreOffice and can
    // take 30–60s, so callers should show a busy state.
    async exportPDFs(type, password) {
        const res = await fetch(`/api/print/${type}`, {
            method: 'POST',
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || "Failed to generate PDFs");
            e.skipped = Array.isArray(err.skipped) ? err.skipped : [];
            throw e;
        }
        const skipped = parseSkippedCompetitionsHeader(res.headers.get('X-Skipped-Competitions'));
        const blob = await res.blob();
        return { blob, skipped };
    },
    // exportResults GETs the RESULTS-populated workbook (played scores,
    // standings, winners, and decision suffixes written as literal values)
    // from the admin-gated /api/competitions/:id/export-results endpoint. This
    // is distinct from AdminExport's "Download .xlsx", which posts to the public
    // /create path and yields a BLANK formula template. Returns an .xlsx Blob;
    // throws the server's error message on non-2xx.
    async exportResults(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/export-results`, {
            method: 'GET',
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to export results");
        }
        return res.blob();
    },
    async startCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}/start`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to start competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    async generateDraw(id, password) {
        const res = await fetch(`/api/competitions/${id}/generate-draw`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to generate draw");
        }
        _forgetShownMatches(id);
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // fetchDrawWarnings: GET /competitions/:id/draw-warnings. The seeding rules
    // never refuse a draw; where a configuration cannot satisfy them all, the
    // deepest constraint gives way and the operator is told what was relaxed.
    // This is that advice, and it is admin-only (the public viewer surfaces
    // deliberately do not carry it).
    //
    // Advisory, so a failure is swallowed into an empty list: a competition
    // page must never break, or lose its draw, over a warning it could not
    // fetch.
    async fetchDrawWarnings(id, password) {
        try {
            const res = await fetch(`/api/competitions/${id}/draw-warnings`, {
                headers: password ? { 'X-Tournament-Password': password } : {}
            });
            if (!res.ok) return [];
            const data = await res.json();
            return data.warnings || [];
        } catch {
            return [];
        }
    },
    async discardDraw(id, password) {
        const res = await fetch(`/api/competitions/${id}/draw`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to discard draw");
        }
        _forgetShownMatches(id);
    },
    // quarantineBracket: POST /competitions/:id/bracket/quarantine. The way out
    // of a bracket.json that will not parse, which otherwise blocks every score
    // write for that competition. Renames the broken file aside (it is NEVER
    // deleted: it is the only record of the knockout results it holds, and
    // because it does not parse nothing can count them). What happens next
    // depends on the format, and the response says which: `rebuilt` is true when
    // the knockout was rebuilt from pools.csv (mixed), false when there was no
    // knockout stage to rebuild and moving the file aside was the whole repair
    // (league, Swiss). 400 when the bracket reads fine, and 400 with a reason
    // for any competition that draws its bracket DIRECTLY, where the file is the
    // only record of the draw and rebuilding would invent different pairings
    // rather than restore these.
    async quarantineBracket(id, password) {
        const res = await fetch(`/api/competitions/${id}/bracket/quarantine`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        const body = await res.json().catch(() => ({}));
        if (!res.ok) {
            throw new Error(body.error || "Failed to quarantine the bracket file");
        }
        return body;
    },
    // completeCompetition: POST /competitions/:id/complete. The only trigger
    // for a bracket-based competition (knockout, or mixed once its knockout is
    // running) to reach status "completed": MaybeAutoCompletePools only
    // auto-transitions the League format, so a finished bracket otherwise sits
    // in "pools"/"knockout" forever and the public viewer's Awards tab (gated
    // on status === "completed") never becomes reachable. 400 when the
    // competition isn't in "pools" or "knockout" status (already completed, or
    // not started yet), or a naginata 3rd-place match is still unscored; 404
    // when the competition doesn't exist.
    async completeCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}/complete`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to complete competition");
        }
        const data = await res.json();
        return normalizeCompetitionDetail(data);
    },
    // Subscribe to the server's SSE event stream. `callback` is fired
    // for each parsed event. `onStatus` (optional) is fired with
    // 'open' when the EventSource transitions to open and 'error' when
    // it transitions away: used by the display surfaces (FR-011
    // scenario 4 / T063) to render a reconnect indicator when the
    // browser is between connections. Existing call sites that omit
    // the second arg are unaffected.
    //
    // All callers share ONE module-level EventSource (ref-counted). The
    // source is opened on the first subscribe and closed when the last
    // subscriber unsubscribes. A subscriber joining an already-OPEN source
    // receives an immediate onStatus('open') replay so it starts in the
    // correct connected state without waiting for the next onopen event.
    subscribeToEvents(callback, onStatus) {
        const record = { callback, onStatus };
        _subscribers.add(record);

        if (_sharedSource === null) {
            // No source yet: open one. The new subscriber keeps its optimistic
            // default until this source's onopen/onerror fires.
            _ensureConnected();
        } else if (_sharedSource.readyState === EventSource.OPEN) {
            // Source is already connected: replay 'open' to this subscriber.
            _emitStatus(record, 'open');
        }
        // Else the source exists but is still CONNECTING (a non-null source is
        // only ever CONNECTING or OPEN: we null it on close). Replay NOTHING
        // and let the bound onopen/onerror fan-out deliver the first
        // authoritative status, exactly as the first subscriber experiences.
        // Replaying 'error' here would flash a false "Reconnecting…" on admin
        // load, where AdminTopbar/AdminDashboard subscribe mid-handshake.

        // Idempotent unsubscribe: guard with a `done` flag so double-calling
        // never double-decrements the ref count.
        let done = false;
        return () => {
            if (done) return;
            done = true;
            _subscribers.delete(record);
            if (_subscribers.size === 0) {
                clearTimeout(_retryTimer);
                _retryTimer = null;
                // F2: disarm the watchdog when no subscribers remain: no point
                // in polling for silence when no one is listening.
                _disarmWatchdog();
                if (_sharedSource) {
                    _sharedSource.close();
                    _sharedSource = null;
                }
            }
        };
    },
    async recordScore(compID, matchID, result, password, match, opts = {}) {
        _noteShownMatch(compID, match);
        const payload = toBackendMatchResult(result, match || result);
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
        // (mergeMatchPatch), which turned a kept 2-1 into 0-0. `startOnly`
        // itself is request-only and stays off the display's copy.
        const _broadcastPatch = (fields) => {
            const {
                rev: _r, revSession: _rs, changed: _ch,
                writeId: _w, basis: _b, session: _s, forceDownstreamReopen: _f,
                startOnly, ...rest
            } = fields || {};
            if (startOnly) {
                for (const k of START_BROADCAST_DROPS) delete rest[k];
                if (match && (match.flagsA != null || match.flagsB != null)) {
                    rest.flagsA = match.flagsA ?? 0;
                    rest.flagsB = match.flagsB ?? 0;
                }
            }
            const court = (match && match.court) || '';
            // Do not emit an unscoped (court-less) broadcast: a display can only
            // safely apply a patch it can attribute to its court, and the display
            // guard drops court-less messages anyway. In production every
            // recordScore caller passes the match (so court is set); the
            // court-less case is test-only (match=null), where no broadcast is
            // expected.
            if (!court) return;
            _bridge.publish('patch', court, compID, {
                result: {
                    id: matchID,
                    ...(match && match.sideA && match.sideA.id ? { sideAId: match.sideA.id } : {}),
                    ...(match && match.sideB && match.sideB.id ? { sideBId: match.sideB.id } : {}),
                    ...rest,
                },
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
        _broadcastPatch(payload);
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
    // T098: competitor eligibility statuses produced by kiken/fusenpai
    // decisions. Endpoint is read-only and unauthenticated: same contract as
    // the other /api/competitions/:id/* GETs surfaced to the viewer.
    async fetchCompetitorStatuses(compID) {
        const res = await fetch(`/api/competitions/${compID}/competitor-status`);
        if (!res.ok) throw new Error("Failed to load competitor statuses");
        const data = await res.json();
        return data.statuses || [];
    },
    async reinstateCompetitor(compID, playerID, password, opts = {}) {
        // R1: an outbox entry under the competitor's own base, answered at
        // once; `settled` resolves with the status the server stored, or
        // rejects with its refusal (`opts.onRefusal` may claim it).
        return _outboxWrite({
            kind: 'reinstate', compID, matchID: '', method: 'POST',
            url: `/api/competitions/${compID}/competitors/${playerID}/reinstate`,
            payload: {}, password, onRefusal: opts && opts.onRefusal,
            base: `competitor:${compID}:${playerID}`,
        });
    },
    // overridePoolRanks records a whole group of pool ranks, a chusen's order,
    // as ONE write: ranks is [{ playerId, rank }], and the server records
    // every rank together and answers for the final order only. Sent one rank
    // at a time, the order passed through states nobody chose, so a knockout
    // match whose place the final order keeps could be named and reopened.
    //
    // playerId is REQUIRED on every entry (operator ruling bc-pnum): the
    // server resolves a pool member by id only (poolHasPlayerID,
    // handlers_competition.go) and rejects the request with 400 when it is
    // missing -- two pool members can legally share a display name from
    // different dojos, so a name can never disambiguate them. Neither a name
    // nor a dojo is sent: the server never reads them.
    //
    // forceDownstreamReopen is the operator's confirmation of a 409
    // downstream_knockout_played refusal: a new order that moves a qualifier
    // out of a knockout match the old one already fought is refused exactly
    // like a pool result correction (and one being fought is refused outright,
    // downstream_knockout_running). Both are parsed into the same structured
    // errors recordScore throws, so the chusen panel offers the same
    // confirm-and-retry (write_result.jsx's attemptScoreWrite); `ranking`
    // marks the refusal as a rank change rather than a corrected result, for
    // the dialog's copy.
    overridePoolRanks(compID, poolID, ranks, password, forceDownstreamReopen, opts = {}) {
        // bc-evts stage 3 review 11, Y7 (the governing rule: "ALL goes to the
        // outbox ... but never blocks the UI"): an outbox entry under the
        // competition's pool-rank base, so a chusen's order and a reset of
        // the overrides are sent strictly in the order they were made,
        // answered pending at once. `settled` carries the server's answer, or
        // rejects with the refusal (_doorRefusal 'pool-ranks': a downstream
        // refusal is the structured error attemptScoreWrite reads, marked as
        // a ranking), which `opts.onRefusal` may claim. An older server's
        // empty 200 body is a success.
        const payload = { ranks };
        if (forceDownstreamReopen) payload.forceDownstreamReopen = true;
        return _outboxWrite({
            kind: 'pool-ranks', compID, matchID: '', method: 'PUT',
            url: `/api/competitions/${compID}/pools/${encodeURIComponent(poolID)}/override-rank`,
            payload, password, onRefusal: opts && opts.onRefusal,
            base: _poolRanksBase(compID),
        });
    },
    // bc-kcdg: forceDownstreamReopen is the operator's confirmed override of a
    // 409 downstream_knockout_played refusal (this match's later round already
    // played on the current winner), threaded through exactly as recordScore's
    // payload carries it, so a confirmed retry here also reopens the blocking
    // match(es) for re-entry rather than silently repainting them.
    // seenModifiedAt (optional): the match's stamp as this device last had
    // it, which the stamp is never older than (bc-hlck).
    async overrideBracketWinner(compID, matchID, winnerName, password, forceDownstreamReopen, seenModifiedAt, opts = {}) {
        const url = `/api/competitions/${compID}/matches/${matchID}/override-winner`;
        // mp-y3nk: stamp in server-relative time, never older than the feeder
        // it names (bc-hlck).
        const payload = _stampPayload({ winnerName }, _serverNowMs(), seenModifiedAt);
        if (forceDownstreamReopen) payload.forceDownstreamReopen = true;
        // bc-evts: an operator's feeder-winner assertion (the "Run now"
        // recovery) is the outbox's like every rule-bound write, on its MATCH's
        // base (it was `override:…`; an entry an older build queued there
        // replays unchanged, F3). `settled` resolves `{ applied, reason,
        // message, heldGroups? }`; a held assertion asks the bracket to resync
        // (_sendEntry), so a caller may advance its local bracket at once.
        return _outboxWrite({
            kind: 'override', compID, matchID, method: 'PUT', url, payload, password,
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
        _emitLocalMatchPatch(compID, matchID, { scheduledAt });
        return answer;
    },
    // Stateless hypothetical estimate against GET /api/schedule/estimate. The
    // free-form estimator PANEL was removed (per-competition estimates on
    // Overview/Settings go through estimateCompetitionSchedule instead), so this
    // method currently has no production caller. It is RETAINED DELIBERATELY -
    // not dead - for the planned standalone hypothetical estimator page
    // (mp-lw5p), which reuses this endpoint and the `teamMatchType` query param.
    // Exercised by api.test.jsx. Do not delete without closing mp-lw5p first.
    async estimateSchedule(args, password, signal) {
        const params = new URLSearchParams();
        Object.entries(args).forEach(([k, v]) => {
            if (v !== undefined && v !== null && v !== "" && !Number.isNaN(v)) {
                params.append(k, v);
            }
        });
        const res = await fetch(`/api/schedule/estimate?${params.toString()}`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to estimate schedule");
        }
        return res.json();
    },
    async estimateCompetitionSchedule(compID, password, signal) {
        const res = await fetch(`/api/competitions/${compID}/schedule/estimate`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to estimate schedule");
        }
        return res.json();
    },
    // Court (shiaijo) clashes between this competition and every other one
    // (same day, shared court, overlapping time windows). Returns a possibly
    // empty array of ClashWarning objects. Non-blocking: surfaced as a warning.
    async getScheduleClashes(compID, password, signal) {
        const res = await fetch(`/api/competitions/${compID}/schedule/clashes`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to check schedule clashes");
        }
        return res.json();
    },
    // Preview of the number prefix a save would assign when the field is
    // empty (bc-pnum G2/R6): the CREATE form calls this to pre-fill the
    // prefix field with the SAME value assignDefaultNumberPrefix would pick
    // server-side, so what the operator sees before saving matches what
    // would actually land. Create only: the competition doesn't exist yet,
    // so there is nothing to exclude from the taken set. bc-pnum B5: an
    // excludeID parameter used to exist for a settings-screen pre-fill
    // caller that was built and retired before this endpoint's first
    // release; it never had a second caller and is removed, not merely
    // unused.
    async getNumberPrefixDefault(name, password, signal) {
        const params = new URLSearchParams();
        if (name) params.append('name', name);
        const res = await fetch(`/api/number-prefix-default?${params.toString()}`, {
            headers: { 'X-Tournament-Password': password },
            signal,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to derive number prefix");
        }
        return res.json();
    },
    async moveMatchCourt(compID, matchID, court, password, opts = {}) {
        // bc-evts M5: an entry of the match, in its order, answered at once;
        // a refusal comes through `settled` or `opts.onRefusal`.
        // R4: shown at once (_emitLocalMatchPatch), and `opts.was` (the
        // court it had) is put back on a refusal.
        const answer = _outboxWrite({
            kind: 'court', compID, matchID, method: 'PUT',
            url: `/api/competitions/${compID}/matches/${matchID}/court`,
            payload: { court }, password, onRefusal: opts && opts.onRefusal, was: opts && opts.was,
        });
        _emitLocalMatchPatch(compID, matchID, { court });
        return answer;
    },
    async revertMatchToQueue(compID, matchID, password, opts = {}) {
        // bc-evts: the outbox's, with a JSON body carrying writeId, basis and
        // session; `settled` resolves true, or rejects with the refusal.
        return _outboxWrite({
            kind: 'revert', compID, matchID, method: 'POST',
            url: `/api/competitions/${compID}/matches/${matchID}/revert-to-queue`,
            payload: {}, password, seen: opts && opts.seen, onRefusal: opts && opts.onRefusal,
        });
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
    // downstream_knockout_played refusal. The body stays a JSON object (never
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
    async reopenMatch(compID, matchID, password, { reason = "", force = false, seen, onRefusal } = {}) {
        // bc-evts: the outbox's; `settled` resolves the server body or rejects
        // with reopenFailureError's error (court_busy, downstream, ...).
        return _outboxWrite({
            kind: 'reopen', compID, matchID, method: 'POST',
            url: `/api/competitions/${compID}/matches/${matchID}/reopen`,
            payload: reopenBody(reason, force), password, seen, onRefusal,
        });
    },
    // mp-gmcg (review A4): atomically requeue the match holding the court AND
    // reopen the target, in ONE server call under one court lock — replaces the
    // former two-call revert-then-reopen, which raced (a peer could take the
    // freed court between the calls). Surfaces the SAME court_busy detail shape
    // as reopenMatch, so the panel re-offers the remedy when a DIFFERENT match
    // has since taken the court. opts as reopenMatch's: the remedy carries the
    // same reason and confirmation the reopen it retries did.
    async requeueBlockerAndReopen(targetComp, targetMatch, blockerComp, blockerMatch, password, { reason = "", force = false, seen, onRefusal, match, blockerLabel } = {}) {
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
            payload: { blockerCompId: blockerComp, blockerMatchId: blockerMatch, ...reopenBody(reason, force) },
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
    async importCompetitions(formData, password) {
        const res = await fetch('/api/tournament/import', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: formData
        });
        if (!res.ok) {
            const body = await res.json().catch(() => ({}));
            throw new Error(body.error || 'Import failed');
        }
        // An import brings competitions whose matches may reuse ids a label
        // was learned for: none is trusted (K8).
        _forgetAllShownMatches();
        return res.json();
    },
    // Deleting a competition asks for the password again: `confirmPassword` is
    // what the operator typed in that moment, sent beside the sign-in password.
    // A refusal of it is a 401 that carries a `confirm_password_*` code; it is
    // thrown as an Error with that `code` and nothing else (no queue, no parking,
    // no sign-out), so the dialog that asked can show the sentence and ask again.
    async deleteCompetition(id, password, confirmPassword) {
        const res = await fetch(`/api/competitions/${id}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password, 'X-Confirm-Password': confirmPassword ?? '' }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw Object.assign(new Error(err.error || "Failed to delete competition"), { code: err.code });
        }
        // Its match ids are free for a competition made again under the same
        // name (X4); another device hears of it as a tournament_updated push.
        _forgetShownMatches(id);
        return true;
    },
    // Returns the updated competition JSON (including the new `status`) so
    // callers can apply it to local state immediately instead of waiting for
    // the SSE refresh / tournament refetch to land.
    async invalidateCompetition(id, password) {
        const res = await fetch(`/api/competitions/${id}/invalidate`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to invalidate competition");
        }
        return res.json();
    },
    // Replace the full fighting-spirit awards list for a competition.
    // awards: array of { title, recipientName, recipientDojo? }.
    async updateCompetitionAwards(id, awards, password) {
        const res = await fetch(`/api/competitions/${id}/awards`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ fightingSpiritAwards: awards })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to update competition awards");
        }
        return res.json();
    },
    // The GET answers saved: false when nothing is stored; lineupOrNull
    // makes that null ("blank, editable"). A 404 means the competition does
    // not exist and throws. PUT replaces; DELETE clears. The read is of the
    // exact round: the Lineups page reads a team's starting lineup (round 0)
    // this way, and the match surfaces (score sheet, displays, the at-court
    // lineup panel) do not read rounds at all: they read fetchLineupInForce
    // through resolveMatchLineup.
    async fetchTeamLineup(compID, teamId, round) {
        return _readLineup(`/api/competitions/${compID}/teams/${teamId}/lineups/${round}`);
    },
    // memberIds (bc-tmid pass 3) is optional and keyed by the same position
    // as positions: the squad member id half of a lineup, sent alongside
    // the name so the server can persist both together. Omitted entirely
    // when the caller passes nothing, so a caller that never adopted squad
    // members (or an older bundle) round-trips exactly as before.
    //
    // changed (operator decision 2026-10-07, "Only changed positions") names the
    // positions this save changes: the server reads only those and lands them
    // on the lineup it holds when the save arrives, so a position another device
    // changed meanwhile stays, and the answer is the whole lineup it then holds
    // (lineup_save.jsx builds the body: positions and memberIds for the changed
    // positions only). Omitted, the body is a whole lineup that replaces the
    // stored one.
    async putTeamLineup(compID, teamId, round, positions, password, memberIds, changed, opts = {}) {
        // bc-evts M5: an outbox entry under the lineup's own base (a starting
        // lineup belongs to no match), answered at once like every write; a
        // save made while the last one is queued joins it (_coalesceTarget).
        return _outboxWrite({
            kind: 'lineup', compID, matchID: '', method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/lineups/${round}`,
            payload: {
                teamId, competitionId: compID, round, positions,
                ...(memberIds ? { memberIds } : {}), ...(changed ? { changed } : {}),
            },
            password, onRefusal: opts && opts.onRefusal,
            base: _lineupKey(compID, teamId, { round }),
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
    async fetchSquads(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/team-members`, {
            headers: password ? { 'X-Tournament-Password': password } : {}
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load team members");
        }
        const data = await res.json();
        // A member the server lists exists, whatever was known of them.
        return _withListedMembersForgotten(data.teamMembers, compID) || {};
    },
    // The server gives the new member its display index (operator ruling:
    // assigned automatically as members are added); clearTeamMember below is
    // the nearest counterpart and only blanks a name: an index, once minted, is
    // never freed. Every member write's `settled` resolves with the member as
    // the server stamped it, and the server announces the change
    // (lineup_updated, with the team) to every device.
    async addTeamMember(compID, teamId, name, password, opts = {}) {
        // R1: the id is minted here, so the lineup save naming the member is
        // queued behind the add (putMatchLineup's `after`) with the id it will
        // have; a resend of the add is answered as stored. The member shown
        // carries no stamp: nothing holds it yet, and the server's copy, when
        // a list brings it, stands over it.
        const id = _newMemberId();
        const trimmed = String(name || '').trim();
        return _memberWrite({
            compID, teamId, method: 'POST',
            url: `/api/competitions/${compID}/teams/${teamId}/members`,
            payload: { id, name: trimmed }, password,
            member: { ...(opts.index ? { index: opts.index } : {}), id, name: trimmed },
            opts,
        });
    },
    // Keeps memberId's id and index; only the display name changes. Answers 200
    // with the member, stamped.
    async renameTeamMember(compID, teamId, memberId, name, password, opts = {}) {
        // R1: an outbox entry; `opts.member` is the member as shown, which the
        // answer keeps (its number) with the new name, and which a refusal
        // puts back (`revertMember`).
        const trimmed = String(name || '').trim();
        const before = opts.member || { id: memberId };
        return _memberWrite({
            compID, teamId, method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/members/${memberId}`,
            payload: { name: trimmed }, password,
            member: _shownMember(before, { id: memberId, name: trimmed }),
            opts: { ...opts, was: before },
        });
    },
    // The operator's "removal": blanks memberId's Name back to "" and
    // leaves the id and display index untouched, so a bout already fought
    // that names this position keeps meaning the same person (bc-pnum).
    // Answers 200 with the member, nameless and stamped. Refused with a 409
    // once the competition has started (state.ErrTeamMemberClearAfterStart);
    // the server's own message is operator-facing, so it is surfaced verbatim
    // rather than remapped here.
    async clearTeamMember(compID, teamId, memberId, password, opts = {}) {
        // R1: an outbox entry, as renameTeamMember; the member shows unnamed at once.
        const before = opts.member || { id: memberId };
        return _memberWrite({
            compID, teamId, method: 'DELETE',
            url: `/api/competitions/${compID}/teams/${teamId}/members/${memberId}`,
            payload: {}, password,
            member: _shownMember(before, { id: memberId, name: '' }),
            opts: { ...opts, was: before },
        });
    },
    // mp-825 / mp-bkg: per-match lineup endpoints. Match ID takes the
    // place of the round key: successive encounters between the same
    // two teams each carry an independent lineup entry. Nothing here reads
    // the lineup saved for exactly one match: what a team fields at a match
    // is fetchLineupInForce.
    // The lineup a team fields at a match (operator ruling 2026-10-05: a team
    // carries the lineup of its previous match unless one is entered for the
    // match). The server owns the rule (engine/lineup_in_force.go): the
    // match's own lineup, else the latest of the team's earlier matches'
    // lineups, else its starting lineup. `saved: false` -> null (nothing
    // applies). A lineup carries where it was saved: `sourceMatchId` (this
    // match, or an earlier one it is carried from) or `sourceRound` (0, the
    // starting lineup). A 404 means the competition does not exist.
    async fetchLineupInForce(compID, teamId, matchId) {
        return _readLineup(`/api/competitions/${compID}/teams/${teamId}/lineup-in-force/${matchId}`);
    },
    // bc-kfnl: a kachinuki encounter's fighters each side has left, for the
    // advisory line above the live bout (kachinuki_advisory.jsx). The server
    // reads it through the same call that appends the next pairing. Public,
    // bounded like every read (_fetchJson). It NEVER throws: the line is a
    // guide only, so any failure, a timeout, a non-OK answer (400 not
    // kachinuki, 404 unknown match) or an answer without both sides reads as
    // null, and the editor then shows no line.
    async fetchKachinukiRoster(compID, matchID) {
        try {
            const { res, body } = await _fetchJson(
                `/api/competitions/${encodeURIComponent(compID)}/matches/${encodeURIComponent(matchID)}/kachinuki-roster`, {});
            if (!res.ok || !body || !body.sideA || !body.sideB) return null;
            return body;
        } catch (_e) {
            return null;
        }
    },
    // memberIds (bc-pnum gap closure) is optional and keyed by the same
    // position as positions, exactly like putTeamLineup's own memberIds
    // above: the squad member id half of a lineup, sent alongside the name
    // so the server can persist both together. Omitted entirely when the
    // caller passes nothing, so a caller that never adopted squad members
    // (or an older bundle) round-trips exactly as before; the outbox entry
    // carries them, so a replayed lineup write carries the ids too. `changed`
    // names the positions
    // the save changes, as for putTeamLineup above.
    async putMatchLineup(compID, teamId, matchId, positions, password, memberIds, changed, opts = {}) {
        // bc-evts M5: an entry of the MATCH, sent strictly in its order with
        // the match's other writes, and answered at once ({pending, settled}):
        // nothing waits on the network. `settled` resolves with the whole
        // lineup the server holds, or rejects with its refusal (a finished
        // match's 409 carries its code in `error` and its sentence in
        // `message`, kept by _refusalError); `opts.onRefusal` may claim it.
        // writeId, session and basis ride along; the lineup PUT reads none.
        // R1: it follows the team's member writes queued before it (`after`
        // their last entry, strict order on their base), so a member it names
        // by a minted id is added, and a rename lands, before the lineup that
        // places them: _followMintedMembers, as it is queued (K4).
        return _outboxWrite({
            kind: 'lineup', compID, matchID: matchId, method: 'PUT',
            url: `/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`,
            payload: {
                teamId, competitionId: compID, matchId, positions,
                ...(memberIds ? { memberIds } : {}), ...(changed ? { changed } : {}),
            },
            password, onRefusal: opts && opts.onRefusal,
        });
    },
    // Removes the match's own lineup: an outbox entry of the match (kind
    // `lineup-remove`, R1), answered at once and sent in the match's order like
    // every write of it; `settled` resolves once the server has removed it. A
    // lineup that is not there is removed already (a 404 settles as done).
    async deleteMatchLineup(compID, teamId, matchId, password, opts = {}) {
        // R1: an entry of the MATCH, after every write of it queued before it
        // (a save of the lineup included, so no wait for one is needed), and
        // answered at once. A 404 (nothing saved to remove) is the outcome
        // asked for. `settled` resolves true, or rejects with the refusal.
        return _outboxWrite({
            kind: 'lineup-remove', compID, matchID: matchId, method: 'DELETE',
            url: `/api/competitions/${compID}/teams/${teamId}/match-lineups/${matchId}`,
            payload: {}, password, onRefusal: opts && opts.onRefusal,
        });
    },
    // Whether a save of this lineup is still queued. `target` is { matchId } for a
    // match's own lineup or { round } for a team's Lineups-page round (0: its
    // starting lineup). A queued save replays after anything sent now, so a
    // removal made meanwhile would be undone by it: the lineup editors hold their
    // "Use the previous match's lineup" while this is true. Nothing is composed
    // on a queued save: a new save of the lineup joins it and names what it changed.
    queuedLineupSave(compID, teamId, target) {
        return _queuedLineupSave(compID, teamId, target || {});
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
    // T190-T193 (US13: Swiss format). Generate the next Swiss round.
    // Backend pre-conditions: format=swiss; all matches in the current
    // round must be completed; swissCurrentRound must be < swissRounds.
    // Returns { round, matches, swissCurrentRound } on 201, throws on
    // 4xx/5xx. 409 with code="round_incomplete" surfaces a friendly
    // "current round still has incomplete matches" message in the UI;
    // the caller checks `e.code === "round_incomplete"` to branch.
    async swissGenerateRound(compID, password) {
        const res = await fetch(`/api/competitions/${compID}/swiss/generate-round`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            // Preserve the structured 409 payload (code + round) on the
            // thrown Error so the caller can show a precise message. The
            // generic .message stays as the server-reported error string.
            const e = new Error(err.error || "Failed to generate Swiss round");
            if (err.code) e.code = err.code;
            if (err.round !== undefined) e.round = err.round;
            throw e;
        }
        return res.json();
    },
    // T190-T193 (US13). Public endpoint: returns cumulative Swiss
    // standings ranked by wins > points > head-to-head. Returns []
    // when the competition hasn't started yet. Each entry is a
    // state.PlayerStanding (same shape used by pool standings).
    async swissStandings(compID) {
        const res = await fetch(`/api/competitions/${compID}/swiss/standings`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load Swiss standings");
        }
        return res.json();
    },
    async leagueStandings(compID) {
        const res = await fetch(`/api/competitions/${compID}/league/standings`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || "Failed to load league standings");
        }
        return res.json();
    },
    async toggleCheckIn(compID, pid, checkedIn, password) {
        const method = checkedIn ? 'PUT' : 'DELETE';
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/participants/${encodeURIComponent(pid)}/checkin`, {
            method,
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.message || err.error || `Failed to ${checkedIn ? 'check in' : 'undo check-in'} participant`);
        }
        return res.json();
    },
    // Bulk check-in for one competition. participantIds is an array of pids as
    // built by checkinApiPid/rdApiPid: a stable UUID, or "" for a legacy
    // UUID-less row (bc-pnum operator ruling: this write is id only, never
    // the "name|dojo" composite -- name and dojo are operator-editable after
    // the draw, so that composite is not a safe wire identifier). An
    // id-less pid resolves to nothing server-side and is reported back via
    // notFound. Returns { checkedIn, alreadyCheckedIn, notFound }. Used by
    // the Registration desk's "check in a whole dojo" action.
    async bulkCheckIn(compID, participantIds, password) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/participants/checkin-bulk`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify({ participantIds })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.message || err.error || 'Failed to check in participants');
        }
        return res.json();
    },
    async addParticipant(compID, payload, password) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/participants`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to add participant');
        }
        return res.json();
    },
    async replaceParticipant(compID, pid, payload, password) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/participants/${encodeURIComponent(pid)}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(payload)
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || 'Failed to replace participant');
        }
        return res.json();
    },
    async sendAnnouncement(message, durationMinutes, password) {
        const res = await fetch('/api/tournament/announce', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Tournament-Password': password
            },
            body: JSON.stringify({ message, durationMinutes })
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to send announcement (Status ${res.status})`);
        }
        return res.json();
    },
    async fetchAnnouncements() {
        const res = await fetch('/api/tournament/announcements');
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to fetch announcements (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteAnnouncement(id, password) {
        const res = await fetch(`/api/announcements/${encodeURIComponent(id)}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        // 404: the announcement is already gone (expired, or another console
        // withdrew it first), which is the outcome the caller asked for.
        if (res.status === 404) return;
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete announcement (Status ${res.status})`);
        }
    },
    async clearAnnouncements(password) {
        const res = await fetch('/api/announcements', {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password }
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to clear announcements (Status ${res.status})`);
        }
    },
    // mp-c38: sponsor logo upload (multipart) and delete.
    async uploadSponsor({ file, name, link, password }) {
        const fd = new FormData();
        fd.append('name', name);
        if (link) fd.append('link', link);
        fd.append('file', file);
        const res = await fetch('/api/sponsors', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: fd,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to upload sponsor (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteSponsor(index, password) {
        const res = await fetch(`/api/sponsors/${index}`, {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete sponsor (Status ${res.status})`);
        }
    },
    // mp-scf: tournament branding: logo upload/delete.
    async uploadBrandingLogo({ file, password }) {
        const fd = new FormData();
        fd.append('file', file);
        const res = await fetch('/api/branding/logo', {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
            body: fd,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to upload logo (Status ${res.status})`);
        }
        return res.json();
    },
    async deleteBrandingLogo(password) {
        const res = await fetch('/api/branding/logo', {
            method: 'DELETE',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to delete logo (Status ${res.status})`);
        }
    },

    // Phase 3b (mp-8rc9): league tie-breaker operator API.
    //
    // leagueTiebreakCandidates: GET /competitions/:id/league-tiebreak/candidates
    // Returns { candidates: [{teamNames, teams, minPosition, maxPosition}], finalized: bool }
    // where teams is [{id, name, dojo}] (id "" for a legacy competitor): groupTeamIds
    // in admin_pools.jsx derives the teamIds a namesake group needs from it.
    async leagueTiebreakCandidates(compID) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak/candidates`);
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to load league tie-breaker candidates (${res.status})`);
        }
        return res.json();
    },

    // chusenCandidates: GET /competitions/:id/chusen-candidates
    // Consequential team-pool ties the daihyosen could not settle; the operator
    // resolves each by chusen (drawing lots), recorded via overridePoolRanks.
    async chusenCandidates(compID, password) {
        // Lives in the admin-gated competition router, so it needs the
        // admin password header (unlike the public league candidates GET).
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/chusen-candidates`, {
            headers: { 'X-Tournament-Password': password || '' }
        });
        if (!res.ok) {
            // Surface the server-provided error + status so auth/config failures
            // are diagnosable from the admin UI, matching the other helpers.
            const err = await res.json().catch(() => ({}));
            throw new Error(err.error || `Failed to load chusen candidates (${res.status})`);
        }
        const data = await res.json();
        // candidates: ties still waiting for a chusen. recorded: ties a chusen
        // already settled, each with the recorded `ranks` parallel to `teams`,
        // so a chusen recorded in the wrong order can be changed.
        return { candidates: data.candidates || [], recorded: data.recorded || [] };
    },

    // leagueTiebreakGenerate: POST /competitions/:id/league-tiebreak
    // Body: { teamNames: string[], teamIds: string[] }. teamIds is REQUIRED
    // (operator ruling bc-pnum): the server selects the tied group by id
    // only and 400s outright when teamIds is missing, has fewer than two
    // entries, carries a blank entry, or has a duplicate -- teamNames alone
    // is ambiguous the moment two tied teams share a display name across
    // dojos, which this project explicitly allows. The caller MUST NOT
    // invoke this without a real id for every team (admin_pools.jsx gates
    // the "Run tie-breaker" button on groupTeamIds(...) !== undefined for
    // exactly this reason, disabling it rather than sending a request that
    // can only 400).
    // Returns { matches: MatchResult[] } on 201.
    // Throws on 400 (invalid group), 409 (matches already exist).
    async leagueTiebreakGenerate(compID, teamNames, password, teamIds) {
        const body = { teamNames, teamIds };
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to generate league tie-breaker (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },

    // leagueTiebreakRemove: DELETE /competitions/:id/league-tiebreak
    // Body: { teamNames: string[], teamIds: string[] }: the tied group whose
    // unscored matches to remove. teamIds is REQUIRED, same contract as
    // leagueTiebreakGenerate above -- a namesake-holding group can only ever
    // have been CREATED via teamIds, so it must be removable the same way
    // or it could never be deleted again. Same UI-level gating applies: the
    // "Remove unscored tie-breaker" button is disabled when groupTeamIds(...)
    // is undefined.
    // Returns { deleted: number } on 200.
    // Throws on 404 (no matches found), 409 (any match already scored).
    async leagueTiebreakRemove(compID, teamNames, password, teamIds) {
        const body = { teamNames, teamIds };
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak`, {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json', 'X-Tournament-Password': password },
            body: JSON.stringify(body),
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to remove league tie-breaker matches (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },

    // leagueTiebreakFinalize: POST /competitions/:id/league-tiebreak/finalize
    // Accepts the current standings as final without a tie-breaker. Sets the
    // LeagueTiebreakFinalized flag so MaybeAutoCompletePools can transition.
    // Returns { finalized: true } on 200.
    // Throws on 404 (comp not found), 409 (already complete).
    async leagueTiebreakFinalize(compID, password) {
        const res = await fetch(`/api/competitions/${encodeURIComponent(compID)}/league-tiebreak/finalize`, {
            method: 'POST',
            headers: { 'X-Tournament-Password': password },
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({}));
            const e = new Error(err.error || `Failed to finalise league standings (${res.status})`);
            if (err.error) e.code = err.error;
            throw e;
        }
        return res.json();
    },

    // -------------------------------------------------------------------------
    // F2/F3: reconnectEvents: force a fresh SSE connection.
    // Closes the current shared source (if any), resets the reconnect backoff
    // counter, and calls _ensureConnected(). Safe to call repeatedly: the
    // double-connect guard in _ensureConnected() prevents duplicate sources.
    // Exposed as API.reconnectEvents / window.API.reconnectEvents so other
    // modules loaded as window globals can reach it without an ES import.
    // -------------------------------------------------------------------------
    reconnectEvents() {
        if (_sharedSource) {
            _sharedSource.close();
            _sharedSource = null;
            _disarmWatchdog();
        }
        if (_retryTimer) { clearTimeout(_retryTimer); _retryTimer = null; }
        _reconnectAttempt = 0;
        _ensureConnected();
    },

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
        const seq = (_tapSeq.get(base) || 0) + 1;
        _tapSeq.set(base, seq);
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
        if (!_lastEnqueuePersisted) answer.persisted = false;
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
        if (!compID || !matchID || !outcome || !outcome.id || _toldOnBoard.has(outcome.id)) return;
        _toldOnBoard.add(outcome.id);
        const base = _revKey(compID, matchID);
        queueMicrotask(() => {
            _emitOn(_tapOutcomeListeners, base, outcome);
            _notifyQueueAlert({
                kind: 'not_applied', count: 1, terminalCount: 0, compID, matchID,
                outcome, where: _shownMatchLabels.get(base) || '', tap: tap || null,
            });
        });
    },
    // noteShownMatch: the match an editor shows, so a held write of it, or of
    // one of its teams, is named by where it is (H5, _whereOf).
    noteShownMatch(compID, match) {
        _noteShownMatch(compID, match);
    },

    heldWriteKeepsFailing(compID, matchID) {
        return API.heldWriteState(compID, matchID).keepsFailing;
    },
    // heldWriteState: both answers the editors' banner reads, from ONE read
    // of the queue (stage 3 review 5, H2: the two asked apart scanned and
    // sorted it at every step of the walk): `keepsFailing`, whether a write
    // of the match or one it waits on keeps failing, and `blocker`, that
    // write when it is another base's (heldWriteBlocker), else null.
    heldWriteState(compID, matchID) {
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
    },
    // heldWriteBlocker: the write of ANOTHER base this match's writes wait on
    // that the server keeps refusing (stage 3 review 4, G2: a member add a
    // lineup save or a representative pick follows), as a heldWrites() item;
    // null when the match's own writes keep failing (those are its own to
    // say), or when nothing it waits on does. Found through what each entry
    // follows and what its own base sends before it (_blockerOf).
    heldWriteBlocker(compID, matchID) {
        return API.heldWriteState(compID, matchID).blocker;
    },
    discardFailingHeldWrites(compID, matchID) {
        let discarded = 0;
        // What holds the match back from another base (G2), when its own
        // writes are not the ones failing; asked before anything goes.
        const blocker = API.heldWriteBlocker(compID, matchID);
        for (const [key, d] of _entriesFor(_revKey(compID, matchID))) {
            if (_discardIfFailing(key, d)) discarded++;
        }
        if (blocker && _discardIfFailing(blocker.key, _writeQueue.get(blocker.key))) discarded++;
        if (discarded > 0) _afterDiscard();
        return discarded;
    },

    // Every write held on this device, in the order it replays, for the admin
    // topbar's held-writes list (HeldWritesPanel, admin_shell.jsx). It is where
    // a held write with no editor to discard it from is reached: a running
    // autosave, a lineup save or a hand-set winner the server keeps refusing.
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
        _ownStatusListeners.add(fn);
        return () => _ownStatusListeners.delete(fn);
    },
    // The feed shows the move, or a later change: the answered record goes.
    forgetOwnStatusMove(compID, matchID) {
        if (_ownStatusAnswered.delete(_revKey(compID, matchID))) _publishOwnStatus();
    },
    subscribeLocalMatchPatch(cb) {
        return subscribeLocalMatchPatch(cb);
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
        // The reads the owner asked for carry the password the add had, which
        // the server may have refused since: an organiser's reads take the new
        // one (a self-run participant's carries none and stays so), and one the
        // refusal left unread is asked for at its time (N5).
        // A new sign-in asks again every read this tab owes, whatever the
        // earlier tries (Y4, stage 3 review 11): the ones refused for the old
        // password were parked, and failures counted under it may have been
        // its own, so the bound starts again.
        _mintedPaused = false;
        if (password) {
            for (const [id, pw] of _mintedReadPw) if (pw) _mintedReadPw.set(id, password);
            _mintedAuthParked.clear();
            _minted.readsResumed([..._mintedReadPw.keys()]);
            _mintedSweep();
        }
        if (resumed > 0) {
            _persistQueue();
            if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
            _flushAttempt = 0;
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
        _queueGen++;
        let parked = 0;
        for (const d of _writeQueue.values()) {
            if (!d) continue;
            d.password = '';
            d.authBlocked = true;
            parked++;
        }
        if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
        // The members reads too: none goes with the revoked password until
        // the new sign-in (Y4).
        _mintedPaused = true;
        if (_mintedTimer !== null) { clearTimeout(_mintedTimer); _mintedTimer = null; }
        _flushAttempt = 0;
        _offlineFlag = false;
        // Every stored entry too, whether or not this tab holds any.
        _parkStoredEntries();
        // The landed-tap record and the copies seen stay: the same operator
        // signs in again, and is told of a tap turned away meanwhile (stage 6
        // review 8, m10).
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
        _turnedAwaySeen.clear();
        _toldOnBoard.clear();
        _ownStatusAnswered.clear();
        if (discardingTotal > 0) {
            _notifyQueueAlert({ kind: 'discarded', count: discardingTotal, terminalCount: discarding });
        }
        // Bump the generation FIRST so an in-flight _flushQueue loop (mid-await on
        // a network write) aborts instead of sending the rest of its snapshot with
        // the now-revoked password.
        _queueGen++;
        // A hard wipe, every tab's entries included: the credential they carry
        // is revoked, so nothing of the merge's is kept.
        _writeQueue.clear();
        _settled.clear();
        // The wipe takes them out of storage too (P2).
        _settledLeftStored = false;
        _settledHere.clear();
        for (const k of _entryHooks.keys()) _settleKey(k, { value: { discarded: true } });
        for (const b of _pendingTapListeners.keys()) _publishPendingTaps(b);
        try { localStorage.removeItem(QUEUE_STORAGE_KEY); } catch (_e) { /* ignore */ }
        // An older build's queue carries the same revoked password (F4).
        try { localStorage.removeItem(LEGACY_QUEUE_STORAGE_KEY); } catch (_e) { /* ignore */ }
        // What is known of the minted members goes with the queue it is for.
        _minted.clear();
        _mintedReadPw.clear();
        _mintedAuthParked.clear();
        _mintedPaused = false;
        if (_mintedTimer !== null) { clearTimeout(_mintedTimer); _mintedTimer = null; }
        // The copies score-events answers handed back go with the session
        // they were answered to (X4).
        _latestDerived.clear();
        // The landed-tap record goes with the operator: this tab's taps and
        // those of tabs no longer alive (stage 6 reviews 7 and 8).
        _forgetLandedTaps();
        if (_flushTimer !== null) { clearTimeout(_flushTimer); _flushTimer = null; }
        _flushAttempt = 0;
        _offlineFlag = false;
        _recomputeSyncStatus();
    },
};

export {
    API, subscribeSyncStatus, subscribeTerminalWriteFailed, subscribeBracketResync, subscribeQueueAlert,
    subscribeUnsentWrites,
    enqueueRunningWrite,
    writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock,
    SUPERSEDED_REASON, SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE, CLOCK_SKEW_UNHEALED_ADVICE,
    // Exported for tests only: the take-the-older rule is a safety property, and
    // reaching it through a full offline/refuse/reconnect fixture would test the
    // queue rather than the arithmetic. No production consumer outside this file.
    _restampFor,
};

// S2: a new build reloads the page only once the outbox is idle, and the
// queue is persisted first.
setOutboxIdleProbe(_outboxIdle);
setBeforeReload(() => { _persistQueue(); });

if (typeof window !== 'undefined') {
    window.API = API;
    // C2: expose sync-status pub/sub so components loaded as window.* globals
    // (admin_scoring_modal.jsx, etc.) can subscribe without an ES import.
    window.subscribeSyncStatus = subscribeSyncStatus;
    // bc-offl: the held-writes count, for the admin topbar (admin_shell.jsx is
    // script-tagged and cannot ES-import this script-tagged module).
    window.subscribeUnsentWrites = subscribeUnsentWrites;
    // mp-gpra: terminal-write failure pub/sub: lets the score editor show an
    // explicit "not saved" state when a queued terminal write is permanently dropped.
    window.subscribeTerminalWriteFailed = subscribeTerminalWriteFailed;
    window.subscribeQueueAlert = subscribeQueueAlert;
    // No `window` mirror for the not-landed predicates or their banner copy:
    // every consumer imports write_result.jsx directly. That module is
    // import-only (never script-tagged), so even a script-tagged surface can
    // import it without the double-module-eval hazard the mirrors existed to
    // dodge.
    // mp-y3nk: bracket-resync pub/sub: signals AdminShiaijo to refetch when a
    // queued override the server LWW-dropped leaves stale optimistic bracket state.
    window.subscribeBracketResync = subscribeBracketResync;
    // mp-gpra: reconnectEvents and hasPendingTerminalWrite are on window.API
    // (via the API object above): no separate window assignments needed.
}
