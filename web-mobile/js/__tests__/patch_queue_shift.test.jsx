import { describe, it, expect } from 'vitest';
import { applyPatch, applyPatchToCompetitions } from '../patch.jsx';
import { applyPatchToTree } from '../court_bridge.jsx';

// A patch holds one competition, so it keeps the server's court-wide queue
// numbers and moves only that competition's own matches on the court a match
// leaves or enters (patch.jsx, shiftQueuePositions). A holder of every
// competition re-stamps the court-wide numbers instead.

const pm = (id, scheduledAt, status, queuePosition, extra = {}) =>
    ({ id, court: "A", scheduledAt, status, queuePosition, ...extra });

const comp = (poolMatches, id = "c1") => ({ id, poolMatches, bracket: { rounds: [] } });

const pos = (c) => Object.fromEntries(c.poolMatches.map((m) => [m.id, m.queuePosition || 0]));

// No two matches of one competition on one court share a number.
function expectDistinctPositions(c) {
    const seen = new Map();
    const all = [...(c.poolMatches || []), ...((c.bracket && c.bracket.rounds) || []).flat()];
    if (c.bracket && c.bracket.thirdPlaceMatch) all.push(c.bracket.thirdPlaceMatch);
    for (const m of all) {
        if (!m.queuePosition) continue;
        const key = `${m.court || ""}\u0000${m.queuePosition}`;
        expect(seen.has(key), `${m.id} and ${seen.get(key)} share ${key}`).toBe(false);
        seen.set(key, m.id);
    }
}

const patch = (prev, result) => applyPatch(prev, { type: "match_updated", data: { result } });

describe('applyPatch renumbers the competition own queue on its court', () => {
    it('the match at 1 finishing moves the one at 2 to 1 and the one at 3 to 2', () => {
        const prev = comp([pm("p1", "09:00", "scheduled", 1), pm("p2", "09:10", "scheduled", 2), pm("p3", "09:20", "scheduled", 3)]);
        const next = patch(prev, { id: "p1", status: "completed", winner: "Alice" });
        expect(pos(next)).toEqual({ p1: 0, p2: 1, p3: 2 });
        expectDistinctPositions(next);
    });

    it('a finish leaves the siblings numbered before it as they are', () => {
        const prev = comp([pm("p1", "09:00", "scheduled", 1), pm("p2", "09:10", "scheduled", 2), pm("p3", "09:20", "scheduled", 3)]);
        const next = patch(prev, { id: "p2", status: "completed", winner: "Alice" });
        expect(pos(next)).toEqual({ p1: 1, p2: 0, p3: 2 });
        expect(next.poolMatches[0]).toBe(prev.poolMatches[0]);
        expectDistinctPositions(next);
    });

    it('a send back of an earlier-timed match takes its turn and the later siblings move up', () => {
        const prev = comp([pm("p1", "09:00", "running", 0), pm("p2", "09:10", "scheduled", 1), pm("p3", "09:20", "scheduled", 2)]);
        const next = patch(prev, { id: "p1", status: "scheduled" });
        expect(pos(next)).toEqual({ p1: 1, p2: 2, p3: 3 });
        expectDistinctPositions(next);
    });

    it('a send back between two siblings takes the place after the earlier one', () => {
        // The server's numbers count another competition's matches too (3 is one).
        const prev = comp([pm("p1", "09:00", "scheduled", 2), pm("p2", "09:10", "running", 0), pm("p3", "09:20", "scheduled", 4)]);
        const next = patch(prev, { id: "p2", status: "scheduled" });
        expect(pos(next)).toEqual({ p1: 2, p2: 3, p3: 5 });
        expect(next.poolMatches[0]).toBe(prev.poolMatches[0]);
        expectDistinctPositions(next);
    });

    it('a bar recorded on a waiting match takes it out of the queue', () => {
        const prev = comp([pm("p1", "09:00", "scheduled", 1), pm("p2", "09:10", "scheduled", 2), pm("p3", "09:20", "scheduled", 3)]);
        const next = patch(prev, { id: "p2", status: "scheduled", ineligibleSides: { a: "fusenpai" } });
        expect(pos(next)).toEqual({ p1: 1, p2: 0, p3: 2 });
        expectDistinctPositions(next);
    });

    it('a bar lifted puts the match back in its turn', () => {
        const prev = comp([
            pm("p1", "09:00", "scheduled", 1),
            pm("p2", "09:10", "scheduled", 0, { ineligibleSides: { a: "kiken-injury" } }),
            pm("p3", "09:20", "scheduled", 2),
        ]);
        const next = patch(prev, { id: "p2", status: "scheduled", ineligibleSides: {} });
        expect(pos(next)).toEqual({ p1: 1, p2: 2, p3: 3 });
        expectDistinctPositions(next);
    });

    it('a time move of a waiting match is a leave then an enter', () => {
        const prev = comp([pm("p1", "09:00", "scheduled", 1), pm("p2", "09:10", "scheduled", 2), pm("p3", "09:20", "scheduled", 3)]);
        const next = patch(prev, { id: "p1", status: "scheduled", scheduledAt: "09:30" });
        expect(pos(next)).toEqual({ p1: 3, p2: 1, p3: 2 });
        expectDistinctPositions(next);
    });

    it('a match entering a court where none of its siblings is numbered takes 1', () => {
        const prev = comp([pm("p1", "09:00", "completed", 0), pm("p2", "09:10", "running", 0)]);
        const next = patch(prev, { id: "p1", status: "scheduled", winner: null });
        expect(pos(next)).toEqual({ p1: 1, p2: 0 });
    });

    it('a match entering behind a waiting sibling with no number stays 0 for the refetch', () => {
        const prev = comp([pm("p1", "09:00", "scheduled", 0), pm("p2", "09:10", "completed", 0)]);
        const next = patch(prev, { id: "p2", status: "scheduled", winner: null });
        expect(pos(next)).toEqual({ p1: 0, p2: 0 });
    });

    it('a pool match leaving moves the knockout match behind it on the same court', () => {
        const prev = {
            id: "c1",
            poolMatches: [pm("p1", "09:00", "scheduled", 1)],
            bracket: { rounds: [[pm("k1", "10:00", "scheduled", 2)]] },
        };
        const next = patch(prev, { id: "p1", status: "running" });
        expect(next.poolMatches[0].queuePosition).toBe(0);
        expect(next.bracket.rounds[0][0].queuePosition).toBe(1);
        expectDistinctPositions(next);
    });

    it('two matches finishing in one event leave no gap and no duplicate', () => {
        const prev = comp([
            pm("p1", "09:00", "scheduled", 1), pm("p2", "09:10", "scheduled", 2),
            pm("p3", "09:20", "scheduled", 3), pm("p4", "09:30", "scheduled", 4),
        ]);
        const next = applyPatch(prev, {
            type: "match_updated",
            data: { results: [{ id: "p3", status: "completed" }, { id: "p1", status: "completed" }] },
        });
        expect(pos(next)).toEqual({ p1: 0, p2: 1, p3: 0, p4: 2 });
        expectDistinctPositions(next);
    });

    it('an unrelated patch keeps every match and array as they were', () => {
        const prev = comp([pm("p1", "09:00", "running", 0), pm("p2", "09:10", "scheduled", 1)]);
        prev.bracket = { rounds: [[pm("k1", "10:00", "scheduled", 2)]] };
        const scored = patch(prev, { id: "p1", status: "running", ipponsA: ["M"] });
        expect(scored.poolMatches[1]).toBe(prev.poolMatches[1]);
        expect(scored.bracket).toBe(prev.bracket);
        expect(patch(prev, { id: "elsewhere", status: "completed" })).toBe(prev);
    });
});

describe('a holder of every competition re-stamps the court-wide numbers', () => {
    const two = () => [
        comp([pm("a1", "09:00", "scheduled", 1), pm("a2", "09:20", "scheduled", 3)], "cA"),
        comp([pm("b1", "09:10", "scheduled", 2)], "cB"),
        comp([{ id: "x1", court: "B", scheduledAt: "09:00", status: "scheduled", queuePosition: 1 }], "cX"),
    ];

    it('another competition match on the court moves up when one leaves', () => {
        const prev = two();
        const next = applyPatchToCompetitions(prev, "cA", { type: "match_updated", data: { result: { id: "a1", status: "running" } } });
        expect(pos(next[0])).toEqual({ a1: 0, a2: 2 });
        expect(pos(next[1])).toEqual({ b1: 1 });
        // Court B's competition is untouched.
        expect(next[2]).toBe(prev[2]);
    });

    it('a patch that moves no queue keeps the list and the other competitions', () => {
        const prev = two();
        const next = applyPatchToCompetitions(prev, "cA", { type: "match_updated", data: { result: { id: "a1", ipponsA: ["M"] } } });
        expect(next).not.toBe(prev);
        expect(next[1]).toBe(prev[1]);
        expect(next[2]).toBe(prev[2]);
        expect(applyPatchToCompetitions(prev, "cA", { type: "match_updated", data: { result: { id: "zz" } } })).toBe(prev);
    });

    it('a display tree is re-stamped the same way', () => {
        const t = { name: "T", competitions: two() };
        const next = applyPatchToTree(t, { type: "patch", compId: "cA", payload: { result: { id: "a1", status: "running" } } });
        expect(pos(next.competitions[0])).toEqual({ a1: 0, a2: 2 });
        expect(pos(next.competitions[1])).toEqual({ b1: 1 });
        expect(next.competitions[2]).toBe(t.competitions[2]);
    });
});
