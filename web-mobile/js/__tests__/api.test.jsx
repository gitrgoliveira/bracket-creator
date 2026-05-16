import { describe, it, expect, vi } from 'vitest';
import { toBackendMatchResult, normalizeMatch, buildPlayerMap, normalizePlayer, isHikiwake } from '../api_serializers.jsx';
import { API } from '../api_client.jsx';

describe('API Utils', () => {
  describe('toBackendMatchResult', () => {
    it('should translate UI patch to backend shape', () => {
      const match = { sideA: { name: 'Player A' }, sideB: { name: 'Player B' } };
      const patch = {
        winner: { name: 'Player A' },
        status: 'complete',
        score: { type: 'ippon', fouls: { a: 1, b: 0 } },
        ipponsA: ['M', 'K'],
        ipponsB: []
      };
      
      const result = toBackendMatchResult(patch, match);
      expect(result.winner).toBe('Player A');
      expect(result.ipponsA).toEqual(['M', 'K']);
      expect(result.hansokuA).toBe(1);
      expect(result.status).toBe('completed');
    });

    it('should handle hikiwake decision', () => {
      const match = { sideA: 'A', sideB: 'B' };
      const patch = { status: 'complete', score: { type: 'hikiwake' } };
      const result = toBackendMatchResult(patch, match);
      expect(result.decision).toBe('hikiwake');
    });

    it('treats hikewake misspelling as ippon (not a draw) on write', () => {
      // Mobile-app cleanup: hikewake legacy spelling is no longer accepted.
      // A score.type of "hikewake" passes through without draw conversion,
      // leaving decision empty (the ippon default).
      const match = { sideA: 'A', sideB: 'B' };
      const patch = { status: 'complete', score: { type: 'hikewake' } };
      const result = toBackendMatchResult(patch, match);
      expect(result.decision).toBe('');
    });
  });

  describe('isHikiwake', () => {
    it('accepts only the canonical spelling', () => {
      expect(isHikiwake('hikiwake')).toBe(true);
      expect(isHikiwake('hikewake')).toBe(false);
    });

    it('rejects other values', () => {
      expect(isHikiwake('')).toBe(false);
      expect(isHikiwake('ippon')).toBe(false);
      expect(isHikiwake(null)).toBe(false);
      expect(isHikiwake(undefined)).toBe(false);
    });
  });

  describe('normalizeMatch', () => {
    it('should normalize string sides to objects using playerMap', () => {
      const playerMap = { 'Alice': { id: 'Alice', name: 'Alice', dojo: 'Dojo A' } };
      const match = { sideA: 'Alice', sideB: 'Bob', status: 'scheduled' };
      const norm = normalizeMatch(match, playerMap);
      
      expect(norm.sideA).toEqual({ id: 'Alice', name: 'Alice', dojo: 'Dojo A' });
      expect(norm.sideB).toEqual({ id: 'Bob', name: 'Bob' }); // Fallback if not in map
    });

    it('should build score object from ippons for pool matches', () => {
      const match = { 
        sideA: 'A', sideB: 'B', winner: 'A', 
        status: 'completed', ipponsA: ['M'], ipponsB: [] 
      };
      const norm = normalizeMatch(match, {});
      expect(norm.score).toEqual({
        type: 'ippon',
        winnerPts: 1,
        loserPts: 0,
        ippons: ['M']
      });
    });
  });

  describe('buildPlayerMap', () => {
    it('should build a map from competition players', () => {
      const comp = {
        players: [
          { Name: 'Alice', Dojo: 'Dojo A', Seed: 1 },
          { name: 'Bob', dojo: 'Dojo B' }
        ]
      };
      const map = buildPlayerMap(comp);
      expect(map['Alice']).toEqual({ id: 'Alice', name: 'Alice', dojo: 'Dojo A', seed: 1 });
      expect(map['Bob']).toEqual({ id: 'Bob', name: 'Bob', dojo: 'Dojo B', seed: 0 });
    });
  });

  describe('normalizePlayer', () => {
    it('should normalize Go-style player to frontend shape', () => {
      const p = { Name: 'Alice', Dojo: 'Dojo A', Seed: 2 };
      expect(normalizePlayer(p)).toEqual({
        name: 'Alice',
        displayName: '',
        dojo: 'Dojo A',
        seed: 2,
        number: '',
        tag: ''
      });
    });
  });

  describe('API object', () => {

    it('fetchTournament should throw on error', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: async () => ({ error: 'internal error' })
      });
      await expect(API.fetchTournament()).rejects.toThrow('internal error');
    });

    it('fetchTournament should return data on success', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ name: 'London Cup' })
      });
      const data = await API.fetchTournament();
      expect(data.name).toBe('London Cup');
    });

    it('startCompetition should normalize result', async () => {
      global.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ id: 'c1', pools: [] })
      });
      const data = await API.startCompetition('c1', 'pw');
      expect(data.id).toBe('c1');
      expect(data.pools).toEqual([]);
    });

    it('recordScore should throw with default message if json fails', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 403,
          json: async () => { throw new Error('parse error'); }
        });
        await expect(API.recordScore('c1', 'm1', {}, 'pw')).rejects.toThrow('Failed to record score');
      });

    describe('moveMatchCourt', () => {
      it('PUTs to the correct endpoint with court in body', async () => {
        global.fetch = vi.fn().mockResolvedValue({ ok: true });
        const result = await API.moveMatchCourt('comp1', 'match42', 'B', 'secret');
        expect(result).toBe(true);
        expect(global.fetch).toHaveBeenCalledWith(
          '/api/competitions/comp1/matches/match42/court',
          expect.objectContaining({
            method: 'PUT',
            headers: expect.objectContaining({
              'Content-Type': 'application/json',
              'X-Tournament-Password': 'secret',
            }),
            body: JSON.stringify({ court: 'B' }),
          })
        );
      });

      it('throws with server error message on failure', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          json: async () => ({ error: 'court not found' }),
        });
        await expect(API.moveMatchCourt('c1', 'm1', 'Z', 'pw'))
          .rejects.toThrow('court not found');
      });

      it('throws default message if json parse fails', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          json: async () => { throw new Error(); },
        });
        await expect(API.moveMatchCourt('c1', 'm1', 'X', 'pw'))
          .rejects.toThrow('Failed to move match court');
      });
    });

    // Regression tests for the empty-body bug: three handlers
    // (override-rank, override-winner, reset-overrides) return `c.Status`
    // with no body. The JS client used to call `return res.json()` on
    // success, which per the Fetch spec rejects with SyntaxError on an
    // empty body — surfacing to the user as alert("Failed: Unexpected
    // end of JSON input") right after a *successful* save. The fix is
    // `return true;`, verified here by mocking a response with no .json
    // method (any call to it would throw, which the test would see).
    describe('overridePoolRank (empty-body success)', () => {
      it('resolves to true on 200 with no body', async () => {
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200 });
        await expect(API.overridePoolRank('c1', 'p1', 'Alice', 2, 'pw'))
          .resolves.toBe(true);
      });

      it('does not call res.json() on the success path', async () => {
        // If a future regression re-introduces `return res.json()`,
        // this empty-body mock would throw "Unexpected end of JSON
        // input" and the test would fail.
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: async () => { throw new SyntaxError('Unexpected end of JSON input'); },
        });
        await expect(API.overridePoolRank('c1', 'p1', 'Alice', 2, 'pw'))
          .resolves.toBe(true);
      });

      it('throws with server error message on 400', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 400,
          json: async () => ({ error: 'rank must be a positive integer ≤ 1000' }),
        });
        await expect(API.overridePoolRank('c1', 'p1', 'Alice', 2000, 'pw'))
          .rejects.toThrow('rank must be a positive integer ≤ 1000');
      });

      it('PUTs JSON body with playerName + rank', async () => {
        global.fetch = vi.fn().mockResolvedValue({ ok: true });
        await API.overridePoolRank('comp1', 'pool-A', 'Alice', 3, 'secret');
        expect(global.fetch).toHaveBeenCalledWith(
          '/api/competitions/comp1/pools/pool-A/override-rank',
          expect.objectContaining({
            method: 'PUT',
            headers: expect.objectContaining({
              'Content-Type': 'application/json',
              'X-Tournament-Password': 'secret',
            }),
            body: JSON.stringify({ playerName: 'Alice', rank: 3 }),
          }),
        );
      });
    });

    describe('overrideBracketWinner (empty-body success)', () => {
      it('resolves to true on 200 with no body', async () => {
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200 });
        await expect(API.overrideBracketWinner('c1', 'm1', 'Alice', 'pw'))
          .resolves.toBe(true);
      });

      it('does not call res.json() on the success path', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => { throw new SyntaxError('Unexpected end of JSON input'); },
        });
        await expect(API.overrideBracketWinner('c1', 'm1', 'Alice', 'pw'))
          .resolves.toBe(true);
      });

      it('throws with server error message on failure', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          json: async () => ({ error: 'match not found' }),
        });
        await expect(API.overrideBracketWinner('c1', 'm1', 'Alice', 'pw'))
          .rejects.toThrow('match not found');
      });
    });

    describe('resetOverrides (204 No Content)', () => {
      it('resolves to true on 204 with no body', async () => {
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 });
        await expect(API.resetOverrides('c1', 'pw')).resolves.toBe(true);
      });

      it('does not call res.json() on the success path', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          status: 204,
          json: async () => { throw new SyntaxError('Unexpected end of JSON input'); },
        });
        await expect(API.resetOverrides('c1', 'pw')).resolves.toBe(true);
      });

      it('DELETEs the overrides endpoint with auth header', async () => {
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 });
        await API.resetOverrides('comp1', 'secret');
        expect(global.fetch).toHaveBeenCalledWith(
          '/api/competitions/comp1/overrides',
          expect.objectContaining({
            method: 'DELETE',
            headers: expect.objectContaining({ 'X-Tournament-Password': 'secret' }),
          }),
        );
      });

      it('throws with server error message on failure', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          json: async () => ({ error: 'permission denied' }),
        });
        await expect(API.resetOverrides('c1', 'pw'))
          .rejects.toThrow('permission denied');
      });
    });
  });
});

