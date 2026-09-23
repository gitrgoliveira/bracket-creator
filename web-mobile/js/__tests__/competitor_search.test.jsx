import { describe, it, expect } from 'vitest';
import {
  matchesCompetitorNumber,
  competitorMatchesQuery,
  matchMentions,
} from '../competitor_search.jsx';
import { buildRoster } from '../viewer_watchlist_core.jsx';
import { buildPlayerMap, normalizeMatch } from '../api_serializers.jsx';
import { numberOf, prefixOf } from '../competitor_identity.jsx';

// The worked example from the module header (bc-nsrc operator rulings): the
// prefix alone selects the draw, and once typed past the prefix the number
// must be typed whole. Table-driven over the SAME roster the header uses, so
// a future edit that narrows or widens the anchoring shows up here first.
//
// Every record carries the prefix it was numbered under, as the two record
// builders stamp it in production (pinned at the bottom of this file). The
// prefix is a FIELD, not something read off the number: "K021" is K02's
// first competitor here, and the number string alone could not say so.
describe('matchesCompetitorNumber; the number rule', () => {
  const rec = (number, numberPrefix) => ({ number, numberPrefix });
  const ROSTER = [
    rec('K1', 'K'), rec('K12', 'K'), rec('K120', 'K'),
    rec('K021', 'K02'),
    rec('M12', 'M'), rec('M112', 'M'), rec('SK1', 'SK'),
  ];

  const cases = [
    // Bare digits: no prefix means it is not a competitor number at all,
    // whatever digits happen to overlap with a real one.
    ['12', []],
    ['1', []],
    // Prefix alone: every draw whose prefix begins with it. "k" begins both
    // K and K02, so both draws answer.
    ['k', ['K1', 'K12', 'K120', 'K021']],
    // A digit-bearing prefix, minted when the plain letter was taken: "k0"
    // and "k02" select the K02 draw and nothing from the K draw.
    ['k0', ['K021']],
    ['k02', ['K021']],
    // "sk" does not begin with "k": SK is a different prefix from K, not a
    // K-prefixed continuation, so "k" must never pull in the SK numbers and
    // "sk" pulls in only its own.
    ['sk', ['SK1']],
    ['s', ['SK1']],
    // Past the prefix the match is exact: "k1" is K1's WHOLE number, so K12
    // and K120 (which merely start with "k1") must NOT hit, and "k021" is
    // K02's first competitor, not K's twenty-first.
    ['k1', ['K1']],
    ['k12', ['K12']],
    ['k120', ['K120']],
    ['k021', ['K021']],
  ];

  cases.forEach(([q, expectedHits]) => {
    it(`"${q}" -> [${expectedHits.join(', ')}]`, () => {
      const hits = ROSTER.filter((p) => matchesCompetitorNumber(p, q)).map((p) => p.number);
      expect(hits).toEqual(expectedHits);
    });
  });

  // Same table again but restated as explicit misses, because the ruling is
  // as much about what must NOT match as what must: a regression that widens
  // the anchor back to includes() passes every "hits" row above trivially (a
  // superset still contains the expected subset) but only these rows catch it.
  it('explicit misses: a prefix-typed number never matches a longer or shorter sibling', () => {
    expect(matchesCompetitorNumber(rec('K12', 'K'), 'k1')).toBe(false);
    expect(matchesCompetitorNumber(rec('K120', 'K'), 'k1')).toBe(false);
    expect(matchesCompetitorNumber(rec('K120', 'K'), 'k12')).toBe(false);
    expect(matchesCompetitorNumber(rec('K1', 'K'), 'k12')).toBe(false);
    // A competitor now holds exactly one number, so the old fixture that put
    // K1/K12/K120 on a single record is re-expressed as three separate ones.
    expect(matchesCompetitorNumber(rec('K1', 'K'), 'sk1')).toBe(false);
    expect(matchesCompetitorNumber(rec('K12', 'K'), 'sk1')).toBe(false);
    expect(matchesCompetitorNumber(rec('K120', 'K'), 'sk1')).toBe(false);
    expect(matchesCompetitorNumber(rec('K1', 'K'), '1')).toBe(false);
    expect(matchesCompetitorNumber(rec('K12', 'K'), '12')).toBe(false);
    // The digit-bearing prefix does not leak into the plain one either way:
    // "k02" is not K's competitor 2 by any reading, and "k2" is not K02's draw.
    expect(matchesCompetitorNumber(rec('K2', 'K'), 'k02')).toBe(false);
    expect(matchesCompetitorNumber(rec('K021', 'K02'), 'k2')).toBe(false);
  });

  it('the prefix is read from the record, not inferred from the number', () => {
    // Same number string, different competition: only the field can tell.
    expect(matchesCompetitorNumber(rec('K021', 'K02'), 'k02')).toBe(true);
    expect(matchesCompetitorNumber(rec('K021', 'K'), 'k02')).toBe(false);
    // Trimmed as Go's EffectiveNumberPrefix trims it: the number was minted
    // from the trimmed value while the wire carries the field as typed.
    expect(matchesCompetitorNumber(rec('K1', ' K '), 'k')).toBe(true);
  });

  it('a bare-digit query still finds a legacy unprefixed number, because that number\'s WHOLE value is the digit', () => {
    // Such a record carries no prefix at all (a competitor only has a number
    // when their competition has a prefix, handlers_viewer.go numberingApplies),
    // so the exact arm is the only one that can answer for it.
    expect(matchesCompetitorNumber({ number: '1' }, '1')).toBe(true);
    expect(matchesCompetitorNumber({ number: '12' }, '1')).toBe(false);
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

  // THE INVARIANT the rule rests on: a record that carries a number carries
  // the prefix it was minted under, stamped from its competition. Without it
  // the "prefix alone" arm has nothing to read, and a reviewer would be
  // tempted to re-add an inference from the number string -- which cannot be
  // made ("K021": K02's first, or K's twenty-first?).
  it('stamps each record with its competition\'s numberPrefix, read back through prefixOf', () => {
    const roster = buildRoster([
      { ...comp('K02 Draw', 'running', [{ id: 'A-p1', name: 'Alice', dojo: 'Shibuya', number: 'K021' }]), numberPrefix: 'K02' },
      { ...comp('K Draw', 'running', [{ id: 'B-p1', name: 'Bob', dojo: 'Osaka', number: 'K21' }]), numberPrefix: ' K ' },
    ]);
    expect(prefixOf(roster[0])).toBe('K02');
    expect(prefixOf(roster[1]), 'trimmed, as the number was minted from the trimmed value').toBe('K');
    expect(roster.filter((p) => matchesCompetitorNumber(p, 'k02')).map((p) => p.number)).toEqual(['K021']);
    expect(roster.filter((p) => matchesCompetitorNumber(p, 'k')).map((p) => p.number)).toEqual(['K021', 'K21']);
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

// The other record kind: a MATCH SIDE. normalizeMatch resolves a side off the
// map buildPlayerMap builds from the competition, so the prefix has to ride
// that map or the schedule's free-text arm (matchMentions) would answer
// "k02" differently from the picker beside it.
describe('a resolved match side carries the prefix too', () => {
  const comp = {
    id: 'k02', numberPrefix: 'K02',
    players: [
      { id: 'p1', name: 'Alice', dojo: 'Shibuya', number: 'K021' },
      { id: 'p2', name: 'Bob', dojo: 'Osaka', number: 'K022' },
    ],
  };

  it('buildPlayerMap stamps the competition prefix on every entry', () => {
    const map = buildPlayerMap(comp);
    expect(prefixOf(map['p1'])).toBe('K02');
    expect(prefixOf(map['Bob'])).toBe('K02');
  });

  it('so matchMentions selects the draw by its prefix and the competitor by the whole number', () => {
    const m = normalizeMatch({ id: 'm1', sideA: 'Alice', sideB: 'Bob', sideAId: 'p1', sideBId: 'p2', status: 'scheduled' }, buildPlayerMap(comp));
    expect(prefixOf(m.sideA)).toBe('K02');
    expect(matchMentions(m, 'k02')).toBe(true);
    expect(matchMentions(m, 'k021')).toBe(true);
    expect(matchMentions(m, 'k2'), '"k2" is neither this draw nor a whole number in it').toBe(false);
  });
});
