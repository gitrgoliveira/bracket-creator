import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { applyFilters, matchHighlightedBy, competitionKindLabel, isSwissFinalStandings, swissStandingsHeading, isFollowedPlayer, compMatches, subBoutLabel, TournamentInfo, isHttpURL, linkBase, isNonPublicOrigin } from '../viewer.jsx';
import { formatDate } from '../ui.jsx';
import { makeReactive } from './helpers/reactive_react.js';

// Walks a vnode tree and concatenates all string/number leaves. Child
// component vnodes (e.g. <TermV>) are NOT executed by the reactive shim,
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
      expect(competitionKindLabel({ kind: 'individual', gender: 'M' })).toBe('Individual · Men');
      expect(competitionKindLabel({ kind: 'individual', gender: 'F' })).toBe('Individual · Women');
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

    it('highlights by name when picked id differs from match side id', () => {
      expect(matchHighlightedBy(match, [{ id: 'uuid-xxx', name: 'Alice' }], '')).toBe(true);
      expect(matchHighlightedBy(match, [{ id: 'uuid-xxx', name: 'Nobody' }], '')).toBe(false);
    });
  });

  // T192 (US13 — FR-050e): Swiss standings page header logic. The
  // viewer flips its header text from "Standings after round N" to
  // "Final standings" once every configured round has been played
  // out — and only then declares a winner. Pure helpers so the
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
      // current=total but pool-matches list is empty — this only
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

  // mp-7e6 — isFollowedPlayer: UUID-first match with name fallback.
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

  // mp-7e6 — compMatches: pool phase/poolName derivation for flat viewer
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

    it('"Standings — pending" when no round has been generated yet', () => {
      const c = { format: 'swiss', swissRounds: 4, swissCurrentRound: 0 };
      expect(swissStandingsHeading(c, [])).toBe('Standings — pending');
    });
  });

  // mp-8sw — subBoutLabel: the team sub-bout center label. The daihyosen
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

// mp-8sw — MatchDetailCard team sub-rows: render-level proof that the
// daihyosen row labels as "Daihyosen" (not "Match -1") and shows the
// "Hantei" marker only when sub.decidedByHantei. Asserts the actual
// rendered tree, complementing the subBoutLabel unit tests above.
describe('MatchDetailCard team sub-rows (mp-8sw)', () => {
  const realReact = global.React;
  let runtime;
  let MatchDetailCard;
  // Preserve any pre-existing window globals we stub so we can restore exact
  // state in afterEach. vi.restoreAllMocks() only undoes vi.spyOn, NOT direct
  // `global.window.x = vi.fn()` assignments — without this the mocked globals
  // leak into later suites and make failures order-dependent.
  const savedGlobals = {};
  const STUBBED = ['formatIpponsScore', 'ipponsFromScore'];

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

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => { savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k) ? { had: true, val: global.window[k] } : { had: false }; });
    // Only globals MatchDetailCard executes on the team path. The non-team
    // ippons block (which calls window.isHikiwake) is gated out for teams.
    global.window.formatIpponsScore = vi.fn(() => '3-2');
    global.window.ipponsFromScore = vi.fn(() => []);
    vi.resetModules();
    ({ MatchDetailCard } = await import('../viewer.jsx'));
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

  it('labels the daihyosen row "Daihyosen" and shows "Hantei" when decidedByHantei', () => {
    const tree = runtime.mount(MatchDetailCard, {
      match: mkTeamMatch([
        { position: 1, ipponsA: ['M'], ipponsB: [], decidedByHantei: false },
        { position: -1, ipponsA: ['K'], ipponsB: [], decidedByHantei: true },
      ]),
      onClose: null,
    });
    const text = collectText(tree);
    expect(text).toContain('Match 1');
    expect(text).toContain('Daihyosen');
    expect(text).not.toContain('Match -1');
    expect(text).toContain('Hantei');
    // The marker must carry left spacing so it does not render flush against
    // the label as "DaihyosenHantei" (Copilot review on #192).
    const marker = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-row-hantei');
    expect(marker).toBeTruthy();
    expect(marker.props.style?.marginLeft).toBeTruthy();
  });

  it('omits the "Hantei" marker when no sub was decided by hantei', () => {
    const tree = runtime.mount(MatchDetailCard, {
      match: mkTeamMatch([
        { position: 1, ipponsA: ['M'], ipponsB: [], decidedByHantei: false },
        { position: -1, ipponsA: ['K'], ipponsB: ['K'], decidedByHantei: false },
      ]),
      onClose: null,
    });
    const text = collectText(tree);
    expect(text).toContain('Daihyosen');
    expect(text).not.toContain('Hantei');
  });

  // mp-116: Overview "Recent results" bug — allMatches useMemo was not threading
  // compKind/teamSize, so isTeam evaluated false and the individual ippons block
  // rendered instead of the team sub-bout rows.
  // Verify that a match carrying compKind="team" (as allMatches now produces)
  // renders match-detail-card__team-subs, NOT match-detail-card__ippons.
  it('renders team sub-bout block (not individual ippons) when compKind="team" from allMatches', () => {
    const match = {
      ...mkTeamMatch([
        { position: 1, ipponsA: ['M'], ipponsB: [], decidedByHantei: false },
        { position: 2, ipponsA: [], ipponsB: ['D'], decidedByHantei: false },
      ]),
      // These flags are what allMatches now correctly supplies for team comps.
      compKind: 'team',
      teamSize: 3,
    };
    const tree = runtime.mount(MatchDetailCard, { match, onClose: null });
    // Team sub-rows container must be present.
    const teamSubs = findInTree(tree, n => n?.props?.className === 'match-detail-card__team-subs');
    expect(teamSubs).toBeTruthy();
    // Individual ippons block must NOT appear.
    const ipponsBlock = findInTree(tree, n => n?.props?.className === 'match-detail-card__ippons');
    expect(ipponsBlock).toBeNull();
  });

  // mp-116: Individual match must still render the ippons block (regression guard).
  it('renders individual ippons block (not team subs) for an individual match', () => {
    // The individual path calls window.isHikiwake — stub it for this test,
    // restoring any prior value in finally so a thrown assertion can't leak
    // the stub into later tests (mirrors the roundLabel pattern above).
    const savedIsHikiwake = global.window.isHikiwake;
    global.window.isHikiwake = vi.fn(() => false);
    try {
      const match = {
        compKind: 'individual',
        teamSize: 0,
        status: 'completed',
        court: 'A',
        phase: 'bracket',
        round: 'QF',
        sideA: { id: 'pA', name: 'Alice' },
        sideB: { id: 'pB', name: 'Bob' },
        ipponsA: ['M'],
        ipponsB: [],
        winner: { id: 'pA' },
      };
      const tree = runtime.mount(MatchDetailCard, { match, onClose: null });
      const ipponsBlock = findInTree(tree, n => n?.props?.className === 'match-detail-card__ippons');
      expect(ipponsBlock).toBeTruthy();
      const teamSubs = findInTree(tree, n => n?.props?.className === 'match-detail-card__team-subs');
      expect(teamSubs).toBeNull();
    } finally {
      if (savedIsHikiwake === undefined) delete global.window.isHikiwake;
      else global.window.isHikiwake = savedIsHikiwake;
    }
  });
});

// mp-116 (Copilot review follow-up): the bracket-tab and pools-tab click sites
// now enrich the match with phase/round (or poolName) before opening the modal,
// because raw BracketMatch / pool match objects carry neither. MatchViewerModal's
// header renders `phase === "pool" ? poolName : round`, so without that metadata
// the header showed a dangling separator with an empty label. These tests lock
// the modal-header contract the callers must satisfy.
describe('MatchViewerModal header + team rendering (mp-116)', () => {
  const realReact = global.React;
  let runtime;
  let MatchViewerModal;
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
    ({ MatchViewerModal } = await import('../viewer.jsx'));
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

  it('renders the round label in the header for a bracket match', () => {
    const match = {
      phase: 'bracket', round: 'Final', compKind: 'team', teamSize: 5,
      status: 'completed', court: 'A',
      sideA: { id: 'tA', name: 'Team A' }, sideB: { id: 'tB', name: 'Team B' },
      subResults: [{ position: 1, ipponsA: ['M'], ipponsB: [] }],
    };
    const tree = runtime.mount(MatchViewerModal, { match, onClose: () => {} });
    const text = collectText(tree);
    // Header must show the round label, not an empty string after "Shiaijo A ·".
    expect(text).toContain('Final');
    // Team path renders the SHIRO/Position/AKA sub-bout table.
    expect(text).toContain('Position');
  });

  it('renders the pool name in the header for a pool match', () => {
    const match = {
      phase: 'pool', poolName: 'Pool A', compKind: 'team', teamSize: 5,
      status: 'completed', court: 'B',
      sideA: { id: 'tA', name: 'Team A' }, sideB: { id: 'tB', name: 'Team B' },
      subResults: [{ position: 1, ipponsA: ['M'], ipponsB: [] }],
    };
    const tree = runtime.mount(MatchViewerModal, { match, onClose: () => {} });
    expect(collectText(tree)).toContain('Pool A');
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

  it('creates an anchor for http(s) rulesURL but renders plain text for non-http', () => {
    // http URL → anchor element
    const treeHttp = runtime.mount(TournamentInfo, {
      tournament: { rulesURL: 'https://example.com/rules.pdf' },
    });
    const anchor = findInTree(treeHttp, n => n.type === 'a');
    expect(anchor).not.toBeNull();

    // Non-http value → plain text, no anchor
    const treePlain = runtime.mount(TournamentInfo, {
      tournament: { rulesURL: 'See the notice board' },
    });
    const anchorPlain = findInTree(treePlain, n => n.type === 'a');
    expect(anchorPlain).toBeNull();
    expect(collectText(treePlain)).toContain('See the notice board');
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

// mp-f4xo: PoolMatrix — clickable cross-table cells
describe('PoolMatrix (mp-f4xo)', () => {
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
    ({ PoolMatrix: PM } = await import('../viewer.jsx'));
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
    const winCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--win'));
    const lossCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--loss'));
    expect(winCell).toBeTruthy();
    expect(lossCell).toBeTruthy();
  });

  it('clicking a completed-match cell fires onMatchClick with enriched match', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--win'));
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
    const pendingCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--pending'));
    expect(pendingCell).toBeTruthy();
    pendingCell.props.onClick();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('clicking a live-match cell fires onMatchClick', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [runningMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    const liveCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--live'));
    expect(liveCell).toBeTruthy();
    liveCell.props.onClick();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('self-diagonal and empty cells have no onClick', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: vi.fn() });
    const cells = allCells(tree);
    const selfCells = cells.filter(c => c.props?.className?.includes('pool-matrix__cell--self'));
    const emptyCells = cells.filter(c => c.props?.className?.includes('pool-matrix__cell--empty'));
    expect(selfCells.length).toBeGreaterThan(0);
    selfCells.forEach(c => expect(c.props.onClick).toBeUndefined());
    emptyCells.forEach(c => expect(c.props.onClick).toBeUndefined());
  });

  it('interactive cells have role=button and tabIndex=0 for a11y', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: vi.fn() });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--win'));
    expect(winCell.props.role).toBe('button');
    expect(winCell.props.tabIndex).toBe(0);
  });

  it('interactive cells have aria-label describing the matchup', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: vi.fn() });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--win'));
    expect(winCell.props['aria-label']).toContain('Alice');
    expect(winCell.props['aria-label']).toContain('Bob');
    expect(winCell.props['aria-label']).toContain('Win');
  });

  it('Enter key on interactive cell fires onMatchClick', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--win'));
    const preventDefaultSpy = vi.fn();
    winCell.props.onKeyDown({ key: 'Enter', preventDefault: preventDefaultSpy });
    expect(spy).toHaveBeenCalledTimes(1);
    expect(preventDefaultSpy).toHaveBeenCalled();
  });

  it('Space key on interactive cell fires onMatchClick', () => {
    const spy = vi.fn();
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {}, onMatchClick: spy });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--win'));
    const preventDefaultSpy = vi.fn();
    winCell.props.onKeyDown({ key: ' ', preventDefault: preventDefaultSpy });
    expect(spy).toHaveBeenCalledTimes(1);
    expect(preventDefaultSpy).toHaveBeenCalled();
  });

  it('cells are not interactive when onMatchClick is not provided', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {} });
    const cells = allCells(tree);
    const winCell = cells.find(c => c.props?.className?.includes('pool-matrix__cell--win'));
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
    const colHeaders = ths.filter(h => h.props?.className?.includes('pool-matrix__col-head'));
    expect(colHeaders.map(h => textContent(h))).toEqual(['A1', 'A2', 'A3']);

    const rowHeads = allCells(tree).filter(c => c.props?.className?.includes('pool-matrix__row-head'));
    const rowNums = rowHeads.map(td => {
      const spans = [].concat(td.children || td.props?.children || []);
      const numSpan = spans.find(s => s?.props?.className?.includes('pool-matrix__num'));
      return textContent(numSpan);
    });
    expect(rowNums).toEqual(['A1', 'A2', 'A3']);
  });

  it('omits numbers entirely when player.number is not set', () => {
    const tree = runtime.mount(PM, { pool, matches: [completedMatch], tweaks: {} });
    const ths = allHeaders(tree);
    const colHeaders = ths.filter(h => h.props?.className?.includes('pool-matrix__col-head'));
    expect(colHeaders.map(h => textContent(h))).toEqual(['', '', '']);

    const rowHeads = allCells(tree).filter(c => c.props?.className?.includes('pool-matrix__row-head'));
    const rowNums = rowHeads.map(td => {
      const spans = [].concat(td.children || td.props?.children || []);
      const numSpan = spans.find(s => s?.props?.className?.includes('pool-matrix__num'));
      return numSpan;
    });
    expect(rowNums).toEqual([undefined, undefined, undefined]);
  });
});

// mp-7x4n: ViewerOverview opens MatchViewerModal in self-run mode,
// MatchDetailCard in officiated mode.
describe('ViewerOverview self-run vs officiated match click (mp-7x4n)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerOverview;
  const STUBBED = ['ipponsFromScore', 'formatIpponsScore', 'queueLabel', 'isHikiwake', 'useEscapeToClose', 'hasBothSides', 'StatusBadge', 'formatLabel', 'roundLabel', 'queueLabelCompact', 'pluralize'];
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
      liveMatches: [],
      upcomingMatches: [m],
      recentMatches: [],
      tweaks: {},
      tournament: { mode: 'self-run' },
      compId: 'c1',
    });
    // VSchedItem is a child component — the reactive shim stores it as a
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
      liveMatches: [],
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
      liveMatches: [],
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
      liveMatches: [],
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
    const orig = window.location.origin;
    Object.defineProperty(window, 'location', {
      value: { ...window.location, origin: 'null' },
      writable: true, configurable: true,
    });
    expect(linkBase({})).toBe('');
    Object.defineProperty(window, 'location', {
      value: { ...window.location, origin: orig },
      writable: true, configurable: true,
    });
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

  it('returns true for IPv6 loopback ::1', () => {
    expect(isNonPublicOrigin('http://[::1]')).toBe(true);
  });
});

