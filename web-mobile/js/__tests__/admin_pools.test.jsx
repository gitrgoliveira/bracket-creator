import { describe, it, expect } from 'vitest';
import { decideRankCommit } from '../admin_pools.jsx';

// decideRankCommit is the pure predicate that drives RankInput.handleBlur.
// It returns one of:
//   {action: "noop"}                    — do nothing
//   {action: "sync", value: <string>}   — setV(value), don't commit
//   {action: "revert", value: <string>} — setV(value) (visual revert)
//   {action: "commit", value: <string>} — call onCommit(value)
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
      // parseInt("5abc") === 5 — user got tired of typing or paste went wrong.
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
      // didn't type — there's nothing to validate.) This matters for
      // an exotic case: focusValue is "0" somehow (shouldn't happen but
      // defensive) — we still sync rather than revert.
      expect(decideRankCommit({ v: "0", initial: 0, focusValue: "0", cancelled: false }))
        .toEqual({ action: "noop" });
    });
  });

  describe('RankInput consumer contract: commit value mirrors to local state', () => {
    // Copilot round-7 finding: when decideRankCommit returns {action:"commit",
    // value: "5"} for typed "5abc", RankInput.handleBlur must call BOTH
    // setV(result.value) AND onCommit(result.value). The earlier version only
    // called onCommit, leaving the input displaying "5abc" until the
    // SSE-driven prop refresh hit useEffectA — a confusing few-hundred-ms
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
      // For "5abc" / "  5  " / "5.9" the raw v differs from result.value —
      // these are exactly the inputs where the consumer-side bug surfaces.
      for (const v of ["  5  ", "5abc", "5.9"]) {
        const r = decideRankCommit({ v, initial: 2, focusValue: "2", cancelled: false });
        expect(r.action).toBe("commit");
        expect(r.value).not.toBe(v);
      }
    });
  });
});

// AdminPools Score-button null-result fix (mp-zcz)
// -------------------------------------------------------
// Before this fix, the Score buttons in both the pool-detail view and the
// compact card list called `onEditScore(c.id, m.id, null, m)` — routing
// null through AdminApp.editMatchScore → toBackendMatchResult(null, m)
// which throws `TypeError: Cannot read properties of null`.
//
// The fix mounts a local ScoreEditorModal in AdminPools (the same pattern
// AdminScoreEditor uses) so the button opens the modal and the modal's
// onSubmit provides a real patch to onEditScore.
//
// The ScoreEditorModal mount lives inside AdminPools which is a window.*
// component — full DOM rendering is not available in the current vitest
// setup (window.* mocks are stubs, not a real jsdom React tree).
// Tracked for follow-up DOM-level testing alongside the existing anchor
// at admin_pools.test.jsx:133-145 (RankInput.handleBlur dispatch).
//
// Regression contract (pure, testable): the Score button no longer calls
// onEditScore directly — instead it updates local `scoreOpenMatch` state
// which renders the modal, and the modal's onSubmit calls onEditScore with
// the real patch. The modal is imported as window.ScoreEditorModal and
// receives `password` so decision endpoints are authenticated.

// Deep-review (2026-05-22): enrichPoolMatchWithComp regression tests.
// Pool-match objects from pools[i].matches carry only the MatchResult
// shape (id, status, sides, ippons, decision) without comp-level
// metadata. The ScoreEditorModal reads compKind / teamSize / compId /
// compName / phase / poolName off the match prop to:
//   * pick TeamScoreEditorModal vs the individual editor,
//   * fetch competition details for maxEnchoPeriods / naginata,
//   * render the header.
// Without enrichment a team-comp pool match would silently route into
// the individual editor and the modal header would render "undefined ·
// undefined". Enrichment is applied at the click boundary so when
// mp-i3h merges poolMatches into pool.matches, the modal still picks
// up the right competition context.
import { enrichPoolMatchWithComp } from '../admin_pools.jsx';

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
});
