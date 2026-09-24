// bc-tmfn: a team match cannot be finished while a numbered bout has no result
// (operator ruling 2026-09-24: every bout of a team match is fought). These pin
// the editor half of the gate: which bouts it names, the both-vacant exemption,
// and the refusal copy, which must read word for word what the server says.
// Go half: internal/mobileapp/team_finish_gate_test.go.

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { unfinishedTeamBouts, unfinishedTeamBoutsMessage } from '../admin_scoring_team.jsx';

const blank = (pos) => ({ _pos: pos, aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: false, encho: 0 });

describe('unfinishedTeamBouts', () => {
  it('names every numbered bout with no result, in bout order', () => {
    const subs = [
      { ...blank(1), aPts: ['M'] },
      blank(2),
      { ...blank(3), draw: true },
      { ...blank(4), fusensho: 'a' },
      blank(5),
    ];
    expect(unfinishedTeamBouts({ subs, teamSize: 5, lineupA: null, lineupB: null })).toEqual([2, 5]);
  });

  it('counts a foul alone or an overtime as a result', () => {
    const subs = [{ ...blank(1), bFouls: 1 }, { ...blank(2), encho: 1 }];
    expect(unfinishedTeamBouts({ subs, teamSize: 2, lineupA: null, lineupB: null })).toEqual([]);
  });

  it('finds rows by position, never by index, and never names the daihyosen row', () => {
    const subs = [blank(-1), { ...blank(2), aPts: ['K'] }, { ...blank(1), aPts: ['M'] }];
    expect(unfinishedTeamBouts({ subs, teamSize: 3, lineupA: null, lineupB: null })).toEqual([3]);
  });

  it('skips a position both lineups leave vacant, and only that', () => {
    const subs = [{ ...blank(1), aPts: ['M'] }, blank(2), blank(3)];
    const lineupA = { positions: { 1: 'A1' } };
    const lineupB = { positions: { 1: 'B1', 3: 'B3' } };
    // Bout 2: vacant on both sides, no bout. Bout 3: vacant for A only, the
    // present fighter takes a fusensho, so it is still owed a result.
    expect(unfinishedTeamBouts({ subs, teamSize: 3, lineupA, lineupB })).toEqual([3]);
  });

  it('treats a picked-but-unnamed member as a fighter', () => {
    const lineup = { positions: {}, memberIds: { taisho: 'm-5' } };
    const subs = [1, 2, 3, 4].map((p) => ({ ...blank(p), draw: true })).concat([blank(5)]);
    expect(unfinishedTeamBouts({ subs, teamSize: 5, lineupA: lineup, lineupB: { positions: {} } })).toEqual([5]);
  });

  it('treats a side with no lineup as occupied at every position', () => {
    const subs = [{ ...blank(1), aPts: ['M'] }, blank(2)];
    expect(unfinishedTeamBouts({ subs, teamSize: 2, lineupA: null, lineupB: { positions: { 1: 'B1' } } })).toEqual([2]);
  });
});

// The Go half reads the same file (TestUnfinishedTeamBoutsMessage_SharedTable).
describe('unfinishedTeamBoutsMessage Go/JS mirror', () => {
  const table = JSON.parse(readFileSync(
    resolve(__dirname, '..', '..', '..', 'internal', 'mobileapp', 'testdata', 'unfinished_team_bouts.json'),
    'utf8',
  ));

  // it.each over an empty array produces zero tests, so a degraded table
  // needs its own failure.
  it('the shared table is present and non-empty', () => {
    expect(table.cases?.length).toBeGreaterThan(0);
  });

  it.each(table.cases)('teamSize $teamSize, bouts $bouts', ({ teamSize, bouts, message }) => {
    expect(unfinishedTeamBoutsMessage(teamSize, bouts), 'update BOTH implementations, not just this one').toBe(message);
  });

  it('never mentions the lineup', () => {
    for (const { teamSize, bouts } of table.cases) {
      expect(unfinishedTeamBoutsMessage(teamSize, bouts)).not.toMatch(/lineup/i);
    }
  });

  it('is empty when nothing is unfinished', () => {
    expect(unfinishedTeamBoutsMessage(5, [])).toBe('');
  });
});
