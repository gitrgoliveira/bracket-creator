// Owner of ONE question: did a score write actually land, and if not, is the
// state the operator is looking at ever going to become true?
//
// EVERY consumer imports this module directly -- api_client.jsx, the three
// scoring editors (admin_scoring_team, admin_scoring_individual,
// admin_scoring_engi), admin_scoring_shared.jsx, admin_shiaijo.jsx, admin.jsx
// (the single editMatchScore chokepoint every score-editor host routes
// through), the schedule score editor, viewer_match.jsx, app.jsx (the queue
// alerts) and admin_shell.jsx (the topbar's held-writes indicator). Nothing
// reads these names off `window`: the mirrors api_client used to publish are
// gone, so there is exactly one binding per name and no second spelling to drift.
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

// writeKeepsEditorOpen: does a score editor's host leave the editor open after
// this write (bc-plcl)? Every host asks it, so none hand-writes its own answer
// (the Pools tab once closed on every write and the bracket panel on a
// correction that never landed).
//   - A write that did not land (queued, superseded, clock_skew) keeps the
//     entry on screen with its not-saved banner.
//   - A running write with no winner (Start match, a per-point autosave, a
//     kachinuki Record bout) leaves the bout live on the board.
//   - Anything else (a finish, a correction, a draw) is the editor's job done.
export function writeKeepsEditorOpen(patch, res) {
    return writeDidNotLand(res) || (!!patch && patch.status === "running" && !patch.winner);
}

// SUPERSEDED_LEAD / SUPERSEDED_REASON / SUPERSEDED_ADVICE: the copy for the one
// case where re-entering is the wrong move (bc-lww1). Every OTHER write failure
// ends in "re-enter the result", and here that is actively wrong: re-entering
// re-stamps the write with the current clock, so it would beat the newer
// stored change and undo it. One owner for these strings: api_client.jsx's
// `_notifyScoreSuperseded` broadcast uses them, and so does notLandedBanner at
// the foot of this file -- the answer for every explicit-tap call site that
// submits with status:"running", the shape that broadcast deliberately stays
// silent for (a superseded autosave is routine noise; an operator tapping
// "Start match" or "Record bout" and having it silently do nothing is not) and
// which therefore builds this banner state from the awaited result instead of
// relying on the subscription.
//
// bc-mrgc (operator ruling 2026-10-03: "Nothing should be dropped. All events
// must be ordered."): a superseded write is no longer lost. The server merges
// a write group by group, and a change older than a stored change to the same
// thing is kept in the match's history instead of applied, so the banner
// leads with "Not applied", not "Not saved", says nothing is lost, and sends
// the operator to the match and its history before entering anything again.
export const SUPERSEDED_LEAD = 'Not applied';
export const SUPERSEDED_REASON = "a newer change to the same thing was recorded first, so this one was kept in the match's history and nothing is lost";
export const SUPERSEDED_ADVICE = 'Check the match and its history before entering anything again: entering it again would replace the newer change.';

// supersededAlertText: the queue alert for finished results a replay found
// superseded (app.jsx queueAlertMessage), worded like the banner above. `n` is
// how many, `one` whether that is a single result (queuedWritesNoun),
// `needsWinner` whether any was held because it would leave a finished match
// without a winner (writeNeedsWinner).
export function supersededAlertText(n, one, needsWinner = false, defaultWinStands = false, decision = null) {
    const text = one
        ? "A result was not applied because a newer change to the same match was recorded first. It was kept in the match's history, so nothing is lost: check the match and its history before entering anything again."
        : `${n} results were not applied because newer changes to the same matches were recorded first. They were kept in each match's history, so nothing is lost: check those matches and their history before entering anything again.`;
    if (needsWinner) {
        // At least one was held because it would leave a finished match
        // without a winner (writeNeedsWinner): for that one, nothing newer won.
        return one
            ? "A result was not applied because it would leave the finished match without a winner, and it needs one. It was kept in the match's history, so nothing is lost: correct the result with a winner."
            : `${text} Where one would leave a finished match without a winner, correct that result with a winner.`;
    }
    if (defaultWinStands) {
        // At least one was held because a decision already closed the match
        // (writeDefaultWinStands): the fix is to correct that decision from
        // the match's score editor, never re-entering the score. `decision`
        // names it when the whole pass held exactly one kind
        // (defaultWinStandsWord falls back to "recorded decision" for a
        // mixed pass or an older server with no heldDecision).
        const word = defaultWinStandsWord(decision);
        return one
            ? `A result was not applied because this match was closed with a ${word}. It was kept in the match's history, so nothing is lost: correct the ${word} from the match's score editor to change the result.`
            : `${text} Where one was closed with a ${word}, correct the ${word} from the match's score editor to change that result.`;
    }
    return text;
}

// writeNeedsWinner / supersededBanner / NEEDS_WINNER_* (operator ruling
// 2026-10-04): a scoring change that would leave a FINISHED match without the
// winner it must have (a knockout match left tied, an engi match left with no
// valid flag count) is not applied. The match keeps its recorded finish, the
// change is kept in the match's history, and the server says why with
// heldReason "needs_winner", on a superseded answer and on one applied in
// part alike. Here the advice is the opposite of the plain superseded one:
// nothing newer won, so the operator corrects the result, with a winner.
// writeNeedsWinner is the one reading of that field; supersededBanner is the
// one choice of banner for a superseded write, asked by notLandedBanner and by
// api_client.jsx's not-applied broadcast.
//
// The same reason rides on a second, APPLIED answer (writeDisplacedGroups): a
// finish that arrived after a newer scoring change which, applied after it,
// would have left the match without a winner. The finish is recorded and that
// later change is MOVED to the history (`displacedGroups`, no heldGroups).
// writeNeedsWinner is therefore true only for a write whose OWN change was
// held for the reason; a displaced answer is not one, and says "Saved". An
// answer that both held groups and moved a later change carries the one
// reason for both, and the client reads it as the move's: telling the
// operator to correct a result that IS recorded would be the worse error.
export const HELD_REASON_NEEDS_WINNER = 'needs_winner';
export function writeDisplacedGroups(res) {
    return res && Array.isArray(res.displacedGroups) ? res.displacedGroups.filter((g) => typeof g === 'string') : [];
}
export function writeNeedsWinner(res) {
    return !!res && res.heldReason === HELD_REASON_NEEDS_WINNER
        && writeDisplacedGroups(res).length === 0
        && (writeWasSuperseded(res) || writeHeldGroups(res).length > 0);
}
// displacedAlertText: the queue alert for queued finishes that landed and
// moved a later change of the same match to its history (writeDisplacedGroups).
export function displacedAlertText(n, one) {
    return one
        ? "A held result was saved. A later change to that match would have left it without a winner, so the change was moved to the match's history."
        : `${n} held results were saved. Later changes to those matches would have left them without a winner, so the changes were moved to each match's history.`;
}
export const NEEDS_WINNER_REASON = "this change would leave the finished match without a winner, and it needs one, so it was kept in the match's history and nothing is lost";
export const NEEDS_WINNER_ADVICE = 'Correct the result with a winner.';
// The sentence the partial-apply note (heldGroupsNote) ends on instead of
// "A newer change to the same thing was recorded first."
export const NEEDS_WINNER_NOTE = 'It would leave the finished match without a winner, and it needs one: correct the result with a winner.';

// decisionWord / FALLBACK_DECISION_WORD (operator ruling 2026-10-04): "default
// win" does not exist in kendo and must never appear in anything a person
// reads -- every finished match has a result, a scoreline or a registered
// decision that NAMES the winner (kiken: kiken-voluntary or kiken-injury;
// fusenpai; fusensho; hantei). decisionWord is the ONE map from a wire
// decision code to the bare word the operator is told: "kiken" for any kiken
// variant (the legacy bare "kiken" included), "fusenpai", "fusensho", or null
// for anything outside that class (fought, hikiwake, daihyosen,
// kachinuki-exhaustion, ippon-shobu, no decision). Every caller that used to
// say "default win" now names the recorded decision through here instead --
// admin_scoring_shared.jsx's withdrawalLabel derives its fusenpai/fusensho
// words from it rather than restating the map.
export function decisionWord(code) {
    if (code === 'kiken' || code === 'kiken-voluntary' || code === 'kiken-injury') return 'kiken';
    if (code === 'fusenpai') return 'fusenpai';
    if (code === 'fusensho') return 'fusensho';
    return null;
}

// FALLBACK_DECISION_WORD: what a sentence names when there is no specific
// decision to name -- an older server answering with no `heldDecision`, or a
// flushed queue pass that held more than one kind across several matches, so
// naming the wrong one would be worse than naming none.
const FALLBACK_DECISION_WORD = 'recorded decision';

// defaultWinStandsWord: decisionWord with that fallback applied, the one
// place DEFAULT_WIN_STANDS_* and supersededAlertText read a decision code
// from.
function defaultWinStandsWord(decision) {
    return decisionWord(decision) || FALLBACK_DECISION_WORD;
}

// writeHeldDecision: the decision code the server named alongside heldReason
// "default_win_stands" (e.g. "fusensho", "kiken-voluntary"), read for the
// writeDefaultWinStands case only -- null for anything else, including an
// older server that sends no `heldDecision` at all, in which case callers
// fall back to FALLBACK_DECISION_WORD through defaultWinStandsWord.
export function writeHeldDecision(res) {
    return (res && typeof res.heldDecision === 'string' && res.heldDecision) || null;
}

// writeDefaultWinStands / DEFAULT_WIN_STANDS_* (bc-mrgc): a running board's
// scoring, or its overtime, over a match a decision ALREADY closed -- kiken,
// fusenpai, or a fusensho awarded because the OTHER side is barred by a
// DIFFERENT match -- is held rather than applied: that decision already
// settled this match, and a scoreline cannot land beside it without one
// discarding the other. The server says why with heldReason
// "default_win_stands" and names which decision with `heldDecision`. Unlike
// needs_winner, nothing here asks for a correction with a winner: the match
// already has one. The fix is to correct that decision from the match's
// score editor, which sends the held scoring on as the real result instead.
// The copy names no specific button: which control does that (Clear
// <decision>, Remove <decision>, or none at all on a kachinuki match)
// depends on the match's format and is owned by admin_scoring_shared.jsx,
// not restated here.
export const HELD_REASON_DEFAULT_WIN_STANDS = 'default_win_stands';
export function writeDefaultWinStands(res) {
    return !!res && res.heldReason === HELD_REASON_DEFAULT_WIN_STANDS
        && writeDisplacedGroups(res).length === 0
        && (writeWasSuperseded(res) || writeHeldGroups(res).length > 0);
}
export function DEFAULT_WIN_STANDS_REASON(decision) {
    const word = defaultWinStandsWord(decision);
    return `this match was closed with a ${word}, so this change was kept in the match's history and nothing is lost`;
}
export function DEFAULT_WIN_STANDS_ADVICE(decision) {
    const word = defaultWinStandsWord(decision);
    return `To change the result, correct the ${word} from the match's score editor.`;
}
export function DEFAULT_WIN_STANDS_NOTE(decision) {
    const word = defaultWinStandsWord(decision);
    return `This match was closed with a ${word}: to change the result, correct the ${word} from the match's score editor.`;
}

export function supersededBanner(res) {
    if (writeNeedsWinner(res)) {
        return { lead: SUPERSEDED_LEAD, reason: NEEDS_WINNER_REASON, advice: NEEDS_WINNER_ADVICE };
    }
    if (writeDefaultWinStands(res)) {
        const decision = writeHeldDecision(res);
        return { lead: SUPERSEDED_LEAD, reason: DEFAULT_WIN_STANDS_REASON(decision), advice: DEFAULT_WIN_STANDS_ADVICE(decision) };
    }
    return { lead: SUPERSEDED_LEAD, reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE };
}

// OVERRIDE_HELD_NOTICE (bc-mrgc phase 3): the court console's toast when a
// winner the operator picked for an unresolved feeder (Resolve feeders) was
// not applied because a newer result for that match was recorded first. Like
// any superseded write it was kept in the match's history, not lost, and the
// court refreshes to show what is recorded.
export const OVERRIDE_HELD_NOTICE = "A winner you picked was not applied: a newer result for that match was recorded first. Your pick was kept in the match's history. Refreshing this court to show the current state.";

// writeHeldGroups / writePartlyHeld (bc-mrgc): which groups of a write the
// server kept in the match's history instead of applying it, because a newer
// change to the same group was already recorded. The server lists them in
// `heldGroups` on BOTH answers that can hold any: a write applied in part (the
// rest landed, applied is not false) and a superseded one (nothing landed).
// writePartlyHeld is the first of those alone, the one a score editor answers
// with a quiet note while its flow carries on; a superseded write gets the
// banner above instead (notLandedBanner).
export function writeHeldGroups(res) {
    return res && Array.isArray(res.heldGroups) ? res.heldGroups.filter((g) => typeof g === 'string') : [];
}
export function writePartlyHeld(res) {
    return !!res && res.applied !== false && writeHeldGroups(res).length > 0;
}

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

// writeWasRefused: did a score editor's write come back REFUSED, stored
// nowhere and never going to be? Asked on what the host handed back, so a
// thrown refusal counts as well: every host catches it, reports it (a toast,
// an alert) and hands the editor nothing. Two shapes:
//   - nothing handed back: the host already reported a refusal (a busy
//     shiaijo, a finished match, a barred competitor, a validation error).
//   - applied:false: superseded or clock_skew (writeWasSuperseded).
// A QUEUED write is not refused: it lands on reconnect.
//
// The editors ask it to DISARM a two-tap commit (Finish, Finish + Start Next,
// End match, the engi Save) whose write was refused: left armed, one more tap
// re-sent the very write just refused, and for a superseded one that tap
// would overwrite the newer result. A queued commit stays armed, since
// sending it again is what the pending banner's Retry now does anyway.
//
// One host shape reads as refused without being one: the court console's and
// the Scores tab's Finish + Start Next return nothing once the finish has
// landed and the next match is started. Disarming there costs nothing, since
// the editor has moved on to the next match.
export function writeWasRefused(res) {
    return !res || writeWasSuperseded(res);
}

// writeRetryable: can sending this write again make it land? The ONE owner of
// whether a score editor offers Retry. Only a QUEUED write can: it never
// reached the server (offline, a timeout, a 5xx), the queue keeps sending it,
// and Retry only sends it sooner. Nothing else a write can come back with is
// fixed by sending the same write again:
//   - superseded: a newer result is stored, and a re-send, stamped now, would
//     overwrite it (SUPERSEDED_ADVICE).
//   - clock_skew: the client has already resynced and re-sent it; entering
//     the result again is the remedy (CLOCK_SKEW_ADVICE), not a replay.
//   - any other refusal (the match has finished, the organiser alone decides
//     the rep bout, a competitor is barred, the shiaijo is busy, a validation
//     error): the server answers it the same way until something else
//     changes, and then the operator enters the result again.
// So an editor keeps the write to re-send only for a queued one, and the
// not-saved banner (always a refusal: notLandedBanner, terminalFailureBanner)
// never carries Retry.
export function writeRetryable(res) {
    return !!res && res.queued === true;
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
    if (writeWasSuperseded(res)) return supersededBanner(res);
    return null;
}

// terminalFailureBanner: the banner a score editor raises for a queued write
// that failed for good (subscribeTerminalWriteFailed), in the same shape as
// notLandedBanner's, plus `sentence` when the refusal is a whole sentence that
// says what to do (api_client.jsx _replayRefusal), and `lead` when the write
// was not lost (a superseded replay, kept in the match's history). One owner,
// so no editor drops the mark.
export function terminalFailureBanner(info) {
    return {
        reason: info.reason || `save rejected (${info.status || 'error'})`,
        advice: info.advice,
        ...(info.sentence ? { sentence: true } : {}),
        ...(info.lead ? { lead: info.lead } : {}),
    };
}

// notSavedText: the ONE line every not-saved banner shows for a
// { reason, advice, sentence, lead } set: "Not saved: <reason>. <advice>", with
// the default advice to re-enter when none is given. A refusal that is a whole
// sentence (`sentence`: the server's own words, or the busy-shiaijo copy) is
// shown as it is after "Not saved:": it ends its own
// sentence and says what to do, so a full stop and advice after it doubled the
// stop and could contradict it ("Re-enter the result" after "Check the scores
// and finish again"). `lead` replaces "Not saved" for a write that was kept
// rather than lost (SUPERSEDED_LEAD, bc-mrgc).
export const NOT_SAVED_ADVICE = "Re-enter the result and submit again.";
export function notSavedText(failed) {
    const lead = failed.lead || 'Not saved';
    if (failed.sentence) return `${lead}: ${failed.reason}`;
    return `${lead}: ${failed.reason}. ${failed.advice || NOT_SAVED_ADVICE}`;
}

// QUEUED_NOTICE: the ONE line a score editor (and the barred-match notice)
// shows for a write that did not reach the server and is HELD on this device:
// it will land on its own once the connection returns. Worded as the docs word
// it ("saved on the device and sent when the connection returns"), and never
// "Not saved", which the docs and notSavedText reserve for a REFUSED write that
// will never land. Three hand-typed wordings existed before this owner.
export const QUEUED_NOTICE = 'Not sent yet: saved on this device, and sent when the connection returns.';

// QUEUED_UNSAVED_NOTICE / queuedNotice: the same held write when the browser
// could not store it (its storage is full or blocked; api_client answers the
// write with `persisted: false` and raises the storage_full alert). It is
// held in this page's memory only, so "saved on this device" would be false:
// a reload or a closed tab loses it. queuedNotice is the ONE choice between
// the two, asked with the queued answer the editor kept (or `true` when it
// only knows a write is pending, e.g. reopened over a queued Finish).
export const QUEUED_UNSAVED_NOTICE = 'Not sent yet: keep this page open until the connection returns.';
// QUEUED_REFUSED_NOTICE: the same held write once the server has refused it
// past the notice threshold (a server error on every attempt). The connection
// is fine then, so "sent when the connection returns" would be false; the
// editor offers to discard it beside this line (HeldWriteDiscard).
export const QUEUED_REFUSED_NOTICE = 'Not saved yet: the server keeps refusing it. It is still being tried.';
export function queuedNotice(res, { keepsFailing = false } = {}) {
    if (keepsFailing) return QUEUED_REFUSED_NOTICE;
    return res && typeof res === 'object' && res.persisted === false ? QUEUED_UNSAVED_NOTICE : QUEUED_NOTICE;
}

// HELD_WRITE_DISCARD_LABEL / heldWriteDiscardConfirm: the way past a held
// write the server keeps refusing (api_client.jsx discardFailingHeldWrites).
// It holds back no other write, but it is retried as long as the page is
// open, so the editor's pending banner offers to discard it, and only it,
// after this confirm. Named "result" as the operator counts it.
export const HELD_WRITE_DISCARD_LABEL = 'Discard held result';
export function heldWriteDiscardConfirm(held) {
    // `held` (from the topbar's list) names any other kind of held write; the
    // editors' button, which discards a result, passes nothing.
    const what = held ? heldWriteWhat(held) : 'result';
    const where = held && held.kind === 'lineup' ? 'the lineup' : 'the match';
    return {
        message: `The server keeps refusing the ${what} held on this device${held ? '' : ' for this match'}, so it may never be sent. `
            + `Discard it? Nothing else is discarded. Check ${where} afterwards, and enter it again if it is still needed.`,
        confirmLabel: 'Discard it',
        danger: true,
    };
}

// heldWriteWhat: what one held write is, in the operator's words, for the
// topbar's held-writes list and its discard confirm. `held` is an item of
// API.heldWrites(): a running autosave is a score update, everything else
// the result or setting it carried.
export function heldWriteWhat(held) {
    switch (held && held.kind) {
        case 'decision': return 'decision';
        case 'override': return 'winner set by hand';
        case 'lineup': return 'team lineup';
        default: return held && held.terminal ? 'finished result' : 'score update';
    }
}

// heldWriteState: where one held write stands, for the same list.
export function heldWriteState(held) {
    if (held && held.keepsFailing) return 'the server keeps refusing it';
    if (held && held.authBlocked) return 'waiting for you to sign in';
    return 'waiting to be sent';
}

// The topbar's held-writes list (HeldWritesPanel, admin_shell.jsx): its
// title, what it says when nothing is held, its per-row discard, and how it
// names a lineup (a lineup save is about a team, not a match).
export const HELD_WRITES_TITLE = 'Held on this device';
export const HELD_WRITES_EMPTY = 'Nothing is held on this device: everything has been sent.';
export const HELD_WRITE_DISCARD_ONE_LABEL = 'Discard';
export function heldLineupLabel(teamName) {
    return teamName ? `${teamName}'s lineup` : 'A team lineup';
}

// heldWriteLine: the list row's second line, what it is and where it stands,
// e.g. "Score update: the server keeps refusing it".
export function heldWriteLine(held) {
    const what = heldWriteWhat(held);
    return `${what.charAt(0).toUpperCase() + what.slice(1)}: ${heldWriteState(held)}`;
}

// queuedWritesNoun: the ONE rule for how held writes are counted to the
// operator. An operator counts results, and a held running autosave is not
// one, so a count that includes any finished result (a completed score, a
// decision, a lineup, a Run now winner) names only those; otherwise it names
// score updates. Returns { n, one, noun }. `finished` qualifies a result as
// "finished result" where the message wants it (the queue alerts, the sent
// toast); the topbar's agreed wording says "1 result not sent" and passes false.
export function queuedWritesNoun(terminal, total, { finished = true } = {}) {
    const term = Number(terminal) || 0;
    const n = term > 0 ? term : (Number(total) || 0);
    const one = n === 1;
    const noun = term > 0
        ? `${finished ? 'finished ' : ''}${one ? 'result' : 'results'}`
        : (one ? 'score update' : 'score updates');
    return { n, one, noun };
}

// heldWritesText: the admin topbar's held-writes indicator (bc-offl), shown
// after the connection pill on every admin page, so a result held on this
// device stays visible after the editor that held it has closed (the court
// console moves on to the next match by itself). `status` is the sync status
// (subscribeSyncStatus), `counts` this tab's queue (subscribeUnsentWrites).
// null when nothing is held, and for auth-required, whose "Sign in to save"
// button already says it.
export function heldWritesText(status, counts) {
    const { total = 0, terminal = 0 } = counts || {};
    if (!(total > 0) || status === 'auth-required') return null;
    const { n, noun } = queuedWritesNoun(terminal, total, { finished: false });
    if (status === 'offline') return `Offline: ${n} ${noun} not sent`;
    if (status === 'server-error') return `Not saving: ${n} ${noun}`;
    return `Sending ${n} ${noun}…`;
}


// dependentActionBlocked: the sentence for a queued write that was only a
// PRE-SAVE gating a second action, the team editor's Add/Remove
// representative bout (saveRunningSheet). notLandedBanner rightly says
// nothing about a queued write at the three explicit-tap sites it was written
// for (Start match in both editors, Record bout in the team one), where the
// editor's queued/offline surface reports it. Here that surface says the
// scores did not send, not why the Add or Remove tap did nothing.
//
// A refusal (applied:false) is left to notLandedBanner: the caller already
// shows its banner, so a second message would only repeat it.
export function dependentActionBlocked(res) {
    if (writeRetryable(res)) {
        return "Couldn't save the current scores (offline or server busy). Try again once the connection is back.";
    }
    return null;
}

// FETCH_TIMEOUT_MS: how long a bounded request is waited on before it is
// reported as not answered. api_client.jsx gives it to every fetchWithTimeout
// and to _fetchJson (headers AND body there), and the team editor gives it to
// the save it makes before a representative-bout add or remove, so the hold
// that save runs under ends too (admin_scoring_team.jsx runRepBoutChange).
export const FETCH_TIMEOUT_MS = 12000;

// What a representative-bout add or remove leaves undone when it does not
// land, and the sentence for one the server never answered, or answered with
// a body that never completed. The team editor shows it as it is, whichever
// half (the save before the request, or the request) went unanswered.
export const REP_BOUT_NOT_ADDED = 'The representative bout was not added';
export const REP_BOUT_NOT_REMOVED = 'The representative bout was not removed';
export function noAnswerSentence(notDone) {
    return `${notDone}: the server did not answer. Check the connection and try again.`;
}


// matchLabel names a match the way the OPERATOR sees it: "Match 3", the label
// on the scores list, the bracket and the printed tree. The internal id
// ("m-r2-0") appears on no operator screen, so naming it there sends them
// looking for something that is not in front of them (operator ruling
// 2026-09-19).
//
// The 3rd-place match is the one match named rather than numbered: the
// numbering walks the bracket's rounds and the bronze hangs off a separate
// field, so it is numbered neither in the app nor on the printed tree, and
// the id fallback would have shown "m-bronze". Mirrors engine.MatchLabel
// (internal/engine/errors.go), which answers for the same match on the wire.
//
// The id fallback remains for a match with no number and no name -- a bye
// placeholder, or a bracket saved before numbering existed -- because a bare
// id still beats "Match 0".
//
// A match the SERVER names arrives with `label` ("Match 3 (Final)"), and that
// label wins: engine.MatchLabel qualifies the number with the match's knockout
// round, because a bare "Match 1" in a dialog raised while correcting
// "Pool A · Match 1" reads as the match on screen. The server owns that
// format so this dialog and the server's own message say the same words;
// composing it here from the number would be a second copy of it. The number
// arm below stays for a caller naming a match it built itself (the scores
// list's own rows).
const BRONZE_MATCH_ID = 'm-bronze';

export function matchLabel(m) {
    if (!m) return '';
    if (typeof m === 'string') return m;
    if (typeof m.label === 'string' && m.label) return m.label;
    if (m.number > 0) return `Match ${m.number}`;
    if (m.id === BRONZE_MATCH_ID) return 'the 3rd-place match';
    return m.id || '';
}

// matchLabelList joins several labels for a sentence: "Match 3 and Match 4".
function matchLabelList(ms) {
    const labels = (ms || []).map(matchLabel).filter(Boolean);
    if (labels.length <= 1) return labels[0] || '';
    return `${labels.slice(0, -1).join(', ')} and ${labels[labels.length - 1]}`;
}

// downstreamKnockoutPlayedRefusal / downstreamKnockoutPlayedConfirm /
// DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED (bc-kcdg / bc-cse).
//
// A THIRD not-landed shape, distinct from the two above: correcting a
// completed KNOCKOUT match whose LATER round has already been played used to
// silently repaint that later match's side while its own recorded result
// stayed put. The server now REFUSES the write outright -- HTTP 409
// {"error": "downstream_knockout_played", matchId, blockingMatchId,
// blockingMatches, displaced, qualifierChange, message} -- rather than the 200 {applied:false} shape the rest
// of this file owns, because this is a hard validation gate, not a
// last-write-wins drop: nothing raced the operator, the write is simply
// disallowed until they say so explicitly.
//
// api_client.jsx is the parser, exactly as it is for the 200 shapes: it
// attaches the four fields to the thrown Error as `.downstreamKnockoutPlayed`
// so a catcher never re-derives the shape from a raw response body or a
// message-string regex (the T103 decision_locked precedent this mirrors).
// Ask downstreamKnockoutPlayedRefusal(err) rather than testing
// `err.downstreamKnockoutPlayed` by hand -- the same reason every other
// predicate in this file exists.
export function downstreamKnockoutPlayedRefusal(err) {
    return (err && err.downstreamKnockoutPlayed) || null;
}

// The confirm dialog copy. It must NAME the blocking match and say plainly
// what confirming does: apply the correction AND send that later match back
// to be fought and re-entered (its recorded result is cleared). Cancelling
// leaves everything as it was -- the caller must not retry on a
// cancelled/false result, only on an explicit confirm.
export function downstreamKnockoutPlayedConfirm({ blockingMatchId, blockingMatches, displaced, qualifierChange, ranking, reopen } = {}) {
    // A POOL correction in a mixed competition that moves who holds a
    // qualifying place says so first: the operator is correcting a pool
    // result, so "who moves in the knockout" is the consequence they cannot
    // see from where they are. Branched before the knockout defaults below,
    // whose `displaced` describes one slot only. `ranking` (set by
    // api_client's overridePoolRanks) is the same refusal for a pool rank
    // recorded by hand (chusen), which corrects no result.
    if (qualifierChange && qualifierChange.length) {
        return {
            message: qualifierMoveConfirmMessage(qualifierChange, blockingMatches, blockingMatchId, displaced, ranking),
            confirmLabel: 'Apply and reopen',
            danger: true,
        };
    }
    // The `displaced` default covers a shape the server genuinely sends: it is
    // the corrected match's STORED winner, and a bye-resolved slot is completed
    // with an empty winner, so a correction written over one arrives with
    // displaced "". The `blocking` default is narrower and is NOT a server
    // shape: every generated bracket match carries an id (engine/bracket.go),
    // so it only covers a caller passing an incomplete object, which is what
    // this function's own unit test does.
    const who = displaced || 'The competitor currently recorded as advancing';
    // matchList names EVERY match the confirmation clears. Normally one; a
    // semifinal feeds both the final and the bronze match, and both go
    // together, so the dialog has to say so rather than naming one and
    // clearing two.
    const ms = (blockingMatches && blockingMatches.length)
        ? blockingMatches
        : (blockingMatchId ? [{ id: blockingMatchId }] : []);
    const many = ms.length > 1;
    const blocking = matchLabelList(ms) || 'the later match';
    // ONE paragraph, no newlines: the dialog renders `message` in a plain <p>
    // (ui.jsx) whose .dialog-msg rule sets no white-space, so a \n here
    // silently collapses to a space rather than breaking the line.
    // The plural arm names NO competitor, and that is not an oversight. The
    // two matches a semifinal feeds hold different people -- the final its
    // winner, the bronze its loser -- so `displaced`, which describes one
    // slot, is false of the other. It read "Ren Takada already played the
    // 3rd-place match and Match 3" when Ren had played only the bronze.
    // Mirrors engine.DownstreamKnockoutPlayedError.Error's own plural arm.
    //
    // `reopen` (set by api_client's reopenFailureError) is the same refusal
    // met by a REOPEN (Reopen match, Clear withdrawal and reopen): the
    // operator is reopening this match, not applying a correction, so the
    // copy says what reopening does and the button names that act.
    if (reopen) {
        return {
            message: many
                ? `${blocking} were built on this match's current result and have already been played. ` +
                  'Reopening this match also reopens both: their winners are cleared and their points are ' +
                  'kept, so check them and take back any that no longer apply.'
                : `${who} already played ${blocking}, which was built on this match's current result. ` +
                  `Reopening this match also reopens ${blocking}: its winner is cleared and its points are ` +
                  'kept, so check them and take back any that no longer apply.',
            confirmLabel: many ? 'Reopen all of them' : 'Reopen both',
            danger: true,
        };
    }
    return {
        message: many
            ? `${blocking} were built on this match's current result and have already been played. ` +
              'Applying this correction reopens both with the new competitors in them: their winners are ' +
              'cleared and their points are kept, so check them and take back any that no longer apply.'
            : `${who} already played ${blocking}, which was built on this match's current result. ` +
              `Applying this correction reopens ${blocking} with the new competitor in it: its winner is ` +
              'cleared and its points are kept, so check them and take back any that no longer apply.',
        confirmLabel: 'Apply correction and reopen',
        danger: true,
    };
}

// placeName: "Pool A's 1st place".
function placeName(c) {
    return `${c.pool || 'The pool'}'s ${c.place || 'qualifying'} place`;
}

// qualifierMoveConfirmMessage is the confirm copy for a pool correction that
// moves one or more qualifying places (the server's qualifierChange, each
// {pool, rank, place, from: {name, id}, to: {name, id}, tied}). ONE paragraph,
// for the same reason as the knockout copy above: the dialog renders a plain
// <p>. It names who moves, then which knockout matches were already fought
// and what confirming does to them.
function qualifierMoveConfirmMessage(changes, blockingMatches, blockingMatchId, displaced, ranking) {
    const nameOf = (who, fallback) => (who && who.name) || fallback;
    const lead = ranking ? 'Recording this ranking' : 'Changing this result';
    let moves;
    if (changes.length === 1) {
        const c = changes[0];
        const from = nameOf(c.from, 'the current qualifier');
        moves = c.tied
            ? `${lead} leaves ${placeName(c)} tied, to be settled by a tie-break, so ${from} no longer holds it in the knockout.`
            : `${lead} moves ${placeName(c)} from ${from} to ${nameOf(c.to, 'another competitor')}, who takes ${from}'s place in the knockout.`;
    } else {
        const items = changes.map((c) => (c.tied
            ? `${placeName(c)} (${nameOf(c.from, 'the current qualifier')}, now tied until a tie-break is fought)`
            : `${placeName(c)} (${nameOf(c.from, 'the current qualifier')} to ${nameOf(c.to, 'another competitor')})`));
        moves = `${lead} changes who holds ${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}, and the knockout is changed to match.`;
    }
    const ms = (blockingMatches && blockingMatches.length)
        ? blockingMatches
        : (blockingMatchId ? [{ id: blockingMatchId }] : []);
    const blocking = matchLabelList(ms) || 'A knockout match';
    const Blocking = blocking.charAt(0).toUpperCase() + blocking.slice(1);
    const many = ms.length > 1;
    // A reopened match on a TIED place has nobody to seat until the tie-break
    // decides who holds that place, so it cannot be "fought again" straight
    // away: say when it will be. When only some places are tied the payload
    // does not say which reopened match stands on which place, so the wait is
    // stated for the tied ones rather than claimed of every match.
    const tied = changes.filter((c) => c.tied).length;
    const places = tied > 1 ? 'the places' : 'the place';
    let refight;
    if (tied === changes.length) {
        refight = `finished once the tie-break decides ${places}`;
    } else if (tied > 0) {
        refight = 'finished again; a match on a tied place waits for the tie-break to decide it';
    } else {
        refight = many ? 'they must be finished again' : 'it must be finished again';
    }
    const fought = many
        ? `${Blocking} were already fought with the competitors being replaced: they will be reopened with their winners cleared and their points kept, and ${refight}.`
        : `${Blocking} was already fought${displaced ? ` with ${displaced}` : ''}: it will be reopened with its winner cleared and its points kept, and ${refight}.`;
    return `${moves} ${fought}`;
}

// downstreamKnockoutRunningMessage (the 409 downstream_knockout_running): a
// pool correction that would move a qualifier out of a knockout match being
// fought RIGHT NOW, or a knockout correction whose new winner would change a
// side of a later match being fought (operator decision 2026-09-27). Not
// confirmable, so this is an error message, never a dialog: the operator
// finishes that match or sends it back to the queue, then saves the
// correction again. The server's Go message says the same words
// (engine.DownstreamKnockoutRunningError).
export function downstreamKnockoutRunningMessage(runningMatches) {
    const { subject, them } = runningParts(runningMatches);
    return `${subject}. Finish ${them} or send ${them} back to the queue, then save this correction again.`;
}

// downstreamKnockoutRunningReopenMessage: the same refusal (409
// downstream_knockout_running) met by a REOPEN rather than a score write
// (bc-cse). "then save this correction again" is wrong here -- a reopen has
// no save step to retry, the operator taps Reopen again once the blocking
// match is out of the way -- so this is a separate message, not a parameter
// on the one above, the same split downstreamKnockoutPlayedConfirm's own
// `reopen` flag already draws for the played-shape refusal. Both read as the
// server's DownstreamKnockoutRunningError does, pinned by the shared table
// internal/engine/testdata/downstream_running_messages.json.
export function downstreamKnockoutRunningReopenMessage(runningMatches) {
    const { subject, them } = runningParts(runningMatches);
    return `${subject}. Finish ${them} or send ${them} back to the queue, then reopen this match again.`;
}

// downstreamKnockoutRunningQueueDrop: the same refusal met by a QUEUED replay,
// in the { reason, advice } shape the not-saved banner renders as "Not saved:
// <reason>. <advice>". The advice is the remedy for a write that is no longer
// on screen: enter it again once that match is out of the way.
export function downstreamKnockoutRunningQueueDrop(runningMatches) {
    const { subject, them } = runningParts(runningMatches);
    return {
        reason: subject,
        advice: `Finish ${them} or send ${them} back to the queue, then enter this result again.`,
    };
}

// courtBusyMessage (bc-rawm): the operator sentence for a 409 court_busy
// refusal on a score write -- the shiaijo this match wants is not free, a
// DIFFERENT match already holds it. Named the way the operator sees it, off
// the server's own `label` field, never the internal matchId the body also
// carries: unlike matchLabel's own id fallback (built for a match this app
// names itself). bc-cse: no "This shiaijo"/"another match" fallback text --
// respondCourtBusy (handlers_match.go) always sends both `court` and a
// `label` (matchLabelOrID falls back to the raw match id itself rather than
// omitting the field), so the fallbacks were dead code defending against a
// shape the server never sends.
export function courtBusyMessage({ court, label }) {
    return `Shiaijo ${court} is running ${label}. Finish it or send it back to the queue first.`;
}

// runningParts names the matches being fought and where, the same words as
// engine's runningSubject: "Match 3 (Final) is being fought now on Shiaijo
// A", and for two, "The 3rd-place match is being fought now on Shiaijo B and
// Match 3 (Final) on Shiaijo A". A match with no court gets no court clause,
// and when none has one the plural form stands.
function runningParts(runningMatches) {
    const ms = (runningMatches || []).filter(Boolean);
    const them = ms.length > 1 ? 'them' : 'it';
    let subject;
    if (!ms.some((m) => m.court)) {
        const named = matchLabelList(ms) || 'A knockout match';
        subject = `${named} ${ms.length > 1 ? 'are' : 'is'} being fought now`;
    } else {
        const parts = ms.map((m, i) => `${matchLabel(m)}${i === 0 ? ' is being fought now' : ''}${m.court ? ` on Shiaijo ${m.court}` : ''}`);
        subject = parts.length > 1
            ? `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}`
            : parts[0];
    }
    return { subject: subject.charAt(0).toUpperCase() + subject.slice(1), them };
}

// The cancellation notice: confirms to the operator that declining the
// override left the match, and the later one it would have reopened,
// completely unchanged -- neither was written.
export const DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED = 'Correction cancelled: the match and the later result it depends on were left unchanged.';

// The same declined confirmation when what was refused is a REOPEN (Reopen
// match, Clear withdrawal and reopen, admin_scoring_shared.jsx's
// useMatchReopen): nothing was corrected, so it does not say "correction".
export const DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED = 'Reopen cancelled: this match and the later result it depends on were left unchanged.';

// The chusen panel's copy for the same declined confirmation when what was
// refused is a chusen order recorded by hand (overridePoolRanks): the order
// is one write, so none of it was recorded, and the knockout match it would
// have reopened was left as it was.
export const DOWNSTREAM_KNOCKOUT_RANKING_CANCELLED = 'Ranking not recorded: the knockout match already fought was left unchanged.';

// downstreamKnockoutPlayedQueueDrop (bc-cse): the copy for THIS refusal
// arriving on a QUEUED replay rather than a live tap. A correction typed
// while offline (or during a transient 5xx run) is retried automatically on
// reconnect; if the later match was played in the meantime, the retry hits
// this same 409. There is no operator at the keyboard for the flush loop to
// prompt -- attemptScoreWrite's confirm dialog above has nothing to show a
// tap into -- so the write is dropped exactly like any other non-retryable
// 4xx (the api_client.jsx flush loop's generic "rejected" branch), but with
// this reason instead of the bare "downstream_knockout_played" token: a
// dropped correction is finished work the operator must be told about in
// words, and the queue must not retry it forever (it will never land
// without forceDownstreamReopen, which nothing sets automatically).
//
// Returns the { reason, advice } shape _notifyTerminalWriteFailed's payload
// and this file's own notLandedBanner both use, so a caller passes it
// straight into that channel rather than composing a third copy of the
// who/blocking defaults downstreamKnockoutPlayedConfirm already states.
export function downstreamKnockoutPlayedQueueDrop({ blockingMatchId, blockingMatches, displaced } = {}) {
    const who = displaced || 'The competitor currently recorded as advancing';
    const blocking = matchLabelList(
        (blockingMatches && blockingMatches.length) ? blockingMatches : (blockingMatchId ? [{ id: blockingMatchId }] : []),
    ) || 'the later match';
    return {
        reason: `${who} already played ${blocking}, so this queued correction could not be applied automatically`,
        advice: `Redo the correction now that you're online: you'll be asked to confirm reopening ${blocking} for re-entry.`,
    };
}

// downstreamKnockoutReopenedNotice: what to tell the operator AFTER a
// confirmed correction, naming the matches it reopened. The counterpart to
// downstreamKnockoutPlayedConfirm, which names them before.
//
// Without this, "did the next match actually reopen?" is answered only by the
// presence of a dialog beforehand and by reading the board afterwards. The
// operator authorised something specific; the app should confirm it happened.
export function downstreamKnockoutReopenedNotice(matches) {
    const list = (matches || []).filter(Boolean);
    if (!list.length) return null;
    const named = matchLabelList(list);
    return list.length === 1
        ? `${named} was reopened with its points kept: check them, then finish it again.`
        : `${named} were reopened with their points kept: check them, then finish them again.`;
}

// attemptScoreWrite (bc-kcdg / bc-cse): the generic confirm+retry loop for the
// refusal above. Takes recordScore/confirmDialog as INJECTED collaborators
// (never read off `window`, never imported from a host module) so it works
// from either script-tagged host that needs it: admin.jsx's editMatchScore
// (the single chokepoint every score-editor host and both editor bodies route
// a score write through) and admin_shiaijo.jsx's ResolveFeedersModal (the
// override-winner "Run now" recovery). Those two cannot import each other --
// both are `<script type="module">` entry points in index.html, and a module
// that is both script-tagged and ES-imported evaluates twice under two URLs,
// splitting its module-level singleton state (see this file's header) -- so
// the shared loop lives here instead, beside the refusal shape it orchestrates,
// which this file is safe to import from anywhere.
//
// On a refusal, prompts via confirmDialog with the message/label
// downstreamKnockoutPlayedConfirm builds; on confirm, resends the SAME result
// with forceDownstreamReopen:true, which the server applies alongside
// reopening the blocking match(es) for re-entry. Declining leaves everything
// as it was.
//
// Throws on any failure, including a declined override; in that one case the
// thrown error carries `.downstreamKnockoutPlayedCancelled = true` so the
// caller can pick DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED instead of the generic
// error copy.
export async function attemptScoreWrite({ recordScore, confirmDialog, compId, matchId, result, password, match }) {
    try {
        return await recordScore(compId, matchId, result, password, match);
    } catch (e) {
        const refusal = downstreamKnockoutPlayedRefusal(e);
        // The forceDownstreamReopen guard on `result` stops a second refusal
        // (e.g. a genuine race between two operators) from looping the confirm
        // dialog: only the first attempt for a given patch is offered the
        // override; a refusal on the forced retry is treated like any other error.
        if (!refusal || result.forceDownstreamReopen) throw e;
        const { message, confirmLabel, danger } = downstreamKnockoutPlayedConfirm(refusal);
        const ok = await confirmDialog({ message, confirmLabel, danger });
        if (ok) {
            const applied = await attemptScoreWrite({
                recordScore, confirmDialog, compId, matchId,
                result: { ...result, forceDownstreamReopen: true },
                password, match,
            });
            // Say what the confirmation DID, not just that it went through,
            // and take that from the SERVER: the write's response carries
            // reopenedMatchIds, which is what it actually reopened. Reusing the
            // refusal's ids here (as the first cut did) reported the server's
            // INTENTION -- "m-r2-0 was reopened" because the dialog named it,
            // whether or not anything was. Absent field means the server
            // reopened nothing, and nothing is claimed.
            if (applied && typeof applied === 'object' && !writeDidNotLand(applied)
                && applied.reopenedMatches && applied.reopenedMatches.length) {
                applied.downstreamReopened = applied.reopenedMatches;
            }
            return applied;
        }
        e.downstreamKnockoutPlayedCancelled = true;
        throw e;
    }
}
