// mp-xhaa: component-render tests for the unified Watchlist panel and its
// pieces. Uses the makeReactive shim (resetModules + dynamic import so the
// component's `const { useState } = React` destructure picks up the reactive
// stub). Child component vnodes (WatchHeroCard, WatchPicker, TermV) are NOT
// executed by the shim — they appear as {type, props} nodes — so panel tests
// assert structure + which child renders, and WatchHeroCard is mounted
// directly for its own inner-render assertions.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

const realReact = global.React;

// Concatenate all string/number leaves in a vnode tree.
function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children != null) return collectText(node.children);
  if (node.props?.children != null) return collectText(node.props.children);
  return '';
}

// Collect every vnode matching predicate (depth-first).
function findAll(node, pred, out = []) {
  if (!node || typeof node !== 'object') return out;
  if (Array.isArray(node)) { node.forEach((n) => findAll(n, pred, out)); return out; }
  if (pred(node)) out.push(node);
  const kids = node.children ?? node.props?.children;
  if (kids != null) [].concat(kids).forEach((k) => findAll(k, pred, out));
  return out;
}
const hasClass = (n, cls) => typeof n.props?.className === 'string' && n.props.className.split(/\s+/).includes(cls);
const byClass = (tree, cls) => findAll(tree, (n) => hasClass(n, cls));

const ROSTER = [
  { id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo', checkedIn: true },
  { id: 'p2', name: 'Nolan Clark', dojo: 'Tsubaki Kenyukai', checkedIn: false },
  { id: 'p3', name: 'Aoi Mori', dojo: 'Hagane Dojo', checkedIn: false },
];
const MATCH = {
  id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: '09:00',
  sideA: { id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' },
  sideB: { id: 'p2', name: 'Nolan Clark', dojo: 'Tsubaki Kenyukai' },
};

describe('WatchHeroCard', () => {
  let runtime, WatchHeroCard;
  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    // viewer.jsx grabs `const pluralize = window.pluralize` at module load.
    global.window.pluralize = (count, singular, plural) =>
      count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`;
    vi.resetModules();
    ({ WatchHeroCard } = await import('../viewer.jsx'));
  });
  afterEach(() => { runtime.unmount(); global.React = realReact; vi.resetModules(); });

  it('returns null when there is no match', () => {
    const tree = runtime.mount(WatchHeroCard, { nextMatch: null, primaryIds: new Set(), entityLabel: 'X', onMatchClick: vi.fn() });
    expect(tree).toBeNull();
  });

  it('shows the side-A player as AKA when the primary is on side A', () => {
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p1']), entityLabel: 'Robert Young', onMatchClick: vi.fn() });
    const name = byClass(tree, 'my-match__name')[0];
    expect(collectText(name)).toContain('AKA');
    expect(collectText(name)).toContain('Robert Young');
    // Opponent is the other side.
    expect(collectText(byClass(tree, 'my-match__opp')[0])).toContain('Nolan Clark');
  });

  it('shows the side-B player as SHIRO when the primary is on side B', () => {
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p2']), entityLabel: 'Nolan Clark', onMatchClick: vi.fn() });
    const name = byClass(tree, 'my-match__name')[0];
    expect(collectText(name)).toContain('SHIRO');
    expect(collectText(name)).toContain('Nolan Clark');
  });

  it('uses a dojo eyebrow when the entity label differs from the competing member', () => {
    // Dojo primary "Hagane Dojo" → member p1 (Robert) is competing.
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p1', 'p3']), entityLabel: 'Hagane Dojo', onMatchClick: vi.fn() });
    expect(collectText(byClass(tree, 'my-match__lbl')[0])).toMatch(/Hagane Dojo · next up/i);
    expect(collectText(byClass(tree, 'my-match__name')[0])).toContain('Robert Young');
  });
});

describe('WatchlistPanel', () => {
  let runtime, WatchlistPanel, WatchHeroCard;
  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    // viewer.jsx grabs `const pluralize = window.pluralize` at module load.
    global.window.pluralize = (count, singular, plural) =>
      count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`;
    vi.resetModules();
    ({ WatchlistPanel, WatchHeroCard } = await import('../viewer.jsx'));
  });
  afterEach(() => { runtime.unmount(); global.React = realReact; vi.resetModules(); });

  const baseProps = (over = {}) => ({
    roster: ROSTER,
    watchlist: [],
    setWatchlist: vi.fn(),
    primaryKey: '',
    setPrimaryKey: vi.fn(),
    primaryEntry: null,
    primaryNextMatch: null,
    upcoming: [],
    onMatchClick: vi.fn(),
    ...over,
  });
  const heroNodes = (tree) => findAll(tree, (n) => n.type === WatchHeroCard);

  it('empty state: hint, picker, no chips, no hero, no pin hint', () => {
    const tree = runtime.mount(WatchlistPanel, baseProps());
    expect(collectText(tree)).toMatch(/Track yourself/);
    expect(byClass(tree, 'pmf__chip')).toHaveLength(0);
    expect(heroNodes(tree)).toHaveLength(0);
    expect(byClass(tree, 'watchlist-pin-hint')).toHaveLength(0);
  });

  it('single entry: one chip with NO pin star, hero rendered, no pin hint', () => {
    const wl = [{ type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' }];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      watchlist: wl, primaryEntry: wl[0], primaryNextMatch: MATCH,
    }));
    expect(byClass(tree, 'pmf__chip')).toHaveLength(1);
    expect(byClass(tree, 'pmf__chip-pin')).toHaveLength(0); // no pin UI for a lone entry
    expect(heroNodes(tree)).toHaveLength(1);
    expect(byClass(tree, 'watchlist-pin-hint')).toHaveLength(0);
  });

  it('multi, no pin: pin stars on every chip, pin hint, no hero, compact list shown', () => {
    const wl = [
      { type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' },
      { type: 'player', id: 'p2', name: 'Nolan Clark', dojo: 'Tsubaki Kenyukai' },
    ];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      watchlist: wl, primaryEntry: null, upcoming: [MATCH],
    }));
    expect(byClass(tree, 'pmf__chip')).toHaveLength(2);
    expect(byClass(tree, 'pmf__chip-pin')).toHaveLength(2);
    expect(byClass(tree, 'watchlist-pin-hint')).toHaveLength(1);
    expect(heroNodes(tree)).toHaveLength(0);
    expect(byClass(tree, 'vsched')).toHaveLength(1); // compact upcoming list
  });

  it('multi, pinned: hero rendered, no pin hint', () => {
    const wl = [
      { type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' },
      { type: 'dojo', dojo: 'Hagane Dojo' },
    ];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      watchlist: wl, primaryKey: 'dojo:Hagane Dojo', primaryEntry: wl[1], primaryNextMatch: MATCH, upcoming: [MATCH],
    }));
    expect(heroNodes(tree)).toHaveLength(1);
    expect(byClass(tree, 'watchlist-pin-hint')).toHaveLength(0);
  });

  it('renders a dojo chip with its member count', () => {
    const wl = [
      { type: 'dojo', dojo: 'Hagane Dojo' },
      { type: 'player', id: 'p2', name: 'Nolan Clark', dojo: 'Tsubaki Kenyukai' },
    ];
    const tree = runtime.mount(WatchlistPanel, baseProps({ watchlist: wl }));
    const dojoChip = byClass(tree, 'pmf__chip--dojo')[0];
    expect(dojoChip).toBeTruthy();
    // Hagane Dojo has 2 members in ROSTER (p1, p3).
    expect(collectText(dojoChip)).toContain('Hagane Dojo (2)');
  });
});

describe('WatchPicker', () => {
  let runtime, WatchPicker;
  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    // viewer.jsx grabs `const pluralize = window.pluralize` at module load.
    global.window.pluralize = (count, singular, plural) =>
      count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`;
    vi.resetModules();
    ({ WatchPicker } = await import('../viewer.jsx'));
  });
  afterEach(() => { runtime.unmount(); global.React = realReact; vi.resetModules(); });

  const dojos = [{ name: 'Hagane Dojo', total: 2 }, { name: 'Tsubaki Kenyukai', total: 1 }];

  const openWith = (query, over = {}) => {
    runtime.mount(WatchPicker, {
      roster: ROSTER, dojos,
      watchedPlayerIds: [], watchedDojos: [],
      onPickPlayer: vi.fn(), onPickDojo: vi.fn(), placeholder: 'Add…',
      ...over,
    });
    const input = byClass(runtime.currentTree(), 'pmf__input')[0];
    input.props.onFocus();
    input.props.onChange({ target: { value: query } });
    return runtime.currentTree();
  };

  it('surfaces matching dojos (first) and players in one dropdown', () => {
    const tree = openWith('Hagane');
    const opts = byClass(tree, 'pmf__option');
    expect(opts.length).toBeGreaterThan(0);
    const dojoOpts = byClass(tree, 'pmf__option--dojo');
    expect(dojoOpts).toHaveLength(1);
    expect(collectText(dojoOpts[0])).toMatch(/Hagane Dojo/);
    expect(collectText(dojoOpts[0])).toMatch(/Watch all · 2 members/);
    // The dojo's members also match the query by dojo name.
    expect(collectText(tree)).toContain('Robert Young');
  });

  it('excludes already-watched players and dojos from the dropdown', () => {
    const tree = openWith('', { watchedPlayerIds: ['p1'], watchedDojos: ['Hagane Dojo'] });
    const txt = collectText(byClass(tree, 'pmf__dropdown')[0]);
    expect(txt).not.toContain('Robert Young'); // excluded player
    // The Hagane Dojo *option* is excluded; assert no dojo option for it.
    const dojoOpts = byClass(tree, 'pmf__option--dojo').map(collectText).join(' ');
    expect(dojoOpts).not.toContain('Hagane Dojo');
    expect(dojoOpts).toContain('Tsubaki Kenyukai');
  });

  it('invokes onPickDojo when a dojo option is chosen', () => {
    const onPickDojo = vi.fn();
    runtime.mount(WatchPicker, {
      roster: ROSTER, dojos, watchedPlayerIds: [], watchedDojos: [],
      onPickPlayer: vi.fn(), onPickDojo, placeholder: 'Add…',
    });
    let input = byClass(runtime.currentTree(), 'pmf__input')[0];
    input.props.onFocus();
    input.props.onChange({ target: { value: 'Hagane' } });
    const dojoOpt = byClass(runtime.currentTree(), 'pmf__option--dojo')[0];
    dojoOpt.props.onClick();
    expect(onPickDojo).toHaveBeenCalledWith({ name: 'Hagane Dojo', total: 2 });
  });
});
