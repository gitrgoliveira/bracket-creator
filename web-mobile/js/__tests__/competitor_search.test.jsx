import { describe, it, expect } from 'vitest';
import {
  matchesCompetitorNumber,
  competitorMatchesQuery,
  matchMentions,
} from '../competitor_search.jsx';
import { buildRoster } from '../viewer_watchlist_core.jsx';
import { numberOf } from '../competitor_identity.jsx';

// The worked example from the module header (bc-nsrc operator ruling): a
// number search needs its prefix, and once the prefix is typed the number
// must be typed whole. Table-driven over the SAME roster the header uses, so
// a future edit that narrows or widens the anchoring shows up here first.
describe('matchesCompetitorNumber; the number rule', () => {
  const NUMBERS = ['K1', 'K12', 'K120', 'M12', 'M112', 'SK1'];

  const cases = [
    // Bare digits: no prefix means it is not a competitor number at all,
    // whatever digits happen to overlap with a real one.
    ['12', []],
    ['1', []],
    // Prefix alone (no digit in q): every number starting with it, unanchored.
    ['k', ['K1', 'K12', 'K120']],
    // "sk1" does not start with "k": SK is a different prefix from K, not a
    // K-prefixed continuation, so "sk" must never pull in the K numbers.
    ['sk', ['SK1']],
    // Once a digit is typed, the match is exact: "k1" is K1's WHOLE number,
    // so K12 and K120 (which merely start with "k1") must NOT hit.
    ['k1', ['K1']],
    ['k12', ['K12']],
    ['k120', ['K120']],
  ];

  cases.forEach(([q, expectedHits]) => {
    it(`"${q}" -> [${expectedHits.join(', ')}]`, () => {
      const hits = NUMBERS.filter((n) => matchesCompetitorNumber({ number: n }, q));
      expect(hits).toEqual(expectedHits);
    });
  });

  // Same table again but restated as explicit misses, because the ruling is
  // as much about what must NOT match as what must: a regression that widens
  // the anchor back to includes() passes every "hits" row above trivially (a
  // superset still contains the expected subset) but only these rows catch it.
  it('explicit misses: a prefix-typed number never matches a longer or shorter sibling', () => {
    expect(matchesCompetitorNumber({ number: 'K12' }, 'k1')).toBe(false);
    expect(matchesCompetitorNumber({ number: 'K120' }, 'k1')).toBe(false);
    expect(matchesCompetitorNumber({ number: 'K120' }, 'k12')).toBe(false);
    expect(matchesCompetitorNumber({ number: 'K1' }, 'k12')).toBe(false);
    // A competitor now holds exactly one number, so the old fixture that put
    // K1/K12/K120 on a single record is re-expressed as three separate ones.
    expect(matchesCompetitorNumber({ number: 'K1' }, 'sk1')).toBe(false);
    expect(matchesCompetitorNumber({ number: 'K12' }, 'sk1')).toBe(false);
    expect(matchesCompetitorNumber({ number: 'K120' }, 'sk1')).toBe(false);
    expect(matchesCompetitorNumber({ number: 'K1' }, '1')).toBe(false);
    expect(matchesCompetitorNumber({ number: 'K12' }, '12')).toBe(false);
  });

  it('a bare-digit query still finds a legacy unprefixed number, because that number\'s WHOLE value is the digit', () => {
    expect(matchesCompetitorNumber({ number: '1' }, '1')).toBe(true);
  });

  it('returns false for an empty query and does not throw on a null/undefined competitor', () => {
    expect(matchesCompetitorNumber({ number: 'K1' }, '')).toBe(false);
    expect(matchesCompetitorNumber(null, 'k1')).toBe(false);
    expect(matchesCompetitorNumber(undefined, 'k1')).toBe(false);
  });
});

describe('numberOf, the accessor this rule reads through', () => {
  // A roster record and a match side carry the number under the SAME field,
  // so there are no longer two shapes to reconcile -- the `numbers` array this
  // suite used to exercise never existed in production and is gone.
  it('reads the `number` string off either competitor shape', () => {
    expect(numberOf({ number: 'K1' })).toBe('K1');
  });

  it('returns "" for a competitor with no number and for a null/undefined competitor', () => {
    expect(numberOf({ number: '' })).toBe('');
    expect(numberOf({})).toBe('');
    expect(numberOf(null)).toBe('');
    expect(numberOf(undefined)).toBe('');
  });
});

describe('competitorMatchesQuery; name/dojo substring, number anchored', () => {
  it('matches name as a substring, case-insensitively (q is already lowercased by the caller)', () => {
    expect(competitorMatchesQuery({ name: 'Alice Tanaka' }, 'tanaka')).toBe(true);
    expect(competitorMatchesQuery({ name: 'Alice Tanaka' }, 'zzz')).toBe(false);
  });

  it('matches dojo as a substring', () => {
    expect(competitorMatchesQuery({ dojo: 'Shibuya Kendo Club' }, 'shibuya')).toBe(true);
    expect(competitorMatchesQuery({ dojo: 'Shibuya Kendo Club' }, 'osaka')).toBe(false);
  });

  it('falls through to the number rule when name and dojo do not match', () => {
    expect(competitorMatchesQuery({ name: 'Alice', dojo: 'X', number: 'K12' }, 'k12')).toBe(true);
    expect(competitorMatchesQuery({ name: 'Alice', dojo: 'X', number: 'K12' }, 'k1')).toBe(false);
  });

  it('an empty query matches everything, including a null/undefined competitor', () => {
    expect(competitorMatchesQuery({ name: 'Alice' }, '')).toBe(true);
    expect(competitorMatchesQuery(null, '')).toBe(true);
    expect(competitorMatchesQuery(undefined, '')).toBe(true);
  });

  it('a non-empty query does not throw on a null/undefined competitor', () => {
    expect(competitorMatchesQuery(null, 'alice')).toBe(false);
    expect(competitorMatchesQuery(undefined, 'k1')).toBe(false);
  });
});

describe('matchMentions; either side of a match', () => {
  const sideA = { name: 'Alice', dojo: 'Shibuya', number: 'K1' };
  const sideB = { name: 'Bob', dojo: 'Osaka', number: 'K12' };

  it('matches when only sideA matches', () => {
    expect(matchMentions({ sideA, sideB }, 'k1')).toBe(true);
  });

  it('matches when only sideB matches', () => {
    expect(matchMentions({ sideA, sideB }, 'osaka')).toBe(true);
  });

  it('is false when neither side matches', () => {
    expect(matchMentions({ sideA, sideB }, 'nobody')).toBe(false);
  });

  it('does not throw on a match with a missing side', () => {
    expect(matchMentions({ sideA }, 'alice')).toBe(true);
    expect(matchMentions({ sideB }, 'alice')).toBe(false);
    expect(matchMentions({}, 'alice')).toBe(false);
  });

  it('is false for an empty query or a null/undefined match', () => {
    expect(matchMentions({ sideA, sideB }, '')).toBe(false);
    expect(matchMentions(null, 'k1')).toBe(false);
    expect(matchMentions(undefined, 'k1')).toBe(false);
  });
});

// buildRoster feeding this module with real roster records.
//
// The shape that matters here is NOT what it first looks like. Participant ids
// are minted PER COMPETITION (a fresh uuid in state.AddParticipant, and
// `${compID}-pN` in the admin client), so one person entered in two
// competitions holds two DIFFERENT ids and arrives as TWO roster records, each
// carrying its own competition's number. Nothing merges them, and nothing
// needs to: both numbers are independently searchable because each sits on its
// own record. Verified against a running server, where the same Kenji Mori
// (Osaka) entered in two competitions was written to disk under two distinct
// uuids.
//
// Fixtures here therefore give each competition its own participant id. A
// fixture reusing ONE id across two competitions would be pinning a state the
// app cannot produce.
describe('buildRoster feeding competitor_search', () => {
  const comp = (id, status, players) => ({ id, name: id, status, checkInEnabled: false, players });

  it('the same person in two competitions is two records, and BOTH numbers are findable', () => {
    const roster = buildRoster([
      comp('A', 'running', [{ id: 'A-p1', name: 'Alice', dojo: 'Shibuya', number: 'K1' }]),
      comp('B', 'running', [{ id: 'B-p1', name: 'Alice', dojo: 'Shibuya', number: 'M12' }]),
    ]);
    expect(roster, 'two competitions, two ids, two records').toHaveLength(2);
    expect(roster.filter((p) => matchesCompetitorNumber(p, 'k1'))).toHaveLength(1);
    expect(roster.filter((p) => matchesCompetitorNumber(p, 'm12'))).toHaveLength(1);
  });

  it('each record carries the competition it came from, for the picker row', () => {
    const roster = buildRoster([
      comp('A', 'running', [{ id: 'A-p1', name: 'Alice', dojo: 'Shibuya', number: 'K1' }]),
    ]);
    expect(roster[0].comps).toEqual(['A']);
  });

  it('a record with no number matches no number query, which is how a pre-draw roster behaves', () => {
    const roster = buildRoster([comp('A', 'setup', [{ id: 'A-p1', name: 'Alice', dojo: 'Shibuya' }])]);
    expect(numberOf(roster[0])).toBe('');
    expect(matchesCompetitorNumber(roster[0], 'k1')).toBe(false);
    expect(competitorMatchesQuery(roster[0], 'alice'), 'still findable by name').toBe(true);
  });

  it('drops a player with no id, and does not collapse two id-less players into one entry', () => {
    // The guard matters because the schedule picker used to run its own dedup
    // WITHOUT it, keying every id-less player on `undefined` and merging them
    // all into a single row. That picker now shares this builder.
    const roster = buildRoster([
      comp('A', 'running', [
        { id: '', name: 'Ghost', dojo: 'X', number: 'K1' },
        { name: 'Ghost2', dojo: 'X', number: 'K2' },
      ]),
    ]);
    expect(roster).toHaveLength(0);
  });
});
