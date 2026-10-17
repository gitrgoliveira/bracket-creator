// bc-evts stage 3, review round 1, M4: the outbox sends each match's writes
// strictly in order, so a write the server keeps refusing holds back that
// match's later changes until it lands or is discarded (other matches go on).
// The alerts say so, and a tap is named as a tap, never as a result.
import { describe, it, expect } from 'vitest';
import { queueAlertMessage } from '../app.jsx';

describe('a write the server keeps refusing', () => {
    it('a result: that match\'s later changes wait until it lands or is discarded', () => {
        const msg = queueAlertMessage({ kind: 'server_error', count: 1, terminalCount: 1, detail: 'HTTP 500' });
        expect(msg).toMatch(/keeps refusing a queued result \(HTTP 500\)/);
        expect(msg).not.toMatch(/later results are still sent/i);
        expect(msg).toMatch(/later changes to that match wait until it lands or is discarded/i);
        expect(msg).toMatch(/discard the held result and enter it again/i);
    });

    it('a running score update is named as a change', () => {
        const msg = queueAlertMessage({ kind: 'server_error', count: 1, terminalCount: 0, detail: 'HTTP 500' });
        expect(msg).toMatch(/keeps refusing a queued change \(HTTP 500\)/);
        expect(msg).not.toMatch(/result/i);
    });

    it('taps are named as taps', () => {
        const msg = queueAlertMessage({ kind: 'server_error', count: 3, terminalCount: 0, taps: true, detail: 'HTTP 503' });
        expect(msg).toMatch(/keeps refusing 3 queued taps \(HTTP 503\)/);
        expect(msg).toMatch(/later changes to that match wait until they land or are discarded/i);
        expect(msg).not.toMatch(/result/i);
    });
});

describe('signing in again', () => {
    it('names taps as taps', () => {
        expect(queueAlertMessage({ kind: 'auth_required', count: 2, terminalCount: 0, taps: true }))
            .toBe('Sign in again to save 2 pending taps. They are still queued.');
        expect(queueAlertMessage({ kind: 'auth_required', count: 1, terminalCount: 0, taps: true }))
            .toBe('Sign in again to save 1 pending tap. It is still queued.');
    });

    it('names a running score update a change, and a result as a result', () => {
        expect(queueAlertMessage({ kind: 'auth_required', count: 1, terminalCount: 0 }))
            .toBe('Sign in again to save 1 pending change. It is still queued.');
        expect(queueAlertMessage({ kind: 'auth_required', count: 2, terminalCount: 2 }))
            .toBe('Sign in again to save 2 pending results. They are still queued.');
    });
});
