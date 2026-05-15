import { describe, it, expect } from 'vitest';
import { mergeCompetitionsIntoTournament } from '../admin.jsx';

// /deep-review finding on UI side: AdminApp's async handlers
// (updateCompetition, moveMatchCourt, editMatchScore, addCompetition,
// startCompetition, createPlayoff, startAllCompetitions, import
// onImported) all did
//   `await window.API.X(...)` then `onUpdate({ ...t, competitions: comps })`
// where `t` is closure-captured at handler-definition time. If SSE
// fires during the in-flight await and updates the tournament (another
// comp's match completes, a comp starts, etc.), the post-await
// onUpdate clobbers the SSE update with stale state. AdminApp's
// existing `tRef` / `onUpdateRef` pair was set up for exactly this but
// the handlers weren't using it. Fix: use tRef.current and
// onUpdateRef.current, and route through mergeCompetitionsIntoTournament
// which encapsulates the merge.
//
// The merge logic is small but worth pinning behaviorally so a future
// refactor can't silently regress the closure-capture vs ref question.
describe('mergeCompetitionsIntoTournament', () => {
  it('preserves all tournament fields except competitions', () => {
    const t = {
      id: 't1', name: 'Cup', venue: 'Hall', date: '2026-05-12',
      courts: ['A', 'B'], password: 'secret',
      competitions: [{ id: 'c1', name: 'C1' }],
    };
    const result = mergeCompetitionsIntoTournament(t, () => []);
    expect(result.id).toBe('t1');
    expect(result.name).toBe('Cup');
    expect(result.venue).toBe('Hall');
    expect(result.date).toBe('2026-05-12');
    expect(result.courts).toEqual(['A', 'B']);
    expect(result.password).toBe('secret');
    expect(result.competitions).toEqual([]);
  });

  it('applies the mutator function to the competitions array', () => {
    const t = { id: 't1', competitions: [{ id: 'c1', name: 'A' }, { id: 'c2', name: 'B' }] };
    const result = mergeCompetitionsIntoTournament(t, comps =>
      comps.map(c => c.id === 'c1' ? { ...c, name: 'A renamed' } : c));
    expect(result.competitions).toEqual([
      { id: 'c1', name: 'A renamed' },
      { id: 'c2', name: 'B' },
    ]);
  });

  it('passes an empty array to the mutator when competitions is undefined', () => {
    // Guards the `|| []` fallback — necessary so handlers that fire on
    // a fresh tournament (no competitions yet) don't crash on .map of
    // undefined.
    const t = { id: 't1', name: 'Cup' };
    const result = mergeCompetitionsIntoTournament(t, comps => {
      expect(Array.isArray(comps)).toBe(true);
      expect(comps).toHaveLength(0);
      return [{ id: 'new', name: 'New' }];
    });
    expect(result.competitions).toEqual([{ id: 'new', name: 'New' }]);
  });

  it('produces a new object reference (immutable update)', () => {
    // React's setState relies on reference equality to detect changes.
    // mergeCompetitionsIntoTournament must always produce a new
    // tournament object so the parent's setT triggers a re-render.
    const t = { id: 't1', competitions: [] };
    const result = mergeCompetitionsIntoTournament(t, comps => comps);
    expect(result).not.toBe(t);
  });

  it('reflects updated values when called with the latest tRef.current', () => {
    // The bug shape this helper is here to fix: AdminApp's handlers
    // need to merge against the LATEST tournament state, not a
    // closure-captured snapshot. This test simulates the pattern by
    // calling the helper twice with different `currentT` values,
    // showing that each call sees only what it was given (no hidden
    // closure-captured state in the helper itself — that's exactly
    // what we want).
    const t1 = { id: 't1', competitions: [{ id: 'c1', name: 'Original' }] };
    const t2 = {
      id: 't1', competitions: [
        { id: 'c1', name: 'Updated by SSE' },
        { id: 'c2', name: 'Added by SSE' },
      ],
    };

    // Caller mutates c1; result must contain SSE's c2 because we
    // passed t2 (latest).
    const result = mergeCompetitionsIntoTournament(t2, comps =>
      comps.map(c => c.id === 'c1' ? { ...c, name: 'My rename' } : c));
    expect(result.competitions).toEqual([
      { id: 'c1', name: 'My rename' },
      { id: 'c2', name: 'Added by SSE' },
    ]);
    // Critically: t2's SSE-added c2 is preserved. Pre-fix, AdminApp's
    // handlers used closure-captured `t = t1`, which had only c1 —
    // the post-await onUpdate({...t1, competitions: [{c1: renamed}]})
    // would have dropped c2 from the local state, requiring another
    // SSE round-trip to recover.
    expect(result.competitions.find(c => c.id === 'c2')).toBeDefined();
  });
});

// Copilot finding (PR #104 round-9-followup): create flows
// (addCompetition / createPlayoff) used `refreshCompsBestEffort` which
// just logged + toasted on refresh failure. The caller then navigated
// to `view.kind="competition", id: created.id`, but local
// `t.competitions` still didn't contain `created` — so AdminApp's
// `t.competitions.find(cc => cc.id === view.id)` returned undefined,
// rendering "Competition not found" until the next SSE/manual refresh
// landed.
//
// Fix: a `refreshCompsAfterCreate(created, ...)` helper that ALSO
// merges the created record into local state when refresh fails.
// The mutator pattern is:
//   comps => comps.some(c => c.id === created.id) ? comps : [...comps, created]
// It's idempotent (no duplicates if `created` is already in `comps`,
// e.g. via an SSE update that landed during the in-flight create).
//
// Pinning the mutator's behaviour here as a pure test — the full
// addCompetition / createPlayoff handlers need DOM rendering to test
// (vitest setup mocks React with stubs; see follow-up #4/#7).
describe('refreshCompsAfterCreate merge mutator', () => {
  // Replicate the inline mutator pattern. Keep in sync with admin.jsx's
  // refreshCompsAfterCreate.
  const appendIfMissing = (created) => (comps) =>
    comps.some(c => c.id === created.id) ? comps : [...comps, created];

  it('appends the created record when local state does not have it', () => {
    const t = { id: 't1', competitions: [{ id: 'c1', name: 'A' }] };
    const created = { id: 'c2', name: 'B' };
    const result = mergeCompetitionsIntoTournament(t, appendIfMissing(created));
    expect(result.competitions).toEqual([
      { id: 'c1', name: 'A' },
      { id: 'c2', name: 'B' },
    ]);
  });

  it('is idempotent — no duplicate when local state already has the record', () => {
    // SSE could have landed during the in-flight create, populating
    // `created.id` into `t.competitions` before refresh failed. The
    // merge must not duplicate.
    const t = { id: 't1', competitions: [
      { id: 'c1', name: 'A' },
      { id: 'c2', name: 'B (already present)' },
    ]};
    const created = { id: 'c2', name: 'B (newly created)' };
    const result = mergeCompetitionsIntoTournament(t, appendIfMissing(created));
    expect(result.competitions).toHaveLength(2);
    // The existing entry is kept (potentially newer SSE-driven value);
    // we don't overwrite it with the create response.
    expect(result.competitions.find(c => c.id === 'c2').name)
      .toBe('B (already present)');
  });

  it('appends to an empty competitions array', () => {
    // First competition in a fresh tournament — the most common
    // create-then-navigate case.
    const t = { id: 't1', competitions: [] };
    const created = { id: 'c1', name: 'First' };
    const result = mergeCompetitionsIntoTournament(t, appendIfMissing(created));
    expect(result.competitions).toEqual([{ id: 'c1', name: 'First' }]);
  });

  it('appends even when competitions is undefined (fresh tournament)', () => {
    // mergeCompetitionsIntoTournament's `|| []` fallback combined with
    // the appendIfMissing pattern means a fresh tournament shape
    // (no competitions field) still works.
    const t = { id: 't1' };
    const created = { id: 'c1', name: 'First' };
    const result = mergeCompetitionsIntoTournament(t, appendIfMissing(created));
    expect(result.competitions).toEqual([{ id: 'c1', name: 'First' }]);
  });
});
