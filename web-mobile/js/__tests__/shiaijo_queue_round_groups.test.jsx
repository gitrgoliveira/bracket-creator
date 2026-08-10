import { describe, it, expect, beforeEach } from 'vitest';
import { groupQueueMatches } from '../admin_shiaijo.jsx';
import { bracketRoundLabel } from '../bracket.jsx';
import { compMatches } from '../viewer_utils.jsx';

// Kept in its own file rather than folded into admin_shiaijo.test.jsx: this
// suite needs the REAL bracket.jsx (for bracketRoundLabel), whose module eval
// installs window.teamIVPWScore & friends, and that would silently take over
// from the hand-rolled window stubs the score-cell suites there rely on.

// mp-u37s: a queue group's HEADING must describe every match under it.
//
// Row labels now come from the EFFECTIVE round (bracketRoundLabel / mp-7f2w
// displayRound), which collapses a round whose other match is a structural bye.
// groupQueueMatches kept keying on the RAW backend index (m.roundIndex) while
// taking the heading from the first member's m.round, so the two disagreed in
// exactly the two ways one raw↔effective mismatch allows:
//   1. one raw round holding two effective rounds → a QUARTERFINAL rendered
//      under a "Semifinals" heading;
//   2. two raw rounds sharing one effective round → TWO groups both headed
//      "Semifinals", the same round split across two blocks of the queue.
// Keying on the displayed label closes both.
describe('groupQueueMatches; the heading describes its members (mp-u37s)', () => {
  // The 5-entrant knockout the engine really persists (same fixture as
  // bracket_round_label_agreement / bracket_display_model): backend round 0
  // holds BOTH m-r1-0 (displayRound 2 → Semifinals, its winner plays the final)
  // and m-r1-3 (displayRound 3 → Quarterfinals). Backend round 1 holds the
  // OTHER semifinal. One raw round, two effective rounds — and one effective
  // round spread over two raw rounds.
  const fivePlayerRounds = () => [
    [
      { id: 'm-r1-0', sideA: 'Alice', sideB: 'Bob', displayRound: 2, feeders: ['', ''], court: 'A', status: 'scheduled' },
      { id: 'm-r1-1', sideA: '', sideB: '', hidden: true },
      { id: 'm-r1-2', sideA: 'Carol', sideB: '', hidden: true },
      { id: 'm-r1-3', sideA: 'Dave', sideB: 'Eve', displayRound: 3, feeders: ['', ''], court: 'A', status: 'scheduled' },
    ],
    [
      { id: 'm-r2-0', sideA: 'Winner of r3-m0', sideB: '', hidden: true },
      { id: 'm-r2-1', sideA: 'Carol', sideB: 'Winner of r3-m3', displayRound: 2, feeders: ['', 'm-r1-3'], court: 'A', status: 'scheduled' },
    ],
    [
      { id: 'm-r3-0', sideA: 'Winner of r2-m0', sideB: 'Winner of r2-m1', displayRound: 1, feeders: ['m-r1-0', 'm-r2-1'], court: 'A', status: 'scheduled' },
    ],
  ];

  // Build the queue rows the way the console really does: through compMatches,
  // which stamps `round` from window.bracketRoundLabel and keeps `roundIndex`
  // raw. Phantom (hidden) matches never reach the queue.
  const queueRows = () => {
    const comp = {
      id: 'c1', name: 'Knockout5', status: 'playoffs', format: 'playoffs',
      kind: 'individual', teamSize: 0, engi: false,
      bracket: { rounds: fivePlayerRounds() },
    };
    return compMatches(comp).filter((m) => m.phase === 'bracket' && !m.hidden);
  };

  beforeEach(() => {
    global.window = global.window || {};
    global.window.bracketRoundLabel = bracketRoundLabel; // the real primitive
  });

  it('never files a quarterfinal under a "Semifinals" heading', () => {
    const groups = groupQueueMatches(queueRows());
    // The invariant, stated directly: every member's own round name equals the
    // heading it is drawn under. Nothing else about the grouping matters.
    groups.forEach((g) => g.matches.forEach((m) => {
      expect(m.round).toBe(g.label);
    }));
    // …and pin the specific pairing that regressed, so the invariant above
    // can't pass vacuously on a fixture that lost its mismatch.
    const byLabel = Object.fromEntries(groups.map((g) => [g.label, g.matches.map((m) => m.id)]));
    expect(byLabel['Semifinals']).not.toContain('m-r1-3');
    expect(byLabel['Quarterfinals']).toEqual(['m-r1-3']);
  });

  it('puts one effective round in ONE group, not two identically-titled ones', () => {
    const groups = groupQueueMatches(queueRows());
    const labels = groups.map((g) => g.label);
    expect(new Set(labels).size).toBe(labels.length); // no duplicate headings
    // Both semifinals — from DIFFERENT backend rounds (0 and 1) — under one
    // "Semifinals" block.
    const sf = groups.find((g) => g.label === 'Semifinals');
    expect(sf.matches.map((m) => m.id)).toEqual(['m-r1-0', 'm-r2-1']);
    expect(sf.matches.map((m) => m.roundIndex)).toEqual([0, 1]); // roundIndex stays raw
    expect(labels).toEqual(['Semifinals', 'Quarterfinals', 'Final']);
  });

  it('still groups pool / swiss / league queues by pool, untouched by the round change', () => {
    // Pool and Swiss key on poolName, league returns null (flat): none of them
    // reads m.round or m.roundIndex, so the bracket fix must not disturb them.
    const pools = groupQueueMatches([
      { phase: 'pool', compFormat: 'mixed', poolName: 'Pool A', id: 'a1' },
      { phase: 'pool', compFormat: 'mixed', poolName: 'Pool B', id: 'b1' },
      { phase: 'pool', compFormat: 'mixed', poolName: 'Pool A', id: 'a2' },
    ]);
    expect(pools.map((g) => g.label)).toEqual(['Pool A', 'Pool B']);
    expect(pools[0].matches.map((m) => m.id)).toEqual(['a1', 'a2']);

    const swiss = groupQueueMatches([
      { phase: 'pool', compFormat: 'swiss', poolName: 'Swiss-R1', id: 's1' },
      { phase: 'pool', compFormat: 'swiss', poolName: 'Swiss-R2', id: 's2' },
    ]);
    expect(swiss.map((g) => g.label)).toEqual(['Round 1', 'Round 2']);

    expect(groupQueueMatches([
      { phase: 'pool', compFormat: 'league', poolName: 'League table', id: 'l1' },
    ])).toBeNull();
  });

  it('keeps the bronze bout in its own group beside the final', () => {
    // The bronze match is stamped round "3rd Place" (viewer_utils), a label no
    // bracket round can collide with, so label-keying still isolates it.
    const groups = groupQueueMatches([
      ...queueRows(),
      { phase: 'bracket', compFormat: 'playoffs', round: '3rd Place', roundIndex: 3, id: 'bronze' },
    ]);
    expect(groups.map((g) => g.label)).toEqual(['Semifinals', 'Quarterfinals', 'Final', '3rd Place']);
  });
});
