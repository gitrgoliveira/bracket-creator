// The clock: the revision counter, the server-clock learn and relearn, and the
// hybrid logical clock that stamps every write (_stampAfterSeen, _afterOwnStamp).
//
// A piece of the outbox that api_client.jsx boots (bc-jsxs). Declaration-only: at
// load it runs nothing but literals, its own earlier declarations and calls into
// leaves (api_client_ids.jsx and the modules outside this family), so the import
// cycles between the pieces are safe (check-load-cycles.mjs). The load-time
// statements, the API object and the window mirrors live in api_client.jsx; never
// import api_client.jsx from here (it would run the outbox twice).

import { _randomId } from './api_client_ids.jsx';
// The offset lives in a leaf so a score editor can read the same clock (server_clock.jsx).
import { serverNowMs, serverClockOffsetMs, setServerClockOffsetMs } from './server_clock.jsx';
import { fetchWithTimeout } from './api_client_requests.jsx';
import { _revKey, _baseOf, _entryStamp, _lastSettledStamp } from './api_client_queue.jsx';
import { provide } from './api_client_store.jsx';

// ---------------------------------------------------------------------------
// mp-y3nk: server-clock offset for timestamp reconciliation.
//
// Writes (score / decisions) are stamped in SERVER-RELATIVE time so an
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
export let _clockOffsetLearned = false;
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
export function _learnServerClockOffset() {
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
export function _relearnClockThrottled() {
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
export function _serverNowMs() { return serverNowMs(); }

// Monotonic reading, or null where performance.now() is unavailable (older
// embedded webviews, and the non-browser imports the test harness does). Null
// simply means "no monotonic anchor", which every caller already handles: the
// wall-clock reconstruction is the fallback.
export function _perfNow() {
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
// a representative bout's argument or a score
// write's patch), never the match at the send: a tap made before another
// device finished the match stays older than that finish (operator ruling
// 2026-09-27). 0 or absent: no floor.
const HLC_FLOOR_MAX_AHEAD_MS = 4000;
export function _stampAfterSeen(stamp, seen) {
    const s = Number(seen);
    if (!Number.isFinite(s) || s <= 0 || stamp > s) return stamp;
    return Math.max(stamp, Math.min(s + 1, _serverNowMs() + HLC_FLOOR_MAX_AHEAD_MS));
}
// What a score write was made against: the `seenModifiedAt` its patch carries
// (even 0: the editor saw no stamp), else the match handed in.
export function _seenOfScore(result, match) {
    if (result && result.seenModifiedAt !== undefined) return Number(result.seenModifiedAt) || 0;
    return Number(match && match.modifiedAt) || 0;
}
// The seen stamp a payload was floored by, kept beside it (never sent) so the
// queue can record it on the entry (_commitEnqueue, seenModifiedAt) and a
// re-stamped replay keeps the floor (_restampQueuedEntryForSkew).
export const _payloadSeen = new WeakMap();
export function _stampPayload(payload, stamp, seen) {
    payload.modifiedAt = _stampAfterSeen(stamp, seen);
    const s = Number(seen);
    if (Number.isFinite(s) && s > 0) _payloadSeen.set(payload, s);
    return payload;
}
// _carryPayloadSeen: a copy of a payload keeps the seen stamp the original was
// floored by (_withoutDownstreamConfirmation). The one write of _payloadSeen
// from outside the clock code.
export function _carryPayloadSeen(from, to) {
    if (_payloadSeen.has(from)) _payloadSeen.set(to, _payloadSeen.get(from));
}

// _perfAgeMs: whole milliseconds since a monotonic reading, or null when there
// is none (no reading, or no performance.now()). Rounded UP, for two reasons:
// the server reads modifiedAt as an int64 and refuses a fraction with a 400,
// and a stamp built as "server now minus this age" then never lands later than
// the moment it stands for, which is the safe direction (see _restampFor).
export function _perfAgeMs(reading) {
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
// a response test re-derived per call site is a response test that drifts.
// The operator-facing copy lives in write_result.jsx beside the SUPERSEDED
// pair, not here: it is a VERDICT string, and the whole point of this response
// is that its verdict is the opposite of the superseded one. A local copy of it
// drifted almost immediately - it kept the superseded advice ("check what is
// recorded") under a comment claiming it named the clock - which is exactly the
// failure one owner prevents. This module keeps only the one reason string
// that has no consumer outside the queue: the detail of the alert that says a
// write is held for the clock (_resendForClock).
export const CLOCK_SKEW_FAIL_REASON = "this device's clock is out of step with the server: check its date and time";

// Heartbeat tripwire threshold. Deliberately BELOW the server's 5s refusal
// margin: the tripwire is meant to fire and heal while writes are still being
// accepted, not after the first result has already been refused.
export const CLOCK_TRIPWIRE_MS = 3000;

const _matchRevCounters = new Map(); // compID:matchID → int
export function _nextRev(compID, matchID) {
    const key = _revKey(compID, matchID);
    const next = (_matchRevCounters.get(key) || 0) + 1;
    _matchRevCounters.set(key, next);
    return next;
}
// _forgetRev: a finished score write landed, so the match's next running write
// starts its revisions again (_sendEntry).
export function _forgetRev(compID, matchID) {
    _matchRevCounters.delete(_revKey(compID, matchID));
}

// Per-page-load scoring-session id. Identifies one client session so the
// server's rev-guard can drop a single session's own out-of-order delivery
// (a reconnect flush). Different sessions are concurrent operators: the
// server treats those as last-write-wins.
export const _revSession = _randomId();
const _lastStampFor = new Map();

// _afterOwnStamp: this device's next stamp for the match, never at or before
// the last one it gave a write of that match.
export function _afterOwnStamp(base, stamp) {
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
export function _noteOwnStamp(key, d) {
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
export function _afterSettledStamp(base, stamp) {
    const settled = _lastSettledStamp.get(base) || 0;
    const s = stamp > settled ? stamp : settled + 1;
    if (s > (_lastStampFor.get(base) || 0)) _lastStampFor.set(base, s);
    return s;
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

// The calls lower modules make into this one (api_client_store.jsx).
provide({ _relearnClockThrottled });
