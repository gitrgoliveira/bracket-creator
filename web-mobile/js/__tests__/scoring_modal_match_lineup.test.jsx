// mp-bkg regression guard: resolveMatchLineup must prefer the per-match
// lineup endpoint (match-lineups/:matchId) over the round lineup, and fall
// back to the round lineup only when the per-match GET returns null
// (nothing saved, bc-k404).
//
// Without this test the "preferred match lineup" change in
// TeamScoreEditorModal is invisible. The component mounts, fires the
// useEffect, but since vitest stubs hooks we test the pure helper directly.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { resolveMatchLineup, resolveLineupTeamId } from '../admin_scoring_modal.jsx';
import { API } from '../api_client.jsx';

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

  it('falls back to round lineup when per-match GET returns null (nothing saved)', async () => {
    const api = makeAPI({ matchResult: null, roundResult: roundLineup });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api);
    expect(result).toEqual(roundLineup);
    expect(api.fetchMatchLineup).toHaveBeenCalledWith(COMP_ID, TEAM_ID, MATCH_ID);
    // The round step asks the server for best-effort resolution: operators
    // typically save one round-0 lineup for the whole day, so an exact-only
    // GET would answer nothing saved for every round after the first (UAT:
    // the final's kachinuki bout 1 was submitted with empty side names).
    expect(api.fetchTeamLineup).toHaveBeenCalledWith(COMP_ID, TEAM_ID, ROUND, { fallback: true });
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
    expect(api.fetchTeamLineup).toHaveBeenCalledWith('cX', 'tY', 3, { fallback: true });
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

  describe('throwOnError', () => {
    const opts = { throwOnError: true };

    it('the default still swallows a failed read on either endpoint', async () => {
      const api = makeAPI({ matchThrows: true, roundThrows: true });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api)).resolves.toBeNull();
    });

    it('rethrows a network error from the per-match read', async () => {
      const api = makeAPI({ matchThrows: true, roundResult: roundLineup });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api, opts)).rejects.toThrow('network');
      expect(api.fetchTeamLineup).not.toHaveBeenCalled();
    });

    it('rethrows a network error from the round read', async () => {
      const api = makeAPI({ matchResult: null, roundThrows: true });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api, opts)).rejects.toThrow('network');
    });

    it('rethrows a thrown 404 (competition not found)', async () => {
      const api = makeAPI({ matchResult: null });
      api.fetchMatchLineup = vi.fn().mockRejectedValue(new Error('competition not found'));
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api, opts)).rejects.toThrow('competition not found');
    });

    it('a null (nothing saved) still falls through to the round, and returns it', async () => {
      const api = makeAPI({ matchResult: null, roundResult: roundLineup });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api, opts)).resolves.toEqual(roundLineup);
    });

    it('nothing saved at either level is null, not an error', async () => {
      const api = makeAPI({ matchResult: null, roundResult: null });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, ROUND, api, opts)).resolves.toBeNull();
    });
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

// resolveMatchLineup through the REAL api_client (bc-k404): the mocked-API
// tests above prove the fall-through logic in isolation, but fetchMatchLineup
// and fetchTeamLineup are what actually turn a `saved: false` body into null
// (and now throw on a genuine 404). This exercises the real boundary against
// a fetch stub routed by URL, so a regression in either function's mapping
// shows up here even if resolveMatchLineup's own logic is untouched.
describe('resolveMatchLineup through the real api_client (bc-k404)', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  // Routes a URL to a 200 JSON body; anything unmatched answers 404 with
  // "competition not found", the real server's shape for a route this test
  // never stubs.
  function routeFetch(routes) {
    return vi.fn((url) => {
      for (const [pattern, body] of routes) {
        const hit = typeof pattern === 'string' ? url === pattern : pattern.test(url);
        if (hit) {
          return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
        }
      }
      return Promise.resolve({
        ok: false, status: 404, json: () => Promise.resolve({ error: 'competition not found' }),
      });
    });
  }

  it('(a) match route saved false, round route (fallback=best) saved true: returns the round lineup', async () => {
    const roundLineup = { teamId: 't1', round: 0, positions: { senpo: 'Alice' }, saved: true };
    global.fetch = routeFetch([
      ['/api/competitions/c1/teams/t1/match-lineups/m1', { teamId: 't1', matchId: 'm1', positions: {}, saved: false }],
      ['/api/competitions/c1/teams/t1/lineups/1?fallback=best', roundLineup],
    ]);
    const result = await resolveMatchLineup('c1', 't1', 'm1', 1, API);
    expect(result).toEqual(roundLineup);
    // The round step must ask with ?fallback=best, not a bare exact GET.
    const roundCall = global.fetch.mock.calls.map(([u]) => u).find((u) => u.includes('/lineups/1'));
    expect(roundCall).toBe('/api/competitions/c1/teams/t1/lineups/1?fallback=best');
  });

  it('(b) both routes saved false: returns null', async () => {
    global.fetch = routeFetch([
      ['/api/competitions/c1/teams/t1/match-lineups/m1', { teamId: 't1', matchId: 'm1', positions: {}, saved: false }],
      ['/api/competitions/c1/teams/t1/lineups/1?fallback=best', { teamId: 't1', round: 1, positions: {}, saved: false }],
    ]);
    const result = await resolveMatchLineup('c1', 't1', 'm1', 1, API);
    expect(result).toBeNull();
  });

  it('(c) match route saved true with EMPTY positions wins; the round URL is never fetched (mp-bkg guard)', async () => {
    const matchLineup = { teamId: 't1', matchId: 'm1', positions: {}, saved: true };
    global.fetch = routeFetch([
      ['/api/competitions/c1/teams/t1/match-lineups/m1', matchLineup],
    ]);
    const result = await resolveMatchLineup('c1', 't1', 'm1', 1, API);
    expect(result).toEqual(matchLineup);
    expect(global.fetch.mock.calls.some(([u]) => u.includes('/lineups/1'))).toBe(false);
  });

  it('(d) match route 404 (competition missing): falls through; round route saved false: returns null', async () => {
    global.fetch = routeFetch([
      ['/api/competitions/c1/teams/t1/lineups/1?fallback=best', { teamId: 't1', round: 1, positions: {}, saved: false }],
    ]);
    const result = await resolveMatchLineup('c1', 't1', 'm1', 1, API);
    expect(result).toBeNull();
  });
});
