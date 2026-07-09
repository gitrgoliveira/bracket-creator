import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { applyFilters, matchHighlightedBy, competitionKindLabel, isSwissFinalStandings, swissStandingsHeading, isFollowedPlayer, compMatches, subBoutLabel, TournamentInfo, isHttpURL, linkBase, isNonPublicOrigin } from '../viewer.jsx';
import { formatDate } from '../ui.jsx';
import { makeReactive } from './helpers/reactive_react.js';

// Walks a vnode tree and concatenates all string/number leaves. Child
// component vnodes (e.g. TermV) are NOT executed by the reactive shim,
// but their literal children (the term text) still live in props.children,
// so this captures everything MatchDetailCard renders itself. Mirrors the
// collectText helper in reset.test.jsx.
function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

// Depth-first search for the first vnode matching predicate. Mirrors the
// helper in reset.test.jsx; used to assert props (e.g. style) on a rendered
// element, which collectText (text-only) can't see.
function findInTree(node, predicate) {
  if (!node || typeof node !== 'object') return null;
  // Arrays appear wherever the component renders a .map() (e.g. the sub-rows),
  // so recurse into them rather than treating the array itself as a vnode.
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

describe('Viewer Utils', () => {
  describe('formatDate', () => {
    it('should format canonical DD-MM-YYYY correctly', () => {
      // DD-MM-YYYY is the canonical storage format that the viewer reads
      // directly from the API. Pinning this exercises the post-DMY-flip
      // path that prod callers actually use.
      expect(formatDate('12-05-2026')).toBe('12 May 2026');
    });
    it('should also accept ISO YYYY-MM-DD format', () => {
      expect(formatDate('2026-05-12')).toBe('12 May 2026');
    });
    it('should return default for missing date', () => {
      expect(formatDate('')).toBe('Date TBA');
    });
  });

  describe('competitionKindLabel', () => {
    it('should return correct label for individual', () => {
      expect(competitionKindLabel({ kind: 'individual' })).toBe('Individual');
    });
    it('should return correct label for teams', () => {
      expect(competitionKindLabel({ kind: 'team' })).toBe('Teams');
    });
  });

  describe('applyFilters', () => {
    const matches = [
      { id: 'm1', compId: 'c1', sideA: { id: 'p1', name: 'Alice', dojo: 'Dojo A' }, sideB: { id: 'p2', name: 'Bob', dojo: 'Dojo B' } },
      { id: 'm2', compId: 'c2', sideA: { id: 'p3', name: 'Charlie', dojo: 'Dojo A' }, sideB: { id: 'p4', name: 'David', dojo: 'Dojo C' } }
    ];

    it('should filter by competition', () => {
      const filtered = applyFilters(matches, [], '', 'c1');
      expect(filtered.length).toBe(1);
      expect(filtered[0].id).toBe('m1');
    });

    it('should filter by picked players', () => {
      const filtered = applyFilters(matches, [{ id: 'p1' }], '', 'all');
      expect(filtered.length).toBe(1);
      expect(filtered[0].id).toBe('m1');
    });

    it('should filter by dojo text', () => {
      const filtered = applyFilters(matches, [], 'Dojo A', 'all');
      expect(filtered.length).toBe(2);
    });

    it('matches the free-text filter against a side competitor number/tag (mp-ce66)', () => {
      const tagged = [
        { id: 'm1', compId: 'c1', sideA: { id: 'p1', name: 'Alice', dojo: 'Dojo A', number: 'A1' }, sideB: { id: 'p2', name: 'Bob', dojo: 'Dojo B', number: 'A2' } },
        { id: 'm2', compId: 'c2', sideA: { id: 'p3', name: 'Charlie', dojo: 'Dojo A', number: 'B1' }, sideB: { id: 'p4', name: 'David', dojo: 'Dojo C', number: 'B2' } },
      ];
      const filtered = applyFilters(tagged, [], 'A1', 'all');
      expect(filtered.length).toBe(1);
      expect(filtered[0].id).toBe('m1');
    });

    it('matches by name when picked id differs from match side id (UUID vs name)', () => {
      const uuidMatches = [
        { id: 'm1', compId: 'c1', sideA: { id: 'Alice', name: 'Alice' }, sideB: { id: 'Bob', name: 'Bob' } },
      ];
      const picked = [{ id: 'uuid-aaa', name: 'Alice' }];
      const filtered = applyFilters(uuidMatches, picked, '', 'all');
      expect(filtered.length).toBe(1);
    });

    it('matches by name on sideB', () => {
      const m = [{ id: 'm1', compId: 'c1', sideA: { id: 'x', name: 'X' }, sideB: { id: 'Bob', name: 'Bob' } }];
      const filtered = applyFilters(m, [{ id: 'uuid-bbb', name: 'Bob' }], '', 'all');
      expect(filtered.length).toBe(1);
    });
  });

  describe('matchHighlightedBy', () => {
    const match = { sideA: { id: 'p1', name: 'Alice', dojo: 'Dojo A' }, sideB: { id: 'p2', name: 'Bob', dojo: 'Dojo B' } };

    it('should return true if player is picked', () => {
      expect(matchHighlightedBy(match, [{ id: 'p1' }], '')).toBe(true);
      expect(matchHighlightedBy(match, [{ id: 'p3' }], '')).toBe(false);
    });

    it('should return true if dojo matches', () => {
      expect(matchHighlightedBy(match, [], 'Dojo B')).toBe(true);
      expect(matchHighlightedBy(match, [], 'Dojo C')).toBe(false);
    });

    it('highlights when the free-text filter matches a competitor number/tag (mp-ce66)', () => {
      const tagged = { sideA: { id: 'p1', name: 'Alice', dojo: 'Dojo A', number: 'A1' }, sideB: { id: 'p2', name: 'Bob', dojo: 'Dojo B', number: 'A2' } };
      expect(matchHighlightedBy(tagged, [], 'A2')).toBe(true);
      expect(matchHighlightedBy(tagged, [], 'A9')).toBe(false);
    });

    it('highlights by name when picked id differs from match side id', () => {
      expect(matchHighlightedBy(match, [{ id: 'uuid-xxx', name: 'Alice' }], '')).toBe(true);
      expect(matchHighlightedBy(match, [{ id: 'uuid-xxx', name: 'Nobody' }], '')).toBe(false);
    });
  });

  // T192 (US13: FR-050e): Swiss standings page header logic. The
  // viewer flips its header text from "Standings after round N" to
  // "Final standings" once every configured round has been played
  // out. Only then does it declare a winner. Pure helpers so the
  // conditional can be pinned without mounting SwissStandingsViewer
  // (the React vitest setup stubs hooks at component level).

  describe('isSwissFinalStandings', () => {
    const mkComp = (overrides) => ({
      format: 'swiss',
      swissRounds: 4,
      swissCurrentRound: 4,
      ...overrides,
    });
    const completedR4 = [
      { id: 'Swiss-R4-0', status: 'completed' },
      { id: 'Swiss-R4-1', status: 'completed' },
    ];

    it('true when on the final round and every match in it is completed', () => {
      expect(isSwissFinalStandings(mkComp(), completedR4)).toBe(true);
    });

    it('false when not yet on the final round', () => {
      expect(isSwissFinalStandings(mkComp({ swissCurrentRound: 3 }), completedR4)).toBe(false);
    });

    it('false when final round has any incomplete match', () => {
      const incompleteR4 = [
        { id: 'Swiss-R4-0', status: 'completed' },
        { id: 'Swiss-R4-1', status: 'running' },
      ];
      expect(isSwissFinalStandings(mkComp(), incompleteR4)).toBe(false);
    });

    it('false when final round has not been generated yet (no matches)', () => {
      // current=total but pool-matches list is empty. This only
      // happens transiently between "Generate next round" returning
      // 201 and the SSE-driven refetch. We must not declare a
      // winner during that window.
      expect(isSwissFinalStandings(mkComp(), [])).toBe(false);
    });

    it('false when format !== swiss', () => {
      expect(isSwissFinalStandings(mkComp({ format: 'mixed' }), completedR4)).toBe(false);
      expect(isSwissFinalStandings(mkComp({ format: 'playoffs' }), completedR4)).toBe(false);
    });

    it('false for null/missing competition', () => {
      expect(isSwissFinalStandings(null, completedR4)).toBe(false);
      expect(isSwissFinalStandings(undefined, completedR4)).toBe(false);
    });
  });

  // mp-7e6: isFollowedPlayer: UUID-first match with name fallback.
  // Pins the two lookup paths so opponents are never resolved to the
  // followed player's own side.
  describe('isFollowedPlayer', () => {
    const followed = { id: 'uuid-alice', name: 'Alice' };

    it('matches by UUID when IDs are equal', () => {
      expect(isFollowedPlayer({ id: 'uuid-alice', name: 'Alice' }, followed)).toBe(true);
    });

    it('falls back to case-insensitive name when IDs differ (legacy/team fixture)', () => {
      expect(isFollowedPlayer({ id: '', name: 'alice' }, followed)).toBe(true);
      expect(isFollowedPlayer({ id: '', name: 'ALICE' }, followed)).toBe(true);
    });

    it('returns false when neither id nor name matches', () => {
      expect(isFollowedPlayer({ id: 'uuid-bob', name: 'Bob' }, followed)).toBe(false);
    });

    it('returns false for null/missing args', () => {
      expect(isFollowedPlayer(null, followed)).toBe(false);
      expect(isFollowedPlayer({ id: 'uuid-alice', name: 'Alice' }, null)).toBe(false);
    });
  });

  // mp-7e6: compMatches: pool phase/poolName derivation for flat viewer
  // poolMatches that don't carry phase/poolName from the API.
  describe('compMatches', () => {
    const mkComp = (overrides) => ({
      id: 'comp1',
      name: 'Test Comp',
      kind: 'individual',
      teamSize: 0,
      status: 'pools',
      bracket: { rounds: [] },
      ...overrides,
    });

    it('derives phase="pool" and poolName from match ID when not set by API', () => {
      const c = mkComp({ poolMatches: [{ id: 'Pool A-0', status: 'scheduled' }] });
      const ms = compMatches(c);
      expect(ms.length).toBe(1);
      expect(ms[0].phase).toBe('pool');
      expect(ms[0].poolName).toBe('Pool A');
    });

    it('preserves existing poolName when already set', () => {
      const c = mkComp({ poolMatches: [{ id: 'Pool B-1', phase: 'pool', poolName: 'Pool B', status: 'scheduled' }] });
      const ms = compMatches(c);
      expect(ms[0].poolName).toBe('Pool B');
    });

    it('handles DH suffix correctly (Pool A-DH-0)', () => {
      const c = mkComp({ poolMatches: [{ id: 'Pool A-DH-0', status: 'scheduled' }] });
      const ms = compMatches(c);
      expect(ms[0].poolName).toBe('Pool A');
    });

    it('returns empty for setup status competition', () => {
      const c = mkComp({ status: 'setup', poolMatches: [{ id: 'Pool A-0', status: 'scheduled' }] });
      expect(compMatches(c)).toEqual([]);
    });

    it('excludes tiebreak/daihyosen bouts from poolCount and poolPosition', () => {
      // 3-player RR = 3 regular matches; the -TB- and -DH- bouts must NOT count
      // toward "Match N of M" (they are not part of the round-robin schedule).
      const c = mkComp({
        poolMatches: [
          { id: 'Pool A-0', status: 'completed' },
          { id: 'Pool A-1', status: 'completed' },
          { id: 'Pool A-2', status: 'completed' },
          { id: 'Pool A-TB-0', status: 'scheduled' },
          { id: 'Pool A-DH-0', status: 'scheduled' },
        ],
      });
      const ms = compMatches(c);
      const regular = ms.filter(m => /Pool A-\d+$/.test(m.id));
      const tb = ms.find(m => m.id === 'Pool A-TB-0');
      const dh = ms.find(m => m.id === 'Pool A-DH-0');
      // Every regular bout sees a true RR total of 3.
      expect(regular.map(m => m.poolCount)).toEqual([3, 3, 3]);
      expect(regular.map(m => m.poolPosition)).toEqual([1, 2, 3]);
      // TB/DH bouts get no position (undefined → the row hides "Match N of M").
      expect(tb.poolCount).toBeUndefined();
      expect(tb.poolPosition).toBeUndefined();
      expect(dh.poolCount).toBeUndefined();
      expect(dh.poolPosition).toBeUndefined();
    });

    // mp-116: compKind/teamSize must be threaded onto every match so that
    // MatchDetailCard.isTeam and MatchViewerModal.isTeam evaluate correctly.
    it('threads compKind and teamSize onto pool matches for team comps', () => {
      const c = mkComp({
        kind: 'team',
        teamSize: 3,
        poolMatches: [{ id: 'Pool A-0', status: 'completed' }],
      });
      const ms = compMatches(c);
      expect(ms[0].compKind).toBe('team');
      expect(ms[0].teamSize).toBe(3);
    });

    it('zeroes out compKind/teamSize for pool-daihyosen matches (pool-DH guard)', () => {
      const c = mkComp({
        kind: 'team',
        teamSize: 3,
        poolMatches: [
          { id: 'Pool A-0', status: 'completed' },
          { id: 'Pool A-DH-0', status: 'completed' },
        ],
      });
      const ms = compMatches(c);
      const normal = ms.find(m => m.id === 'Pool A-0');
      const dh = ms.find(m => m.id === 'Pool A-DH-0');
      expect(normal.compKind).toBe('team');
      expect(normal.teamSize).toBe(3);
      expect(dh.compKind).toBe('');
      expect(dh.teamSize).toBe(0);
    });

    // A pool tiebreaker ('-TB-') is also a single ippon-shobu rep bout, so it
    // must route to the individual editor just like a daihyosen. Guarding
    // against the DH-only regression flagged on PR #330 (compKind/teamSize was
    // gated on isPoolDaihyosenID, which misses '-TB-').
    it('zeroes out compKind/teamSize for pool-tiebreaker matches (pool-TB guard)', () => {
      const c = mkComp({
        kind: 'team',
        teamSize: 3,
        poolMatches: [
          { id: 'Pool A-0', status: 'completed' },
          { id: 'Pool A-TB-0', status: 'completed' },
        ],
      });
      const ms = compMatches(c);
      const normal = ms.find(m => m.id === 'Pool A-0');
      const tb = ms.find(m => m.id === 'Pool A-TB-0');
      expect(normal.compKind).toBe('team');
      expect(normal.teamSize).toBe(3);
      expect(tb.compKind).toBe('');
      expect(tb.teamSize).toBe(0);
      expect(tb.compEngi).toBe(false);
    });

    // Flat viewer-API poolMatches may carry an explicit poolName: undefined.
    // The derived poolName/phaseName must survive the `...m` spread (regression:
    // spreading m last clobbered the derived pool name with undefined).
    it('derives poolName even when the match carries poolName: undefined', () => {
      const c = mkComp({
        kind: 'team',
        teamSize: 3,
        poolMatches: [{ id: 'Pool A-0', status: 'completed', poolName: undefined, phaseName: undefined }],
      });
      const m = compMatches(c).find(x => x.id === 'Pool A-0');
      expect(m.poolName).toBe('Pool A');
      expect(m.phaseName).toBe('Pool A');
      expect(m.phase).toBe('pool');
    });

    it('threads compKind and teamSize onto bracket matches for team comps', () => {
      global.window = global.window || {};
      const savedRoundLabel = global.window.roundLabel;
      global.window.roundLabel = (i, _total) => `Round ${i + 1}`;
      try {
        const c = mkComp({
          kind: 'team',
          teamSize: 5,
          bracket: { rounds: [[{ id: 'QF-0', status: 'completed' }]] },
        });
        const ms = compMatches(c);
        const bm = ms.find(m => m.phase === 'bracket');
        expect(bm).toBeTruthy();
        expect(bm.compKind).toBe('team');
        expect(bm.teamSize).toBe(5);
      } finally {
        if (savedRoundLabel === undefined) delete global.window.roundLabel;
        else global.window.roundLabel = savedRoundLabel;
      }
    });
  });

  describe('swissStandingsHeading', () => {
    it('"Final standings" when all rounds complete', () => {
      const c = { format: 'swiss', swissRounds: 3, swissCurrentRound: 3 };
      const matches = [
        { id: 'Swiss-R3-0', status: 'completed' },
        { id: 'Swiss-R3-1', status: 'completed' },
      ];
      expect(swissStandingsHeading(c, matches)).toBe('Final standings');
    });

    it('"Standings after round N" while in progress', () => {
      const c = { format: 'swiss', swissRounds: 4, swissCurrentRound: 2 };
      expect(swissStandingsHeading(c, [])).toBe('Standings after round 2');
    });

    it('"Standings: pending" when no round has been generated yet', () => {
      const c = { format: 'swiss', swissRounds: 4, swissCurrentRound: 0 };
      expect(swissStandingsHeading(c, [])).toBe('Standings: pending');
    });
  });

  // mp-8sw: subBoutLabel: the team sub-bout center label. The daihyosen
  // (rep bout) is stored with sentinel position -1 and must render as
  // "Daihyosen", not the literal "Match -1" the position||index fallback
  // would produce. Shared by both viewer sub-row sites; the Hantei marker
  // beside it is a trivial `sub.decidedByHantei` gate verified manually.
  describe('subBoutLabel', () => {
    it('renders "Daihyosen" for the sentinel position -1 (not "Match -1")', () => {
      expect(subBoutLabel({ position: -1 }, 0)).toBe('Daihyosen');
      expect(subBoutLabel({ position: -1 }, 4)).toBe('Daihyosen');
    });

    it('renders "Match N" using the stored position for normal bouts', () => {
      expect(subBoutLabel({ position: 1 }, 0)).toBe('Match 1');
      expect(subBoutLabel({ position: 3 }, 0)).toBe('Match 3');
    });

    it('falls back to index+1 when position is missing/zero', () => {
      expect(subBoutLabel({}, 0)).toBe('Match 1');
      expect(subBoutLabel({ position: 0 }, 2)).toBe('Match 3');
      expect(subBoutLabel(undefined, 1)).toBe('Match 2');
    });
  });
});

// mp-8sw: MatchDetailCard team sub-rows: render-level proof that the
// daihyosen row labels as "Daihyosen" (not "Match -1") and shows the
// "Hantei" marker only when sub.decidedByHantei. Asserts the actual
// rendered tree, complementing the subBoutLabel unit tests above.
describe('MatchDetailCard team sub-rows (mp-8sw)', () => {
  const realReact = global.React;
  let runtime;
  let MatchDetailCard;
  // Preserve any pre-existing window globals we stub so we can restore exact
  // state in afterEach. vi.restoreAllMocks() only undoes vi.spyOn, NOT direct
  // `global.window.x = vi.fn()` assignments: without this the mocked globals
  // leak into later suites and make failures order-dependent.
  const savedGlobals = {};
  const STUBBED = ['formatIpponsScore', 'ipponsFromScore', 'teamIVScore', 'matchScoreStr', 'isHikiwake'];

  const mkTeamMatch = (subs) => ({
    compKind: 'team',
    status: 'completed',
    court: 'A',
    phase: 'bracket',
    round: 'Final',
    sideA: { id: 'tA', name: 'Team A' },
    sideB: { id: 'tB', name: 'Team B' },
    // Present (truthy) so the component skips window.ipponsFromScore.
    ipponsA: [],
    ipponsB: [],
    subResults: subs,
  });

  let TeamScoreboard;
  let IndividualScore;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => { savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k) ? { had: true, val: global.window[k] } : { had: false }; });
    global.window.formatIpponsScore = vi.fn(() => '3-2');
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = (m, ippB, ippA) =>
      (global.window.teamIVScore(m)) ||
      global.window.formatIpponsScore(ippB, ippA, m?.score, m?.decision, m?.encho, m?.decidedByHantei);
    global.window.ipponsFromScore = vi.fn(() => []);
    global.window.isHikiwake = vi.fn(() => false);
    vi.resetModules();
    ({ MatchDetailCard } = await import('../viewer.jsx'));
    ({ TeamScoreboard, IndividualScore } = await import('../match_scoreboard.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    // Restore the exact prior state of the window globals we stubbed so they
    // don't leak into other suites (vi.restoreAllMocks does not cover direct
    // property assignments).
    STUBBED.forEach(k => {
      if (savedGlobals[k]?.had) global.window[k] = savedGlobals[k].val;
      else delete global.window[k];
    });
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // mp-13y: MatchDetailCard now delegates the scoreboard to the shared
  // match_scoreboard.jsx components: TeamScoreboard (team) / IndividualScore
  // (individual). These are child component vnodes which the reactive shim does
  // not expand, so we assert delegation (type + props). The scoreboard's own
  // rendering (DH banner, Hantei, ippon slots, IV/PW summary) is covered by
  // match_scoreboard.test.jsx.
  function findVnode(node, pred) {
    if (!node || typeof node !== 'object') return null;
    if (Array.isArray(node)) { for (const k of node) { const f = findVnode(k, pred); if (f) return f; } return null; }
    if (pred(node)) return node;
    const kids = node.children || node.props?.children || [];
    for (const k of [].concat(kids)) { const f = findVnode(k, pred); if (f) return f; }
    return null;
  }

  it('delegates a team match to TeamScoreboard (showDH when a DH sub exists)', () => {
    const tree = runtime.mount(MatchDetailCard, {
      match: mkTeamMatch([
        { position: 1, ipponsA: ['M'], ipponsB: [], decidedByHantei: false },
        { position: -1, ipponsA: ['K'], ipponsB: [], decidedByHantei: true },
      ]),
      onClose: null,
    });
    const sb = findVnode(tree, n => n.type === TeamScoreboard);
    expect(sb).toBeTruthy();
    expect(sb.props.showDH).toBe(true);
    expect(sb.props.subResults.length).toBe(2);
    expect(findVnode(tree, n => n.type === IndividualScore)).toBeNull();
  });

  it('delegates an individual match to IndividualScore (not TeamScoreboard)', () => {
    const match = {
      compKind: 'individual', teamSize: 0, status: 'completed', court: 'A',
      phase: 'bracket', round: 'QF',
      sideA: { id: 'pA', name: 'Alice' }, sideB: { id: 'pB', name: 'Bob' },
      ipponsA: ['M'], ipponsB: [], winner: { id: 'pA' },
    };
    const tree = runtime.mount(MatchDetailCard, { match, onClose: null });
    expect(findVnode(tree, n => n.type === IndividualScore)).toBeTruthy();
    expect(findVnode(tree, n => n.type === TeamScoreboard)).toBeNull();
  });
});

// mp-116 (Copilot review follow-up): the bracket-tab and pools-tab click sites
// now enrich the match with phase/round (or poolName) before opening the modal,
// because raw BracketMatch / pool match objects carry neither. MatchViewerModal's
// header renders `phase === "pool" ? poolName: round`, so without that metadata
// the header showed a dangling separator with an empty label. These tests lock
// the modal-header contract the callers must satisfy.
describe('MatchViewerModal header + team rendering (mp-116)', () => {
  const realReact = global.React;
  let runtime;
  let MatchViewerModal;
  let MatchDetailCard;
  const STUBBED = ['useEscapeToClose', 'ipponsFromScore'];
  const savedGlobals = {};

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => { savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k) ? { had: true, val: global.window[k] } : { had: false }; });
    global.window.useEscapeToClose = vi.fn();
    global.window.ipponsFromScore = vi.fn(() => []);
    vi.resetModules();
    ({ MatchViewerModal, MatchDetailCard } = await import('../viewer.jsx'));
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

  // mp-13y: the modal now reuses the canonical MatchDetailCard for its body
  // (DRY: same header, colour badges and BoutSubRow grid as the inline card).
  // The header text rendering itself is covered by the MatchDetailCard suite;
  // here we assert the modal delegates with the correct match.
  it('delegates to MatchDetailCard with the round label for a bracket match', () => {
    const match = {
      phase: 'bracket', round: 'Final', compKind: 'team', teamSize: 5,
      status: 'completed', court: 'A',
      sideA: { id: 'tA', name: 'Team A' }, sideB: { id: 'tB', name: 'Team B' },
      subResults: [{ position: 1, ipponsA: ['M'], ipponsB: [] }],
    };
    const tree = runtime.mount(MatchViewerModal, { match, onClose: () => {} });
    const card = findInTree(tree, (n) => n.type === MatchDetailCard);
    expect(card).toBeTruthy();
    expect(card.props.match.round).toBe('Final');
  });

  it('delegates to MatchDetailCard with the pool name for a pool match', () => {
    const match = {
      phase: 'pool', poolName: 'Pool A', compKind: 'team', teamSize: 5,
      status: 'completed', court: 'B',
      sideA: { id: 'tA', name: 'Team A' }, sideB: { id: 'tB', name: 'Team B' },
      subResults: [{ position: 1, ipponsA: ['M'], ipponsB: [] }],
    };
    const tree = runtime.mount(MatchViewerModal, { match, onClose: () => {} });
    const card = findInTree(tree, (n) => n.type === MatchDetailCard);
    expect(card).toBeTruthy();
    expect(card.props.match.poolName).toBe('Pool A');
  });
});

// mp-ef3 Copilot round 2: TournamentInfo component and isHttpURL helper tests.
describe('TournamentInfo', () => {
  let runtime;
  const realReact = global.React;

  beforeEach(() => {
    runtime = makeReactive();
    global.React = runtime.React;
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
  });

  it('returns null when all fields are empty', () => {
    const tree = runtime.mount(TournamentInfo, { tournament: {} });
    expect(tree).toBeNull();
  });

  it('renders venue, times, awards, and notes when provided', () => {
    const tree = runtime.mount(TournamentInfo, {
      tournament: {
        venueAddress: '1 Main St',
        openingTime: '09:00',
        closingTime: '18:00',
        awardsNote: 'Gold, Silver',
        infoNotes: 'Bring your shinai',
      },
    });
    const text = collectText(tree);
    expect(text).toContain('1 Main St');
    expect(text).toContain('09:00');
    expect(text).toContain('18:00');
    expect(text).toContain('Gold, Silver');
    expect(text).toContain('Bring your shinai');
  });

  it('creates an anchor for http(s) websiteURL but renders plain text for non-http', () => {
    // http URL → anchor element
    const treeHttp = runtime.mount(TournamentInfo, {
      tournament: { websiteURL: 'https://example.com/rules.pdf' },
    });
    const anchor = findInTree(treeHttp, n => n.type === 'a');
    expect(anchor).not.toBeNull();

    // Non-http value → plain text, no anchor
    const treePlain = runtime.mount(TournamentInfo, {
      tournament: { websiteURL: 'See the notice board' },
    });
    const anchorPlain = findInTree(treePlain, n => n.type === 'a');
    expect(anchorPlain).toBeNull();
    expect(collectText(treePlain)).toContain('See the notice board');
  });

  it('contactLink: builds mailto/tel only for well-formed values, plain text otherwise (mp-ubcb)', () => {
    const linkFor = (value) => {
      const tree = runtime.mount(TournamentInfo, { tournament: { contacts: [{ label: 'C', value }] } });
      return findInTree(tree, n => n.type === 'a');
    };
    // Well-formed email → mailto anchor.
    expect(linkFor('info@dojo.com')?.props?.href).toBe('mailto:info@dojo.com');
    expect(linkFor('foo+tag@example.co.uk')?.props?.href).toBe('mailto:foo+tag@example.co.uk');
    // Phone → tel anchor (decorators stripped).
    expect(linkFor('+44 20 7946 0000')?.props?.href).toBe('tel:+442079460000');
    // Leading-dot domain → NOT a link (renders as plain text).
    expect(linkFor('a@.evil.com')).toBeNull();
    // mailto query/fragment injection → NOT a link.
    expect(linkFor('foo@example.com?bcc=attacker@evil.org')).toBeNull();
    // Whitespace / scheme-injection / bare strings → NOT a link.
    expect(linkFor('not an email@x')).toBeNull();
    expect(linkFor('javascript:alert(1)@x')).toBeNull();
  });

  it('isHttpURL accepts http and https but rejects other schemes', () => {
    expect(isHttpURL('http://example.com')).toBe(true);
    expect(isHttpURL('https://example.com')).toBe(true);
    expect(isHttpURL('HTTP://EXAMPLE.COM')).toBe(true);
    expect(isHttpURL('javascript:alert(1)')).toBe(false);
    expect(isHttpURL('ftp://files.example.com')).toBe(false);
    expect(isHttpURL('')).toBe(false);
    expect(isHttpURL(undefined)).toBe(false);
  });
});

// mp-f4xo: LeagueMatrix: clickable cross-table cells
describe('LeagueMatrix (mp-f4xo)', () => {
  const realReact = global.React;
  let runtime;
  let PM;
  let savedIsHikiwake;

  const pool = {
    poolName: 'Pool A',
    players: [
      { name: 'Alice' },
      { name: 'Bob' },
      { name: 'Charlie' },
    ],
  };

  const completedMatch = {
    id: 'Pool A-1',
    sideA: { id: 'pA', name: 'Alice' },
    sideB: { id: 'pB', name: 'Bob' },
    status: 'completed',
    winner: { id: 'pA', name: 'Alice' },
    ipponsA: ['M'],
    ipponsB: [],
    decision: 'fought',
  };

  const pendingMatch = {
    id: 'Pool A-2',
    sideA: { id: 'pA', name: 'Alice' },
    sideB: { id: 'pC', name: 'Charlie' },
    status: 'scheduled',
    winner: null,
    ipponsA: [],
    ipponsB: [],
  };

  const runningMatch = {
    id: 'Pool A-3',
    sideA: { id: 'pB', name: 'Bob' },
    sideB: { id: 'pC', name: 'Charlie' },
    status: 'running',
    winner: null,
    ipponsA: [],
    ipponsB: [],
  };

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    savedIsHikiwake = global.window.isHikiwake;
    global.window.isHikiwake = vi.fn(() => false);
    vi.resetModules();
    ({ LeagueMatrix: PM } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    if (savedIsHikiwake === undefined) delete global.window.isHikiwake;
    else global.window.isHikiwake = savedIsHikiwake;
  });

  function allCells(tree) {
    const cells = [];
    (function walk(n) {
      if (!n || typeof n !== 'object') return;
      if (Array.isArray(n)) { n.forEach(walk); return; }
      if (n.type === 'td') cells.push(n);
      const kids = n.children || n.props?.children || [];
      [].concat(kids).forEach(walk);
    })(tree);
    return cells;
  }

  function allHeaders(tree) {
    const ths = [];
    (function walk(n) {
      if (!n || typeof n !== 'object') return;
      if (Array.isArray(n)) { n.forEach(walk); return; }
      if (n.type === 'th') ths.push(n);
      const kids = n.children || n.props?.children || [];
      [].concat(kids).forEach(walk);
    })(tree);
    return ths;
  }

  function textContent(node) {
    if (node == null) return '';
    if (typeof node === 'string' || typeof node === 'number') return String(node);
    if (Array.isArray(node)) return node.map(textContent).join('');
    const kids = node.children || node.props?.children;
    if (kids) return textContent(kids);
    return '';
  }

  it('renders W/L cells for a completed match', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {} });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--win'));
    const lossCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--loss'));
    expect(winCell).toBeTruthy();
    expect(lossCell).toBeTruthy();
  });

  // Engi (flag-count scoring) is the ONLY competition type where a matrix
  // cell shows a NUMBER; every other type shows ippon letters (tested
  // above via completedMatch's "M"). flagsA=sideA=Aka, flagsB=sideB=Shiro.
  it('renders the flag COUNT (not ippon letters) in an engi league matrix cell', () => {
    const engiMatch = {
      id: 'Pool A-1',
      sideA: { id: 'pA', name: 'Alice' },
      sideB: { id: 'pB', name: 'Bob' },
      status: 'completed',
      winner: { id: 'pA', name: 'Alice' },
      flagsA: 3, flagsB: 2,
      ipponsA: [], ipponsB: [],
    };
    const tree = runtime.mount(PM, { pool, matches: [engiMatch], tweaks: {} });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--win'));
    const lossCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--loss'));
    expect(textContent(winCell)).toBe('3');  // Alice = sideA = Aka = flagsA
    expect(textContent(lossCell)).toBe('2'); // Bob = sideB = Shiro = flagsB
  });

  it('clicking a completed-match cell fires onMatchClick with enriched match', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--win'));
    expect(winCell.props.onClick).toBeTypeOf('function');
    winCell.props.onClick();
    expect(spy).toHaveBeenCalledTimes(1);
    const enriched = spy.mock.calls[0][0];
    expect(enriched.phase).toBe('pool');
    expect(enriched.poolName).toBe('Pool A');
    expect(enriched.phaseName).toBe('Pool A');
    expect(enriched.compKind).toBe('');
    expect(enriched.teamSize).toBe(0);
    expect(enriched.id).toBe(completedMatch.id);
  });

  it('clicking a pending-match cell fires onMatchClick', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [pendingMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    const pendingCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--pending'));
    expect(pendingCell).toBeTruthy();
    pendingCell.props.onClick();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('clicking a running-match cell fires onMatchClick', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [runningMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    // A running bout renders the same as a pending one in the scoreboard
    // (no live marker), but its cell is still interactive.
    const runningCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--pending'));
    expect(runningCell).toBeTruthy();
    runningCell.props.onClick();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('self-diagonal and empty cells have no onClick', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: vi.fn() });
    const cells = allCells(tree);
    const selfCells = cells.filter(c => c.props?.className?.includes('league-matrix__cell--self'));
    const emptyCells = cells.filter(c => c.props?.className?.includes('league-matrix__cell--empty'));
    expect(selfCells.length).toBeGreaterThan(0);
    selfCells.forEach(c => expect(c.props.onClick).toBeUndefined());
    emptyCells.forEach(c => expect(c.props.onClick).toBeUndefined());
  });

  it('interactive cells have role=button and tabIndex=0 for a11y', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: vi.fn() });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--win'));
    expect(winCell.props.role).toBe('button');
    expect(winCell.props.tabIndex).toBe(0);
  });

  it('interactive cells have aria-label describing the matchup', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: vi.fn() });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--win'));
    expect(winCell.props['aria-label']).toContain('Alice');
    expect(winCell.props['aria-label']).toContain('Bob');
    expect(winCell.props['aria-label']).toContain('Win');
  });

  it('Enter key on interactive cell fires onMatchClick', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--win'));
    const preventDefaultSpy = vi.fn();
    winCell.props.onKeyDown({ key: 'Enter', preventDefault: preventDefaultSpy });
    expect(spy).toHaveBeenCalledTimes(1);
    expect(preventDefaultSpy).toHaveBeenCalled();
  });

  it('Space key on interactive cell fires onMatchClick', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--win'));
    const preventDefaultSpy = vi.fn();
    winCell.props.onKeyDown({ key: ' ', preventDefault: preventDefaultSpy });
    expect(spy).toHaveBeenCalledTimes(1);
    expect(preventDefaultSpy).toHaveBeenCalled();
  });

  it('cells are not interactive when onMatchClick is not provided', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {} });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('league-matrix__cell--win'));
    expect(winCell.props.onClick).toBeUndefined();
    expect(winCell.props.role).toBeUndefined();
  });

  it('returns null for fewer than 2 players', () => {
    const tree = runtime.mount(PM, { pool: { poolName: 'Pool X', players: [{ name: 'Solo' }] }, matches: [], tweaks: {} });
    expect(tree).toBeNull();
  });

  it('shows player.number in column headers and row labels when set', () => {
    const numberedPool = {
      poolName: 'Pool A',
      players: [
        { name: 'Alice', number: 'A1' },
        { name: 'Bob', number: 'A2' },
        { name: 'Charlie', number: 'A3' },
      ],
    };
    const tree = runtime.mount(PM, { pool: numberedPool, matches: [completedMatch], tweaks: {} });
    const ths = allHeaders(tree);
    const colHeaders = ths.filter(h => h.props?.className?.includes('league-matrix__col-head'));
    expect(colHeaders.map(h => textContent(h))).toEqual(['A1', 'A2', 'A3']);

    const rowHeads = allCells(tree).filter(c => c.props?.className?.includes('league-matrix__row-head'));
    const rowNums = rowHeads.map(td => {
      const spans = [].concat(td.children || td.props?.children || []);
      const numSpan = spans.find(s => s?.props?.className?.includes('league-matrix__num'));
      return textContent(numSpan);
    });
    expect(rowNums).toEqual(['A1', 'A2', 'A3']);
  });

  it('falls back to draw-order index when player.number is not set', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {} });
    const ths = allHeaders(tree);
    const colHeaders = ths.filter(h => h.props?.className?.includes('league-matrix__col-head'));
    // Column headers always show a visible label: the 1-based draw-order
    // position index when the player has no assigned number.
    expect(colHeaders.map(h => textContent(h))).toEqual(['1', '2', '3']);

    const rowHeads = allCells(tree).filter(c => c.props?.className?.includes('league-matrix__row-head'));
    const rowNums = rowHeads.map(td => {
      const spans = [].concat(td.children || td.props?.children || []);
      const numSpan = spans.find(s => s?.props?.className?.includes('league-matrix__num'));
      return textContent(numSpan);
    });
    // The number span is now always rendered, mirroring the column index so
    // row N and column N cross-reference the same player.
    expect(rowNums).toEqual(['1', '2', '3']);
  });

  // Regression: two participants share a name but have different dojos/ids.
  // Name-only cell indexing collapses both into one matchMap entry and shows
  // the wrong result; id-based keys must keep them distinct. (Copilot #261)
  it('disambiguates same-name participants by id when mapping cells', () => {
    const samePool = {
      poolName: 'Pool A',
      players: [
        { id: 'T1', name: 'Tanaka Kenji', dojo: 'Tokyo' },
        { id: 'T2', name: 'Tanaka Kenji', dojo: 'Osaka' }, // same name, different id
        { id: 'A1', name: 'Alice', dojo: 'Kyoto' },
      ],
    };
    // T1 beat Alice; T2 lost to Alice. With name-only keys both
    // "Tanaka Kenji vs Alice" cells resolve to the same (last-registered)
    // match and show identical results.
    const mWin = {
      id: 'Pool A-0', sideA: { id: 'T1', name: 'Tanaka Kenji' }, sideB: { id: 'A1', name: 'Alice' },
      sideAId: 'T1', sideBId: 'A1', status: 'completed', winner: { id: 'T1', name: 'Tanaka Kenji' },
      ipponsA: ['M'], ipponsB: [], decision: 'fought',
    };
    const mLoss = {
      id: 'Pool A-1', sideA: { id: 'T2', name: 'Tanaka Kenji' }, sideB: { id: 'A1', name: 'Alice' },
      sideAId: 'T2', sideBId: 'A1', status: 'completed', winner: { id: 'A1', name: 'Alice' },
      ipponsA: [], ipponsB: ['K'], decision: 'fought',
    };
    const tree = runtime.mount(PM, { pool: samePool, matches: [mWin, mLoss], tweaks: {} });
    const labels = allCells(tree).map(c => c.props?.['aria-label']).filter(Boolean);
    // The id-keyed mapping yields BOTH outcomes for the same name vs Alice:
    // proof the two Tanakas resolved to different matches. The aria-label also
    // carries the disambiguating dojo (a11y: screen readers can't rely on the
    // hover title), so the two Tanakas are distinguishable in the label itself.
    expect(labels).toContain('Match: Tanaka Kenji (Tokyo) vs Alice (Kyoto): Win');
    expect(labels).toContain('Match: Tanaka Kenji (Osaka) vs Alice (Kyoto): Loss');
  });

  // Regression: the head-to-head between two same-name participants. The
  // winner is stored by name (ambiguous), so rowWon must resolve via
  // winnerId: otherwise BOTH rows show the same result. (browser-found)
  it('resolves the winner of a same-name head-to-head by id', () => {
    const twoTanaka = {
      poolName: 'Pool A',
      players: [
        { id: 'T1', name: 'Tanaka Kenji', dojo: 'Tokyo' },
        { id: 'T2', name: 'Tanaka Kenji', dojo: 'Osaka' },
      ],
    };
    // T1 beat T2. winner name "Tanaka Kenji" is ambiguous; winnerId 'T1' is not.
    const headToHead = {
      id: 'Pool A-0', sideA: { id: 'T1', name: 'Tanaka Kenji' }, sideB: { id: 'T2', name: 'Tanaka Kenji' },
      sideAId: 'T1', sideBId: 'T2', winnerId: 'T1', status: 'completed',
      winner: { id: 'T1', name: 'Tanaka Kenji' }, ipponsA: ['M'], ipponsB: [], decision: 'fought',
    };
    const tree = runtime.mount(PM, { pool: twoTanaka, matches: [headToHead], tweaks: {} });
    const bodyCells = allCells(tree).filter(c => c.props?.className?.includes('league-matrix__cell--win') || c.props?.className?.includes('league-matrix__cell--loss'));
    const wins = bodyCells.filter(c => c.props?.className?.includes('--win'));
    const losses = bodyCells.filter(c => c.props?.className?.includes('--loss'));
    // Exactly one win (T1's row) and one loss (T2's row): NOT two wins.
    expect(wins).toHaveLength(1);
    expect(losses).toHaveLength(1);
  });
});

// mp-7x4n: ViewerOverview opens MatchViewerModal in self-run mode,
// MatchDetailCard in officiated mode.
describe('ViewerOverview self-run vs officiated match click (mp-7x4n)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerOverview;
  const STUBBED = ['ipponsFromScore', 'formatIpponsScore', 'queueLabel', 'isHikiwake', 'useEscapeToClose', 'hasBothSides', 'StatusBadge', 'formatLabel', 'roundLabel', 'queueLabelCompact', 'pluralize', 'teamIVScore', 'matchScoreStr'];
  const savedGlobals = {};

  const mkMatch = (id) => ({
    id,
    status: 'scheduled',
    phase: 'pool',
    poolName: 'Pool A',
    court: 'A',
    sideA: { id: 'a1', name: 'Alice' },
    sideB: { id: 'b1', name: 'Bob' },
    ipponsA: [],
    ipponsB: [],
  });

  const mkComp = () => ({
    id: 'c1',
    name: 'Test',
    format: 'mixed',
    status: 'pools',
    courts: ['A'],
  });

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => { savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k) ? { had: true, val: global.window[k] } : { had: false }; });
    global.window.ipponsFromScore = vi.fn(() => []);
    global.window.formatIpponsScore = vi.fn(() => '');
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = (m, ippB, ippA) =>
      (global.window.teamIVScore(m)) ||
      global.window.formatIpponsScore(ippB, ippA, m?.score, m?.decision, m?.encho, m?.decidedByHantei);
    global.window.queueLabel = vi.fn(() => '');
    global.window.queueLabelCompact = vi.fn(() => '');
    global.window.isHikiwake = vi.fn(() => false);
    global.window.useEscapeToClose = vi.fn();
    global.window.hasBothSides = vi.fn(() => true);
    global.window.StatusBadge = vi.fn(() => null);
    global.window.formatLabel = vi.fn((x) => x || '');
    global.window.roundLabel = vi.fn((x) => x || '');
    global.window.pluralize = vi.fn((n, s) => `${n} ${s}`);
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

  it('self-run: clicking an upcoming match opens MatchViewerModal', () => {
    const m = mkMatch('m1');
    const tree = runtime.mount(ViewerOverview, {
      c: mkComp(),
      myPlayer: null,
      myUpcoming: null,
      currentMatch: null,
      runningMatches: [],
      upcomingMatches: [m],
      recentMatches: [],
      tweaks: {},
      tournament: { mode: 'self-run' },
      compId: 'c1',
    });
    // VSchedItem is a child component: the reactive shim stores it as a
    // vnode {type: Function, props: {onClick, ...}} without executing it.
    // Find it by its type being a function with an onClick prop.
    const vsched = findInTree(tree, n => typeof n?.type === 'function' && n?.props?.onClick && n?.props?.m);
    expect(vsched).toBeTruthy();
    vsched.props.onClick();
    const updated = runtime.currentTree();
    const modal = findInTree(updated, n => n?.type?.name === 'MatchViewerModal');
    expect(modal).toBeTruthy();
    expect(modal.props.match).toBeTruthy();
    expect(modal.props.match.id).toBe('m1');
  });

  it('officiated: clicking an upcoming match expands inline MatchDetailCard', () => {
    const m = mkMatch('m1');
    const tree = runtime.mount(ViewerOverview, {
      c: mkComp(),
      myPlayer: null,
      myUpcoming: null,
      currentMatch: null,
      runningMatches: [],
      upcomingMatches: [m],
      recentMatches: [],
      tweaks: {},
      tournament: { mode: 'officiated' },
      compId: 'c1',
    });
    const vsched = findInTree(tree, n => typeof n?.type === 'function' && n?.props?.onClick && n?.props?.m);
    expect(vsched).toBeTruthy();
    vsched.props.onClick();
    const updated = runtime.currentTree();
    // MatchDetailCard should be rendered (function component with match + onClose props)
    const detailCard = findInTree(updated, n => typeof n?.type === 'function' && n?.props?.match && n?.props?.onClose !== undefined && !n?.props?.tournament);
    expect(detailCard).toBeTruthy();
    // No modal-backdrop (no MatchViewerModal)
    const modal = findInTree(updated, n => n?.type?.name === 'MatchViewerModal');
    expect(modal).toBeNull();
  });

  it('self-run: clicking the ON NOW current match opens MatchViewerModal', () => {
    const running = { ...mkMatch('curr1'), status: 'running' };
    runtime.mount(ViewerOverview, {
      c: mkComp(),
      myPlayer: null,
      myUpcoming: null,
      currentMatch: running,
      runningMatches: [],
      upcomingMatches: [],
      recentMatches: [],
      tweaks: {},
      tournament: { mode: 'self-run' },
      compId: 'c1',
    });
    const onNow = findInTree(runtime.currentTree(), n => n?.type === 'div' && n?.props?.role === 'button');
    expect(onNow).toBeTruthy();
    onNow.props.onClick();
    const modal = findInTree(runtime.currentTree(), n => n?.type?.name === 'MatchViewerModal');
    expect(modal).toBeTruthy();
    expect(modal.props.match.id).toBe('curr1');
  });

  it('self-run: Enter key on ON NOW current match opens MatchViewerModal', () => {
    const running = { ...mkMatch('curr2'), status: 'running' };
    runtime.mount(ViewerOverview, {
      c: mkComp(),
      myPlayer: null,
      myUpcoming: null,
      currentMatch: running,
      runningMatches: [],
      upcomingMatches: [],
      recentMatches: [],
      tweaks: {},
      tournament: { mode: 'self-run' },
      compId: 'c1',
    });
    const onNow = findInTree(runtime.currentTree(), n => n?.type === 'div' && n?.props?.role === 'button');
    expect(onNow).toBeTruthy();
    const fakeEvent = { key: 'Enter', preventDefault: vi.fn() };
    onNow.props.onKeyDown(fakeEvent);
    expect(fakeEvent.preventDefault).toHaveBeenCalled();
    const modal = findInTree(runtime.currentTree(), n => n?.type?.name === 'MatchViewerModal');
    expect(modal).toBeTruthy();
    expect(modal.props.match.id).toBe('curr2');
  });
});

// mp-s1gl: linkBase and isNonPublicOrigin unit tests.
describe('linkBase', () => {
  it('returns configured publicURL when set', () => {
    expect(linkBase({ publicURL: 'https://my-tournament.example.com' })).toBe('https://my-tournament.example.com');
  });

  it('falls back to window.location.origin when publicURL is empty string', () => {
    expect(linkBase({ publicURL: '' })).toBe(window.location.origin);
  });

  it('falls back to window.location.origin when publicURL is absent', () => {
    expect(linkBase({})).toBe(window.location.origin);
  });

  it('falls back to window.location.origin when tournament is null', () => {
    expect(linkBase(null)).toBe(window.location.origin);
  });

  it('falls back to window.location.origin when tournament is undefined', () => {
    expect(linkBase(undefined)).toBe(window.location.origin);
  });

  it('returns empty string when publicURL is absent and origin is opaque ("null")', () => {
    const origDescriptor = Object.getOwnPropertyDescriptor(window, 'location');
    try {
      Object.defineProperty(window, 'location', {
        value: { origin: 'null' },
        writable: true, configurable: true,
      });
      expect(linkBase({})).toBe('');
    } finally {
      if (origDescriptor) {
        Object.defineProperty(window, 'location', origDescriptor);
      }
    }
  });
});

describe('isNonPublicOrigin', () => {
  it('returns true for empty string', () => {
    expect(isNonPublicOrigin('')).toBe(true);
  });

  it('returns true for null', () => {
    expect(isNonPublicOrigin(null)).toBe(true);
  });

  it('returns true for undefined', () => {
    expect(isNonPublicOrigin(undefined)).toBe(true);
  });

  it('returns true for the string "null" (opaque sandboxed-iframe origin)', () => {
    expect(isNonPublicOrigin('null')).toBe(true);
  });

  it('returns true for localhost', () => {
    expect(isNonPublicOrigin('http://localhost')).toBe(true);
  });

  it('returns true for localhost with port', () => {
    expect(isNonPublicOrigin('http://localhost:8080')).toBe(true);
  });

  it('returns true for 0.0.0.0', () => {
    expect(isNonPublicOrigin('http://0.0.0.0')).toBe(true);
  });

  it('returns true for 127.x.x.x loopback', () => {
    expect(isNonPublicOrigin('http://127.0.0.1')).toBe(true);
  });

  it('returns true for 127.x.x.x loopback variant', () => {
    expect(isNonPublicOrigin('http://127.1.2.3')).toBe(true);
  });

  it('returns true for 192.168.x.x LAN', () => {
    expect(isNonPublicOrigin('http://192.168.1.100')).toBe(true);
  });

  it('returns true for 10.x.x.x private range', () => {
    expect(isNonPublicOrigin('http://10.0.0.1')).toBe(true);
  });

  it('returns true for 172.16.x.x private range', () => {
    expect(isNonPublicOrigin('http://172.16.0.1')).toBe(true);
  });

  it('returns true for 172.31.x.x private range', () => {
    expect(isNonPublicOrigin('http://172.31.255.254')).toBe(true);
  });

  it('returns false for 172.15.x.x (not in private range)', () => {
    expect(isNonPublicOrigin('http://172.15.0.1')).toBe(false);
  });

  it('returns false for 172.32.x.x (not in private range)', () => {
    expect(isNonPublicOrigin('http://172.32.0.1')).toBe(false);
  });

  it('returns true for *.local mDNS names', () => {
    expect(isNonPublicOrigin('http://myserver.local')).toBe(true);
  });

  it('returns true for a public hostname with a non-standard port', () => {
    expect(isNonPublicOrigin('http://my-tournament.example.com:8080')).toBe(true);
  });

  it('returns false for a public https URL', () => {
    expect(isNonPublicOrigin('https://my-tournament.example.com')).toBe(false);
  });

  it('returns false for a public http URL (non-localhost)', () => {
    expect(isNonPublicOrigin('http://staging.example.com')).toBe(false);
  });

  it('returns true for IPv6 loopback::1', () => {
    expect(isNonPublicOrigin('http://[::1]')).toBe(true);
  });
});


// VSchedItem live score rendering: T217
// Assertions: running match with ≥1 ippon renders score + .vsched-item__score--live;
// running match with no score falls through to "vs".
describe('VSchedItem live score rendering (mp-42rg)', () => {
  const realReact = global.React;
  let runtime;
  let VSchedItemComp;
  const savedGlobals = {};
  const STUBBED = ['ipponsFromScore', 'matchScoreStr', 'roundLabel', 'pluralize', 'queueLabelCompact'];

  function findNode(node, pred) {
    if (!node || typeof node !== 'object') return null;
    if (Array.isArray(node)) {
      for (const k of node) { const f = findNode(k, pred); if (f) return f; }
      return null;
    }
    if (pred(node)) return node;
    const kids = node.children || node.props?.children || [];
    for (const k of [].concat(kids)) { const f = findNode(k, pred); if (f) return f; }
    return null;
  }

  const mkMatch = (overrides) => ({
    id: 'm1', compId: 'c1', status: 'running', court: 'A',
    phase: 'bracket', round: 'QF',
    sideA: { id: 'pA', name: 'Alice' },
    sideB: { id: 'pB', name: 'Bob' },
    ipponsA: [], ipponsB: [],
    ...overrides,
  });

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.ipponsFromScore = vi.fn(() => []);
    global.window.matchScoreStr = vi.fn(() => '');
    global.window.roundLabel = vi.fn((i) => `Round ${i + 1}`);
    global.window.pluralize = vi.fn((n, s) => `${n} ${s}`);
    global.window.queueLabelCompact = null;
    vi.resetModules();
    ({ VSchedItem: VSchedItemComp } = await import('../viewer.jsx'));
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

  it('renders .vsched-item--running class for a running match', () => {
    const tree = runtime.mount(VSchedItemComp, { m: mkMatch(), tweaks: {} });
    const btn = findNode(tree, n => n.type === 'button');
    expect(btn?.props?.className).toMatch(/vsched-item--running/);
  });

  it('renders score + .vsched-item__score--live when running and matchScoreStr returns a non-empty string', () => {
    global.window.matchScoreStr = vi.fn(() => 'M–·');
    const tree = runtime.mount(VSchedItemComp, { m: mkMatch({ ipponsA: ['M'], ipponsB: [] }), tweaks: {} });
    const scoreSpan = findNode(tree, n => n.type === 'span' && String(n.props?.className || '').includes('vsched-item__score'));
    expect(scoreSpan).toBeTruthy();
    expect(scoreSpan.props.className).toContain('vsched-item__score--live');
    const scoreText = [].concat(scoreSpan.children ?? scoreSpan.props?.children ?? []).join('');
    expect(scoreText).toBe('M–·');
  });

  it('renders "vs" when running but matchScoreStr returns empty (no ippon yet)', () => {
    global.window.matchScoreStr = vi.fn(() => '');
    const tree = runtime.mount(VSchedItemComp, { m: mkMatch(), tweaks: {} });
    const vsSpan = findNode(tree, n => n.type === 'span' && String(n.props?.className || '').includes('vsched-item__vs'));
    expect(vsSpan).toBeTruthy();
    const text = [].concat(vsSpan.children ?? vsSpan.props?.children ?? []).join('');
    expect(text).toBe('vs');
  });

  it('does NOT add .vsched-item__score--live for a completed match', () => {
    global.window.matchScoreStr = vi.fn(() => 'MK–D');
    const tree = runtime.mount(VSchedItemComp, { m: mkMatch({ status: 'completed' }), tweaks: {} });
    const scoreSpan = findNode(tree, n => n.type === 'span' && String(n.props?.className || '').includes('vsched-item__score'));
    expect(scoreSpan).toBeTruthy();
    expect(scoreSpan.props.className).not.toContain('vsched-item__score--live');
  });
});

// -----------------------------------------------------------------------
// ViewerHome empty-state discoverability tests (mp-og2g)
// -----------------------------------------------------------------------
describe('ViewerHome empty-state discoverability (mp-og2g)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerHomeComp;
  let originalLocalStorageDescriptor;
  const STUBBED = [
    'StatusBadge', 'formatDate', 'formatLabel', 'formatViewerHeaderEyebrow',
    'pluralize', 'hasBothSides', 'compareDmy', 'queueLabelCompact',
    'roundLabel', 'matchScoreStr', 'ipponsFromScore', 'EmptyState',
  ];
  const savedGlobals = {};

  const emptyTournament = { name: 'T', date: '10-06-2026', competitions: [] };
  const withCompTournament = {
    name: 'T', date: '10-06-2026',
    competitions: [{ id: 'c1', name: 'Open', status: 'setup', players: [], poolMatches: [] }],
  };
  const NOOP = () => {};

  function findNode(node, pred) {
    if (!node || typeof node !== 'object') return null;
    if (Array.isArray(node)) {
      for (const k of node) { const f = findNode(k, pred); if (f) return f; }
      return null;
    }
    if (pred(node)) return node;
    if (typeof node.type === 'function') {
      try {
        const p = { ...(node.props || {}) };
        if (node.children?.length) p.children = node.children.length === 1 ? node.children[0] : node.children;
        const f = findNode(node.type(p), pred);
        if (f) return f;
      } catch { /* fall through */ }
    }
    const kids = node.children || node.props?.children || [];
    for (const k of [].concat(kids)) { const f = findNode(k, pred); if (f) return f; }
    return null;
  }

  beforeEach(async () => {
    originalLocalStorageDescriptor = Object.getOwnPropertyDescriptor(window, 'localStorage');
    const store = {};
    Object.defineProperty(window, 'localStorage', {
      value: {
        getItem: (k) => (k in store ? store[k] : null),
        setItem: (k, v) => { store[k] = String(v); },
        removeItem: (k) => { delete store[k]; },
        clear: () => { Object.keys(store).forEach((k) => delete store[k]); },
      },
      writable: true,
      configurable: true,
    });
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.StatusBadge = vi.fn(() => null);
    global.window.EmptyState = function EmptyState(props) { return { type: 'div', props: { className: 'empty', ...props }, children: [props.icon, props.title, props.message, props.cta].filter(Boolean) }; };
    global.window.formatDate = (d) => d || '';
    global.window.formatLabel = (l) => l || '';
    global.window.formatViewerHeaderEyebrow = () => '';
    global.window.pluralize = (n, s) => `${n} ${s}`;
    global.window.hasBothSides = () => true;
    global.window.compareDmy = () => 0;
    global.window.queueLabelCompact = null;
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.matchScoreStr = () => '';
    global.window.ipponsFromScore = () => [];
    vi.resetModules();
    ({ ViewerHome: ViewerHomeComp } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    if (originalLocalStorageDescriptor) {
      Object.defineProperty(window, 'localStorage', originalLocalStorageDescriptor);
    }
    runtime.unmount();
    global.React = realReact;
    STUBBED.forEach(k => {
      if (savedGlobals[k]?.had) global.window[k] = savedGlobals[k].val;
      else delete global.window[k];
    });
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('adds viewer__admin-pill--prominent class to admin pill when no competitions exist', () => {
    const tree = runtime.mount(ViewerHomeComp, {
      tournament: emptyTournament,
      sseConnected: true, onSelectCompetition: NOOP, onAdminClick: NOOP,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP,
    });
    const pill = findNode(tree, n => n.type === 'button' && String(n.props?.className || '').includes('viewer__admin-pill'));
    expect(pill).toBeTruthy();
    expect(pill.props.className).toContain('viewer__admin-pill--prominent');
  });

  it('does NOT add viewer__admin-pill--prominent when competitions exist', () => {
    const tree = runtime.mount(ViewerHomeComp, {
      tournament: withCompTournament,
      sseConnected: true, onSelectCompetition: NOOP, onAdminClick: NOOP,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP,
    });
    const pill = findNode(tree, n => n.type === 'button' && String(n.props?.className || '').includes('viewer__admin-pill'));
    expect(pill).toBeTruthy();
    expect(pill.props.className).not.toContain('viewer__admin-pill--prominent');
  });

  it('renders empty-state "Open admin" CTA wired to onAdminClick when no competitions exist', () => {
    const spy = vi.fn();
    const tree = runtime.mount(ViewerHomeComp, {
      tournament: emptyTournament,
      sseConnected: true, onSelectCompetition: NOOP, onAdminClick: spy,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP,
    });
    const cta = findNode(tree, n => n.type === 'button' && String(n.props?.className || '').includes('empty__cta'));
    expect(cta).toBeTruthy();
    cta.props.onClick();
    expect(spy).toHaveBeenCalledTimes(1);
  });
});

// -----------------------------------------------------------------------
// ViewerHome globalRunning de-dup render test (mp-42rg)
// -----------------------------------------------------------------------
describe('ViewerHome globalRunning de-dup (mp-42rg)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerHomeComp;
  let originalLocalStorageDescriptor;
  // Module-level consts captured at import time + render-time window reads
  const STUBBED = [
    'StatusBadge', 'formatDate', 'formatLabel', 'formatViewerHeaderEyebrow',
    'pluralize', 'hasBothSides', 'compareDmy', 'queueLabelCompact',
    'roundLabel', 'matchScoreStr', 'ipponsFromScore', 'EmptyState',
  ];
  const savedGlobals = {};

  function findNode(node, pred) {
    if (!node || typeof node !== 'object') return null;
    if (Array.isArray(node)) {
      for (const k of node) { const f = findNode(k, pred); if (f) return f; }
      return null;
    }
    if (pred(node)) return node;
    const kids = node.children || node.props?.children || [];
    for (const k of [].concat(kids)) { const f = findNode(k, pred); if (f) return f; }
    return null;
  }

  const mkPlayer = (id, name) => ({ id, name, dojo: 'TestDojo' });
  const mkRunning = (id, sideAId, sideBId) => ({
    id, status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A',
    sideA: { id: sideAId, name: sideAId }, sideB: { id: sideBId, name: sideBId },
    ipponsA: [], ipponsB: [],
  });
  const mkTournament = (poolMatches) => ({
    name: 'T', date: '10-06-2026',
    competitions: [{
      id: 'c1', name: 'Open', format: 'individual', status: 'pools',
      players: [
        mkPlayer('p-alice', 'Alice'), mkPlayer('p-bob', 'Bob'),
        mkPlayer('p-carol', 'Carol'), mkPlayer('p-dave', 'Dave'),
      ],
      poolMatches,
    }],
  });
  const NOOP = () => {};

  beforeEach(async () => {
    // Install a fresh localStorage mock via defineProperty so prior test suites
    // that clobber window.localStorage via defineProperty don't break this suite.
    originalLocalStorageDescriptor = Object.getOwnPropertyDescriptor(window, 'localStorage');
    const store = {};
    Object.defineProperty(window, 'localStorage', {
      value: {
        getItem: (k) => (k in store ? store[k] : null),
        setItem: (k, v) => { store[k] = String(v); },
        removeItem: (k) => { delete store[k]; },
        clear: () => { Object.keys(store).forEach((k) => delete store[k]); },
      },
      writable: true,
      configurable: true,
    });
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    // Module-level const bindings: must be set before vi.resetModules + import
    global.window.StatusBadge = vi.fn(() => null);
    global.window.formatDate = (d) => d || '';
    global.window.formatLabel = (l) => l || '';
    global.window.formatViewerHeaderEyebrow = () => '';
    global.window.pluralize = (n, s) => `${n} ${s}`;
    // Render-time window reads
    global.window.hasBothSides = () => true;
    global.window.compareDmy = () => 0;
    global.window.queueLabelCompact = null;
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.matchScoreStr = () => '';
    global.window.ipponsFromScore = () => [];
    global.window.EmptyState = function EmptyState(props) { return { type: 'div', props: { className: 'empty', ...props }, children: [props.icon, props.title, props.message, props.cta].filter(Boolean) }; };
    vi.resetModules();
    ({ ViewerHome: ViewerHomeComp } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    if (originalLocalStorageDescriptor) {
      Object.defineProperty(window, 'localStorage', originalLocalStorageDescriptor);
    }
    runtime.unmount();
    global.React = realReact;
    STUBBED.forEach(k => {
      if (savedGlobals[k]?.had) global.window[k] = savedGlobals[k].val;
      else delete global.window[k];
    });
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('excludes watched running matches from .hero-running and shows unwatched ones', () => {
    // Alice is watched. Alice's match (m-alice) must be excluded from
    // .hero-running; Carol's unwatched match (m-carol) must appear there.
    window.localStorage.setItem('bc_watchlist',
      JSON.stringify([{ type: 'player', id: 'p-alice', name: 'Alice' }]));
    const aliceMatch = mkRunning('m-alice', 'p-alice', 'p-bob');
    const carolMatch = mkRunning('m-carol', 'p-carol', 'p-dave');
    const tree = runtime.mount(ViewerHomeComp, {
      tournament: mkTournament([aliceMatch, carolMatch]),
      sseConnected: true, onSelectCompetition: NOOP, onAdminClick: NOOP,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP,
    });
    const heroRunning = findNode(tree, n => n.props?.className === 'hero-running');
    // .hero-running present because Carol's match is not watched
    expect(heroRunning).toBeTruthy();
    // Alice's match key must NOT appear inside .hero-running
    const aliceKey = findNode(heroRunning, n => n.props?.key === 'c1:m-alice');
    expect(aliceKey).toBeNull();
    // Carol's match key MUST appear inside .hero-running
    const carolKey = findNode(heroRunning, n => n.props?.key === 'c1:m-carol');
    expect(carolKey).toBeTruthy();
  });

  it('hides .hero-running entirely when every running match is in the watchlist', () => {
    // Both Alice and Carol watched → globalRunning is empty → no .hero-running
    window.localStorage.setItem('bc_watchlist',
      JSON.stringify([
        { type: 'player', id: 'p-alice', name: 'Alice' },
        { type: 'player', id: 'p-carol', name: 'Carol' },
      ]));
    const aliceMatch = mkRunning('m-alice', 'p-alice', 'p-bob');
    const carolMatch = mkRunning('m-carol', 'p-carol', 'p-dave');
    const tree = runtime.mount(ViewerHomeComp, {
      tournament: mkTournament([aliceMatch, carolMatch]),
      sseConnected: true, onSelectCompetition: NOOP, onAdminClick: NOOP,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP,
    });
    const heroRunning = findNode(tree, n => n.props?.className === 'hero-running');
    expect(heroRunning).toBeNull();
  });
});
