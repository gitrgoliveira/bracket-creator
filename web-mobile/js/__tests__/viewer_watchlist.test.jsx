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

    // The earliest is m8 (07:30): both p1 and p2 are watched.
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
    // see surfaced: treat `running` as upcoming, exclude only `completed`.
    const watched = [{ id: 'p1' }];
    const all = [
      { id: 'live', sideAId: 'p1', sideBId: 'x', status: 'running', scheduledAt: '09:00' },
      { id: 'done', sideAId: 'p1', sideBId: 'y', status: 'completed', scheduledAt: '08:00' },
    ];
    const upcoming = buildWatchlistUpcoming(watched, all);
    expect(upcoming.map((m) => m.id)).toEqual(['live']);
  });

  // mp-42rg: verifies the de-duplication contract: running matches in the
  // watched-upcoming list carry stable composite keys (compId:id) that
  // ViewerHome uses to filter them out of the global NOW section. Without this,
  // the same match appears 3× on a 375px viewport.
  it('running matches in upcoming have stable composite keys for global-NOW de-duplication', () => {
    const watched = [{ id: 'p1' }, { id: 'p2' }];
    const all = [
      { id: 'r1', compId: 'comp-A', sideAId: 'p1', sideBId: 'x', status: 'running', scheduledAt: '09:00' },
      { id: 'r2', compId: 'comp-A', sideAId: 'y', sideBId: 'z', status: 'running', scheduledAt: '09:05' },
      { id: 's1', compId: 'comp-A', sideAId: 'p2', sideBId: 'w', status: 'scheduled', scheduledAt: '10:00' },
    ];
    const upcoming = buildWatchlistUpcoming(watched, all);
    const upcomingKeys = new Set(upcoming.map((m) => `${m.compId}:${m.id}`));

    // r1 involves watched p1 → in upcoming, should be excluded from global NOW
    expect(upcomingKeys.has('comp-A:r1')).toBe(true);
    // r2 involves no watched player → not in upcoming, stays in global NOW
    expect(upcomingKeys.has('comp-A:r2')).toBe(false);

    // Simulates ViewerHome's globalRunning filter (composite key)
    const globalRunning = all.filter((m) => m.status === 'running' && !upcomingKeys.has(`${m.compId}:${m.id}`));
    expect(globalRunning.map((m) => m.id)).toEqual(['r2']);
  });

  it('cross-competition collision: same match id in different comps are not confused', () => {
    // "Pool A-0" is a common id; comp-B has its own unrelated running match with
    // the same id but a different compId: it must NOT be filtered out.
    const watched = [{ id: 'p1' }];
    const all = [
      { id: 'Pool A-0', compId: 'comp-A', sideAId: 'p1', sideBId: 'x', status: 'running', scheduledAt: '09:00' },
      { id: 'Pool A-0', compId: 'comp-B', sideAId: 'y', sideBId: 'z', status: 'running', scheduledAt: '09:00' },
    ];
    const upcoming = buildWatchlistUpcoming(watched, all);
    const upcomingKeys = new Set(upcoming.map((m) => `${m.compId}:${m.id}`));

    const globalRunning = all.filter((m) => m.status === 'running' && !upcomingKeys.has(`${m.compId}:${m.id}`));
    // comp-A match is watched → excluded; comp-B match has the same id but
    // a different compId → must remain visible in the global NOW section.
    expect(globalRunning).toHaveLength(1);
    expect(globalRunning[0].compId).toBe('comp-B');
  });

  // bc-pnum: a match side WITH an id must match only a
  // watched id, never falling through to a name hit. Watching Sato of Tokyo
  // must not also surface Sato of Osaka's (unrelated, real-id-carrying)
  // matches just because the names coincide.
  it('watching Sato of Tokyo does not also surface Sato of Osaka', () => {
    const watched = [{ id: 'sato-tokyo', name: 'Sato' }];
    const all = [
      { id: 'm1', sideA: { id: 'sato-osaka', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' }, status: 'scheduled', scheduledAt: '09:00' },
    ];
    expect(buildWatchlistUpcoming(watched, all)).toEqual([]);
  });

  it('an id-less watched entry still matches an id-less (unresolved bracket) side by name', () => {
    const watched = [{ id: '', name: 'Sato' }];
    const all = [
      { id: 'm1', sideA: { id: '', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' }, status: 'scheduled', scheduledAt: '09:00' },
    ];
    expect(buildWatchlistUpcoming(watched, all).map((m) => m.id)).toEqual(['m1']);
  });

  // bc-pnum (MEDIUM): the case the two tests above
  // don't cover. An id-CARRYING watched entry's name must never leak into
  // the name fallback: an id-less side that merely shares that entry's
  // display name is a mixed pair (the watched entry has a real id, this
  // side doesn't) and must not match, exactly like watching Sato of Tokyo
  // not surfacing an id-carrying Sato of Osaka above.
  it('an id-carrying watched entry does not match an id-less side that merely shares its name', () => {
    const watched = [{ id: 'sato-tokyo', name: 'Sato' }];
    const all = [
      { id: 'm1', sideA: { id: '', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' }, status: 'scheduled', scheduledAt: '09:00' },
    ];
    expect(buildWatchlistUpcoming(watched, all)).toEqual([]);
  });
});

// Operator ruling 2026-09-22: a watched competitor with no upcoming or current
// match contributes their LAST RESULT to this list instead of vanishing from
// it. The watchlist is how a reader follows a PERSON, not only a fixture.
describe('buildWatchlistUpcoming: a finished competitor shows their last result', () => {
  const done = (id, who, at, modifiedAt) => (
    { id, sideAId: who, sideBId: 'x', scheduledAt: at, status: 'completed', modifiedAt }
  );
  const soon = (id, who, at) => (
    { id, sideAId: who, sideBId: 'x', scheduledAt: at, status: 'scheduled' }
  );

  it('offers the last result when the competitor has nothing left to fight', () => {
    const out = buildWatchlistUpcoming([{ id: 'p1' }], [done('d1', 'p1', '09:00', 10)]);
    expect(out.map((m) => m.id)).toEqual(['d1']);
  });

  it('PER COMPETITOR: one still fighting does not suppress a finished one', () => {
    // THE distinction. A list-level "is it empty" test would show only p1's
    // upcoming match and drop p2 entirely, which is what used to happen.
    const out = buildWatchlistUpcoming(
      [{ id: 'p1' }, { id: 'p2' }],
      [soon('s1', 'p1', '11:00'), done('d2', 'p2', '09:00', 10)],
    );
    expect(out.map((m) => m.id)).toEqual(['s1', 'd2']);
  });

  it('a competitor still fighting contributes NO result, only what is ahead', () => {
    const out = buildWatchlistUpcoming(
      [{ id: 'p1' }],
      [soon('s1', 'p1', '11:00'), done('d1', 'p1', '09:00', 10)],
    );
    expect(out.map((m) => m.id)).toEqual(['s1']);
  });

  it('picks the most recent result by WRITE time, not by slot', () => {
    // resultRecencyDesc (result_recency.jsx): a court running out of schedule
    // order scores an earlier slot later, and that is the newer result.
    const out = buildWatchlistUpcoming(
      [{ id: 'p1' }],
      [done('late-slot', 'p1', '15:00', 100), done('scored-last', 'p1', '09:00', 900)],
    );
    expect(out.map((m) => m.id)).toEqual(['scored-last']);
  });

  it('results come AFTER what is still to be fought', () => {
    const out = buildWatchlistUpcoming(
      [{ id: 'p1' }, { id: 'p2' }],
      [done('d2', 'p2', '08:00', 10), soon('s1', 'p1', '11:00')],
    );
    expect(out.map((m) => m.id), 'upcoming first, then the result').toEqual(['s1', 'd2']);
  });

  it('a competitor with no matches at all contributes nothing', () => {
    const out = buildWatchlistUpcoming([{ id: 'p1' }, { id: 'ghost' }], [soon('s1', 'p1', '11:00')]);
    expect(out.map((m) => m.id)).toEqual(['s1']);
  });

  it('one shared result is not listed twice when both sides are watched', () => {
    const shared = { id: 'd', sideAId: 'p1', sideBId: 'p2', scheduledAt: '09:00', status: 'completed', modifiedAt: 10 };
    const out = buildWatchlistUpcoming([{ id: 'p1' }, { id: 'p2' }], [shared]);
    expect(out.map((m) => m.id)).toEqual(['d']);
  });

  // THE CAP. Order and survival are different questions, and answering only
  // the first left this whole feature switched off for the reader who needs it
  // most: appending the results and truncating meant a watched set with `max`
  // bouts still ahead kept none of them. A coach watching a dojo through round
  // one clears ten pending bouts immediately (measured on a 32-player draw:
  // fifteen). Operator ruling 2026-09-23, taken against the two rendered lists.
  describe('the cap reserves room for the results', () => {
    // n people, each with one pending match; plus one who is already out.
    const manyPending = (n) => Array.from({ length: n }, (_, i) =>
      soon('s' + i, 'p' + i, String(9 + i).padStart(2, '0') + ':00'));
    const watchedPending = (n) => Array.from({ length: n }, (_, i) => ({ id: 'p' + i }));

    it('keeps the result when the fixtures alone would fill the list', () => {
      const out = buildWatchlistUpcoming(
        [...watchedPending(10), { id: 'out1' }],
        [...manyPending(10), done('d1', 'out1', '08:00', 10)],
        10,
      );
      expect(out, 'still capped').toHaveLength(10);
      expect(out.map((m) => m.id), 'the result survives').toContain('d1');
      expect(out[out.length - 1].id, 'and it is last, after the fixtures').toBe('d1');
    });

    it('drops the FURTHEST-OUT fixture to make that room, never the nearest', () => {
      const out = buildWatchlistUpcoming(
        [...watchedPending(10), { id: 'out1' }],
        [...manyPending(10), done('d1', 'out1', '08:00', 10)],
        10,
      );
      const ids = out.map((m) => m.id);
      expect(ids, 'the nearest bout is never the one sacrificed').toContain('s0');
      expect(ids, 'the last fixture is').not.toContain('s9');
    });

    it('fixtures keep at least HALF the list when many competitors are out', () => {
      // The other side of the bound, and it needs more fixtures than the floor
      // allows or it pins nothing: with only two of them, reserving max-8=2
      // slots and reserving ceil(10/2)=5 both yield the same two rows. Eight
      // pending and eight out is where the floor is the only thing deciding.
      const outs = Array.from({ length: 8 }, (_, i) => ({ id: 'o' + i }));
      const results = outs.map((o, i) => done('d' + i, o.id, '08:00', 10 + i));
      const out = buildWatchlistUpcoming(
        [...watchedPending(8), ...outs],
        [...manyPending(8), ...results],
        10,
      );
      expect(out).toHaveLength(10);
      const pending = out.filter((m) => m.status !== 'completed');
      expect(pending.length, 'the panel does not become a results page').toBe(5);
      expect(out.filter((m) => m.status === 'completed').length).toBe(5);
    });

    it('changes nothing when everything already fits', () => {
      const out = buildWatchlistUpcoming(
        [{ id: 'p1' }, { id: 'out1' }],
        [soon('s1', 'p1', '11:00'), done('d1', 'out1', '08:00', 10)],
        10,
      );
      expect(out.map((m) => m.id)).toEqual(['s1', 'd1']);
    });
  });
});
