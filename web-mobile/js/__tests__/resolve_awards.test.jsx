// Tests for bracketHasDecidedFinal and resolveCompetitionAwards (viewer.jsx).
// These helpers are the single source of truth for mixed→linked-playoffs
// podium resolution.
import { describe, it, expect, vi } from 'vitest';
import { bracketHasDecidedFinal, resolveCompetitionAwards } from '../viewer.jsx';

// ── bracketHasDecidedFinal ────────────────────────────────────────────────────

describe('bracketHasDecidedFinal', () => {
  it('returns true when the last round has a decided final', () => {
    const bracket = {
      rounds: [
        [{ sideA: 'A', sideB: 'B', winner: 'A' }],
        [{ sideA: 'A', sideB: 'C', winner: 'A' }],
      ],
    };
    expect(bracketHasDecidedFinal(bracket)).toBe(true);
  });

  it('returns false when the final winner is null/empty', () => {
    const bracket = {
      rounds: [
        [{ sideA: 'A', sideB: 'B', winner: 'A' }],
        [{ sideA: 'A', sideB: 'C', winner: null }],
      ],
    };
    expect(bracketHasDecidedFinal(bracket)).toBe(false);
  });

  it('returns false when the final winner is empty string', () => {
    const bracket = {
      rounds: [[{ sideA: 'A', sideB: 'B', winner: '' }]],
    };
    expect(bracketHasDecidedFinal(bracket)).toBe(false);
  });

  it('returns false for empty rounds array', () => {
    expect(bracketHasDecidedFinal({ rounds: [] })).toBe(false);
  });

  it('returns false for null bracket', () => {
    expect(bracketHasDecidedFinal(null)).toBe(false);
  });

  it('returns false for undefined bracket', () => {
    expect(bracketHasDecidedFinal(undefined)).toBe(false);
  });

  it('returns true with normalized object winner', () => {
    const bracket = {
      rounds: [
        [{ sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' }, winner: { id: 'a', name: 'Alice' } }],
      ],
    };
    expect(bracketHasDecidedFinal(bracket)).toBe(true);
  });
});

// ── resolveCompetitionAwards ──────────────────────────────────────────────────

// Build a minimal decided bracket fixture.
function decidedBracket() {
  return {
    rounds: [
      [
        { sideA: 'Alice', sideB: 'Bob', winner: 'Alice' },
        { sideA: 'Carol', sideB: 'Dan', winner: 'Carol' },
      ],
      [{ sideA: 'Alice', sideB: 'Carol', winner: 'Alice' }],
    ],
  };
}

function undecidedBracket() {
  return {
    rounds: [
      [{ sideA: 'Alice', sideB: 'Bob', winner: 'Alice' }, { sideA: 'Carol', sideB: 'Dan', winner: 'Carol' }],
      [{ sideA: 'Alice', sideB: 'Carol', winner: null }],
    ],
  };
}

describe('resolveCompetitionAwards', () => {
  // ── mixed → final (two 3rds) ──────────────────────────────────────────────
  it('mixed comp with decided linked-playoffs final → state "final", podium has two 3rds', async () => {
    const mixedComp = { id: 'mixed-1', format: 'mixed' };
    const playoffComp = { id: 'po-1', format: 'playoffs', sourceCompID: 'mixed-1' };
    const allComps = [mixedComp, playoffComp];
    const bracket = decidedBracket();
    const fetchers = {
      fetchCompetitionDetails: vi.fn().mockResolvedValue({ bracket, players: [] }),
      swissStandings: null,
    };

    const result = await resolveCompetitionAwards(mixedComp, allComps, fetchers);

    expect(result.state).toBe('final');
    expect(result.podium).toHaveLength(4);
    expect(result.podium[0]).toMatchObject({ place: 1, name: 'Alice' });
    expect(result.podium[1]).toMatchObject({ place: 2, name: 'Carol' });
    expect(result.podium[2]).toMatchObject({ place: 3 });
    expect(result.podium[3]).toMatchObject({ place: 3 });
    // Verify it fetches the linked PLAYOFF comp, not the mixed comp itself
    expect(fetchers.fetchCompetitionDetails).toHaveBeenCalledWith('po-1');
  });

  // ── mixed → in-progress (knockout not decided) ────────────────────────────
  it('mixed comp with undecided linked-playoffs final → state "in-progress", podium []', async () => {
    const mixedComp = { id: 'mixed-2', format: 'mixed' };
    const playoffComp = { id: 'po-2', format: 'playoffs', sourceCompID: 'mixed-2' };
    const allComps = [mixedComp, playoffComp];
    const bracket = undecidedBracket();
    const fetchers = {
      fetchCompetitionDetails: vi.fn().mockResolvedValue({ bracket, players: [] }),
      swissStandings: null,
    };

    const result = await resolveCompetitionAwards(mixedComp, allComps, fetchers);

    expect(result.state).toBe('in-progress');
    expect(result.podium).toEqual([]);
  });

  // ── mixed → in-progress (no linked playoff comp yet) ─────────────────────
  it('mixed comp with no linked playoffs comp → state "in-progress", podium []', async () => {
    const mixedComp = { id: 'mixed-3', format: 'mixed' };
    const allComps = [mixedComp]; // no playoffs comp yet
    const fetchers = {
      fetchCompetitionDetails: vi.fn(),
      swissStandings: null,
    };

    const result = await resolveCompetitionAwards(mixedComp, allComps, fetchers);

    expect(result.state).toBe('in-progress');
    expect(result.podium).toEqual([]);
    expect(fetchers.fetchCompetitionDetails).not.toHaveBeenCalled();
  });

  // ── standalone playoffs → final ───────────────────────────────────────────
  it('standalone playoffs comp with decided final → state "final", podium has two 3rds', async () => {
    const comp = { id: 'ko-1', format: 'playoffs' }; // no sourceCompID
    const allComps = [comp];
    const bracket = decidedBracket();
    const fetchers = {
      fetchCompetitionDetails: vi.fn().mockResolvedValue({ bracket, players: [] }),
      swissStandings: null,
    };

    const result = await resolveCompetitionAwards(comp, allComps, fetchers);

    expect(result.state).toBe('final');
    expect(result.podium).toHaveLength(4);
    expect(result.podium[0]).toMatchObject({ place: 1, name: 'Alice' });
    expect(result.podium[2]).toMatchObject({ place: 3 });
    expect(result.podium[3]).toMatchObject({ place: 3 });
  });

  // ── linked playoffs shell → skip ──────────────────────────────────────────
  it('playoffs comp with sourceCompID → state "skip", podium []', async () => {
    const comp = { id: 'po-99', format: 'playoffs', sourceCompID: 'mixed-99' };
    const allComps = [comp];
    const fetchers = {
      fetchCompetitionDetails: vi.fn(),
      swissStandings: null,
    };

    const result = await resolveCompetitionAwards(comp, allComps, fetchers);

    expect(result.state).toBe('skip');
    expect(result.podium).toEqual([]);
    expect(fetchers.fetchCompetitionDetails).not.toHaveBeenCalled();
  });

  // ── league (standings-based) → final ─────────────────────────────────────
  it('league comp → state "final", standings-based podium', async () => {
    const comp = { id: 'league-1', format: 'league' };
    const allComps = [comp];
    const standings = {
      'Pool A': [
        { player: { name: 'Alice', dojo: 'Aoyama' } },
        { player: { name: 'Bob', dojo: 'Bunkyo' } },
        { player: { name: 'Carol', dojo: 'Chiba' } },
        { player: { name: 'Dan', dojo: 'Denenchofu' } },
      ],
    };
    const fetchers = {
      fetchCompetitionDetails: vi.fn().mockResolvedValue({ bracket: null, standings, pools: [{ poolName: 'Pool A' }], players: [] }),
      swissStandings: null,
    };

    const result = await resolveCompetitionAwards(comp, allComps, fetchers);

    expect(result.state).toBe('final');
    expect(result.podium).toHaveLength(4);
    expect(result.podium[0]).toMatchObject({ place: 1, name: 'Alice' });
    expect(result.podium[3]).toMatchObject({ place: 3, name: 'Dan' });
  });

  // ── swiss (standings via dedicated endpoint) → final ─────────────────────
  it('swiss comp → calls swissStandings endpoint and returns standings-based podium', async () => {
    const comp = { id: 'swiss-1', format: 'swiss' };
    const allComps = [comp];
    const swissData = [
      { player: { name: 'Kenji', dojo: 'Club A' } },
      { player: { name: 'Hiro', dojo: 'Club B' } },
    ];
    const fetchers = {
      fetchCompetitionDetails: vi.fn().mockResolvedValue({ bracket: null, standings: null, pools: null, players: [] }),
      swissStandings: vi.fn().mockResolvedValue(swissData),
    };

    const result = await resolveCompetitionAwards(comp, allComps, fetchers);

    expect(result.state).toBe('final');
    expect(fetchers.swissStandings).toHaveBeenCalledWith('swiss-1');
    expect(result.podium[0]).toMatchObject({ place: 1, name: 'Kenji' });
    expect(result.podium[1]).toMatchObject({ place: 2, name: 'Hiro' });
  });
});
