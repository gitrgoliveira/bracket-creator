// mp-turx: tests for the in-place "Start knockout" mixed-competition flow.
//
// Covers:
//   1. API.startKnockout client method (URL, method, password header, error handling)
//   2. ViewerCompetition: mixed comp renders as a single card (no cross-link);
//      Pools and Bracket tabs are present; Bracket works with preview and live bracket
//   3. AdminCompetition: "Start knockout" button visible for mixed comp with status
//      "pools" and all pool matches complete; hidden when pools are not done;
//      hidden once status is "playoffs"; bracket becomes scoreable when preview=false

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
// Suite 1: API.startKnockout client method
// ---------------------------------------------------------------------------

describe('API.startKnockout', () => {
  let API;
  let originalFetch;

  const mockFetch = (status, body) =>
    vi.fn(() =>
      Promise.resolve({
        ok: status >= 200 && status < 300,
        status,
        json: () => Promise.resolve(body),
      })
    );

  beforeAll(async () => {
    ({ API } = await import('../api_client.jsx'));
  });

  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('POSTs to /api/competitions/:id/start-knockout', async () => {
    const comp = { id: 'comp-1', status: 'playoffs', bracket: { rounds: [] } };
    global.fetch = mockFetch(200, comp);
    const result = await API.startKnockout('comp-1', 'secret');
    const [url, opts] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/comp-1/start-knockout');
    expect(opts.method).toBe('POST');
    expect(result).toEqual(comp);
  });

  it('sends X-Tournament-Password header', async () => {
    global.fetch = mockFetch(200, {});
    await API.startKnockout('comp-42', 'my-password');
    const [, opts] = global.fetch.mock.calls[0];
    expect(opts.headers['X-Tournament-Password']).toBe('my-password');
  });

  it('throws with server message on 409 (precondition unmet)', async () => {
    global.fetch = mockFetch(409, { error: 'pools not complete' });
    await expect(API.startKnockout('comp-1', 'pw')).rejects.toThrow('pools not complete');
  });

  it('throws with server message on 404 (competition missing)', async () => {
    global.fetch = mockFetch(404, { error: 'competition not found' });
    await expect(API.startKnockout('missing', 'pw')).rejects.toThrow('competition not found');
  });

  it('falls back to generic message when server returns no error field', async () => {
    global.fetch = mockFetch(500, {});
    await expect(API.startKnockout('comp-1', 'pw')).rejects.toThrow('Failed to start knockout');
  });
});

// ---------------------------------------------------------------------------
// Suite 2: Viewer — single-competition rendering for mixed comps
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
    'queueLabel', 'queueLabelCompact',
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
    rounds: [[{ id: 'r0-m0', sideA: { name: 'Pool A 1st' }, sideB: { name: 'Pool A 2nd' }, status: 'scheduled' }]],
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

  it('does NOT render the cross-link when no playoffs comp references this comp', () => {
    // New merged mixed comp: no separate playoffs comp in the tournament
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

  it('shows Bracket tab with live bracket after start-knockout (preview=false, status playoffs)', () => {
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

});

// ---------------------------------------------------------------------------
// Suite 4: AdminCompetition — "Start knockout" button logic
// ---------------------------------------------------------------------------

describe('AdminCompetition: Start knockout button (mp-turx)', () => {
  const realReact = global.React;
  let runtime;
  // AdminCompetition is exported only via window (not a named ES export);
  // we access it from global.window after the module is imported.
  let AdminCompetition;
  const savedGlobals = {};

  // Globals that admin_competition.jsx reads from window at module-eval time
  const STUBBED = [
    'StatusBadge', 'formatDate', 'competitionKindLabel', 'Breadcrumbs',
    'AdminTopbar', 'AdminParticipants', 'AdminSettings', 'AdminExport',
    'AdminScoreEditor', 'AdminPools', 'AdminCompOverview', 'AdminTeamLineupsList',
    'AdminSwissRounds', 'LiveMatchPanel', 'BracketTree', 'promptAdminPassword',
    'isValidDate', 'roundLabel', 'hasBothSides',
    // These are also globals that admin_competition.jsx uses for React hooks
    // via module-level destructuring (const { useState: useStateA, ... } = React)
    // so React must already be set when the module loads.
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

  const completedPoolMatches = [
    { id: 'Pool A-1', status: 'completed', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' } },
    { id: 'Pool A-2', status: 'completed', sideA: { id: 'p3', name: 'Carol' }, sideB: { id: 'p1', name: 'Alice' } },
  ];
  const incompletePoolMatches = [
    { id: 'Pool A-1', status: 'completed', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' } },
    { id: 'Pool A-2', status: 'scheduled', sideA: { id: 'p3', name: 'Carol' }, sideB: { id: 'p1', name: 'Alice' } },
  ];

  const samplePools = [{ poolName: 'Pool A', players: [] }];
  const previewBracket = {
    preview: true,
    rounds: [[{ id: 'r0-m0', sideA: { name: 'Pool A 1st' }, sideB: { name: 'Pool A 2nd' }, status: 'scheduled' }]],
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

    // Save existing globals
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });

    // Stub globals that admin_competition.jsx reads from window
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
    global.window.hasBothSides = (m) => !!(m && m.sideA && m.sideB);
    global.window.API = {
      startCompetition: vi.fn(() => Promise.resolve({})),
      generateDraw: vi.fn(() => Promise.resolve({})),
      discardDraw: vi.fn(() => Promise.resolve({})),
      startKnockout: vi.fn(() => Promise.resolve({})),
    };

    vi.resetModules();
    // admin_competition.jsx registers itself on window.AdminCompetition;
    // it is NOT a named ES export. Import for side effects, then read from window.
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
  };

  it('shows "Start knockout" button when mixed, status pools, all pool matches complete', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'pools' }),
      pools: samplePools,
      poolMatches: completedPoolMatches,
      bracket: previewBracket,
      onStartKnockout: vi.fn(),
    });
    const text = collectText(tree);
    expect(text).toContain('Start knockout');
  });

  it('does NOT show "Start knockout" when pool matches are not all complete', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'pools' }),
      pools: samplePools,
      poolMatches: incompletePoolMatches,
      bracket: previewBracket,
      onStartKnockout: vi.fn(),
    });
    const text = collectText(tree);
    expect(text).not.toContain('Start knockout');
    expect(text).toContain('Finish all pool matches');
  });

  it('does NOT show "Start knockout" when no pool matches exist yet', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'pools' }),
      pools: samplePools,
      poolMatches: [],
      bracket: previewBracket,
      onStartKnockout: vi.fn(),
    });
    const text = collectText(tree);
    expect(text).not.toContain('Start knockout');
  });

  it('shows "Knockout in progress" when status is playoffs', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'playoffs' }),
      pools: samplePools,
      poolMatches: completedPoolMatches,
      bracket: liveBracket,
      onStartKnockout: vi.fn(),
    });
    const text = collectText(tree);
    expect(text).toContain('Knockout in progress');
    expect(text).not.toContain('Start knockout');
  });

  it('bracket section vnode receives preview=true bracket when pools not done', () => {
    // The reactive shim does not recurse into child components (AdminBracket
    // is a local function, not the root component here). We verify that the
    // AdminBracket child vnode receives the preview bracket (preview: true),
    // which is the gate for disabling scoring inside AdminBracket.
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'pools' }),
      pools: samplePools,
      poolMatches: incompletePoolMatches,
      bracket: previewBracket,
      section: 'bracket',
      onStartKnockout: vi.fn(),
    });
    // Find the AdminBracket vnode (it's a local function, identified by
    // the bracket prop it receives).
    const bracketVnode = findInTree(tree, n => n?.type?.name === 'AdminBracket');
    expect(bracketVnode).toBeTruthy();
    expect(bracketVnode.props.bracket.preview).toBe(true);
  });

  it('bracket section vnode receives preview=false bracket after start-knockout', () => {
    // Same principle: verify the AdminBracket child vnode gets the live
    // (non-preview) bracket after status transitions to "playoffs".
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'playoffs' }),
      pools: samplePools,
      poolMatches: completedPoolMatches,
      bracket: liveBracket,
      section: 'bracket',
      onStartKnockout: vi.fn(),
    });
    const bracketVnode = findInTree(tree, n => n?.type?.name === 'AdminBracket');
    expect(bracketVnode).toBeTruthy();
    expect(bracketVnode.props.bracket.preview).toBeFalsy();
  });

  it('calls onStartKnockout with comp id when "Start knockout" is clicked', () => {
    const onStartKnockout = vi.fn(() => Promise.resolve());
    const onSection = vi.fn();
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'pools' }),
      pools: samplePools,
      poolMatches: completedPoolMatches,
      bracket: previewBracket,
      onStartKnockout,
      onSection,
    });
    const btn = findInTree(tree, n => n?.type === 'button' && collectText(n).includes('Start knockout'));
    expect(btn).toBeTruthy();
    btn.props.onClick();
    expect(onStartKnockout).toHaveBeenCalledWith('comp-1');
  });

  it('does NOT show old "Create playoff bracket" wording for mixed comps', () => {
    const tree = runtime.mount(AdminCompetition, {
      ...baseProps,
      competition: mkMixedComp({ status: 'pools' }),
      pools: samplePools,
      poolMatches: completedPoolMatches,
      bracket: previewBracket,
      onStartKnockout: vi.fn(),
    });
    const text = collectText(tree);
    expect(text).not.toContain('Create playoff bracket');
    expect(text).not.toContain('Playoffs competition');
  });
});
