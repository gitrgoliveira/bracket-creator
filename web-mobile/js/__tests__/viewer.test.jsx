import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { applyFilters, matchHighlightedBy, competitionKindLabel, isSwissFinalStandings, swissStandingsHeading, isFollowedPlayer, compMatches, subBoutLabel } from '../viewer.jsx';
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
});

