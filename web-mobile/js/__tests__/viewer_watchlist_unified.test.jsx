// mp-xhaa: hardened logic layer for the unified watchlist.
// Covers polymorphic (player + dojo) entries, idempotent legacy migration,
// dojo-aware resolution, primary selection (implicit/pinned/stale), and the
// primary hero next-match builder. Pure functions only: no DOM, no hooks.
import { describe, it, expect } from 'vitest';
import { readSource, readCode } from './helpers/source.js';
import {
  entryKey,
  normalizeWatchlistEntry,
  normalizeWatchlist,
  migrateWatchlistOnLoad,
  resolveEntryPlayerIds,
  resolveWatchedPlayers,
  effectivePrimaryKey,
  findPrimaryEntry,
  heroEntry,
  buildPrimaryNextMatch,
  buildPrimaryLastResult,
  rosterFullyLoaded,
} from '../viewer.jsx';

// Fictitious dojo names per the design brief (no real-world clubs).
const roster = [
  { id: 'a1', name: 'Akira', dojo: 'Hagane Dojo', checkedIn: true },
  { id: 'a2', name: 'Aoi', dojo: 'Hagane Dojo', checkedIn: false },
  { id: 'b1', name: 'Botan', dojo: 'Tsubaki Kenyukai', checkedIn: false },
  { id: 'x1', name: 'Xeno', dojo: '' }, // empty dojo: never matched by a dojo entry
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
      { id: 'a1', name: 'DUPLICATE' }, // dedup: first wins
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
  it('is idempotent: already-present legacy id is a no-op', () => {
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

// bc-wlhc. The two questions the panel asks are NOT the same question, and
// conflating them is what deleted the card the moment a coach added a second
// person to watch. Pinned as a PAIR: an assertion on heroEntry alone would pass
// if someone "simplified" findPrimaryEntry to share the fallback, which would
// hand the chime to whoever happened to be added first.
describe('heroEntry vs findPrimaryEntry', () => {
  const two = [{ id: 'a1' }, { id: 'a2' }];

  it('falls back to the first entry when nothing is pinned, while the chime does not', () => {
    expect(heroEntry(two, '')).toEqual({ type: 'player', id: 'a1', name: '', dojo: '' });
    expect(findPrimaryEntry(two, ''), 'the chime stays opt-in').toBeNull();
  });

  it('follows a valid pin, like the primary', () => {
    expect(heroEntry(two, 'player:a2')).toEqual({ type: 'player', id: 'a2', name: '', dojo: '' });
  });

  it('falls back when the pin is stale, so a removed pin cannot blank the card', () => {
    expect(heroEntry(two, 'player:gone')).toEqual({ type: 'player', id: 'a1', name: '', dojo: '' });
    expect(findPrimaryEntry(two, 'player:gone')).toBeNull();
  });

  it('is null only when nothing is watched', () => {
    expect(heroEntry([], '')).toBeNull();
    expect(heroEntry(null, '')).toBeNull();
  });

  // The UNPINNED fallback. Without the predicate it returned the first-ADDED
  // entry whatever its state, which reproduced the bug this function exists to
  // fix by list order: the coach who added a partner first and themselves
  // second lost their card the moment the partner finished.
  describe('the unpinned fallback prefers an entry that can fill the card', () => {
    const two = [{ type: 'player', id: 'a1' }, { type: 'player', id: 'a2' }];
    const entry = (id) => ({ type: 'player', id, name: '', dojo: '' });
    const pending = { id: 'm-soon', status: 'scheduled' };
    const result = { id: 'm-done', status: 'completed' };

    it('skips a first-added entry with nothing to show', () => {
      expect(heroEntry(two, '', (e) => (e.id === 'a2' ? pending : null))).toEqual(entry('a2'));
    });

    it('still names the first when NOBODY has a match', () => {
      // Not null: the panel needs a subject for "No upcoming matches for X".
      expect(heroEntry(two, '', () => null)).toEqual(entry('a1'));
    });

    it('a PIN wins even when it yields nothing', () => {
      // An explicit choice. Quietly showing someone else would be the worse
      // surprise, and the pin also drives the chime.
      expect(heroEntry(two, 'player:a1', (e) => (e.id === 'a2' ? pending : null))).toEqual(entry('a1'));
    });

    it('without a predicate it is unchanged: first added', () => {
      expect(heroEntry(two, '')).toEqual(entry('a1'));
    });

    // The TIERS, and why this takes a match rather than a boolean. Once the
    // card began falling back to a finished bout, "can this entry fill the
    // card" went true for anyone with a RESULT -- which by mid-afternoon is
    // almost everyone -- so a boolean predicate collapsed the fallback back to
    // the first-ADDED entry and re-opened the bug above in a quieter form.
    it('a match still to FIGHT outranks a RESULT, whatever the list order', () => {
      // The reported shape exactly: the partner (added first) has finished,
      // the reader (added second) is due on court.
      const matchFor = (e) => (e.id === 'a1' ? result : pending);
      expect(heroEntry(two, '', matchFor)).toEqual(entry('a2'));
    });

    it('a RESULT still outranks an entry with nothing at all', () => {
      // Second tier: nobody is due on, so the card shows the one person whose
      // day can still be reported on rather than an empty card for the other.
      const matchFor = (e) => (e.id === 'a2' ? result : null);
      expect(heroEntry(two, '', matchFor)).toEqual(entry('a2'));
    });

    it('two entries with results keep list order', () => {
      // Within a tier nothing reorders: first added wins, as it always did.
      expect(heroEntry(two, '', () => result)).toEqual(entry('a1'));
    });
  });

  // The helper being right is not the same as the HOST calling the right one.
  // Swapping viewer_home's two derivations passes every assertion above and
  // every prop assertion in the panel suite, and puts the bug straight back,
  // so the wiring is pinned at the source. A SOURCE check because ViewerHome
  // mounts over the viewer fetch harness; what a regression does is pass the
  // other variable, and that is what this catches.
  it('viewer_home feeds the card heroEntry and the chime findPrimaryEntry', () => {
    const src = readSource('viewer_home.jsx');
    // Matched loosely on purpose: what must hold is that the CARD's entry comes
    // from heroEntry over (watchlist, primaryKey). Pinning the whole memo line
    // meant a refactor of its body -- which is what adding the "has a match"
    // predicate was -- reddened a test whose message is about wiring.
    expect(src).toMatch(/heroEntry\(watchlist, primaryKey/);
    expect(src).toMatch(/heroEntry=\{heroWatchEntry\}/);
    expect(src).toMatch(/heroNextMatch=\{heroNextMatch\}/);
    // And the alert keeps the opt-in one.
    expect(src).toMatch(/useFollowedMatchAlert\(primaryNextMatch/);
    expect(src).toMatch(/findPrimaryEntry\(watchlist, primaryKey/);
  });


  // The ledger records what LANDED. A resolved token that the merge dropped at
  // WATCHLIST_MAX was being recorded as applied, after which the strip removed
  // the only copy of the link: the reader could prune to make room and reload
  // and get nothing. Read through readCode so the comment explaining this,
  // which necessarily names the same symbols, cannot satisfy the assertions.
  it('viewer_home carries out sharedLinkPass and decides nothing itself', () => {
    // The pass is the one owner of every rule about applying a link, and the
    // effect must only act on its verdict. In particular the WRITE is gated on
    // `pass.write` (something landed), never on the entries having resolved:
    // that gap is the loop the fixpoint test in watchlist_merge.test.jsx pins.
    const code = readCode('viewer_home.jsx');
    expect(code).toMatch(/const pass = sharedLinkPass\(\{/);
    expect(code).toMatch(/pass\.landed\.forEach\(\(k\) => sharedApplied\.current\.add\(k\)\);/);
    expect(code).toMatch(/if \(pass\.write\) setWatchlist\(/);
    expect(code).toMatch(/if \(!pass\.settle\) return;/);
    expect(code, 'the write gated on resolution is what looped').not.toMatch(/if \(entries\.length\) setWatchlist/);
    expect(code, 'no settle rule is spelled inline any more').not.toMatch(/outstanding > 0\) return;/);
  });
});

// A roster that is non-empty but INCOMPLETE. The viewer payload builds each
// competition independently and swallows a per-competition participants
// failure, so one unreadable participants.csv leaves every other competition
// populating the roster -- and the watchlist's roster.length guard, which is
// a proxy for "the roster loaded", passes.
// buildPrimaryLastResult: the other half of the split (operator ruling
// 2026-09-22). It used to be a fallback arm INSIDE buildPrimaryNextMatch,
// where it leaked to every other caller of that function.
describe('buildPrimaryLastResult', () => {
  it('returns the last result once nothing is left to fight', () => {
    // The card used to print "No upcoming matches" here, which answers the
    // wrong question: a competitor who is out is exactly who the reader still
    // cares about.
    const only = [{ id: 'done', sideAId: 'a1', sideBId: 'z', status: 'completed' }];
    expect(buildPrimaryLastResult({ type: 'player', id: 'a1' }, roster, only).id).toBe('done');
  });

  it('picks the most recent RESULT, by write time rather than by slot', () => {
    // resultRecencyDesc's rule (result_recency.jsx): the latest result is the
    // last WRITE, which is not the latest scheduled slot once a court has run
    // out of schedule order. Here the earlier slot was scored later.
    const done = [
      { id: 'late-slot', sideAId: 'a1', sideBId: 'z', status: 'completed', scheduledAt: '15:00', modifiedAt: 100 },
      { id: 'scored-last', sideAId: 'a1', sideBId: 'z', status: 'completed', scheduledAt: '09:00', modifiedAt: 900 },
    ];
    expect(buildPrimaryLastResult({ type: 'player', id: 'a1' }, roster, done).id).toBe('scored-last');
  });

  it('never returns a match still to be fought', () => {
    const pending = [{ id: 'soon', sideAId: 'a1', sideBId: 'z', status: 'scheduled', scheduledAt: '11:00' }];
    expect(buildPrimaryLastResult({ type: 'player', id: 'a1' }, roster, pending)).toBeNull();
  });

  it('answers for a dojo primary through its current members', () => {
    const done = [{ id: 'aoi-done', sideAId: 'a2', sideBId: 'z', status: 'completed' }];
    expect(buildPrimaryLastResult({ type: 'dojo', dojo: 'Hagane Dojo' }, roster, done).id).toBe('aoi-done');
  });

  it('keeps the id-only rule: an id-less side named after a dojo-mate gets nothing', () => {
    // The bc-pnum rule now lives in the helper both builders share, so it
    // cannot reach one and miss the other.
    const legacy = [{ id: 'legacy1', sideA: { id: '', name: 'Aoi' }, sideB: { id: '', name: 'X' }, status: 'completed' }];
    expect(buildPrimaryLastResult({ type: 'dojo', dojo: 'Hagane Dojo' }, roster, legacy)).toBeNull();
  });

  it('returns null for a null primary and for an empty list', () => {
    expect(buildPrimaryLastResult(null, roster, [])).toBeNull();
    expect(buildPrimaryLastResult({ type: 'player', id: 'a1' }, roster, [])).toBeNull();
  });
});

describe('rosterFullyLoaded', () => {
  it('is true when every competition reports its roster loaded', () => {
    expect(rosterFullyLoaded([{ rosterAvailable: true }, { rosterAvailable: true }])).toBe(true);
  });

  it('is false when ANY competition failed to load one', () => {
    expect(rosterFullyLoaded([{ rosterAvailable: true }, { rosterAvailable: false }])).toBe(false);
  });

  it('reads an ABSENT key as loaded', () => {
    // An older payload carries no such key. Reading that as "unavailable"
    // would silence the stale-entry warning entirely.
    expect(rosterFullyLoaded([{}, { rosterAvailable: true }])).toBe(true);
    expect(rosterFullyLoaded([])).toBe(true);
    expect(rosterFullyLoaded(null)).toBe(true);
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
  it('ignores a completed match while anything is still to be fought', () => {
    const m = buildPrimaryNextMatch({ type: 'player', id: 'a1' }, roster, matches);
    expect(m.id).toBe('soon');
  });

  it('returns NULL when every match is already fought, rather than the result', () => {
    // It answers one question. The last-result card is buildPrimaryLastResult
    // below, composed by the ONE surface that wants it: this function is also
    // what feeds the chime and ViewerOverview's hard-coded "Your next match"
    // banner, and a bout already fought must never reach either.
    const only = [{ id: 'done', sideAId: 'a1', sideBId: 'z', status: 'completed' }];
    expect(buildPrimaryNextMatch({ type: 'player', id: 'a1' }, roster, only)).toBeNull();
  });

  it('still returns null when the competitor has no matches at all', () => {
    expect(buildPrimaryNextMatch({ type: 'player', id: 'a1' }, roster, [])).toBeNull();
  });
  it('returns null for a dojo with no current members', () => {
    expect(buildPrimaryNextMatch({ type: 'dojo', dojo: 'Empty Dojo' }, roster, matches)).toBeNull();
  });
  it('returns null for a null primary', () => {
    expect(buildPrimaryNextMatch(null, roster, matches)).toBeNull();
  });

  // bc-pnum (HIGH regression from c23ea84e): the
  // primary entry always carries a real id (resolveEntryPlayerIds only ever
  // returns roster-backed ids), so an id-less match side is a MIXED pair
  // and must never be guessed at by name -- sameCompetitor's rule. The
  // removed name fallback (activated whenever the id pass found nothing)
  // matched ANY pending match by name regardless of whether that side
  // carried an id, so a legacy/id-less roster showed the wrong hero card
  // instead of none.
  it('never falls back to a name match for a dojo primary: an id-less side named after a dojo-mate gets no card', () => {
    // Hagane Dojo's members are Akira (a1) and Aoi (a2). Neither carries an
    // id on this legacy match, but one side happens to be named "Aoi" --
    // the OLD fallback built its name set from every CURRENT dojo member's
    // roster name, so it matched this dojo-mate's name even though this
    // specific match never carried her id.
    const legacyMatches = [
      { id: 'legacy1', sideA: { id: '', name: 'Aoi' }, sideB: { id: '', name: 'Someone Else' }, status: 'scheduled', scheduledAt: '09:00' },
    ];
    expect(buildPrimaryNextMatch({ type: 'dojo', dojo: 'Hagane Dojo' }, roster, legacyMatches)).toBeNull();
  });

  // Reproduces the live bug report verbatim: Akira (id a1) is followed. The
  // only pending match is an id-less legacy row where one side happens to
  // be named "Akira" too (a data quirk / bracket placeholder echoing the
  // follower's own name). The id pass finds nothing (neither side carries
  // a1); there must be no name fallback to fall into and name the follower
  // as their own opponent.
  it('never names the follower as their own opponent via a same-name id-less side', () => {
    const selfNamedMatch = [
      { id: 'weird', sideA: { id: '', name: 'Akira' }, sideB: { id: '', name: 'Someone Else' }, status: 'scheduled', scheduledAt: '09:00' },
    ];
    expect(buildPrimaryNextMatch({ type: 'player', id: 'a1', name: 'Akira' }, roster, selfNamedMatch)).toBeNull();
  });
});
