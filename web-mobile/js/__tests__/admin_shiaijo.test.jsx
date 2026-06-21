import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { parsePath, pathFromState } from '../app.jsx';
import { sortShiaijoMatches, partitionShiaijoMatches, shiaijoScoreCell, isTeamMatch } from '../admin_shiaijo.jsx';

// A team encounter's score must never be shown as a bare number — it always
// carries an IV (Individual Victories) label, since a raw figure could read as
// wins or points. Individual bouts show the self-explanatory ippon score.
describe('shiaijoScoreCell — team numbers are never context-free', () => {
  const orig = {};
  beforeEach(() => {
    orig.iv = window.teamIVScore; orig.fmt = window.formatIpponsScore; orig.ip = window.ipponsFromScore;
    window.teamIVScore = (m) => (m.subResults && m.subResults.length ? '2–1' : null);
    window.formatIpponsScore = () => 'M–K';
    window.ipponsFromScore = () => [];
  });
  afterEach(() => {
    window.teamIVScore = orig.iv; window.formatIpponsScore = orig.fmt; window.ipponsFromScore = orig.ip;
  });

  it('routes a completed team match to a labeled IV cell', () => {
    expect(shiaijoScoreCell({ status: 'completed', compKind: 'team', teamSize: 5, subResults: [{}] }))
      .toEqual({ kind: 'team', iv: '2–1' });
  });

  it('shows nothing (not a bare number) for a running team match with no decided bouts', () => {
    expect(shiaijoScoreCell({ status: 'running', teamSize: 5, subResults: [] }))
      .toEqual({ kind: 'none' });
  });

  it('routes an individual match to the self-explanatory ippon score', () => {
    expect(shiaijoScoreCell({ status: 'completed', teamSize: 0 }))
      .toEqual({ kind: 'ippon', ippon: 'M–K' });
  });

  it('shows "vs" for a scheduled match regardless of team size', () => {
    expect(shiaijoScoreCell({ status: 'scheduled', teamSize: 5 })).toEqual({ kind: 'vs' });
    expect(shiaijoScoreCell({ status: 'scheduled', teamSize: 0 })).toEqual({ kind: 'vs' });
  });
});

// mp-tidg: viewer competition tab routing — /competition/:id/:tab gives
// browser back/forward support across tabs (Overview / Pools / Bracket / etc).
describe('parsePath — /competition/:id/:tab', () => {
  it('parses a tab segment into viewerTab', () => {
    expect(parsePath('/competition/abc/bracket')).toEqual({
      mode: 'viewer', viewerCompId: 'abc', viewerTab: 'bracket',
    });
  });

  it('returns viewerTab null when no tab segment is present', () => {
    expect(parsePath('/competition/abc')).toEqual({
      mode: 'viewer', viewerCompId: 'abc', viewerTab: null,
    });
  });
});

describe('pathFromState — viewer competition tab round-trip', () => {
  it('emits bare /competition/:id for overview (default tab)', () => {
    expect(pathFromState('viewer', undefined, 'abc', {}, 'overview')).toBe('/competition/abc');
    expect(pathFromState('viewer', undefined, 'abc', {}, null)).toBe('/competition/abc');
  });

  it('appends the tab segment for non-overview tabs', () => {
    expect(pathFromState('viewer', undefined, 'abc', {}, 'bracket')).toBe('/competition/abc/bracket');
  });

  it('round-trips bracket tab through parsePath', () => {
    const url = pathFromState('viewer', undefined, 'abc', {}, 'bracket');
    expect(parsePath(url)).toEqual({
      mode: 'viewer', viewerCompId: 'abc', viewerTab: 'bracket',
    });
  });

  it('register takes precedence over vcid — no tab suffix', () => {
    expect(pathFromState('viewer', 'register', 'abc', {}, 'bracket')).toBe('/register/abc');
  });

  // mp-tidg: encode/decode symmetry — pathFromState encodeURIComponent's the
  // tab and parsePath safeDecode's it, so a URL-special tab id round-trips.
  // (Real tab ids are plain ASCII, so this is a defensive contract, not a
  // behaviour change for current tabs.)
  it('encodes a URL-special tab segment and round-trips it', () => {
    const url = pathFromState('viewer', undefined, 'abc', {}, 'a b');
    expect(url).toBe('/competition/abc/a%20b');
    expect(parsePath(url)).toEqual({
      mode: 'viewer', viewerCompId: 'abc', viewerTab: 'a b',
    });
  });
});

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


describe('isTeamMatch — gates the "Enter lineup" affordance', () => {
  it('true for team encounters (by compKind or teamSize)', () => {
    expect(isTeamMatch({ compKind: 'team' })).toBe(true);
    expect(isTeamMatch({ teamSize: 5 })).toBe(true);
  });
  it('false for individual bouts and missing matches', () => {
    expect(isTeamMatch({ compKind: 'individual', teamSize: 0 })).toBe(false);
    expect(isTeamMatch({})).toBe(false);
    expect(isTeamMatch(null)).toBe(false);
  });
});
