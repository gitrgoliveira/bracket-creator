// mp-xhaa: rate-limited secondary (non-primary) watched-match alert decision.
// Pure-function tests for computeSecondaryAlert : the anti-storm rule that
// keeps a coach watching a whole dojo from getting a banner per student.
import { describe, it, expect } from 'vitest';
import { computeSecondaryAlert } from '../viewer.jsx';

const COOLDOWN = 30000;
const empty = { seen: [], lastAt: 0 };

const upnext = (id) => ({ id, status: 'scheduled', queuePosition: 1, scheduledAt: '10:00' });
const running = (id) => ({ id, status: 'running' });

describe('computeSecondaryAlert', () => {
  it('fires for the first on-deck secondary match when cooled down', () => {
    const r = computeSecondaryAlert(empty, [upnext('m1')], 100000, COOLDOWN);
    expect(r.fire).toBe(true);
    expect(r.match.id).toBe('m1');
    expect(r.lastAt).toBe(100000);
    expect(r.seen).toEqual(['m1:upnext']);
  });

  it('does NOT re-fire for the same match on a later render (dedup by signature)', () => {
    const first = computeSecondaryAlert(empty, [upnext('m1')], 100000, COOLDOWN);
    const second = computeSecondaryAlert(first, [upnext('m1')], 100050, COOLDOWN);
    expect(second.fire).toBe(false);
    expect(second.match).toBeNull();
    expect(second.lastAt).toBe(100000); // unchanged
  });

  it('fires only ONE banner when several students go on-deck at once', () => {
    const r = computeSecondaryAlert(empty, [upnext('mZ'), upnext('mA'), upnext('mM')], 100000, COOLDOWN);
    expect(r.fire).toBe(true);
    // earliest-scheduled fresh match wins; all share 10:00, so it's a stable
    // pick of the first after sort : assert exactly one fired and the rest are
    // marked seen (suppressed).
    expect(r.seen.sort()).toEqual(['mA:upnext', 'mM:upnext', 'mZ:upnext'].sort());
  });

  it('suppresses a new match that arrives within the cooldown window', () => {
    const first = computeSecondaryAlert(empty, [upnext('m1')], 100000, COOLDOWN);
    // m2 appears 5s later : still within cooldown → suppressed, NOT marked seen
    const second = computeSecondaryAlert(first, [upnext('m1'), upnext('m2')], 105000, COOLDOWN);
    expect(second.fire).toBe(false);
    expect(second.seen).not.toContain('m2:upnext'); // left unseen so it can fire later
  });

  it('fires the suppressed match once the cooldown elapses', () => {
    const first = computeSecondaryAlert(empty, [upnext('m1')], 100000, COOLDOWN);
    const suppressed = computeSecondaryAlert(first, [upnext('m1'), upnext('m2')], 105000, COOLDOWN);
    // 31s after the first fire → cooled down again
    const third = computeSecondaryAlert(suppressed, [upnext('m1'), upnext('m2')], 131000, COOLDOWN);
    expect(third.fire).toBe(true);
    expect(third.match.id).toBe('m2');
  });

  it('lets a match alert again after it leaves and returns to on-deck', () => {
    const first = computeSecondaryAlert(empty, [upnext('m1')], 100000, COOLDOWN);
    // m1 leaves on-deck → pruned from seen
    const gone = computeSecondaryAlert(first, [], 100100, COOLDOWN);
    expect(gone.seen).toEqual([]);
    // m1 returns, well past cooldown → fires again
    const back = computeSecondaryAlert(gone, [upnext('m1')], 140000, COOLDOWN);
    expect(back.fire).toBe(true);
    expect(back.match.id).toBe('m1');
  });

  it('treats a status change (upnext → running) as a fresh signature', () => {
    const first = computeSecondaryAlert(empty, [upnext('m1')], 100000, COOLDOWN);
    const promoted = computeSecondaryAlert(first, [running('m1')], 140000, COOLDOWN);
    expect(promoted.fire).toBe(true);
    expect(promoted.seen).toEqual(['m1:running']);
  });

  it('does nothing for an empty on-deck list', () => {
    const r = computeSecondaryAlert(empty, [], 100000, COOLDOWN);
    expect(r.fire).toBe(false);
    expect(r.seen).toEqual([]);
  });

  it('tolerates a null/garbage previous state and on-deck argument', () => {
    expect(computeSecondaryAlert(null, null, 100000, COOLDOWN)).toEqual({ fire: false, match: null, seen: [], lastAt: 0 });
  });
});
