import { describe, it, expect } from 'vitest';
import { slotDisplayName, makeSlotLabeller, bracketSlotLabeller } from '../bracket.jsx';
import { hasBothSides, isPendingBracketMatch, bracketFullyComplete, compMatchStats } from '../admin_helpers.jsx';

// The engine writes an unresolved bracket feeder as "Winner of r<depth>-m<idx>"
// (engine.winnerOfPlaceholder). That is a WIRE value: an internal round/index
// pair that means nothing to a spectator AND does not match the match numbers
// the bracket prints on its cards ("M1", "M2"), which are ordered by effective
// round. Printing it pointed readers at the wrong card. slotDisplayName is the
// one rule for what a human reads in that slot; the wire value is untouched.

// 5-player bracket exactly as the engine persists it (same fixture as
// bracket_display_model.test.jsx):
//   QF: Dave/Eve (dr3) · SF: Alice/Bob + Carol (dr2) · Final (dr1)
//   phantoms: the dead r1-m1, Carol's bye r1-m2, the latent-bye SF r2-m0.
// Match numbers: m-r1-3 = 1, m-r1-0 = 2, m-r2-1 = 3, m-r3-0 = 4.
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

describe('slotDisplayName: the one rule for a bracket slot a human reads', () => {
  it('never returns the raw round/match id, with or without a number', () => {
    expect(slotDisplayName('Winner of r3-m3', 1)).toBe('Winner of M1');
    expect(slotDisplayName('Winner of r3-m3', 0)).toBe('TBD');
    expect(slotDisplayName('Winner of r12-m7')).toBe('TBD');
    expect(slotDisplayName('Winner of r3-m3', 1)).not.toMatch(/r\d+-m\d+/);
    expect(slotDisplayName('Winner of r3-m3')).not.toMatch(/r\d+-m\d+/);
  });

  it('leaves real competitor names untouched, including near-misses', () => {
    expect(slotDisplayName('Yuki Tanaka', 3)).toBe('Yuki Tanaka');
    // A legitimate participant whose name merely starts with the same words
    // must survive: the rule matches the EXACT wire shape, like the filters.
    expect(slotDisplayName('Winner of the 2025 Cup', 3)).toBe('Winner of the 2025 Cup');
    expect(slotDisplayName('')).toBe('');
    expect(slotDisplayName(undefined)).toBe(undefined);
  });

  it('leaves pool-origin placeholders alone: they are self-describing', () => {
    // "Pool A-1st" names a pool and a finishing position; a spectator can act on
    // it. Only the rX-mY feeder is an internal index, so only it is relabelled.
    expect(slotDisplayName('Pool A-1st', 2)).toBe('Pool A-1st');
    expect(slotDisplayName('Pool B-2nd')).toBe('Pool B-2nd');
  });
});

describe('makeSlotLabeller: resolves a slot to the number the bracket prints', () => {
  const label = () => bracketSlotLabeller(fivePlayerRounds());

  it('names the feeder by its CARD number, not the id in the slot', () => {
    // The bug: "Winner of r3-m3" was printed raw, and r3-m3 is not "M3" — it is
    // the card drawn as M1. Resolved via m-r2-1's feeders entry ['', 'm-r1-3'].
    expect(label()('Winner of r3-m3', 'm-r1-3')).toBe('Winner of M1');
    expect(label()('Winner of r2-m1', 'm-r2-1')).toBe('Winner of M3');
  });

  it('sees through a phantom bye match via the engine feeder graph', () => {
    // The final's sideA literally reads "Winner of r2-m0", but r2-m0 is a dead
    // bye match: the winner really arrives from m-r1-0 (card M2). The positional
    // parse alone can only reach the phantom, which has no number → "TBD".
    const slot = 'Winner of r2-m0';
    expect(label()(slot, 'm-r1-0')).toBe('Winner of M2'); // feeder-graph path
    expect(label()(slot)).toBe('TBD');                    // positional → phantom
  });

  it('falls back to the positional parse (Go parseWinnerOf) with no feeder id', () => {
    // roundIndex = rounds.length - depth; "r3-m3" → rounds[0][3] = m-r1-3 = M1.
    expect(label()('Winner of r3-m3')).toBe('Winner of M1');
  });

  it('degrades to TBD, never the raw id, when nothing resolves', () => {
    expect(label()('Winner of r9-m9')).toBe('TBD');               // no such match
    expect(label()('Winner of r9-m9', 'm-nope')).toBe('TBD');     // neither path resolves
    expect(makeSlotLabeller(null, null)('Winner of r1-m0')).toBe('TBD'); // no bracket
    expect(makeSlotLabeller([], {})('Winner of r1-m0')).toBe('TBD');
  });

  it('falls back to the positional parse when the feeder id is unknown', () => {
    // A stale/unknown feeder id must not swallow the resolution: the positional
    // parse still finds m-r1-3 (= M1) for "r3-m3".
    expect(label()('Winner of r3-m3', 'm-nope')).toBe('Winner of M1');
  });

  it('uses the server matchNumber when no display model is supplied', () => {
    // Callers that hold no display model, e.g. bracketSlotLabeller. There is no
    // second numbering to reconcile any more: on a numbered bracket
    // buildDisplayModel's matchNumById holds match.matchNumber verbatim, so this
    // path reads the same served field one step earlier.
    const rounds = [
      [{ id: 'a0', sideA: 'P1', sideB: 'P2', matchNumber: 7 }],
      [{ id: 'b0', sideA: 'Winner of r2-m0', sideB: 'P3' }],
    ];
    expect(makeSlotLabeller(rounds, null)('Winner of r2-m0')).toBe('Winner of M7');
  });

  it('is pure: labelling never rewrites the bracket it read', () => {
    const rounds = fivePlayerRounds();
    const before = JSON.stringify(rounds);
    bracketSlotLabeller(rounds)('Winner of r3-m3', 'm-r1-3');
    expect(JSON.stringify(rounds)).toBe(before);
  });
});

// The wire value is load-bearing: helper.reservedWinnerOfRE (Go),
// admin_helpers.BRACKET_PLACEHOLDER_RE and display_helpers.DISPLAY_PLACEHOLDER_RE
// all decide playability/schedulability from it. Relabelling is DISPLAY-ONLY, so
// every one of those classifications must be untouched.
describe('placeholder filtering is unchanged by the display label', () => {
  const feederMatch = () => ({
    id: 'm-r2-1',
    status: 'scheduled',
    sideA: { id: '', name: 'Carol' },
    sideB: { id: '', name: 'Winner of r3-m3' },
    feeders: ['', 'm-r1-3'],
  });

  it('a feeder-sided match is still not playable and still "pending"', () => {
    const m = feederMatch();
    expect(hasBothSides(m)).toBe(false);         // not actionable / not schedulable
    expect(isPendingBracketMatch(m)).toBe(true); // still a non-actionable "Later" row
    // Still not counted as a real match, and still blocks "Complete competition".
    expect(compMatchStats({ bracket: { rounds: [[m]] } })).toEqual({ total: 0, done: 0, running: 0 });
    expect(bracketFullyComplete({ rounds: [[{ ...m, status: 'completed' }, m]] })).toBe(false);
  });

  it('the label does not make a match pass the filters (no side is rewritten)', () => {
    const m = feederMatch();
    const label = bracketSlotLabeller(fivePlayerRounds());
    expect(label(m.sideB.name, m.feeders[1])).toBe('Winner of M1');
    // The match object still carries the wire value, so the predicates that read
    // it are byte-for-byte unaffected.
    expect(m.sideB.name).toBe('Winner of r3-m3');
    expect(hasBothSides(m)).toBe(false);
    expect(isPendingBracketMatch(m)).toBe(true);
  });

  it('a "Winner of M1" name would NOT be mistaken for a wire placeholder', () => {
    // Defensive: if a relabelled string ever reached a filter it would read as a
    // real competitor, which is exactly why nothing writes it back into a side.
    const relabelled = { id: 'x', status: 'scheduled', sideA: { name: 'Carol' }, sideB: { name: 'Winner of M1' } };
    expect(hasBothSides(relabelled)).toBe(true);
  });
});
