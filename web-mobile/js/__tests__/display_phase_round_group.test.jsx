import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { gatherIndividualGroup, phaseProgressOnCourt } from '../display.jsx';
import { bracketRoundSiblings, phaseLabel } from '../display_helpers.jsx';
import { bracketRoundLabel } from '../bracket.jsx';

// Kept out of display_white_board.test.jsx deliberately: this suite imports the
// REAL bracket.jsx so phaseLabel routes through the real bracketRoundLabel, and
// bracket.jsx's module eval installs window.bracketRoundLabel. That file's
// phaseLabel suite asserts the NO-helper fallback path ("does NOT derive a
// pool-like label from a bracket id when bracketRoundLabel is unavailable"), so
// loading bracket.jsx alongside it would change what its other cases see.

// mp-u37s: the TV phase strip's heading and its match set must name the SAME
// round.
//
// phaseLabel moved to the EFFECTIVE round (mp-7f2w displayRound, which collapses
// a round whose other match is a structural bye), but phaseProgressOnCourt and
// gatherIndividualGroup kept building their set from rounds[roundIndex] — the
// RAW backend round. In any draw where the two differ the board rendered a
// heading over the wrong bouts: "SEMIFINALS · 0 / 2" counting a quarterfinal,
// and that quarterfinal listed in the rows beneath the semifinal heading.
// bracketRoundSiblings is the shared selector that closes both.

// The 5-entrant knockout the engine really persists (same fixture as
// bracket_round_label_agreement / bracket_display_model), with the court and
// status the board reads. Backend round 0 holds BOTH m-r1-0 (displayRound 2 →
// Semifinals; its winner plays the final) and m-r1-3 (displayRound 3 →
// Quarterfinals). The OTHER semifinal, m-r2-1, sits in backend round 1: one
// effective round spread over two raw rounds, one raw round holding two
// effective rounds. Everything is on court A so the court filter can never be
// what separates them — only the round rule can.
const fivePlayerRounds = () => [
  [
    { id: 'm-r1-0', sideA: 'Alice', sideB: 'Bob', displayRound: 2, court: 'A', status: 'running', scheduledAt: '10:00' },
    { id: 'm-r1-1', sideA: '', sideB: '', hidden: true },
    { id: 'm-r1-2', sideA: 'Carol', sideB: '', hidden: true },
    { id: 'm-r1-3', sideA: 'Dave', sideB: 'Eve', displayRound: 3, court: 'A', status: 'completed', scheduledAt: '09:30' },
  ],
  [
    { id: 'm-r2-0', sideA: 'Winner of r3-m0', sideB: '', hidden: true },
    { id: 'm-r2-1', sideA: 'Carol', sideB: 'Eve', displayRound: 2, court: 'A', status: 'scheduled', scheduledAt: '10:05' },
  ],
  [
    { id: 'm-r3-0', sideA: 'Winner of r2-m0', sideB: 'Winner of r2-m1', displayRound: 1, court: 'A', status: 'scheduled' },
  ],
];

const compOf = (rounds) => ({ id: 'c1', name: 'Knockout5', kind: 'individual', teamSize: 0, bracket: { rounds } });
const findMatch = (rounds, id) => rounds.flat().find((m) => m.id === id);

// promoted, exactly as findRunningOnCourt / findUpcomingOnCourt build it:
// roundIndex is the RAW index in bracket.rounds.
const promotedFor = (rounds, id) => {
  const ri = rounds.findIndex((r) => r.some((m) => m.id === id));
  return { competition: compOf(rounds), match: findMatch(rounds, id), isBracket: true, roundIndex: ri, totalRounds: rounds.length };
};

describe('bracketRoundSiblings; selects the effective round, not the raw one (mp-u37s)', () => {
  it('gathers one effective round across two backend rounds', () => {
    const rounds = fivePlayerRounds();
    const sibs = bracketRoundSiblings(rounds, findMatch(rounds, 'm-r1-0'), 0);
    // Both semifinals, from backend rounds 0 AND 1 — and NOT the quarterfinal
    // that shares backend round 0 with the promoted match.
    expect(sibs.map((m) => m.id)).toEqual(['m-r1-0', 'm-r2-1']);
  });

  it('gathers only the quarterfinal for the quarterfinal, though it shares raw round 0', () => {
    const rounds = fivePlayerRounds();
    const sibs = bracketRoundSiblings(rounds, findMatch(rounds, 'm-r1-3'), 0);
    expect(sibs.map((m) => m.id)).toEqual(['m-r1-3']);
  });

  it('never returns a hidden phantom, even one tagged with the same effective round', () => {
    const rounds = [
      [{ id: 'real', displayRound: 2 }, { id: 'phantom', displayRound: 2, hidden: true }],
    ];
    expect(bracketRoundSiblings(rounds, rounds[0][0], 0).map((m) => m.id)).toEqual(['real']);
  });

  it('falls back to the raw round, unchanged, for a legacy bracket with no metadata', () => {
    // A balanced 4-player bracket as older data (and every power-of-two draw)
    // stores it: no displayRound anywhere, so roundIndex IS the effective round
    // and the raw array must be handed back untouched — same identity, so no
    // caller can be surprised by a filtered copy.
    const rounds = [
      [{ id: 'sf-0', court: 'A' }, { id: 'sf-1', court: 'B' }],
      [{ id: 'final', court: 'A' }],
    ];
    expect(bracketRoundSiblings(rounds, rounds[0][0], 0)).toBe(rounds[0]);
    expect(bracketRoundSiblings(rounds, rounds[1][0], 1)).toBe(rounds[1]);
  });

  it('treats the engine sentinels (displayRound 0 / -1) as "no effective round"', () => {
    // 0 is the unset sentinel and -1 marks the bronze bout; neither is a round,
    // so both take the raw-round fallback (mirrors bracketRoundLabel).
    const rounds = [[{ id: 'a', displayRound: 0 }, { id: 'b', displayRound: -1 }]];
    expect(bracketRoundSiblings(rounds, rounds[0][0], 0)).toBe(rounds[0]);
    expect(bracketRoundSiblings(rounds, rounds[0][1], 0)).toBe(rounds[0]);
  });

  it('returns an empty group rather than throwing for a missing match or round', () => {
    expect(bracketRoundSiblings([], undefined, 0)).toEqual([]);
    expect(bracketRoundSiblings([[{ id: 'a' }]], { id: 'a' }, 7)).toEqual([]);
    // A hole in a round (seen in hand-edited/partial payloads) must not throw
    // while scanning for effective-round siblings.
    const holed = [[{ id: 'a', displayRound: 2 }, null], [undefined]];
    expect(bracketRoundSiblings(holed, holed[0][0], 0).map((m) => m.id)).toEqual(['a']);
  });
});

describe('phaseProgressOnCourt; the denominator matches the heading (mp-u37s)', () => {
  beforeEach(() => {
    global.window = global.window || {};
    global.window.bracketRoundLabel = bracketRoundLabel; // the real primitive
  });

  it('counts only same-effective-round matches on the court, not the raw round', () => {
    const rounds = fivePlayerRounds();
    // Promoted: the bye-collapsed semifinal in backend round 0. The raw round
    // would drag in the COMPLETED quarterfinal (m-r1-3) — reading "1 / 2" for a
    // semifinal round in which nothing has been decided yet.
    expect(phaseProgressOnCourt(promotedFor(rounds, 'm-r1-0'), 'A')).toEqual({ done: 0, total: 2 });
  });

  it('counts the quarterfinal alone, not the semifinal sharing its backend round', () => {
    const rounds = fivePlayerRounds();
    expect(phaseProgressOnCourt(promotedFor(rounds, 'm-r1-3'), 'A')).toEqual({ done: 1, total: 1 });
  });

  it('every counted match carries the heading the strip renders above it', () => {
    // The invariant behind both numbers: heading and set name one round.
    const rounds = fivePlayerRounds();
    [['m-r1-0', 'Semifinals'], ['m-r1-3', 'Quarterfinals']].forEach(([id, expected]) => {
      const p = promotedFor(rounds, id);
      const heading = phaseLabel(p.match, true, p.roundIndex, p.totalRounds, 'playoffs');
      expect(heading).toBe(expected);
      const counted = bracketRoundSiblings(rounds, p.match, p.roundIndex);
      expect(counted.length).toBeGreaterThan(0);
      counted.forEach((m) => {
        // Each counted match, labelled on its OWN raw round index, still reads
        // as the heading. (m-r2-1 lives in raw round 1, whose raw label is
        // "Semifinals" only because the effective round says so.)
        const ri = rounds.findIndex((r) => r.includes(m));
        expect(phaseLabel(m, true, ri, rounds.length, 'playoffs')).toBe(heading);
      });
    });
  });

  it('still counts the raw round for a legacy bracket with no effective-round metadata', () => {
    const rounds = [
      [{ id: 'sf-0', court: 'A', status: 'completed', sideA: { name: 'A1' }, sideB: { name: 'A2' } },
       { id: 'sf-1', court: 'A', status: 'scheduled', sideA: { name: 'A3' }, sideB: { name: 'A4' } },
       { id: 'sf-2', court: 'B', status: 'completed', sideA: { name: 'B1' }, sideB: { name: 'B2' } }],
      [{ id: 'final', court: 'A', status: 'scheduled', sideA: { name: 'Winner of r2-m0' }, sideB: { name: 'Winner of r2-m1' } }],
    ];
    expect(phaseProgressOnCourt(promotedFor(rounds, 'sf-0'), 'A')).toEqual({ done: 1, total: 2 });
  });
});

describe('gatherIndividualGroup; the rows under the heading are that round (mp-u37s)', () => {
  const saved = {};
  beforeEach(() => {
    global.window = global.window || {};
    saved.label = global.window.bracketRoundLabel;
    global.window.bracketRoundLabel = bracketRoundLabel;
  });
  afterEach(() => { global.window.bracketRoundLabel = saved.label; });

  it('does not list a quarterfinal among the semifinal rows', () => {
    const rounds = fivePlayerRounds();
    const p = promotedFor(rounds, 'm-r1-0');
    expect(phaseLabel(p.match, true, p.roundIndex, p.totalRounds, 'playoffs')).toBe('Semifinals');
    // Bracket phase shows completed + current only. The other semifinal is
    // still scheduled, so the running one is alone — and the COMPLETED
    // quarterfinal from the same backend round must not appear beside it.
    const rows = gatherIndividualGroup(p, 'A');
    expect(rows.map((m) => m.id)).toEqual(['m-r1-0']);
  });

  it('lists the completed semifinal under the semifinal heading once it is played', () => {
    const rounds = fivePlayerRounds();
    // m-r2-1 (the other semifinal, backend round 1) finishes; m-r1-0 is running.
    findMatch(rounds, 'm-r2-1').status = 'completed';
    const rows = gatherIndividualGroup(promotedFor(rounds, 'm-r1-0'), 'A');
    expect(rows.map((m) => m.id)).toEqual(['m-r2-1', 'm-r1-0']); // completed first, current last
  });
});
