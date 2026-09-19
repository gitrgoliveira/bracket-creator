// The viewer's "Recent results" orders by when the RESULT was written, not by
// scheduled time (mp-jnvl). A court that runs out of schedule order - a bout
// brought forward, a requeue, a court running behind - would otherwise headline
// this list with a bout played earlier, and disagree with the operator console,
// which answers the same question from the same shared rule
// (web-mobile/js/result_recency.jsx).
//
// Mounted through the real ViewerCompetition rather than testing the comparator
// alone: the comparator has its own unit tests, and what can regress here is
// the WIRING - that this list still passes through the shared rule instead of
// reverting to a local scheduledAt sort. Two-stage mount (competition, then
// overview) for the reason viewer_competition_bye_results.test.jsx states: the
// reactive runtime does not invoke child function components.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findAll, findInTree, collectText, hasClass } from './helpers/vdom.js';

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

describe('ViewerCompetition Recent results orders by result-write time (mp-jnvl)', () => {
  const realReact = global.React;
  let runtime, saved;
  let ViewerCompetition, ViewerOverview, VSchedItem, normalizeCompetitionDetail;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    saved = installStubs();
    vi.resetModules();
    await import('../admin_helpers.jsx');
    await import('../bracket.jsx');
    ({ ViewerCompetition, ViewerOverview, VSchedItem } = await import('../viewer.jsx'));
    ({ normalizeCompetitionDetail } = await import('../api_serializers.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    restoreStubs(saved);
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // Three bouts on one court. The bout in the EARLIEST slot was written LAST,
  // which is what a court running out of schedule order produces.
  const rawDetail = (withStamps) => ({
    id: 'c1',
    name: 'Knockout Cup',
    kind: 'individual',
    teamSize: 0,
    format: 'knockout',
    status: 'knockout',
    startTime: '09:00',
    courts: ['A'],
    config: {
      players: [
        { id: 'p1', name: 'Aiko Tanaka', dojo: 'Dojo One' },
        { id: 'p2', name: 'Bo Nakamura', dojo: 'Dojo Two' },
        { id: 'p3', name: 'Chie Sato', dojo: 'Dojo Three' },
        { id: 'p4', name: 'Dai Mori', dojo: 'Dojo Four' },
        { id: 'p5', name: 'Emi Kato', dojo: 'Dojo Five' },
        { id: 'p6', name: 'Fumi Ito', dojo: 'Dojo Six' },
      ],
    },
    pools: [],
    poolMatches: [],
    standings: {},
    bracket: {
      preview: false,
      rounds: [[
        {
          id: 'early-slot', sideA: 'Aiko Tanaka', sideB: 'Bo Nakamura',
          winner: 'Aiko Tanaka', status: 'completed', court: 'A',
          scheduledAt: '09:00', ipponsA: ['M', 'K'], ipponsB: ['D'],
          ...(withStamps ? { modifiedAt: 3000 } : {}),
        },
        {
          id: 'middle-slot', sideA: 'Chie Sato', sideB: 'Dai Mori',
          winner: 'Chie Sato', status: 'completed', court: 'A',
          scheduledAt: '09:10', ipponsA: ['M'], ipponsB: [],
          ...(withStamps ? { modifiedAt: 1000 } : {}),
        },
        {
          id: 'late-slot', sideA: 'Emi Kato', sideB: 'Fumi Ito',
          winner: 'Emi Kato', status: 'completed', court: 'A',
          scheduledAt: '09:20', ipponsA: ['K'], ipponsB: [],
          ...(withStamps ? { modifiedAt: 2000 } : {}),
        },
      ]],
    },
  });

  function mountCompetition(detail) {
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

  function recentRows(overviewProps) {
    const tree = runtime.mount(ViewerOverview, overviewProps);
    const section = findInTree(tree, (n) => {
      const kids = [].concat(n.props?.children || []).filter((k) => k && typeof k === 'object');
      return kids.some((k) => hasClass(k, 'section-title') && collectText(k) === 'Recent results')
        && kids.some((k) => hasClass(k, 'vsched'));
    });
    expect(section, 'expected a "Recent results" section with a match list').not.toBeNull();
    return findAll(section, (n) => n.type === VSchedItem).map((n) => n.props.m);
  }

  it('headlines the bout written last, even though it holds the earliest slot', () => {
    const detail = normalizeCompetitionDetail(rawDetail(true));
    const rows = recentRows(mountCompetition(detail));
    // Newest write first. A scheduledAt sort would put late-slot at the top.
    expect(rows.map((m) => m.id)).toEqual(['early-slot', 'late-slot', 'middle-slot']);
  });

  it('falls back to schedule order, latest first, when nothing carries a stamp', () => {
    const detail = normalizeCompetitionDetail(rawDetail(false));
    const rows = recentRows(mountCompetition(detail));
    // A competition whose results predate the stamp reads exactly as before.
    expect(rows.map((m) => m.id)).toEqual(['late-slot', 'middle-slot', 'early-slot']);
  });
});
