import { describe, it, expect } from 'vitest';
import { enrichPoolMatchWithComp, poolMatchesForPool } from '../admin_pools.jsx';

describe('enrichPoolMatchWithComp', () => {
  const comp = { id: 'c1', name: 'Comp One', kind: 'team', teamSize: 5 };

  it('returns null/undefined unchanged so a missing match short-circuits cleanly', () => {
    expect(enrichPoolMatchWithComp(null, comp)).toBeNull();
    expect(enrichPoolMatchWithComp(undefined, comp)).toBeUndefined();
  });

  it('fills in all comp-* fields from the competition when the match has none', () => {
    const m = { id: 'A-0', status: 'scheduled' };
    const enriched = enrichPoolMatchWithComp(m, comp);
    expect(enriched.compId).toBe('c1');
    expect(enriched.compName).toBe('Comp One');
    expect(enriched.compKind).toBe('team');
    expect(enriched.teamSize).toBe(5);
    expect(enriched.phase).toBe('pool');
    // Pool name derived from id prefix when no override is supplied.
    expect(enriched.poolName).toBe('A');
    // Original fields preserved verbatim.
    expect(enriched.id).toBe('A-0');
    expect(enriched.status).toBe('scheduled');
  });

  it('sets compFormat from competition.format so score editors can render format-aware labels', () => {
    const leagueComp = { id: 'c2', name: 'League Cup', kind: 'individual', teamSize: 0, format: 'league' };
    const m = { id: 'Pool A-0', status: 'scheduled' };
    const enriched = enrichPoolMatchWithComp(m, leagueComp);
    expect(enriched.compFormat).toBe('league');
  });

  it('prefers compFormat already on the match over the competition format', () => {
    const leagueComp = { id: 'c2', name: 'League Cup', kind: 'individual', teamSize: 0, format: 'league' };
    const m = { id: 'Pool A-0', status: 'scheduled', compFormat: 'mixed' };
    const enriched = enrichPoolMatchWithComp(m, leagueComp);
    expect(enriched.compFormat).toBe('mixed');
  });

  it('falls back to empty string for compFormat when competition has no format', () => {
    const m = { id: 'A-0', status: 'scheduled' };
    const enriched = enrichPoolMatchWithComp(m, comp); // comp has no .format
    expect(enriched.compFormat).toBe('');
  });

  it('prefers the explicit poolNameOverride over the id-derived prefix', () => {
    const m = { id: 'A-0', status: 'scheduled' };
    const enriched = enrichPoolMatchWithComp(m, comp, 'Pool Alpha');
    expect(enriched.poolName).toBe('Pool Alpha');
  });

  it('does NOT clobber existing comp-* fields on the match (server-injected wins)', () => {
    // Defensive: if a future SSE patch or refresh annotates pool matches
    // with comp-* metadata, we must not blow it away.
    const m = {
      id: 'A-0',
      status: 'scheduled',
      compId: 'server-id',
      compName: 'Server Name',
      compKind: 'individual',
      teamSize: 0,
      phase: 'bracket',
      poolName: 'ServerPool',
    };
    const enriched = enrichPoolMatchWithComp(m, comp, 'Pool Alpha');
    expect(enriched.compId).toBe('server-id');
    expect(enriched.compName).toBe('Server Name');
    expect(enriched.compKind).toBe('individual');
    expect(enriched.teamSize).toBe(0);
    expect(enriched.phase).toBe('bracket');
    expect(enriched.poolName).toBe('ServerPool');
  });

  it('uses teamSize=0 as a valid value (?? not ||) so individual comps stay individual', () => {
    // teamSize is numeric and 0 means "not a team competition". Using `||`
    // would treat 0 as falsy and fall through to the comp's teamSize. Use
    // `??` so the explicit 0 sticks.
    const m = { id: 'A-0', status: 'scheduled', teamSize: 0 };
    const enriched = enrichPoolMatchWithComp(m, comp);
    expect(enriched.teamSize).toBe(0);
  });

  it('handles a match id without a "-" gracefully (empty poolName, no crash)', () => {
    // Defensive: malformed ids ("X", "", null) shouldn't throw. Pool
    // name falls back to "" so the modal header degrades but doesn't crash.
    const enrichedX = enrichPoolMatchWithComp({ id: 'X', status: 'scheduled' }, comp);
    expect(enrichedX.poolName).toBe('');
    const enrichedEmpty = enrichPoolMatchWithComp({ id: '', status: 'scheduled' }, comp);
    expect(enrichedEmpty.poolName).toBe('');
  });

  it('handles hyphenated pool names correctly (not split on first hyphen)', () => {
    // Pool names can contain hyphens (e.g. "Pool A-East"). A naive
    // split('-')[0] would return "Pool A" losing the "-East" suffix.
    // The regex strips only the trailing numeric/DH/TB match-index segment.
    expect(enrichPoolMatchWithComp({ id: 'Pool A-East-0', status: 'scheduled' }, comp).poolName)
      .toBe('Pool A-East');
    expect(enrichPoolMatchWithComp({ id: 'Pool A-East-DH-0', status: 'scheduled' }, comp).poolName)
      .toBe('Pool A-East');
    expect(enrichPoolMatchWithComp({ id: 'Pool A-East-TB-2', status: 'scheduled' }, comp).poolName)
      .toBe('Pool A-East');
    // Simple (non-hyphenated) pool name still works.
    expect(enrichPoolMatchWithComp({ id: 'A-0', status: 'scheduled' }, comp).poolName)
      .toBe('A');
  });

  it('handles a null competition (rare, but defensive against transitional state)', () => {
    const m = { id: 'A-0', status: 'scheduled' };
    const enriched = enrichPoolMatchWithComp(m, null);
    expect(enriched.compId).toBe('');
    expect(enriched.compName).toBe('');
    expect(enriched.compKind).toBe('');
    expect(enriched.teamSize).toBe(0);
    expect(enriched.phase).toBe('pool');
    expect(enriched.poolName).toBe('A');
  });

  it('normalizes string sideA/sideB to {id,name} objects so ScoreEditorModal can read .name', () => {
    // Pool matches from the Go backend have string sideA/sideB. Without
    // normalization, ScoreEditorModal's m.sideA?.name would be undefined
    // and competitor names would not render.
    const m = { id: 'A-0', status: 'scheduled', sideA: 'Alice', sideB: 'Bob' };
    const enriched = enrichPoolMatchWithComp(m, comp);
    expect(enriched.sideA).toEqual(expect.objectContaining({ id: 'Alice', name: 'Alice' }));
    expect(enriched.sideB).toEqual(expect.objectContaining({ id: 'Bob', name: 'Bob' }));
  });

  it('resolves player dojo from buildPlayerMap when available', () => {
    const prev = window.buildPlayerMap;
    try {
      window.buildPlayerMap = () => ({
        Alice: { id: 'Alice', name: 'Alice', dojo: 'DojoA' },
      });
      const m = { id: 'A-0', status: 'scheduled', sideA: 'Alice', sideB: 'Unknown' };
      const enriched = enrichPoolMatchWithComp(m, comp);
      expect(enriched.sideA).toEqual({ id: 'Alice', name: 'Alice', dojo: 'DojoA' });
      expect(enriched.sideB).toEqual({ id: 'Unknown', name: 'Unknown' });
    } finally {
      if (prev === undefined) {
        delete window.buildPlayerMap;
      } else {
        window.buildPlayerMap = prev;
      }
    }
  });

  it('converts falsy sideA/sideB to {id:"",name:""} (bye/TBD slot)', () => {
    const m = { id: 'A-0', status: 'scheduled', sideA: '', sideB: null };
    const enriched = enrichPoolMatchWithComp(m, comp);
    expect(enriched.sideA).toEqual({ id: '', name: '' });
    expect(enriched.sideB).toEqual({ id: '', name: '' });
  });

  it('leaves already-normalized object sideA/sideB unchanged', () => {
    const sideA = { id: 'p1', name: 'Alice', dojo: 'DojoA' };
    const sideB = { id: 'p2', name: 'Bob', dojo: 'DojoB' };
    const m = { id: 'A-0', status: 'scheduled', sideA, sideB };
    const enriched = enrichPoolMatchWithComp(m, comp);
    expect(enriched.sideA).toBe(sideA);
    expect(enriched.sideB).toBe(sideB);
  });

  // --- DH / supplementary bout routing ---

  it('sets repIsTeam=true and forces compKind="" + teamSize=0 for a team-comp DH bout', () => {
    // Pool-DH matches are representative tie-break bouts scored as INDIVIDUAL
    // even in a team competition. enrichPoolMatchWithComp must override
    // compKind/teamSize so ScoreEditorModal routes to the individual editor,
    // and must set repIsTeam so the rep-player dropdowns appear.
    const teamComp = { id: 'tc1', name: 'Team Cup', kind: 'team', teamSize: 5 };
    const m = { id: 'Pool A-DH-0', status: 'scheduled', sideA: 'Team Alpha', sideB: 'Team Beta' };
    const enriched = enrichPoolMatchWithComp(m, teamComp);
    expect(enriched.repIsTeam).toBe(true);
    expect(enriched.compKind).toBe('');
    expect(enriched.teamSize).toBe(0);
  });

  it('populates repRosterA and repRosterB via AdminLineupHelpers.rosterFor when stubbed', () => {
    const rosterAlpha = [{ id: 'p1', name: 'Alice' }, { id: 'p2', name: 'Bob' }];
    const rosterBeta = [{ id: 'p3', name: 'Carol' }];
    const teamAlpha = { id: 't1', name: 'Team Alpha', Name: undefined };
    const teamBeta = { id: 't2', name: 'Team Beta', Name: undefined };
    const teamComp = {
      id: 'tc1', name: 'Team Cup', kind: 'team', teamSize: 5,
      players: [teamAlpha, teamBeta],
    };
    const m = { id: 'Pool A-DH-0', status: 'scheduled', sideA: 'Team Alpha', sideB: 'Team Beta' };
    const prevHelpers = window.AdminLineupHelpers;
    try {
      window.AdminLineupHelpers = {
        rosterFor: (team) => {
          if (!team) return [];
          if (team.name === 'Team Alpha') return rosterAlpha;
          if (team.name === 'Team Beta') return rosterBeta;
          return [];
        },
      };
      const enriched = enrichPoolMatchWithComp(m, teamComp);
      expect(enriched.repIsTeam).toBe(true);
      expect(enriched.repRosterA).toEqual(rosterAlpha);
      expect(enriched.repRosterB).toEqual(rosterBeta);
    } finally {
      if (prevHelpers === undefined) {
        delete window.AdminLineupHelpers;
      } else {
        window.AdminLineupHelpers = prevHelpers;
      }
    }
  });

  it('repRosterA and repRosterB default to empty arrays when AdminLineupHelpers is absent', () => {
    const teamComp = { id: 'tc1', name: 'Team Cup', kind: 'team', teamSize: 5 };
    const m = { id: 'Pool A-DH-0', status: 'scheduled', sideA: 'Team Alpha', sideB: 'Team Beta' };
    const prevHelpers = window.AdminLineupHelpers;
    try {
      delete window.AdminLineupHelpers;
      const enriched = enrichPoolMatchWithComp(m, teamComp);
      expect(enriched.repIsTeam).toBe(true);
      expect(Array.isArray(enriched.repRosterA)).toBe(true);
      expect(Array.isArray(enriched.repRosterB)).toBe(true);
    } finally {
      if (prevHelpers !== undefined) {
        window.AdminLineupHelpers = prevHelpers;
      }
    }
  });

  it('sets repIsTeam=false for a regular pool match in a team competition', () => {
    // Non-supplementary bouts should NOT get the rep-picker treatment even
    // if the competition is a team comp.
    const teamComp = { id: 'tc1', name: 'Team Cup', kind: 'team', teamSize: 5 };
    const m = { id: 'Pool A-0', status: 'scheduled', sideA: 'Team Alpha', sideB: 'Team Beta' };
    const enriched = enrichPoolMatchWithComp(m, teamComp);
    expect(enriched.repIsTeam).toBe(false);
    // Regular team match keeps the team routing fields intact.
    expect(enriched.compKind).toBe('team');
    expect(enriched.teamSize).toBe(5);
  });

  it('sets repIsTeam=false for a DH bout in an individual competition', () => {
    // isTeamComp is false when kind !== "team" and teamSize is 0.
    const individualComp = { id: 'ic1', name: 'Ind Cup', kind: 'individual', teamSize: 0 };
    const m = { id: 'Pool A-DH-0', status: 'scheduled', sideA: 'Alice', sideB: 'Bob' };
    const enriched = enrichPoolMatchWithComp(m, individualComp);
    expect(enriched.repIsTeam).toBe(false);
    // compKind/teamSize are still overridden for individual routing.
    expect(enriched.compKind).toBe('');
    expect(enriched.teamSize).toBe(0);
  });
});

describe('poolMatchesForPool', () => {
  const matches = [
    { id: 'Pool A-0', status: 'completed' },
    { id: 'Pool A-1', status: 'completed' },
    { id: 'Pool A-2', status: 'scheduled' },
    { id: 'Pool B-0', status: 'running' },
    { id: 'Pool B-1', status: 'scheduled' },
    { id: 'Pool A-DH-0', status: 'scheduled' },   // daihyosen
    { id: 'Pool A-TB-0', status: 'scheduled' },   // tiebreak
  ];

  it('returns only matches for the requested pool name', () => {
    const result = poolMatchesForPool(matches, 'Pool A');
    expect(result.map(m => m.id)).toEqual(['Pool A-0', 'Pool A-1', 'Pool A-2', 'Pool A-DH-0', 'Pool A-TB-0']);
  });

  it('includes DH (daihyosen) and TB (tiebreak) match variants for the pool', () => {
    const result = poolMatchesForPool(matches, 'Pool A');
    expect(result.some(m => m.id === 'Pool A-DH-0')).toBe(true);
    expect(result.some(m => m.id === 'Pool A-TB-0')).toBe(true);
  });

  it('does not bleed Pool A matches into Pool AB (prefix-safety)', () => {
    // Pool names are "Pool A", "Pool B", etc. A naive startsWith('Pool A')
    // would wrongly include "Pool AB-0". The regex strips the trailing
    // numeric/DH/TB segment, so "Pool AB-0" → derivedName="Pool AB" ≠ "Pool A".
    const withAB = [...matches, { id: 'Pool AB-0', status: 'scheduled' }];
    const result = poolMatchesForPool(withAB, 'Pool A');
    expect(result.some(m => m.id === 'Pool AB-0')).toBe(false);
  });

  it('returns an empty array when no matches belong to the pool', () => {
    expect(poolMatchesForPool(matches, 'Pool Z')).toEqual([]);
  });

  it('returns an empty array for null/undefined poolMatches (guard for not-yet-started competitions)', () => {
    expect(poolMatchesForPool(null, 'Pool A')).toEqual([]);
    expect(poolMatchesForPool(undefined, 'Pool A')).toEqual([]);
  });

  it('skips matches with a missing or empty id without crashing', () => {
    const withBadIds = [{ id: '', status: 'scheduled' }, { status: 'scheduled' }, ...matches];
    // Bad-id entries derive poolName="" which won't match any real pool name
    const result = poolMatchesForPool(withBadIds, 'Pool A');
    expect(result.map(m => m.id)).toEqual(['Pool A-0', 'Pool A-1', 'Pool A-2', 'Pool A-DH-0', 'Pool A-TB-0']);
  });

  it('regression: match id undefined did not crash (coerced to "" by (m.id || ""))', () => {
    const withUndefinedId = [{ id: undefined, status: 'scheduled' }];
    expect(() => poolMatchesForPool(withUndefinedId, 'Pool A')).not.toThrow();
    expect(poolMatchesForPool(withUndefinedId, 'Pool A')).toEqual([]);
  });
  it('completed match retains status for Score→Edit flip', () => {
    const mixed = [
      { id: 'Pool A-0', status: 'completed' },
      { id: 'Pool A-1', status: 'scheduled' },
    ];
    const result = poolMatchesForPool(mixed, 'Pool A');
    expect(result[0].status).toBe('completed');
    expect(result[1].status).toBe('scheduled');
  });
});
