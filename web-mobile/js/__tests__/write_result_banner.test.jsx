// notLandedBanner is the ONE owner of "which not-saved banner does this write
// result deserve?". Three explicit-tap call sites (Start match in both scoring
// editors, Record bout in the team one) used to hold the same if/else chain,
// and the ORDER in that chain is the whole point: both verdicts are
// `applied === false`, so asking the broad question first swallows the narrow
// one and tells a clock-refused operator to go and check a newer result that
// does not exist.
//
// These cases pin the mapping, including the two silences. A queued write is
// NOT a banner: it is not stored yet, but it will be, and the editor's
// queued/offline surface already owns that state.

import { describe, it, expect } from 'vitest';
import {
    notLandedBanner,
    terminalFailureBanner,
    notSavedText,
    NOT_SAVED_ADVICE,
    dependentActionBlocked,
    SUPERSEDED_LEAD,
    SUPERSEDED_REASON,
    writeHeldGroups,
    writePartlyHeld,
    SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT,
    CLOCK_SKEW_ADVICE,
    writeNeedsWinner,
    NEEDS_WINNER_REASON,
    NEEDS_WINNER_ADVICE,
    supersededAlertText,
    writeDisplacedGroups,
    displacedAlertText,
} from '../write_result.jsx';
import { heldGroupsNote, keptInHistoryNote } from '../match_groups.jsx';
import { closingHistoryToast } from '../admin.jsx';

describe('notLandedBanner', () => {
    it('maps a clock refusal to the clock copy, not the superseded copy', () => {
        const b = notLandedBanner({ applied: false, reason: 'clock_skew' });
        expect(b).toEqual({ reason: CLOCK_SKEW_REASON_TEXT, advice: CLOCK_SKEW_ADVICE });
        // The narrow verdict must win: a clock refusal stored NOTHING, so the
        // superseded advice ("check the recorded result first") would point the
        // operator at a result that does not exist and talk them out of the one
        // action that saves their work.
        expect(b.reason).not.toBe(SUPERSEDED_REASON);
    });

    it('maps a plain supersede to the superseded copy', () => {
        // bc-mrgc: the write is kept in the match's history, so it leads with
        // "Not applied", never "Not saved".
        expect(notLandedBanner({ applied: false })).toEqual({
            lead: SUPERSEDED_LEAD,
            reason: SUPERSEDED_REASON,
            advice: SUPERSEDED_ADVICE,
        });
    });

    it('maps an applied:false carrying some other reason to the superseded copy', () => {
        // Only the exact 'clock_skew' wire value takes the narrow branch; any
        // other reason is still just "a newer result won".
        expect(notLandedBanner({ applied: false, reason: 'something_else' })).toEqual({
            lead: SUPERSEDED_LEAD,
            reason: SUPERSEDED_REASON,
            advice: SUPERSEDED_ADVICE,
        });
    });

    it('says nothing about a write that landed', () => {
        expect(notLandedBanner({ applied: true })).toBeNull();
        expect(notLandedBanner({})).toBeNull();
    });

    it('says nothing about a queued write', () => {
        // Deliberate silence, and the behaviour the pasted chains had: `queued`
        // satisfies neither predicate, and the write still lands on reconnect.
        expect(notLandedBanner({ queued: true })).toBeNull();
    });

    it('says nothing when there is no result at all', () => {
        // The call sites await a submit helper that can resolve undefined when
        // the host swallowed the error, so this must not throw.
        expect(notLandedBanner(null)).toBeNull();
        expect(notLandedBanner(undefined)).toBeNull();
    });
});

// F1 (bc-p3-dh-lineups-excel review): dependentActionBlocked answers a
// narrower question than notLandedBanner -- not "does this write deserve a
// banner" but "does a caller-specific dependent action need to say why it
// silently did nothing". A refusal already has notLandedBanner's own banner,
// so this stays silent for that case on purpose (no duplicate message).
describe('dependentActionBlocked', () => {
    it('names a queued write as the reason a dependent action did not run', () => {
        expect(dependentActionBlocked({ queued: true })).toBe(
            "Couldn't save the current scores (offline or server busy). Try again once the connection is back."
        );
    });

    it('says nothing about a refusal (notLandedBanner already reports it)', () => {
        expect(dependentActionBlocked({ applied: false })).toBeNull();
        expect(dependentActionBlocked({ applied: false, reason: 'clock_skew' })).toBeNull();
    });

    it('says nothing about a write that landed, or when there is no result at all', () => {
        expect(dependentActionBlocked({ applied: true })).toBeNull();
        expect(dependentActionBlocked({})).toBeNull();
        expect(dependentActionBlocked(null)).toBeNull();
        expect(dependentActionBlocked(undefined)).toBeNull();
    });
});

// notSavedText is the ONE line every not-saved banner shows (the three score
// editors and the barred-match notice). A refusal the server worded itself is
// shown as it is: a full stop and advice after it doubled the stop, and the
// default advice could contradict it.
describe('notSavedText', () => {
    const REMOVED = "This match's representative bout was removed on another device. Check the scores and finish again.";

    it('shows a sentence the server wrote as it is, once', () => {
        expect(notSavedText({ reason: REMOVED, sentence: true })).toBe(`Not saved: ${REMOVED}`);
    });

    it('words a code or a status around it, with the advice given or the default', () => {
        expect(notSavedText({ reason: 'conflict' })).toBe(`Not saved: conflict. ${NOT_SAVED_ADVICE}`);
        expect(notSavedText({ reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE })).toBe(`Not saved: ${SUPERSEDED_REASON}. ${SUPERSEDED_ADVICE}`);
    });

    it('reads every banner notLandedBanner makes, a kept write leading with its own words', () => {
        const clock = notLandedBanner({ applied: false, reason: 'clock_skew' });
        expect(notSavedText(clock)).toBe(`Not saved: ${clock.reason}. ${clock.advice}`);
        // bc-mrgc: a superseded write was kept in the match's history, not lost.
        const kept = notLandedBanner({ applied: false });
        expect(notSavedText(kept)).toBe(`Not applied: ${SUPERSEDED_REASON}. ${SUPERSEDED_ADVICE}`);
        expect(notSavedText(kept)).toMatch(/kept in the match's history/);
        expect(notSavedText(kept)).toMatch(/nothing is lost/);
        expect(notSavedText(kept)).not.toMatch(/Not saved/);
    });

    it('a superseded replay keeps its lead through terminalFailureBanner', () => {
        const b = terminalFailureBanner({ status: 200, lead: SUPERSEDED_LEAD, reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE });
        expect(notSavedText(b)).toBe(`Not applied: ${SUPERSEDED_REASON}. ${SUPERSEDED_ADVICE}`);
    });
});

describe('terminalFailureBanner', () => {
    it('keeps the server-sentence mark a replayed refusal carries', () => {
        expect(terminalFailureBanner({ status: 409, reason: 'Said by the server.', sentence: true }))
            .toEqual({ reason: 'Said by the server.', advice: undefined, sentence: true });
    });

    it('names the status when a failure carries no reason, and marks nothing', () => {
        const b = terminalFailureBanner({ status: 400 });
        expect(b).toEqual({ reason: 'save rejected (400)', advice: undefined });
        expect(notSavedText(b)).toBe(`Not saved: save rejected (400). ${NOT_SAVED_ADVICE}`);
    });
});

// bc-mrgc: a write the server applied in part names, in `heldGroups`, the
// groups it kept in the match's history instead. A superseded write lists
// them too, but is answered by the banner, never by the quiet note.
describe('writeHeldGroups / writePartlyHeld', () => {
    it('a write applied in part is partly held, naming its groups', () => {
        const res = { id: 'm1', status: 'running', heldGroups: ['points', 'bout:2'] };
        expect(writeHeldGroups(res)).toEqual(['points', 'bout:2']);
        expect(writePartlyHeld(res)).toBe(true);
    });

    it('a superseded write names its groups but is not partly held', () => {
        const res = { applied: false, reason: 'superseded', heldGroups: ['points'] };
        expect(writeHeldGroups(res)).toEqual(['points']);
        expect(writePartlyHeld(res)).toBe(false);
    });

    it('a write that held nothing, a queued one, and nothing at all are not', () => {
        for (const res of [{ id: 'm1' }, { id: 'm1', heldGroups: [] }, { queued: true }, undefined, null]) {
            expect(writePartlyHeld(res)).toBe(false);
            expect(writeHeldGroups(res)).toEqual([]);
        }
    });
});

// Operator ruling 2026-10-04: a scoring change that would leave a finished
// match without the winner it needs (a knockout left tied) is not applied, is
// kept in the match's history, and the server says so with heldReason
// "needs_winner". Nothing newer won, so the advice is to correct the result
// with a winner, never the plain superseded "check the newer change".
describe('a change held because the finished match needs a winner', () => {
    const superseded = { applied: false, reason: 'superseded', heldGroups: ['points'], heldReason: 'needs_winner' };
    const partly = { id: 'm1', status: 'completed', heldGroups: ['points'], heldReason: 'needs_winner' };

    it('is read from heldReason, on a superseded answer and on one applied in part', () => {
        expect(writeNeedsWinner(superseded)).toBe(true);
        expect(writeNeedsWinner(partly)).toBe(true);
        expect(writeNeedsWinner({ applied: false, reason: 'superseded', heldGroups: ['points'] })).toBe(false);
        expect(writeNeedsWinner({ queued: true })).toBe(false);
        expect(writeNeedsWinner(null)).toBe(false);
    });

    it('the superseded banner tells the operator to correct the result with a winner', () => {
        const banner = notLandedBanner(superseded);
        expect(banner).toEqual({ lead: SUPERSEDED_LEAD, reason: NEEDS_WINNER_REASON, advice: NEEDS_WINNER_ADVICE });
        const text = notSavedText(banner);
        expect(text).toMatch(/^Not applied: /);
        expect(text).toMatch(/without a winner/);
        expect(text).toMatch(/Correct the result with a winner\.$/);
        expect(text).not.toMatch(/newer change/);
    });

    it('the partial-apply note says it in place of "a newer change"', () => {
        expect(heldGroupsNote(['points'], true)).toBe(
            "Kept in the match's history, not applied: points. It would leave the finished match without a winner, and it needs one: correct the result with a winner.");
        expect(heldGroupsNote(['points'])).toBe(
            "Kept in the match's history, not applied: points. A newer change to the same thing was recorded first.");
    });

    it('the queue alert says it too', () => {
        expect(supersededAlertText(1, true, true)).toMatch(/without a winner.*correct the result with a winner\.$/);
        expect(supersededAlertText(1, true, true)).not.toMatch(/newer change/);
        expect(supersededAlertText(2, false, true)).toMatch(/correct that result with a winner\.$/);
        expect(supersededAlertText(1, true)).not.toMatch(/winner/);
    });

    it('no em-dash and no "mat" in any of the copy', () => {
        const all = [NEEDS_WINNER_REASON, NEEDS_WINNER_ADVICE, heldGroupsNote(['points'], true), supersededAlertText(2, false, true)].join(' ');
        expect(all).not.toMatch(/\u2014/);
        expect(all).not.toMatch(/\bmats?\b/i);
    });
});

// The same reason on a second, APPLIED answer: an older finish arrived after a
// newer scoring change that, applied after it, would have left the finished
// knockout without a winner. The finish IS recorded and that later change was
// MOVED to the history (`displacedGroups`, no heldGroups). Nothing tells the
// operator to correct a result that is recorded.
describe('a finish recorded that moved a later change to the history', () => {
    const displaced = { id: 'm1', status: 'completed', winner: 'A', displacedGroups: ['points'], heldReason: 'needs_winner' };
    const SAVED = "Saved. A later change to points would have left the finished match without a winner, so it was moved to the match's history.";

    it('is not a write held for a winner', () => {
        expect(writeDisplacedGroups(displaced)).toEqual(['points']);
        expect(writeNeedsWinner(displaced)).toBe(false);
        expect(writeNeedsWinner({ ...displaced, heldGroups: [] })).toBe(false); // bulk-score's item shape
        expect(writeDisplacedGroups({ id: 'm1' })).toEqual([]);
        // Held groups beside a move: the one reason is read as the move's,
        // so nothing says to correct a result that is recorded.
        expect(writeNeedsWinner({ ...displaced, heldGroups: ['encho'] })).toBe(false);
    });

    it('the note says Saved and what was moved, never "not applied" or "correct"', () => {
        expect(keptInHistoryNote(displaced)).toBe(SAVED);
        expect(keptInHistoryNote({ ...displaced, heldGroups: [] })).toBe(SAVED);
        expect(SAVED).not.toMatch(/not applied|correct the result/i);
    });

    it('a write held in part and one that moved a later change says both', () => {
        const both = { id: 'm1', status: 'completed', heldGroups: ['encho'], displacedGroups: ['bout:2'] };
        expect(keptInHistoryNote(both)).toBe(
            "Kept in the match's history, not applied: overtime. A newer change to the same thing was recorded first. "
            + "Saved. A later change to bout 2 would have left the finished match without a winner, so it was moved to the match's history.");
    });

    it('nothing kept: no note', () => {
        expect(keptInHistoryNote({ id: 'm1', status: 'completed' })).toBeNull();
        expect(keptInHistoryNote({ queued: true })).toBeNull();
    });

    it('the closing toast after a Finish says it; a write that did not land, or keeps the editor open, says nothing here', () => {
        expect(closingHistoryToast({ status: 'completed', winner: 'A' }, displaced)).toBe(SAVED);
        expect(closingHistoryToast({ status: 'completed', winner: 'A' }, { queued: true })).toBeNull();
        expect(closingHistoryToast({ status: 'completed', winner: 'A' }, { applied: false, reason: 'superseded', heldGroups: ['points'], heldReason: 'needs_winner' })).toBeNull();
        expect(closingHistoryToast({ status: 'running' }, displaced)).toBeNull();
    });

    it('the queue alert says the held result was saved', () => {
        expect(displacedAlertText(1, true)).toMatch(/^A held result was saved\. .*moved to the match's history\.$/);
        expect(displacedAlertText(2, false)).toMatch(/^2 held results were saved\./);
    });
});
