import { describe, it, expect } from 'vitest';
import { applyFilters, matchHighlightedBy, competitionKindLabel, isSwissFinalStandings, swissStandingsHeading } from '../viewer.jsx';
import { formatDate } from '../ui.jsx';

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
      expect(isSwissFinalStandings(mkComp({ format: 'pools' }), completedR4)).toBe(false);
      expect(isSwissFinalStandings(mkComp({ format: 'playoffs' }), completedR4)).toBe(false);
    });

    it('false for null/missing competition', () => {
      expect(isSwissFinalStandings(null, completedR4)).toBe(false);
      expect(isSwissFinalStandings(undefined, completedR4)).toBe(false);
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
});
