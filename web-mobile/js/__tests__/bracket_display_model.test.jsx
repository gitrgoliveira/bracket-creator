import { describe, it, expect } from 'vitest';
import { buildDisplayModel, computeMetaTops, roundLabel, bronzeUnderFinalStyle } from '../bracket.jsx';

// bronzeUnderFinalStyle positions the 3rd-place (bronze) card UNDER the final
// match card and makes it smaller. The tree is a flex row of 230px columns
// (.bc-round min-width) with a 56px gap, so each column step is 286px; the
// final is the last column, and the 200px bronze is centred under the 230px
// final (+(230-200)/2 = 15px).
describe('bronzeUnderFinalStyle: smaller card offset under the final column', () => {
  const m = (a, b) => ({ id: `${a}-${b}`, sideA: { name: a }, sideB: { name: b } });

  it('offsets by (numCols-1) steps and centres the smaller card under the final', () => {
    // 2 rounds (SF + Final) → 2 columns → marginLeft = 1*286 + 15 = 301.
    const s = bronzeUnderFinalStyle([[m('A', 'B'), m('C', 'D')], [m('W1', 'W2')]]);
    expect(s.width).toBe(200);
    expect(s.marginLeft).toBe(301);
  });

  it('scales the offset with bracket depth (further right for more rounds)', () => {
    const rounds = [
      [m('a', 'b'), m('c', 'd'), m('e', 'f'), m('g', 'h')],
      [m('w', 'x'), m('y', 'z')],
      [m('f1', 'f2')],
    ];
    expect(bronzeUnderFinalStyle(rounds).marginLeft).toBe(2 * 286 + 15); // 587
  });

  it('never goes negative for a single-round or empty bracket', () => {
    expect(bronzeUnderFinalStyle([[m('a', 'b')]]).marginLeft).toBe(15);
    expect(bronzeUnderFinalStyle([]).marginLeft).toBe(15);
    expect(bronzeUnderFinalStyle(undefined).marginLeft).toBe(15);
  });
});

// mp-13y: roundLabel renders abbreviated "R{N}" where N is the bracket size
// (2^(fromEnd+1)) for generic early rounds. total = number of rounds (log2 size);
// roundIdx is 0-based from the first round, so fromEnd = total-1-roundIdx.
describe('roundLabel; abbreviated R{N} for early rounds', () => {
  it('names the terminal rounds and R16', () => {
    expect(roundLabel(3, 4)).toBe('Final');         // fromEnd 0
    expect(roundLabel(2, 4)).toBe('Semifinals');    // fromEnd 1
    expect(roundLabel(1, 4)).toBe('Quarterfinals'); // fromEnd 2
    expect(roundLabel(0, 4)).toBe('R16');           // fromEnd 3 → 2^4
  });
  it('computes R{N} for 64/128/256-player brackets', () => {
    expect(roundLabel(0, 6)).toBe('R64');   // fromEnd 5 → 2^6
    expect(roundLabel(0, 7)).toBe('R128');  // fromEnd 6 → 2^7
    expect(roundLabel(0, 8)).toBe('R256');  // fromEnd 7 → 2^8
  });
});

// mp-7f2w: the engine tags bracket matches with effective-round metadata
// (displayRound / hidden / feeders) so the viewer renders the same
// effective-round columns as the Excel Tree sheet; structural byes skip a
// column instead of showing empty cards. buildDisplayModel turns the persisted
// balanced rounds into those columns + a feeder graph, and falls back to the
// legacy balanced-rounds shape when no metadata is present.

// 5-player bracket as the engine persists it (verified against bracket.json):
//   QF: Dave/Eve (dr3)  ·  SF: Alice/Bob + Carol (dr2)  ·  Final (dr1)
//   phantoms: dead match, Carol's bye, latent-bye SF.
const fivePlayerRounds = () => [
  [
    { id: 'm-r1-0', sideA: 'Alice', sideB: 'Bob', displayRound: 2, feeders: ['', ''] },
    { id: 'm-r1-1', sideA: '', sideB: '', hidden: true },
    { id: 'm-r1-2', sideA: 'Carol', sideB: '', hidden: true },
    { id: 'm-r1-3', sideA: 'Dave', sideB: 'Eve', displayRound: 3, feeders: ['', ''] },
  ],
  [
    { id: 'm-r2-0', sideA: 'Winner of r3-m0', sideB: '', hidden: true },
    { id: 'm-r2-1', sideA: 'Carol', sideB: 'Winner of r3-m3', displayRound: 2, feeders: ['', 'm-r1-3'] },
  ],
  [
    { id: 'm-r3-0', sideA: 'Winner of r2-m0', sideB: 'Winner of r2-m1', displayRound: 1, feeders: ['m-r1-0', 'm-r2-1'] },
  ],
];

describe('buildDisplayModel', () => {
  it('groups real matches into effective-round columns and drops phantoms', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    expect(model.hasMeta).toBe(true);
    // Columns ordered first round → final: [QF, SF, Final].
    expect(model.columns.map((c) => c.filter((m) => !m.isByeSlot).length)).toEqual([1, 2, 1]);
    const ids = (col) => col.map((m) => m.id).sort();
    expect(ids(model.columns[0].filter((m) => !m.isByeSlot))).toEqual(['m-r1-3']); // QF: Dave/Eve
    expect(ids(model.columns[1])).toEqual(['m-r1-0', 'm-r2-1']); // SF: Alice/Bob + Carol
    expect(ids(model.columns[2])).toEqual(['m-r3-0']); // Final
    // No hidden/phantom match leaks into any column.
    const all = model.columns.flat();
    expect(all.some((m) => m.hidden)).toBe(false);
    expect(all.filter((m) => !m.isByeSlot)).toHaveLength(4); // N-1 real matches
  });

  it('exposes a feeder graph keyed by match id (byes carry no feeder)', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    expect(model.feedersById['m-r3-0']).toEqual(['m-r1-0', 'm-r2-1']); // final ← Alice/Bob, Carol SF
    expect(model.feedersById['m-r2-1']).toEqual(['bye-m-r2-1-0', 'm-r1-3']); // Carol SF ← bye slot + Dave/Eve
    expect(model.feedersById['m-r1-0']).toEqual(['bye-m-r1-0-0', 'bye-m-r1-0-1']); // Alice/Bob seeded in; both sides are byes
    expect(model.feedersById['bye-m-r1-0-0']).toEqual([]); // bye slot is a leaf
    expect(model.feedersById['m-r1-3']).toEqual([]); // Dave/Eve seeded in
  });

  it('assigns sequential match numbers left-to-right, top-to-bottom (matchNumById)', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    // Column order: QF (col 0) → SF (col 1) → Final (col 2)
    // Within each column top card first, so: m-r1-3 (QF) → m-r1-0, m-r2-1 (SF) → m-r3-0 (Final)
    expect(model.matchNumById['m-r1-3']).toBe(1); // QF: Dave/Eve
    expect(model.matchNumById['m-r1-0']).toBe(2); // SF: Alice/Bob
    expect(model.matchNumById['m-r2-1']).toBe(3); // SF: Carol
    expect(model.matchNumById['m-r3-0']).toBe(4); // Final
  });

  it('falls back to balanced rounds unchanged when no metadata', () => {
    // 4-player balanced bracket with no displayRound/hidden fields. The legacy
    // renderer draws connectors positionally inside BracketConnectors (from
    // `rounds`), so buildDisplayModel produces no feeder graph here.
    const rounds = [
      [
        { id: 'a0', sideA: 'P1', sideB: 'P2' },
        { id: 'a1', sideA: 'P3', sideB: 'P4' },
      ],
      [{ id: 'b0', sideA: 'Winner of r2-m0', sideB: 'Winner of r2-m1' }],
    ];
    const model = buildDisplayModel(rounds);
    expect(model.hasMeta).toBe(false);
    expect(model.columns).toBe(rounds); // unchanged shape
    expect(model.feedersById).toEqual({}); // legacy path is positional, no graph
  });

  it('handles empty / null input', () => {
    expect(buildDisplayModel(null).hasMeta).toBe(false);
    expect(buildDisplayModel([]).hasMeta).toBe(false);
  });
});

describe('computeMetaTops', () => {
  it('centres each parent on the mean of its feeders and stacks bye leaves', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    const heights = { 'm-r1-0': 100, 'm-r1-3': 100, 'm-r2-1': 100, 'm-r3-0': 100 };
    const tops = computeMetaTops(model.columns, model.feedersById, heights);
    // bye slots have no entry in heights; use the DEFAULT_H fallback (110).
    const centre = (id) => tops[id] + (heights[id] ?? 110) / 2;
    // Alice/Bob (leaf) is stacked first, Dave/Eve second.
    expect(centre('m-r1-0')).toBeLessThan(centre('m-r1-3'));
    // Carol's SF is centred on its two feeders: bye-m-r2-1-0 (Carol side) + Dave/Eve.
    expect(centre('m-r2-1')).toBeCloseTo((centre('bye-m-r2-1-0') + centre('m-r1-3')) / 2, 5);
    // Final is centred between Alice/Bob and Carol's SF.
    expect(centre('m-r3-0')).toBeCloseTo((centre('m-r1-0') + centre('m-r2-1')) / 2, 5);
  });

  // mp-ydk7: connectors anchor at each card's sides-block midline, which sits
  // BELOW the geometric centre for a match card (the meta-header offset) but AT
  // the centre for a bye-slot card (no .bc-side). When a parent is fed by one
  // match card + one bye-slot, centring it on feeder GEOMETRIC CENTRES leaves its
  // seam ~6px off the feeder-anchor midpoint (the elbow misses the seam). Passing
  // per-card `offsets` must centre each parent so its OWN anchor equals the mean
  // of its feeders' anchors → delta 0 for every parent, including the asymmetric
  // match+bye case. Guards the regression the centre-of-mass layout shipped on
  // unbalanced (mp-5ng7) brackets.
  it('zeroes feeder-vs-child delta under asymmetric match/bye anchor offsets', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    const MATCH_H = 114, BYE_H = 44;
    // Match cards: sides-midline 63px from top (12px header + ~half the sides).
    // Bye-slot cards: centred, so offset = height / 2.
    const MATCH_OFF = 63, BYE_OFF = BYE_H / 2;
    const isBye = (id) => id.startsWith('bye-');
    const heights = {}, offsets = {};
    for (const col of model.columns) {
      for (const m of col) {
        heights[m.id] = isBye(m.id) ? BYE_H : MATCH_H;
        offsets[m.id] = isBye(m.id) ? BYE_OFF : MATCH_OFF;
      }
    }
    const tops = computeMetaTops(model.columns, model.feedersById, heights, offsets);
    const anchor = (id) => tops[id] + offsets[id]; // the y the SVG connectors join at
    // For every parent: its anchor (seam) == mean of its feeders' anchors → delta 0.
    for (const [childId, feeders] of Object.entries(model.feedersById)) {
      const fs = feeders.filter(Boolean);
      if (!fs.length) continue;
      const mean = fs.reduce((s, fid) => s + anchor(fid), 0) / fs.length;
      expect(anchor(childId)).toBeCloseTo(mean, 5);
    }
    // Sanity: the case under test really is asymmetric; Carol's SF mixes a bye
    // (centre-anchored) with a match feeder (seam-anchored, larger offset).
    expect(offsets['bye-m-r2-1-0']).not.toBe(offsets['m-r1-3']);
  });
});
