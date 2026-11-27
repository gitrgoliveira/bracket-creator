// bc-drpb: the watchlist and a draft whose draw the public may not see yet.
// buildRoster records each competition a competitor is in by id (compIds,
// beside the names in comps) and names the hidden drafts among them
// (preparingDraws); preparingDrawsFor answers, for a watched competitor or
// dojo, the hidden drafts the watchlist says "Draw being prepared" for, beside
// whatever their other competitions show.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { collectText } from './helpers/vdom.js';
import { buildRoster, preparingDrawsFor } from '../viewer_watchlist_core.jsx';

const OFFICIATED = { mode: 'officiated' };
const SELF_RUN = { mode: 'self-run' };

const comp = (id, name, status, extra = {}, players = []) => ({ id, name, status, players, ...extra });

const yamada = { id: 'y1', name: 'Yamada', dojo: 'Kodokan' };
const tanaka = { id: 't1', name: 'Tanaka', dojo: 'kodokan' };
const suzuki = { id: 's1', name: 'Suzuki', dojo: 'Mumeishi' };

const world = () => [
  comp('c-hidden', 'Open', 'draw-ready', { drawHidden: true }, [yamada, suzuki]),
  comp('c-published', 'Juniors', 'draw-ready', {}, [tanaka]),
  comp('c-running', 'Seniors', 'pools', { drawHidden: true }, [{ ...yamada, id: 'y2' }]),
  comp('c-hidden-2', 'Open', 'draw-ready', { drawHidden: true }, [{ ...tanaka, id: 't2' }]),
];

describe('buildRoster carries competition ids and the hidden drafts (bc-drpb)', () => {
  it('records each competition by id beside its name', () => {
    const roster = buildRoster(world(), OFFICIATED);
    const y1 = roster.find((p) => p.id === 'y1');
    expect(y1.comps).toEqual(['Open']);
    expect(y1.compIds).toEqual(['c-hidden']);
    expect(y1.preparingDraws).toEqual([{ id: 'c-hidden', name: 'Open' }]);
  });

  it('a published draft and a started competition are not being prepared, whatever is stored', () => {
    const roster = buildRoster(world(), OFFICIATED);
    expect(roster.find((p) => p.id === 't1').preparingDraws).toEqual([]);
    expect(roster.find((p) => p.id === 'y2').preparingDraws).toEqual([]);
  });

  it('a self-run tournament has no hidden drafts', () => {
    const roster = buildRoster(world(), SELF_RUN);
    expect(roster.every((p) => p.preparingDraws.length === 0)).toBe(true);
  });
});

describe('preparingDrawsFor (bc-drpb)', () => {
  it('a watched competitor: the hidden draft they are in', () => {
    const roster = buildRoster(world(), OFFICIATED);
    expect(preparingDrawsFor({ type: 'player', id: 'y1', name: 'Yamada' }, roster)).toEqual([{ id: 'c-hidden', name: 'Open' }]);
    expect(preparingDrawsFor({ type: 'player', id: 't1', name: 'Tanaka' }, roster)).toEqual([]);
  });

  it('a watched dojo: every hidden draft a member is in, once each, told apart by id though they share a name', () => {
    const roster = buildRoster(world(), OFFICIATED);
    expect(preparingDrawsFor({ type: 'dojo', dojo: 'Kodokan' }, roster)).toEqual([
      { id: 'c-hidden', name: 'Open' },
      { id: 'c-hidden-2', name: 'Open' },
    ]);
  });

  it('nothing for no entry, or an entry the roster does not hold', () => {
    const roster = buildRoster(world(), OFFICIATED);
    expect(preparingDrawsFor(null, roster)).toEqual([]);
    expect(preparingDrawsFor({ type: 'player', id: 'gone', name: 'Gone' }, roster)).toEqual([]);
    expect(preparingDrawsFor({ type: 'dojo', dojo: 'Nowhere' }, roster)).toEqual([]);
  });

  it('a publish takes the line away: the next roster has no hidden draft', () => {
    const published = world().map((c) => (c.id === 'c-hidden' ? { ...c, drawHidden: undefined } : c));
    const roster = buildRoster(published, OFFICIATED);
    expect(preparingDrawsFor({ type: 'player', id: 'y1', name: 'Yamada' }, roster)).toEqual([]);
  });
});

// The panel draws the line: "Draw being prepared: <competition>" for each
// hidden draft the hero entry is in, beside the hero card when there is one,
// and in place of "No upcoming matches" when there is not.
function findAll(node, pred, out = []) {
  if (!node || typeof node !== 'object') return out;
  if (Array.isArray(node)) { node.forEach((n) => findAll(n, pred, out)); return out; }
  if (pred(node)) out.push(node);
  const kids = node.children ?? node.props?.children;
  if (kids != null) [].concat(kids).forEach((k) => findAll(k, pred, out));
  return out;
}
const byTestId = (tree, id) => findAll(tree, (n) => n.props?.['data-testid'] === id);

describe('WatchlistPanel: "Draw being prepared" (bc-drpb)', () => {
  const realReact = global.React;
  let runtime, WatchlistPanel, WatchHeroCard;
  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window.pluralize = (count, singular, plural) =>
      count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`;
    vi.resetModules();
    await import('../viewer.jsx');
    ({ WatchlistPanel, WatchHeroCard } = await import('../viewer_watchlist.jsx'));
  });
  afterEach(() => { runtime.unmount(); global.React = realReact; vi.resetModules(); });

  const props = (roster, entry, over = {}) => ({
    tournament: OFFICIATED,
    roster,
    watchlist: [entry],
    setWatchlist: vi.fn(),
    primaryKey: '',
    setPrimaryKey: vi.fn(),
    primaryEntry: entry,
    heroEntry: entry,
    heroNextMatch: null,
    upcoming: [],
    onMatchClick: vi.fn(),
    ...over,
  });
  const yamadaEntry = { type: 'player', id: 'y1', name: 'Yamada', dojo: 'Kodokan' };

  it('a competitor with no match yet: the line for the hidden draft, and no "No upcoming matches"', () => {
    const tree = runtime.mount(WatchlistPanel, props(buildRoster(world(), OFFICIATED), yamadaEntry));
    const lines = byTestId(tree, 'watchlist-draw-preparing');
    expect(lines).toHaveLength(1);
    expect(collectText(lines[0])).toMatch(/Draw being prepared: Open/);
    expect(byTestId(tree, 'watchlist-primary-done')).toHaveLength(0);
  });

  it('beside the hero card when another competition has a match', () => {
    const match = { id: 'm1', status: 'scheduled', phase: 'pool', poolName: 'Pool A', court: 'A',
      sideA: { id: 'y1', name: 'Yamada', dojo: 'Kodokan' }, sideB: { id: 's1', name: 'Suzuki', dojo: 'Mumeishi' } };
    const tree = runtime.mount(WatchlistPanel, props(buildRoster(world(), OFFICIATED), yamadaEntry, { heroNextMatch: match }));
    expect(findAll(tree, (n) => n.type === WatchHeroCard)).toHaveLength(1);
    expect(collectText(byTestId(tree, 'watchlist-draw-preparing')[0])).toMatch(/Draw being prepared: Open/);
  });

  it('after a publish the line goes and "No upcoming matches" comes back', () => {
    const published = world().map((c) => (c.id === 'c-hidden' ? { ...c, drawHidden: undefined } : c));
    const tree = runtime.mount(WatchlistPanel, props(buildRoster(published, OFFICIATED), yamadaEntry));
    expect(byTestId(tree, 'watchlist-draw-preparing')).toHaveLength(0);
    expect(collectText(byTestId(tree, 'watchlist-primary-done')[0])).toMatch(/No upcoming matches for Yamada/);
  });

  it('a watched dojo names each hidden draft once', () => {
    const entry = { type: 'dojo', dojo: 'Kodokan' };
    const tree = runtime.mount(WatchlistPanel, props(buildRoster(world(), OFFICIATED), entry));
    const text = collectText(byTestId(tree, 'watchlist-draw-preparing')[0]);
    expect(text.match(/Draw being prepared/g)).toHaveLength(2);
    expect(byTestId(tree, 'watchlist-primary-done')).toHaveLength(0);
  });
});
