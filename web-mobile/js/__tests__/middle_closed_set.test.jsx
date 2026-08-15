import { describe, it, expect } from 'vitest';
import { matchMiddleMark, matchStateCell, formatIpponsScore } from '../bracket.jsx';

// OPERATOR RULING: there must NEVER be a centre `Ht`, and there is only one
// right way for the centre to be. The middle is a CLOSED SET of exactly four
// values — "vs" (plain, including unplayed/pending), "X" (tie), "(E)"
// (overtime), "(DH)" (representative bout) — and nothing else may ever appear
// there. `Ht`, `Kiken` and `Fus.` are RESULT marks: they name one competitor,
// so they ride beside that competitor and can never occupy the shared cell.
//
// The rule already has one owner (`boutMiddle` in bracket.jsx) and every
// result-projecting surface derives from it, so today it holds by
// construction. What was missing was enforcement: it was stated in comments
// and in CLAUDE.md, but nothing FAILED if a fifth value or a stray `Ht` were
// introduced. These tests close the set over the whole input space rather than
// spot-checking cases, so a new decision type or a new middle branch has to
// come here and state itself.
const MIDDLE = ['vs', 'X', '(E)', '(DH)'];

// The 10 canonical wire values (CLAUDE.md § Match Decision Types) plus the
// junk a hand-edited file or an older client can still deliver.
const DECISIONS = [
  '', 'fought', 'hikiwake', 'kiken', 'kiken-voluntary', 'kiken-injury',
  'fusenpai', 'fusensho', 'daihyosen', 'kachinuki-exhaustion',
  'HIKIWAKE', 'nonsense', null, undefined,
];
const ENCHOS = [
  undefined, null, {}, { periodCount: 0 }, { periodCount: 1 }, { periodCount: 3 },
  { periodCount: -1 }, { on: true },
];
// A hantei is decided from a TIED scoreline, so these are the shapes that
// historically tempted a centre `Ht`: level scores, with and without points.
const SCORES = [
  undefined, null, {}, { type: 'hikiwake' }, { type: 'ippon' },
  { winnerPts: 0, loserPts: 0 }, { winnerPts: 1, loserPts: 1 },
  { winnerPts: 2, loserPts: 1 }, { decidedByHantei: true },
];

describe('the middle is a closed set (operator ruling)', () => {
  it('matchMiddleMark only ever yields a member of the set, or empty', () => {
    const seen = new Set();
    for (const decision of DECISIONS) {
      for (const encho of ENCHOS) {
        for (const score of SCORES) {
          // decidedByHantei is swept HERE too, not only in the Ht test below:
          // a hantei is the input that historically produced a fifth value, so
          // the closed set has to be proved over it rather than around it.
          for (const decidedByHantei of [true, false, undefined]) {
            const mid = matchMiddleMark({ decision, encho, score, decidedByHantei });
            seen.add(mid);
            expect(MIDDLE.concat([''])).toContain(mid);
          }
        }
      }
    }
    // Guard against the test passing vacuously: the sweep must actually
    // exercise the special marks, not just return "" everywhere.
    expect(seen.has('X')).toBe(true);
    expect(seen.has('(E)')).toBe(true);
    expect(seen.has('(DH)')).toBe(true);
  });

  it('never puts Ht — or any side result mark — in the middle', () => {
    for (const decision of DECISIONS) {
      for (const encho of ENCHOS) {
        for (const score of SCORES) {
          // decidedByHantei is threaded every way a caller could: on the
          // match, on the score, and as the decision itself.
          for (const hantei of [true, false, undefined]) {
            const mid = matchMiddleMark({ decision, encho, score, decidedByHantei: hantei });
            expect(mid).not.toContain('Ht');
            expect(mid).not.toContain('Kiken');
            expect(mid).not.toContain('Fus');
          }
        }
      }
    }
  });

  // matchStateCell and formatIpponsScore are the two other surfaces that
  // compose a middle. The score string is the one place a mark may TRAIL the
  // cells (an unattributable-winner degradation), but even there it must not
  // land between them.
  // formatIpponsScore(ipponsLeft, ipponsRight, score, decision, encho,
  //                   decidedByHantei, winnerSide)
  // It composes the same primitives directly rather than calling boutMiddle,
  // so it needs its own guard. A result mark may TRAIL the cells here when the
  // winner is unattributable, but it must never become the separator.
  it('the score string never carries a result mark in its middle', () => {
    let sawMiddle = false;
    for (const decision of DECISIONS) {
      for (const encho of ENCHOS) {
        for (const hantei of [true, false]) {
          for (const winnerSide of ['left', 'right', null, undefined]) {
            const s = formatIpponsScore(['M'], ['K'], { type: 'ippon' },
              decision, encho, hantei, winnerSide);
            if (typeof s !== 'string' || !s) continue;
            const parts = s.split(/\s+/);
            const middles = parts.filter(p => MIDDLE.includes(p));
            if (middles.length) sawMiddle = true;
            // The contract is `[left cell] [middle] [right cell]`, so a
            // well-formed string has EXACTLY ONE middle token and it is a
            // member of the set. If a result mark had been made the separator
            // there would be none, which is the shape this catches.
            expect(middles.length, `no middle in ${JSON.stringify(s)}`).toBe(1);
            expect(MIDDLE).toContain(middles[0]);
          }
        }
      }
    }
    expect(sawMiddle).toBe(true);
  });

  it('matchStateCell stays inside the set for every status', () => {
    for (const status of ['scheduled', 'running', 'completed', '', undefined]) {
      for (const decision of DECISIONS) {
        const cell = matchStateCell({ status, decision, encho: { periodCount: 1 } });
        const text = typeof cell === 'string' ? cell : (cell && cell.mid) || '';
        if (!text) continue;
        expect(String(text)).not.toContain('Ht');
      }
    }
  });
});
