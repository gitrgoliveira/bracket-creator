import { describe, it, expect } from 'vitest';
import { decideRankCommit, enrichPoolMatchWithComp, buildRunningById, isRanksLocked, poolMatchesForPool } from '../admin_pools.jsx';

// decideRankCommit is the pure predicate that drives RankInput.handleBlur.
// It returns one of:
//   {action: "noop"}                    : do nothing
//   {action: "sync", value: <string>}   : setV(value), don't commit
//   {action: "revert", value: <string>}: setV(value) (visual revert)
//   {action: "commit", value: <string>}: call onCommit(value)
//
// The component test (focus/blur/keydown DOM events) is out of scope for
// the current vitest setup which mocks React with stub hooks. These pure
// tests cover the decision logic that drives those behaviours.

describe('decideRankCommit', () => {
  describe('cancelled (Esc was pressed)', () => {
    it('is noop regardless of other inputs', () => {
      expect(decideRankCommit({ v: "5", initial: 2, focusValue: "2", cancelled: true }))
        .toEqual({ action: "noop" });
      // Even if the typed value would otherwise be valid + different:
      expect(decideRankCommit({ v: "999", initial: 1, focusValue: "1", cancelled: true }))
        .toEqual({ action: "noop" });
    });
  });

  describe('focus-without-edit (v === focusValue)', () => {
    it('is noop when initial is unchanged', () => {
      expect(decideRankCommit({ v: "2", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "noop" });
    });

    it('syncs to latest initial when initial changed during focus (TOCTOU guard)', () => {
      // User focused while seeing rank=2 (v="2", focusValue="2").
      // SSE updated rank to 5 (initial=5). User clicked away without typing.
      // We must NOT commit "2" (would revert the server's 5). Instead,
      // visually sync to 5.
      expect(decideRankCommit({ v: "2", initial: 5, focusValue: "2", cancelled: false }))
        .toEqual({ action: "sync", value: "5" });
    });
  });

  describe('invalid input → revert', () => {
    it('reverts on non-numeric typing', () => {
      expect(decideRankCommit({ v: "abc", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('reverts on empty string', () => {
      expect(decideRankCommit({ v: "", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('reverts on zero', () => {
      expect(decideRankCommit({ v: "0", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('reverts on negative', () => {
      expect(decideRankCommit({ v: "-1", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('reverts on rank > 1000 (matches server cap)', () => {
      expect(decideRankCommit({ v: "1001", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
      expect(decideRankCommit({ v: "999999", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });

    it('accepts rank = 1000 (boundary)', () => {
      expect(decideRankCommit({ v: "1000", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "commit", value: "1000" });
    });

    it('reverts on NaN/Infinity-like input', () => {
      expect(decideRankCommit({ v: "NaN", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
      // parseInt("Infinity") is NaN, so this also reverts.
      expect(decideRankCommit({ v: "Infinity", initial: 3, focusValue: "3", cancelled: false }))
        .toEqual({ action: "revert", value: "3" });
    });
  });

  describe('valid edit → commit', () => {
    it('commits when user changed rank to a different valid value', () => {
      expect(decideRankCommit({ v: "5", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "commit", value: "5" });
    });

    it('does not commit when normalized value matches initial (e.g. typed "02" for 2)', () => {
      // parseInt("02") === 2 === String(initial). No real change.
      expect(decideRankCommit({ v: "02", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "noop" });
    });

    it('strips leading whitespace via parseInt (commit normalized)', () => {
      // parseInt("  5  ") === 5
      expect(decideRankCommit({ v: "  5  ", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "commit", value: "5" });
    });

    it('truncates fractional input via parseInt', () => {
      expect(decideRankCommit({ v: "5.7", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "commit", value: "5" });
    });

    it('handles "5abc" by parsing leading int', () => {
      // parseInt("5abc") === 5: user got tired of typing or paste went wrong.
      expect(decideRankCommit({ v: "5abc", initial: 2, focusValue: "2", cancelled: false }))
        .toEqual({ action: "commit", value: "5" });
    });
  });

  describe('priority ordering', () => {
    it('cancelled wins over focus-without-edit-sync', () => {
      // SSE-changed initial AND Esc pressed simultaneously → cancelled
      // takes priority. We don't sync; we trust the Esc handler's setV.
      expect(decideRankCommit({ v: "2", initial: 5, focusValue: "2", cancelled: true }))
        .toEqual({ action: "noop" });
    });

    it('focus-without-edit wins over invalid-input revert', () => {
      // If v === focusValue, we never reach the parseInt branch. (User
      // didn't type: there's nothing to validate.) This matters for
      // an exotic case: focusValue is "0" somehow (shouldn't happen but
      // defensive): we still sync rather than revert.
      expect(decideRankCommit({ v: "0", initial: 0, focusValue: "0", cancelled: false }))
        .toEqual({ action: "noop" });
    });
  });

  describe('RankInput consumer contract: commit value mirrors to local state', () => {
    // Copilot round-7 finding: when decideRankCommit returns {action:"commit",
    // value: "5"} for typed "5abc", RankInput.handleBlur must call BOTH
    // setV(result.value) AND onCommit(result.value). The earlier version only
    // called onCommit, leaving the input displaying "5abc" until the
    // SSE-driven prop refresh hit useEffectA: a confusing few-hundred-ms
    // window where the visible value didn't match what was sent.
    //
    // The handleBlur dispatch lives in admin_pools.jsx:70-91 and can't be
    // unit-tested in isolation without DOM rendering (vitest setup mocks
    // React with stubs; tracked as follow-up #4/#7). These assertions pin
    // the contract that handleBlur depends on: result.value is what should
    // be mirrored into local state on commit.

    it('commit result.value is the normalized form (whitespace trimmed)', () => {
      const r = decideRankCommit({ v: "  5  ", initial: 2, focusValue: "2", cancelled: false });
      expect(r).toEqual({ action: "commit", value: "5" });
      // Consumer contract: setV("5") so the input shows "5" not "  5  ".
    });

    it('commit result.value is the normalized form (trailing junk stripped)', () => {
      const r = decideRankCommit({ v: "5abc", initial: 2, focusValue: "2", cancelled: false });
      expect(r).toEqual({ action: "commit", value: "5" });
      // Consumer contract: setV("5") so the input shows "5" not "5abc".
    });

    it('commit result.value is the normalized form (fraction truncated)', () => {
      const r = decideRankCommit({ v: "5.9", initial: 2, focusValue: "2", cancelled: false });
      expect(r).toEqual({ action: "commit", value: "5" });
      // Consumer contract: setV("5") so the input shows "5" not "5.9".
    });

    it('commit result.value differs from the raw v for the normalization shapes above', () => {
      // Sanity: if these match, the consumer's setV(result.value) is a no-op
      // (whatever was typed is already canonical) and the bug doesn't manifest.
      // For "5abc" / "  5  " / "5.9" the raw v differs from result.value:
      // these are exactly the inputs where the consumer-side bug surfaces.
      for (const v of ["  5  ", "5abc", "5.9"]) {
        const r = decideRankCommit({ v, initial: 2, focusValue: "2", cancelled: false });
        expect(r.action).toBe("commit");
        expect(r.value).not.toBe(v);
      }
    });
  });
});

describe('buildRunningById', () => {
  it('returns an object keyed by match id', () => {
    const matches = [
      { id: 'pool-A-1', status: 'completed', ipponsA: 2, ipponsB: 0 },
      { id: 'pool-A-2', status: 'scheduled' },
    ];
    const running = buildRunningById(matches);
    expect(running['pool-A-1'].status).toBe('completed');
    expect(running['pool-A-2'].status).toBe('scheduled');
  });

  it('returns empty object when poolMatches is null or empty', () => {
    expect(buildRunningById(null)).toEqual({});
    expect(buildRunningById([])).toEqual({});
  });

  it('running state overrides stale pool.matches entry', () => {
    const stale = { id: 'pool-A-1', status: 'scheduled' };
    const running = buildRunningById([{ id: 'pool-A-1', status: 'completed', ipponsA: 1, ipponsB: 0 }]);
    const resolved = running[stale.id] || stale;
    expect(resolved.status).toBe('completed');
    expect(resolved.ipponsA).toBe(1);
  });

  it('falls back to stale entry when match not yet in running list', () => {
    const stale = { id: 'pool-A-99', status: 'scheduled' };
    const running = buildRunningById([]);
    const resolved = running[stale.id] || stale;
    expect(resolved.status).toBe('scheduled');
  });
});

describe('isRanksLocked', () => {
  it('unlocked when status is pools', () => {
    expect(isRanksLocked('pools')).toBe(false);
  });

  it('locked when status is playoffs', () => {
    expect(isRanksLocked('playoffs')).toBe(true);
  });

  it('locked when status is completed', () => {
    expect(isRanksLocked('completed')).toBe(true);
  });

  it('locked when status is setup', () => {
    // CompStatusSetup ("setup") is the pre-pools state. The component
    // early-returns when pools is empty, so this branch rarely renders,
    // but the predicate must still report locked for defense-in-depth.
    expect(isRanksLocked('setup')).toBe(true);
  });

  it('locked when status is invalid', () => {
    // CompStatusInvalid ("invalid") is set when a competition is reset.
    // Pools may still exist on disk but rank inputs must not be editable.
    expect(isRanksLocked('invalid')).toBe(true);
  });

  it('locked when status is empty string or undefined', () => {
    expect(isRanksLocked('')).toBe(true);
    expect(isRanksLocked(undefined)).toBe(true);
  });
});

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
