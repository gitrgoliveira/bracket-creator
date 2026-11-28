// bc-sseq FR8: the two held reasons of the court's result, each with ONE
// reader in write_result.jsx.
//   - court_result (writeCourtResult): a result from a device without the
//     court's standing that a later-stamped court result replaced. The court's
//     result counts; nothing to correct or enter again.
//   - downstream_built_on (writeResultLocked 'built_on'): a court result held
//     because other matches were built on the one it would replace. Correct is
//     the remedy, like a played knockout match.
import { describe, it, expect } from 'vitest';
import {
    writeCourtResult, writeResultLocked, resultLockedReason, supersededBanner, notLandedBanner, notSavedText,
    writeMatchClosed, isCourtResultWrite, HELD_REASON_COURT_RESULT, HELD_REASON_BUILT_ON,
    RESULT_LOCKED_PLAYED_REASON, RESULT_LOCKED_RUNNING_REASON, RESULT_BUILT_ON_REASON, COURT_RESULT_REASON,
    MATCH_CLOSED_ADVICE, SUPERSEDED_LEAD,
} from '../write_result.jsx';

const superseded = (extra) => ({ applied: false, reason: 'superseded', heldGroups: ['result'], ...extra });

describe('a result the court replaced (court_result)', () => {
    const court = superseded({
        heldReason: HELD_REASON_COURT_RESULT,
        message: "Not applied: the court running shiaijo A recorded a different result for this match. The court's result counts; yours is kept in the match's history.",
    });

    it('is read by writeCourtResult alone, and is neither match_closed nor a lock', () => {
        expect(HELD_REASON_COURT_RESULT).toBe('court_result');
        expect(writeCourtResult(court)).toBe(true);
        expect(writeMatchClosed(court)).toBe(false);
        expect(writeResultLocked(court)).toBeNull();
        expect(writeCourtResult(superseded({ heldReason: 'match_closed' }))).toBe(false);
        expect(writeCourtResult(null)).toBe(false);
    });

    it("shows the server's sentence once, with no advice to correct or enter again", () => {
        const b = supersededBanner(court);
        expect(b).toEqual({
            lead: SUPERSEDED_LEAD, sentence: true,
            reason: "the court running shiaijo A recorded a different result for this match. The court's result counts; yours is kept in the match's history.",
        });
        expect(notSavedText(notLandedBanner(court))).toBe(
            "Not applied: the court running shiaijo A recorded a different result for this match. The court's result counts; yours is kept in the match's history.",
        );
    });

    it('says the same in its own words when the server sends none', () => {
        const b = supersededBanner(superseded({ heldReason: HELD_REASON_COURT_RESULT }));
        expect(notSavedText(b)).toBe(`Not applied: ${COURT_RESULT_REASON}.`);
        expect(notSavedText(b)).not.toMatch(/Correct|enter/i);
    });
});

describe('a court result held because other matches were built on the result (downstream_built_on)', () => {
    const builtOn = superseded({
        heldReason: HELD_REASON_BUILT_ON,
        message: "Not applied: round 2 was already paired from the result another device recorded for this match. The court's result is kept in the match's history. To use it, tap Correct; round 2 keeps its pairings.",
    });

    it('is read through writeResultLocked, as the knockout lock is', () => {
        expect(HELD_REASON_BUILT_ON).toBe('downstream_built_on');
        expect(writeResultLocked(builtOn)).toBe('built_on');
        expect(writeResultLocked(superseded({ heldReason: 'downstream_knockout_played' }))).toBe('played');
        expect(writeResultLocked(superseded({ heldReason: 'downstream_knockout_running' }))).toBe('running');
        expect(writeCourtResult(builtOn)).toBe(false);
    });

    it("shows the server's sentence, naming the case and Correct, once", () => {
        const text = notSavedText(notLandedBanner(builtOn));
        expect(text).toBe("Not applied: round 2 was already paired from the result another device recorded for this match. The court's result is kept in the match's history. To use it, tap Correct; round 2 keeps its pairings.");
        expect(text.match(/Not applied/g)).toHaveLength(1);
    });

    it('without a sentence, says why and that Correct is the way', () => {
        const b = supersededBanner(superseded({ heldReason: HELD_REASON_BUILT_ON }));
        expect(b).toEqual({ lead: SUPERSEDED_LEAD, reason: RESULT_BUILT_ON_REASON, advice: MATCH_CLOSED_ADVICE });
        expect(resultLockedReason('played')).toBe(RESULT_LOCKED_PLAYED_REASON);
        expect(resultLockedReason('running')).toBe(RESULT_LOCKED_RUNNING_REASON);
        expect(resultLockedReason('built_on')).toBe(RESULT_BUILT_ON_REASON);
        expect(resultLockedReason(null)).toBeNull();
    });
});

describe('the server sentence that leads with the banner word is said once', () => {
    it('a match_closed answer whose message starts "Not applied:" does not double it', () => {
        const res = superseded({ heldReason: 'match_closed', message: "Not applied: this match is already finished, so this change was kept in the match's history. To change the result, tap Correct." });
        expect(notSavedText(notLandedBanner(res))).toBe("Not applied: this match is already finished, so this change was kept in the match's history. To change the result, tap Correct.");
    });
});

describe('isCourtResultWrite', () => {
    const cc = { tabId: '11111111-1111-4111-8111-111111111111', deviceId: '22222222-2222-4222-8222-222222222222' };
    it('is a queued finished result stamped with the court control', () => {
        expect(isCourtResultWrite({ kind: 'score', terminal: true, payload: { status: 'completed', courtControl: cc } })).toBe(true);
        expect(isCourtResultWrite({ kind: 'decision', terminal: true, payload: { decision: 'kiken', courtControl: cc } })).toBe(true);
    });
    it('is not a running write, an unstamped result or another kind', () => {
        expect(isCourtResultWrite({ kind: 'score', terminal: false, payload: { status: 'running', courtControl: cc } })).toBe(false);
        expect(isCourtResultWrite({ kind: 'score', terminal: true, payload: { status: 'completed' } })).toBe(false);
        expect(isCourtResultWrite({ kind: 'evt', payload: { courtControl: cc } })).toBe(false);
        expect(isCourtResultWrite(null)).toBe(false);
    });
});
