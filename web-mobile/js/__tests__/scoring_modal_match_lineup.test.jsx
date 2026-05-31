// mp-bkg regression guard: resolveMatchLineup must prefer the per-match
// lineup endpoint (match-lineups/:matchId) over the round lineup, and fall
// back to the round lineup only when the per-match GET returns null (404).
//
// Without this test the "preferred match lineup" change in
// TeamScoreEditorModal is invisible — the component mounts, fires the
// useEffect, but since vitest stubs hooks we test the pure helper directly.

import { describe, it, expect, vi } from 'vitest';
import { resolveMatchLineup, resolveLineupTeamId } from '../admin_scoring_modal.jsx';

describe('resolveMatchLineup (mp-bkg regression guard)', () => {
  const COMP_ID = 'comp1';
  const TEAM_ID = 'team1';
  const MATCH_ID = 'match-xyz';
  const ROUND = 1;

  const matchLineup = { teamId: TEAM_ID, matchId: MATCH_ID, positions: { senpo: 'Alice (match)' } };
  const roundLineup = { teamId: TEAM_ID, round: ROUND, positions: { senpo: 'Alice (round)' } };

  // Inject API as a plain object of mocked async functions.
  function makeAPI({ matchResult, roundResult, matchThrows = false, roundThrows = false } = {}) {
    return {
      fetchMatchLineup: matchThrows
        ? vi.fn().mockRejectedValue(new Error('network'))
        : vi.fn().mockResolvedValue(matchResult ?? null),
      fetchTeamLineup: roundThrows
        ? vi.fn().mockRejectedValue(new Error('network'))
        : vi.fn().mockResolvedValue(roundResult ?? null),
    };
  }

  it('returns the per-match lineup when it exists (non-null)', async () => {
    const api = makeAPI({ matchResult: matchLineup, roundResult: roundLineup });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api);
    expect(result).toEqual(matchLineup);
    // fetchTeamLineup should NOT be called when per-match entry exists
    expect(api.fetchTeamLineup).not.toHaveBeenCalled();
  });

  it('falls back to round lineup when per-match GET returns null (404)', async () => {
    const api = makeAPI({ matchResult: null, roundResult: roundLineup });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api);
    expect(result).toEqual(roundLineup);
    expect(api.fetchMatchLineup).toHaveBeenCalledWith(COMP_ID, TEAM_ID, MATCH_ID);
    expect(api.fetchTeamLineup).toHaveBeenCalledWith(COMP_ID, TEAM_ID, ROUND);
  });

  it('falls back to round lineup when per-match GET throws (network error)', async () => {
    const api = makeAPI({ matchThrows: true, roundResult: roundLineup });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api);
    expect(result).toEqual(roundLineup);
  });

  it('returns null when both per-match and round lineups are null (no lineup submitted)', async () => {
    const api = makeAPI({ matchResult: null, roundResult: null });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api);
    expect(result).toBeNull();
  });

  it('returns null when per-match is null and round throws', async () => {
    const api = makeAPI({ matchResult: null, roundThrows: true });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api);
    expect(result).toBeNull();
  });

  it('returns null when both throw (full network failure)', async () => {
    const api = makeAPI({ matchThrows: true, roundThrows: true });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api);
    expect(result).toBeNull();
  });

  it('passes the correct arguments to each API call', async () => {
    const api = makeAPI({ matchResult: null, roundResult: null });
    await resolveMatchLineup('cX', 'tY', 'mZ', 3, api);
    expect(api.fetchMatchLineup).toHaveBeenCalledWith('cX', 'tY', 'mZ');
    expect(api.fetchTeamLineup).toHaveBeenCalledWith('cX', 'tY', 3);
  });

  it('REGRESSION: per-match entry is preferred over round (the whole point of mp-bkg)', async () => {
    // This is the load-bearing test: if fetchMatchLineup returns a non-null
    // lineup it must win over the round lineup. Previously, the modal always
    // used fetchTeamLineup (round-only), so per-match edits were cosmetic.
    const matchSpecific = { positions: { senpo: 'Match-specific player' } };
    const roundDefault = { positions: { senpo: 'Round-default player' } };
    const api = makeAPI({ matchResult: matchSpecific, roundResult: roundDefault });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api);
    expect(result?.positions?.senpo).toBe('Match-specific player');
    expect(result).not.toEqual(roundDefault);
  });
});

describe('resolveLineupTeamId (mp-bkg: name-keyed side → participant UUID)', () => {
  // comp.players carry the real id (UUID) + name; a match side is keyed by
  // NAME (api_serializers.buildPlayerMap sets id = name). Lineups are stored
  // under the UUID, so the scoring modal must map name → UUID before fetching.
  const PLAYERS = [
    { id: 'uuid-red-111', name: 'Red Dojo', metadata: ['Aka Ichi'] },
    { id: 'uuid-blue-222', name: 'Blue Dojo', metadata: ['Shiro Ichi'] },
  ];

  it('REGRESSION: maps a name-keyed side to the participant UUID', () => {
    // This is the load-bearing mapping: the match side id is "Red Dojo"
    // (the name), but the lineup is stored under "uuid-red-111".
    expect(resolveLineupTeamId('Red Dojo', PLAYERS)).toBe('uuid-red-111');
  });

  it('matches when the side key is already the UUID', () => {
    expect(resolveLineupTeamId('uuid-blue-222', PLAYERS)).toBe('uuid-blue-222');
  });

  it('tolerates PascalCase participant fields', () => {
    const pascal = [{ ID: 'uuid-x', Name: 'Green Dojo' }];
    expect(resolveLineupTeamId('Green Dojo', pascal)).toBe('uuid-x');
  });

  it('falls back to the side key when no participant matches', () => {
    expect(resolveLineupTeamId('Ghost Dojo', PLAYERS)).toBe('Ghost Dojo');
  });

  it('returns "" for an empty side key and tolerates a missing player list', () => {
    expect(resolveLineupTeamId('', PLAYERS)).toBe('');
    expect(resolveLineupTeamId('Red Dojo', undefined)).toBe('Red Dojo');
  });
});
