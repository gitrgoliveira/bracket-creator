// bc-aadv / PR #463 round 13 (gap C): the ONE owner of "what did a Start write
// come back with" (start_match.jsx). The court console and the Scores tab both
// start a match, and before this leaf only the console read the answer: a
// clock_skew refusal (HTTP 200 {applied:false}) was taken for a start that
// landed on the Scores tab, a thrown 409 was swallowed, and nothing stopped a
// second start while one was out.

import { describe, it, expect } from 'vitest';
import { CLOCK_SKEW_REASON_TEXT } from '../write_result.jsx';
import {
    classifyStartOutcome, startFailureMessage, createStartGuard, startPatch, startRefusalStands,
    START_CLOCK_SKEW_MESSAGE, START_FAILED_MESSAGE,
} from '../start_match.jsx';

describe('classifyStartOutcome', () => {
    it('a clock_skew refusal is a start that did not happen, in the console\'s own words', () => {
        const out = classifyStartOutcome({ applied: false, reason: 'clock_skew' });
        expect(out.ok).toBe(false);
        expect(out.msg).toBe(`Could not start: ${CLOCK_SKEW_REASON_TEXT}. The clock has been resynced; try again.`);
        expect(out.msg).toBe(START_CLOCK_SKEW_MESSAGE);
    });

    it('a queued start lands on reconnect, so it counts as started', () => {
        expect(classifyStartOutcome({ queued: true })).toEqual({ ok: true });
    });

    it('a landed start, an answer with no body and nothing at all are all started', () => {
        expect(classifyStartOutcome({ status: 'ok' })).toEqual({ ok: true });
        expect(classifyStartOutcome({ applied: true })).toEqual({ ok: true });
        expect(classifyStartOutcome(undefined)).toEqual({ ok: true });
        expect(classifyStartOutcome(null)).toEqual({ ok: true });
    });

    it('a superseded start is not a clock refusal to report, but it is not a start that went out either', () => {
        // Nothing of it was written: a newer change to the match's result is
        // already stored, so a host that records "the start went out" must not.
        expect(classifyStartOutcome({ applied: false, reason: 'superseded' })).toEqual({ ok: true, superseded: true });
    });

    it('only a superseded answer carries the flag: landed, queued and body-less starts did go out', () => {
        for (const res of [{ applied: true }, { queued: true }, { status: 'ok' }, undefined, null]) {
            expect(classifyStartOutcome(res).superseded, JSON.stringify(res)).toBeUndefined();
        }
    });
});

describe('startFailureMessage', () => {
    it('is the thrown error\'s own sentence', () => {
        expect(startFailureMessage(new Error('Alice withdrew in Pool A · Match 1 and cannot fight again.')))
            .toBe('Alice withdrew in Pool A · Match 1 and cannot fight again.');
    });

    it('falls back to one sentence when the error says nothing', () => {
        expect(startFailureMessage(new Error(''))).toBe(START_FAILED_MESSAGE);
        expect(startFailureMessage(undefined)).toBe(START_FAILED_MESSAGE);
        expect(START_FAILED_MESSAGE).toBe('Could not start the match: check eligibility and try again.');
    });
});

describe('the start copy follows the house words', () => {
    it('carries no em-dash and never says mat', () => {
        const all = START_CLOCK_SKEW_MESSAGE + START_FAILED_MESSAGE;
        expect(all).not.toMatch(/—/);
        expect(all).not.toMatch(/\bmats?\b/i);
    });
});

describe('createStartGuard', () => {
    it('starts nothing out until begin is called', () => {
        expect(createStartGuard().inFlight()).toBeNull();
    });

    it('takes one start at a time: another match is refused while one is out, naming it', () => {
        const g = createStartGuard();
        const a = { id: 'a' };
        expect(g.begin('a', a)).toEqual({ started: true });
        const refused = g.begin('b', { id: 'b' });
        expect(refused.started).toBe(false);
        expect(refused.repeat).toBe(false);
        expect(refused.blocker).toEqual({ key: 'a', match: a });
        expect(g.inFlight()).toEqual({ key: 'a', match: a });
    });

    it('a repeat for the match already being started is refused silently', () => {
        const g = createStartGuard();
        g.begin('a', { id: 'a' });
        const again = g.begin('a', { id: 'a' });
        expect(again.started).toBe(false);
        expect(again.repeat).toBe(true);
    });

    it('end frees it, and only the start that holds it can end it', () => {
        const g = createStartGuard();
        g.begin('a', { id: 'a' });
        g.end('b');
        expect(g.inFlight().key).toBe('a');
        g.end('a');
        expect(g.inFlight()).toBeNull();
        expect(g.begin('b', { id: 'b' })).toEqual({ started: true });
    });
});

describe('startPatch', () => {
    it('is a result-only, start-only write (bc-sbq, bc-mrgc)', () => {
        const p = startPatch();
        expect(p.startOnly).toBe(true);
        expect(p.changed).toEqual(['result']);
        expect(p.status).toBe('running');
        expect(p.winner).toBeNull();
    });

    it('hands out a fresh object each call', () => {
        expect(startPatch()).not.toBe(startPatch());
    });
});

// PR #463 round 15 (F): how long a refused Start notice lives is ONE rule, asked
// by the court console and the Scores tab alike.
describe('startRefusalStands', () => {
    it('stands while the refused match is still scheduled in the list', () => {
        expect(startRefusalStands({ row: { status: 'scheduled' }, listHoldsMatches: true })).toBe(true);
    });

    it('falls when the refused match has left scheduled', () => {
        for (const status of ['running', 'completed']) {
            expect(startRefusalStands({ row: { status }, listHoldsMatches: true })).toBe(false);
        }
    });

    it('falls when a list that holds matches no longer holds the refused one', () => {
        expect(startRefusalStands({ row: null, listHoldsMatches: true })).toBe(false);
        expect(startRefusalStands({ row: undefined, listHoldsMatches: true })).toBe(false);
    });

    it('stands across an empty list: it says nothing about the match', () => {
        expect(startRefusalStands({ row: null, listHoldsMatches: false })).toBe(true);
        expect(startRefusalStands({ row: undefined, listHoldsMatches: false })).toBe(true);
    });
});
