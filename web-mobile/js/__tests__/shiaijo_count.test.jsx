import { describe, it, expect } from 'vitest';
import {
  shiaijoCountError,
  shiaijoCountHint,
  shiaijoVenueHint,
  shiaijoVenueSplitExample,
  allowedShiaijoCounts,
  formatDrawsBracket,
  competitionDrawBlockedReason,
  partitionStartableCompetitions,
  VALID_SHIAIJO_COUNTS,
  MAX_COURTS,
} from '../admin_helpers.jsx';

// Mirrors internal/helper/shiaijo_count_test.go. A competition whose draw builds
// a knockout bracket runs on a POWER OF TWO number of shiaijo: the draw gives
// each shiaijo its own block of the bracket and merges those blocks in PAIRS,
// so the count has to halve cleanly all the way down. 6 halves to 3 and stops.
//
// The rule is per COMPETITION. A venue may have any number of shiaijo (3, 5, 7
// are all legal); a competition on a 3-shiaijo venue simply runs on 1 or 2.
describe('shiaijoCountError', () => {
  const cases = [
    { n: 1, valid: true },  // a single-shiaijo competition is explicitly allowed
    { n: 2, valid: true },
    { n: 3, valid: false, below: 2, above: 4 },
    { n: 4, valid: true },
    { n: 5, valid: false, below: 4, above: 8 },
    // 6 was VALID under the previous "1 or an even number" rule. It is the
    // case that proves this file tracks the power-of-two rule and not parity.
    { n: 6, valid: false, below: 4, above: 8 },
    { n: 7, valid: false, below: 4, above: 8 },
    { n: 8, valid: true },
    { n: 9, valid: false, below: 8, above: 16 },
    { n: 10, valid: false, below: 8, above: 16 },
    { n: 12, valid: false, below: 8, above: 16 },
    { n: 16, valid: true },
  ];

  cases.forEach(({ n, valid, below, above }) => {
    it(`${valid ? 'accepts' : 'rejects'} ${n} shiaijo`, () => {
      const err = shiaijoCountError(n);
      if (valid) {
        expect(err).toBeNull();
      } else {
        expect(err).toContain(`${n} shiaijo cannot be paired down to a single bracket`);
        // Names the nearest valid counts either side, and always offers 1.
        expect(err).toContain(`Use ${below} or ${above}, or 1`);
        // The canonical reason, shared with the Go message and the docs.
        expect(err).toContain('each shiaijo its own block of the bracket');
        expect(err).toContain('merge in pairs');
        expect(err).toContain('halve cleanly');
      }
    });
  });

  it('never reads as "at least 2 shiaijo"', () => {
    // A 1-shiaijo competition is legal, so the message must offer 1 rather
    // than stating a minimum. Same pin as the Go side.
    const err = shiaijoCountError(6).toLowerCase();
    expect(err).toContain(', or 1');
    expect(err).not.toContain('at least 2');
    expect(err).not.toContain('at least two');
  });

  it('offers only the count below once past the ceiling', () => {
    // 32 shiaijo is past the court cap, so there is no higher
    // valid count to suggest: the message must not invent one.
    const err = shiaijoCountError(20);
    expect(err).toContain('Use 16, or 1');
    expect(err).not.toContain('32');
  });

  it('stays silent for non-counts and empty allocations', () => {
    // 0 means "inherit the tournament's courts" and is resolved server-side;
    // NaN comes from a not-yet-loaded competition object.
    expect(shiaijoCountError(0)).toBeNull();
    expect(shiaijoCountError(NaN)).toBeNull();
    expect(shiaijoCountError(undefined)).toBeNull();
  });

  it('derives the valid counts from the court cap', () => {
    // 16 is the ceiling because 32 exceeds MAX_COURTS, not because it was
    // typed into a list somewhere.
    expect(VALID_SHIAIJO_COUNTS).toEqual([1, 2, 4, 8, 16]);
    expect(VALID_SHIAIJO_COUNTS[VALID_SHIAIJO_COUNTS.length - 1] * 2).toBeGreaterThan(MAX_COURTS);
  });
});

// The message must never name a count the operator's hall cannot supply.
// shiaijoCountError appears ALONE on four surfaces (dashboard card, "Start
// all" picker, competition header, overview checklist); on a 3-shiaijo venue
// the venue-agnostic wording told the operator to "use 2 or 4".
describe('shiaijoCountError: venue-aware options', () => {
  it('offers only counts a 3-shiaijo venue can actually supply', () => {
    const err = shiaijoCountError(3, 3);
    expect(err).toContain('3 shiaijo cannot be paired down to a single bracket');
    expect(err).toContain('This tournament has 3, so this competition can use 1 or 2');
    // The whole point: no 4 anywhere in the sentence.
    expect(err).not.toContain('4');
  });

  it('names the venue first, so the message is not read as a verdict on the hall', () => {
    // "This tournament has 3" before "this competition can use 1 or 2": the
    // hall is acknowledged as fine, and only the competition's slice is ruled on.
    const err = shiaijoCountError(3, 3);
    expect(err.indexOf('This tournament has 3')).toBeLessThan(err.indexOf('this competition can use'));
  });

  it('grows the offer with the venue', () => {
    expect(shiaijoCountError(3, 5)).toContain('This tournament has 5, so this competition can use 1, 2 or 4');
    expect(shiaijoCountError(6, 7)).toContain('This tournament has 7, so this competition can use 1, 2 or 4');
    expect(shiaijoCountError(10, 12)).toContain('This tournament has 12, so this competition can use 1, 2, 4 or 8');
  });

  it('is unchanged when the venue constrains nothing', () => {
    // 16+ shiaijo can supply every legal count, so the nearest-counts phrasing
    // is right and a venue clause would be noise. This is also the shape the
    // two FORMS use (they pass no venue at all), where shiaijoCountHint sits
    // directly beneath and supplies the venue view.
    const bare = shiaijoCountError(3);
    expect(shiaijoCountError(3, 16)).toBe(bare);
    expect(shiaijoCountError(3, 0)).toBe(bare);
    expect(shiaijoCountError(3, undefined)).toBe(bare);
    expect(bare).toContain('Use 2 or 4, or 1');
  });

  it('still carries the canonical reason in the venue-aware form', () => {
    const err = shiaijoCountError(3, 3);
    expect(err).toContain('each shiaijo its own block of the bracket');
    expect(err).toContain('merge in pairs');
    expect(err).toContain('halve cleanly');
  });

  it('never reads as a minimum, whatever the venue', () => {
    [3, 5, 6, 7, 9, 10, 12].forEach((venue) => {
      const err = shiaijoCountError(venue, venue).toLowerCase();
      expect(err).toContain('can use 1');
      expect(err).not.toContain('at least 2');
    });
  });

  it('stays silent for a valid count regardless of venue', () => {
    expect(shiaijoCountError(2, 3)).toBeNull();
    expect(shiaijoCountError(1, 3)).toBeNull();
  });
});

// allowedShiaijoCounts is the one primitive behind every venue-aware surface,
// so a venue can never be narrowed on one screen and not another.
describe('allowedShiaijoCounts', () => {
  it('narrows the list to what the venue holds', () => {
    expect(allowedShiaijoCounts(3)).toEqual({ venue: 3, allowed: [1, 2], constrained: true });
    expect(allowedShiaijoCounts(5)).toEqual({ venue: 5, allowed: [1, 2, 4], constrained: true });
  });

  it('treats a missing venue as "not loaded", not "has none"', () => {
    expect(allowedShiaijoCounts(0)).toEqual({ venue: 0, allowed: VALID_SHIAIJO_COUNTS, constrained: false });
    expect(allowedShiaijoCounts(undefined).allowed).toEqual(VALID_SHIAIJO_COUNTS);
    expect(allowedShiaijoCounts(NaN).allowed).toEqual(VALID_SHIAIJO_COUNTS);
  });

  it('is unconstrained once the venue can supply every legal count', () => {
    expect(allowedShiaijoCounts(16).constrained).toBe(false);
    expect(allowedShiaijoCounts(26).constrained).toBe(false);
  });
});

// The reassurance the operator guide gets right and no UI string carried: the
// venue is fine, and the odd shiaijo does not stand idle.
describe('shiaijoVenueSplitExample', () => {
  it('shows two competitions covering a 3-shiaijo venue exactly', () => {
    expect(shiaijoVenueSplitExample(3)).toBe(
      'With 3 shiaijo you can run one competition on 2 and another on the remaining 1 at the same time, so all 3 stay busy.'
    );
  });

  it('picks the largest first block that leaves a legal remainder', () => {
    expect(shiaijoVenueSplitExample(5)).toContain('one competition on 4 and another on the remaining 1');
    expect(shiaijoVenueSplitExample(6)).toContain('one competition on 4 and another on the remaining 2');
    expect(shiaijoVenueSplitExample(12)).toContain('one competition on 8 and another on the remaining 4');
    expect(shiaijoVenueSplitExample(20)).toContain('one competition on 16 and another on the remaining 4');
  });

  it('offers nothing when no exact two-way split exists', () => {
    // 7 is 4 + 3 and 3 is not a legal allocation, so one shiaijo really would
    // be left over. Claiming "all 7 stay busy" would be false advice.
    expect(shiaijoVenueSplitExample(7)).toBeNull();
    expect(shiaijoVenueSplitExample(11)).toBeNull();
    expect(shiaijoVenueSplitExample(19)).toBeNull();
  });

  it('offers nothing for a venue that is itself a legal count', () => {
    [1, 2, 4, 8, 16].forEach((v) => expect(shiaijoVenueSplitExample(v)).toBeNull());
  });

  it('never proposes a split a competition could not use', () => {
    for (let venue = 1; venue <= MAX_COURTS; venue++) {
      const sentence = shiaijoVenueSplitExample(venue);
      if (!sentence) continue;
      const [, a, b] = sentence.match(/on (\d+) and another on the remaining (\d+)/);
      expect(shiaijoCountError(Number(a))).toBeNull();
      expect(shiaijoCountError(Number(b))).toBeNull();
      expect(Number(a) + Number(b)).toBe(venue);
    }
  });
});

// The tournament-level venue field: the FIRST place a shiaijo count is typed,
// and the one that said nothing about the rule at all, so an organiser typed 3
// and met the refusal two screens later.
describe('shiaijoVenueHint', () => {
  it('says any number is fine, then scopes the rule to each competition', () => {
    const hint = shiaijoVenueHint(3);
    expect(hint).toContain('Pick the number your venue actually has: any number is fine.');
    expect(hint).toContain('Each competition then runs on 1 or 2 of them.');
    expect(hint).toContain('This is a rule about each competition, never about your venue.');
  });

  it('shows the split that keeps every shiaijo busy', () => {
    expect(shiaijoVenueHint(3)).toContain(
      'With 3 shiaijo you can run one competition on 2 and another on the remaining 1 at the same time, so all 3 stay busy.'
    );
  });

  it('follows the number being typed rather than a worked example', () => {
    expect(shiaijoVenueHint(5)).toContain('runs on 1, 2 or 4 of them');
    expect(shiaijoVenueHint(5)).toContain('on 4 and another on the remaining 1');
    expect(shiaijoVenueHint(5)).not.toContain('With 3 shiaijo');
  });

  it('drops the split for a venue that is itself a legal count, keeping the scope clause', () => {
    const hint = shiaijoVenueHint(4);
    expect(hint).toContain('This is a rule about each competition, never about your venue.');
    expect(hint).not.toContain('at the same time');
  });

  it('survives a blank field', () => {
    // decideNumericUpdate stores NaN for an empty input.
    expect(shiaijoVenueHint(NaN)).toContain('Each competition then runs on 1, 2, 4, 8 or 16 of them.');
    expect(shiaijoVenueHint(NaN)).not.toContain('at the same time');
  });
});

// The STANDING hint: what this operator may pick and why, shown before any
// rejection. Venue-aware, because "a 3 shiaijo venue does not mean a
// competition should ever be able to use all of them" and the operator's real
// question at that field is "why can't I pick all three of my shiaijo".
describe('shiaijoCountHint', () => {
  it('names only the counts a 3-shiaijo tournament can actually use', () => {
    const hint = shiaijoCountHint(3);
    expect(hint).toContain('can use 1 or 2 shiaijo');
    expect(hint).toContain('this tournament has 3');
    expect(hint).not.toContain('4');
  });

  it('states the reason, so the rule is taught rather than only enforced', () => {
    expect(shiaijoCountHint(3)).toContain(
      'The knockout draw gives each shiaijo its own block of the bracket and the blocks merge in pairs, so the count has to halve cleanly.'
    );
  });

  it('drops the mechanism sentence when the error is already stating it', () => {
    const short = shiaijoCountHint(3, false);
    expect(short).toContain('can use 1 or 2 shiaijo');
    expect(short).not.toContain('halve cleanly');
  });

  it('grows the list with the venue', () => {
    expect(shiaijoCountHint(1)).toContain('can use 1 shiaijo (this tournament has 1)');
    expect(shiaijoCountHint(2)).toContain('can use 1 or 2 shiaijo (this tournament has 2)');
    expect(shiaijoCountHint(5)).toContain('can use 1, 2 or 4 shiaijo (this tournament has 5)');
    expect(shiaijoCountHint(9)).toContain('can use 1, 2, 4 or 8 shiaijo (this tournament has 9)');
  });

  it('drops the venue clause when the venue constrains nothing', () => {
    const hint = shiaijoCountHint(16);
    expect(hint).toContain('can use 1, 2, 4, 8 or 16 shiaijo.');
    expect(hint).not.toContain('this tournament has');
  });

  it('falls back to the full list for a not-yet-loaded tournament', () => {
    // 0 / undefined is "courts not loaded", not "the venue has none": the
    // hint must not invent a constraint out of a missing fetch.
    expect(shiaijoCountHint(0)).toContain('can use 1, 2, 4, 8 or 16 shiaijo.');
    expect(shiaijoCountHint(undefined)).toContain('can use 1, 2, 4, 8 or 16 shiaijo.');
  });

  it('always offers 1, so it can never read as "at least 2 shiaijo"', () => {
    [0, 1, 2, 3, 5, 7, 26].forEach((venue) => {
      expect(shiaijoCountHint(venue)).toContain('can use 1');
    });
  });

  // Every message about this rule led with a verdict about a number. An
  // organiser whose hall has exactly 3 shiaijo reads that as a verdict about
  // their hall. The reassurance the operator guide carries appeared in no UI
  // string at all.
  it('says the rule is about the competition, not the operator\'s venue', () => {
    expect(shiaijoCountHint(3)).toContain('This is a rule about each competition, never about your venue.');
  });

  it('answers "so one of my three shiaijo sits idle?" with the split', () => {
    expect(shiaijoCountHint(3)).toContain(
      'With 3 shiaijo you can run one competition on 2 and another on the remaining 1 at the same time, so all 3 stay busy.'
    );
  });

  it('keeps the reassurance when the mechanism sentence is dropped', () => {
    // The error directly above states the mechanism; it never states the
    // reassurance, so this is the only place the operator can read it.
    const short = shiaijoCountHint(3, false);
    expect(short).toContain('This is a rule about each competition, never about your venue.');
    expect(short).not.toContain('halve cleanly');
  });

  it('says nothing reassuring to a venue that never meets the rule', () => {
    // 4 shiaijo is itself a legal allocation: nothing here will ever be
    // refused, so there is no misreading to correct and no noise to add.
    [1, 2, 4, 8, 16].forEach((venue) => {
      expect(shiaijoCountHint(venue)).not.toContain('never about your venue');
    });
  });

  it('reassures a venue too big to be constrained but still not a legal count', () => {
    // 20 shiaijo can supply every legal count, so the head has no venue
    // clause; the organiser still typed a number the rule will refuse.
    const hint = shiaijoCountHint(20);
    expect(hint).not.toContain('this tournament has');
    expect(hint).toContain('This is a rule about each competition, never about your venue.');
    expect(hint).toContain('one competition on 16 and another on the remaining 4');
  });
});

describe('formatDrawsBracket', () => {
  // Mirrors engine.CompetitionDrawsBracket: league and Swiss courts are
  // independent parallel courts with no bracket blocks to merge, so the rule is not applied
  // to them. The unset format IS in scope: the engine's draw pipeline builds a
  // standalone playoffs bracket for it.
  it.each([
    ['mixed', true],
    ['playoffs', true],
    ['', true],
    ['league', false],
    ['swiss', false],
  ])('format %s draws a bracket: %s', (format, expected) => {
    expect(formatDrawsBracket(format)).toBe(expected);
  });

  it('leaves a league allocation the rule would reject unflagged', () => {
    // The concrete case: the league court hint recommends
    // floor(players/2)-1 courts, which is 3 for 8 players. A format-blind
    // rule would reject the app's own recommendation.
    const suggestedForEight = Math.max(1, Math.floor(8 / 2) - 1);
    expect(suggestedForEight).toBe(3);
    expect(shiaijoCountError(suggestedForEight)).not.toBeNull();
    expect(formatDrawsBracket('league')).toBe(false);
  });
});

// The ONE derived predicate behind three surfaces: the competition header's
// Generate/Start buttons, the dashboard card's "Start competition →" and the
// tournament-level "Start all" picker. Before it was lifted, only the first
// consulted the rule.
describe('competitionDrawBlockedReason', () => {
  const comp = (over = {}) => ({ id: 'c1', name: 'Mudansha', format: 'mixed', courts: ['A', 'B'], ...over });

  it('passes a valid allocation', () => {
    expect(competitionDrawBlockedReason(comp(), ['A', 'B', 'C'])).toBeNull();
  });

  it('blocks a count the draw cannot halve, with the rule as the reason', () => {
    const reason = competitionDrawBlockedReason(comp({ courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(reason).toContain('3 shiaijo cannot be paired down to a single bracket');
    // VENUE-AWARE. Every surface that renders this reason renders it alone,
    // with no hint beneath to correct it, so on a 3-shiaijo venue it must not
    // offer the 4 that venue does not have.
    expect(reason).toContain('This tournament has 3, so this competition can use 1 or 2');
    expect(reason).not.toContain('4');
  });

  it('keeps offering the count above once the venue can supply it', () => {
    // Same competition, bigger hall: 4 is reachable, so the nearest-counts
    // phrasing is right and the venue clause would be noise.
    const courts = ['A', 'B', 'C'];
    const venue = ['A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P'];
    const reason = competitionDrawBlockedReason(comp({ courts }), venue);
    expect(reason).toContain('Use 2 or 4, or 1');
    expect(reason).not.toContain('This tournament has');
  });

  it('falls back to the venue-agnostic message before the tournament loads', () => {
    const reason = competitionDrawBlockedReason(comp({ courts: ['A', 'B', 'C'] }), undefined);
    expect(reason).toContain('Use 2 or 4, or 1');
  });

  it('blocks 6, which the previous parity rule allowed', () => {
    const courts = ['A', 'B', 'C', 'D', 'E', 'F'];
    expect(competitionDrawBlockedReason(comp({ courts }), courts)).toContain('6 shiaijo cannot be paired');
  });

  it('exempts league and Swiss from the count rule', () => {
    const courts = ['A', 'B', 'C'];
    expect(competitionDrawBlockedReason(comp({ format: 'league', courts }), courts)).toBeNull();
    expect(competitionDrawBlockedReason(comp({ format: 'swiss', courts }), courts)).toBeNull();
  });

  it('blocks a shiaijo the tournament no longer has, in EVERY format', () => {
    // Two courts, so the count rule is satisfied and only the orphan rule can
    // fire. League is included on purpose: a league match on a court with no
    // operator view is just as invisible as a bracket one.
    const reason = competitionDrawBlockedReason(comp({ format: 'league', courts: ['A', 'D'] }), ['A', 'B', 'C']);
    expect(reason).toContain('no longer part of this tournament');
  });

  it('is null for a missing competition', () => {
    expect(competitionDrawBlockedReason(null, ['A'])).toBeNull();
  });

  it('does not invent an orphan blocker before the tournament has loaded', () => {
    expect(competitionDrawBlockedReason(comp(), undefined)).toBeNull();
  });

  // An EMPTY allocation means "inherit the tournament's shiaijo", and the
  // engine validates the inherited count. Verified live against the running
  // server: POST generate-draw on a competition stored without a courts key,
  // on a 3-shiaijo venue, answers 400 "It has no shiaijo of its own, so the
  // draw would run on all 3 of the tournament's". The console still offered a
  // live "Start competition" for it, which is the exact defect this predicate
  // was lifted to prevent.
  it('blocks a competition with no shiaijo of its own on an illegal venue', () => {
    const reason = competitionDrawBlockedReason(comp({ courts: [] }), ['A', 'B', 'C']);
    expect(reason).toContain("no shiaijo of its own, so the draw would run on all 3 of the tournament's");
    expect(reason).toContain('This tournament has 3, so this competition can use 1 or 2');
  });

  it('treats a null courts list the same as an empty one', () => {
    // The record shape this whole screen is the documented remedy for: Go
    // ships a nil slice as JSON null.
    expect(competitionDrawBlockedReason(comp({ courts: null }), ['A', 'B', 'C']))
      .toContain('no shiaijo of its own');
  });

  it('allows inheriting a venue whose own count IS a legal allocation', () => {
    // Inheriting 2 of 2 is exactly what the server accepts; blocking it would
    // be a refusal the backend does not make.
    expect(competitionDrawBlockedReason(comp({ courts: [] }), ['A', 'B'])).toBeNull();
    expect(competitionDrawBlockedReason(comp({ courts: [] }), ['A'])).toBeNull();
  });

  it('says nothing about inheritance for league and Swiss', () => {
    // They have no bracket to merge, so inheriting all 3 is legal.
    expect(competitionDrawBlockedReason(comp({ format: 'league', courts: [] }), ['A', 'B', 'C'])).toBeNull();
    expect(competitionDrawBlockedReason(comp({ format: 'swiss', courts: [] }), ['A', 'B', 'C'])).toBeNull();
  });

  it('stays silent about inheritance before the tournament has loaded', () => {
    // venue 0 is "not fetched yet", not "the venue has no shiaijo".
    expect(competitionDrawBlockedReason(comp({ courts: [] }), undefined)).toBeNull();
    expect(competitionDrawBlockedReason(comp({ courts: [] }), [])).toBeNull();
  });
});

// What the dashboard's "Start all" is allowed to offer. It used to offer every
// competition in setup, including ones the server refuses.
describe('partitionStartableCompetitions', () => {
  const players = [{ id: 'p1' }, { id: 'p2' }];
  const c = (id, over = {}) => ({ id, name: id, status: 'setup', format: 'mixed', courts: ['A', 'B'], players, ...over });

  it('offers a valid competition', () => {
    const { startable, blocked } = partitionStartableCompetitions([c('a')], ['A', 'B', 'C']);
    expect(startable.map(x => x.id)).toEqual(['a']);
    expect(blocked).toEqual([]);
  });

  it('does NOT offer a competition the server would refuse', () => {
    const { startable, blocked } = partitionStartableCompetitions(
      [c('ok'), c('bad', { courts: ['A', 'B', 'C'] })], ['A', 'B', 'C']);
    expect(startable.map(x => x.id)).toEqual(['ok']);
    expect(blocked).toHaveLength(1);
    expect(blocked[0].comp.id).toBe('bad');
    expect(blocked[0].reason).toContain('3 shiaijo cannot be paired down to a single bracket');
    // The "Start all" modal prints this reason with nothing beneath it, so it
    // is venue-aware for the same reason the dashboard card's is.
    expect(blocked[0].reason).toContain('This tournament has 3, so this competition can use 1 or 2');
    expect(blocked[0].reason).not.toContain('4');
  });

  it('reports the blocked competition rather than dropping it', () => {
    // "Start all" must never quietly mean "start most": the operator has to
    // see which competition was left behind and why.
    const { startable, blocked } = partitionStartableCompetitions([c('bad', { courts: ['A', 'B', 'C'] })], ['A', 'B', 'C']);
    expect(startable).toEqual([]);
    expect(blocked.map(b => b.comp.id)).toEqual(['bad']);
  });

  it('ignores competitions that were never eligible', () => {
    // Already running, and too few participants: neither is a court problem,
    // so neither belongs in the blocked list.
    const { startable, blocked } = partitionStartableCompetitions([
      c('running', { status: 'pools' }),
      c('empty', { players: [] }),
      c('ready', { status: 'draw-ready' }),
    ], ['A', 'B', 'C']);
    expect(startable).toEqual([]);
    expect(blocked).toEqual([]);
  });

  it('survives a missing competitions list', () => {
    expect(partitionStartableCompetitions(undefined, ['A'])).toEqual({ startable: [], blocked: [] });
  });
});
