import { describe, it, expect } from 'vitest';
import { buildDisplayModel, computeMetaTops, roundLabel, bronzeUnderFinalStyle } from '../bracket.jsx';

// bronzeUnderFinalStyle positions the 3rd-place (bronze) card UNDER the final
// match card and makes it smaller. The tree is a flex row of 230px columns
// (.bc-round min-width) with a 56px gap, so each column step is 286px; the
// final is the last column, and the 210px bronze is centred under the 230px
// final (+(230-210)/2 = 10px).
describe('bronzeUnderFinalStyle: smaller card offset under the final column', () => {
  const m = (a, b) => ({ id: `${a}-${b}`, sideA: { name: a }, sideB: { name: b } });

  // A 210px card centred under the 230px final → +(230-210)/2 = 10px.
  it('offsets by (numCols-1) steps and centres the smaller card under the final', () => {
    // 2 rounds (SF + Final) → 2 columns → marginLeft = 1*286 + 10 = 296.
    const s = bronzeUnderFinalStyle([[m('A', 'B'), m('C', 'D')], [m('W1', 'W2')]]);
    expect(s.width).toBe(210);
    expect(s.marginLeft).toBe(296);
  });

  it('scales the offset with bracket depth (further right for more rounds)', () => {
    const rounds = [
      [m('a', 'b'), m('c', 'd'), m('e', 'f'), m('g', 'h')],
      [m('w', 'x'), m('y', 'z')],
      [m('f1', 'f2')],
    ];
    expect(bronzeUnderFinalStyle(rounds).marginLeft).toBe(2 * 286 + 10); // 582
  });

  it('never goes negative for a single-round or empty bracket', () => {
    expect(bronzeUnderFinalStyle([[m('a', 'b')]]).marginLeft).toBe(10);
    expect(bronzeUnderFinalStyle([]).marginLeft).toBe(10);
    expect(bronzeUnderFinalStyle(undefined).marginLeft).toBe(10);
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
    expect(model.columns.map((c) => c.length)).toEqual([1, 2, 1]);
    const ids = (col) => col.map((m) => m.id).sort();
    // The first column holds ONLY the one first-round bout. Alice, Bob and
    // Carol skip it and get no placeholder card there (bc-tmfn): they first
    // appear in their semifinal cards.
    expect(ids(model.columns[0])).toEqual(['m-r1-3']); // QF: Dave/Eve
    expect(ids(model.columns[1])).toEqual(['m-r1-0', 'm-r2-1']); // SF: Alice/Bob + Carol
    expect(ids(model.columns[2])).toEqual(['m-r3-0']); // Final
    // No hidden/phantom match leaks into any column.
    const all = model.columns.flat();
    expect(all.some((m) => m.hidden)).toBe(false);
    expect(all).toHaveLength(4); // N-1 real matches, and nothing else
  });

  it('exposes a feeder graph of real matches only (a skipped side has no feeder)', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    expect(model.feedersById['m-r3-0']).toEqual(['m-r1-0', 'm-r2-1']); // final ← Alice/Bob, Carol SF
    expect(model.feedersById['m-r2-1']).toEqual(['m-r1-3']); // Carol SF ← Dave/Eve only; Carol comes straight in
    expect(model.feedersById['m-r1-0']).toEqual([]); // Alice/Bob both come straight into the SF
    expect(model.feedersById['m-r1-3']).toEqual([]); // Dave/Eve: the first-round bout
    // Keyed by exactly the drawn matches: nothing synthesized.
    expect(Object.keys(model.feedersById).sort()).toEqual(['m-r1-0', 'm-r1-3', 'm-r2-1', 'm-r3-0']);
    // The raw [sideA, sideB] pair survives on the column entry, so the
    // connector can tell WHICH side a lone feeder fills (connectorTargetY).
    const carolSF = model.columns.flat().find((m) => m.id === 'm-r2-1');
    expect(carolSF.feeders).toEqual(['', 'm-r1-3']);
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

  // bc-draw Phase 5. One effective round can hold matches from SEVERAL backend
  // rounds at once: a shallow region's first bout shares a displayRound with a
  // deep region's second bout. The printed Excel sheet numbers each effective
  // round left to right across the whole tree, so the tie-break has to be the
  // match's leftmost first-round slot, pos<<(backendRound+1) - not the position
  // alone, which spans two slots in one round and four in the next. The fixture
  // below is the smallest bracket where the two disagree: m-r1-4 (slot 8) is left
  // of m-r2-3 (slot 12) but its position, 4, is to the right of 3.
  const interleavedRounds = () => {
    const real = (id, sideA, sideB, displayRound, feeders = ['', '']) => ({ id, sideA, sideB, displayRound, feeders });
    const dead = (id, sideA = '', sideB = '') => ({ id, sideA, sideB, hidden: true });
    return [
      [
        real('m-r1-0', 'A1', 'A2', 4), real('m-r1-1', 'A3', 'A4', 4),
        real('m-r1-2', 'A5', 'A6', 4), real('m-r1-3', 'A7', 'A8', 4),
        real('m-r1-4', 'B1', 'B2', 3), dead('m-r1-5'),
        real('m-r1-6', 'C1', 'C2', 4), real('m-r1-7', 'C3', 'C4', 4),
      ],
      [
        real('m-r2-0', 'Winner of r4-m0', 'Winner of r4-m1', 3, ['m-r1-0', 'm-r1-1']),
        real('m-r2-1', 'Winner of r4-m2', 'Winner of r4-m3', 3, ['m-r1-2', 'm-r1-3']),
        dead('m-r2-2', 'Winner of r4-m4', ''),
        real('m-r2-3', 'Winner of r4-m6', 'Winner of r4-m7', 3, ['m-r1-6', 'm-r1-7']),
      ],
      [
        real('m-r3-0', 'Winner of r3-m0', 'Winner of r3-m1', 2, ['m-r2-0', 'm-r2-1']),
        real('m-r3-1', 'Winner of r3-m2', 'Winner of r3-m3', 2, ['m-r1-4', 'm-r2-3']),
      ],
      [real('m-r4-0', 'Winner of r2-m0', 'Winner of r2-m1', 1, ['m-r3-0', 'm-r3-1'])],
    ];
  };

  it('numbers an effective round left-to-right across backend rounds (matchNumById)', () => {
    const nums = buildDisplayModel(interleavedRounds()).matchNumById;
    // Deepest effective round first, in slot order.
    expect([nums['m-r1-0'], nums['m-r1-1'], nums['m-r1-2'], nums['m-r1-3'], nums['m-r1-6'], nums['m-r1-7']])
      .toEqual([1, 2, 3, 4, 5, 6]);
    // The interleaved round: m-r1-4 sits between m-r2-1 and m-r2-3 on the sheet.
    expect(nums['m-r2-0']).toBe(7);
    expect(nums['m-r2-1']).toBe(8);
    expect(nums['m-r1-4']).toBe(9);
    expect(nums['m-r2-3']).toBe(10);
    expect([nums['m-r3-0'], nums['m-r3-1'], nums['m-r4-0']]).toEqual([11, 12, 13]);
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
  // bc-tmfn: with no placeholder cards, a parent can have 0, 1 or 2 feeders.
  // 100px cards with the seam at 50: the Aka row's centre sits at 25, the
  // Shiro row's at 75, so a one-fed card shifts 25px against its feeder, more
  // than the 16px gap between stacked cards.
  const heights = { 'm-r1-0': 100, 'm-r1-3': 100, 'm-r2-1': 100, 'm-r3-0': 100 };
  const offsets = { 'm-r1-0': 50, 'm-r1-3': 50, 'm-r2-1': 50, 'm-r3-0': 50 };
  const GAP = 16;

  it('puts a one-fed card\'s Shiro row level with its feeder, and keeps the card clear of the one above', () => {
    const model = buildDisplayModel(fivePlayerRounds()); // Carol's SF is fed on sideB
    const tops = computeMetaTops(model.columns, model.feedersById, heights, offsets, { 'm-r2-1': 75 });
    const anchor = (id) => tops[id] + offsets[id];
    expect(Object.keys(tops).sort()).toEqual(['m-r1-0', 'm-r1-3', 'm-r2-1', 'm-r3-0']);
    // Alice/Bob's SF, fed by nothing, is stacked first.
    expect(tops['m-r1-0']).toBe(0);
    // Carol's SF: its Shiro row centre is level with Dave/Eve's anchor, so the
    // line between them is straight.
    expect(tops['m-r2-1'] + 75).toBe(anchor('m-r1-3'));
    // It rose 25px against Dave/Eve, and still keeps the full gap below
    // Alice/Bob in their shared column: Dave/Eve was stacked lower instead.
    expect(tops['m-r2-1']).toBe(tops['m-r1-0'] + heights['m-r1-0'] + GAP);
    expect(tops['m-r1-3']).toBe(tops['m-r2-1'] + 25);
    // The final is centred on the two semifinal anchors.
    expect(anchor('m-r3-0')).toBeCloseTo((anchor('m-r1-0') + anchor('m-r2-1')) / 2, 5);
  });

  it('puts a one-fed card\'s Aka row level with its feeder, and keeps what is stacked after it clear', () => {
    // Carol's SF fed on sideA, and first under the final, so Alice/Bob's SF is
    // stacked after it in the same column.
    const rounds = fivePlayerRounds();
    rounds[1][1] = { ...rounds[1][1], sideA: 'Winner of r3-m3', sideB: 'Carol', feeders: ['m-r1-3', ''] };
    rounds[2][0] = { ...rounds[2][0], feeders: ['m-r2-1', 'm-r1-0'] };
    const model = buildDisplayModel(rounds);
    const tops = computeMetaTops(model.columns, model.feedersById, heights, offsets, { 'm-r2-1': 25 });
    const anchor = (id) => tops[id] + offsets[id];
    expect(tops['m-r1-3']).toBe(0);
    // The Aka row centre is level with Dave/Eve's anchor: the card dropped 25px.
    expect(tops['m-r2-1'] + 25).toBe(anchor('m-r1-3'));
    expect(tops['m-r2-1']).toBe(25);
    // Alice/Bob's SF, stacked next in the same column, keeps the full gap.
    expect(tops['m-r1-0']).toBe(tops['m-r2-1'] + heights['m-r2-1'] + GAP);
  });

  it('levels a one-fed card at its seam when no fed-row centre is given', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    const tops = computeMetaTops(model.columns, model.feedersById, heights, offsets);
    expect(tops['m-r2-1'] + offsets['m-r2-1']).toBe(tops['m-r1-3'] + offsets['m-r1-3']);
  });

  // mp-ydk7: connectors anchor at each card's sides-block midline, which sits
  // BELOW the geometric centre by the meta header, and cards differ in height
  // (a filled name+dojo card is taller than a TBD one). Centring a parent on
  // its feeders' GEOMETRIC centres would leave its seam off the feeders'
  // seams. Passing per-card `offsets` must place each two-fed parent so its
  // OWN anchor equals the mean of its feeders' anchors, and a one-fed parent
  // so its fed row equals its feeder's anchor.
  it('zeroes feeder-vs-child delta under asymmetric card heights and anchor offsets', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    const h = { 'm-r1-0': 118, 'm-r1-3': 118, 'm-r2-1': 104, 'm-r3-0': 104 };
    const off = { 'm-r1-0': 66, 'm-r1-3': 66, 'm-r2-1': 59, 'm-r3-0': 59 };
    const tops = computeMetaTops(model.columns, model.feedersById, h, off, { 'm-r2-1': 81 });
    const anchor = (id) => tops[id] + off[id]; // the y the SVG connectors join at
    // The final, fed by two: its anchor is the mean of theirs.
    expect(anchor('m-r3-0')).toBeCloseTo((anchor('m-r1-0') + anchor('m-r2-1')) / 2, 5);
    // Carol's SF, fed by one on its Shiro row: that row is level with it.
    expect(tops['m-r2-1'] + 81).toBeCloseTo(anchor('m-r1-3'), 5);
    // Sanity: the case under test really is asymmetric; the final's two
    // feeders carry different offsets.
    expect(off['m-r1-0']).not.toBe(off['m-r2-1']);
  });
});

