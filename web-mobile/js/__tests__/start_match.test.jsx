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
    START_CLOCK_SKEW_MESSAGE, START_FAILED_MESSAGE, START_SUPERSEDED_MESSAGE,
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

    it('a superseded start is a refused start, in its own words: nothing was written and nothing will land later', () => {
        // A newer change to the match's result is already stored. The start is
        // refused like a clock_skew one (a stored notice and a toast on both
        // hosts), not waved through: a host that took it for a start that went
        // out would pin a panel on a match the server did not start, say nothing
        // to the operator, and word a past-tense "was being started" for a start
        // that wrote nothing (PR #463 round 18).
        const out = classifyStartOutcome({ applied: false, reason: 'superseded' });
        expect(out.ok).toBe(false);
        expect(out.msg).toBe(START_SUPERSEDED_MESSAGE);
        expect(out.msg).toBe('Not started: a newer change to this match was recorded first. Check the match before starting it.');
    });

    it('a superseded answer that also holds groups (heldGroups) is the same refusal', () => {
        expect(classifyStartOutcome({ applied: false, reason: 'superseded', heldGroups: ['result'] }))
            .toEqual({ ok: false, msg: START_SUPERSEDED_MESSAGE });
    });

    it('only a superseded or clock_skew answer is a refusal: landed, queued and body-less starts did go out', () => {
        for (const res of [{ applied: true }, { queued: true }, { status: 'ok' }, undefined, null]) {
            expect(classifyStartOutcome(res).ok, JSON.stringify(res)).toBe(true);
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
        const all = START_CLOCK_SKEW_MESSAGE + START_FAILED_MESSAGE + START_SUPERSEDED_MESSAGE;
        expect(all).not.toMatch(/—/);
        expect(all).not.toMatch(/\bmats?\b/i);
    });

    // The two refusals give opposite advice: after a clock refusal the clock has
    // been resynced and a second tap normally succeeds, but a superseded start
    // means a NEWER change to the match is stored, and starting it again blind
    // is what the sentence must not invite.
    it('the superseded sentence never says "try again": it sends the operator to the match', () => {
        expect(START_SUPERSEDED_MESSAGE).not.toMatch(/try again/i);
        expect(START_SUPERSEDED_MESSAGE).toMatch(/Check the match/);
        expect(START_CLOCK_SKEW_MESSAGE).toMatch(/try again/);
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
