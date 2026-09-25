// bc-kcdg: the downstream_knockout_played refusal (correcting a completed
// knockout match whose later round has already been played) is a THIRD
// not-landed shape owned by write_result.jsx, distinct from the 200
// {applied:false} shapes the rest of the file covers. api_client.jsx parses
// the server's 409 into a thrown Error carrying `.downstreamKnockoutPlayed`;
// these tests pin the predicate and the confirm-dialog copy in isolation of
// that parsing.

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { resolve } from 'path';
import { bracketRoundLabel } from '../bracket.jsx';
import {
    downstreamKnockoutPlayedRefusal,
    downstreamKnockoutPlayedConfirm,
    DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED,
    DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED,
    downstreamKnockoutReopenedNotice,
    downstreamKnockoutRunningMessage,
    matchLabel,
    courtBusyMessage,
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

// The same refusal met by a REOPEN (Reopen match, Clear withdrawal and
// reopen): the operator is reopening a match, not applying a correction, so the
// dialog says what reopening does and the button names that act. api_client's
// reopenFailureError marks the refusal `reopen`.
describe('downstreamKnockoutPlayedConfirm for a reopen', () => {
    it('says reopening this match reopens the later one, never "correction"', () => {
        const { message, confirmLabel, danger } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-r2-0',
            blockingMatches: [{ id: 'm-r2-0', number: 3 }],
            displaced: 'Suzuki Ichiro',
            reopen: true,
        });
        expect(message).toContain('Suzuki Ichiro already played Match 3');
        expect(message).toContain('Reopening this match also reopens Match 3');
        expect(message.toLowerCase()).toContain('cleared');
        expect(message.toLowerCase()).not.toContain('correction');
        expect(confirmLabel).toBe('Reopen both');
        expect(danger).toBe(true);
    });

    it('names every match in the plural and still never says "correction"', () => {
        const { message, confirmLabel } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-bronze',
            blockingMatches: [{ id: 'm-bronze', number: 4 }, { id: 'm-r2-0', number: 3 }],
            displaced: 'Suzuki Ichiro',
            reopen: true,
        });
        expect(message).toContain('Match 4');
        expect(message).toContain('Match 3');
        expect(message).toContain('Reopening this match also reopens both');
        expect(message.toLowerCase()).not.toContain('correction');
        expect(message).not.toContain('Suzuki Ichiro');
        expect(confirmLabel).toBe('Reopen all of them');
    });

    it('has its own cancellation copy', () => {
        expect(DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED).toMatch(/^Reopen cancelled/);
        expect(DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED.toLowerCase()).not.toContain('correction');
        expect(DOWNSTREAM_KNOCKOUT_REOPEN_CANCELLED).not.toBe(DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED);
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

// A POOL correction in a mixed competition that moves who holds a qualifying
// place: the server adds qualifierChange, and the dialog says who moves before
// it says which knockout match reopens. One paragraph, confirm "Apply and
// reopen".
describe('downstreamKnockoutPlayedConfirm with a qualifier change', () => {
    const blocking = { blockingMatchId: 'm-r1-0', blockingMatches: [{ id: 'm-r1-0', number: 9 }] };

    it('names the place, both occupants and the match fought with the old one', () => {
        const { message, confirmLabel, danger } = downstreamKnockoutPlayedConfirm({
            ...blocking,
            displaced: 'Aoki Taro',
            qualifierChange: [{ pool: 'Pool A', rank: 1, place: '1st', from: { name: 'Aoki Taro', id: 'a' }, to: { name: 'Bob', id: 'b' } }],
        });
        expect(message).toBe(
            "Changing this result moves Pool A's 1st place from Aoki Taro to Bob, who takes Aoki Taro's place in the knockout. " +
            'Match 9 was already fought with Aoki Taro: it will be reopened, its result cleared, and it must be fought again.',
        );
        expect(message).not.toMatch(/\n/);
        expect(confirmLabel).toBe('Apply and reopen');
        expect(danger).toBe(true);
    });

    it('lists every place a swap moves, and every match it reopens', () => {
        const { message } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-r1-0',
            blockingMatches: [{ id: 'm-r1-0', number: 9 }, { id: 'm-r1-1', number: 10 }],
            displaced: 'Aoki Taro',
            qualifierChange: [
                { pool: 'Pool A', rank: 1, place: '1st', from: { name: 'Aoki Taro' }, to: { name: 'Bob' } },
                { pool: 'Pool A', rank: 2, place: '2nd', from: { name: 'Bob' }, to: { name: 'Aoki Taro' } },
            ],
        });
        expect(message).toBe(
            "Changing this result changes who holds Pool A's 1st place (Aoki Taro to Bob) and Pool A's 2nd place (Bob to Aoki Taro), and the knockout is changed to match. " +
            'Match 9 and Match 10 were already fought with the competitors being replaced: they will be reopened, their results cleared, and they must be fought again.',
        );
    });

    it('a place left tied names no successor', () => {
        const { message } = downstreamKnockoutPlayedConfirm({
            ...blocking,
            displaced: 'Aoki Taro',
            qualifierChange: [{ pool: 'Pool A', rank: 1, place: '1st', from: { name: 'Aoki Taro' }, to: { name: '' }, tied: true }],
        });
        expect(message).toBe(
            "Changing this result leaves Pool A's 1st place tied, to be settled by a tie-break, so Aoki Taro no longer holds it in the knockout. " +
            'Match 9 was already fought with Aoki Taro: it will be reopened, its result cleared, and fought once the tie-break decides the place.',
        );
    });

    // A reopened match on a tied place cannot be fought until the tie-break
    // has decided who holds that place, so the copy must not promise a
    // straight re-fight. Several tied places, and a mix of moved and tied.
    it('matches on tied places wait for the tie-break', () => {
        const allTied = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-r1-0',
            blockingMatches: [{ id: 'm-r1-0', number: 9 }, { id: 'm-r1-1', number: 10 }],
            qualifierChange: [
                { pool: 'Pool A', rank: 1, place: '1st', from: { name: 'Aoki Taro' }, to: { name: '' }, tied: true },
                { pool: 'Pool A', rank: 2, place: '2nd', from: { name: 'Bob' }, to: { name: '' }, tied: true },
            ],
        });
        expect(allTied.message).toContain(
            'Match 9 and Match 10 were already fought with the competitors being replaced: they will be reopened, their results cleared, and fought once the tie-break decides the places.',
        );
        expect(allTied.message).not.toContain('must be fought again');

        const mixed = downstreamKnockoutPlayedConfirm({
            ...blocking,
            displaced: 'Aoki Taro',
            qualifierChange: [
                { pool: 'Pool A', rank: 1, place: '1st', from: { name: 'Aoki Taro' }, to: { name: 'Bob' } },
                { pool: 'Pool A', rank: 2, place: '2nd', from: { name: 'Bob' }, to: { name: '' }, tied: true },
            ],
        });
        expect(mixed.message).toContain(
            'Match 9 was already fought with Aoki Taro: it will be reopened, its result cleared, and fought again; a match on a tied place waits for the tie-break to decide it.',
        );
    });

    // A rank recorded by hand (chusen) corrects no result, so the dialog
    // does not say it does.
    it('a ranking refusal leads with the ranking, not a result', () => {
        const { message, confirmLabel } = downstreamKnockoutPlayedConfirm({
            ...blocking,
            displaced: 'Aoki Taro',
            ranking: true,
            qualifierChange: [{ pool: 'Pool A', rank: 1, place: '1st', from: { name: 'Aoki Taro' }, to: { name: 'Bob' } }],
        });
        expect(message).toBe(
            "Recording this ranking moves Pool A's 1st place from Aoki Taro to Bob, who takes Aoki Taro's place in the knockout. " +
            'Match 9 was already fought with Aoki Taro: it will be reopened, its result cleared, and it must be fought again.',
        );
        expect(confirmLabel).toBe('Apply and reopen');
    });

    it('an empty qualifierChange is a knockout correction and keeps its own copy', () => {
        const { message, confirmLabel } = downstreamKnockoutPlayedConfirm({ ...blocking, displaced: 'Aoki Taro', qualifierChange: [] });
        expect(message).toContain('Aoki Taro already played Match 9');
        expect(confirmLabel).toBe('Apply correction and reopen');
    });
});

// The terminal refusal: a knockout match the move reaches is being fought now.
describe('downstreamKnockoutRunningMessage', () => {
    it('names the match and the way out', () => {
        expect(downstreamKnockoutRunningMessage([{ id: 'm-r1-0', number: 9 }]))
            .toBe('Match 9 is being fought now. Finish it or send it back to the queue, then save again.');
    });
    it('names several in the plural', () => {
        expect(downstreamKnockoutRunningMessage([{ id: 'm-r1-0', number: 9 }, { id: 'm-bronze', number: 0 }]))
            .toBe('Match 9 and the 3rd-place match are being fought now. Finish them or send them back to the queue, then save again.');
    });
    it('never prints an empty subject', () => {
        expect(downstreamKnockoutRunningMessage(undefined))
            .toBe('A knockout match is being fought now. Finish it or send it back to the queue, then save again.');
    });
});

// bc-rawm: the 409 court_busy sentence for a score write, built client-side
// from the server's `court` + `label` rather than echoed from the server's
// own `message` (api_client.jsx's recordScore, and reopenFailureError's
// reuse of the same shape).
describe('courtBusyMessage', () => {
    it('names the shiaijo and the blocking match by its operator label', () => {
        expect(courtBusyMessage({ court: 'A', label: 'Pool A · Match 2' }))
            .toBe('Shiaijo A is running Pool A · Match 2. Finish it or send it back to the queue first.');
    });
    // bc-cse: no "This shiaijo"/"another match" fallback -- respondCourtBusy
    // (handlers_match.go) always sends both `court` and a `label`
    // (matchLabelOrID falls back to the raw match id rather than omitting
    // it), so the two fallback strings were dead code and were removed.
});

// UAT (bc-tmfn): correcting "Pool A · Match 1" raised a dialog about "Match 1"
// -- the first KNOCKOUT match -- which read as the very match on screen, since
// pool matches are numbered from 1 inside each pool. The server now names each
// knockout match with its round ("Match 1 (Semifinals)", engine.MatchLabel)
// and sends that as `label`; every piece of copy that names a knockout match
// prints it as it is, so the dialog, the refusal and the reopened notice all
// say what the server's own message says.
describe('a server-named knockout match keeps its round in every message', () => {
    const semi = { id: 'm-r2-0', number: 1, label: 'Match 1 (Semifinals)' };

    it('matchLabel prints the label rather than composing its own', () => {
        expect(matchLabel(semi)).toBe('Match 1 (Semifinals)');
        // A match the client names itself (no label) keeps the number form.
        expect(matchLabel({ id: 'm-r2-0', number: 1 })).toBe('Match 1');
    });

    it('the qualifier-move confirm cannot be read as the pool match being corrected', () => {
        const { message } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: semi.id,
            blockingMatches: [semi],
            displaced: 'Goto Ken',
            qualifierChange: [{ pool: 'Pool A', rank: 1, place: '1st', from: { name: 'Goto Ken' }, to: { name: 'Ito Dai' } }],
        });
        expect(message).toContain('Match 1 (Semifinals) was already fought with Goto Ken');
        expect(message).not.toMatch(/Match 1 was/);
    });

    it('the knockout-correction confirm names the round too', () => {
        const { message } = downstreamKnockoutPlayedConfirm({
            blockingMatchId: 'm-r3-0',
            blockingMatches: [{ id: 'm-r3-0', number: 3, label: 'Match 3 (Final)' }],
            displaced: 'Goto Ken',
        });
        expect(message).toContain('Goto Ken already played Match 3 (Final)');
        expect(message).toContain('reopens Match 3 (Final) for re-entry');
    });

    it('the reopened notice and the running refusal name it the same way', () => {
        expect(downstreamKnockoutReopenedNotice([semi]))
            .toBe('Match 1 (Semifinals) was reopened: it must be fought and scored again.');
        expect(downstreamKnockoutRunningMessage([semi]))
            .toBe('Match 1 (Semifinals) is being fought now. Finish it or send it back to the queue, then save again.');
        // A bracket saved before rounds were recorded is named "knockout Match
        // N" by the server; the sentence still starts with a capital.
        expect(downstreamKnockoutRunningMessage([{ id: 'm-r1-0', number: 1, label: 'knockout Match 1' }]))
            .toBe('Knockout Match 1 is being fought now. Finish it or send it back to the queue, then save again.');
    });
});

// The round a dialog names ("Match 3 (Semifinals)") must be the heading of the
// column the operator finds that match under. The server spells it
// (internal/engine/errors.go roundLabelFromEnd) and so does the bracket
// (bracket.jsx roundLabelFromEnd, via bracketRoundLabel); both are driven over
// internal/engine/testdata/round_labels.json. Go half:
// TestRoundLabelFromEnd_GoldenTable.
describe('round label Go/JS mirror', () => {
    const table = JSON.parse(
        readFileSync(
            resolve(__dirname, '..', '..', '..', 'internal', 'engine', 'testdata', 'round_labels.json'),
            'utf8',
        ),
    );

    it('the shared golden table is present and non-empty', () => {
        expect(
            table.cases?.length,
            'internal/engine/testdata/round_labels.json parsed to zero cases: the mirror would assert nothing',
        ).toBeGreaterThan(0);
    });

    it.each(table.cases)('displayRound $displayRound is "$label"', ({ displayRound, label }) => {
        expect(
            bracketRoundLabel({ displayRound }, 0, 0),
            'JS round name disagrees with the shared table; update BOTH spellings, not just this one',
        ).toBe(label);
    });
});
