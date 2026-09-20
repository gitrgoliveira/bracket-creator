// mp-xhaa: component-render tests for the unified Watchlist panel and its
// pieces. Uses the makeReactive shim (resetModules + dynamic import so the
// component's `const { useState } = React` destructure picks up the reactive
// stub). Child component vnodes (WatchHeroCard, WatchPicker, TermV) are NOT
// executed by the shim: they appear as {type, props} nodes: so panel tests
// assert structure + which child renders, and WatchHeroCard is mounted
// directly for its own inner-render assertions.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { collectText, expandNamed } from './helpers/vdom.js';

const realReact = global.React;

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
    // viewer.jsx populates window.* with the helpers WatchHeroCard reads at
    // render (matchParticipantIds, poolLabel, mymatchQueueLabel, TermV);
    // the component itself now lives in viewer_watchlist.jsx.
    await import('../viewer.jsx');
    ({ WatchHeroCard } = await import('../viewer_watchlist.jsx'));
  });
  afterEach(() => { runtime.unmount(); global.React = realReact; vi.resetModules(); });

  it('returns null when there is no match', () => {
    const tree = runtime.mount(WatchHeroCard, { nextMatch: null, primaryIds: new Set(), entityLabel: 'X', onMatchClick: vi.fn() });
    expect(tree).toBeNull();
  });

  // bc-wlhc: the side is carried by a TINTED ROW per competitor, not an 8px
  // badge, so these assert the fill CLASS as well as the word -- the class is
  // the channel a colour-blind or glare-blinded reader actually gets, and the
  // old text-only assertion could not see it. Row order is subject first: this
  // is a personal card, not a bracket card, so "me" leads and the tint (not the
  // position) carries the side.
  const sideRows = (tree) => byClass(tree, 'wl-hero__side');
  const rowText = (row) => collectText(row, expandNamed('NumberedName'));

  it('shows the side-A player as Aka when the primary is on side A', () => {
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p1']), entityLabel: 'Robert Young', onMatchClick: vi.fn() });
    const [subject, opp] = sideRows(tree);
    expect(hasClass(subject, 'side-fill--aka')).toBe(true);
    expect(rowText(subject)).toContain('Aka');
    expect(rowText(subject)).toContain('Robert Young');
    // "you" marks which of the two rows is the watched entity.
    expect(rowText(subject)).toContain('you');
    // Opponent is the other side, and takes the other fill.
    expect(hasClass(opp, 'side-fill--shiro')).toBe(true);
    expect(rowText(opp)).toContain('Nolan Clark');
    expect(rowText(opp)).not.toContain('you');
  });

  it('shows the side-B player as Shiro when the primary is on side B', () => {
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p2']), entityLabel: 'Nolan Clark', onMatchClick: vi.fn() });
    const [subject, opp] = sideRows(tree);
    expect(hasClass(subject, 'side-fill--shiro')).toBe(true);
    expect(rowText(subject)).toContain('Shiro');
    expect(rowText(subject)).toContain('Nolan Clark');
    expect(hasClass(opp, 'side-fill--aka')).toBe(true);
  });

  // bc-rvfx: the watchlist card rendered NO competitor number anywhere, while
  // the VSchedItem rows inside the very same panel rendered one. A spectator
  // saw the card they look at first as the only place they could not match
  // against the printed draw sheet.
  //
  // The fixture above deliberately carries NO `number`, which is why widening
  // collectText alone would leave this rendering unpinned -- the failure the
  // PR #428 audit found in four sibling files. This fixture supplies one.
  it('renders the competitor number on both the subject and the opponent', () => {
    const numbered = {
      ...MATCH,
      sideA: { ...MATCH.sideA, number: 'K12' },
      sideB: { ...MATCH.sideB, number: 'K7' },
    };
    const tree = runtime.mount(WatchHeroCard, { nextMatch: numbered, primaryIds: new Set(['p1']), entityLabel: 'Robert Young', onMatchClick: vi.fn() });
    const [subject, opp] = sideRows(tree);
    expect(rowText(subject)).toContain('K12');
    expect(rowText(subject)).toContain('Robert Young');
    expect(rowText(opp)).toContain('K7');
    expect(rowText(opp)).toContain('Nolan Clark');
  });

  it('renders the name alone when the competitor has no number yet', () => {
    // Pre-draw, nobody has a number: the chip must not render a stray gap or
    // an empty element. MATCH carries no `number`, which is that state.
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p1']), entityLabel: 'Robert Young', onMatchClick: vi.fn() });
    const name = rowText(sideRows(tree)[0]);
    expect(name).toContain('Robert Young');
    expect(name).not.toMatch(/K\d/);
  });

  it('uses a dojo eyebrow when the entity label differs from the competing member', () => {
    // Dojo primary "Hagane Dojo" → member p1 (Robert) is competing.
    // MATCH is running, so the hero label is the bare dojo eyebrow: no
    // "· next up" suffix (a running match is happening now, not next up).
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p1', 'p3']), entityLabel: 'Hagane Dojo', onMatchClick: vi.fn() });
    const lbl = collectText(byClass(tree, 'wl-hero__lbl')[0], expandNamed('NumberedName'));
    expect(lbl).toContain('Hagane Dojo');
    expect(lbl).not.toMatch(/next up/i);
    expect(rowText(sideRows(tree)[0])).toContain('Robert Young');
  });

  // bc-wlhc. MATCH is running, so this also pins the two things the old card
  // got wrong at exactly that moment: the running signal must be the navy BAND
  // (the old 1.20:1 ring was the only cue), and the live region must still be
  // in the DOM (the old one sat on the Queue chip, which is removed when
  // running, so "your match has started" could never be announced).
  it('signals a running match with the band, and keeps the live region mounted', () => {
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p1']), entityLabel: 'Robert Young', onMatchClick: vi.fn() });
    const band = byClass(tree, 'wl-hero__now')[0];
    expect(band, 'a running match renders the navy band').toBeTruthy();
    expect(collectText(band, expandNamed('NumberedName'))).toMatch(/on court now/i);
    const live = findAll(tree, (n) => n.props?.['aria-live'] === 'polite')[0];
    expect(live, 'the live region survives the start of the match').toBeTruthy();
    // While running the line reads "Now", not the scheduled time.
    expect(collectText(live, expandNamed('NumberedName'))).toContain('Now');
  });

  it('never wraps the court letter: it is its own nowrap element', () => {
    const tree = runtime.mount(WatchHeroCard, { nextMatch: MATCH, primaryIds: new Set(['p1']), entityLabel: 'Robert Young', onMatchClick: vi.fn() });
    const v = byClass(tree, 'wl-hero__where-v')[0];
    expect(v, 'the court letter has its own element').toBeTruthy();
    expect(collectText(v, expandNamed('NumberedName')).trim()).toBe('A');
  });
});

describe('WatchlistPanel', () => {
  let runtime, WatchlistPanel, WatchHeroCard, WatchPicker;
  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    // viewer.jsx grabs `const pluralize = window.pluralize` at module load.
    global.window.pluralize = (count, singular, plural) =>
      count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`;
    vi.resetModules();
    // viewer.jsx populates window.* with the helpers the panel reads at render
    // (effectivePrimaryKey, entryKey, resolveEntryPlayerIds, addPlayerToWatchlist,
    // VSchedItem, WATCHLIST_MAX); the components live in viewer_watchlist.jsx.
    await import('../viewer.jsx');
    ({ WatchlistPanel, WatchHeroCard, WatchPicker } = await import('../viewer_watchlist.jsx'));
  });
  afterEach(() => { runtime.unmount(); global.React = realReact; vi.resetModules(); });

  const baseProps = (over = {}) => ({
    roster: ROSTER,
    watchlist: [],
    setWatchlist: vi.fn(),
    primaryKey: '',
    setPrimaryKey: vi.fn(),
    primaryEntry: null,
    heroEntry: null,
    heroNextMatch: null,
    upcoming: [],
    onMatchClick: vi.fn(),
    ...over,
  });
  const heroNodes = (tree) => findAll(tree, (n) => n.type === WatchHeroCard);

  it('empty state: hint, picker, no chips, no hero, no pin hint', () => {
    const tree = runtime.mount(WatchlistPanel, baseProps());
    expect(collectText(tree, expandNamed('NumberedName'))).toMatch(/Track yourself/);
    expect(byClass(tree, 'pmf__chip')).toHaveLength(0);
    expect(heroNodes(tree)).toHaveLength(0);
    expect(byClass(tree, 'watchlist-pin-hint')).toHaveLength(0);
  });

  it('single entry: one chip with NO pin star, hero rendered, no pin hint', () => {
    const wl = [{ type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' }];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      watchlist: wl, primaryEntry: wl[0], heroEntry: wl[0], heroNextMatch: MATCH,
    }));
    expect(byClass(tree, 'pmf__chip')).toHaveLength(1);
    expect(byClass(tree, 'pmf__chip-pin')).toHaveLength(0); // no pin UI for a lone entry
    expect(heroNodes(tree)).toHaveLength(1);
    expect(byClass(tree, 'watchlist-pin-hint')).toHaveLength(0);
  });

  // bc-wlhc: adding a second person used to DELETE the hero, because the card
  // was gated on the same null the chime is gated on. The hint and the hero now
  // coexist: the card shows the first-added entry, the hint says what pinning
  // still buys (moving it, plus the chime).
  it('multi, no pin: pin stars, hero STILL rendered, hint names who it is showing', () => {
    const wl = [
      { type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' },
      { type: 'player', id: 'p2', name: 'Nolan Clark', dojo: 'Tsubaki Kenyukai' },
    ];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      watchlist: wl, primaryEntry: null, heroEntry: wl[0], heroNextMatch: MATCH, upcoming: [MATCH],
    }));
    expect(byClass(tree, 'pmf__chip')).toHaveLength(2);
    expect(byClass(tree, 'pmf__chip-pin')).toHaveLength(2);
    expect(heroNodes(tree), 'the card survives a second watched person').toHaveLength(1);
    const hint = byClass(tree, 'watchlist-pin-hint');
    expect(hint).toHaveLength(1);
    const hintText = collectText(hint[0]);
    expect(hintText).toMatch(/Showing Robert Young/);
    // The shown person is unpinned too, so the invitation must cover THEM.
    // "follow someone else" told a reader watching themselves plus a partner
    // that there was nothing here for them, and left the chime off.
    expect(hintText, 'the hint must not exclude the person it is showing').not.toMatch(/someone else/);
    expect(hintText).toMatch(/chime/);
    expect(byClass(tree, 'vsched')).toHaveLength(1); // compact upcoming list
  });

  it('multi, pinned: hero rendered, no pin hint', () => {
    const wl = [
      { type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' },
      { type: 'dojo', dojo: 'Hagane Dojo' },
    ];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      watchlist: wl, primaryKey: 'dojo:Hagane Dojo', primaryEntry: wl[1], heroEntry: wl[1], heroNextMatch: MATCH, upcoming: [MATCH],
    }));
    expect(heroNodes(tree)).toHaveLength(1);
    expect(byClass(tree, 'watchlist-pin-hint')).toHaveLength(0);
  });

  // bc-wlhc. The watchlist is persisted across tournaments, so a stored id can
  // stop resolving (a re-import, a delete/recreate, a replaced participant).
  // The chip kept rendering from its STORED name and looked perfectly healthy,
  // while the panel below it said "No upcoming matches for X" -- a statement
  // the panel had no way to know was true, and which read as "X is done for the
  // day" to a reader watching X fight on the court in front of them.
  //
  // Note what is NOT done: the id is not re-resolved by name. bc-pnum rules
  // that an id resolving to nothing resolves to nothing. The fix is to make the
  // failure visible, not to guess past it.
  it('marks a watch entry whose id is not in the roster, and says so instead of claiming no matches', () => {
    const wl = [{ type: 'player', id: 'gone-from-roster', name: 'Robert Young', dojo: 'Hagane Dojo' }];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      watchlist: wl, primaryEntry: wl[0], heroEntry: wl[0], heroNextMatch: null,
    }));
    expect(byClass(tree, 'pmf__chip--unresolved'), 'the chip carries the unresolved state').toHaveLength(1);
    expect(byClass(tree, 'pmf__chip-warn'), 'and a non-colour marker for it').toHaveLength(1);

    const hint = findAll(tree, (n) => n.props?.['data-testid'] === 'watchlist-unresolved');
    expect(hint, 'the hint names the real problem').toHaveLength(1);
    const text = collectText(hint[0]);
    expect(text).toMatch(/not in this tournament's roster/);
    expect(text, 'never the misleading claim').not.toMatch(/No upcoming matches/);
  });

  it('claims nothing about a roster it does not have', () => {
    // Absence is a claim, and an empty roster supports no claim. app.jsx holds
    // the viewer behind a spinner until the payload lands, so this is a floor
    // rather than a live bug -- without it, a future lazy roster load would
    // turn every chip amber on first paint and tell the reader to delete
    // people who are perfectly fine.
    const wl = [{ type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' }];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      roster: [], watchlist: wl, primaryEntry: wl[0], heroEntry: wl[0], heroNextMatch: null,
    }));
    expect(byClass(tree, 'pmf__chip--unresolved')).toHaveLength(0);
    expect(findAll(tree, (n) => n.props?.['data-testid'] === 'watchlist-unresolved')).toHaveLength(0);
  });

  it('still says "no upcoming matches" when the entry DOES resolve', () => {
    // The other half: the reworded hint must not swallow the ordinary
    // finished-for-today case, which is the common one.
    const wl = [{ type: 'player', id: 'p1', name: 'Robert Young', dojo: 'Hagane Dojo' }];
    const tree = runtime.mount(WatchlistPanel, baseProps({
      watchlist: wl, primaryEntry: wl[0], heroEntry: wl[0], heroNextMatch: null,
    }));
    expect(byClass(tree, 'pmf__chip--unresolved')).toHaveLength(0);
    const hint = findAll(tree, (n) => n.props?.['data-testid'] === 'watchlist-primary-done');
    expect(hint).toHaveLength(1);
    expect(collectText(hint[0])).toMatch(/No upcoming matches for Robert Young/);
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
    expect(collectText(dojoChip, expandNamed('NumberedName'))).toContain('Hagane Dojo (2)');
  });

  // onFirstAdd / maybeFirstAdd: fires exactly once per empty→add transition.
  // Relies on WatchPicker appearing as an unexecuted child vnode: call its
  // onPickPlayer prop directly to invoke addPlayer() → maybeFirstAdd().
  it('onFirstAdd fires exactly once on the first player add when watchlist is empty', () => {
    const onFirstAdd = vi.fn();
    runtime.mount(WatchlistPanel, baseProps({ onFirstAdd }));
    const picker = findAll(runtime.currentTree(), (n) => n.type === WatchPicker)[0];
    picker.props.onPickPlayer(ROSTER[0]);
    expect(onFirstAdd).toHaveBeenCalledOnce();
  });

  it('onFirstAdd does not fire on subsequent adds in the same session', () => {
    const onFirstAdd = vi.fn();
    runtime.mount(WatchlistPanel, baseProps({ onFirstAdd }));
    const picker = findAll(runtime.currentTree(), (n) => n.type === WatchPicker)[0];
    picker.props.onPickPlayer(ROSTER[0]);
    picker.props.onPickPlayer(ROSTER[1]);
    expect(onFirstAdd).toHaveBeenCalledOnce();
  });

  it('onFirstAdd fires again after the watchlist is cleared and the panel remounts', () => {
    const onFirstAdd = vi.fn();
    // First session: add from empty → fires once.
    runtime.mount(WatchlistPanel, baseProps({ onFirstAdd }));
    findAll(runtime.currentTree(), (n) => n.type === WatchPicker)[0].props.onPickPlayer(ROSTER[0]);
    expect(onFirstAdd).toHaveBeenCalledOnce();
    // Simulate watchlist cleared → panel remounts (hookSlots reset, mount effect re-runs).
    runtime.mount(WatchlistPanel, baseProps({ onFirstAdd }));
    findAll(runtime.currentTree(), (n) => n.type === WatchPicker)[0].props.onPickPlayer(ROSTER[1]);
    expect(onFirstAdd).toHaveBeenCalledTimes(2);
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
    // WatchPicker now lives in viewer_watchlist.jsx (reads window.pluralize,
    // set above, at module load).
    await import('../viewer.jsx');
    ({ WatchPicker } = await import('../viewer_watchlist.jsx'));
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
    expect(collectText(dojoOpts[0], expandNamed('NumberedName'))).toMatch(/Hagane Dojo/);
    expect(collectText(dojoOpts[0], expandNamed('NumberedName'))).toMatch(/Watch all · 2 members/);
    // The dojo's members also match the query by dojo name.
    expect(collectText(tree, expandNamed('NumberedName'))).toContain('Robert Young');
  });

  // bc-wlhc: with nothing to offer, the dropdown used to render NOTHING, so on
  // a phone (no hover, no console) a mistyped name was indistinguishable from a
  // broken control. The two dead ends are worded apart because the reader's
  // next action differs: fix the spelling, versus nothing left to add.
  const emptyRow = (tree) => findAll(tree, (n) => n.props?.['data-testid'] === 'watchpicker-empty');

  it('says so when the query matches nobody', () => {
    const tree = openWith('Zzzzz');
    expect(emptyRow(tree), 'the dropdown must not render empty').toHaveLength(1);
    const text = collectText(emptyRow(tree)[0]);
    expect(text).toMatch(/No one here matches/);
    expect(text).toMatch(/Zzzzz/);
  });

  it('distinguishes "already watching them all" from "no such person"', () => {
    const tree = openWith('Hagane', {
      watchedPlayerIds: ['p1', 'p3'], watchedDojos: ['Hagane Dojo'],
    });
    expect(emptyRow(tree)).toHaveLength(1);
    const text = collectText(emptyRow(tree)[0]);
    expect(text).toMatch(/already on your watchlist/);
    expect(text, 'the roster DOES hold them, so this is the wrong message').not.toMatch(/No one here matches/);
  });

  it('says the tournament is empty when the roster is', () => {
    const tree = openWith('', { roster: [], dojos: [] });
    expect(emptyRow(tree)).toHaveLength(1);
    expect(collectText(emptyRow(tree)[0])).toMatch(/No competitors have been added/);
  });

  it('excludes already-watched players and dojos from the dropdown', () => {
    const tree = openWith('', { watchedPlayerIds: ['p1'], watchedDojos: ['Hagane Dojo'] });
    const txt = collectText(byClass(tree, 'pmf__dropdown')[0], expandNamed('NumberedName'));
    expect(txt).not.toContain('Robert Young'); // excluded player
    // The Hagane Dojo *option* is excluded; assert no dojo option for it.
    const dojoOpts = byClass(tree, 'pmf__option--dojo').map(n => collectText(n, expandNamed('NumberedName'))).join(' ');
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

// Regression for the tri-review finding: the legacy single-follow keys must be
// deleted ONLY after the migrated watchlist is durably written. A swallowed
// write (e.g. QuotaExceededError) combined with unconditional deletion would
// silently and permanently lose the followed player.
describe('useWatchlist legacy migration', () => {
  let runtime, useWatchlist, origLSDesc;

  const makeLS = (initial, throwOnSetKey) => {
    const store = { ...initial };
    return {
      _store: store,
      getItem: (k) => (k in store ? store[k] : null),
      setItem: (k, v) => {
        if (throwOnSetKey && k === throwOnSetKey) throw new Error('QuotaExceededError');
        store[k] = String(v);
      },
      removeItem: (k) => { delete store[k]; },
    };
  };
  const installLS = (ls) => {
    Object.defineProperty(global.window, 'localStorage', { value: ls, writable: true, configurable: true });
  };

  beforeEach(async () => {
    origLSDesc = Object.getOwnPropertyDescriptor(global.window, 'localStorage');
    runtime = makeReactive();
    global.React = runtime.React;
    global.window.pluralize = (count, singular, plural) =>
      count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`;
    vi.resetModules();
    ({ useWatchlist } = await import('../viewer.jsx'));
  });
  afterEach(() => {
    runtime.unmount();
    if (origLSDesc) Object.defineProperty(global.window, 'localStorage', origLSDesc);
    global.React = realReact;
    vi.resetModules();
  });

  // Mount a probe component that just runs the hook and captures its list.
  const mountHook = () => {
    let captured;
    const Probe = () => { captured = useWatchlist()[0]; return null; };
    runtime.mount(Probe, {});
    return captured;
  };

  it('keeps the legacy keys when the migrated watchlist write fails', () => {
    const ls = makeLS({ bc_my_player_id: 'p1', bc_my_player_name: 'Alice' }, 'bc_watchlist');
    installLS(ls);
    const list = mountHook();
    // The in-memory list still migrated for the session…
    expect(list).toEqual([{ type: 'player', id: 'p1', name: 'Alice', dojo: '' }]);
    // …but the legacy keys MUST survive so the follow isn't lost on reload.
    expect(ls._store.bc_my_player_id).toBe('p1');
    expect(ls._store.bc_my_player_name).toBe('Alice');
    expect(ls._store.bc_watchlist).toBeUndefined();
  });

  it('deletes the legacy keys once the migrated watchlist is written', () => {
    const ls = makeLS({ bc_my_player_id: 'p1', bc_my_player_name: 'Alice' });
    installLS(ls);
    const list = mountHook();
    expect(list).toEqual([{ type: 'player', id: 'p1', name: 'Alice', dojo: '' }]);
    expect(ls._store.bc_my_player_id).toBeUndefined();
    expect(ls._store.bc_my_player_name).toBeUndefined();
    expect(JSON.parse(ls._store.bc_watchlist)).toEqual([{ type: 'player', id: 'p1', name: 'Alice', dojo: '' }]);
  });
});
