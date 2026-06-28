// mp-xhaa: hardened logic layer for the unified watchlist.
// Covers polymorphic (player + dojo) entries, idempotent legacy migration,
// dojo-aware resolution, primary selection (implicit/pinned/stale), and the
// primary hero next-match builder. Pure functions only : no DOM, no hooks.
import { describe, it, expect } from 'vitest';
import {
  entryKey,
  normalizeWatchlistEntry,
  normalizeWatchlist,
  migrateWatchlistOnLoad,
  resolveEntryPlayerIds,
  resolveWatchedPlayers,
  effectivePrimaryKey,
  findPrimaryEntry,
  buildPrimaryNextMatch,
} from '../viewer.jsx';

// Fictitious dojo names per the design brief (no real-world clubs).
const roster = [
  { id: 'a1', name: 'Akira', dojo: 'Hagane Dojo', checkedIn: true },
  { id: 'a2', name: 'Aoi', dojo: 'Hagane Dojo', checkedIn: false },
  { id: 'b1', name: 'Botan', dojo: 'Tsubaki Kenyukai', checkedIn: false },
  { id: 'x1', name: 'Xeno', dojo: '' }, // empty dojo : never matched by a dojo entry
];

describe('entryKey', () => {
  it('keys players by id and dojos by name', () => {
    expect(entryKey({ type: 'player', id: 'a1' })).toBe('player:a1');
    expect(entryKey({ type: 'dojo', dojo: 'Hagane Dojo' })).toBe('dojo:Hagane Dojo');
  });
  it('keys legacy (typeless) player entries by id', () => {
    expect(entryKey({ id: 'a1', name: 'Akira' })).toBe('player:a1');
  });
  it('returns "" for unusable values', () => {
    expect(entryKey(null)).toBe('');
    expect(entryKey({})).toBe('');
    expect(entryKey({ type: 'dojo' })).toBe(''); // no dojo name
    expect(entryKey('nope')).toBe('');
  });
});

describe('normalizeWatchlistEntry', () => {
  it('upgrades a legacy player entry to {type:"player"}', () => {
    expect(normalizeWatchlistEntry({ id: 'a1', name: 'Akira', dojo: 'Hagane Dojo' }))
      .toEqual({ type: 'player', id: 'a1', name: 'Akira', dojo: 'Hagane Dojo' });
  });
  it('keeps a dojo entry and trims the name', () => {
    expect(normalizeWatchlistEntry({ type: 'dojo', dojo: '  Hagane Dojo  ' }))
      .toEqual({ type: 'dojo', dojo: 'Hagane Dojo' });
  });
  it('coerces numeric ids and missing fields to strings/empties', () => {
    expect(normalizeWatchlistEntry({ id: 42 }))
      .toEqual({ type: 'player', id: '42', name: '', dojo: '' });
  });
  it('returns null for entries with no usable identity', () => {
    expect(normalizeWatchlistEntry(null)).toBeNull();
    expect(normalizeWatchlistEntry({ name: 'no id' })).toBeNull();
    expect(normalizeWatchlistEntry({ type: 'dojo', dojo: '   ' })).toBeNull();
  });
});

describe('normalizeWatchlist', () => {
  it('drops unusable entries, dedups by key (first wins), preserves order', () => {
    const out = normalizeWatchlist([
      { id: 'a1', name: 'Akira' },
      { type: 'dojo', dojo: 'Hagane Dojo' },
      { id: 'a1', name: 'DUPLICATE' }, // dedup : first wins
      { name: 'garbage' },             // dropped
      { type: 'dojo', dojo: 'Hagane Dojo' }, // dedup
    ]);
    expect(out).toEqual([
      { type: 'player', id: 'a1', name: 'Akira', dojo: '' },
      { type: 'dojo', dojo: 'Hagane Dojo' },
    ]);
  });
  it('tolerates a non-array argument', () => {
    expect(normalizeWatchlist(null)).toEqual([]);
    expect(normalizeWatchlist(undefined)).toEqual([]);
    expect(normalizeWatchlist('nope')).toEqual([]);
  });
  it('caps at WATCHLIST_MAX (50)', () => {
    const many = Array.from({ length: 60 }, (_, i) => ({ id: `p${i}` }));
    expect(normalizeWatchlist(many)).toHaveLength(50);
  });
});

describe('migrateWatchlistOnLoad', () => {
  it('injects the legacy followed player at the front when absent', () => {
    const { list, migrated } = migrateWatchlistOnLoad(
      [{ type: 'dojo', dojo: 'Hagane Dojo' }], 'a1', 'Akira');
    expect(migrated).toBe(true);
    expect(list[0]).toEqual({ type: 'player', id: 'a1', name: 'Akira', dojo: '' });
    expect(list).toHaveLength(2);
  });
  it('is idempotent : already-present legacy id is a no-op', () => {
    const existing = [{ id: 'a1', name: 'Akira' }];
    const { list, migrated } = migrateWatchlistOnLoad(existing, 'a1', 'Akira');
    expect(migrated).toBe(false);
    expect(list).toEqual([{ type: 'player', id: 'a1', name: 'Akira', dojo: '' }]);
  });
  it('does nothing when there is no legacy id', () => {
    const { list, migrated } = migrateWatchlistOnLoad([{ id: 'b1' }], '', '');
    expect(migrated).toBe(false);
    expect(list).toEqual([{ type: 'player', id: 'b1', name: '', dojo: '' }]);
  });
  it('normalizes a corrupt stored watchlist while migrating', () => {
    const { list } = migrateWatchlistOnLoad('not-an-array', 'a1', 'Akira');
    expect(list).toEqual([{ type: 'player', id: 'a1', name: 'Akira', dojo: '' }]);
  });
});

describe('resolveEntryPlayerIds', () => {
  it('expands a dojo entry to current roster members', () => {
    expect(resolveEntryPlayerIds({ type: 'dojo', dojo: 'Hagane Dojo' }, roster)).toEqual(['a1', 'a2']);
  });
  it('returns the lone id for a player entry (even if not in roster)', () => {
    expect(resolveEntryPlayerIds({ type: 'player', id: 'ghost' }, roster)).toEqual(['ghost']);
  });
  it('returns [] for a dojo with no current members (auto-includes later)', () => {
    expect(resolveEntryPlayerIds({ type: 'dojo', dojo: 'Empty Dojo' }, roster)).toEqual([]);
  });
});

describe('resolveWatchedPlayers', () => {
  it('expands dojos, prefers roster records (check-in), and dedups by id', () => {
    const out = resolveWatchedPlayers([
      { type: 'player', id: 'a1' },              // also a member of Hagane Dojo
      { type: 'dojo', dojo: 'Hagane Dojo' },     // a1 (dup), a2
      { type: 'player', id: 'ghost', name: 'Ghost' }, // not in roster
    ], roster);
    expect(out.map((p) => p.id)).toEqual(['a1', 'a2', 'ghost']);
    expect(out[0]).toMatchObject({ id: 'a1', name: 'Akira', checkedIn: true });
    expect(out[2]).toMatchObject({ id: 'ghost', name: 'Ghost' });
  });
  it('returns [] for an empty or junk watchlist', () => {
    expect(resolveWatchedPlayers([], roster)).toEqual([]);
    expect(resolveWatchedPlayers(null, roster)).toEqual([]);
  });
});

describe('effectivePrimaryKey', () => {
  const oneDojo = [{ type: 'dojo', dojo: 'Hagane Dojo' }];
  const two = [{ id: 'a1' }, { id: 'a2' }];
  it('is null for an empty watchlist', () => {
    expect(effectivePrimaryKey([], '')).toBeNull();
  });
  it('is the sole entry implicitly when there is exactly one (ignores pin)', () => {
    expect(effectivePrimaryKey(oneDojo, '')).toBe('dojo:Hagane Dojo');
    expect(effectivePrimaryKey([{ id: 'a1' }], 'player:zzz')).toBe('player:a1');
  });
  it('is null with ≥2 entries and no pin (no hero, no chime)', () => {
    expect(effectivePrimaryKey(two, '')).toBeNull();
  });
  it('honors a valid pin with ≥2 entries', () => {
    expect(effectivePrimaryKey(two, 'player:a2')).toBe('player:a2');
  });
  it('drops a stale pin (pinned entry was removed) to null', () => {
    expect(effectivePrimaryKey(two, 'player:gone')).toBeNull();
  });
});

describe('findPrimaryEntry', () => {
  it('returns the primary entry object or null', () => {
    expect(findPrimaryEntry([{ id: 'a1' }], '')).toEqual({ type: 'player', id: 'a1', name: '', dojo: '' });
    expect(findPrimaryEntry([{ id: 'a1' }, { id: 'a2' }], '')).toBeNull();
    expect(findPrimaryEntry([{ id: 'a1' }, { id: 'a2' }], 'player:a2'))
      .toEqual({ type: 'player', id: 'a2', name: '', dojo: '' });
  });
});

describe('buildPrimaryNextMatch', () => {
  const matches = [
    { id: 'done', sideAId: 'a1', sideBId: 'z', status: 'completed', scheduledAt: '08:00' },
    { id: 'soon', sideAId: 'a1', sideBId: 'z', status: 'scheduled', scheduledAt: '11:00' },
    { id: 'early', sideAId: 'a2', sideBId: 'z', status: 'scheduled', scheduledAt: '09:00' },
    { id: 'live', sideAId: 'a2', sideBId: 'z', status: 'running', scheduledAt: '12:00' },
  ];
  it('returns the running match first for a dojo primary (running beats earlier time)', () => {
    const m = buildPrimaryNextMatch({ type: 'dojo', dojo: 'Hagane Dojo' }, roster, matches);
    expect(m.id).toBe('live');
  });
  it('returns the earliest scheduled match for a single player with no live match', () => {
    const m = buildPrimaryNextMatch({ type: 'player', id: 'a1' }, roster, matches);
    expect(m.id).toBe('soon'); // 'done' excluded, 'soon' is a1's only upcoming
  });
  it('excludes completed matches and returns null when none remain', () => {
    const only = [{ id: 'done', sideAId: 'a1', sideBId: 'z', status: 'completed' }];
    expect(buildPrimaryNextMatch({ type: 'player', id: 'a1' }, roster, only)).toBeNull();
  });
  it('returns null for a dojo with no current members', () => {
    expect(buildPrimaryNextMatch({ type: 'dojo', dojo: 'Empty Dojo' }, roster, matches)).toBeNull();
  });
  it('returns null for a null primary', () => {
    expect(buildPrimaryNextMatch(null, roster, matches)).toBeNull();
  });
});
