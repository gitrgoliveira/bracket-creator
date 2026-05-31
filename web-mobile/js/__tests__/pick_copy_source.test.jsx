// mp-bkg: tests for pickCopySource — the "Copy from previous match"
// candidate-selection algorithm exported from admin_schedule.jsx.
//
// Contract:
//   pickCopySource(allMatches, currentMatchId, teamId, savedLineups)
//     → the best source Match, or null if no candidate.
//
// Sorting rule:
//   1. scheduledAt DESC (nulls last — unscheduled treated as least-recent)
//   2. court ASC
//   3. index in allMatches ASC (queue/sequence order)
//   4. matchId DESC

import { describe, it, expect } from 'vitest';
import { pickCopySource } from '../admin_schedule.jsx';

// Build a minimal match object.
function mkMatch(id, opts = {}) {
  const { sideAId = 'team-a', sideBId = 'team-b', scheduledAt = null, court = '', idx = 0 } = opts;
  return {
    id,
    sideA: { id: sideAId },
    sideB: { id: sideBId },
    scheduledAt,
    court,
    _idx: idx, // not used by pickCopySource; just for readability
  };
}

describe('pickCopySource', () => {
  const TEAM = 'team-a';
  const OTHER = 'team-b';
  const CURRENT = 'current-match';

  it('returns null when no candidates exist', () => {
    expect(pickCopySource([], CURRENT, TEAM, {})).toBeNull();
  });

  it('returns null when savedLineups is empty (no lineups saved)', () => {
    const m1 = mkMatch('m1', { sideAId: TEAM });
    expect(pickCopySource([m1], CURRENT, TEAM, {})).toBeNull();
  });

  it('returns null when only current match has a saved lineup', () => {
    const cur = mkMatch(CURRENT, { sideAId: TEAM });
    expect(pickCopySource([cur], CURRENT, TEAM, { [CURRENT]: { positions: { senpo: 'x' } } })).toBeNull();
  });

  it('ignores matches where the team is not involved', () => {
    const m1 = mkMatch('m1', { sideAId: 'other-team-1', sideBId: 'other-team-2' });
    expect(pickCopySource([m1], CURRENT, TEAM, { m1: { positions: {} } })).toBeNull();
  });

  it('picks the only candidate when exactly one exists', () => {
    const m1 = mkMatch('m1', { sideAId: TEAM });
    const savedLineups = { m1: { positions: { senpo: 'Alice' } } };
    const result = pickCopySource([m1], CURRENT, TEAM, savedLineups);
    expect(result?.id).toBe('m1');
  });

  it('works when team is sideBId (SHIRO side)', () => {
    const m1 = mkMatch('m1', { sideAId: OTHER, sideBId: TEAM });
    const saved = { m1: { positions: {} } };
    expect(pickCopySource([m1], CURRENT, TEAM, saved)?.id).toBe('m1');
  });

  it('REGRESSION: matches a name-keyed side when teamKeys = [uuid, name]', () => {
    // Real data path: a participant's id is a UUID, but the match side is
    // keyed by team NAME (api_serializers name-as-id fallback). Matching the
    // UUID alone found zero candidates, so copy-from-previous silently
    // no-opped. teamKeys carries BOTH keys so the name-keyed side resolves.
    const m1 = {
      id: 'm1',
      sideA: { id: 'Red Dojo', name: 'Red Dojo' },
      sideB: { id: 'Blue Dojo', name: 'Blue Dojo' },
      scheduledAt: '09:00',
      court: 'A',
    };
    const saved = { m1: { positions: { senpo: 'Aka Ichi' } } };
    expect(pickCopySource([m1], CURRENT, ['uuid-red-123', 'Red Dojo'], saved)?.id).toBe('m1');
    // UUID-only (the pre-fix behaviour) finds nothing — the side is name-keyed.
    expect(pickCopySource([m1], CURRENT, 'uuid-red-123', saved)).toBeNull();
  });

  describe('sort rule 1: scheduledAt DESC (most recent first)', () => {
    it('picks the match with the later scheduledAt', () => {
      const m1 = mkMatch('m1', { sideAId: TEAM, scheduledAt: '09:00' });
      const m2 = mkMatch('m2', { sideAId: TEAM, scheduledAt: '10:00' });
      const saved = { m1: { positions: {} }, m2: { positions: {} } };
      expect(pickCopySource([m1, m2], CURRENT, TEAM, saved)?.id).toBe('m2');
    });

    it('picks match with scheduledAt over one without (nulls last)', () => {
      const m1 = mkMatch('m1', { sideAId: TEAM, scheduledAt: '08:00' });
      const m2 = mkMatch('m2', { sideAId: TEAM, scheduledAt: null });
      const saved = { m1: { positions: {} }, m2: { positions: {} } };
      // null → "" which is lexicographically less than any time string, so in
      // DESC order m1 (08:00) > m2 ("") → m1 wins (nulls sort last).
      expect(pickCopySource([m1, m2], CURRENT, TEAM, saved)?.id).toBe('m1');
    });

    it('both nulls fall through to court tiebreak', () => {
      // Both have no scheduledAt → equal on rule 1 → proceed to court
      const m1 = mkMatch('m1', { sideAId: TEAM, scheduledAt: null, court: 'B' });
      const m2 = mkMatch('m2', { sideAId: TEAM, scheduledAt: null, court: 'A' });
      const saved = { m1: { positions: {} }, m2: { positions: {} } };
      // court ASC: 'A' < 'B' → m2 (court A) wins
      expect(pickCopySource([m1, m2], CURRENT, TEAM, saved)?.id).toBe('m2');
    });
  });

  describe('sort rule 2: court ASC (tiebreak)', () => {
    it('picks the match on the earlier court (A before B)', () => {
      const m1 = mkMatch('m1', { sideAId: TEAM, scheduledAt: '10:00', court: 'B' });
      const m2 = mkMatch('m2', { sideAId: TEAM, scheduledAt: '10:00', court: 'A' });
      const saved = { m1: { positions: {} }, m2: { positions: {} } };
      expect(pickCopySource([m1, m2], CURRENT, TEAM, saved)?.id).toBe('m2');
    });
  });

  describe('sort rule 3: index in allMatches ASC (queue order)', () => {
    it('picks the match appearing earlier in the array when time and court are equal', () => {
      const m1 = mkMatch('m1', { sideAId: TEAM, scheduledAt: '10:00', court: 'A' });
      const m2 = mkMatch('m2', { sideAId: TEAM, scheduledAt: '10:00', court: 'A' });
      const saved = { m1: { positions: {} }, m2: { positions: {} } };
      // m1 is at index 0, m2 at index 1 → m1 has lower index → m1 wins
      expect(pickCopySource([m1, m2], CURRENT, TEAM, saved)?.id).toBe('m1');
    });
  });

  // Note: sort rule 4 (matchId DESC) is a safety-net tiebreak for cases
  // where two distinct JS objects have identical scheduledAt, court, AND the
  // same indexOf position in allMatches. Because Array.indexOf returns the
  // first occurrence of each reference and each match object is distinct,
  // two different matches always have different indices — rule 3 always
  // resolves before rule 4. Rule 4 is therefore effectively unreachable in
  // practice and has no dedicated test.

  it('only considers matches with a non-empty savedLineups entry', () => {
    const m1 = mkMatch('m1', { sideAId: TEAM, scheduledAt: '11:00' }); // no saved lineup
    const m2 = mkMatch('m2', { sideAId: TEAM, scheduledAt: '09:00' }); // has saved lineup
    const saved = { m2: { positions: { senpo: 'Bob' } } };
    expect(pickCopySource([m1, m2], CURRENT, TEAM, saved)?.id).toBe('m2');
  });

  describe('"previous match" filter: excludes siblings scheduled after the current match', () => {
    // The button is "Copy from previous match": when the current match has a
    // time, only siblings at or before that time are candidates.
    const CUR_ID = 'cur';
    const mkCur = (scheduledAt) => ({ id: CUR_ID, sideA: { id: TEAM }, sideB: { id: OTHER }, scheduledAt, court: 'A' });

    it('excludes a later sibling and picks the earlier one', () => {
      const cur = mkCur('10:00');
      const earlier = mkMatch('earlier', { sideAId: TEAM, scheduledAt: '09:00' });
      const later = mkMatch('later', { sideAId: TEAM, scheduledAt: '11:00' });
      const saved = { earlier: { positions: { senpo: 'E' } }, later: { positions: { senpo: 'L' } } };
      // Without the filter, DESC sort would pick the later match (11:00).
      expect(pickCopySource([cur, earlier, later], CUR_ID, TEAM, saved)?.id).toBe('earlier');
    });

    it('returns null when the only saved sibling is later than the current match', () => {
      const cur = mkCur('10:00');
      const later = mkMatch('later', { sideAId: TEAM, scheduledAt: '11:00' });
      const saved = { later: { positions: { senpo: 'L' } } };
      expect(pickCopySource([cur, later], CUR_ID, TEAM, saved)).toBeNull();
    });

    it('does not restrict when the current match has no scheduled time', () => {
      const cur = mkCur(null);
      const later = mkMatch('later', { sideAId: TEAM, scheduledAt: '11:00' });
      const saved = { later: { positions: { senpo: 'L' } } };
      // No current time → any saved sibling is a valid source.
      expect(pickCopySource([cur, later], CUR_ID, TEAM, saved)?.id).toBe('later');
    });
  });
});
