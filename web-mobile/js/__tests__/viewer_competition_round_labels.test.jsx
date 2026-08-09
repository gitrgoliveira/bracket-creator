// mp-u37s: the public competition page's own bracket-row stamping.
//
// ViewerCompetition builds its OWN allMatches list (viewer_competition.jsx):
// a second copy of the "flatten bracket.rounds and stamp round/phaseName" loop,
// separate from viewer_utils.compMatches. compMatches is pinned by
// bracket_round_label_agreement.test.jsx; this copy was not, yet the Overview
// rows it feeds are the exact surface the round-label mismatch was reported on.
// Reverting this file's window.bracketRoundLabel(...) call to
// window.roundLabel(ri, …) left the whole suite green.
//
// Separate file rather than an addition to viewer_competition_bye_results.jsx:
// that file pins one specific regression (a structural bye must not appear as a
// Recent result) around a 3-entrant fixture chosen so the FILTER is forced to
// key on the sides. This invariant needs a 5-entrant collapsed-bye draw where a
// match's effective round differs from its raw one, and it asserts labels, not
// membership. Sharing a fixture would weaken both.
//
// Harness follows viewer_competition_bye_results.jsx: mount ViewerCompetition,
// take the props it hands ViewerOverview. The REAL window.bracketRoundLabel is
// used (side-effect import of bracket.jsx); it is deliberately NOT stubbed.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findInTree } from './helpers/vdom.js';

// Stubbed purely to keep unrelated child components inert. bracketRoundLabel
// and hasBothSides are deliberately NOT in this list: the test needs the real
// implementations (the former is under test, the latter decides which rows
// reach the Overview lists at all).
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

describe('ViewerCompetition stamps bracket rows with the EFFECTIVE round (mp-u37s)', () => {
  const realReact = global.React;
  let runtime;
  let saved;
  let ViewerCompetition, ViewerOverview, normalizeCompetitionDetail;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    saved = installStubs();
    vi.resetModules();
    // Side-effect imports: the REAL window.hasBothSides and the REAL
    // window.roundLabel / window.bracketRoundLabel the viewer calls while
    // building its match lists.
    await import('../admin_helpers.jsx');
    await import('../bracket.jsx');
    ({ ViewerCompetition, ViewerOverview } = await import('../viewer.jsx'));
    ({ normalizeCompetitionDetail } = await import('../api_serializers.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    restoreStubs(saved);
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // The 5-entrant individual knockout as the engine persists it: 3 backend
  // rounds, one of them entirely structural byes, so mp-7f2w collapses it and
  // stamps an effective round (displayRound, counted from the final) that does
  // not follow the raw slot in bracket.rounds. Same shape as
  // bracket_round_label_agreement.test.jsx, in raw server-payload form so
  // normalizeCompetitionDetail produces exactly what the browser holds.
  //
  //   m-r1-0  backend round 0, displayRound 2 → raw "Quarterfinals",
  //                                             effective "Semifinals"  ← the case
  //   m-r1-3  backend round 0, displayRound 3 → both say "Quarterfinals" ← control
  const rawDetail = () => ({
    id: 'c1',
    name: 'Knockout5',
    kind: 'individual',
    teamSize: 0,
    format: 'playoffs',
    status: 'playoffs',
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
    poolMatches: [],
    standings: {},
    bracket: {
      preview: false,
      rounds: [
        [
          { id: 'm-r1-0', sideA: 'Alice', sideB: 'Bob', displayRound: 2, feeders: ['', ''], status: 'running', court: 'A', scheduledAt: '09:20' },
          { id: 'm-r1-1', sideA: '', sideB: '', hidden: true },
          { id: 'm-r1-2', sideA: 'Carol', sideB: '', hidden: true },
          { id: 'm-r1-3', sideA: 'Dave', sideB: 'Eve', displayRound: 3, feeders: ['', ''], status: 'completed', winner: 'Dave', court: 'A', scheduledAt: '09:00' },
        ],
        [
          { id: 'm-r2-0', sideA: 'Winner of r3-m0', sideB: '', hidden: true },
          { id: 'm-r2-1', sideA: 'Carol', sideB: 'Winner of r3-m3', displayRound: 2, feeders: ['', 'm-r1-3'], status: 'scheduled', court: 'A', scheduledAt: '09:40' },
        ],
        [
          { id: 'm-r3-0', sideA: 'Winner of r2-m0', sideB: 'Winner of r2-m1', displayRound: 1, feeders: ['m-r1-0', 'm-r2-1'], status: 'scheduled', court: 'A', scheduledAt: '10:00' },
        ],
      ],
    },
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

  it('calls the bye-collapsed match a Semifinal on the rows it builds', () => {
    const props = overviewProps(normalizeCompetitionDetail(rawDetail()));
    const running = props.runningMatches;
    expect(running.map((m) => m.id)).toEqual(['m-r1-0']);
    // The raw rule would name backend round 0 of 3 a quarterfinal. Pinned here
    // so this assertion is provably discriminating: the two labels differ.
    expect(window.roundLabel(0, 3)).toBe('Quarterfinals');
    expect(running[0].round).toBe('Semifinals');
    expect(running[0].phaseName).toBe('Semifinals');
  });

  it('leaves a match whose effective round matches its raw one alone', () => {
    const props = overviewProps(normalizeCompetitionDetail(rawDetail()));
    const recent = props.recentMatches;
    expect(recent.map((m) => m.id)).toEqual(['m-r1-3']);
    // Genuinely a quarterfinal (displayRound 3 of 3 rounds), so both rules agree.
    expect(recent[0].round).toBe('Quarterfinals');
    expect(recent[0].phaseName).toBe('Quarterfinals');
  });

  it('keeps roundIndex RAW so lineup fetches still key on the backend round', () => {
    const props = overviewProps(normalizeCompetitionDetail(rawDetail()));
    // The label moved to the effective round; the index must NOT follow it.
    expect(props.runningMatches[0].roundIndex).toBe(0);
    expect(props.recentMatches[0].roundIndex).toBe(0);
  });
});
