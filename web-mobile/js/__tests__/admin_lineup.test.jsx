import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { canRevise, rosterFor, teamIdOf, positionsForSize } from '../admin_lineup.jsx';

// admin_lineup.jsx ships three module-private helpers that gate the
// "Revise lineup" flow and normalize the team-shape variants that the
// backend has emitted historically. Each is exported from the module so
// vitest can pin the wire-format-drift guard (canRevise) and the
// camelCase/PascalCase tolerance (rosterFor, teamIdOf) without
// mounting the component (the React mock in vitest.setup.js doesn't
// run hooks for real, so component-level rendering tests don't pay
// their way here — pure helpers do).

describe('canRevise', () => {
  // window.compMatches is the projection helper viewer.jsx installs to
  // flatten a competition into a [{phase, round, status, ...}] list.
  // canRevise relies on it; the tests stub it per scenario.
  let originalCompMatches;
  beforeEach(() => {
    originalCompMatches = window.compMatches;
  });
  afterEach(() => {
    window.compMatches = originalCompMatches;
  });

  it('returns false when competition is null', () => {
    window.compMatches = () => [];
    expect(canRevise(null, 0)).toBe(false);
    expect(canRevise(undefined, 0)).toBe(false);
  });

  it('returns false when window.compMatches is missing', () => {
    // Real-world: viewer.jsx wasn't loaded yet (e.g. test harness that
    // imports admin_lineup in isolation). Fail-closed rather than
    // throw.
    delete window.compMatches;
    expect(canRevise({ id: 'c1' }, 0)).toBe(false);
  });

  describe('wire-format-drift guard (the recent hardening)', () => {
    // Regression anchor: the heuristic previously returned true whenever
    // no `Round N+2` match was running AND no `Round N+1` was live.
    // For a pool-only competition (no `Round N` labels at all), both
    // checks evaluated to false and the helper unconditionally returned
    // true — allowing Revise even when we have no idea what round the
    // server is on. The fix requires at least one `^Round \d+$` label
    // before delegating to the round-specific checks.

    it('returns false when compMatches returns an empty list', () => {
      window.compMatches = () => [];
      expect(canRevise({ id: 'c1' }, 0)).toBe(false);
    });

    it('returns false when no match has a `Round N` label (pool-only shape)', () => {
      window.compMatches = () => [
        { id: 'm1', round: 'Pool A', status: 'completed' },
        { id: 'm2', round: 'Pool A', status: 'completed' },
        { id: 'm3', round: 'Pool B', status: 'scheduled' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(false);
    });

    it('returns false when round labels exist but are not the `Round N` shape', () => {
      // Custom labels (e.g. "Final", "Semifinals") that don't match the
      // strict regex must also fail-closed — the round-specific checks
      // would never match these strings anyway.
      window.compMatches = () => [
        { id: 'm1', round: 'Finals', status: 'completed' },
        { id: 'm2', round: 'Semifinals', status: 'completed' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(false);
    });

    it('returns false when round field is non-string (e.g. numeric)', () => {
      // If the wire format ever switches to a numeric round, the regex
      // won't match — must still fail-closed.
      window.compMatches = () => [
        { id: 'm1', round: 1, status: 'completed' },
        { id: 'm2', round: 2, status: 'scheduled' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(false);
    });

    it('returns false when round is "Round 1.5" or "round 1" (case/format drift)', () => {
      // The regex is /^Round \d+$/ — strict. Lowercase, decimals, or
      // trailing text should all fail-closed.
      window.compMatches = () => [
        { id: 'm1', round: 'round 1', status: 'completed' },
        { id: 'm2', round: 'Round 1.5', status: 'completed' },
        { id: 'm3', round: 'Round 1 (Quarterfinal)', status: 'completed' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(false);
    });
  });

  describe('round-specific gating (after wire-format guard passes)', () => {
    it('returns false when a current-round match is still live', () => {
      // round=0 (display "Round 1"); a current-round match is in
      // "Round 1" (round + 1 == 1). Live → block revise.
      window.compMatches = () => [
        { id: 'm1', round: 'Round 1', status: 'running' },
        { id: 'm2', round: 'Round 1', status: 'completed' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(false);
    });

    it('returns false when a next-round match has already started (running)', () => {
      // round=0; next round is "Round 2" (round + 2 == 2). Running
      // there means the operator can't revise — the round we'd be
      // revising the lineup for is already in flight.
      window.compMatches = () => [
        { id: 'm1', round: 'Round 1', status: 'completed' },
        { id: 'm2', round: 'Round 2', status: 'running' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(false);
    });

    it('returns false when a next-round match has already started (completed)', () => {
      // Symmetric with the "running" case — once a Round 2 match has
      // finished, Round 1's lineup is fully consumed.
      window.compMatches = () => [
        { id: 'm1', round: 'Round 1', status: 'completed' },
        { id: 'm2', round: 'Round 2', status: 'completed' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(false);
    });

    it('returns true on the happy path (current round done, next round not yet started)', () => {
      window.compMatches = () => [
        { id: 'm1', round: 'Round 1', status: 'completed' },
        { id: 'm2', round: 'Round 1', status: 'completed' },
        { id: 'm3', round: 'Round 2', status: 'scheduled' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(true);
    });

    it('returns true when no Round 2 match exists yet (final-round lineup correction)', () => {
      window.compMatches = () => [
        { id: 'm1', round: 'Round 1', status: 'completed' },
      ];
      expect(canRevise({ id: 'c1' }, 0)).toBe(true);
    });

    it('handles round offset correctly: round=1 looks at Round 2/3', () => {
      // round=1 (display "Round 2"); current round is Round 2,
      // next round is Round 3. A live Round 2 match blocks revise.
      window.compMatches = () => [
        { id: 'm1', round: 'Round 1', status: 'completed' },
        { id: 'm2', round: 'Round 2', status: 'running' },
      ];
      expect(canRevise({ id: 'c1' }, 1)).toBe(false);
    });
  });
});

describe('rosterFor', () => {
  // Player team objects historically come in two casings:
  //   - Go JSON marshaller default: `Metadata` (PascalCase) — older builds
  //   - api_serializers.jsx normalised: `metadata` (camelCase) — current
  // The helper must accept either and fall back to empty array.

  it('accepts camelCase metadata', () => {
    const team = { name: 'Tora A', metadata: ['Tanaka', 'Sato', 'Yamada'] };
    expect(rosterFor(team)).toEqual(['Tanaka', 'Sato', 'Yamada']);
  });

  it('accepts PascalCase Metadata', () => {
    const team = { Name: 'Tora A', Metadata: ['Tanaka', 'Sato', 'Yamada'] };
    expect(rosterFor(team)).toEqual(['Tanaka', 'Sato', 'Yamada']);
  });

  it('prefers camelCase when both are present', () => {
    // camelCase is the canonical post-normalization shape, so the
    // helper checks it first. Pinning this means a future "be smart
    // about merging both lists" refactor has to update the test.
    const team = {
      metadata: ['camel1', 'camel2'],
      Metadata: ['pascal1', 'pascal2'],
    };
    expect(rosterFor(team)).toEqual(['camel1', 'camel2']);
  });

  it('returns [] when both arrays are empty', () => {
    expect(rosterFor({ metadata: [], Metadata: [] })).toEqual([]);
  });

  it('returns [] when neither shape is present', () => {
    expect(rosterFor({ name: 'Tora A' })).toEqual([]);
  });

  it('returns [] when team is null or undefined', () => {
    expect(rosterFor(null)).toEqual([]);
    expect(rosterFor(undefined)).toEqual([]);
  });

  it('returns [] when metadata is not an array (defensive)', () => {
    expect(rosterFor({ metadata: 'not an array' })).toEqual([]);
    expect(rosterFor({ Metadata: { 0: 'wrong shape' } })).toEqual([]);
  });
});

describe('teamIdOf', () => {
  // Stable team identifier resolution. The backend uses player.id (UUID
  // assigned at first persist), older code paths emit `ID`, and a
  // pre-persist team has neither — fall back to name.

  it('returns team.id when present (canonical case)', () => {
    expect(teamIdOf({ id: 'uuid-1', name: 'Tora A' })).toBe('uuid-1');
  });

  it('falls back to ID (PascalCase) when lowercase id is missing', () => {
    expect(teamIdOf({ ID: 'uuid-2', name: 'Tora A' })).toBe('uuid-2');
  });

  it('falls back to name when no id field is present', () => {
    expect(teamIdOf({ name: 'Tora A' })).toBe('Tora A');
  });

  it('falls back to Name (PascalCase) when nothing else is present', () => {
    expect(teamIdOf({ Name: 'Tora A' })).toBe('Tora A');
  });

  it('prefers id over ID', () => {
    expect(teamIdOf({ id: 'lower', ID: 'upper' })).toBe('lower');
  });

  it('prefers id over name', () => {
    expect(teamIdOf({ id: 'uuid-1', name: 'Tora A' })).toBe('uuid-1');
  });

  it('prefers name over Name', () => {
    expect(teamIdOf({ name: 'lowercase', Name: 'Uppercase' })).toBe('lowercase');
  });

  it('returns "" when team is null/undefined or empty', () => {
    expect(teamIdOf(null)).toBe('');
    expect(teamIdOf(undefined)).toBe('');
    expect(teamIdOf({})).toBe('');
  });

  it('treats empty-string id as falsy and falls through to name', () => {
    // The chain uses `||` so empty strings short-circuit to the next
    // option — necessary because the backend has been observed to emit
    // an empty `id` on pre-persist teams alongside a real Name.
    expect(teamIdOf({ id: '', name: 'Tora A' })).toBe('Tora A');
  });
});

describe('positionsForSize', () => {
  // Sanity coverage for the FIK 5-position constant + the numeric
  // fallback for non-5 team sizes. Pins the position-key shape that
  // the lineup form + the team-scoring modal both consume.

  it('returns the canonical FIK 5-position labels for teamSize=5', () => {
    expect(positionsForSize(5)).toEqual([
      { key: 'senpo', label: 'Senpo' },
      { key: 'jiho', label: 'Jiho' },
      { key: 'chuken', label: 'Chuken' },
      { key: 'fukusho', label: 'Fukusho' },
      { key: 'taisho', label: 'Taisho' },
    ]);
  });

  it('returns numeric "1".."N" labels for non-5 sizes', () => {
    expect(positionsForSize(3)).toEqual([
      { key: '1', label: '1' },
      { key: '2', label: '2' },
      { key: '3', label: '3' },
    ]);
  });

  it('returns 7 positions for teamSize=7', () => {
    const out = positionsForSize(7);
    expect(out).toHaveLength(7);
    expect(out[0]).toEqual({ key: '1', label: '1' });
    expect(out[6]).toEqual({ key: '7', label: '7' });
  });
});
