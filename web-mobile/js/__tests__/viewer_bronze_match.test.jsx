import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

// Walks a vnode tree and concatenates all string/number leaves. Child
// component vnodes (those with a function type) are recursively executed
// to expose their children (within a try/catch), matching the pattern used
// in other viewer tests (viewer_draw_ready.test.jsx, viewer.test.jsx).
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

// Depth-first search for all nodes matching a predicate. Function-type nodes
// (child components that the reactive shim does not auto-execute) are also
// executed within a try/catch so their returned subtree is searched too.
// This mirrors how collectText works and lets tests assert on the content
// returned by stub components (e.g. window.MatchCard).
function findAll(node, predicate, acc = []) {
  if (!node || typeof node !== 'object') return acc;
  if (Array.isArray(node)) {
    node.forEach(k => findAll(k, predicate, acc));
    return acc;
  }
  if (predicate(node)) acc.push(node);
  // Also search the subtree returned by function-type (child component) nodes.
  if (typeof node.type === 'function') {
    try {
      const p = { ...(node.props || {}) };
      if (node.children?.length) p.children = node.children.length === 1 ? node.children[0] : node.children;
      findAll(node.type(p), predicate, acc);
    } catch { /* skip unrenderable components */ }
  }
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => findAll(k, predicate, acc));
  return acc;
}

// Looks for a node with a given data-testid prop (works for native string
// elements where props are set directly, and for the reactive-shim shape).
// Also searches inside function-type component subtrees (see findAll above).
function findByTestId(node, testId) {
  const matches = findAll(node, n =>
    n && typeof n === 'object' && !Array.isArray(n) &&
    (n.props?.['data-testid'] === testId || n['data-testid'] === testId)
  );
  return matches[0] || null;
}

// mp-gy6g: the public viewer bracket tab must render a "3rd Place" labeled
// card for naginata competitions that carry bracket.thirdPlaceMatch.
// Kendo competitions (no thirdPlaceMatch) must be unchanged.
describe('ViewerCompetition bronze / 3rd-place match rendering (mp-gy6g)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerCompetition;
  const savedGlobals = {};

  // window globals captured at module-eval time in viewer_competition.jsx
  // (e.g. `const StatusBadge = window.StatusBadge`) MUST be set before import.
  // Lazily-looked-up globals (window.BracketTree, window.MatchCard used in JSX)
  // only need to be set before a render occurs, but listing them here keeps
  // the setup/teardown symmetric and avoids surprise bleed between test suites.
  const STUBBED = [
    'StatusBadge', 'formatDate', 'formatLabel', 'pluralize', 'Term',
    'BracketTree', 'MatchCard', 'buildBracket', 'roundLabel', 'formatIpponsScore',
    'ipponsFromScore', 'isHikiwake', 'hasBothSides', 'compareDmy',
    'queueLabel', 'queueLabelCompact', 'teamIVScore', 'matchScoreStr',
    'EmptyState',
  ];

  const mkComp = (overrides = {}) => ({
    id: 'nagi-1',
    name: 'Naginata Cup',
    kind: 'individual',
    teamSize: 0,
    format: 'playoffs',
    status: 'running',
    startTime: '09:00',
    courts: ['A'],
    players: [],
    ...overrides,
  });

  // A four-player bracket with a completed final and a completed 3rd-place match.
  const mkBracket = (withBronze) => ({
    rounds: [
      [
        { id: 'r0-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' }, status: 'completed', winner: { id: 'p1', name: 'Alice' } },
        { id: 'r0-m1', sideA: { id: 'p3', name: 'Carol' }, sideB: { id: 'p4', name: 'Dave' }, status: 'completed', winner: { id: 'p3', name: 'Carol' } },
      ],
      [
        { id: 'r1-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p3', name: 'Carol' }, status: 'completed', winner: { id: 'p1', name: 'Alice' } },
      ],
    ],
    ...(withBronze ? {
      thirdPlaceMatch: {
        id: 'm-bronze',
        sideA: { id: 'p2', name: 'Bob' },
        sideB: { id: 'p4', name: 'Dave' },
        status: 'completed',
        winner: { id: 'p2', name: 'Bob' },
      },
    } : {}),
  });

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });

    // Named stubs so findAllByType / findByTestId can identify them.
    global.window.StatusBadge = function StatusBadge() { return null; };
    global.window.BracketTree = function BracketTree() { return null; };
    // MatchCard stub: render a div that carries the data-testid and exposes
    // sideA/sideB names as text so collectText can find them.
    global.window.MatchCard = function MatchCard({ match }) {
      return {
        type: 'div',
        props: { 'data-testid': 'viewer-bronze-match-card' },
        children: [
          match?.sideA?.name || '',
          match?.sideB?.name || '',
        ],
      };
    };
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    global.window.EmptyState = function EmptyState(props) {
      return { type: 'div', props: { className: 'empty', ...props }, children: [props.icon, props.title, props.message].filter(Boolean) };
    };
    global.window.formatDate = (d) => d || '';
    global.window.formatLabel = (s) => s || '';
    global.window.pluralize = (n, a, b) => `${n} ${n === 1 ? a : b}`;
    global.window.buildBracket = () => [];
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.formatIpponsScore = () => '';
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = () => '';
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

  it('renders a "3rd Place" label and bronze MatchCard when thirdPlaceMatch is present', () => {
    const bracket = mkBracket(true);
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [] },
      competition: mkComp(),
      pools: [],
      poolMatches: [],
      standings: [],
      bracket,
      onBack: () => {},
      tweaks: {},
      activeTab: 'bracket',
      onTabChange: () => {},
    });

    const text = collectText(tree);
    // The "3rd Place" label must appear in the bracket tab.
    expect(text).toContain('3rd Place');

    // The viewer-bronze-section wrapper must be in the tree.
    const section = findByTestId(tree, 'viewer-bronze-section');
    expect(section).not.toBeNull();

    // The MatchCard stub for the bronze match must be rendered.
    const card = findByTestId(tree, 'viewer-bronze-match-card');
    expect(card).not.toBeNull();

    // Both competitor names must appear (via the MatchCard stub).
    expect(text).toContain('Bob');
    expect(text).toContain('Dave');
  });

  it('does NOT render a bronze section for kendo competitions (no thirdPlaceMatch)', () => {
    const bracket = mkBracket(false);
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [] },
      competition: mkComp(),
      pools: [],
      poolMatches: [],
      standings: [],
      bracket,
      onBack: () => {},
      tweaks: {},
      activeTab: 'bracket',
      onTabChange: () => {},
    });

    const section = findByTestId(tree, 'viewer-bronze-section');
    expect(section).toBeNull();

    const card = findByTestId(tree, 'viewer-bronze-match-card');
    expect(card).toBeNull();

    const text = collectText(tree);
    expect(text).not.toContain('3rd Place');
  });

  // mp-gy6g review fix: the bronze card's onClick used to stamp roundIndex:
  // -1, but resolveRoundIndex (admin_helpers.jsx) treats any negative
  // roundIndex as absent and falls back to 0, mis-grouping the bronze match
  // into round 0 for round-indexed consumers (team lineup fetch, displays,
  // queue grouping). It must match compMatches' convention of "one past the
  // final" (rounds.length) instead.
  it('stamps the bronze match roundIndex as rounds.length (one past the final), not -1', () => {
    const bracket = mkBracket(true);
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [] },
      competition: mkComp(),
      pools: [],
      poolMatches: [],
      standings: [],
      bracket,
      onBack: () => {},
      tweaks: {},
      activeTab: 'bracket',
      onTabChange: () => {},
    });

    // Find the raw MatchCard vnode for the bronze match (before the stub
    // executes) so its onClick prop is available unmodified.
    const cardVnode = findAll(tree, n => n && n.type === global.window.MatchCard)[0];
    expect(cardVnode).toBeTruthy();
    cardVnode.props.onClick();

    // The click sets selectedMatch, which mounts MatchViewerModal with the
    // clicked match as a prop. Find it by name (real import, not stubbed)
    // and read the roundIndex without invoking the component.
    const updated = runtime.currentTree();
    const modalVnode = findAll(updated, n => n && typeof n.type === 'function' && n.type.name === 'MatchViewerModal')[0];
    expect(modalVnode).toBeTruthy();
    expect(modalVnode.props.match.roundIndex).toBe(bracket.rounds.length);
    expect(modalVnode.props.match.roundIndex).not.toBe(-1);
  });
});
