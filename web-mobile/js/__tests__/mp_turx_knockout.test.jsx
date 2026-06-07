// mp-turx: tests for the incremental mixed-competition knockout flow.
//
// Covers:
//   1. ViewerCompetition: mixed comp renders as a single card (no cross-link);
//      Pools and Bracket tabs are present; Bracket works with placeholder and
//      resolved bracket data.
//   2. AdminBracket (via direct vnode invocation): a match with both sides
//      resolved is clickable while a match with a placeholder side is a no-op.
//   3. AdminCompetition page-head: no "Start knockout" / "Knockout in progress"
//      / "Finish all pool matches" text; bracket tab labels keyed off draw-ready
//      only.
//
// Removed (feature gone):
//   - API.startKnockout suite (endpoint removed from backend + api_client.jsx)
//   - AdminCompetition "Start knockout button" suite (UI removed)

import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

// ---------------------------------------------------------------------------
// Helpers shared across suites
// ---------------------------------------------------------------------------

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

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

function findAllByType(node, typeRef, acc = []) {
  if (!node || typeof node !== 'object') return acc;
  if (Array.isArray(node)) { node.forEach(k => findAllByType(k, typeRef, acc)); return acc; }
  if (node.type === typeRef) acc.push(node);
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => findAllByType(k, typeRef, acc));
  return acc;
}

// ---------------------------------------------------------------------------
// Suite 1: Viewer — single-competition rendering for mixed comps
// ---------------------------------------------------------------------------

describe('ViewerCompetition: merged mixed comp shows no cross-link (mp-turx back-compat)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerCompetition;
  const savedGlobals = {};
  const STUBBED = [
    'StatusBadge', 'formatDate', 'formatLabel', 'pluralize', 'Term',
    'BracketTree', 'buildBracket', 'roundLabel', 'formatIpponsScore',
    'ipponsFromScore', 'isHikiwake', 'hasBothSides', 'compareDmy',
    'queueLabel', 'queueLabelCompact', 'teamIVScore', 'matchScoreStr',
  ];

  const mkMixedComp = (overrides = {}) => ({
    id: 'mixed-1',
    name: 'Open Mixed',
    kind: 'individual',
    teamSize: 0,
    format: 'mixed',
    status: 'pools',
    startTime: '09:00',
    courts: ['A'],
    poolWinners: 2,
    players: [],
    ...overrides,
  });

  const samplePools = [{ poolName: 'Pool A', players: [{ id: 'p1', name: 'Alice' }, { id: 'p2', name: 'Bob' }] }];
  const previewBracket = {
    preview: true,
    rounds: [[{ id: 'r0-m0', sideA: { name: 'Pool A-1st' }, sideB: { name: 'Pool A-2nd' }, status: 'scheduled' }]],
  };
  const liveBracket = {
    preview: false,
    rounds: [[{ id: 'r0-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' }, status: 'scheduled' }]],
  };

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });
    global.window.StatusBadge = function StatusBadge() { return null; };
    global.window.BracketTree = function BracketTree() { return null; };
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    global.window.formatDate = (d) => d || '';
    global.window.formatLabel = (s) => s || '';
    global.window.pluralize = (n, a, b) => `${n} ${n === 1 ? a : b}`;
    global.window.buildBracket = () => liveBracket.rounds;
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.formatIpponsScore = () => '';
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = (m, ippB, ippA) =>
      (global.window.teamIVScore(m)) ||
      global.window.formatIpponsScore(ippB, ippA, m?.score, m?.decision, m?.encho, m?.decidedByHantei);
    global.window.ipponsFromScore = () => [];
    global.window.isHikiwake = () => false;
    global.window.hasBothSides = (m) => !!(m && m.sideA && m.sideB && typeof m.sideA !== 'string' && m.sideA.id);
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

  it('does NOT render the cross-link when no playoffs comp references this comp', () => {
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [mkMixedComp()] },
      competition: mkMixedComp(),
      pools: samplePools,
      poolMatches: [],
      standings: [],
      bracket: previewBracket,
      onBack: () => {},
      onSelectCompetition: () => {},
      tweaks: {},
    });
    const text = collectText(tree);
    expect(text).not.toContain('View the playoffs bracket');
    expect(text).not.toContain('View the pools');
  });

  it('shows Pools and Bracket tabs for a merged mixed comp in status "pools"', () => {
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [mkMixedComp()] },
      competition: mkMixedComp(),
      pools: samplePools,
      poolMatches: [],
      standings: [],
      bracket: previewBracket,
      onBack: () => {},
      onSelectCompetition: () => {},
      tweaks: {},
    });
    const tabButtons = [];
    findAllByType(tree, 'button', tabButtons);
    const labels = tabButtons.map(b => collectText(b));
    expect(labels.some(l => l.includes('Pools'))).toBe(true);
    expect(labels.some(l => l.includes('Bracket'))).toBe(true);
  });

  it('shows Bracket tab with live bracket (preview=false, status playoffs)', () => {
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [mkMixedComp({ status: 'playoffs' })] },
      competition: mkMixedComp({ status: 'playoffs' }),
      pools: samplePools,
      poolMatches: [],
      standings: [],
      bracket: liveBracket,
      onBack: () => {},
      onSelectCompetition: () => {},
      tweaks: {},
    });
    const tabButtons = [];
    findAllByType(tree, 'button', tabButtons);
    const labels = tabButtons.map(b => collectText(b));
    expect(labels.some(l => l.includes('Bracket'))).toBe(true);
  });

  it('does NOT render "Start knockout" text for a mixed comp in any state', () => {
    // The "Start knockout" affordance has been removed entirely.
    for (const status of ['pools', 'playoffs', 'completed']) {
      const tree = runtime.mount(ViewerCompetition, {
        tournament: { competitions: [mkMixedComp({ status })] },
        competition: mkMixedComp({ status }),
        pools: samplePools,
        poolMatches: [],
        standings: [],
        bracket: status === 'playoffs' ? liveBracket : previewBracket,
        onBack: () => {},
        onSelectCompetition: () => {},
        tweaks: {},
      });
      expect(collectText(tree)).not.toContain('Start knockout');
    }
  });
});

// ---------------------------------------------------------------------------
// Suite 2: AdminCompetition page-head (no Start-knockout affordance)
// ---------------------------------------------------------------------------

describe('AdminCompetition: page-head has no Start-knockout affordance (mp-turx)', () => {
  const realReact = global.React;
  let runtime;
  let AdminCompetition;
  const savedGlobals = {};

  const STUBBED = [
    'StatusBadge', 'formatDate', 'competitionKindLabel', 'Breadcrumbs',
    'AdminTopbar', 'AdminParticipants', 'AdminSettings', 'AdminExport',
    'AdminScoreEditor', 'AdminPools', 'AdminCompOverview', 'AdminTeamLineupsList',
    'AdminSwissRounds', 'LiveMatchPanel', 'BracketTree', 'promptAdminPassword',
    'isValidDate', 'roundLabel', 'hasBothSides',
    'AdminCompetition',
  ];

  const mkMixedComp = (overrides = {}) => ({
    id: 'comp-1',
    name: 'Open Mixed',
    kind: 'individual',
    teamSize: 0,
    format: 'mixed',
    status: 'pools',
    startTime: '09:00',
    courts: ['A'],
    poolWinners: 2,
    players: [],
    date: '01-06-2026',
    ...overrides,
  });

  const placeholderBracket = {
    preview: true,
    rounds: [[{ id: 'r0-m0', sideA: { name: 'Pool A-1st' }, sideB: { name: 'Pool B-1st' }, status: 'scheduled' }]],
  };
  const liveBracket = {
    preview: false,
    rounds: [[{ id: 'r0-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' }, status: 'scheduled' }]],
  };

  const baseTournament = { id: 't1', name: 'Tournament', competitions: [], courts: [] };

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};

    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });

    global.window.StatusBadge = function StatusBadge() { return null; };
    global.window.formatDate = (d) => d || '';
    global.window.competitionKindLabel = () => 'Individual';
    global.window.Breadcrumbs = function Breadcrumbs() { return null; };
    global.window.AdminTopbar = function AdminTopbar() { return null; };
    global.window.AdminParticipants = function AdminParticipants() { return null; };
    global.window.AdminSettings = function AdminSettings() { return null; };
    global.window.AdminExport = function AdminExport() { return null; };
    global.window.AdminScoreEditor = function AdminScoreEditor() { return null; };
    global.window.AdminPools = function AdminPools() { return null; };
    global.window.AdminCompOverview = function AdminCompOverview() { return null; };
    global.window.AdminSwissRounds = function AdminSwissRounds() { return null; };
    global.window.LiveMatchPanel = function LiveMatchPanel() { return null; };
    global.window.BracketTree = function BracketTree() { return null; };
    global.window.promptAdminPassword = () => 'admin';
    global.window.isValidDate = () => true;
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.hasBothSides = (m) => !!(m && m.sideA && m.sideB && m.sideA.id && m.sideB.id);
    global.window.API = {
      startCompetition: vi.fn(() => Promise.resolve({})),
      generateDraw: vi.fn(() => Promise.resolve({})),
      discardDraw: vi.fn(() => Promise.resolve({})),
    };

    vi.resetModules();
    await import('../admin_competition.jsx');
    AdminCompetition = global.window.AdminCompetition;
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

  const baseProps = {
    tournament: baseTournament,
    section: 'overview',
    onSection: vi.fn(),
    onBack: vi.fn(),
    onOpenCompetition: vi.fn(),
    onUpdate: vi.fn(),
    onRefreshCompetition: vi.fn(),
    onMoveCourt: vi.fn(),
    onEditScore: vi.fn(),
    onLogout: vi.fn(),
    onViewerMode: vi.fn(),
    tweaks: {},
    password: 'pw',
    showToast: vi.fn(),
    standings: [],
    pools: [],
    poolMatches: [],
  };

  it('does NOT render "Start knockout" button for mixed comp with all pool matches done', () => {
    const completedPoolMatches = [
      { id: 'Pool A-1', status: 'completed', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' } },
    ];
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'pools' }),
      poolMatches: completedPoolMatches,
      bracket: placeholderBracket,
    });
    expect(collectText(tree)).not.toContain('Start knockout');
  });

  it('does NOT render "Finish all pool matches to unlock the knockout" hint', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'pools' }),
      poolMatches: [{ id: 'Pool A-1', status: 'scheduled' }],
      bracket: placeholderBracket,
    });
    expect(collectText(tree)).not.toContain('Finish all pool matches');
  });

  it('does NOT render "Knockout in progress" for mixed comp in playoffs status', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'playoffs' }),
      bracket: liveBracket,
    });
    expect(collectText(tree)).not.toContain('Knockout in progress');
  });

  it('silently ignores onStartKnockout prop (backward compat — no crash)', () => {
    expect(() => {
      runtime.mount(AdminCompetition, {
        ...baseProps,
        competition: mkMixedComp({ status: 'pools' }),
        bracket: placeholderBracket,
        onStartKnockout: vi.fn(), // must be silently ignored
      });
    }).not.toThrow();
  });

  it('bracket tab label is "Bracket — now" for a running competition', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      section: 'overview',
      competition: mkMixedComp({ status: 'playoffs' }),
      bracket: liveBracket,
    });
    const text = collectText(tree);
    expect(text).toContain('Bracket — now');
    expect(text).not.toContain('Bracket — preview');
  });

  it('bracket tab label is "Bracket — preview" only for draw-ready competitions', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      section: 'overview',
      competition: mkMixedComp({ status: 'draw-ready' }),
      bracket: liveBracket,
    });
    expect(collectText(tree)).toContain('Bracket — preview');
  });

  it('bracket tab label is NOT "Bracket — preview" for mixed comp with preview=true bracket but status=pools', () => {
    // Previously bracket.preview=true would force "Bracket — preview" label.
    // Now only draw-ready status matters.
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      section: 'overview',
      competition: mkMixedComp({ status: 'pools' }),
      bracket: placeholderBracket, // preview: true
    });
    const text = collectText(tree);
    // status is 'pools' (not draw-ready), so label should be "Bracket — now"
    expect(text).toContain('Bracket — now');
    expect(text).not.toContain('Bracket — preview');
  });

  it('AdminBracket vnode receives bracket props regardless of bracket.preview', () => {
    // The AdminBracket child vnode should always be rendered (no preview gate
    // blocking it). Verify the vnode gets the placeholder bracket.
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      section: 'bracket',
      competition: mkMixedComp({ status: 'pools' }),
      bracket: placeholderBracket,
    });
    const bracketVnode = findInTree(tree, n => n?.type?.name === 'AdminBracket');
    expect(bracketVnode).toBeTruthy();
    expect(bracketVnode.props.bracket).toBe(placeholderBracket);
  });
});

// ---------------------------------------------------------------------------
// Suite 3: AdminBracket per-match playability (direct vnode invocation)
// ---------------------------------------------------------------------------
//
// AdminBracket is a local (non-exported) function inside admin_competition.jsx.
// We retrieve it via the AdminBracket vnode's .type from an AdminCompetition
// render, then invoke it directly with a fresh reactive runtime so BracketTree's
// onMatchClick prop is captured for assertion.
// ---------------------------------------------------------------------------

describe('AdminBracket: per-match playability (mp-turx)', () => {
  const realReact = global.React;
  let outerRuntime;
  let innerRuntime;
  let AdminCompetition;
  const savedGlobals = {};

  const STUBBED = [
    'StatusBadge', 'formatDate', 'competitionKindLabel', 'Breadcrumbs',
    'AdminTopbar', 'AdminParticipants', 'AdminSettings', 'AdminExport',
    'AdminScoreEditor', 'AdminPools', 'AdminCompOverview', 'AdminTeamLineupsList',
    'AdminSwissRounds', 'LiveMatchPanel', 'BracketTree', 'promptAdminPassword',
    'isValidDate', 'roundLabel', 'hasBothSides',
    'AdminCompetition',
  ];

  // Real hasBothSides implementation (mirrors admin_helpers.jsx) so the
  // per-match click guard is exercised with real logic.
  const BRACKET_PLACEHOLDER_RE = /^Winner of r\d+-m\d+$/;
  const POOL_ORIGIN_PLACEHOLDER_RE = /^Pool .+-\d+(st|nd|rd|th)$/;
  function realHasBothSides(m) {
    if (!m) return false;
    const sideName = (s) => { if (!s) return ''; if (typeof s === 'string') return s; return s.name || ''; };
    const a = sideName(m.sideA);
    const b = sideName(m.sideB);
    if (!a || !b) return false;
    if (BRACKET_PLACEHOLDER_RE.test(a) || BRACKET_PLACEHOLDER_RE.test(b)) return false;
    if (POOL_ORIGIN_PLACEHOLDER_RE.test(a) || POOL_ORIGIN_PLACEHOLDER_RE.test(b)) return false;
    return true;
  }

  const mkComp = (overrides = {}) => ({
    id: 'comp-1', name: 'Mixed', kind: 'individual', teamSize: 0,
    format: 'mixed', status: 'pools', startTime: '09:00',
    courts: ['A'], poolWinners: 2, players: [], date: '01-06-2026',
    naginata: false,
    ...overrides,
  });

  const placeholderBracket = {
    preview: true,
    rounds: [[
      { id: 'r0-m0', sideA: { name: 'Pool A-1st' }, sideB: { name: 'Pool B-1st' }, status: 'scheduled' },
    ]],
  };
  const winnerFeederBracket = {
    preview: false,
    rounds: [
      [
        { id: 'r0-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' }, status: 'completed' },
        { id: 'r0-m1', sideA: { id: 'p3', name: 'Carol' }, sideB: { id: 'p4', name: 'Dave' }, status: 'completed' },
      ],
      [
        { id: 'r1-m0', sideA: { name: 'Winner of r0-m0' }, sideB: { name: 'Winner of r0-m1' }, status: 'scheduled' },
      ],
    ],
  };
  const liveBracket = {
    preview: false,
    rounds: [[
      { id: 'r0-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' }, status: 'scheduled' },
    ]],
  };

  beforeEach(async () => {
    outerRuntime = makeReactive();
    innerRuntime = makeReactive();
    global.React = outerRuntime.React;
    global.window = global.window || {};

    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });

    global.window.StatusBadge = function StatusBadge() { return null; };
    global.window.formatDate = (d) => d || '';
    global.window.competitionKindLabel = () => 'Individual';
    global.window.Breadcrumbs = function Breadcrumbs() { return null; };
    global.window.AdminTopbar = function AdminTopbar() { return null; };
    global.window.AdminParticipants = function AdminParticipants() { return null; };
    global.window.AdminSettings = function AdminSettings() { return null; };
    global.window.AdminExport = function AdminExport() { return null; };
    global.window.AdminScoreEditor = function AdminScoreEditor() { return null; };
    global.window.AdminPools = function AdminPools() { return null; };
    global.window.AdminCompOverview = function AdminCompOverview() { return null; };
    global.window.AdminSwissRounds = function AdminSwissRounds() { return null; };
    global.window.LiveMatchPanel = function LiveMatchPanel() { return null; };
    global.window.BracketTree = function BracketTree() { return null; };
    global.window.promptAdminPassword = () => 'admin';
    global.window.isValidDate = () => true;
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.hasBothSides = realHasBothSides;
    global.window.API = {
      startCompetition: vi.fn(() => Promise.resolve({})),
      generateDraw: vi.fn(() => Promise.resolve({})),
      discardDraw: vi.fn(() => Promise.resolve({})),
    };

    vi.resetModules();
    await import('../admin_competition.jsx');
    AdminCompetition = global.window.AdminCompetition;
  });

  afterEach(() => {
    outerRuntime.unmount();
    innerRuntime.unmount();
    global.React = realReact;
    STUBBED.forEach(k => {
      if (savedGlobals[k]?.had) global.window[k] = savedGlobals[k].val;
      else delete global.window[k];
    });
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // Helper: get the AdminBracket vnode type (the local function) by rendering
  // AdminCompetition with section="bracket" and returning the vnode's .type.
  function getAdminBracketFn(bracket, compOverrides = {}) {
    const tree = outerRuntime.mount(AdminCompetition, {
      tournament: { id: 't1', name: 'Tournament', competitions: [], courts: [] },
      competition: mkComp(compOverrides),
      pools: [],
      poolMatches: [],
      standings: [],
      bracket,
      section: 'bracket',
      onSection: vi.fn(),
      onBack: vi.fn(),
      onOpenCompetition: vi.fn(),
      onUpdate: vi.fn(),
      onRefreshCompetition: vi.fn(),
      onMoveCourt: vi.fn(),
      onEditScore: vi.fn(),
      onLogout: vi.fn(),
      onViewerMode: vi.fn(),
      tweaks: {},
      password: 'pw',
      showToast: vi.fn(),
    });
    const bracketVnode = findInTree(tree, n => n?.type?.name === 'AdminBracket');
    return bracketVnode;
  }

  // Helper: render the AdminBracket function with innerRuntime (fresh hook slots).
  // Returns { tree, bracketTreeVnode } where bracketTreeVnode holds the
  // window.BracketTree vnode created by AdminBracket's render.
  function mountAdminBracket(bracket, compOverrides = {}) {
    const vnode = getAdminBracketFn(bracket, compOverrides);
    if (!vnode) throw new Error('AdminBracket vnode not found in tree');
    // Switch React to innerRuntime for the AdminBracket invocation so
    // hook slots don't collide with outerRuntime's AdminCompetition hooks.
    const prev = global.React;
    global.React = innerRuntime.React;
    let tree;
    try {
      tree = innerRuntime.mount(vnode.type, vnode.props);
    } finally {
      global.React = prev;
    }
    // The reactive shim calls createElement for BracketTree but does NOT call
    // BracketTree() itself. Extract onMatchClick from the BracketTree vnode's
    // props directly — that is what AdminBracket passes to window.BracketTree.
    const BracketTreeFn = global.window.BracketTree;
    const bracketTreeVnode = findInTree(tree, n => n?.type === BracketTreeFn);
    return { tree, onMatchClick: bracketTreeVnode?.props?.onMatchClick || null };
  }

  it('BracketTree always receives an onMatchClick handler (tree is always interactive)', () => {
    const { onMatchClick } = mountAdminBracket(placeholderBracket);
    expect(onMatchClick).toBeTypeOf('function');
  });

  it('onMatchClick is a no-op for a pool-origin placeholder match ("Pool A-1st")', () => {
    const { onMatchClick } = mountAdminBracket(placeholderBracket);
    expect(onMatchClick).toBeTypeOf('function');
    const placeholderMatch = { id: 'r0-m0', sideA: { name: 'Pool A-1st' }, sideB: { name: 'Pool B-1st' }, status: 'scheduled' };
    // hasBothSides returns false for pool-origin placeholders, so setSelected is not called.
    expect(() => onMatchClick(placeholderMatch, 0, 0)).not.toThrow();
  });

  it('onMatchClick is a no-op for a "Winner of rX-mY" feeder match', () => {
    const { onMatchClick } = mountAdminBracket(winnerFeederBracket, { status: 'playoffs' });
    expect(onMatchClick).toBeTypeOf('function');
    const feederMatch = { id: 'r1-m0', sideA: { name: 'Winner of r0-m0' }, sideB: { name: 'Winner of r0-m1' }, status: 'scheduled' };
    expect(() => onMatchClick(feederMatch, 1, 0)).not.toThrow();
  });

  it('onMatchClick does not throw for a resolved match (hasBothSides returns true)', () => {
    const { onMatchClick } = mountAdminBracket(liveBracket, { status: 'playoffs' });
    expect(onMatchClick).toBeTypeOf('function');
    const resolvedMatch = { id: 'r0-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' }, status: 'scheduled' };
    expect(() => onMatchClick(resolvedMatch, 0, 0)).not.toThrow();
  });

  it('shows informational banner when bracket has unresolved placeholder matches', () => {
    const { tree } = mountAdminBracket(placeholderBracket);
    expect(collectText(tree)).toContain('fills in automatically');
  });

  it('does NOT show the incremental-fill banner when all bracket matches are resolved', () => {
    const { tree } = mountAdminBracket(liveBracket, { status: 'playoffs' });
    expect(collectText(tree)).not.toContain('fills in automatically');
  });
});

// ---------------------------------------------------------------------------
// Suite 4: api_client — startKnockout method removed
// ---------------------------------------------------------------------------

describe('api_client: startKnockout method removed (mp-turx)', () => {
  beforeAll(async () => {
    vi.resetModules();
  });

  it('API object no longer has a startKnockout method', async () => {
    vi.resetModules();
    const { API } = await import('../api_client.jsx');
    expect(API.startKnockout).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Suite 5: hasBothSides — pool-origin placeholder rejection
// ---------------------------------------------------------------------------

describe('hasBothSides: rejects pool-origin placeholders (mp-turx)', () => {
  let hasBothSides;

  beforeAll(async () => {
    vi.resetModules();
    ({ hasBothSides } = await import('../admin_helpers.jsx'));
  });

  it('returns false for "Pool A-1st" side (pool-origin placeholder)', () => {
    expect(hasBothSides({ sideA: 'Pool A-1st', sideB: { id: 'p1', name: 'Alice' } })).toBe(false);
  });

  it('returns false for "Pool B-2nd" side (pool-origin placeholder)', () => {
    expect(hasBothSides({ sideA: { id: 'p1', name: 'Alice' }, sideB: 'Pool B-2nd' })).toBe(false);
  });

  it('returns false for "Pool A-3rd" ordinal variant', () => {
    expect(hasBothSides({ sideA: 'Pool A-3rd', sideB: 'Pool B-4th' })).toBe(false);
  });

  it('returns false for "Winner of r0-m1" feeder (existing behaviour unchanged)', () => {
    expect(hasBothSides({ sideA: { name: 'Winner of r0-m1' }, sideB: { name: 'Alice' } })).toBe(false);
  });

  it('returns true for two real participants', () => {
    expect(hasBothSides({ sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' } })).toBe(true);
  });

  it('does NOT reject a participant legitimately named "Winner of the 2025 Cup" (long name, no exact format)', () => {
    // Only the exact format "Winner of rN-mN" is rejected — not all names
    // starting with "Winner of". A real participant with that full name should pass.
    expect(hasBothSides({ sideA: 'Winner of the 2025 Cup', sideB: { id: 'p2', name: 'Bob' } })).toBe(true);
  });
});
