// bc-kcdg: the downstream_knockout_played refusal (correcting a completed
// knockout match whose later round has already been played) is a THIRD
// not-landed shape owned by write_result.jsx, distinct from the 200
// {applied:false} shapes the rest of the file covers. api_client.jsx parses
// the server's 409 into a thrown Error carrying `.downstreamKnockoutPlayed`;
// these tests pin the predicate and the confirm-dialog copy in isolation of
// that parsing.

import { describe, it, expect } from 'vitest';
import {
    downstreamKnockoutPlayedRefusal,
    downstreamKnockoutPlayedConfirm,
    DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED,
    downstreamKnockoutReopenedNotice,
    matchLabel,
} from '../write_result.jsx';

describe('downstreamKnockoutPlayedRefusal', () => {
    it('reads the structured payload off a decorated Error', () => {
        const err = new Error('downstream_knockout_played');
        err.downstreamKnockoutPlayed = { matchId: 'm1', blockingMatchId: 'm5', displaced: 'Aoki Taro' };
        expect(downstreamKnockoutPlayedRefusal(err)).toEqual({
            matchId: 'm1', blockingMatchId: 'm5', displaced: 'Aoki Taro',
        });
    });

    it('is null for a plain error with no decoration', () => {
        expect(downstreamKnockoutPlayedRefusal(new Error('Failed to record score'))).toBeNull();
    });

    it('is null for a decorated error whose payload is falsy', () => {
        const err = new Error('x');
        err.downstreamKnockoutPlayed = null;
        expect(downstreamKnockoutPlayedRefusal(err)).toBeNull();
    });

    it('does not throw on null/undefined', () => {
        expect(downstreamKnockoutPlayedRefusal(null)).toBeNull();
        expect(downstreamKnockoutPlayedRefusal(undefined)).toBeNull();
    });
});

describe('downstreamKnockoutPlayedConfirm', () => {
    it('names the blocking match and states what confirming does', () => {
        const { message, confirmLabel, danger } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-r2-0',
            blockingMatches: [{ id: 'm-r2-0', number: 7 }],
            displaced: 'Suzuki Ichiro',
        });
        // Names the match the way the operator sees it, never the internal id.
        expect(message).toContain('Match 7');
        expect(message).not.toContain('m-r2-0');
        // Names the displaced competitor.
        expect(message).toContain('Suzuki Ichiro');
        // States plainly what confirming DOES to the later match -- reopens it,
        // clearing its recorded result -- rather than just "proceed anyway".
        // The wording says reopen, not "back to the queue", because the match
        // was already played and is reopened IN PLACE: the queue is untouched
        // (operator ruling 2026-09-19).
        expect(message.toLowerCase()).toContain('reopens match');
        expect(message.toLowerCase()).toContain('re-entry');
        expect(message.toLowerCase()).toContain('cleared');
        // Never the word "mat" (kendo has no mats -- CLAUDE.md).
        expect(message.toLowerCase()).not.toMatch(/\bmat\b/);
        expect(confirmLabel).toBeTruthy();
        expect(danger).toBe(true);
    });

    it('falls back to generic wording when displaced/blockingMatchId are missing', () => {
        const { message } = downstreamKnockoutPlayedConfirm({});
        expect(message).toBeTruthy();
        expect(message.toLowerCase()).toContain('reopens');
    });

    it('defaults to an empty object so it never throws on a bare call', () => {
        expect(() => downstreamKnockoutPlayedConfirm()).not.toThrow();
    });
});

describe('DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED', () => {
    it('is a non-empty, distinct string from the other not-landed copy', () => {
        expect(DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED).toBeTruthy();
        expect(typeof DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED).toBe('string');
    });
});

describe('downstreamKnockoutPlayedConfirm with two blocked matches', () => {
    // A semifinal feeds both the final and the bronze match. Both are cleared
    // by one confirmation, so the dialog has to NAME both: naming one while
    // clearing two is the defect this wording exists to avoid, and splitting
    // them into two dialogs is impossible (the second would never be asked,
    // because after the first confirmation the winner no longer changes).
    it('names every match it is about to clear, in the plural', () => {
        const { message } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-bronze',
            blockingMatches: [{ id: 'm-bronze', number: 4 }, { id: 'm-r2-0', number: 3 }],
            displaced: 'Suzuki Ichiro',
        });
        // Both, by number, and never by the ids the operator has never seen.
        expect(message).toContain('Match 4');
        expect(message).toContain('Match 3');
        expect(message).not.toContain('m-bronze');
        expect(message).not.toContain('m-r2-0');
        expect(message.toLowerCase()).toContain('reopens both');
        expect(message.toLowerCase()).toContain('cleared');
        expect(message.toLowerCase()).not.toMatch(/\bmat\b/);
        // No competitor is named when TWO matches block. They hold different
        // people -- the final its winner, the bronze its loser -- so one name
        // is false of one of them. The dialog read "Ren Takada already played
        // the 3rd-place match and Match 3" when Ren had played only the bronze.
        expect(message).not.toContain('Suzuki Ichiro');
    });

    it('stays singular when only one match is blocked', () => {
        const { message } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-r2-0',
            blockingMatches: [{ id: 'm-r2-0', number: 3 }],
            displaced: 'Suzuki Ichiro',
        });
        expect(message).toContain('Match 3');
        expect(message.toLowerCase()).not.toContain('were reopened');
    });
});

describe('downstreamKnockoutReopenedNotice', () => {
    // The operator confirmed something specific. Telling them only that the
    // save worked leaves "did the later match actually reopen?" to be answered
    // by reading the board, which is what the confirmation was supposed to
    // settle.
    it('names the single match that was reopened, by its number', () => {
        expect(downstreamKnockoutReopenedNotice([{ id: 'm-r2-0', number: 3 }]))
            .toBe('Match 3 was reopened: it must be fought and scored again.');
    });

    it('falls back to the id when a match carries no number', () => {
        // A bye placeholder, or a bracket saved before numbering existed.
        // Better a bare id than "Match 0".
        expect(downstreamKnockoutReopenedNotice([{ id: 'm-r2-0', number: 0 }]))
            .toContain('m-r2-0');
    });

    it('names both when a semifinal reopened its two siblings', () => {
        const notice = downstreamKnockoutReopenedNotice([
            { id: 'm-bronze', number: 4 }, { id: 'm-r2-0', number: 3 },
        ]);
        expect(notice).toContain('Match 4');
        expect(notice).toContain('Match 3');
        expect(notice).toContain('were reopened');
    });

    it('says nothing when nothing was reopened', () => {
        expect(downstreamKnockoutReopenedNotice([])).toBeNull();
        expect(downstreamKnockoutReopenedNotice(undefined)).toBeNull();
    });
});

describe('matchLabel names the 3rd-place match', () => {
    // The bronze is numbered neither in the app nor on the printed tree (the
    // numbering walks the bracket's rounds and the bronze hangs off its own
    // field), so the id fallback would have put "m-bronze" in front of an
    // operator who has never seen an internal id.
    it('names it rather than falling back to its id', () => {
        expect(matchLabel({ id: 'm-bronze', number: 0 })).toBe('the 3rd-place match');
    });

    it('still prefers a number when one exists', () => {
        expect(matchLabel({ id: 'm-bronze', number: 4 })).toBe('Match 4');
    });

    it('reaches the dialog copy, so the refusal never names an id', () => {
        const { message } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-bronze',
            blockingMatches: [{ id: 'm-bronze', number: 0 }, { id: 'm-r2-0', number: 3 }],
            displaced: 'Suzuki Ichiro',
        });
        expect(message).toContain('the 3rd-place match');
        expect(message).toContain('Match 3');
        expect(message).not.toContain('m-bronze');
    });
});
