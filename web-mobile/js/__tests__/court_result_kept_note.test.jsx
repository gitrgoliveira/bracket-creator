// bc-sseq FR8: the editors' kept-in-history note (keptInHistoryNote) for the
// court's result.
//   - The court's result recorded over a result another device recorded
//     FIRST moves that one to the history: "Saved", the court's result counts,
//     and never "a later change" (it was made first).
//   - A court result held because other matches were built on the result it
//     would replace reads the built-on reason, never "the next round has
//     already started" (resultLockedReason owns the words of every lock).
//   - A write whose result the court's replaced, applied in part, says the
//     court's result counts rather than "a newer change".
import { describe, it, expect } from 'vitest';
import { keptInHistoryNote, displacedGroupsNote, COURT_RESULT_DISPLACED_NOTE } from '../match_groups.jsx';
import {
    writeDisplacedByCourtResult, HELD_REASON_BUILT_ON, HELD_REASON_COURT_RESULT, HELD_REASON_MATCH_CLOSED,
    RESULT_BUILT_ON_REASON, RESULT_LOCKED_RUNNING_REASON, RESULT_LOCKED_PLAYED_REASON, COURT_RESULT_REASON,
} from '../write_result.jsx';

const cap = (s) => s.replace(/^./, (c) => c.toUpperCase());

describe("the court's result replaced a result recorded first", () => {
    const court = { id: 'm1', status: 'completed', displacedGroups: ['points', 'result'] };

    it('is read by writeDisplacedByCourtResult, and a closed-match move is not', () => {
        expect(writeDisplacedByCourtResult(court)).toBe(true);
        expect(writeDisplacedByCourtResult({ ...court, heldReason: HELD_REASON_MATCH_CLOSED })).toBe(false);
        // A representative pick moved by the bout's removal names no result.
        expect(writeDisplacedByCourtResult({ displacedGroups: ['repPickA'] })).toBe(false);
        expect(writeDisplacedByCourtResult({})).toBe(false);
        expect(writeDisplacedByCourtResult(null)).toBe(false);
    });

    it('says Saved and that the court counts, never a later change', () => {
        const note = keptInHistoryNote(court);
        expect(note).toBe(COURT_RESULT_DISPLACED_NOTE);
        expect(note).toMatch(/^Saved/);
        expect(note).not.toMatch(/later change/i);
        expect(displacedGroupsNote(['result'], false, true)).toBe(COURT_RESULT_DISPLACED_NOTE);
    });

    it('a later change the closed-match rule moved keeps its own words', () => {
        const closed = { displacedGroups: ['points'], heldReason: HELD_REASON_MATCH_CLOSED };
        expect(keptInHistoryNote(closed)).toBe("Saved. A later change to points was made after the match was finished, so it was moved to the match's history.");
        expect(keptInHistoryNote({ displacedGroups: ['repPickA'] })).toBe("Saved. A later change to Aka's pick for the representative bout was moved to the match's history.");
    });
});

describe('a court result held because other matches were built on the result', () => {
    it('reads the built-on reason, not a knockout round', () => {
        const note = keptInHistoryNote({ applied: false, reason: 'superseded', heldGroups: ['result'], heldReason: HELD_REASON_BUILT_ON });
        expect(note).toContain(cap(RESULT_BUILT_ON_REASON));
        expect(note).not.toContain(RESULT_LOCKED_RUNNING_REASON);
        expect(note).not.toContain(RESULT_LOCKED_PLAYED_REASON);
        expect(note).toContain('tap Correct');
    });

    it('the knockout locks keep their words', () => {
        expect(keptInHistoryNote({ applied: false, reason: 'superseded', heldGroups: ['result'], heldReason: 'downstream_knockout_played' }))
            .toContain(cap(RESULT_LOCKED_PLAYED_REASON));
        expect(keptInHistoryNote({ applied: false, reason: 'superseded', heldGroups: ['result'], heldReason: 'downstream_knockout_running' }))
            .toContain(cap(RESULT_LOCKED_RUNNING_REASON));
    });
});

describe("a write whose result the court's replaced, applied in part", () => {
    it("says the court's result counts, not a newer change", () => {
        const note = keptInHistoryNote({ id: 'm1', status: 'completed', heldGroups: ['result'], heldReason: HELD_REASON_COURT_RESULT });
        expect(note).toBe(`Kept in the match's history, not applied: the result. ${cap(COURT_RESULT_REASON)}.`);
        expect(note).not.toMatch(/newer change/);
    });
});
