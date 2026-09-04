// mp-dej2: a Swiss competition's Overview tab must show its own round's
// matches, not the "Nothing scheduled" empty state.
//
// Swiss piggybacks on pool-matches.csv with a synthetic pool name
// ("Swiss-R1") but NEVER writes pools.csv, so the viewer payload always
// carries `pools: []` with the round's matches in `poolMatches`. Before this
// fix, ViewerCompetition's allMatches memo (viewer_competition.jsx) built its
// list by iterating the `pools` prop and filtering poolMatches by pool name —
// an empty `pools` array meant the loop iterated nothing, so a Swiss
// competition's Overview always rendered the empty state even with live
// matches, while the SAME matches showed correctly on the viewer home page
// (which uses the shared viewer_utils.compMatches builder). The fix collapses
// allMatches onto compMatches, which reads poolMatches directly.
//
// This also exercises the "Swiss-R1" label leak fix (viewer_utils.jsx's
// leagueAwareLabel folding in swissRoundLabel from pool_ids.jsx): once Swiss
// rows reach allMatches, they reach VSchedItem's poolLabel(m) too, so the
// synthetic pool name must not leak to spectators as a raw string.
//
// Harness follows viewer_competition_bye_results.jsx / _round_labels.jsx:
// mount ViewerCompetition, take the props it hands ViewerOverview, then mount
// ViewerOverview and inspect the rendered rows. The REAL window.hasBothSides
// (admin_helpers.jsx side-effect import) decides which rows reach the lists;
// it is deliberately NOT stubbed.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findAll, findInTree, collectText, hasClass } from './helpers/vdom.js';

// Stubbed purely to keep unrelated child components inert. hasBothSides is
// deliberately NOT in this list: the test needs the real implementation.
const STUBBED = [
  'StatusBadge', 'formatDate', 'formatLabel', 'pluralize', 'Term',
  'BracketTree', 'MatchCard', 'buildBracket', 'bronzeUnderFinalStyle',
  'API', 'LoadingSpinner',
];

function installStubs() {
  global.window = global.window || {};
  const saved = {};
  STUBBED.forEach((k) => {
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
  global.window.bronzeUnderFinalStyle = () => ({});
  global.window.API = { leagueStandings: () => Promise.resolve([]) };
  global.window.LoadingSpinner = function LoadingSpinner() { return null; };
  return saved;
}

function restoreStubs(saved) {
  STUBBED.forEach((k) => {
    if (saved[k]?.had) global.window[k] = saved[k].val;
    else delete global.window[k];
  });
}

describe('ViewerCompetition Overview shows Swiss round matches (mp-dej2)', () => {
  const realReact = global.React;
  let runtime;
  let saved;
  let ViewerCompetition, ViewerOverview, VSchedItem, poolLabel, normalizeCompetitionDetail;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    saved = installStubs();
    vi.resetModules();
    // Side-effect imports: publish the REAL window.hasBothSides and
    // window.leagueAwareLabel (the latter via viewer_utils.jsx's own
    // module-eval side effect, transitively pulled in by viewer.jsx) that the
    // viewer calls while building and rendering the match lists.
    await import('../admin_helpers.jsx');
    await import('../bracket.jsx');
    ({ ViewerCompetition, ViewerOverview, VSchedItem, poolLabel } = await import('../viewer.jsx'));
    ({ normalizeCompetitionDetail } = await import('../api_serializers.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    restoreStubs(saved);
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // A live Swiss round 1: two real pairings plus a bye (odd roster). Shaped
  // exactly like the engine emits it (internal/engine/swiss.go
  // buildSwissMatches): ids "Swiss-R<round>-<idx>", no poolName/phaseName
  // stamped (compMatches derives the pool name from the id), pools always [].
  const rawDetail = () => ({
    id: 'c1',
    name: 'Swiss Open',
    kind: 'individual',
    teamSize: 0,
    format: 'swiss',
    status: 'pools',
    startTime: '09:00',
    courts: ['A'],
    config: {
      players: [
        { id: 'p1', name: 'Alice', dojo: 'Dojo One' },
        { id: 'p2', name: 'Bob', dojo: 'Dojo Two' },
        { id: 'p3', name: 'Carol', dojo: 'Dojo Three' },
        { id: 'p4', name: 'Dave', dojo: 'Dojo Four' },
        { id: 'p5', name: 'Eve', dojo: 'Dojo Five' },
      ],
    },
    pools: [],
    poolMatches: [
      { id: 'Swiss-R1-0', sideA: 'Alice', sideB: 'Bob', status: 'scheduled', court: 'A', scheduledAt: '09:00' },
      { id: 'Swiss-R1-1', sideA: 'Carol', sideB: 'Dave', status: 'scheduled', court: 'A', scheduledAt: '09:10' },
      // The bye: Eve draws no opponent this round, auto-completed by the engine.
      { id: 'Swiss-R1-2', sideA: 'Eve', sideB: '', winner: 'Eve', status: 'completed', court: 'A', scheduledAt: '09:00' },
    ],
    standings: {},
  });

  // Mount ViewerCompetition on the Overview tab and return the props it built
  // for ViewerOverview (running/upcoming/recent among them).
  function overviewProps(detail) {
    const tree = runtime.mount(ViewerCompetition, {
      tournament: { competitions: [detail], mode: 'public' },
      competition: detail,
      pools: detail.pools,
      poolMatches: detail.poolMatches,
      standings: detail.standings,
      bracket: detail.bracket,
      onBack: () => {},
      tweaks: {},
      activeTab: 'overview',
    });
    const overview = findInTree(tree, (n) => n.type === ViewerOverview);
    expect(overview, 'expected the Overview tab to render ViewerOverview').not.toBeNull();
    return overview.props;
  }

  // Render ViewerOverview and return the whole rendered tree, plus the match
  // objects of the VSchedItem rows sitting under a given section heading.
  function renderOverview(props) {
    return runtime.mount(ViewerOverview, props);
  }

  function rowsUnderHeading(tree, heading) {
    const section = findInTree(tree, (n) => {
      const kids = [].concat(n.props?.children || []).filter((k) => k && typeof k === 'object');
      return kids.some((k) => hasClass(k, 'section-title') && collectText(k) === heading)
        && kids.some((k) => hasClass(k, 'vsched'));
    });
    if (!section) return null;
    return findAll(section, (n) => n.type === VSchedItem);
  }

  it('lists both real Swiss pairings under "Up next" and omits the bye', () => {
    const detail = normalizeCompetitionDetail(rawDetail());
    const props = overviewProps(detail);

    // The Overview no longer falls through to "Nothing scheduled": the loop
    // that used to walk the (always-empty for Swiss) `pools` prop is gone.
    expect(props.upcomingMatches.map((m) => m.id).sort()).toEqual(['Swiss-R1-0', 'Swiss-R1-1']);
    expect(props.upcomingMatches.some((m) => m.id === 'Swiss-R1-2')).toBe(false);
    expect(props.recentMatches.some((m) => m.id === 'Swiss-R1-2')).toBe(false);
    expect(props.runningMatches.some((m) => m.id === 'Swiss-R1-2')).toBe(false);

    const tree = renderOverview(props);

    const upNextRows = rowsUnderHeading(tree, 'Up next · 2');
    expect(upNextRows, 'expected an "Up next" section with two rows').not.toBeNull();
    expect(upNextRows.map((n) => n.props.m.id).sort()).toEqual(['Swiss-R1-0', 'Swiss-R1-1']);

    // The "Nothing scheduled" empty state must be absent.
    expect(collectText(tree)).not.toContain('Nothing scheduled');
  });

  // mp-dej2 follow-up: collapsing onto compMatches also pulled in the bronze
  // (3rd-place) knockout, which the hand-rolled loop never walked. Two halves,
  // and they pull in opposite directions, so both are pinned here:
  //   1. the bronze SHOULD reach the lists (it is a real bout, and every other
  //      surface already showed it), and
  //   2. it must NOT become the bracket auto-scroll target, because it renders
  //      outside BracketTree and useAutoScrollToMatch bails on a missing ref,
  //      which would silently stop the tab centring on anything.
  describe('bronze (3rd-place) knockout', () => {
    const bronzeDetail = () => ({
      id: 'c2',
      name: 'Naginata KO',
      kind: 'individual',
      teamSize: 0,
      format: 'knockout',
      status: 'knockout',
      startTime: '09:00',
      courts: ['A'],
      config: { players: [] },
      pools: [],
      poolMatches: [],
      bracket: {
        rounds: [[
          { id: 'r1-m1', sideA: 'Alice', sideB: 'Bob', status: 'scheduled', court: 'A', scheduledAt: '10:00' },
        ]],
        // Scheduled EARLIER than the final, so it sorts first and becomes
        // currentMatch: the reachable case, not a hypothetical one.
        thirdPlaceMatch: { id: 'bronze-1', sideA: 'Carol', sideB: 'Dave', status: 'scheduled', court: 'A', scheduledAt: '09:30' },
      },
      standings: {},
    });

    it('lists the bronze bout alongside the bracket matches', () => {
      const detail = normalizeCompetitionDetail(bronzeDetail());
      const props = overviewProps(detail);
      const ids = props.upcomingMatches.map((m) => m.id);
      expect(ids).toContain('bronze-1');
      expect(ids).toContain('r1-m1');
    });

    it('centres the Bracket tab on the final while the bronze is the current match', () => {
      // The REGRESSION this guards. The bronze is scheduled just before the
      // final, so once both are pending it is reliably currentMatch. Before the
      // collapse the bronze never reached this page and the tab centred on the
      // final; targeting the bronze instead centres on nothing, because it is
      // drawn outside BracketTree and useAutoScrollToMatch bails on a missing
      // ref. So the tab must fall back to the final, not merely skip.
      const detail = normalizeCompetitionDetail(bronzeDetail());
      const tree = runtime.mount(ViewerCompetition, {
        tournament: { competitions: [detail], mode: 'public' },
        competition: detail,
        pools: detail.pools,
        poolMatches: detail.poolMatches,
        standings: detail.standings,
        bracket: detail.bracket,
        onBack: () => {},
        tweaks: {},
        activeTab: 'bracket',
      });
      const bt = findInTree(tree, (n) => n.type === global.window.BracketTree);
      expect(bt).not.toBeNull();
      // "<id>::<timestamp>" — assert on the id half.
      const target = String(bt.props.autoScrollMatchId || '').split('::')[0];
      expect(target).toBe('r1-m1');
    });

    it('does not point the bracket auto-scroll at the bronze, which renders outside the tree', () => {
      const detail = normalizeCompetitionDetail(bronzeDetail());
      // Mount on the Bracket tab, where the auto-scroll effect runs.
      const tree = runtime.mount(ViewerCompetition, {
        tournament: { competitions: [detail], mode: 'public' },
        competition: detail,
        pools: detail.pools,
        poolMatches: detail.poolMatches,
        standings: detail.standings,
        bracket: detail.bracket,
        onBack: () => {},
        tweaks: {},
        activeTab: 'bracket',
      });
      const bt = findInTree(tree, (n) => n.type === global.window.BracketTree);
      expect(bt, 'expected the Bracket tab to render BracketTree').not.toBeNull();
      // The bronze is currentMatch here (09:30 beats the final's 10:00), so an
      // ungated effect would have stamped "bronze-1::<ts>" as the target.
      const target = bt.props.autoScrollMatchId;
      expect(String(target || '')).not.toContain('bronze-1');
    });
  });

  it('shows "Round 1", never the raw "Swiss-R1" id, as the row phase label', () => {
    // The reactive test runtime does not invoke VSchedItem's own function
    // body (same caveat as viewer_competition_bye_results.jsx / _round_labels):
    // a mounted VSchedItem vnode carries its props but is not expanded into
    // rendered children, so collectText(row) on it is empty. Assert instead
    // on the same primitive VSchedItem's render calls for its phase label
    // (poolLabel(m), viewer_utils.jsx), against the exact match objects the
    // "Up next" rows were built from.
    const detail = normalizeCompetitionDetail(rawDetail());
    const props = overviewProps(detail);
    const tree = renderOverview(props);

    const upNextRows = rowsUnderHeading(tree, 'Up next · 2');
    expect(upNextRows).not.toBeNull();
    expect(upNextRows.length).toBe(2);
    for (const row of upNextRows) {
      const m = row.props.m;
      expect(m.poolName).toBe('Swiss-R1');
      expect(poolLabel(m)).toBe('Round 1');
      expect(poolLabel(m)).not.toBe('Swiss-R1');
    }
  });
});
