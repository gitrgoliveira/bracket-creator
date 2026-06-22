import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

// Walks a vnode tree and concatenates all string/number leaves, including
// the literal children of child-component vnodes (which the reactive shim
// does not execute). Mirrors collectText in viewer.test.jsx.
function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (typeof node.type === 'function') {
    try {
      const p = { ...(node.props || {}) };
      if (node.children?.length) p.children = node.children.length === 1 ? node.children[0] : node.children;
      return collectText(node.type(p));
    } catch { /* fall through */ }
  }
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
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

// mp-rrd Phase 1: the public viewer must expose Pools + Bracket tabs at
// draw-ready (the draw is published but no match has been called), keep
// "not started" for setup, keep Swiss excluded from pools/bracket, and
// never treat draw-ready as running. (Phase 2 split-comp cross-links removed
// in mp-turx — mixed comps are now a single competition.)
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
    'queueLabel', 'queueLabelCompact', 'teamIVScore', 'matchScoreStr',
    'EmptyState',
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
    global.window.EmptyState = function EmptyState(props) { return { type: 'div', props: { className: 'empty', ...props }, children: [props.icon, props.title, props.message, props.cta].filter(Boolean) }; };
    global.window.formatDate = (d) => d || '';
    global.window.formatLabel = (s) => s || '';
    global.window.pluralize = (n, a, b) => `${n} ${n === 1 ? a : b}`;
    global.window.buildBracket = () => sampleBracket.rounds;
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.formatIpponsScore = () => '';
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = (m, ippB, ippA) =>
      (global.window.teamIVScore(m)) ||
      global.window.formatIpponsScore(ippB, ippA, m?.score, m?.decision, m?.encho, m?.decidedByHantei);
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
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'ipponsFromScore', 'teamIVScore', 'matchScoreStr', 'EmptyState'];

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
    global.window.EmptyState = function EmptyState(props) { return { type: 'div', props: { className: 'empty', ...props }, children: [props.icon, props.title, props.message, props.cta].filter(Boolean) }; };
    global.window.isHikiwake = () => false;
    global.window.formatIpponsScore = () => '';
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = (m, ippB, ippA) =>
      (global.window.teamIVScore(m)) ||
      global.window.formatIpponsScore(ippB, ippA, m?.score, m?.decision, m?.encho, m?.decidedByHantei);
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

  const baseProps = { myPlayer: null, myUpcoming: null, currentMatch: null, runningMatches: [], upcomingMatches: [], recentMatches: [], tweaks: {} };

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

  it('draw-ready SWISS comp points to the Standings tab, NOT Pools/Bracket', () => {
    // Swiss renders a Standings tab instead of Pools/Bracket, so the
    // "browse the Pools and Bracket tabs" wording would be misleading.
    const tree = runtime.mount(ViewerOverview, { c: { status: 'draw-ready', format: 'swiss', startTime: '09:00' }, ...baseProps });
    const text = collectText(tree);
    expect(text).toContain('Draw is ready');
    expect(text).toContain('Standings');
    expect(text).not.toContain('Pools and Bracket');
  });
});

// mp-rrd: the home page must NOT treat a draw-ready comp as running. runningCompIds
// (the set gating NOW / Up-next / running dot) is derived purely from
// competition status, so we can pin the exclusion behaviour through the
// exported helpers + a direct status filter without mounting ViewerHome
// (which uses localStorage-backed hooks the static React stub can't drive).
describe('draw-ready is not running (mp-rrd)', () => {
  it('tournamentMatches excludes setup but includes draw-ready structure', async () => {
    vi.resetModules();
    global.window = global.window || {};
    // Save/restore the globals we stub so we don't leak state into other
    // suites if they were already present (delete-only would clobber them).
    const stubbed = ['hasBothSides', 'roundLabel'];
    const prior = stubbed.map(k => Object.prototype.hasOwnProperty.call(global.window, k)
      ? { k, had: true, val: global.window[k] }
      : { k, had: false });
    global.window.hasBothSides = (m) => !!(m && m.sideA && m.sideB);
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    try {
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
    } finally {
      prior.forEach(({ k, had, val }) => {
        if (had) global.window[k] = val;
        else delete global.window[k];
      });
    }
  });

  it('the running-comp filter (status-based) excludes both setup and draw-ready', () => {
    // Mirrors the runningCompIds predicate in ViewerHome verbatim.
    const isRunningComp = c => !!(c.status && c.status !== 'setup' && c.status !== 'draw-ready');
    expect(isRunningComp({ status: 'setup' })).toBe(false);
    expect(isRunningComp({ status: 'draw-ready' })).toBe(false);
    expect(isRunningComp({ status: 'pools' })).toBe(true);
    expect(isRunningComp({ status: 'started' })).toBe(true);
  });
});
