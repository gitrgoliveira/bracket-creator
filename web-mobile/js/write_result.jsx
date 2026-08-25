// Owner of ONE question: did a score write actually land, and if not, is the
// state the operator is looking at ever going to become true?
//
// EVERY consumer imports this module directly -- api_client.jsx, the two
// scoring editors (admin_scoring_team, admin_scoring_individual),
// admin_scoring_shared.jsx, admin_shiaijo.jsx, the schedule score editor and
// viewer_match.jsx. Nothing reads these names off `window`: the mirrors
// api_client used to publish are gone, so there is exactly one binding per
// name and no second spelling to drift.
//
// This is a leaf on purpose (no imports, no window reads), and it is
// import-only: it has no <script type="module"> tag of its own and must never
// gain one. A module that is BOTH script-tagged and ES-imported loads twice
// under two URLs and splits its module-level singleton state -- a known,
// previously-shipped failure here (mp-zd1v), and the reason a script-tagged
// surface like admin_shiaijo.jsx cannot import api_client.jsx to reach this
// rule. Because THIS file is never tagged, importing it is safe from either
// kind of module, which is what makes the direct import available everywhere
// and the `window` mirrors unnecessary.
//
// Prefer the import over any re-derivation of the test: two of the shiaijo call
// sites sit inside a swallowing catch, where a missing binding would degrade
// into the silent not-saved failure these predicates exist to remove.
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
// `_notifyScoreSuperseded` broadcast uses it, and so does notLandedBanner at
// the foot of this file -- the answer for every explicit-tap call site that
// submits with status:"running", the shape that broadcast deliberately stays
// silent for (a superseded autosave is routine noise; an operator tapping
// "Start match" or "Record bout" and having it silently do nothing is not) and
// which therefore builds this banner state from the awaited result instead of
// relying on the subscription.
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
// consumers are api_client.jsx and notLandedBanner below, which is what the
// explicit-tap call sites in the scoring editors ask to turn an awaited result
// into this banner state.
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

// notLandedBanner: the ONE owner of "which not-saved banner does this result
// deserve?", answering with the { reason, advice } pair the scoring editors put
// straight into their writeFailed state, or null when there is nothing to say.
//
// The three explicit-tap sites (Start match in both editors, Record bout in the
// team one) each held the same if/else chain: ask writeWasRefusedForClock
// first, fall back to writeWasSuperseded, say nothing otherwise. That ORDER is
// the whole point and is easy to paste wrong -- both verdicts are
// `applied === false`, so testing the broad one first swallows the narrow one
// and tells a clock-refused operator to go and look at a newer result that does
// not exist. A rule three call sites must remember to re-derive is the rule
// that drifted before (see the note at the top of this file).
//
// null covers BOTH remaining cases, and they are different on purpose:
//   - the write landed: nothing to report.
//   - the write was QUEUED: not stored yet, but it will be. The editor's
//     queued/offline surface already owns that state, so these taps say
//     nothing about it -- exactly what the pasted chains did, since `queued`
//     satisfies neither predicate.
export function notLandedBanner(res) {
    if (writeWasRefusedForClock(res)) {
        return { reason: CLOCK_SKEW_REASON_TEXT, advice: CLOCK_SKEW_ADVICE };
    }
    if (writeWasSuperseded(res)) {
        return { reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE };
    }
    return null;
}
