// start_match.jsx: the ONE owner of how a match START is written and of what
// the answer to it means (bc-aadv, PR #463 round 13).
//
// Two hosts start a match on the operator's behalf: the court console
// (admin_shiaijo.jsx startMatch: the Up next card, a queue row, Finish + Start
// Next and the start after a decision) and the Scores tab
// (admin_schedule_score_editor.jsx: the same two automatic advances). The
// console read the answer and the Scores tab did not, so a start the server
// refused with HTTP 200 {applied:false, reason:'clock_skew'} was taken for one
// that landed there, a thrown 409 (eligibility, court_busy, already_ineligible)
// was swallowed, and nothing stopped a second start while one was out. The
// three rules below are therefore asked here, once, and neither host restates
// them:
//
//   - startPatch(): the write itself.
//   - classifyStartOutcome(res) / startFailureMessage(err): what an answer or a
//     thrown error means, in the operator's words.
//   - createStartGuard(): one start at a time.
//
// Each host keeps its own STATE (the console's startError/refusedTap and its
// startingRef/pickingRef pair, whose pick exemption is the console's alone; the
// Scores tab's startRefusal) and its own surface for the sentence. This leaf
// holds no state of its own beyond the guard object a host creates.
//
// A leaf: write_result.jsx (the answer predicates and the clock-skew words) and
// match_groups.jsx (the group name startPatch writes, itself a near leaf over
// write_result.jsx and result_slot.jsx). Neither host's module is imported
// here, and both import this one directly.

import { writeWasRefusedForClock, CLOCK_SKEW_REASON_TEXT } from './write_result.jsx';
import { GROUP_RESULT } from './match_groups.jsx';

// Minimal "start" patch (status -> running, empty score). Mirrors the editors'
// own buildPatch("running") for an unscored match and works for both individual
// and team matches (subResults is omitted, which the serializer treats as "no
// bouts scored yet"). The server routes it through eng.StartMatchTx, so all
// start-gating (eligibility 409, court_busy, the players check) still runs: a
// 409 throws and the caller reports it.
//
// startOnly (bc-sbq): this write only starts the match, so the server keeps the
// score the match already holds (a match sent back to the queue keeps one);
// toBackendMatchResult leaves the empty scoreline below off the wire. The
// editors' own Start sends their board unflagged, so an operator who cleared
// every mark still clears it.
//
// bc-mrgc: a start changes the status and nothing else, so it names the result
// group alone (match_groups.jsx): the server keeps every other group of the
// match as stored, the score a send-back kept included.
//
// The court console and the Scores tab both import it from here; it is not
// published on `window`.
export function startPatch() {
    return {
        startOnly: true,
        changed: [GROUP_RESULT],
        status: "running", winner: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
        score: { type: "ippon", winnerPts: 0, loserPts: 0, ippons: [], fouls: { a: 0, b: 0 }, live: true, corrected: false },
    };
}

// The sentence for a start the server refused for the device's clock. A
// clock_skew refusal means the server stored NOTHING and, unlike a queued
// start, nothing will land later; the relearn it triggers means a second tap
// normally succeeds, so the sentence says that instead of leaving a dead first
// tap.
export const START_CLOCK_SKEW_MESSAGE = "Could not start: " + CLOCK_SKEW_REASON_TEXT + ". The clock has been resynced; try again.";

// The sentence for a thrown start that carries none of its own.
export const START_FAILED_MESSAGE = "Could not start the match: check eligibility and try again.";

// classifyStartOutcome: what did a start write come back with?
//   { ok: true }          the start landed, or was QUEUED (a queued start lands
//                         on reconnect, so the host treats it as started; the
//                         court console always has), or came back without a
//                         body.
//   { ok: false, msg }    the start was refused for the clock: nothing was
//                         stored and nothing will land later, so a host that
//                         called it started would pin a panel on a match that
//                         never started while the tap looked like it worked.
// Only a clock refusal is a refusal here. A superseded answer (applied:false,
// reason: superseded) reads as started, as the console has always read it: a
// newer change to the match's result is already stored, and the live data the
// host re-reads shows whatever that change made of the match.
// A start that THROWS (a 409) is not an answer at all; startFailureMessage
// words it.
export function classifyStartOutcome(res) {
    if (writeWasRefusedForClock(res)) return { ok: false, msg: START_CLOCK_SKEW_MESSAGE };
    return { ok: true };
}

// startFailureMessage: the sentence for a start that threw. The server's own
// words when it sent any ("Alice withdrew in Pool A · Match 1 and cannot
// fight again. ..."), else one fallback.
export function startFailureMessage(err) {
    return (err && err.message) || START_FAILED_MESSAGE;
}

// startRefusalStands: does a stored Start refusal still hold? A refusal is about
// one match at one moment, so it falls when its row has left `scheduled`, or
// when a list that holds matches no longer holds it (the match moved), and a
// stale one never revives when that match is later sent back to the queue. An
// EMPTY list says nothing about the match (it is empty while a feed loads and
// across a transient empty refetch), so the refusal stands then. `row` is the
// refused match's live row in the host's list, or null when the list lacks it;
// `listHoldsMatches` is whether that list holds any match at all. The court
// console and the Scores tab both ask this, so they cannot disagree on how long
// the notice lives.
export function startRefusalStands({ row, listHoldsMatches }) {
    if (row) return row.status === "scheduled";
    return !listHoldsMatches;
}

// createStartGuard: ONE start at a time. A host creates one and brackets every
// start it makes with begin/end.
//
//   begin(key, match)  { started: true }                        the start may go ahead
//                      { started: false, repeat: true, blocker } the SAME match is already being
//                                                                 started: it is on its way, the
//                                                                 repeat is dropped silently
//                      { started: false, repeat: false, blocker } ANOTHER match is being started:
//                                                                 the host refuses this one and
//                                                                 says so (startWhileStartingMessage,
//                                                                 write_result.jsx) naming
//                                                                 blocker.match
//   end(key)           frees the guard, but only for the start that holds it.
//   inFlight()         the { key, match } being started, or null.
//
// The console keeps its own startingRef/pickingRef pair instead: a pick
// (pickMatch) is exempt from its own guard and defers the running bout before
// its start, which this guard has no word for. The Scores tab has no pick.
export function createStartGuard() {
    let current = null;
    return {
        begin(key, match) {
            if (current) {
                return { started: false, repeat: current.key === key, blocker: current };
            }
            current = { key, match };
            return { started: true };
        },
        end(key) {
            if (current && current.key === key) current = null;
        },
        inFlight() {
            return current;
        },
    };
}
