import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { buildDisplayModel } from '../bracket.jsx';

// JS half of the shared Go/JS golden table for bracket match numbers. Go half:
// TestBracketMatchNumbersGolden in internal/engine/bracket_match_numbers_golden_test.go,
// whose header explains what the contract is and why it is fragile.
//
// A referee reads the printed Excel sheet and the operator's screen side by
// side, so the card labelled "M12" and the sheet's "Match 12" have to be the
// same bout. The engine stamps MatchNumber on every real match and SERVES it
// (state.BracketMatch.MatchNumber, `matchNumber` on the wire), and
// buildDisplayModel now labels its cards with that number instead of deriving a
// third copy of the walk. Two things still need pinning, and neither is x === x:
//
//  1. The SPA surfaces exactly the served numbers. Its REAL-match filter (not
//     hidden && displayRound > 0) has to select exactly the set the engine
//     numbered (not Hidden && not empty-vs-empty) — those are different
//     predicates over the same data — and the bye-slot cards it synthesizes
//     itself must stay out of the numbering.
//  2. The legacy FALLBACK walk still reproduces the engine's numbering exactly.
//     It runs for brackets persisted before matchNumber existed, which still
//     render, and now that the primary path consumes the served value it is the
//     ONLY place the Go/JS ordering agreement is exercised at all.
describe('bracket match numbers: served numbering, and the legacy fallback walk', () => {
  const table = JSON.parse(
    readFileSync(
      resolve(__dirname, '..', '..', '..', 'internal', 'engine', 'testdata', 'bracket_match_numbers.json'),
      'utf8'
    )
  );

  // Load-bearing: it.each over an empty array silently produces zero tests
  // (no red), so a degraded table needs its own failure.
  it('the shared golden table is present and non-empty', () => {
    expect(
      table.cases?.length,
      'internal/engine/testdata/bracket_match_numbers.json parsed to zero cases: the mirror would assert nothing'
    ).toBeGreaterThan(0);
  });

  // Also load-bearing, and less obvious: on most bracket shapes, ordering by
  // leaf slot and ordering by raw position agree, so a table of only those
  // shapes passes even against the pre-fix numbering. At least one case has to
  // be able to tell them apart. What that guards is now the FALLBACK suite
  // below — the served path takes the engine's order as given and never sorts.
  //
  // This side cannot check the flag itself — it has no way to build a bracket —
  // but it does not have to: the Go half MEASURES discrimination on the bracket
  // the engine built and fails if the flag disagrees, so a `true` reaching this
  // file has been earned rather than asserted. See the Go half for how the
  // shapes were found.
  it('the table contains a case that can catch the ordering drift', () => {
    expect(
      table.cases.filter((c) => c.discriminating).length,
      'no discriminating case left in the golden: the fallback suite would pass against the very bug it exists to catch'
    ).toBeGreaterThan(0);
  });

  // The engine's answer for one case: matchId -> number, for every match it
  // numbered.
  const engineNumbers = (rounds) => {
    const out = {};
    rounds.forEach((round) => round.forEach((m) => {
      if (m.matchNumber > 0) out[m.id] = m.matchNumber;
    }));
    return out;
  };

  // A golden case as a bracket saved BEFORE the engine numbered anything:
  // displayRound / hidden / feeders intact, matchNumber gone. Those brackets are
  // the whole reason the local walk still exists.
  const withoutMatchNumbers = (rounds) =>
    rounds.map((round) => round.map(({ matchNumber: _dropped, ...rest }) => rest));

  const allCards = (rounds) => buildDisplayModel(rounds).columns.flat();

  // Dense 1..N over exactly the cards drawn. Neither the set check nor the value
  // check sees a gap that both sides share, and a gap means one side skipped a
  // bout the other numbered.
  const expectDense = (matchNumById) => {
    const numbers = Object.values(matchNumById).sort((a, b) => a - b);
    expect(numbers).toEqual(numbers.map((_, i) => i + 1));
  };

  describe('numbered brackets: the served number is what reaches the card', () => {
    it.each(table.cases)('$entrants entrants ($name)', ({ rounds }) => {
      const expected = engineNumbers(rounds);
      const { matchNumById } = buildDisplayModel(rounds);

      // NOT a tautology even though the model now copies matchNumber: this is
      // the two REAL-match filters agreeing. Go keeps a match when it is not
      // Hidden and not empty-vs-empty, the model keeps it when it is not hidden
      // and displayRound > 0. A match that ever satisfies one and not the other
      // either loses its label or leaves a number no card on screen carries.
      expect(Object.keys(matchNumById).sort()).toEqual(Object.keys(expected).sort());
      expect(matchNumById).toEqual(expected);
      expectDense(matchNumById);

      // Bye-slot placeholders are cards the model SYNTHESIZES so a structural
      // bye reads spatially; they are not bouts, no referee can be sent to one,
      // and neither side numbers them. Numbering them would shift every card
      // after them out of step with the sheet.
      allCards(rounds).filter((m) => m.isByeSlot).forEach((slot) => {
        expect(matchNumById[slot.id]).toBeUndefined();
      });
    });
  });

  // The bye-slot assertion above is vacuous on a table of only balanced shapes,
  // and it is the model's own invention, so nothing else would notice.
  it('the table exercises bye-slot cards at all', () => {
    const byes = table.cases.reduce((n, c) => n + allCards(c.rounds).filter((m) => m.isByeSlot).length, 0);
    expect(byes, 'no case in the golden produces a structural-bye card: the unnumbered-bye check asserts nothing').toBeGreaterThan(0);
  });

  // The one case that proves the model CONSUMES the served numbering rather
  // than coincidentally re-deriving the same values: hand it numbers the local
  // walk would never produce (the engine's own, reversed) and require the cards
  // to follow the payload. Without this, every numbered-bracket assertion above
  // would still pass against a model that ignored matchNumber entirely, because
  // the engine's numbers and the local walk's agree by construction — which is
  // exactly the duplication being removed.
  it('follows the served numbers even where they contradict the local walk', () => {
    const { rounds } = table.cases.find((c) => c.discriminating) || table.cases[0];
    const engine = engineNumbers(rounds);
    const total = Object.keys(engine).length;
    const mirror = (n) => total + 1 - n; // still dense 1..N, and never the walk's order
    const reversed = rounds.map((round) => round.map((m) => (
      m.matchNumber > 0 ? { ...m, matchNumber: mirror(m.matchNumber) } : m
    )));

    const { matchNumById } = buildDisplayModel(reversed);
    const wanted = {};
    Object.entries(engine).forEach(([id, n]) => { wanted[id] = mirror(n); });
    expect(matchNumById).toEqual(wanted);
    // Sanity: the reversal really did move something, so the assertion above
    // cannot be satisfied by the local walk. (Only a 1-match bracket is its own
    // mirror, and the golden has none.)
    expect(matchNumById).not.toEqual(engine);
  });

  describe('legacy brackets (matchNumber absent): the fallback walk mirrors Go', () => {
    // This suite is where the cross-language ORDERING contract now lives. The
    // fixtures are the engine's own brackets with the numbering stripped, which
    // is what a bracket.json written before MatchNumber existed deserialises to:
    // effective-round metadata present, numbers absent. The walk has to land on
    // the same numbers the engine would have stamped.
    it.each(table.cases)('$entrants entrants ($name)', ({ rounds }) => {
      const expected = engineNumbers(rounds);
      const stripped = withoutMatchNumbers(rounds);
      // Guard the fixture surgery: a strip that missed a field would hand the
      // served path back, and this suite would quietly stop testing the walk
      // while still passing.
      expect(
        stripped.flat().every((m) => m.matchNumber === undefined),
        'a fixture kept its matchNumber: this case would exercise the served path, not the fallback walk'
      ).toBe(true);

      const { matchNumById } = buildDisplayModel(stripped);
      expect(Object.keys(matchNumById).sort()).toEqual(Object.keys(expected).sort());
      expect(matchNumById).toEqual(expected);
      expectDense(matchNumById);
    });
  });
});
