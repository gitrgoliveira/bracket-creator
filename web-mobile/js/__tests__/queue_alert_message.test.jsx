import { describe, it, expect } from 'vitest';
import { queueAlertMessage } from '../app.jsx';

// The alert kinds are what the operator actually READS when a result does not
// reach the server, so the wording is behaviour, not decoration.
describe('queueAlertMessage', () => {
    // bc-lww1: a superseded write is deliberately NOT folded into 'rejected'.
    // Both mean "this result is not stored", but they call for OPPOSITE actions:
    // a rejected result must be re-entered, while a superseded one must not be —
    // re-entering re-stamps it with the current clock, so it would beat and undo
    // the newer result that just won.
    it('tells the operator NOT to simply re-enter a superseded result', () => {
        const msg = queueAlertMessage({ kind: 'superseded', count: 1, terminalCount: 1 });
        expect(msg).toBeTruthy();
        expect(msg).toMatch(/not saved/i);
        expect(msg).toMatch(/newer result/i);
        // The distinguishing instruction. 'rejected' says "Re-enter it."; this
        // must not, or the advice actively causes the data loss it reports.
        expect(msg).toMatch(/check what is recorded/i);
        expect(msg).not.toMatch(/^.*\bRe-enter it\.\s*$/i);
    });

    it('still tells the operator to re-enter a genuinely rejected result', () => {
        const msg = queueAlertMessage({ kind: 'rejected', count: 1, terminalCount: 1 });
        expect(msg).toMatch(/re-enter it/i);
        // And it must not have inherited the supersede wording.
        expect(msg).not.toMatch(/newer result/i);
    });

    it('pluralises a superseded batch', () => {
        const msg = queueAlertMessage({ kind: 'superseded', count: 3, terminalCount: 3 });
        expect(msg).toMatch(/3 results were not saved/i);
        expect(msg).toMatch(/matches/i);
    });

    it('returns null for an unknown kind, so nothing is toasted', () => {
        expect(queueAlertMessage({ kind: 'not-a-kind', count: 1, terminalCount: 1 })).toBeNull();
    });
});
