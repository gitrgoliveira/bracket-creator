import { describe, it, expect } from 'vitest';
import { resultRecencyDesc } from '../result_recency.jsx';

// One rule, two surfaces: the court console orders its Completed list (and so
// the context strip's anchor, which is that list's tail) and the public viewer
// orders Recent results, both through this comparator, so they can never name
// different bouts as the latest (mp-jnvl).
describe('resultRecencyDesc; newest result first', () => {
  const at = (id, scheduledAt, modifiedAt) => ({ id, scheduledAt, ...(modifiedAt ? { modifiedAt } : {}) });

  it('puts the newer write first even when it was scheduled earlier', () => {
    const rows = [at('late', '09:05', 1000), at('early', '09:00', 2000)];
    expect([...rows].sort(resultRecencyDesc).map((m) => m.id)).toEqual(['early', 'late']);
  });

  // A stamp is positive evidence of when the result landed; a scheduled time is
  // only evidence of when someone intended to fight it.
  it('puts a stamped bout ahead of an unstamped one scheduled later', () => {
    const rows = [at('unstamped', '09:30'), at('stamped', '09:00', 1000)];
    expect([...rows].sort(resultRecencyDesc).map((m) => m.id)).toEqual(['stamped', 'unstamped']);
  });

  // The reachable unstamped case is a match that was NEVER started: a queue
  // row's Record default win closes a match still `scheduled`, which has no
  // earlier write to inherit a stamp from.
  it('ranks a never-started default win below a bout carrying a stamp', () => {
    const rows = [at('default-win', '09:30'), at('fought', '09:00', 5)];
    expect([...rows].sort(resultRecencyDesc).map((m) => m.id)).toEqual(['fought', 'default-win']);
  });

  it('treats a zero stamp as unstamped rather than as the epoch', () => {
    const rows = [{ id: 'zero', scheduledAt: '09:00', modifiedAt: 0 }, at('stamped', '09:30', 1000)];
    expect([...rows].sort(resultRecencyDesc).map((m) => m.id)).toEqual(['stamped', 'zero']);
  });

  it('falls back to scheduled time, latest first, when nothing is stamped', () => {
    const rows = [at('a', '09:00'), at('c', '09:30'), at('b', '09:15')];
    expect([...rows].sort(resultRecencyDesc).map((m) => m.id)).toEqual(['c', 'b', 'a']);
  });

  // A row missing a field is ordinary: an unstamped bout carries no modifiedAt,
  // and an untimed row (the ""-scheduledAt rows Skip produces) carries no
  // scheduledAt.
  it('orders rows with missing fields without throwing', () => {
    const rows = [{ id: 'bare' }, { id: 'timed', scheduledAt: '09:00' }, { id: 'stamped', modifiedAt: 5 }];
    expect(rows.sort(resultRecencyDesc).map((m) => m.id)).toEqual(['stamped', 'timed', 'bare']);
  });
});
