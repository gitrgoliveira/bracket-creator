import { describe, it, expect } from 'vitest';
import { buildDisplayModel, computeMetaTops } from '../bracket.jsx';

// mp-7f2w: the engine tags bracket matches with effective-round metadata
// (displayRound / hidden / feeders) so the viewer renders the same
// effective-round columns as the Excel Tree sheet — structural byes skip a
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
    expect(ids(model.columns[0])).toEqual(['m-r1-3']); // QF: Dave/Eve
    expect(ids(model.columns[1])).toEqual(['m-r1-0', 'm-r2-1']); // SF: Alice/Bob + Carol
    expect(ids(model.columns[2])).toEqual(['m-r3-0']); // Final
    // No hidden/phantom match leaks into any column.
    const all = model.columns.flat();
    expect(all.some((m) => m.hidden)).toBe(false);
    expect(all).toHaveLength(4); // N-1 real matches
  });

  it('exposes a feeder graph keyed by match id (byes carry no feeder)', () => {
    const model = buildDisplayModel(fivePlayerRounds());
    expect(model.feedersById['m-r3-0']).toEqual(['m-r1-0', 'm-r2-1']); // final ← Alice/Bob, Carol SF
    expect(model.feedersById['m-r2-1']).toEqual(['m-r1-3']); // Carol SF ← Dave/Eve (Carol side is a bye)
    expect(model.feedersById['m-r1-0']).toEqual([]); // Alice/Bob seeded in
    expect(model.feedersById['m-r1-3']).toEqual([]); // Dave/Eve seeded in
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
    const centre = (id) => tops[id] + heights[id] / 2;
    // Alice/Bob (leaf) is stacked first, Dave/Eve second.
    expect(centre('m-r1-0')).toBeLessThan(centre('m-r1-3'));
    // Carol's SF sits on its single feeder (Dave/Eve).
    expect(centre('m-r2-1')).toBeCloseTo(centre('m-r1-3'), 5);
    // Final is centred between Alice/Bob and Carol's SF.
    expect(centre('m-r3-0')).toBeCloseTo((centre('m-r1-0') + centre('m-r2-1')) / 2, 5);
  });
});
