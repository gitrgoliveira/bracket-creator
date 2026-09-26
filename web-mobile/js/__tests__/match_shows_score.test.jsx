// bc-sbq: matchShowsScore (match_shows_score.jsx) is the one answer to "does a display
// show this match's recorded score". A match sent back to the queue keeps its
// score on the server but reads as NOT STARTED until it runs again (operator
// ruling 2026-09-26). The render suite pins each host
// (render/queued_match_reads_not_started.render.test.jsx); this pins the
// predicate itself and matchStateCell, the pool-list centre cell built on it.
import { describe, it, expect } from 'vitest';
import { matchShowsScore } from '../match_shows_score.jsx';
import { matchStateCell } from '../bracket.jsx';

describe('matchShowsScore', () => {
  it('shows the score of a running or a completed match', () => {
    expect(matchShowsScore({ status: 'running' })).toBe(true);
    expect(matchShowsScore({ status: 'completed' })).toBe(true);
  });

  it('shows no score for a scheduled match, even one that kept its score', () => {
    expect(matchShowsScore({ status: 'scheduled' })).toBe(false);
    expect(matchShowsScore({ status: 'scheduled', ipponsA: ['M'], hansokuB: 1, encho: { periodCount: 1 } })).toBe(false);
  });

  it('shows no score when there is no match or no status to go by', () => {
    expect(matchShowsScore(null)).toBe(false);
    expect(matchShowsScore(undefined)).toBe(false);
    expect(matchShowsScore({})).toBe(false);
  });
});

describe('matchStateCell: a queued match reads "vs"', () => {
  const kept = (status) => ({
    status, sideA: { name: 'Aoki' }, sideB: { name: 'Endo' },
    ipponsA: [], ipponsB: [], encho: { periodCount: 1 },
  });

  it('prints the plain "vs" for a scheduled match that kept an overtime', () => {
    expect(matchStateCell(kept('scheduled'))).toBe('vs');
  });

  it('prints the kept overtime once the match runs again', () => {
    expect(matchStateCell(kept('running'))).toBe('(E)');
  });
});
