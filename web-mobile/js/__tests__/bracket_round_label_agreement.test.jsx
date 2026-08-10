import { describe, it, expect, beforeEach } from 'vitest';
import { buildDisplayModel, roundLabel, bracketRoundLabel } from '../bracket.jsx';
import { compMatches } from '../viewer_utils.jsx';
import { phaseLabel } from '../display_helpers.jsx';

// mp-u37s: a bracket round label must read the SAME on every surface.
//
// Two independent renderings of one match existed: the Bracket tab groups
// matches into EFFECTIVE-round columns (mp-7f2w `displayRound`, which collapses
// a round that is entirely structural byes), while every row surface (viewer
// Overview, admin score editor, watchlist, schedule, TV boards) named the round
// from the match's RAW index in bracket.rounds. In any non-power-of-two draw the
// two disagree: a match can sit in backend round 0 yet feed the final directly.
//
// bracketRoundLabel is the single primitive both sides now go through.

// The 5-player individual knockout as the engine persists it, trimmed to the fields under test (winner/status/court/scheduledAt/matchNumber dropped). Captured
// from a live run: TOURNAMENT_DATA_DIR=… mobile-app, 5 participants, start,
// then competitions/<id>/bracket.json. 3 backend rounds; Alice/Bob sit in
// backend round 0 but carry displayRound 2 (their winner meets the final).
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

// The column labels BracketTreeMeta is EXPECTED to render: one per column,
// taken from a match in that column through the shared primitive. This mirrors
// the component's call rather than reading its output, so it pins the ROW side
// of the agreement only; that the DOM header really says this is pinned by
// render/bracket_round_label.render.test.jsx, which mounts BracketTree and
// reads .bc-round-label.
const columnLabels = (rounds) => {
  const model = buildDisplayModel(rounds);
  return model.columns.map((col, ci) => bracketRoundLabel(col[0], ci, model.columns.length));
};

// matchId → the label of the Bracket tab column the card is drawn in.
const columnLabelByMatchId = (rounds) => {
  const model = buildDisplayModel(rounds);
  const out = {};
  model.columns.forEach((col, ci) => {
    const label = bracketRoundLabel(col[0], ci, model.columns.length);
    col.forEach((m) => { if (!m.isByeSlot) out[m.id] = label; });
  });
  return out;
};

describe('bracketRoundLabel: one round name per match, every surface (mp-u37s)', () => {
  it('names the effective round, not the raw backend round', () => {
    // Alice/Bob: backend round 0 of 3 → the raw rule says "Quarterfinals".
    expect(roundLabel(0, 3)).toBe('Quarterfinals');
    // …but its winner plays the final, so it IS a semifinal.
    expect(bracketRoundLabel({ displayRound: 2 }, 0, 3)).toBe('Semifinals');
  });

  it('falls back to the raw round index for legacy brackets with no metadata', () => {
    expect(bracketRoundLabel({ id: 'x' }, 0, 3)).toBe(roundLabel(0, 3));
    expect(bracketRoundLabel(undefined, 1, 3)).toBe(roundLabel(1, 3));
    // displayRound 0 is the engine's "unset" sentinel, not a real round.
    expect(bracketRoundLabel({ displayRound: 0 }, 2, 3)).toBe('Final');
    // -1 is the bronze sentinel; it must not be read as a round either.
    expect(bracketRoundLabel({ displayRound: -1 }, 2, 3)).toBe('Final');
  });

  it('labels the collapsed 5-player bracket columns QF / SF / Final', () => {
    expect(columnLabels(fivePlayerRounds())).toEqual(['Quarterfinals', 'Semifinals', 'Final']);
  });
});

describe('row labels agree with bracket columns across a collapsed bye round (mp-u37s)', () => {
  beforeEach(() => {
    global.window = global.window || {};
    // The REAL primitives, exactly as bracket.jsx publishes them at runtime.
    global.window.roundLabel = roundLabel;
    global.window.bracketRoundLabel = bracketRoundLabel;
  });

  const comp = () => ({
    id: 'c1', name: 'Knockout5', status: 'playoffs', format: 'playoffs',
    kind: 'individual', teamSize: 0, engi: false,
    bracket: { rounds: fivePlayerRounds() },
  });

  it('gives every bracket match the same round on the row surface as in its column', () => {
    const byColumn = columnLabelByMatchId(fivePlayerRounds());
    const rows = compMatches(comp()).filter((m) => m.phase === 'bracket');
    // Only the real (non-phantom) matches carry a column, so pin that we
    // actually compared all four rather than vacuously passing on zero.
    const compared = rows.filter((m) => byColumn[m.id]);
    expect(compared).toHaveLength(4);
    compared.forEach((m) => {
      expect(m.round).toBe(byColumn[m.id]);
      expect(m.phaseName).toBe(byColumn[m.id]);
    });
  });

  it('calls the bye-collapsed match a semifinal on the row surface too', () => {
    const rows = compMatches(comp()).filter((m) => m.phase === 'bracket');
    const byId = Object.fromEntries(rows.map((m) => [m.id, m]));
    // THE regression: this row read "Quarterfinals" while the column above the
    // very same card read "Semifinals".
    expect(byId['m-r1-0'].round).toBe('Semifinals');
    expect(byId['m-r1-3'].round).toBe('Quarterfinals'); // genuinely a QF; unchanged
    expect(byId['m-r2-1'].round).toBe('Semifinals');
    expect(byId['m-r3-0'].round).toBe('Final');
  });

  it('agrees on the TV/lobby/scoreboard boards too (phaseLabel)', () => {
    // Those surfaces flatten c.bracket.rounds themselves and pass the raw
    // (roundIndex, totalRounds) with no phaseName stamped, so phaseLabel's
    // bracket branch is the label they show.
    const rounds = fivePlayerRounds();
    const byColumn = columnLabelByMatchId(rounds);
    rounds.forEach((round, ri) => round.forEach((m) => {
      if (!byColumn[m.id]) return; // phantom: never promoted to a board
      expect(phaseLabel(m, true, ri, rounds.length, 'playoffs')).toBe(byColumn[m.id]);
    }));
    expect(phaseLabel(rounds[0][0], true, 0, 3, 'playoffs')).toBe('Semifinals');
  });

  it('keeps roundIndex RAW so lineup fetches still key on the backend round', () => {
    const rows = compMatches(comp()).filter((m) => m.phase === 'bracket');
    const byId = Object.fromEntries(rows.map((m) => [m.id, m]));
    // The label moved to the effective round; the index must NOT follow it.
    expect(byId['m-r1-0'].roundIndex).toBe(0);
    expect(byId['m-r2-1'].roundIndex).toBe(1);
    expect(byId['m-r3-0'].roundIndex).toBe(2);
  });
});
