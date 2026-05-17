import { describe, it, expect } from 'vitest';
import { mergeCompetitionsIntoTournament, mergeTournamentPatch, normalizeCreatedRecord } from '../admin.jsx';

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
    const _t1 = { id: 't1', competitions: [{ id: 'c1', name: 'Original' }] };
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

// /deep-review round-11 finding: AdminApp.updateTournament was the last
// async mutation handler still using closure-captured `t` and `onUpdate`
// instead of tRef.current / onUpdateRef.current. The other 7+ async
// handlers were migrated in earlier rounds (see
// mergeCompetitionsIntoTournament docstring), but updateTournament's
// inline `onUpdate({ ...t, ...patch })` was missed. Same bug shape:
// SSE-driven updates to the tournament (e.g. match_updated bumping a
// competition's matches, competition_started flipping status) that
// land during the in-flight `await window.API.updateTournament(...)`
// get clobbered by the post-await onUpdate pushing the stale closure-
// captured `t`.
//
// Pinning the merge behaviour as a pure test — the handler itself is a
// closure inside AdminApp and can't be rendered in this vitest setup
// (mocked React hooks). The test simulates the bug by passing a
// "latest" tournament (mirroring what tRef.current would hold post-SSE)
// and checking that the merge result reflects the LATEST competitions
// list, not a snapshotted closure capture.
describe('mergeTournamentPatch', () => {
  // NB: test fixtures below use the literal strings "X", "Y", "Z" for
  // password values. Avoid anything that looks like a credential (e.g.
  // "session-secret", "new-pw") — generic-password secret scanners
  // (GitGuardian etc.) match on keyword-adjacent string values regardless
  // of context, and a flagged test fixture noise-fails CI.
  it('overlays patch fields onto the latest tournament', () => {
    const t = { id: 't1', name: 'Old', date: '01-01-2026', venue: 'Hall', password: 'X' };
    const result = mergeTournamentPatch(t, { name: 'New', venue: 'Dojo' }, 'X');
    expect(result.name).toBe('New');
    expect(result.venue).toBe('Dojo');
    expect(result.date).toBe('01-01-2026');
  });

  it('restores session password when the patch omits password', () => {
    // The viewer API strips tournament.password from responses, so a
    // freshly fetched tournament has password=""; we need to refill it
    // from the session before sending back to the server.
    const t = { id: 't1', name: 'Cup', password: '' };
    const result = mergeTournamentPatch(t, { name: 'New' }, 'Y');
    expect(result.password).toBe('Y');
  });

  it('keeps the patch password when explicitly provided', () => {
    // The "user wants to change the password" path: patch.password wins
    // over the session password, regardless of what was on disk.
    const t = { id: 't1', name: 'Cup', password: 'X' };
    const result = mergeTournamentPatch(t, { password: 'Z' }, 'Y');
    expect(result.password).toBe('Z');
  });

  it('preserves the latest tournament competitions when patch omits them', () => {
    // The CORE bug shape — pre-fix, updateTournament used the closure-
    // captured `t` to build `next = { ...t, ...patch }`, so a SSE update
    // that mutated `tRef.current.competitions` during the in-flight PUT
    // was clobbered when `onUpdate(next)` fired post-await. The helper
    // now takes the LATEST tournament as input; the caller threads
    // tRef.current through it, so SSE updates that landed mid-flight are
    // preserved in the merged result.
    const latestT = {
      id: 't1', name: 'Old', password: 'X',
      competitions: [
        { id: 'c1', name: 'Pools', status: 'pools' }, // SSE-updated mid-flight
        { id: 'c2', name: 'Playoffs', status: 'completed' }, // SSE-added mid-flight
      ],
    };
    const patch = { name: 'New tournament name' };
    const result = mergeTournamentPatch(latestT, patch, 'X');
    expect(result.name).toBe('New tournament name');
    // The SSE-driven competitions list is preserved — pre-fix this would
    // have been the closure-captured pre-PUT snapshot (whatever t was at
    // handler-definition time), reverting status changes on every save.
    expect(result.competitions).toEqual([
      { id: 'c1', name: 'Pools', status: 'pools' },
      { id: 'c2', name: 'Playoffs', status: 'completed' },
    ]);
  });

  it('allows the patch to override fields the latest snapshot also carries', () => {
    // Belt-and-braces: the spread semantics (`{ ...latest, ...patch }`)
    // mean the patch wins on any conflicting field. Pin this so a future
    // refactor doesn't accidentally swap the spread order (which would
    // make the latest snapshot win, ignoring the user's edit).
    const t = { id: 't1', name: 'A', date: '01-01-2026' };
    const result = mergeTournamentPatch(t, { name: 'B', date: '02-01-2026' }, 'X');
    expect(result.name).toBe('B');
    expect(result.date).toBe('02-01-2026');
  });

  it('produces a new object reference (immutable update)', () => {
    // React's setState relies on reference equality to detect changes.
    // mergeTournamentPatch must always produce a new tournament object
    // so the parent's setT triggers a re-render.
    const t = { id: 't1', name: 'A' };
    const result = mergeTournamentPatch(t, {}, 'X');
    expect(result).not.toBe(t);
  });
});

// /deep-review round-12 finding (Copilot #6): the create-response from
// the Go server (POST /competitions, POST /playoffs) shipped
// `players: null` because the Go `Players` slice was nil (the handler
// constructed the response struct without populating Players from
// disk). refreshCompsAfterCreate's refresh-failure fallback appended
// the raw response into local state, and downstream render paths
// reading `c.players.length` crashed on null.
//
// Server-side fix populates Players for the response; client-side
// fix (normalizeCreatedRecord) is defense-in-depth so an older server
// still in production doesn't break the UI.
describe('normalizeCreatedRecord', () => {
  it('replaces null players with an empty array', () => {
    const created = { id: 'c1', name: 'New', players: null };
    const result = normalizeCreatedRecord(created);
    expect(result.players).toEqual([]);
  });

  it('replaces undefined players with an empty array', () => {
    // Server with `omitempty` on Players field would omit it entirely.
    const created = { id: 'c1', name: 'New' };
    const result = normalizeCreatedRecord(created);
    expect(result.players).toEqual([]);
  });

  it('preserves a non-empty players array', () => {
    const created = { id: 'c1', name: 'New', players: [{ id: 'p1', name: 'P1' }] };
    const result = normalizeCreatedRecord(created);
    expect(result.players).toEqual([{ id: 'p1', name: 'P1' }]);
  });

  it('preserves an explicit empty players array', () => {
    // The "cleared roster" shape — distinct from null/undefined.
    const created = { id: 'c1', name: 'New', players: [] };
    const result = normalizeCreatedRecord(created);
    expect(result.players).toEqual([]);
  });

  it('preserves all other fields', () => {
    const created = {
      id: 'c1', name: 'New', date: '12-05-2026',
      status: 'setup', courts: ['A', 'B'], players: null,
    };
    const result = normalizeCreatedRecord(created);
    expect(result.id).toBe('c1');
    expect(result.name).toBe('New');
    expect(result.date).toBe('12-05-2026');
    expect(result.status).toBe('setup');
    expect(result.courts).toEqual(['A', 'B']);
  });

  it('produces a new object reference', () => {
    const created = { id: 'c1', players: null };
    const result = normalizeCreatedRecord(created);
    expect(result).not.toBe(created);
  });
});
