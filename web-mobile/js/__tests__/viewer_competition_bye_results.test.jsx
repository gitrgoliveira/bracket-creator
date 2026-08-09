// Regression: a BYE must never appear in the viewer's "Recent results" list.
//
// A knockout competition with a non-power-of-two roster carries structural
// byes: internal/engine/bracket.go auto-resolves them at generation time
// (`sideA == "" && sideB != ""` → Winner = sideB, Status = completed), so they
// arrive at the client as COMPLETED matches WITH a winner. The Recent-results
// filter in viewer_competition.jsx used to test only `status === "completed"
// && m.winner`, so those byes rendered as finished results reading
// "TBD vs <name>" under a Final badge, with nothing to say that nobody was
// ever scheduled. A bye is bracket structure, not a result; it stays
// discoverable in the Bracket tab, where the entrant renders as an unopposed
// slot feeding the next round. (Verified in the browser: it is NOT tagged
// "BYE" there. bracket.jsx gates that tag on score.type === "bye", which the
// Go side never sets, so it is unreachable from a server payload.)
//
// The filter's guard is the shared `hasBothSides` predicate, NOT a hand-rolled
// `m.sideA && m.sideB`: normalizeMatch (api_serializers.jsx) substitutes a
// TRUTHY `{id:"",name:""}` for a missing side, which the naive check waves
// through. This test therefore:
//   * builds the RAW server payload and runs it through the real
//     normalizeCompetitionDetail, so the absent side takes the exact shape the
//     browser sees (asserted below, so a payload drift can't weaken the test);
//   * uses the REAL window.hasBothSides (admin_helpers.jsx is imported for its
//     side-effect window publication) rather than a stub that returns true,
//     which would make the assertion vacuous.
//
// ViewerCompetition owns the running/upcoming/recent computation and hands the
// result to ViewerOverview, which renders the rows. The reactive test runtime
// does not invoke child function components, so the assertion is made in two
// stages: mount ViewerCompetition, take the props it built for ViewerOverview,
// then mount ViewerOverview with them and read the VSchedItem rows actually
// rendered under the "Recent results" heading.
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

describe('ViewerCompetition Recent results excludes byes', () => {
  const realReact = global.React;
  let runtime;
  let saved;
  let ViewerCompetition, ViewerOverview, VSchedItem, normalizeCompetitionDetail;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    saved = installStubs();
    vi.resetModules();
    // Side-effect imports: publish the REAL window.hasBothSides (the predicate
    // under test) and the real window.roundLabel / window.matchScoreStr the
    // viewer calls while building and rendering the match lists.
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

  // Three entrants → one real first-round match plus one structural bye,
  // shaped exactly as internal/engine/bracket.go emits it: the bye's empty
  // side is the JSON empty string "", and it is already completed with the
  // lone competitor as winner.
  const rawDetail = () => ({
    id: 'c1',
    name: 'Knockout Cup',
    kind: 'individual',
    teamSize: 0,
    format: 'playoffs',
    status: 'playoffs',
    startTime: '09:00',
    courts: ['A'],
    config: {
      players: [
        { id: 'p1', name: 'Aiko Tanaka', dojo: 'Dojo One' },
        { id: 'p2', name: 'Bo Nakamura', dojo: 'Dojo Two' },
        { id: 'p3', name: 'Chie Sato', dojo: 'Dojo Three' },
      ],
    },
    pools: [],
    poolMatches: [],
    standings: {},
    bracket: {
      preview: false,
      rounds: [[
        {
          id: 'm-r0-0',
          sideA: 'Aiko Tanaka',
          sideB: 'Bo Nakamura',
          winner: 'Aiko Tanaka',
          status: 'completed',
          court: 'A',
          scheduledAt: '09:00',
          scoreA: 'MK',
          scoreB: 'D',
        },
        {
          // The bye: no sideA was ever drawn.
          id: 'm-r0-1',
          sideA: '',
          sideB: 'Chie Sato',
          winner: 'Chie Sato',
          status: 'completed',
          court: 'A',
          scheduledAt: '09:10',
        },
      ]],
    },
  });

  // Mount ViewerCompetition on the Overview tab and return the props it built
  // for ViewerOverview (recentMatches among them).
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

  // Render ViewerOverview and return the match objects of the VSchedItem rows
  // sitting under the "Recent results" heading.
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

  it('normalizeMatch turns the bye\'s absent side into a TRUTHY {id:"",name:""}', () => {
    const detail = normalizeCompetitionDetail(rawDetail());
    const bye = detail.bracket.rounds[0][1];
    // This is why the filter must use hasBothSides: `m.sideA && m.sideB` is
    // true here. If this ever fails, the regression test below has stopped
    // exercising the case that motivated the fix.
    expect(bye.sideA).toEqual({ id: '', name: '' });
    expect(!!(bye.sideA && bye.sideB)).toBe(true);
    expect(window.hasBothSides(bye)).toBe(false);
    expect(window.hasBothSides(detail.bracket.rounds[0][0])).toBe(true);
  });

  it('lists the real completed match and omits the auto-completed bye', () => {
    const detail = normalizeCompetitionDetail(rawDetail());
    const rows = recentRows(mountCompetition(detail));

    expect(rows.map((m) => m.id)).toEqual(['m-r0-0']);
    // Named explicitly: the bye's lone competitor must not headline a result.
    expect(rows.some((m) => m.id === 'm-r0-1')).toBe(false);
    expect(rows.every((m) => m.sideA?.name && m.sideB?.name)).toBe(true);
  });
});
