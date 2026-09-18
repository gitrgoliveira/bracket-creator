import { describe, it, expect } from 'vitest';
import { resultRecencyDesc, recentlyPlayed, mostRecentlyPlayed } from '../result_recency.jsx';

// One rule, two surfaces: the court console's context strip ("the bout just
// played") and the public viewer's Recent results both order completed bouts
// through this leaf, so they can never name different bouts as the latest
// (mp-jnvl).
describe('resultRecencyDesc; newest result first', () => {
  const at = (id, scheduledAt, modifiedAt) => ({ id, scheduledAt, ...(modifiedAt ? { modifiedAt } : {}) });

  it('puts the newer write first even when it was scheduled earlier', () => {
    const rows = [at('late', '09:05', 1000), at('early', '09:00', 2000)];
    expect([...rows].sort(resultRecencyDesc).map((m) => m.id)).toEqual(['early', 'late']);
  });

  it('puts a stamped bout ahead of an unstamped one scheduled later', () => {
    const rows = [at('unstamped', '09:30'), at('stamped', '09:00', 1000)];
    expect([...rows].sort(resultRecencyDesc).map((m) => m.id)).toEqual(['stamped', 'unstamped']);
  });

  it('falls back to scheduled time, latest first, when nothing is stamped', () => {
    const rows = [at('a', '09:00'), at('c', '09:30'), at('b', '09:15')];
    expect([...rows].sort(resultRecencyDesc).map((m) => m.id)).toEqual(['c', 'b', 'a']);
  });

  it('is total on missing fields, so a drifted row cannot throw', () => {
    expect(() => [{}, { scheduledAt: '09:00' }, { modifiedAt: 5 }].sort(resultRecencyDesc)).not.toThrow();
  });
});


// The context/standings strip anchors to "the bout just played" once the court
// has nothing running. `completed` is ordered by SCHEDULED time, so its tail is
// the wrong bout whenever the court ran out of schedule order - the operator
// scores a late-slot bout early, then an earlier one, and the strip silently
// stays on the late-slot bout (mp-jnvl).
describe('mostRecentlyPlayed; recency comes from the result write, not the slot', () => {
  // Shaped like the real list: sorted by scheduledAt, so `late` is the tail.
  const early = { id: 'm1', scheduledAt: '09:00', status: 'completed' };
  const late = { id: 'm2', scheduledAt: '09:05', status: 'completed' };

  it('picks the bout written last even when it was scheduled first', () => {
    const played = [{ ...early, modifiedAt: 2000 }, { ...late, modifiedAt: 1000 }];
    expect(mostRecentlyPlayed(played).id).toBe('m1');
  });

  it('still picks the tail when that is genuinely the last write', () => {
    const played = [{ ...early, modifiedAt: 1000 }, { ...late, modifiedAt: 2000 }];
    expect(mostRecentlyPlayed(played).id).toBe('m2');
  });

  // Unstamped paths are real: quick-score, /decision and the daihyosen writes
  // build their result without a modifiedAt, as do files predating the stamp.
  it('falls back to schedule order when nothing carries a stamp', () => {
    expect(mostRecentlyPlayed([early, late]).id).toBe('m2');
  });

  it('ignores unstamped bouts when any bout is stamped', () => {
    const played = [{ ...early, modifiedAt: 1000 }, late];
    expect(mostRecentlyPlayed(played).id).toBe('m1');
  });

  it('treats a zero stamp as unstamped rather than as the epoch', () => {
    // A 0 must not read as "written in 1970 and therefore oldest-but-ranked":
    // it means no stamp at all, so the STAMPED bout wins even though 0 < 1000
    // would order the same way, and the unstamped one still beats an unstamped
    // bout in an earlier slot.
    expect(mostRecentlyPlayed([{ ...late, modifiedAt: 0 }, { ...early, modifiedAt: 1000 }]).id).toBe('m1');
    expect(mostRecentlyPlayed([{ ...early, modifiedAt: 0 }, { ...late }]).id).toBe('m2');
  });

  it('breaks a stamp tie towards the later slot, matching the tail fallback', () => {
    const played = [{ ...early, modifiedAt: 5000 }, { ...late, modifiedAt: 5000 }];
    expect(mostRecentlyPlayed(played).id).toBe('m2');
  });

  it('returns null for an empty list', () => {
    expect(mostRecentlyPlayed([])).toBeNull();
  });

  // The reachable unstamped case is a match that was NEVER started: the
  // withdrawal panel lists only `scheduled` matches, and the default win it
  // awards sends no stamp, so nothing is inherited from a start write. (A bye
  // is NOT such a case - hasBothSides strips byes before this list is built.)
  it('ranks a never-started default win below a bout with a write stamp', () => {
    const defaultWin = { id: 'fusenpai', scheduledAt: '09:05', status: 'completed' };
    const fought = { id: 'm1', scheduledAt: '09:00', status: 'completed', modifiedAt: 2000 };
    expect(mostRecentlyPlayed([fought, defaultWin]).id).toBe('m1');
  });
});


// The Completed preview shows only the last few bouts to keep the live queue
// above the fold. "Last few" has to mean most recently PLAYED: picking the tail
// of a schedule-ordered list hides the result the operator just entered (mp-jnvl).
describe('recentlyPlayed; the preview keeps the newest results, in list order', () => {
  const bout = (id, scheduledAt, modifiedAt) => ({ id, scheduledAt, status: 'completed', ...(modifiedAt ? { modifiedAt } : {}) });

  it('keeps the newest results rather than the last slots', () => {
    const list = [bout('a', '09:00', 4000), bout('b', '09:05', 1000), bout('c', '09:10', 2000)];
    expect(recentlyPlayed(list, 2).map((m) => m.id)).toEqual(['a', 'c']);
  });

  it('returns the kept rows in the caller\'s order, not in recency order', () => {
    const list = [bout('a', '09:00', 4000), bout('b', '09:05', 1000), bout('c', '09:10', 2000)];
    // 'a' is the most recent write but still renders first: only WHICH bouts
    // are shown changes, never the order the section reads in. Asserted
    // positively - a "not ['c','a']" check passes for every wrong ordering too.
    expect(recentlyPlayed(list, 2).map((m) => m.id)).toEqual(['a', 'c']);
  });

  it('falls back to the last slots when nothing carries a stamp', () => {
    const list = [bout('a', '09:00'), bout('b', '09:05'), bout('c', '09:10')];
    expect(recentlyPlayed(list, 2).map((m) => m.id)).toEqual(['b', 'c']);
  });

  it('returns the whole list untouched when it is no longer than the preview', () => {
    const list = [bout('a', '09:00'), bout('b', '09:05')];
    expect(recentlyPlayed(list, 8)).toBe(list);
    expect(recentlyPlayed([], 8)).toEqual([]);
  });
});
