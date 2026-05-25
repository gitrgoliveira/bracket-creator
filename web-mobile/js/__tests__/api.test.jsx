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
        tag: '',
        danGrade: '',
      });
    });

    it('maps Go Metadata[0] → danGrade on PascalCase input', () => {
      const p = { Name: 'Bob', Dojo: 'Kenshikan', Seed: 0, Metadata: ['3d'] };
      expect(normalizePlayer(p).danGrade).toBe('3d');
    });

    it('backfills danGrade from metadata[0] on already-camelCase input', () => {
      const p = { name: 'Carol', dojo: 'Yoshinkan', seed: 0, metadata: ['2d'] };
      expect(normalizePlayer(p).danGrade).toBe('2d');
    });

    it('leaves danGrade unchanged when already set on camelCase input', () => {
      const p = { name: 'Dave', dojo: 'Mumeishi', seed: 0, danGrade: '4d' };
      expect(normalizePlayer(p).danGrade).toBe('4d');
    });

    it('returns empty danGrade when no metadata', () => {
      const p = { Name: 'Eve', Dojo: 'Mumeishi', Seed: 0 };
      expect(normalizePlayer(p).danGrade).toBe('');
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

    // mp-a7y: pins the invalidate endpoint URL + auth header so a
    // future refactor can't silently re-route the call. The handler
    // is gated on tournament password (internal/mobileapp/handlers_competition.go);
    // missing the X-Tournament-Password header would 401 with the
    // status-flip never reaching disk.
    describe('invalidateCompetition', () => {
      it('POSTs to /api/competitions/{id}/invalidate with password header and returns the updated competition JSON', async () => {
        const updated = { id: 'comp-abc', status: 'invalid', name: 'Test Comp' };
        global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => updated });
        const result = await API.invalidateCompetition('comp-abc', 'secret');
        // Returns the parsed payload (not a bare `true`) so callers can
        // apply the updated competition to local state without waiting
        // for the SSE refresh to land.
        expect(result).toEqual(updated);
        expect(global.fetch).toHaveBeenCalledWith(
          '/api/competitions/comp-abc/invalidate',
          expect.objectContaining({
            method: 'POST',
            headers: expect.objectContaining({
              'X-Tournament-Password': 'secret',
            }),
          })
        );
      });

      it('throws with server error message on failure', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          json: async () => ({ error: 'only in-progress competitions can be invalidated' }),
        });
        await expect(API.invalidateCompetition('c1', 'pw'))
          .rejects.toThrow('only in-progress competitions can be invalidated');
      });

      it('throws default message if json parse fails', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          json: async () => { throw new Error(); },
        });
        await expect(API.invalidateCompetition('c1', 'pw'))
          .rejects.toThrow('Failed to invalidate competition');
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

    // T190–T193 (US13 — FR-050a / FR-050d / FR-050e): Swiss endpoint
    // wrappers. The generate-round call carries auth and returns the
    // new round payload; the 409 / round_incomplete contract carries
    // structured fields (code, round) that the helper must propagate
    // on the Error object so the admin UI can branch on them.
    describe('swissGenerateRound', () => {
      it('POSTs to the correct endpoint with auth header', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          status: 201,
          json: async () => ({ round: 2, matches: [], swissCurrentRound: 2 }),
        });
        const r = await API.swissGenerateRound('c1', 'secret');
        expect(r.round).toBe(2);
        expect(r.swissCurrentRound).toBe(2);
        expect(global.fetch).toHaveBeenCalledWith(
          '/api/competitions/c1/swiss/generate-round',
          expect.objectContaining({
            method: 'POST',
            headers: expect.objectContaining({ 'X-Tournament-Password': 'secret' }),
          })
        );
      });

      it('propagates 409 round_incomplete with code on the thrown Error', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 409,
          json: async () => ({ error: 'round 2 not yet completed', code: 'round_incomplete', round: 2 }),
        });
        try {
          await API.swissGenerateRound('c1', 'pw');
          throw new Error('should have rejected');
        } catch (e) {
          // The admin section checks `e.code === "round_incomplete"`
          // to surface the friendly inline error. Pin the field
          // propagation so a refactor can't silently drop it.
          expect(e.message).toMatch(/round 2 not yet completed/);
          expect(e.code).toBe('round_incomplete');
          expect(e.round).toBe(2);
        }
      });

      it('throws default message when server gives no error body', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 500,
          json: async () => { throw new Error('parse'); },
        });
        await expect(API.swissGenerateRound('c1', 'pw'))
          .rejects.toThrow('Failed to generate Swiss round');
      });
    });

    describe('swissStandings', () => {
      it('GETs the standings without auth (public endpoint)', async () => {
        const sample = [{ player: { name: 'Alice' }, wins: 3, losses: 1, draws: 0 }];
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          status: 200,
          json: async () => sample,
        });
        const r = await API.swissStandings('c1');
        expect(r).toEqual(sample);
        // No password header on the public endpoint — pin the call
        // shape so a refactor doesn't accidentally make it admin-only.
        expect(global.fetch).toHaveBeenCalledWith('/api/competitions/c1/swiss/standings');
      });

      it('throws with server message on error', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 404,
          json: async () => ({ error: 'competition not found' }),
        });
        await expect(API.swissStandings('c1')).rejects.toThrow('competition not found');
      });
    });

    // POST /api/tournament — bootstrap. In file mode the call is
    // unauthenticated (the AuthMiddleware uninitialized branch lets
    // it through). In locked mode the server requires the env-var
    // password and the SPA passes it via the new optional second
    // arg, which the client attaches as X-Tournament-Password.
    describe('createTournament', () => {
      it('omits X-Tournament-Password when authPassword is undefined (file-mode bootstrap)', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ name: 'X' }),
        });
        await API.createTournament({ name: 'X' });
        const [, opts] = global.fetch.mock.calls[0];
        expect(opts.method).toBe('POST');
        expect(opts.headers['Content-Type']).toBe('application/json');
        expect(opts.headers['X-Tournament-Password']).toBeUndefined();
      });

      it('attaches X-Tournament-Password when authPassword is supplied (locked-mode bootstrap)', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ name: 'X' }),
        });
        await API.createTournament({ name: 'X' }, 'kotai-A');
        const [, opts] = global.fetch.mock.calls[0];
        expect(opts.headers['X-Tournament-Password']).toBe('kotai-A');
      });

      it('falsy authPassword does not attach the header (avoid accidental empty string)', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ name: 'X' }),
        });
        await API.createTournament({ name: 'X' }, '');
        const [, opts] = global.fetch.mock.calls[0];
        expect(opts.headers['X-Tournament-Password']).toBeUndefined();
      });

      it('throws with server message on error', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 401,
          json: async () => ({ error: 'invalid tournament password' }),
        });
        await expect(API.createTournament({ name: 'X' }, 'wrong')).rejects.toThrow('invalid tournament password');
      });
    });

    // GET /api/auth-config — public endpoint reporting the active
    // auth mode (file vs locked). The SPA reads it on App() mount to
    // decide whether to show the "Forgot password?" link in AuthModal
    // and whether the /reset page renders a form vs an
    // operator-disabled message. Failing-open (default to file +
    // reset-enabled) on transport errors is intentional — never let a
    // transient 5xx hide the recovery path from operators who need it.
    describe('fetchAuthConfig', () => {
      it('returns the parsed config on success', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ mode: 'locked', resetEnabled: false }),
        });
        await expect(API.fetchAuthConfig()).resolves.toEqual({
          mode: 'locked',
          resetEnabled: false,
        });
        expect(global.fetch).toHaveBeenCalledWith('/api/auth-config');
      });

      it('fails open on non-2xx (defaults to file + reset-enabled)', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 500,
          json: async () => ({ error: 'oops' }),
        });
        await expect(API.fetchAuthConfig()).resolves.toEqual({
          mode: 'file',
          resetEnabled: true,
        });
      });
    });

    // POST /api/tournament/reset — the unauth'd password-recovery
    // endpoint. The client must:
    //   - send the new password in a JSON body
    //   - return true on a 204 success (no res.json() — same empty-body
    //     pattern as overridePoolRank above)
    //   - throw an Error whose .status is the HTTP status, so the
    //     ResetPasswordForm can branch on 404 ("disabled by operator")
    //     vs other errors.
    describe('resetPassword', () => {
      it('POSTs JSON {password} to /api/tournament/reset', async () => {
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 });
        await API.resetPassword('newpw');
        expect(global.fetch).toHaveBeenCalledWith(
          '/api/tournament/reset',
          expect.objectContaining({
            method: 'POST',
            headers: expect.objectContaining({
              'Content-Type': 'application/json',
            }),
            body: JSON.stringify({ password: 'newpw' }),
          }),
        );
      });

      it('does NOT send an X-Tournament-Password header', async () => {
        // The whole point of /reset is that the caller doesn't know
        // the current password. Sending the header anyway would be
        // harmless server-side but would obscure intent and trip a
        // future security scanner that flagged the header on the
        // public endpoint.
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 });
        await API.resetPassword('newpw');
        const [, opts] = global.fetch.mock.calls[0];
        expect(opts.headers['X-Tournament-Password']).toBeUndefined();
      });

      it('resolves to true on 204 No Content', async () => {
        global.fetch = vi.fn().mockResolvedValue({ ok: true, status: 204 });
        await expect(API.resetPassword('newpw')).resolves.toBe(true);
      });

      it('does not call res.json() on the success path', async () => {
        // Empty 204 body — any call to .json() would throw
        // SyntaxError per the Fetch spec. Mirrors the
        // overridePoolRank regression coverage above.
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          status: 204,
          json: async () => { throw new SyntaxError('Unexpected end of JSON input'); },
        });
        await expect(API.resetPassword('newpw')).resolves.toBe(true);
      });

      it('throws an Error with the server-reported message on failure', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 400,
          json: async () => ({ error: 'password is required' }),
        });
        await expect(API.resetPassword('')).rejects.toThrow('password is required');
      });

      it('propagates HTTP status on the thrown Error so the UI can branch on 404', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 404,
          json: async () => ({ error: 'reset disabled' }),
        });
        try {
          await API.resetPassword('whatever');
          throw new Error('should have rejected');
        } catch (e) {
          expect(e.status).toBe(404);
          expect(e.message).toBe('reset disabled');
        }
      });

      it('throws with default message if response json parse fails', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          status: 500,
          json: async () => { throw new Error('parse error'); },
        });
        await expect(API.resetPassword('newpw')).rejects.toThrow('Failed to reset password');
      });
    });

    describe('estimateSchedule', () => {
      it('GETs /api/schedule/estimate with required params in query string', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ totalDurationMinutes: 120, perCourtMinutes: [120], ceremonyMinutes: 0 }),
        });
        await API.estimateSchedule({ matchDuration: 3, multiplier: 1.5, courts: 2, numMatches: 10 }, 'pw');
        const [url] = global.fetch.mock.calls[0];
        expect(url).toMatch(/^\/api\/schedule\/estimate\?/);
        const params = new URLSearchParams(url.split('?')[1]);
        expect(params.get('matchDuration')).toBe('3');
        expect(params.get('multiplier')).toBe('1.5');
        expect(params.get('courts')).toBe('2');
        expect(params.get('numMatches')).toBe('10');
      });

      it('filters out undefined, null, empty-string, and NaN arg values', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ totalDurationMinutes: 60, perCourtMinutes: [60], ceremonyMinutes: 0 }),
        });
        await API.estimateSchedule({
          matchDuration: 3,
          multiplier: 1.5,
          courts: 1,
          teamSize: undefined,
          boutsPerTeamMatch: null,
          buffer: '',
          ceremonyMinutes: NaN,
        }, 'pw');
        const [url] = global.fetch.mock.calls[0];
        const params = new URLSearchParams(url.split('?')[1]);
        expect(params.has('teamSize')).toBe(false);
        expect(params.has('boutsPerTeamMatch')).toBe(false);
        expect(params.has('buffer')).toBe(false);
        expect(params.has('ceremonyMinutes')).toBe(false);
      });

      it('sends X-Tournament-Password header', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ totalDurationMinutes: 90, perCourtMinutes: [90], ceremonyMinutes: 0 }),
        });
        await API.estimateSchedule({ matchDuration: 5, multiplier: 1, courts: 1 }, 'secret-pw');
        const [, opts] = global.fetch.mock.calls[0];
        expect(opts.headers['X-Tournament-Password']).toBe('secret-pw');
      });

      it('forwards the AbortSignal to fetch', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ totalDurationMinutes: 45, perCourtMinutes: [45], ceremonyMinutes: 0 }),
        });
        const controller = new AbortController();
        await API.estimateSchedule({ matchDuration: 3, multiplier: 1.5, courts: 1 }, 'pw', controller.signal);
        const [, opts] = global.fetch.mock.calls[0];
        expect(opts.signal).toBe(controller.signal);
      });

      it('returns parsed JSON on success', async () => {
        const payload = { totalDurationMinutes: 180, perCourtMinutes: [90, 90], ceremonyMinutes: 30 };
        global.fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => payload });
        const result = await API.estimateSchedule({ matchDuration: 3, multiplier: 1.5, courts: 2 }, 'pw');
        expect(result).toEqual(payload);
      });

      it('throws with server error message on non-ok response', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          json: async () => ({ error: 'matchDuration must be a positive finite number' }),
        });
        await expect(
          API.estimateSchedule({ matchDuration: -1, multiplier: 1.5, courts: 1 }, 'pw')
        ).rejects.toThrow('matchDuration must be a positive finite number');
      });

      it('throws default message if json parse fails on error response', async () => {
        global.fetch = vi.fn().mockResolvedValue({
          ok: false,
          json: async () => { throw new Error('parse error'); },
        });
        await expect(
          API.estimateSchedule({ matchDuration: 3, multiplier: 1.5, courts: 1 }, 'pw')
        ).rejects.toThrow('Failed to estimate schedule');
      });
    });
  });
});

