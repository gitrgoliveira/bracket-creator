// Owner of ONE question: did a score write actually land, and if not, is the
// state the operator is looking at ever going to become true?
//
// This is a leaf on purpose (no imports, no window reads) so that BOTH kinds of
// consumer can reach the same rule without either being forced into the other's
// loading model:
//
//   - api_client.jsx imports it and re-publishes both predicates on `window`,
//     which is how the script-tagged surfaces (admin_shiaijo, the schedule score
//     editor, viewer_match) ask the question.
//   - admin_scoring_shared.jsx imports it DIRECTLY, because it is itself
//     imported by the scoring modals and by unit tests that never load
//     api_client at all. It cannot import api_client to get here: api_client is
//     script-tagged, and a module that is both script-tagged and ES-imported
//     loads twice under two URLs, splitting its module-level singleton state
//     (the write queue, the listener sets). That is a known, previously-shipped
//     failure in this codebase, so the dependency has to point this way.
//
// Why the rule lives in one place at all: it was previously spelled
// `res.queued` at each call site, and when the server gained a SECOND
// not-landed shape the conversion reached five sites and missed the sixth --
// which happened to be the one guarding a hard prerequisite. A predicate a
// caller must remember to re-derive is a predicate that drifts.

// writeDidNotLand: the write is NOT stored. Two shapes, one meaning --
// `queued` (never reached the server, held for retry) and `applied:false` (the
// server's timestamp last-write-wins guard dropped it because a newer result is
// already recorded).
//
// Ask this before behaving as if the result is now stored: closing an editor,
// advancing to the next match, reporting success.
export function writeDidNotLand(res) {
    return !!res && (res.queued === true || res.applied === false);
}

// SUPERSEDED_REASON / SUPERSEDED_ADVICE: the copy for the one case where
// re-entering is the wrong move (bc-lww1). Every OTHER write failure ends in
// "re-enter the result", and here that is actively wrong: re-entering
// re-stamps the write with the current clock, so it would beat the newer
// stored result and undo it. One owner for this string pair: api_client.jsx's
// `_notifyScoreSuperseded` broadcast uses it, and so does every explicit-tap
// call site that submits with status:"running" -- the shape that broadcast
// deliberately stays silent for (a superseded autosave is routine noise; an
// operator tapping "Start match" or "Record bout" and having it silently do
// nothing is not) and so must build this banner state itself from the
// awaited result instead of relying on the subscription.
export const SUPERSEDED_REASON = 'a newer result for this match is already recorded';
export const SUPERSEDED_ADVICE = 'Check the recorded result before re-entering anything: re-submitting would overwrite the newer one.';

// writeWasSuperseded: the STRONGER half. Both shapes above mean "not stored",
// but they differ on whether the local optimistic state will still come true.
//
// A `queued` write lands on reconnect, so applying it locally is correct -- an
// offline court advancing its own bracket is the only thing that keeps it
// moving. A superseded write NEVER lands: a different writer's result won, so
// anything derived from the operator's version (a bracket advance, a promoted
// next match, a dependent request built on that scoreline) is wrong and must be
// skipped rather than deferred.
//
// Ask writeDidNotLand before treating a result as stored; ask this before
// applying anything derived from it.
export function writeWasSuperseded(res) {
    return !!res && res.applied === false;
}

// CLOCK_SKEW_REASON_TEXT / CLOCK_SKEW_ADVICE: the copy for the OTHER
// not-landed verdict (bc-cse). The server refuses a write whose modifiedAt is
// implausibly far in its own future with 200 {"applied": false, "reason":
// "clock_skew"}, and the remedy is the exact OPPOSITE of the superseded one:
//
//   - superseded  -> a newer result IS stored. Re-entering re-stamps the write
//                    with the current clock, so it would beat that newer result
//                    and undo it. Look first, and only then decide.
//   - clock_skew  -> NOTHING is stored. No other writer won, nothing about the
//                    match changed, and this device's stamp was simply wrong.
//                    Re-entering IS the remedy, and it is safe.
//
// So they must never share a banner. Telling a clock-refused operator to "check
// the recorded result before re-entering" points them at a result that does not
// exist, and tells them not to do the one thing that would save their work.
//
// Owned here, beside the superseded pair, for the same reason that pair is: the
// consumers are api_client.jsx (which re-publishes both on `window`) and the
// explicit-tap call sites in the scoring editors, which build this banner state
// themselves from the awaited result.
export const CLOCK_SKEW_REASON_TEXT = "this device's clock was out of step with the server";
export const CLOCK_SKEW_ADVICE = 'The clock has been resynced. Nothing was recorded, so enter the result again.';

// CLOCK_SKEW_UNHEALED_ADVICE: the same verdict AFTER the client has already
// healed and retried once and been refused again. Everything above still holds
// (nothing is stored, re-entering is safe), but the resync did not fix the
// frame, so promising that it did would send the operator round the identical
// refusal with no idea why. The device itself has to be corrected.
export const CLOCK_SKEW_UNHEALED_ADVICE = 'Nothing was recorded. This device\'s clock needs fixing, or entering the result again will be refused the same way.';

// writeWasRefusedForClock: the NARROW half of writeWasSuperseded. Both are
// `applied === false`, so every advance-skip and banner gate that asks
// writeWasSuperseded keeps treating a clock refusal as not-landed - which is
// right, because a refused write must never advance a bracket whatever the
// reason it was refused for.
//
// This is additive on top of that: ask it FIRST wherever the operator is told
// WHY, so the two verdicts get their own copy (see the pair above). Anywhere the
// question is only "did this land?", writeWasSuperseded remains the right ask.
export function writeWasRefusedForClock(res) {
    return !!res && res.applied === false && res.reason === 'clock_skew';
}
