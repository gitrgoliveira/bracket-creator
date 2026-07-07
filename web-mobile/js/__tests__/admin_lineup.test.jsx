import { describe, it, expect } from 'vitest';
import { rosterFor, mergeRosterWithAssigned, teamIdOf, positionsForSize } from '../admin_lineup.jsx';

// admin_lineup.jsx ships module-private helpers that normalize the
// team-shape variants the backend has emitted historically. Each is
// exported from the module so vitest can pin the camelCase/PascalCase
// tolerance (rosterFor, teamIdOf) without
// mounting the component (the React mock in vitest.setup.js doesn't
// run hooks for real, so component-level rendering tests don't pay
// their way here: pure helpers do).

describe('rosterFor', () => {
  // Player team objects historically come in two casings:
  //   - Go JSON marshaller default: `Metadata` (PascalCase): older builds
  //   - api_serializers.jsx normalised: `metadata` (camelCase): current
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

describe('mergeRosterWithAssigned', () => {
  // An operator entering a substitute via the picker's "+ Add …" row stores a
  // free name that is NOT in team.metadata. Without folding the lineup's
  // already-assigned names back into the roster, that substitute vanishes from
  // the autocomplete for the team's other positions.

  it('returns the base roster unchanged when no lineup is given', () => {
    const base = ['Tanaka', 'Sato'];
    expect(mergeRosterWithAssigned(base, null)).toEqual(['Tanaka', 'Sato']);
    expect(mergeRosterWithAssigned(base, undefined)).toEqual(['Tanaka', 'Sato']);
    expect(mergeRosterWithAssigned(base, {})).toEqual(['Tanaka', 'Sato']);
  });

  it('appends an added substitute not present in the base roster', () => {
    const base = ['Tanaka', 'Sato'];
    const lineup = { positions: { senpo: 'Tanaka', jiho: 'Newcomer' } };
    expect(mergeRosterWithAssigned(base, lineup)).toEqual(['Tanaka', 'Sato', 'Newcomer']);
  });

  it('keeps base names first, then extras in first-seen order', () => {
    const base = ['Tanaka', 'Sato'];
    const lineup = { positions: { senpo: 'Zeta', taisho: 'Alpha' } };
    expect(mergeRosterWithAssigned(base, lineup)).toEqual(['Tanaka', 'Sato', 'Zeta', 'Alpha']);
  });

  it('de-duplicates case-insensitively against base and other extras', () => {
    const base = ['Tanaka', 'Sato'];
    const lineup = { positions: { p1: 'tanaka', p2: 'Newcomer', p3: 'NEWCOMER' } };
    // "tanaka" collides with base "Tanaka"; second "NEWCOMER" collides with the
    // first "Newcomer": both dropped.
    expect(mergeRosterWithAssigned(base, lineup)).toEqual(['Tanaka', 'Sato', 'Newcomer']);
  });

  it('ignores blank / whitespace-only assignments', () => {
    const base = ['Tanaka'];
    const lineup = { positions: { p1: '', p2: '   ', p3: 'Sub' } };
    expect(mergeRosterWithAssigned(base, lineup)).toEqual(['Tanaka', 'Sub']);
  });

  it('trims surrounding whitespace on added names', () => {
    const base = [];
    const lineup = { positions: { p1: '  Padded  ' } };
    expect(mergeRosterWithAssigned(base, lineup)).toEqual(['Padded']);
  });

  it('tolerates a non-array base roster', () => {
    const lineup = { positions: { p1: 'Solo' } };
    expect(mergeRosterWithAssigned(null, lineup)).toEqual(['Solo']);
    expect(mergeRosterWithAssigned(undefined, lineup)).toEqual(['Solo']);
  });

  it('does not mutate the base roster array', () => {
    const base = ['Tanaka'];
    mergeRosterWithAssigned(base, { positions: { p1: 'Sub' } });
    expect(base).toEqual(['Tanaka']);
  });

  it('returns the same base array reference when there are no extras (no needless copy)', () => {
    const base = ['Tanaka', 'Sato'];
    const lineup = { positions: { senpo: 'Tanaka', jiho: 'Sato' } };
    expect(mergeRosterWithAssigned(base, lineup)).toBe(base);
  });
});

describe('teamIdOf', () => {
  // Stable team identifier resolution. The backend uses player.id (UUID
  // assigned at first persist), older code paths emit `ID`, and a
  // pre-persist team has neither: fall back to name.

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
    // option: necessary because the backend has been observed to emit
    // an empty `id` on pre-persist teams alongside a real Name.
    expect(teamIdOf({ id: '', name: 'Tora A' })).toBe('Tora A');
  });
});

describe('positionsForSize', () => {
  // Sanity coverage for the FIK 5-position constant + the numeric
  // fallback for non-5 team sizes. Pins the position-key shape that
  // the lineup form + the team-scoring modal both consume.

  it('returns the canonical FIK 5-position labels for teamSize=5', () => {
    // U1: each FIK position carries an optional `termId` so the label
    // can be wrapped in a <Term> tooltip on render. Numeric sizes
    // (positionsForSize(3) etc.) intentionally omit termId: there is
    // no canonical kendo term for "Match 3".
    expect(positionsForSize(5)).toEqual([
      { key: 'senpo', label: 'Senpo', termId: 'senpo' },
      { key: 'jiho', label: 'Jiho', termId: 'jiho' },
      { key: 'chuken', label: 'Chuken', termId: 'chuken' },
      { key: 'fukusho', label: 'Fukusho', termId: 'fukusho' },
      { key: 'taisho', label: 'Taisho', termId: 'taisho' },
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
