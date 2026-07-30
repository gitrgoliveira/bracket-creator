import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

// mp-94v0: ScoreEditorModal picks the engi flag editor purely from
// `m.compEngi` (admin_scoring_individual.jsx: `const isEngi = !!m.compEngi`),
// derived synchronously with NO fallback fetch of the competition config.
// Any surface that builds its own match objects and hands them to a score
// editor must therefore stamp compEngi, or an engi bout silently opens the
// kendo ippon editor and the save is rejected by recordEngiMatch (0+0 flags).
//
// viewer_utils.jsx (the tournament/home path) stamped it; the COMPETITION-page
// surfaces (viewer_competition.jsx, viewer_standings.jsx) did not, so the same
// match routed correctly from the home page and incorrectly from its own
// competition page.
//
// These tests drive the real click closures rather than grepping the source:
// the reactive stub's createElement does not invoke function components, so a
// child vnode still carries the exact onMatchClick closure the parent built.

// Walk a vnode tree collecting every node matching a predicate.
function findAll(node, pred, acc = []) {
  if (node == null || typeof node !== 'object') return acc;
  if (Array.isArray(node)) { node.forEach(k => findAll(k, pred, acc)); return acc; }
  if (pred(node)) acc.push(node);
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => findAll(k, pred, acc));
  return acc;
}

// The enrichment closures only. PoolsViewer forwards the RAW onMatchClick to
// its LeagueMatrix child (pass-through), so filtering by identity against the
// callback we supplied keeps just the `() => onMatchClick(enriched)` wrappers
// the component built. Invoking the raw one would push undefined and mask a
// genuine miss.
const matchClickHandlers = (tree, raw) =>
  findAll(tree, n => typeof n.props?.onMatchClick === 'function')
    .map(n => n.props.onMatchClick)
    .filter(h => h !== raw);

const STUBBED = [
  'StatusBadge', 'formatDate', 'formatLabel', 'pluralize', 'Term',
  'BracketTree', 'MatchCard', 'buildBracket', 'roundLabel', 'formatIpponsScore',
  'ipponsFromScore', 'isHikiwake', 'hasBothSides', 'compareDmy',
  'queueLabel', 'queueLabelCompact', 'teamIVScore', 'matchScoreStr',
  'matchStateCell', 'engiPairParts', 'bronzeUnderFinalStyle', 'API', 'LoadingSpinner',
];

function installStubs() {
  global.window = global.window || {};
  const saved = {};
  STUBBED.forEach(k => {
    saved[k] = Object.prototype.hasOwnProperty.call(global.window, k)
      ? { had: true, val: global.window[k] }
      : { had: false };
  });
  global.window.StatusBadge = function StatusBadge() { return null; };
  global.window.BracketTree = function BracketTree() { return null; };
  global.window.MatchCard = function MatchCard() { return null; };
  global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
  global.window.formatDate = (d) => d || '';
  global.window.formatLabel = (s) => s || '';
  global.window.pluralize = (n, a, b) => `${n} ${n === 1 ? a : b}`;
  global.window.buildBracket = () => [];
  global.window.roundLabel = (i) => `Round ${i + 1}`;
  global.window.formatIpponsScore = () => '';
  global.window.teamIVScore = () => null;
  global.window.matchScoreStr = () => '';
  global.window.matchStateCell = () => '-';
  global.window.ipponsFromScore = () => [];
  global.window.isHikiwake = () => false;
  global.window.hasBothSides = (m) => !!(m && m.sideA && m.sideB);
  global.window.compareDmy = (a, b) => String(a).localeCompare(String(b));
  global.window.queueLabel = () => '';
  global.window.queueLabelCompact = () => null;
  global.window.engiPairParts = (n) => String(n || '').split(' - ');
  global.window.bronzeUnderFinalStyle = () => ({});
  // LeagueStandingsViewer fetches standings on mount. The numbered match list
  // under test is driven by the poolMatches prop, not this response, so an
  // empty resolve is enough to get past the effect.
  global.window.API = { leagueStandings: () => Promise.resolve([]) };
  global.window.LoadingSpinner = function LoadingSpinner() { return null; };
  return saved;
}

function restoreStubs(saved) {
  STUBBED.forEach(k => {
    if (saved[k]?.had) global.window[k] = saved[k].val;
    else delete global.window[k];
  });
}

// An engi pair is ONE participant whose two member names are joined by " - ".
const engiPlayers = [
  { id: 'p1', name: 'Aya Mori - Ken Sato', dojo: 'Dojo1' },
  { id: 'p2', name: 'Rin Abe - Yu Ito', dojo: 'Dojo2' },
];
const poolMatch = {
  id: 'Pool A-0',
  poolName: 'Pool A',
  sideA: { id: 'p1', name: 'Aya Mori - Ken Sato' },
  sideB: { id: 'p2', name: 'Rin Abe - Yu Ito' },
  status: 'scheduled',
};

describe('compEngi stamping on competition-page match objects (mp-94v0)', () => {
  const realReact = global.React;
  let runtime;
  let saved;
  let PoolsViewer, LeagueStandingsViewer, LeagueMatrix, ViewerCompetition;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    saved = installStubs();
    vi.resetModules();
    ({ PoolsViewer, LeagueStandingsViewer, LeagueMatrix, ViewerCompetition } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    restoreStubs(saved);
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // ── PoolsViewer: the Pools tab numbered match list ────────────────────────
  describe('PoolsViewer (Pools tab)', () => {
    const mount = (engi) => {
      const captured = [];
      const onMatchClick = (m) => captured.push(m);
      const tree = runtime.mount(PoolsViewer, {
        pools: [{ poolName: 'Pool A', players: engiPlayers }],
        standings: {},
        poolMatches: [poolMatch],
        tweaks: { showDojo: false },
        competition: { id: 'c1', name: 'Kata Open', kind: 'individual', teamSize: 0, format: 'mixed', engi },
        onMatchClick,
      });
      return { tree, captured, onMatchClick };
    };

    it('stamps compEngi:true on a clicked pool match of an engi competition', () => {
      const { tree, captured, onMatchClick } = mount(true);
      const handlers = matchClickHandlers(tree, onMatchClick);
      expect(handlers.length, 'expected at least one clickable pool match row').toBeGreaterThan(0);
      handlers.forEach(h => h());
      expect(captured.length).toBeGreaterThan(0);
      captured.forEach(m => expect(m.compEngi, `match ${m.id} must carry compEngi`).toBe(true));
    });

    it('stamps compEngi:false for a non-engi competition (no false routing to the flag editor)', () => {
      const { tree, captured, onMatchClick } = mount(false);
      matchClickHandlers(tree, onMatchClick).forEach(h => h());
      expect(captured.length).toBeGreaterThan(0);
      captured.forEach(m => expect(m.compEngi).toBe(false));
    });
  });

  // ── LeagueStandingsViewer: the League tab numbered match list ─────────────
  describe('LeagueStandingsViewer (League tab)', () => {
    it('stamps compEngi:true on a clicked league match of an engi competition', async () => {
      const captured = [];
      const onMatchClick = (m) => captured.push(m);
      runtime.mount(LeagueStandingsViewer, {
        competition: { id: 'c1', name: 'Kata Open', kind: 'individual', teamSize: 0, format: 'league', engi: true, players: engiPlayers },
        poolMatches: [poolMatch],
        tweaks: { showDojo: false },
        onMatchClick,
      });
      // The standings fetch resolves in a microtask; until it does the
      // component renders only the initial-load spinner.
      await Promise.resolve();
      await Promise.resolve();
      const tree = runtime.currentTree();
      const handlers = matchClickHandlers(tree, onMatchClick);
      expect(handlers.length, 'expected at least one clickable league match row').toBeGreaterThan(0);
      handlers.forEach(h => h());
      expect(captured.length).toBeGreaterThan(0);
      captured.forEach(m => expect(m.compEngi).toBe(true));
    });
  });

  // ── LeagueMatrix: the cross-table cell click ──────────────────────────────
  describe('LeagueMatrix (cross-table cell)', () => {
    it('stamps compEngi on a cell-click match when isEngi is threaded in', () => {
      const captured = [];
      runtime.mount(LeagueMatrix, {
        pool: { poolName: 'Pool A', players: engiPlayers },
        matches: [poolMatch],
        tweaks: { showDojo: false },
        onMatchClick: (m) => captured.push(m),
        isEngi: true,
      });
      // The matrix builds its enriched object in enrichMatch and fires it from
      // handleCellClick; assert through the component's own contract rather
      // than reaching into internals.
      const tree = runtime.currentTree();
      const cells = findAll(tree, n => typeof n.props?.onClick === 'function');
      expect(cells.length, 'expected clickable matrix cells').toBeGreaterThan(0);
      cells.forEach(c => c.props.onClick());
      expect(captured.length, 'expected a cell click to surface a match').toBeGreaterThan(0);
      captured.forEach(m => expect(m.compEngi).toBe(true));
    });
  });

  // ── ViewerCompetition: the Bracket tab ────────────────────────────────────
  describe('ViewerCompetition (Bracket tab)', () => {
    const mkComp = (engi) => ({
      id: 'c1',
      name: 'Kata Open',
      kind: 'individual',
      teamSize: 0,
      format: 'playoffs',
      status: 'playoffs',
      startTime: '09:00',
      courts: ['A'],
      players: engiPlayers,
      engi,
    });
    const bracket = {
      preview: false,
      rounds: [[{ id: 'r0-m0', sideA: engiPlayers[0], sideB: engiPlayers[1], status: 'scheduled' }]],
    };

    it('stamps compEngi on a bracket match selected from the Bracket tab', () => {
      const tree = runtime.mount(ViewerCompetition, {
        tournament: { competitions: [mkComp(true)], mode: 'self-run' },
        competition: mkComp(true),
        pools: [],
        poolMatches: [],
        standings: [],
        bracket,
        onBack: () => {},
        tweaks: {},
        activeTab: 'bracket',
      });
      // BracketTree is stubbed, so its vnode still carries the click closure
      // ViewerCompetition built.
      const trees = findAll(tree, n => n.type === global.window.BracketTree && typeof n.props?.onMatchClick === 'function');
      expect(trees.length, 'expected the Bracket tab to render a BracketTree with onMatchClick').toBeGreaterThan(0);
      trees[0].props.onMatchClick(bracket.rounds[0][0], 0, 0, 1);
      // The click sets selectedMatch, which is handed to MatchViewerModal as
      // `match`. Find that vnode in the re-rendered tree.
      const modals = findAll(runtime.currentTree(), n => n.props?.match?.id === 'r0-m0');
      expect(modals.length, 'expected the selected match to reach a match modal').toBeGreaterThan(0);
      expect(modals[0].props.match.compEngi).toBe(true);
    });
  });
});
