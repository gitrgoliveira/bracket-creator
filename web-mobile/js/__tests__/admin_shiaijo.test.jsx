import { describe, it, expect } from 'vitest';
import { parsePath, pathFromState } from '../app.jsx';
import { sortShiaijoMatches, partitionShiaijoMatches } from '../admin_shiaijo.jsx';

// Routing for the dedicated shiaijo operator view (mp-c2yr).
describe('parsePath — /admin/shiaijo/:court', () => {
  it('maps a court segment to the shiaijo admin kind', () => {
    expect(parsePath('/admin/shiaijo/A')).toEqual({
      mode: 'admin', admin: { kind: 'shiaijo', court: 'A' },
    });
  });

  it('percent-decodes a court label', () => {
    // "Court B" → "/admin/shiaijo/Court%20B"
    expect(parsePath('/admin/shiaijo/Court%20B')).toEqual({
      mode: 'admin', admin: { kind: 'shiaijo', court: 'Court B' },
    });
  });

  it('does NOT throw on malformed percent-encoding — falls back to the raw segment', () => {
    // decodeURIComponent('%E0') throws; parsePath runs in the popstate
    // handler with no try/catch, so a crash here would break back/forward
    // navigation. safeDecode swallows the error and keeps the raw value.
    expect(() => parsePath('/admin/shiaijo/%E0')).not.toThrow();
    expect(parsePath('/admin/shiaijo/%E0')).toEqual({
      mode: 'admin', admin: { kind: 'shiaijo', court: '%E0' },
    });
  });

  it('falls back to the dashboard when the court segment is missing', () => {
    expect(parsePath('/admin/shiaijo')).toEqual({
      mode: 'admin', admin: { kind: 'dashboard' },
    });
  });
});

describe('pathFromState — shiaijo round-trip', () => {
  const toPath = (court) =>
    pathFromState('admin', undefined, undefined, { kind: 'shiaijo', court });

  it('emits /admin/shiaijo/:court for a real court', () => {
    expect(toPath('A')).toBe('/admin/shiaijo/A');
  });

  it('encodes a court label with spaces', () => {
    expect(toPath('Court B')).toBe('/admin/shiaijo/Court%20B');
  });

  it('falls back to /admin (not /admin/shiaijo/) when court is empty', () => {
    // A blank court would otherwise emit an unroutable /admin/shiaijo/
    // that parsePath bounces back to the dashboard — a state↔URL mismatch.
    expect(toPath('')).toBe('/admin');
    expect(toPath(undefined)).toBe('/admin');
  });

  it('round-trips a normal court through parsePath', () => {
    const url = toPath('A');
    expect(parsePath(url)).toEqual({
      mode: 'admin', admin: { kind: 'shiaijo', court: 'A' },
    });
  });
});

// Match ordering + grouping for the operator sections.
describe('sortShiaijoMatches', () => {
  const m = (status, scheduledAt, id) => ({ status, scheduledAt, id });

  it('orders running → scheduled → completed', () => {
    const out = sortShiaijoMatches([
      m('completed', '09:00', 'c'),
      m('scheduled', '09:30', 's'),
      m('running', '09:15', 'r'),
    ]);
    expect(out.map((x) => x.id)).toEqual(['r', 's', 'c']);
  });

  it('breaks ties within a status by scheduled time', () => {
    const out = sortShiaijoMatches([
      m('scheduled', '10:00', 'late'),
      m('scheduled', '09:00', 'early'),
      m('scheduled', '09:30', 'mid'),
    ]);
    expect(out.map((x) => x.id)).toEqual(['early', 'mid', 'late']);
  });

  it('sorts untimed matches last within their group', () => {
    const out = sortShiaijoMatches([
      m('scheduled', null, 'untimed'),
      m('scheduled', '09:00', 'timed'),
    ]);
    expect(out.map((x) => x.id)).toEqual(['timed', 'untimed']);
  });

  it('does not mutate the input array', () => {
    const input = [m('completed', '09:00', 'c'), m('running', '09:15', 'r')];
    const before = input.map((x) => x.id);
    sortShiaijoMatches(input);
    expect(input.map((x) => x.id)).toEqual(before);
  });

  it('places an unknown status after the known ones', () => {
    const out = sortShiaijoMatches([
      m('mystery', '08:00', 'x'),
      m('running', '09:15', 'r'),
    ]);
    expect(out.map((x) => x.id)).toEqual(['r', 'x']);
  });
});

describe('partitionShiaijoMatches', () => {
  const m = (status, scheduledAt, id) => ({ status, scheduledAt, id });

  it('splits a mixed list into the three operator sections', () => {
    const { running, scheduled, completed, sorted } = partitionShiaijoMatches([
      m('completed', '09:00', 'c1'),
      m('running', '09:15', 'r1'),
      m('scheduled', '09:30', 's1'),
      m('scheduled', '09:20', 's2'),
    ]);
    expect(running.map((x) => x.id)).toEqual(['r1']);
    expect(scheduled.map((x) => x.id)).toEqual(['s2', 's1']); // time-ordered
    expect(completed.map((x) => x.id)).toEqual(['c1']);
    // sorted is the full running→scheduled→completed ordering used for
    // prev/next modal navigation.
    expect(sorted.map((x) => x.id)).toEqual(['r1', 's2', 's1', 'c1']);
  });

  it('returns empty groups for an empty input', () => {
    const out = partitionShiaijoMatches([]);
    expect(out).toEqual({ sorted: [], running: [], scheduled: [], completed: [] });
  });
});
