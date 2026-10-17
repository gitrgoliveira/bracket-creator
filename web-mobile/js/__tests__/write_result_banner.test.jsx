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
    SUPERSEDED_LEAD,
    SUPERSEDED_REASON,
    writeHeldGroups,
    writePartlyHeld,
    SUPERSEDED_ADVICE,
    CLOCK_SKEW_REASON_TEXT,
    CLOCK_SKEW_ADVICE,
    writeMatchClosed,
    writeResultLocked,
    MATCH_CLOSED_REASON,
    MATCH_CLOSED_ADVICE,
    RESULT_LOCKED_PLAYED_REASON,
    RESULT_LOCKED_RUNNING_REASON,
    decisionWord,
    supersededAlertText,
    writeDisplacedGroups,
    writeDisplacedForClosedMatch,
    displacedAlertText,
    tapOutcomeQuiet,
    tapNotAppliedText,
    tapNotAppliedAlertText,
    markToasted,
    wasToasted,
    startWhileStartingMessage,
    startWasBlockedByStartMessage,
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

// The closed-match rule (operator ruling 2026-10-15): a change made after the
// match was finished is not applied, is kept in the match's history, and the
// server says so with heldReason "match_closed". Nothing newer won, so the
// advice is Correct, never the plain superseded "check the newer change".
describe('a change turned away because the match is finished (match_closed)', () => {
    const held = { applied: false, reason: 'superseded', heldGroups: ['points'], heldReason: 'match_closed' };

    it('is read from heldReason, on a superseded answer and on one applied in part', () => {
        expect(writeMatchClosed(held)).toBe(true);
        expect(writeMatchClosed({ id: 'm1', status: 'completed', heldGroups: ['points'], heldReason: 'match_closed' })).toBe(true);
        expect(writeMatchClosed({ ...held, heldReason: undefined })).toBe(false);
        // A finish that moved a later change is recorded: not turned away.
        expect(writeMatchClosed({ id: 'm1', status: 'completed', displacedGroups: ['points'], heldReason: 'match_closed' })).toBe(false);
        expect(writeMatchClosed({ id: 'm1', status: 'completed', heldReason: 'match_closed' })).toBe(false);
        expect(writeMatchClosed(null)).toBe(false);
    });

    it('the banner says the match is finished and to tap Correct', () => {
        expect(notLandedBanner(held)).toEqual({ lead: SUPERSEDED_LEAD, reason: MATCH_CLOSED_REASON, advice: MATCH_CLOSED_ADVICE });
        expect(notSavedText(notLandedBanner(held))).toBe(
            "Not applied: the match is finished, so this change was kept in the match's history. To change the result, tap Correct.");
    });

    it("the server's sentence wins when it sends one", () => {
        const message = 'This match is finished. To change its result, tap Correct.';
        expect(notLandedBanner({ ...held, message })).toEqual({ lead: SUPERSEDED_LEAD, reason: message, sentence: true });
        expect(notSavedText(notLandedBanner({ ...held, message }))).toBe(`Not applied: ${message}`);
    });

    it('the partial-apply note says it in place of "a newer change"', () => {
        expect(keptInHistoryNote({ id: 'm1', status: 'completed', heldGroups: ['points'], heldReason: 'match_closed' })).toBe(
            "Kept in the match's history, not applied: points. The match is finished: to change the result, tap Correct.");
        expect(heldGroupsNote(['points'])).toBe(
            "Kept in the match's history, not applied: points. A newer change to the same thing was recorded first.");
    });

    it('the queue alert says it too', () => {
        expect(supersededAlertText(1, true, true)).toMatch(/^A result was not applied: the match is finished.*tap Correct\.$/);
        expect(supersededAlertText(2, false, true)).toMatch(/^2 results were not applied.*tap Correct\.$/);
        expect(supersededAlertText(1, true)).toMatch(/newer change/);
    });
});

describe('a result held because the next knockout round depends on it', () => {
    const played = { applied: false, reason: 'superseded', heldGroups: ['result'], heldReason: 'downstream_knockout_played' };
    const running = { ...played, heldReason: 'downstream_knockout_running' };

    it('writeResultLocked names which', () => {
        expect(writeResultLocked(played)).toBe('played');
        expect(writeResultLocked(running)).toBe('running');
        expect(writeResultLocked(held())).toBeNull();
        expect(writeResultLocked(null)).toBeNull();
        function held() { return { applied: false, reason: 'superseded', heldReason: 'match_closed' }; }
    });

    it('the banner says the recorded result stands, unless the server sends its sentence', () => {
        expect(notLandedBanner(played)).toEqual({ lead: SUPERSEDED_LEAD, reason: RESULT_LOCKED_PLAYED_REASON, advice: MATCH_CLOSED_ADVICE });
        expect(notLandedBanner(running)).toEqual({ lead: SUPERSEDED_LEAD, reason: RESULT_LOCKED_RUNNING_REASON, advice: MATCH_CLOSED_ADVICE });
        expect(notLandedBanner({ ...played, message: 'The next round was already fought.' }).sentence).toBe(true);
    });

    it('the note names it', () => {
        expect(keptInHistoryNote(played)).toBe(
            "Kept in the match's history, not applied: the result. The next round was already fought, so the recorded result stands. To change the result, tap Correct.");
    });
});

describe('a finish recorded that moved a later change to the history', () => {
    const displaced = { id: 'm1', status: 'completed', winner: 'A', displacedGroups: ['points'], heldReason: 'match_closed' };
    const SAVED = "Saved. A later change to points was made after the match was finished, so it was moved to the match's history.";

    it('is not a write turned away', () => {
        expect(writeDisplacedGroups(displaced)).toEqual(['points']);
        expect(writeMatchClosed(displaced)).toBe(false);
        expect(writeMatchClosed({ ...displaced, heldGroups: [] })).toBe(false); // bulk-score's item shape
        expect(writeDisplacedGroups({ id: 'm1' })).toEqual([]);
        expect(writeMatchClosed({ ...displaced, heldGroups: ['encho'] })).toBe(false);
    });

    it('the note says Saved and what was moved, never "not applied"', () => {
        expect(keptInHistoryNote(displaced)).toBe(SAVED);
        expect(keptInHistoryNote({ ...displaced, heldGroups: [] })).toBe(SAVED);
        expect(SAVED).not.toMatch(/not applied/i);
    });

    it('a change moved for another reason is worded plainly', () => {
        expect(keptInHistoryNote({ id: 'm1', status: 'completed', displacedGroups: ['repPickB'] })).toBe(
            "Saved. A later change to Shiro's pick for the representative bout was moved to the match's history.");
        expect(writeDisplacedForClosedMatch(displaced)).toBe(true);
        expect(writeDisplacedForClosedMatch({ ...displaced, heldReason: undefined })).toBe(false);
        expect(writeDisplacedForClosedMatch({ ...displaced, displacedGroups: [] })).toBe(false);
        expect(writeDisplacedForClosedMatch(null)).toBe(false);
    });

    it('a write held in part and one that moved a later change says both', () => {
        const both = { id: 'm1', status: 'completed', heldGroups: ['encho'], displacedGroups: ['bout:2'] };
        expect(keptInHistoryNote(both)).toBe(
            "Kept in the match's history, not applied: overtime. A newer change to the same thing was recorded first. "
            + "Saved. A later change to bout 2 was moved to the match's history.");
    });

    it('nothing kept: no note', () => {
        expect(keptInHistoryNote({ id: 'm1', status: 'completed' })).toBeNull();
        expect(keptInHistoryNote({ queued: true })).toBeNull();
    });

    it('the closing toast after a Finish says it; a write that did not land, or keeps the editor open, says nothing here', () => {
        expect(closingHistoryToast({ status: 'completed', winner: 'A' }, displaced)).toBe(SAVED);
        expect(closingHistoryToast({ status: 'completed', winner: 'A' }, { queued: true })).toBeNull();
        expect(closingHistoryToast({ status: 'completed', winner: 'A' }, { applied: false, reason: 'superseded', heldGroups: ['points'], heldReason: 'match_closed' })).toBeNull();
        expect(closingHistoryToast({ status: 'running' }, displaced)).toBeNull();
    });

    it('the queue alert says the held result was saved', () => {
        expect(displacedAlertText(1, true)).toBe("A held result was saved. A later change to that match was moved to the match's history.");
        expect(displacedAlertText(2, false)).toMatch(/^2 held results were saved\./);
    });
});

describe('the tap-outcome readers', () => {
    it('applied, recorded and duplicate are quiet; everything else is not', () => {
        for (const outcome of ['applied', 'recorded', 'duplicate']) expect(tapOutcomeQuiet({ outcome })).toBe(true);
        for (const outcome of ['capped', 'beforeBaseline', 'noTarget', 'notAllowed', 'matchClosed', 'notStarted', 'refused']) {
            expect(tapOutcomeQuiet({ outcome })).toBe(false);
            expect(tapNotAppliedText({ outcome })).toMatch(/^Not applied: /);
        }
        expect(tapOutcomeQuiet(null)).toBe(false);
        expect(tapNotAppliedText({ outcome: 'applied' })).toBeNull();
    });
    it("the server's message comes first", () => {
        expect(tapNotAppliedText({ outcome: 'notStarted', reason: 'match_sent_back', message: 'The match was sent back to the queue.' }))
            .toBe('The match was sent back to the queue.');
    });
    it('a capped tap reads that the bout was already decided; a closed match says Correct', () => {
        expect(tapNotAppliedText({ outcome: 'capped' })).toBe('Not applied: the bout was already decided.');
        expect(tapNotAppliedText({ outcome: 'matchClosed' })).toMatch(/tap Correct\.$/);
    });
    it('the alert counts one item per tap', () => {
        expect(tapNotAppliedAlertText(1, true, 'Not applied: the bout was already decided.')).toBe('Not applied: the bout was already decided.');
        expect(tapNotAppliedAlertText(3, false)).toMatch(/^3 taps were not applied\./);
    });
    it('no em-dash, no "mat", no "default win" in any of the copy', () => {
        const all = [MATCH_CLOSED_REASON, MATCH_CLOSED_ADVICE, RESULT_LOCKED_PLAYED_REASON, RESULT_LOCKED_RUNNING_REASON,
            supersededAlertText(1, true, true), supersededAlertText(2, false, true), displacedAlertText(1, true), displacedAlertText(2, false),
            ...['capped', 'beforeBaseline', 'noTarget', 'notAllowed', 'matchClosed', 'notStarted', 'refused'].map((outcome) => tapNotAppliedText({ outcome })),
            tapNotAppliedAlertText(2, false)];
        for (const t of all) {
            expect(t).not.toMatch(/\u2014/);
            expect(t).not.toMatch(/\bmats?\b/i);
            expect(t).not.toMatch(/default[\s-]+win/i);
        }
    });
});

describe('decisionWord', () => {
    it('collapses every kiken variant to "kiken"', () => {
        expect(decisionWord('kiken')).toBe('kiken');
        expect(decisionWord('kiken-voluntary')).toBe('kiken');
        expect(decisionWord('kiken-injury')).toBe('kiken');
    });
    it('names fusenpai and fusensho as themselves', () => {
        expect(decisionWord('fusenpai')).toBe('fusenpai');
        expect(decisionWord('fusensho')).toBe('fusensho');
    });
    it('is null for anything outside the class', () => {
        expect(decisionWord('fought')).toBeNull();
        expect(decisionWord('hikiwake')).toBeNull();
        expect(decisionWord('daihyosen')).toBeNull();
        expect(decisionWord('')).toBeNull();
        expect(decisionWord(undefined)).toBeNull();
    });
});

describe('the sentence for a start that was blocked by another start', () => {
    it('names the start that was out and says what to do, in the past tense', () => {
        expect(startWasBlockedByStartMessage({ label: 'Pool A · Match 2' })).toBe(
            'Not started: Pool A · Match 2 was being started when this match was asked for. Start it when the court is free.',
        );
    });
    it('is the same label as the present-tense sentence names, and a different sentence', () => {
        const label = 'Match 3 (Final)';
        expect(startWhileStartingMessage({ label })).toContain(label);
        expect(startWasBlockedByStartMessage({ label })).toContain(label);
        expect(startWasBlockedByStartMessage({ label })).not.toBe(startWhileStartingMessage({ label }));
    });
});

// A thrown refusal is toasted ONCE. editMatchScore (admin.jsx) toasts what it
// throws; the two hosts that start a match catch the same error and used to toast
// the same sentence again, which replaced the single-slot toast and restarted its
// timer. editMatchScore marks the error it toasted, and a host asks before it does.
describe('markToasted / wasToasted', () => {
    it('an error marked toasted reads as toasted, and markToasted hands the same error back', () => {
        const err = new Error('x');
        expect(wasToasted(err)).toBe(false);
        expect(markToasted(err)).toBe(err);
        expect(wasToasted(err)).toBe(true);
    });
    it('anything that is not an error object reads as not toasted and does not throw', () => {
        expect(wasToasted(undefined)).toBe(false);
        expect(wasToasted(null)).toBe(false);
        expect(wasToasted('string')).toBe(false);
        expect(wasToasted(7)).toBe(false);
        expect(wasToasted({})).toBe(false);
    });
    it('marking a non-object does not throw, and leaves it not toasted', () => {
        expect(() => markToasted(undefined)).not.toThrow();
        expect(() => markToasted('string')).not.toThrow();
        expect(wasToasted(markToasted('string'))).toBe(false);
    });
});
