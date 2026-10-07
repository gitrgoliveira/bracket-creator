// mp-bkg regression guard: resolveMatchLineup reads the lineup a team fields at
// a match from ONE endpoint (lineup-in-force/:matchId), and a match's own
// lineup always wins there (the whole point of the per-match API). Which lineup
// a team carries into a match is the server's rule (engine/lineup_in_force.go),
// so this client only has to hand back what the server names, source included,
// and treat nothing in force (saved: false -> null) as nothing.
//
// The component mounts and fires its useEffect, but since vitest stubs hooks we
// test the pure helper directly.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { resolveMatchLineup, resolveLineupTeamId } from '../admin_scoring_modal.jsx';
import { API } from '../api_client.jsx';

describe('resolveMatchLineup (mp-bkg regression guard)', () => {
  const COMP_ID = 'comp1';
  const TEAM_ID = 'team1';
  const MATCH_ID = 'match-xyz';

  const ownLineup = { teamId: TEAM_ID, matchId: MATCH_ID, positions: { senpo: 'Alice (own)' }, sourceMatchId: MATCH_ID, saved: true };
  const carriedLineup = { teamId: TEAM_ID, matchId: 'earlier-match', positions: { senpo: 'Alice (carried)' }, sourceMatchId: 'earlier-match', saved: true };
  const startingLineup = { teamId: TEAM_ID, round: 0, positions: { senpo: 'Alice (starting)' }, sourceRound: 0, saved: true };

  // Inject API as a plain object of mocked async functions.
  function makeAPI({ result, throws = false } = {}) {
    return {
      fetchLineupInForce: throws
        ? vi.fn().mockRejectedValue(new Error('network'))
        : vi.fn().mockResolvedValue(result ?? null),
    };
  }

  it('returns the lineup in force as the server names it, its source included', async () => {
    for (const lineup of [ownLineup, carriedLineup, startingLineup]) {
      const api = makeAPI({ result: lineup });
      expect(await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api)).toEqual(lineup);
    }
  });

  it('returns null when nothing is in force (no lineup saved)', async () => {
    const api = makeAPI({ result: null });
    expect(await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api)).toBeNull();
  });

  it('returns null when the read fails (a display degrades gracefully)', async () => {
    const api = makeAPI({ throws: true });
    expect(await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api)).toBeNull();
  });

  it('asks for the team and the match, and for no round', async () => {
    const api = makeAPI({ result: null });
    await resolveMatchLineup('cX', 'tY', 'mZ', api);
    expect(api.fetchLineupInForce).toHaveBeenCalledTimes(1);
    expect(api.fetchLineupInForce).toHaveBeenCalledWith('cX', 'tY', 'mZ');
  });

  it('REGRESSION: the match\'s own lineup is what comes back when one is saved (the whole point of mp-bkg)', async () => {
    // The server answers the match's own lineup first; the client must not
    // swap it for anything it reads elsewhere.
    const api = makeAPI({ result: ownLineup });
    const result = await resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api);
    expect(result?.positions?.senpo).toBe('Alice (own)');
    expect(result?.sourceMatchId).toBe(MATCH_ID);
  });

  describe('throwOnError', () => {
    const opts = { throwOnError: true };

    it('the default still swallows a failed read', async () => {
      const api = makeAPI({ throws: true });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api)).resolves.toBeNull();
    });

    it('rethrows a network error', async () => {
      const api = makeAPI({ throws: true });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api, opts)).rejects.toThrow('network');
    });

    it('rethrows a thrown 404 (competition not found)', async () => {
      const api = { fetchLineupInForce: vi.fn().mockRejectedValue(new Error('competition not found')) };
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api, opts)).rejects.toThrow('competition not found');
    });

    it('returns the lineup in force', async () => {
      const api = makeAPI({ result: carriedLineup });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api, opts)).resolves.toEqual(carriedLineup);
    });

    it('nothing in force is null, not an error', async () => {
      const api = makeAPI({ result: null });
      await expect(resolveMatchLineup(COMP_ID, TEAM_ID, MATCH_ID, api, opts)).resolves.toBeNull();
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
// tests above prove the hand-back logic in isolation, but fetchLineupInForce is
// what actually turns a `saved: false` body into null (and throws on a genuine
// 404). This exercises the real boundary against a fetch stub routed by URL, so
// a regression in its mapping shows up here even if resolveMatchLineup's own
// logic is untouched.
describe('resolveMatchLineup through the real api_client (bc-k404)', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  const IN_FORCE_URL = '/api/competitions/c1/teams/t1/lineup-in-force/m1';

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

  it('(a) a lineup in force comes back whole, with the source the server named', async () => {
    const carried = { teamId: 't1', matchId: 'earlier', round: 0, positions: { senpo: 'Alice' }, sourceMatchId: 'earlier', saved: true };
    global.fetch = routeFetch([[IN_FORCE_URL, carried]]);
    const result = await resolveMatchLineup('c1', 't1', 'm1', API);
    expect(result).toEqual(carried);
  });

  it('(b) saved false: returns null', async () => {
    global.fetch = routeFetch([
      [IN_FORCE_URL, { teamId: 't1', matchId: 'm1', positions: {}, saved: false }],
    ]);
    const result = await resolveMatchLineup('c1', 't1', 'm1', API);
    expect(result).toBeNull();
  });

  it('(c) one read, of the in-force route alone: the round and match routes are never fetched', async () => {
    const own = { teamId: 't1', matchId: 'm1', positions: {}, sourceMatchId: 'm1', saved: true };
    global.fetch = routeFetch([[IN_FORCE_URL, own]]);
    const result = await resolveMatchLineup('c1', 't1', 'm1', API);
    expect(result).toEqual(own);
    expect(global.fetch.mock.calls.map(([u]) => u)).toEqual([IN_FORCE_URL]);
  });

  it('(d) 404 (competition missing): null by default, thrown for the panel that must not guess', async () => {
    global.fetch = routeFetch([]);
    expect(await resolveMatchLineup('c1', 't1', 'm1', API)).toBeNull();
    await expect(resolveMatchLineup('c1', 't1', 'm1', API, { throwOnError: true }))
      .rejects.toThrow('competition not found');
  });
});
