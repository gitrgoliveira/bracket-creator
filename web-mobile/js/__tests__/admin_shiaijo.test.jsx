import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { parsePath, pathFromState } from '../app.jsx';
import { sortShiaijoMatches, partitionShiaijoMatches, shiaijoScoreCell, isTeamMatch, groupQueueMatches, shiaijoStandingsKind, makeReconnectRefetcher, pendingFeederSlots, propagateBracketWinnerLocal } from '../admin_shiaijo.jsx';

// A team encounter's score must never be shown as a bare number; it always
// carries an IV (Individual Victories) label, since a raw figure could read as
// wins or points. Engi (flag-count scoring) is the ONLY competition type where
// the headline figure is a number at all, and it too carries an explicit
// label. Every other individual bout shows the self-explanatory ippon LETTERS.
describe('shiaijoScoreCell; team and engi numbers are never context-free', () => {
  const orig = {};
  beforeEach(() => {
    orig.iv = window.teamIVScore; orig.fmt = window.formatIpponsScore; orig.ip = window.ipponsFromScore; orig.engi = window.engiFlagScore;
    window.teamIVScore = (m) => (m.subResults && m.subResults.length ? '2–1' : null);
    window.formatIpponsScore = () => 'M–K';
    window.ipponsFromScore = () => [];
    window.engiFlagScore = (m) => (m.flagsA != null || m.flagsB != null ? `${m.flagsB || 0}–${m.flagsA || 0}` : null);
  });
  afterEach(() => {
    window.teamIVScore = orig.iv; window.formatIpponsScore = orig.fmt; window.ipponsFromScore = orig.ip; window.engiFlagScore = orig.engi;
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

  it('routes a completed engi match to a labeled flags cell, not a bare number', () => {
    expect(shiaijoScoreCell({ status: 'completed', teamSize: 0, flagsA: 2, flagsB: 3 }))
      .toEqual({ kind: 'engi', flags: '3–2' });
  });

  it('engi flags take priority over the ippon fallback when both are present', () => {
    expect(shiaijoScoreCell({ status: 'completed', teamSize: 0, flagsA: 1, flagsB: 0 }))
      .toEqual({ kind: 'engi', flags: '0–1' });
  });

  it('shows "vs" for a scheduled match regardless of team size', () => {
    expect(shiaijoScoreCell({ status: 'scheduled', teamSize: 5 })).toEqual({ kind: 'vs' });
    expect(shiaijoScoreCell({ status: 'scheduled', teamSize: 0 })).toEqual({ kind: 'vs' });
  });
});

// Standings ordering is decided by FORMAT, not surface (mp-ahu6): a league's
// court-side standings must render through the rank-ordered viewer, exactly
// like the public viewer and admin Pools tab, never the draw-order pool
// viewer. This pins the routing decision so it cannot silently regress.
describe('shiaijoStandingsKind; standings viewer routing follows format (mp-ahu6)', () => {
  it('routes a league match to the rank-ordered viewer', () => {
    expect(shiaijoStandingsKind({ compFormat: 'league' })).toBe('league');
  });

  it('routes mixed and playoffs matches to the draw-order viewer', () => {
    expect(shiaijoStandingsKind({ compFormat: 'mixed' })).toBe('pool');
    expect(shiaijoStandingsKind({ compFormat: 'playoffs' })).toBe('pool');
  });

  it('defaults to the draw-order viewer for a missing/unknown format', () => {
    expect(shiaijoStandingsKind({})).toBe('pool');
    expect(shiaijoStandingsKind(null)).toBe('pool');
  });
});

// The completed result is rendered on its own centred line BELOW the names
// (.shiaijo-qrow__result), NOT in the top-right state slot; so the (often long)
// names keep the full-width matchup line. The state slot carries only "Final".
describe('ShiaijoQueueRow; completed result placement', () => {
  const realReact = global.React;
  let runtime, ShiaijoQueueRow, orig = {};

  function walk(node, visit) {
    if (node == null || node === false || node === true) return;
    if (Array.isArray(node)) { for (const n of node) walk(n, visit); return; }
    if (typeof node !== 'object') return;
    visit(node);
    const kids = (node.children !== undefined && node.children !== null && !(Array.isArray(node.children) && !node.children.length))
      ? node.children : node.props?.children;
    walk(kids, visit);
  }
  const byClass = (tree, cls) => {
    const out = [];
    walk(tree, n => { if (n && typeof n.props?.className === 'string' && n.props.className.split(/\s+/).includes(cls)) out.push(n); });
    return out;
  };
  const text = (node) => {
    if (node == null) return '';
    if (typeof node === 'string' || typeof node === 'number') return String(node);
    if (Array.isArray(node)) return node.map(text).join('');
    if (node.children) return text(node.children);
    if (node.props?.children) return text(node.props.children);
    return '';
  };

  beforeEach(async () => {
    orig.fmt = window.formatIpponsScore; orig.ip = window.ipponsFromScore; orig.iv = window.teamIVScore;
    window.formatIpponsScore = () => '·;MK';
    window.ipponsFromScore = () => [];
    window.teamIVScore = () => null;
    runtime = makeReactive();
    global.React = runtime.React;
    ({ ShiaijoQueueRow } = await import('../admin_shiaijo.jsx'));
  });
  afterEach(() => {
    runtime.unmount(); global.React = realReact;
    window.formatIpponsScore = orig.fmt; window.ipponsFromScore = orig.ip; window.teamIVScore = orig.iv;
  });

  it('puts the score in a centred result line below the names, not the corner', () => {
    runtime.mount(ShiaijoQueueRow, {
      m: { id: 'm1', compId: 'c1', status: 'completed', teamSize: 0,
           sideA: { name: 'Mens Player 01' }, sideB: { name: 'Mens Player 03' } },
      scheduled: [], courts: ['A'],
    });
    const tree = runtime.currentTree();
    const result = byClass(tree, 'shiaijo-qrow__result');
    expect(result.length).toBe(1);
    expect(text(result[0])).toContain('·;MK');
    // The top-right state slot shows only "Final"; the score is NOT there.
    const state = byClass(tree, 'shiaijo-qrow__state');
    expect(state.length).toBe(1);
    expect(text(state[0])).toContain('Final');
    expect(text(state[0])).not.toContain('MK');
  });

  it('shows no result line for a scheduled match (centre stays vs)', () => {
    runtime.mount(ShiaijoQueueRow, {
      m: { id: 'm2', compId: 'c1', status: 'scheduled', teamSize: 0,
           sideA: { name: 'A' }, sideB: { name: 'B' } },
      scheduled: [], courts: ['A'],
    });
    const tree = runtime.currentTree();
    expect(byClass(tree, 'shiaijo-qrow__result').length).toBe(0);
    expect(byClass(tree, 'shiaijo-qrow__vs').length).toBe(1);
  });
});

// mp-tidg: viewer competition tab routing; /competition/:id/:tab gives
// browser back/forward support across tabs (Overview / Pools / Bracket / etc).
describe('parsePath; /competition/:id/:tab', () => {
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

  it('normalizes an explicit /overview segment to null (canonical default)', () => {
    expect(parsePath('/competition/abc/overview')).toEqual({
      mode: 'viewer', viewerCompId: 'abc', viewerTab: null,
    });
  });
});

describe('pathFromState; viewer competition tab round-trip', () => {
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

  it('register takes precedence over vcid; no tab suffix', () => {
    expect(pathFromState('viewer', 'register', 'abc', {}, 'bracket')).toBe('/register/abc');
  });

  // mp-tidg: encode/decode symmetry; pathFromState encodeURIComponent's the
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
describe('parsePath; /admin/shiaijo/:court', () => {
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

  it('does NOT throw on malformed percent-encoding; falls back to the raw segment', () => {
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

describe('pathFromState; shiaijo round-trip', () => {
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
    // that parsePath bounces back to the dashboard; a state↔URL mismatch.
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


// makeReconnectRefetcher gates a court re-sync on a genuine SSE reconnect (mp-y3nk
// Phase 2): a court whose tablet dropped offline may have missed events (e.g. a
// feeder resolved on another court), so on reconnect it must refetch to self-heal
// rather than wait for the next ordinary event. Crucially it must NOT refetch on
// the FIRST connect (that is the mount fetch's job) or it would double-fetch on
// every page load.
describe('makeReconnectRefetcher; refetch only on reconnect (open-after-error)', () => {
  it('does not refetch on the first open (initial connect)', () => {
    const spy = vi.fn();
    makeReconnectRefetcher(spy)('open');
    expect(spy).not.toHaveBeenCalled();
  });

  it('refetches on an open that follows an error (a real reconnect)', () => {
    const spy = vi.fn();
    const h = makeReconnectRefetcher(spy);
    h('error'); h('open');
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('collapses a burst of errors into a single refetch on reconnect', () => {
    const spy = vi.fn();
    const h = makeReconnectRefetcher(spy);
    h('error'); h('error'); h('open');
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('refetches again on each subsequent drop → reconnect cycle', () => {
    const spy = vi.fn();
    const h = makeReconnectRefetcher(spy);
    h('error'); h('open'); h('error'); h('open');
    expect(spy).toHaveBeenCalledTimes(2);
  });

  it('ignores a repeated open with no intervening error (no spurious refetch)', () => {
    const spy = vi.fn();
    const h = makeReconnectRefetcher(spy);
    h('error'); h('open'); h('open');
    expect(spy).toHaveBeenCalledTimes(1);
  });
});

// pendingFeederSlots maps a pending placeholder final to the feeder matches whose
// winners the operator can assert to make it runnable during an update outage
// (mp-y3nk Phase 3). "Winner of rX-mY" resolves POSITIONALLY, mirroring the Go
// parseWinnerOf: round index = rounds.length - X, match index = Y. Only sides
// that are still placeholders yield a slot; a feeder whose own sides are not yet
// resolved is returned as non-resolvable so the UI can disable it.
describe('pendingFeederSlots; feeders an operator can assert to resolve a final', () => {
  // 4-player bracket: rounds[0] = 2 semifinals, rounds[1] = the final. With
  // rounds.length === 2, the final's feeders are "Winner of r2-m0/m1".
  const makeRounds = () => ([
    [
      { id: 'm-r2-0', status: 'scheduled', sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' } },
      { id: 'm-r2-1', status: 'scheduled', sideA: { id: 'c', name: 'Carol' }, sideB: { id: 'd', name: 'Dan' } },
    ],
    [
      { id: 'm-r1-0', status: 'scheduled', sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' } },
    ],
  ]);

  it('returns a resolvable slot per placeholder side with the feeder competitors as options', () => {
    const rounds = makeRounds();
    const slots = pendingFeederSlots(rounds[1][0], rounds);
    expect(slots.length).toBe(2);
    expect(slots[0]).toMatchObject({ side: 'A', placeholder: 'Winner of r2-m0', resolvable: true });
    expect(slots[0].feeder.id).toBe('m-r2-0');
    expect(slots[0].options.map(o => o.name)).toEqual(['Alice', 'Bob']);
    expect(slots[1]).toMatchObject({ side: 'B', placeholder: 'Winner of r2-m1', resolvable: true });
    expect(slots[1].feeder.id).toBe('m-r2-1');
    expect(slots[1].options.map(o => o.name)).toEqual(['Carol', 'Dan']);
  });

  it('omits a side that is already resolved to a real competitor', () => {
    const rounds = makeRounds();
    rounds[1][0].sideA = { id: 'a', name: 'Alice' }; // r2-m0 already resolved
    const slots = pendingFeederSlots(rounds[1][0], rounds);
    expect(slots.length).toBe(1);
    expect(slots[0].placeholder).toBe('Winner of r2-m1');
  });

  it('marks a feeder whose own sides are still placeholders as non-resolvable (no options)', () => {
    const rounds = makeRounds();
    rounds[0][0].sideA = { id: '', name: 'Winner of r3-m0' }; // feeder itself unresolved
    const slots = pendingFeederSlots(rounds[1][0], rounds);
    expect(slots[0]).toMatchObject({ placeholder: 'Winner of r2-m0', resolvable: false });
    expect(slots[0].options).toEqual([]);
  });

  it('marks an out-of-range placeholder as non-resolvable with a null feeder', () => {
    const rounds = makeRounds();
    rounds[1][0].sideA = { id: '', name: 'Winner of r2-m9' }; // no such match index
    const slots = pendingFeederSlots(rounds[1][0], rounds);
    expect(slots[0]).toMatchObject({ placeholder: 'Winner of r2-m9', resolvable: false, feeder: null });
  });

  it('returns [] for a fully-resolved match and tolerates a missing bracket', () => {
    const rounds = makeRounds();
    rounds[1][0].sideA = { id: 'a', name: 'Alice' };
    rounds[1][0].sideB = { id: 'c', name: 'Carol' };
    expect(pendingFeederSlots(rounds[1][0], rounds)).toEqual([]);
    expect(pendingFeederSlots(rounds[1][0], null)).toEqual([]);
    expect(pendingFeederSlots(null, rounds)).toEqual([]);
  });
});

// propagateBracketWinnerLocal advances a winner into the next round's placeholder
// side ON THE CLIENT, so a court running fully offline (mp-y3nk offline console)
// can complete a bout and have the next match: including the final: become
// runnable without waiting for the server to propagate and the client to
// refetch. It mirrors the Go engine's propagateBracketWinner positional rule:
// a completed match at rounds[r][m] feeds rounds[r+1][floor(m/2)], filling sideA
// when m is even and sideB when m is odd. Pure + immutable (returns fresh rounds)
// so it is safe to drop into React state.
describe('propagateBracketWinnerLocal; offline bracket advancement', () => {
  const rounds = () => ([
    [
      { id: 'm-r2-0', status: 'scheduled', sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' } },
      { id: 'm-r2-1', status: 'scheduled', sideA: { id: 'c', name: 'Carol' }, sideB: { id: 'd', name: 'Dan' } },
    ],
    [
      { id: 'm-r1-0', status: 'scheduled', sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' } },
    ],
  ]);

  it('completes the match and fills the final\'s sideA from an even-index feeder', () => {
    const out = propagateBracketWinnerLocal(rounds(), 'm-r2-0', 'Alice');
    expect(out[0][0].status).toBe('completed');
    expect(out[0][0].winner).toMatchObject({ name: 'Alice' });
    expect(out[1][0].sideA).toMatchObject({ id: 'a', name: 'Alice' }); // carries id, not just name
    expect(out[1][0].sideB).toMatchObject({ name: 'Winner of r2-m1' });  // untouched
  });

  it('fills the final\'s sideB from an odd-index feeder', () => {
    const out = propagateBracketWinnerLocal(rounds(), 'm-r2-1', 'Dan');
    expect(out[1][0].sideB).toMatchObject({ id: 'd', name: 'Dan' });
    expect(out[1][0].sideA).toMatchObject({ name: 'Winner of r2-m0' });
  });

  it('marks the final startable once BOTH feeders have resolved', () => {
    let out = propagateBracketWinnerLocal(rounds(), 'm-r2-0', 'Alice');
    out = propagateBracketWinnerLocal(out, 'm-r2-1', 'Carol');
    expect(out[1][0].sideA).toMatchObject({ name: 'Alice' });
    expect(out[1][0].sideB).toMatchObject({ name: 'Carol' });
    expect(out[1][0].status).toBe('scheduled'); // both sides real → runnable
  });

  it('does not mutate the input (immutable)', () => {
    const input = rounds();
    const out = propagateBracketWinnerLocal(input, 'm-r2-0', 'Alice');
    expect(input[1][0].sideA).toMatchObject({ name: 'Winner of r2-m0' }); // original untouched
    expect(out).not.toBe(input);
  });

  it('completes a final with no downstream round and no error', () => {
    const r = rounds();
    r[1][0].sideA = { id: 'a', name: 'Alice' };
    r[1][0].sideB = { id: 'c', name: 'Carol' };
    const out = propagateBracketWinnerLocal(r, 'm-r1-0', 'Alice');
    expect(out[1][0].status).toBe('completed');
    expect(out[1][0].winner).toMatchObject({ name: 'Alice' });
  });

  it('returns the input unchanged for an unknown match id or missing rounds', () => {
    const r = rounds();
    expect(propagateBracketWinnerLocal(r, 'nope', 'X')).toBe(r);
    expect(propagateBracketWinnerLocal(null, 'm-r2-0', 'Alice')).toBe(null);
  });
});

describe('groupQueueMatches; Upcoming queue grouping', () => {
  const pool = (poolName, id) => ({ phase: 'pool', compFormat: 'mixed', poolName, id });
  const bracket = (round, roundIndex, id) => ({ phase: 'bracket', compFormat: 'mixed', round, roundIndex, id });

  it('returns null (flat) for a league competition; no grouping', () => {
    const matches = [
      { phase: 'pool', compFormat: 'league', poolName: 'League table', id: 'l1' },
      { phase: 'pool', compFormat: 'league', poolName: 'League table', id: 'l2' },
    ];
    expect(groupQueueMatches(matches)).toBeNull();
  });

  it('returns null for an empty list', () => {
    expect(groupQueueMatches([])).toBeNull();
  });

  it('groups pool matches by pool name in first-appearance order', () => {
    const groups = groupQueueMatches([
      pool('Pool A', 'a1'), pool('Pool A', 'a2'), pool('Pool B', 'b1'),
    ]);
    expect(groups.map((g) => g.label)).toEqual(['Pool A', 'Pool B']);
    expect(groups[0].matches.map((m) => m.id)).toEqual(['a1', 'a2']);
    expect(groups[1].matches.map((m) => m.id)).toEqual(['b1']);
  });

  it('groups playoff matches by round, keyed by round index', () => {
    const groups = groupQueueMatches([
      bracket('Semifinals', 0, 's1'), bracket('Semifinals', 0, 's2'), bracket('Final', 1, 'f1'),
    ]);
    expect(groups.map((g) => g.label)).toEqual(['Semifinals', 'Final']);
    expect(groups[0].matches.map((m) => m.id)).toEqual(['s1', 's2']);
    expect(groups[1].matches.map((m) => m.id)).toEqual(['f1']);
  });

  it('keeps pools and rounds as separate groups for a mixed comp mid-transition', () => {
    const groups = groupQueueMatches([
      pool('Pool A', 'a1'), bracket('Semifinals', 0, 's1'),
    ]);
    expect(groups.map((g) => g.label)).toEqual(['Pool A', 'Semifinals']);
  });
});


describe('isTeamMatch; gates the "Enter lineup" affordance', () => {
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
