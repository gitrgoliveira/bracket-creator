import { describe, it, expect } from 'vitest';
import { queueAlertMessage, queueAlertToastType } from '../app.jsx';

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
        // bc-mrgc: the result is kept in the match's history, not lost.
        expect(msg).toMatch(/not applied/i);
        expect(msg).toMatch(/newer change/i);
        expect(msg).toMatch(/kept in the match's history/i);
        expect(msg).toMatch(/nothing is lost/i);
        // The distinguishing instruction. 'rejected' says "Re-enter it."; this
        // must not, or the advice actively causes the data loss it reports.
        expect(msg).toMatch(/check the match and its history/i);
        expect(msg).not.toMatch(/^.*\bRe-enter it\.\s*$/i);
    });

    it('still tells the operator to re-enter a genuinely rejected result', () => {
        const msg = queueAlertMessage({ kind: 'rejected', count: 1, terminalCount: 1 });
        expect(msg).toMatch(/re-enter it/i);
        // And it must not have inherited the supersede wording.
        expect(msg).not.toMatch(/newer result/i);
    });

    // A refusal the server worded itself (`sentence`, marked where the replay
    // was refused) says what to do, so it closes the alert as it is: "Re-enter
    // it." after "Check the scores and finish again." contradicted it.
    it('shows a refusal the server worded itself as the alert\'s advice', () => {
        const sentence = "This match's representative bout was removed on another device. Check the scores and finish again.";
        const msg = queueAlertMessage({ kind: 'rejected', count: 1, terminalCount: 1, detail: sentence, sentence: true });
        expect(msg).toBe(`A result was refused by the server and cannot be saved. ${sentence}`);
    });

    it('keeps its own words around a refusal that is only a code', () => {
        const msg = queueAlertMessage({ kind: 'rejected', count: 1, terminalCount: 1, detail: 'conflict' });
        expect(msg).toBe('A result was refused by the server (conflict) and cannot be saved. Re-enter it.');
    });

    it('pluralises a superseded batch', () => {
        const msg = queueAlertMessage({ kind: 'superseded', count: 3, terminalCount: 3 });
        expect(msg).toMatch(/3 results were not applied/i);
        expect(msg).toMatch(/kept in each match's history/i);
        expect(msg).toMatch(/matches/i);
    });

    it('returns null for an unknown kind, so nothing is toasted', () => {
        expect(queueAlertMessage({ kind: 'not-a-kind', count: 1, terminalCount: 1 })).toBeNull();
    });

    // bc-mrgc phase 3: nothing queued is discarded for its age any more (the
    // server orders an old write by its stamp and keeps what loses in the
    // match's history), so the 12-hour discard notice is gone with it.
    it('has no expiry notice: no queued write is discarded for its age', () => {
        expect(queueAlertMessage({ kind: 'expired', count: 1, terminalCount: 1 })).toBeNull();
    });

    // bc-offl (operator decision 2026-09-27, Q2): held results that landed.
    it('confirms held results that were sent, singular and plural', () => {
        expect(queueAlertMessage({ kind: 'sent', count: 1, terminalCount: 1 })).toBe('1 finished result sent.');
        expect(queueAlertMessage({ kind: 'sent', count: 3, terminalCount: 3 })).toBe('3 finished results sent.');
    });
});

describe('queueAlertToastType', () => {
    it('shows a sent confirmation as a success toast', () => {
        expect(queueAlertToastType({ kind: 'sent', count: 1, terminalCount: 1 })).toBe('success');
    });

    it.each(['unreadable', 'rejected', 'superseded', 'server_error', 'auth_required', 'storage_full', 'discarded'])(
        'keeps %s an error toast',
        (kind) => {
            expect(queueAlertToastType({ kind, count: 1, terminalCount: 1 })).toBe('error');
        },
    );
});
