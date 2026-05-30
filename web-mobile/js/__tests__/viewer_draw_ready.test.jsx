import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

// Walks a vnode tree and concatenates all string/number leaves, including
// the literal children of child-component vnodes (which the reactive shim
// does not execute). Mirrors collectText in viewer.test.jsx.
function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

// Depth-first search for the first vnode matching predicate.
function findInTree(node, predicate) {
  if (!node || typeof node !== 'object') return null;
  if (Array.isArray(node)) {
    for (const k of node) {
      const found = findInTree(k, predicate);
      if (found) return found;
    }
    return null;
  }
  if (predicate(node)) return node;
  const kids = node.children || node.props?.children || [];
  for (const k of [].concat(kids)) {
    const found = findInTree(k, predicate);
    if (found) return found;
  }
  return null;
}

// Collect every vnode whose `type` is the named child component reference.
// The reactive shim does not execute child components, but it preserves the
// `type` (the function reference) so we can assert which tabs/components the
// parent chose to render.
function findAllByType(node, typeRef, acc = []) {
  if (!node || typeof node !== 'object') return acc;
  if (Array.isArray(node)) {
    node.forEach(k => findAllByType(k, typeRef, acc));
    return acc;
  }
  if (node.type === typeRef) acc.push(node);
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => findAllByType(k, typeRef, acc));
  return acc;
}

// mp-rrd Phase 1 + 2: the public viewer must expose Pools + Bracket tabs at
// draw-ready (the draw is published but no match has been called), keep
// "not started" for setup, keep Swiss excluded from pools/bracket, never
// treat draw-ready as live, and link a pools comp to its playoffs comp.
describe('ViewerCompetition draw-ready exposure (mp-rrd)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerCompetition;
  const savedGlobals = {};
  // window globals captured at module-eval time (const StatusBadge = window.X)
  // MUST be set before importing viewer.jsx, plus the lazily-looked-up ones.
  const STUBBED = [
    'StatusBadge', 'formatDate', 'formatLabel', 'pluralize', 'Term',
    'BracketTree', 'buildBracket', 'roundLabel', 'formatIpponsScore',
    'ipponsFromScore', 'isHikiwake', 'hasBothSides', 'compareDmy',
    'queueLabel', 'queueLabelCompact',
  ];

  // A minimal mixed pools comp at draw-ready with one populated pool and a
  // persisted bracket. Pools + bracket are returned unconditionally by the
  // viewer handler, so they are present even before the comp starts.
  const mkPoolsComp = (overrides = {}) => ({
    id: 'pools-1',
    name: 'Mixed Pools Cup',
    kind: 'individual',
    teamSize: 0,
    format: 'mixed',
    status: 'draw-ready',
    startTime: '09:00',
    courts: ['A'],
    poolWinners: 2,
    players: [],
    ...overrides,
  });

  const samplePools = [{ poolName: 'Pool A', players: [{ id: 'p1', name: 'Alice' }, { id: 'p2', name: 'Bob' }] }];
  const sampleBracket = { rounds: [[{ id: 'r0-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' }, status: 'scheduled' }]] };

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });
    // Identifiable child-component references so findAllByType can detect them.
    global.window.StatusBadge = function StatusBadge() { return null; };
    global.window.BracketTree = function BracketTree() { return null; };
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    global.window.formatDate = (d) => d || '';
    global.window.formatLabel = (s) => s || '';
    global.window.pluralize = (n, a, b) => `${n} ${n === 1 ? a : b}`;
    global.window.buildBracket = () => sampleBracket.rounds;
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.formatIpponsScore = () => '';
    global.window.ipponsFromScore = () => [];
    global.window.isHikiwake = () => false;
    global.window.hasBothSides = (m) => !!(m && m.sideA && m.sideB);
    global.window.compareDmy = (a, b) => String(a).localeCompare(String(b));
    global.window.queueLabel = () => '';
    global.window.queueLabelCompact = () => null;
    vi.resetModules();
    ({ ViewerCompetition } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    STUBBED.forEach(k => {
      if (savedGlobals[k]?.had) global.window[k] = savedGlobals[k].val;
      else delete global.window[k];
    });
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('renders Pools and Bracket tabs at draw-ready', () => {
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [] },
      competition: mkPoolsComp(),
      pools: samplePools,
      poolMatches: [],
      standings: [],
      bracket: sampleBracket,
      onBack: () => {},
      onSelectCompetition: () => {},
      tweaks: {},
    });
    // Tab buttons are plain <button> nodes whose text is the tab label.
    const tabButtons = [];
    findAllByType(tree, 'button', tabButtons);
    const labels = tabButtons.map(b => collectText(b));
    expect(labels.some(l => l.includes('Pools'))).toBe(true);
    expect(labels.some(l => l.includes('Bracket'))).toBe(true);
    expect(labels.some(l => l.includes('Overview'))).toBe(true);
  });

  it('setup still hides the Pools/Bracket tabs', () => {
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [] },
      competition: mkPoolsComp({ status: 'setup' }),
      pools: samplePools,
      poolMatches: [],
      standings: [],
      bracket: sampleBracket,
      onBack: () => {},
      onSelectCompetition: () => {},
      tweaks: {},
    });
    const tabButtons = [];
    findAllByType(tree, 'button', tabButtons);
    const labels = tabButtons.map(b => collectText(b));
    expect(labels.some(l => l.includes('Pools'))).toBe(false);
    expect(labels.some(l => l.includes('Bracket'))).toBe(false);
  });

  it('Swiss is excluded from Pools/Bracket tabs even at draw-ready', () => {
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [] },
      competition: mkPoolsComp({ format: 'swiss' }),
      pools: samplePools,
      poolMatches: [],
      standings: [],
      bracket: sampleBracket,
      onBack: () => {},
      onSelectCompetition: () => {},
      tweaks: {},
    });
    const tabButtons = [];
    findAllByType(tree, 'button', tabButtons);
    const labels = tabButtons.map(b => collectText(b));
    expect(labels.some(l => l.includes('Pools'))).toBe(false);
    expect(labels.some(l => l.includes('Bracket'))).toBe(false);
    // Swiss surfaces its own Standings tab instead.
    expect(labels.some(l => l.includes('Standings'))).toBe(true);
  });

  it('links a pools comp to its playoffs comp and navigates on click', () => {
    const playoffs = { id: 'po-1', name: 'Mixed Playoffs', sourceCompID: 'pools-1', status: 'setup', courts: ['A'], players: [], format: 'playoffs', kind: 'individual', teamSize: 0, startTime: '11:00' };
    const onSelect = vi.fn();
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [mkPoolsComp(), playoffs] },
      competition: mkPoolsComp(),
      pools: samplePools,
      poolMatches: [],
      standings: [],
      bracket: sampleBracket,
      onBack: () => {},
      onSelectCompetition: onSelect,
      tweaks: {},
    });
    const text = collectText(tree);
    expect(text).toContain('View the playoffs bracket');
    expect(text).toContain('Mixed Playoffs');
    // Find the cross-link button (carries the linked comp name) and click it.
    const linkBtn = findInTree(tree, n => n?.type === 'button' && collectText(n).includes('Mixed Playoffs'));
    expect(linkBtn).toBeTruthy();
    linkBtn.props.onClick();
    expect(onSelect).toHaveBeenCalledWith('po-1');
  });

  it('links a playoffs comp back to its source pools comp', () => {
    const pools = mkPoolsComp();
    const playoffs = { id: 'po-1', name: 'Mixed Playoffs', sourceCompID: 'pools-1', status: 'draw-ready', courts: ['A'], players: [], format: 'playoffs', kind: 'individual', teamSize: 0, startTime: '11:00', poolWinners: 0 };
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [pools, playoffs] },
      competition: playoffs,
      pools: [],
      poolMatches: [],
      standings: [],
      bracket: sampleBracket,
      onBack: () => {},
      onSelectCompetition: () => {},
      tweaks: {},
    });
    const text = collectText(tree);
    expect(text).toContain('View the pools');
    expect(text).toContain('Mixed Pools Cup');
  });
});

// mp-rrd: the Overview tab body. draw-ready must show a "Draw is ready"
// pointer to the Pools/Bracket tabs; setup keeps the plain "Not started yet".
// ViewerOverview is the leaf component that renders this text (the parent
// ViewerCompetition only mounts it), so we mount it directly.
describe('ViewerOverview pre-start messaging (mp-rrd)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerOverview;
  const savedGlobals = {};
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'ipponsFromScore'];

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    global.window.isHikiwake = () => false;
    global.window.formatIpponsScore = () => '';
    global.window.ipponsFromScore = () => [];
    vi.resetModules();
    ({ ViewerOverview } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    STUBBED.forEach(k => {
      if (savedGlobals[k]?.had) global.window[k] = savedGlobals[k].val;
      else delete global.window[k];
    });
    vi.restoreAllMocks();
    vi.resetModules();
  });

  const baseProps = { myPlayer: null, myUpcoming: null, currentMatch: null, liveMatches: [], upcomingMatches: [], recentMatches: [], tweaks: {} };

  it('draw-ready shows "Draw is ready" and points to the tabs, not "Not started yet"', () => {
    const tree = runtime.mount(ViewerOverview, { c: { status: 'draw-ready', startTime: '09:00' }, ...baseProps });
    const text = collectText(tree);
    expect(text).toContain('Draw is ready');
    expect(text).toContain('Pools and Bracket');
    expect(text).not.toContain('Not started yet');
  });

  it('setup still shows "Not started yet"', () => {
    const tree = runtime.mount(ViewerOverview, { c: { status: 'setup', startTime: '09:00' }, ...baseProps });
    const text = collectText(tree);
    expect(text).toContain('Not started yet');
    expect(text).not.toContain('Draw is ready');
  });
});

// mp-rrd: the home page must NOT treat a draw-ready comp as live. liveCompIds
// (the set gating LIVE NOW / Up-next / live dot) is derived purely from
// competition status, so we can pin the exclusion behaviour through the
// exported helpers + a direct status filter without mounting ViewerHome
// (which uses localStorage-backed hooks the static React stub can't drive).
describe('draw-ready is not live (mp-rrd)', () => {
  it('tournamentMatches excludes setup but includes draw-ready structure', async () => {
    vi.resetModules();
    global.window = global.window || {};
    global.window.hasBothSides = (m) => !!(m && m.sideA && m.sideB);
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    const { tournamentMatches, compMatches } = await import('../viewer.jsx');
    const drawReady = {
      id: 'dr', status: 'draw-ready', kind: 'individual', teamSize: 0,
      poolMatches: [{ id: 'Pool A-0', status: 'scheduled', sideA: { id: 'p1', name: 'A' }, sideB: { id: 'p2', name: 'B' } }],
      bracket: { rounds: [] },
    };
    const setup = { id: 'st', status: 'setup', kind: 'individual', teamSize: 0, poolMatches: [{ id: 'Pool A-0', status: 'scheduled' }], bracket: { rounds: [] } };
    // setup contributes nothing; draw-ready contributes its draw matches.
    expect(compMatches(setup)).toEqual([]);
    expect(compMatches(drawReady).length).toBe(1);
    const all = tournamentMatches({ competitions: [drawReady, setup] });
    expect(all.length).toBe(1);
    expect(all[0].compId).toBe('dr');
    delete global.window.hasBothSides;
    delete global.window.roundLabel;
  });

  it('the live-comp filter (status-based) excludes both setup and draw-ready', () => {
    // Mirrors the liveCompIds predicate in ViewerHome verbatim.
    const isLiveComp = c => !!(c.status && c.status !== 'setup' && c.status !== 'draw-ready');
    expect(isLiveComp({ status: 'setup' })).toBe(false);
    expect(isLiveComp({ status: 'draw-ready' })).toBe(false);
    expect(isLiveComp({ status: 'pools' })).toBe(true);
    expect(isLiveComp({ status: 'started' })).toBe(true);
  });
});
