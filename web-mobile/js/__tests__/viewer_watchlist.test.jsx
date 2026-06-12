// Pure-logic tests for buildWatchlistUpcoming (FR-024).
// Slice 4 / T110: drives the "Watched matches" home section.
import { describe, it, expect } from 'vitest';
import { buildWatchlistUpcoming } from '../viewer.jsx';

describe('buildWatchlistUpcoming', () => {
  it('returns ≤6 upcoming matches sorted by time', () => {
    const watched = [{ id: 'p1' }, { id: 'p2' }, { id: 'p3' }];
    // Ten scheduled matches spread across watched + unrelated players.
    // Times intentionally out of order so the sort assertion is meaningful.
    const allMatches = [
      { id: 'm1', sideAId: 'p1', sideBId: 'xa', scheduledAt: '10:00', status: 'scheduled' },
      { id: 'm2', sideAId: 'xb', sideBId: 'p2', scheduledAt: '09:00', status: 'scheduled' },
      { id: 'm3', sideAId: 'p3', sideBId: 'xc', scheduledAt: '11:30', status: 'scheduled' },
      { id: 'm4', sideAId: 'p1', sideBId: 'xd', scheduledAt: '08:15', status: 'scheduled' },
      { id: 'm5', sideAId: 'xe', sideBId: 'xf', scheduledAt: '08:00', status: 'scheduled' },
      { id: 'm6', sideAId: 'p2', sideBId: 'xg', scheduledAt: '12:00', status: 'scheduled' },
      { id: 'm7', sideAId: 'xh', sideBId: 'p3', scheduledAt: '13:00', status: 'scheduled' },
      { id: 'm8', sideAId: 'p1', sideBId: 'p2', scheduledAt: '07:30', status: 'scheduled' },
      { id: 'm9', sideAId: 'p3', sideBId: 'xi', scheduledAt: '06:00', status: 'completed' },
      { id: 'm10', sideAId: 'p2', sideBId: 'xj', scheduledAt: '14:00', status: 'scheduled' },
    ];

    const upcoming = buildWatchlistUpcoming(watched, allMatches);
    expect(upcoming.length).toBeLessThanOrEqual(6);

    // Completed match m9 must be excluded.
    expect(upcoming.find((m) => m.id === 'm9')).toBeUndefined();
    // Unrelated match m5 must be excluded (no watched player on either side).
    expect(upcoming.find((m) => m.id === 'm5')).toBeUndefined();

    // Ascending sort by scheduledAt.
    const times = upcoming.map((m) => m.scheduledAt);
    const sorted = [...times].sort();
    expect(times).toEqual(sorted);

    // The earliest is m8 (07:30) — both p1 and p2 are watched.
    expect(upcoming[0].id).toBe('m8');
  });

  it('caps at 6 even when more matches involve watched players', () => {
    const watched = [{ id: 'p1' }];
    const all = Array.from({ length: 10 }, (_, i) => ({
      id: `m${i}`,
      sideAId: 'p1',
      sideBId: `o${i}`,
      scheduledAt: `0${i}:00`,
      status: 'scheduled',
    }));
    const upcoming = buildWatchlistUpcoming(watched, all);
    expect(upcoming).toHaveLength(6);
  });

  it('returns [] when watchlist is empty', () => {
    expect(buildWatchlistUpcoming([], [{ id: 'm1', sideAId: 'p1' }])).toEqual([]);
    expect(buildWatchlistUpcoming(null, [{ id: 'm1', sideAId: 'p1' }])).toEqual([]);
  });

  it('handles canonical sideA.id object shape (real API payload)', () => {
    const watched = [{ id: 'p1' }];
    const all = [
      { id: 'm1', sideA: { id: 'p1' }, sideB: { id: 'p2' }, scheduledAt: '10:00', status: 'scheduled' },
    ];
    const upcoming = buildWatchlistUpcoming(watched, all);
    expect(upcoming).toHaveLength(1);
    expect(upcoming[0].id).toBe('m1');
  });

  it('keeps `running` matches in the upcoming list', () => {
    // A watched player who is mid-match is exactly what a coach wants to
    // see surfaced — treat `running` as upcoming, exclude only `completed`.
    const watched = [{ id: 'p1' }];
    const all = [
      { id: 'live', sideAId: 'p1', sideBId: 'x', status: 'running', scheduledAt: '09:00' },
      { id: 'done', sideAId: 'p1', sideBId: 'y', status: 'completed', scheduledAt: '08:00' },
    ];
    const upcoming = buildWatchlistUpcoming(watched, all);
    expect(upcoming.map((m) => m.id)).toEqual(['live']);
  });

  // mp-42rg: verifies the de-duplication contract — running matches in the
  // watched-upcoming list carry stable `.id` values that the ViewerHome
  // component uses to filter them out of the global NOW section. Without this,
  // the same match appears 3× on a 375px viewport.
  it('running matches in upcoming have stable IDs for global-NOW de-duplication', () => {
    const watched = [{ id: 'p1' }, { id: 'p2' }];
    const all = [
      { id: 'r1', sideAId: 'p1', sideBId: 'x', status: 'running', scheduledAt: '09:00' },
      { id: 'r2', sideAId: 'y', sideBId: 'z', status: 'running', scheduledAt: '09:05' },
      { id: 's1', sideAId: 'p2', sideBId: 'w', status: 'scheduled', scheduledAt: '10:00' },
    ];
    const upcoming = buildWatchlistUpcoming(watched, all);
    const upcomingIds = new Set(upcoming.map((m) => m.id));

    // r1 involves watched p1 → in upcoming, should be excluded from global NOW
    expect(upcomingIds.has('r1')).toBe(true);
    // r2 involves no watched player → not in upcoming, stays in global NOW
    expect(upcomingIds.has('r2')).toBe(false);

    // Simulates ViewerHome's globalRunning filter
    const globalRunning = all.filter((m) => m.status === 'running' && !upcomingIds.has(m.id));
    expect(globalRunning.map((m) => m.id)).toEqual(['r2']);
  });
});
