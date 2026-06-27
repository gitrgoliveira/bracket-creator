import { describe, it, expect } from 'vitest';
import {
  buildRunningIpponResult,
  loadScoreboardPoints,
  swissRoundIDPrefix,
  filterSwissRoundMatches,
  isSwissRoundComplete,
  canGenerateNextSwissRound,
  isSwissCompetitionComplete,
  formatCompMinutes,
} from '../admin_competition.jsx';

// Copilot finding on PR #103: RunningMatchPanel's scoreboard mode supports
// 2-ippon wins (and 2-1 with loser points), but the old recordWinner
// only ever recorded a 1-ippon result (winnerPts=1, single-letter
// array). The fix lifts the result-building logic into the pure
// buildRunningIpponResult helper, which consumes the full points arrays.
//
// Kendo win conditions covered:
//   - 2 ippons (sansoo)             — automatic win
//   - 1 ippon at time-up            — winner if opponent has 0
//   - 1 ippon vs 1 with one ahead   — same shape (2-1, 3-2 not allowed)
// Draws (0-0, 1-1, 2-2) go through the full editor's hikiwake toggle,
// not this helper — scoreboard submit is disabled in those states.
//
// Tests below pin every adversarial-input case so a future refactor
// can't silently re-introduce the truncation.

const SIDE_A = { id: "a1", name: "Akira", dojo: "Tora" };
const SIDE_B = { id: "b1", name: "Hiroshi", dojo: "Tora" };

describe('buildRunningIpponResult', () => {
  describe('1-ippon win (tap / card mode contract)', () => {
    it('side A win, no letters passed → defaults to ["M"]', () => {
      const r = buildRunningIpponResult("a", SIDE_A, SIDE_B);
      expect(r.winner).toBe(SIDE_A);
      expect(r.status).toBe("completed");
      expect(r.ipponsA).toEqual(["M"]);
      expect(r.ipponsB).toEqual([]);
      expect(r.score).toEqual({
        type: "ippon",
        winnerPts: 1,
        loserPts: 0,
        ippons: ["M"],
        fouls: { a: 0, b: 0 },
      });
    });

    it('side B win, no letters passed → defaults to ["M"]', () => {
      const r = buildRunningIpponResult("b", SIDE_A, SIDE_B);
      expect(r.winner).toBe(SIDE_B);
      expect(r.ipponsA).toEqual([]);
      expect(r.ipponsB).toEqual(["M"]);
      expect(r.score.winnerPts).toBe(1);
      expect(r.score.ippons).toEqual(["M"]);
    });

    it('side A win with explicit single letter ["K"]', () => {
      const r = buildRunningIpponResult("a", SIDE_A, SIDE_B, ["K"]);
      expect(r.ipponsA).toEqual(["K"]);
      expect(r.ipponsB).toEqual([]);
      expect(r.score.winnerPts).toBe(1);
      expect(r.score.ippons).toEqual(["K"]);
    });

    it('empty array → falls back to ["M"] (the legacy default)', () => {
      // [] is empty/falsy-by-length so the helper substitutes ["M"].
      // Keeps the tap-mode "no letter at all" path working when a
      // caller accidentally hands in [] instead of undefined.
      const r = buildRunningIpponResult("a", SIDE_A, SIDE_B, []);
      expect(r.ipponsA).toEqual(["M"]);
      expect(r.score.winnerPts).toBe(1);
    });

    it('null winnerIppons → falls back to ["M"]', () => {
      const r = buildRunningIpponResult("a", SIDE_A, SIDE_B, null);
      expect(r.ipponsA).toEqual(["M"]);
    });

    // Scoreboard-mode 1-0 (time-up) win: the user entered one letter for
    // the winner and zero for the loser, then time ran out and they hit
    // Submit. Both arrays are passed explicitly (unlike the tap/card
    // cases which pass undefined for the loser side). The previous tests
    // cover this implicitly via the empty-loserIppons default; this case
    // pins it explicitly so a future refactor that changes the default
    // doesn't silently break the most common scoreboard flow.
    it('side A scoreboard 1-0 (time-up): explicit empty loser array', () => {
      const r = buildRunningIpponResult("a", SIDE_A, SIDE_B, ["M"], []);
      expect(r.winner).toBe(SIDE_A);
      expect(r.ipponsA).toEqual(["M"]);
      expect(r.ipponsB).toEqual([]);
      expect(r.score.winnerPts).toBe(1);
      expect(r.score.loserPts).toBe(0);
    });

    it('side B scoreboard 1-0 (time-up): symmetric to side A', () => {
      const r = buildRunningIpponResult("b", SIDE_A, SIDE_B, ["D"], []);
      expect(r.winner).toBe(SIDE_B);
      expect(r.ipponsA).toEqual([]);
      expect(r.ipponsB).toEqual(["D"]);
      expect(r.score.winnerPts).toBe(1);
      expect(r.score.loserPts).toBe(0);
    });
  });

  describe('2-ippon win (the Copilot finding)', () => {
    it('side A 2-0 win: ipponsA has both letters, ipponsB is empty', () => {
      const r = buildRunningIpponResult("a", SIDE_A, SIDE_B, ["M", "K"], []);
      expect(r.winner).toBe(SIDE_A);
      expect(r.ipponsA).toEqual(["M", "K"]);
      expect(r.ipponsB).toEqual([]);
      expect(r.score.winnerPts).toBe(2);
      expect(r.score.loserPts).toBe(0);
      expect(r.score.ippons).toEqual(["M", "K"]);
    });

    it('side B 2-0 win: ipponsB has both letters', () => {
      const r = buildRunningIpponResult("b", SIDE_A, SIDE_B, ["D", "T"], []);
      expect(r.winner).toBe(SIDE_B);
      expect(r.ipponsA).toEqual([]);
      expect(r.ipponsB).toEqual(["D", "T"]);
      expect(r.score.winnerPts).toBe(2);
      expect(r.score.ippons).toEqual(["D", "T"]);
    });

    it('side A 2-1 win: loser keeps their single ippon (loserPts=1)', () => {
      // Pre-fix the loser's letter was dropped on the floor — only the
      // winner's first letter survived. The 2-1 case is the most likely
      // place where the truncation matters: the user entered detail for
      // both sides, but only the winner's first letter persisted.
      const r = buildRunningIpponResult("a", SIDE_A, SIDE_B, ["M", "K"], ["D"]);
      expect(r.ipponsA).toEqual(["M", "K"]);
      expect(r.ipponsB).toEqual(["D"]);
      expect(r.score.winnerPts).toBe(2);
      expect(r.score.loserPts).toBe(1);
    });

    it('side B 2-1 win: symmetric — loser letters land in ipponsA', () => {
      const r = buildRunningIpponResult("b", SIDE_A, SIDE_B, ["K", "T"], ["M"]);
      expect(r.ipponsA).toEqual(["M"]);
      expect(r.ipponsB).toEqual(["K", "T"]);
      expect(r.score.winnerPts).toBe(2);
      expect(r.score.loserPts).toBe(1);
    });
  });

  describe('schema invariants', () => {
    it('always sets status="completed" (running panel never schedules)', () => {
      expect(buildRunningIpponResult("a", SIDE_A, SIDE_B).status).toBe("completed");
      expect(buildRunningIpponResult("b", SIDE_A, SIDE_B, ["M", "K"]).status).toBe("completed");
    });

    it('always sets type="ippon" (hikiwake/hantei not supported here)', () => {
      // Draws go through admin_scoring_modal.jsx's full editor with the
      // hikiwake toggle. Hantei wins go through the card-mode hantei
      // button → onRecord("a"|"b", "hantei") path which doesn't hit
      // recordWinner's ippon builder at all.
      expect(buildRunningIpponResult("a", SIDE_A, SIDE_B).score.type).toBe("ippon");
    });

    it('fouls always zero (running panel doesn\'t expose hansoku)', () => {
      const r = buildRunningIpponResult("a", SIDE_A, SIDE_B, ["M", "K"]);
      expect(r.score.fouls).toEqual({ a: 0, b: 0 });
    });

    it('winner field is the correct side object', () => {
      expect(buildRunningIpponResult("a", SIDE_A, SIDE_B).winner).toBe(SIDE_A);
      expect(buildRunningIpponResult("b", SIDE_A, SIDE_B).winner).toBe(SIDE_B);
    });
  });
});

describe('loadScoreboardPoints', () => {
  // Companion bug to the 2-ippon truncation Copilot found. The previous
  // RunningMatchPanel useEffect loaded aPoints/bPoints from
  // `match.score.ippons` (winner-only) gated by `winner.id === sideX.id`,
  // which silently dropped the LOSER's letters on every render. Once
  // buildRunningIpponResult started writing 2-1 wins correctly (loser's
  // single ippon preserved), the loader's truncation surfaced — a 2-1
  // win came back as 2-0 and re-submission re-truncated it.
  //
  // Fix reads from `match.ipponsA` / `match.ipponsB` directly. Tests pin
  // every adversarial case so a future refactor can't silently
  // re-introduce the asymmetry.

  describe('defensive empties', () => {
    it('null match → { aPoints: [], bPoints: [] }', () => {
      expect(loadScoreboardPoints(null)).toEqual({ aPoints: [], bPoints: [] });
    });

    it('undefined match → { aPoints: [], bPoints: [] }', () => {
      expect(loadScoreboardPoints(undefined)).toEqual({ aPoints: [], bPoints: [] });
    });

    it('match with no ipponsA/ipponsB → empty arrays', () => {
      expect(loadScoreboardPoints({ id: "m1" })).toEqual({ aPoints: [], bPoints: [] });
    });

    it('match with empty ipponsA/ipponsB arrays → empty arrays', () => {
      expect(loadScoreboardPoints({ ipponsA: [], ipponsB: [] })).toEqual({ aPoints: [], bPoints: [] });
    });
  });

  describe('ippon-mode reads (the read side of buildRunningIpponResult writes)', () => {
    it('1-0 side A win round-trip: aPoints=["M"], bPoints=[]', () => {
      // buildRunningIpponResult("a", A, B, ["M"], []) wrote ipponsA=["M"], ipponsB=[].
      // loadScoreboardPoints should read it back identically.
      const r = loadScoreboardPoints({
        ipponsA: ["M"],
        ipponsB: [],
        score: { type: "ippon" },
        winner: { id: "a1" },
        sideA: { id: "a1" },
      });
      expect(r).toEqual({ aPoints: ["M"], bPoints: [] });
    });

    it('2-0 side A win round-trip: aPoints=["M","K"], bPoints=[]', () => {
      const r = loadScoreboardPoints({
        ipponsA: ["M", "K"],
        ipponsB: [],
        score: { type: "ippon" },
      });
      expect(r).toEqual({ aPoints: ["M", "K"], bPoints: [] });
    });

    it('2-1 side A win round-trip: aPoints=["M","K"], bPoints=["D"] — the bug case', () => {
      // Pre-fix: loader returned aPoints=["M","K"] (from score.ippons,
      // which was the winner's letters) and bPoints=[] (winner.id !==
      // sideB.id). So a 2-1 win came back as 2-0 in the UI even though
      // the backend persisted 2-1 correctly. Re-submission would then
      // re-write 2-0 and drop the loser's letter on the server too.
      const r = loadScoreboardPoints({
        ipponsA: ["M", "K"],
        ipponsB: ["D"],
        score: { type: "ippon" },
      });
      expect(r).toEqual({ aPoints: ["M", "K"], bPoints: ["D"] });
    });

    it('2-1 side B win round-trip: symmetric', () => {
      const r = loadScoreboardPoints({
        ipponsA: ["M"],
        ipponsB: ["K", "T"],
        score: { type: "ippon" },
      });
      expect(r).toEqual({ aPoints: ["M"], bPoints: ["K", "T"] });
    });
  });

  describe('hikiwake mode (post-fix: loads tied state too)', () => {
    it('1-1 hikiwake: aPoints=["M"], bPoints=["K"]', () => {
      // Pre-fix the gate `score.type === "ippon"` meant hikiwake matches
      // loaded empty arrays. Post-fix the loader is permissive: it shows
      // the saved tied state so the operator can adjust. Submit stays
      // disabled until one side strictly leads, so there's no accidental
      // hikiwake-converted-to-ippon risk.
      const r = loadScoreboardPoints({
        ipponsA: ["M"],
        ipponsB: ["K"],
        score: { type: "hikiwake" },
      });
      expect(r).toEqual({ aPoints: ["M"], bPoints: ["K"] });
    });

    it('0-0 hikiwake: still empty', () => {
      const r = loadScoreboardPoints({
        ipponsA: [],
        ipponsB: [],
        score: { type: "hikiwake" },
      });
      expect(r).toEqual({ aPoints: [], bPoints: [] });
    });
  });

  describe('placeholder filtering', () => {
    it('filters out "•" empty-slot placeholders (matches scoring modal pattern)', () => {
      // The full editor uses "•" to mark empty slots in its 2-element
      // ippon arrays. Running panel never writes "•" but data may round-trip
      // through the full editor — filtering defensively keeps the
      // scoreboard display clean.
      const r = loadScoreboardPoints({
        ipponsA: ["M", "•"],
        ipponsB: ["•", "K"],
        score: { type: "ippon" },
      });
      expect(r).toEqual({ aPoints: ["M"], bPoints: ["K"] });
    });

    it('filters out falsy entries (undefined, empty string)', () => {
      const r = loadScoreboardPoints({
        ipponsA: ["M", "", undefined],
        ipponsB: [null, "K"],
        score: { type: "ippon" },
      });
      expect(r).toEqual({ aPoints: ["M"], bPoints: ["K"] });
    });
  });
});

// ── H3 regression: AdminSettings useEffect deps completeness ──
//
// AdminSettings syncs server-pushed changes into local state via a
// useEffect whose deps list every c.* field rendered in the JSX (via
// `local.*`). A missing dep means an SSE update to that field won't
// propagate — the user sees stale data (the UI keeps showing the
// pre-SSE value).
//
// This used to ALSO matter for save correctness because saveNow spread
// `{ ...c, ...next }` into the PUT — so a stale `local.status` would
// be PUT back over a server-side status change. That's now handled
// independently by the saveNow whitelist below (it builds the PUT
// body from settings fields only, never including non-settings fields
// like status/players/hasParticipantIDs). The deps-list test is still
// required for UI freshness; the whitelist test is required for save
// safety. Both decoupled = a missing dep is a UI bug only, not a
// silent server-state revert.
//
// If you add a new `local.foo` reference in AdminSettings' JSX, add
// `c.foo` to the useEffect deps AND add "foo" to EXPECTED_DEPS below.
// If you add a new settings field that saveNow should PUT, add it to
// SAVE_NOW_FIELDS (and EXPECTED_DEPS so the UI stays in sync).
import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));

describe('AdminSettings useEffect deps completeness (H3 regression)', () => {
  // Fields the sync useEffect must list as deps. Two reasons a field
  // lands here:
  //
  //   (a) The JSX reads `local.<field>` — without `c.<field>` in deps,
  //       SSE-pushed changes don't propagate and the UI shows stale
  //       data. Example: `local.status` is read for the delete-warning
  //       prompt, so SSE-driven status changes must update local.
  //
  //   (b) saveNow PUTs `next.<field>` (allowlist) — without `c.<field>`
  //       in deps, a concurrent admin's PUT (broadcast via SSE) won't
  //       update local, and the next save of any other field would PUT
  //       a stale value over the server's update. Example: `mirror` is
  //       not in the JSX but IS in saveNow's allowlist, so it needs to
  //       round-trip through local for the same defense.
  //
  // If you add a `local.<field>` reference OR a `<field>: next.<field>`
  // entry in finalNext, add `<field>` here AND add `c.<field>` to the
  // sync useEffect deps array.
  const EXPECTED_DEPS = [
    'id', 'name', 'date', 'startTime',
    'poolSize', 'poolWinners', 'poolSizeMode',
    'courts', 'roundRobin', 'withZekkenName',
    'teamSize', 'numberPrefix',
    'format', 'kind',
    // status: JSX-read (delete-warning prompt)
    'status',
    // mirror: saveNow-allowlist (defense against zero-value clobber)
    'mirror',
    // FR-050 / T044: poolFormat round-trips through saveNow's PUT body,
    // so it needs a dep to absorb SSE-pushed concurrent admin changes.
    'poolFormat',
    // FR-052..FR-054 / T047: per-phase duration inputs are rendered in
    // the settings form AND round-tripped via finalNext. Sync deps
    // required so an SSE-driven update lands in local state while the
    // user is on the settings page.
    'poolMatchDuration', 'playoffMatchDuration',
  ];

  it('useEffect deps include every field rendered via local.*', () => {
    const src = readFileSync(
      resolve(__dirname, '..', 'admin_competition_settings.jsx'),
      'utf8'
    );

    // Find the sync-to-local useEffect. After round-15, the body merges
    // field-by-field with an edited-fields guard (Copilot finding on
    // the prior `{ ...prev, ...c }` overwrite that lost user edits
    // during the debounce window). The distinctive marker is
    // `editedFieldsRef.current.has(k)` inside the Object.keys(c) loop.
    // Extract the deps array from the closing `}, [c.id, c.name, ...])`
    // line.
    const depsMatch = src.match(
      /editedFieldsRef\.current\.has\(k\)[\s\S]*?\}, \[([^\]]+)\]\)/
    );
    expect(depsMatch).not.toBeNull();

    const depsLine = depsMatch[1];
    for (const field of EXPECTED_DEPS) {
      const pattern = `c.${field}`;
      expect(
        depsLine.includes(pattern),
        `expected deps to include ${pattern} — if you added local.${field} to the JSX, add c.${field} to the useEffect deps`
      ).toBe(true);
    }
  });
});

// ── saveNow payload whitelist ──
//
// AdminSettings.saveNow used to spread `{ ...c, ...next }` into the
// PUT body, which carried fields like status/players/hasParticipantIDs
// that AdminSettings doesn't manage. If the sync-to-local deps array
// was incomplete for any such field (Copilot finding on the H3 sync
// effect), a setting save would PUT a stale value back over the
// server-side change.
//
// Fix: build the finalNext object from a fixed allowlist of settings
// fields, ignoring whatever else is in `local`. This makes
// AdminSettings genuinely settings-only at the wire boundary and
// decouples save correctness from the sync effect's dep coverage.
//
// Structural test: read the source and verify finalNext's object
// literal contains ONLY allowlisted keys (so a future refactor that
// re-introduces a `{ ...c, ...next }` spread can't sneak past CI).

describe('AdminSettings.saveNow payload whitelist', () => {
  // Settings fields the PUT body is allowed to include. Must match
  // (a) the OpenAPI settings list in specs/openapi.yaml for PUT
  //     /competitions/{id} and (b) the backend transform in
  //     handlers_competition.go (which copies these fields from body
  //     onto disk). Server-managed fields (status, players,
  //     hasParticipantIDs) and viewer-derived fields (poolMatches,
  //     pools, bracket, schedule) must NOT appear.
  //
  // `mirror` IS in this allowlist even though AdminSettings doesn't
  // show a Mirror checkbox: data.jsx:200 defaults `mirror: true` for
  // new competitions and the backend transform unconditionally writes
  // `current.Mirror = comp.Mirror`. Omitting it would JSON-encode to
  // false and clobber the disk value on every settings save.
  const ALLOWED = new Set([
    'id', 'name', 'date', 'startTime',
    'poolSize', 'poolWinners', 'poolSizeMode',
    'courts', 'roundRobin', 'withZekkenName',
    'teamSize', 'numberPrefix',
    'format', 'kind',
    'mirror',
    // FR-050 / T044: round-robin shape selector.
    'poolFormat',
    // FR-052..FR-054 / T047: per-phase duration overrides. Zero means
    // "use legacy default" — fall through to backend ApplyCompetitionDefaults.
    'poolMatchDuration', 'playoffMatchDuration',
    // FR-050a / T190: Swiss rounds (number of rounds the operator
    // configured for a Swiss-format competition). Editable pre-start
    // and during play; the next "Generate next round" call respects
    // the latest value.
    'swissRounds',
    // Naginata support: round-trips the flag so a settings save doesn't
    // clobber a previously-set naginata: true with Go's zero-value false.
    'naginata',
    // mp-6nq: per-competition check-in tracking flag.
    'checkInEnabled',
    // Phase 3b (mp-8rc9): league tie-breaker config. Only meaningful for
    // team-league competitions; safe to include for all formats because
    // the backend PUT allowlist ignores unknown fields.
    'leagueTiebreakTopN',
    'leagueTwoThirdPlaces',
    // Round-tripped (no UI control) to avoid clobbering a kachinuki
    // competition's value to "" on a settings save.
    'teamMatchType',
  ]);
  // Fields that MUST NOT appear in the PUT body — pinning the
  // negative invariant explicitly so a careless re-add is caught.
  const FORBIDDEN = ['status', 'players', 'hasParticipantIDs', 'poolMatches', 'pools', 'bracket', 'schedule'];

  it('finalNext contains only allowlisted settings keys', () => {
    const src = readFileSync(
      resolve(__dirname, '..', 'admin_competition_settings.jsx'),
      'utf8'
    );

    // Find `const finalNext = { ... };` and extract its top-level keys.
    // The literal is multi-line, so match through the closing brace.
    const fnMatch = src.match(/const finalNext = \{([\s\S]*?)\n\s*\};/);
    expect(fnMatch, 'expected `const finalNext = { ... };` in saveNow').not.toBeNull();
    const body = fnMatch[1];

    // No spread allowed — saveNow used to do `{ ...c, ...next, ... }`
    // and that's exactly the regression we want to prevent.
    expect(/\.\.\./.test(body), 'finalNext must not use spread — list each field explicitly').toBe(false);

    // Extract key names (everything before `:` per line, ignoring
    // strings/comments). Simple but sufficient for an object literal.
    const keys = [];
    for (const line of body.split('\n')) {
      const m = line.match(/^\s*([a-zA-Z_$][a-zA-Z0-9_$]*)\s*:/);
      if (m) keys.push(m[1]);
    }
    expect(keys.length, 'finalNext appears to have no fields — parse failed').toBeGreaterThan(0);

    for (const k of keys) {
      expect(ALLOWED.has(k), `finalNext key "${k}" is not in the settings allowlist — either add it to ALLOWED here (if it really is a setting) or remove it from finalNext`).toBe(true);
    }
    for (const forbidden of FORBIDDEN) {
      expect(keys.includes(forbidden), `finalNext must NOT include "${forbidden}" — that field is server-managed via a dedicated endpoint, not settings`).toBe(false);
    }
  });

  // Numeric finalNext fields (poolSize / poolWinners / teamSize) must
  // wrap their value in the safeInt fallback. The bug shape: cleared
  // number input stores NaN in local; if user then edits a non-numeric
  // field, saveNow PUTs next.poolSize=NaN → JSON encodes null → Go
  // binds 0 → backend transform writes PoolSize=0 → disk clobbered.
  // safeInt(next.X, c.X) preserves disk value when local isn't a
  // usable positive integer.
  it('finalNext numeric fields are wrapped in safeInt fallback', () => {
    const src = readFileSync(
      resolve(__dirname, '..', 'admin_competition_settings.jsx'),
      'utf8'
    );
    const fnMatch = src.match(/const finalNext = \{([\s\S]*?)\n\s*\};/);
    expect(fnMatch).not.toBeNull();
    const body = fnMatch[1];

    for (const field of ['poolSize', 'poolWinners', 'teamSize']) {
      const pattern = new RegExp(`\\b${field}:\\s*safeInt\\(`);
      expect(
        pattern.test(body),
        `finalNext.${field} must use safeInt(...) — raw next.${field} is NaN-clobber prone (JSON.stringify({${field}: NaN}) → "${field}":null → Go zero-value)`
      ).toBe(true);
    }
  });

  // The safeInt helper itself must check the full set of dimensions
  // that the NaN-clobber bug requires: Number.isFinite (catches NaN /
  // Infinity), Number.isInteger (catches fractional), and >= 1 (catches
  // 0 / negative). Pin the full set so a future "simplification" that
  // drops Number.isInteger doesn't silently re-introduce the
  // fractional-clobber dimension.
  it('safeInt helper guards isFinite + isInteger + >= 1', () => {
    const src = readFileSync(
      resolve(__dirname, '..', 'admin_competition_settings.jsx'),
      'utf8'
    );
    const safeIntMatch = src.match(/const safeInt = \(v, fallback\) =>\s*([^;]+);/);
    expect(safeIntMatch, 'expected `const safeInt = (v, fallback) => ...;` in saveNow').not.toBeNull();
    const body = safeIntMatch[1];
    expect(body, 'safeInt must guard Number.isFinite').toContain('Number.isFinite');
    expect(body, 'safeInt must guard Number.isInteger').toContain('Number.isInteger');
    expect(body, 'safeInt must require >= 1').toMatch(/>=\s*1/);
  });
});

// ── saveNow reads latest c + edited overlay (Copilot round-15) ──
//
// Pre-fix: saveLater(next) captured `next` at keystroke time; the
// debounce-gate sync effect dropped SSE updates during the 400ms
// window; saveNow then PUT the stale captured snapshot, silently
// reverting concurrent admin changes to fields the user wasn't
// editing. The fix has three structural invariants we pin here:
//
//   1. saveLater takes NO snapshot arg — it relies on refs at fire time.
//   2. saveNow builds an `effective` object by overlaying `localRef.current`
//      values onto `cRef.current` for each field in `editedFieldsRef`.
//   3. update / updateNow / updateNumber call `editedFieldsRef.current.add(k)`
//      before scheduling the save.
//
// Behavioral tests for the full lifecycle are blocked by vitest.setup's
// stubbed React hooks; structural tests are the durable mechanism.
describe('AdminSettings saveNow stale-snapshot fix (Copilot round-15)', () => {
  const src = readFileSync(
    resolve(__dirname, '..', 'admin_competition_settings.jsx'),
    'utf8'
  );

  // Non-vacuity guard (mp-hpe3): AdminSettings was split into
  // admin_competition_settings.jsx. If this path ever points at a module that
  // doesn't contain AdminSettings, the source-introspection regexes below would
  // match nothing and could pass trivially — fail loudly instead.
  it('reads the module that actually defines AdminSettings', () => {
    expect(src).toContain('function AdminSettings');
  });

  it('uses manual save — no debounced autosave timer (mp-3xn6)', () => {
    // Competition Settings was converted from debounced autosave to explicit
    // "Save changes" (matching the Tournament Edit-details page). The old
    // `saveLater` debounce must be gone, and saveNow must be reachable from an
    // explicit Save button rather than fired from the edit handlers.
    expect(src).not.toContain('saveLater');
    expect(src).not.toContain('debounceRef');
    // An explicit Save control wired to saveNow.
    expect(src).toMatch(/onClick=\{saveNow\}/);
    // The edit handlers must NOT auto-persist: no save call inside update/
    // updateNow/updateNumber bodies.
    for (const handler of ['update', 'updateNow', 'updateNumber']) {
      const re = new RegExp(`const ${handler} = \\(([^)]*)\\) => \\{([\\s\\S]*?)\\n {2}\\};`);
      const m = src.match(re);
      expect(m, `expected \`const ${handler} = (...) => { ... };\``).not.toBeNull();
      expect(m[2], `${handler} must not save automatically`).not.toContain('saveNow(');
    }
  });

  it('saveNow builds effective from cRef + editedFieldsRef overlay', () => {
    // Match the start of saveNow's body and look for the three
    // distinctive identifiers in the overlay block.
    const m = src.match(/const saveNow = \(\) => \{([\s\S]*?)Promise\.resolve\(onUpdate/);
    expect(m, 'expected `const saveNow = () => { ... Promise.resolve(onUpdate` block').not.toBeNull();
    const body = m[1];
    expect(body).toContain('cRef.current');
    expect(body).toContain('localRef.current');
    expect(body).toContain('editedFieldsRef.current.forEach');
  });

  it('user-edit handlers mark fields via editedFieldsRef.add', () => {
    // Each handler that mutates `local` must mark the edited field
    // BEFORE scheduling the save, so the sync effect preserves it
    // when SSE arrives during the debounce window.
    for (const handler of ['update', 'updateNow', 'updateNumber']) {
      const re = new RegExp(`const ${handler} = \\(([^)]*)\\) => \\{([\\s\\S]*?)\\n {2}\\};`);
      const m = src.match(re);
      expect(m, `expected \`const ${handler} = (...) => { ... };\` declaration`).not.toBeNull();
      expect(
        m[2],
        `${handler} must call editedFieldsRef.current.add(...) so the sync effect preserves the user's edit during the debounce window`
      ).toContain('editedFieldsRef.current.add');
      // Copilot finding (PR #320 round 4): saveNow reads localRef.current at
      // click time. The useEffect that syncs localRef from `local` is async,
      // so a rapid edit-then-Save click can land before the effect runs and
      // miss the latest edit. Each handler must therefore write localRef.current
      // synchronously inside the setLocal updater.
      expect(
        m[2],
        `${handler} must write localRef.current = next inside its setLocal updater so saveNow sees the latest staged value even when the operator clicks Save immediately after editing`
      ).toContain('localRef.current = next');
    }
  });

  it('sync effect uses editedFieldsRef.has guard, not blanket debounceRef gate', () => {
    // Pre-fix sync effect: `if (debounceRef.current) return prev;`
    // — dropped ALL updates during the debounce, losing SSE changes to
    // fields the user wasn't editing. Post-fix: per-field check via
    // `editedFieldsRef.current.has(k)` inside Object.keys(c).
    expect(src).toContain('editedFieldsRef.current.has(k)');
    // The blanket gate must be gone. The simple textual check would
    // false-positive on other debounceRef usages (saveLater +
    // cleanup), so anchor on the specific shape: `if
    // (debounceRef.current) return prev` was the bug, scoped to the
    // sync effect.
    expect(src).not.toMatch(/if \(debounceRef\.current\)\s+return prev/);
  });
});

// ============================================================
// Swiss helpers (T190–T193, FR-050a / FR-050d / FR-050e)
// ============================================================
//
// The Swiss-round admin section ships four pure helpers that together
// implement the "current round complete, generate next" decision flow.
// Exported separately so the conditional logic can be pinned without
// mounting AdminSwissRounds (the React vitest setup stubs hooks at the
// component level — only the helpers can be exercised here).

describe('swissRoundIDPrefix', () => {
  // Must match engine/swiss.go swissMatchID format: "Swiss-R{N}-".
  // A drift here would cause filterSwissRoundMatches to silently
  // return [] for a round that has matches on the server.
  it('formats round 1 as "Swiss-R1-"', () => {
    expect(swissRoundIDPrefix(1)).toBe('Swiss-R1-');
  });

  it('formats round 4 as "Swiss-R4-"', () => {
    expect(swissRoundIDPrefix(4)).toBe('Swiss-R4-');
  });

  it('formats round 10 as "Swiss-R10-"', () => {
    expect(swissRoundIDPrefix(10)).toBe('Swiss-R10-');
  });
});

describe('filterSwissRoundMatches', () => {
  const r1m0 = { id: 'Swiss-R1-0', status: 'completed' };
  const r1m1 = { id: 'Swiss-R1-1', status: 'completed' };
  const r2m0 = { id: 'Swiss-R2-0', status: 'scheduled' };
  const r2m1 = { id: 'Swiss-R2-1', status: 'running' };

  it('returns only the matches for the given round', () => {
    const all = [r1m0, r1m1, r2m0, r2m1];
    expect(filterSwissRoundMatches(all, 1)).toEqual([r1m0, r1m1]);
    expect(filterSwissRoundMatches(all, 2)).toEqual([r2m0, r2m1]);
  });

  it('returns [] for a round that has no matches yet', () => {
    expect(filterSwissRoundMatches([r1m0, r1m1], 3)).toEqual([]);
  });

  it('returns [] for null/undefined/empty inputs', () => {
    expect(filterSwissRoundMatches(null, 1)).toEqual([]);
    expect(filterSwissRoundMatches(undefined, 1)).toEqual([]);
    expect(filterSwissRoundMatches([], 1)).toEqual([]);
  });

  it('returns [] for invalid round numbers (0, -1, null)', () => {
    expect(filterSwissRoundMatches([r1m0], 0)).toEqual([]);
    expect(filterSwissRoundMatches([r1m0], -1)).toEqual([]);
    expect(filterSwissRoundMatches([r1m0], null)).toEqual([]);
  });

  it('does NOT confuse "Swiss-R1-" prefix with "Swiss-R10-"', () => {
    // If filterSwissRoundMatches used a contains check instead of
    // startsWith on the canonical prefix, round 1 would match round
    // 10's matches too. Pin the behaviour explicitly.
    const r10m0 = { id: 'Swiss-R10-0', status: 'scheduled' };
    expect(filterSwissRoundMatches([r1m0, r10m0], 1)).toEqual([r1m0]);
    expect(filterSwissRoundMatches([r1m0, r10m0], 10)).toEqual([r10m0]);
  });

  it('ignores non-Swiss matches (e.g. pool matches with same shape)', () => {
    // A pool-match ID like "PoolA-0" must not slip into a Swiss round.
    const poolM = { id: 'PoolA-0', status: 'completed' };
    expect(filterSwissRoundMatches([poolM, r1m0], 1)).toEqual([r1m0]);
  });
});

describe('isSwissRoundComplete', () => {
  it('true when every match is completed', () => {
    expect(isSwissRoundComplete([
      { status: 'completed' },
      { status: 'completed' },
    ])).toBe(true);
  });

  it('false when any match is still scheduled', () => {
    expect(isSwissRoundComplete([
      { status: 'completed' },
      { status: 'scheduled' },
    ])).toBe(false);
  });

  it('false when any match is running', () => {
    expect(isSwissRoundComplete([
      { status: 'completed' },
      { status: 'running' },
    ])).toBe(false);
  });

  it('false for empty list (unbegun round is not complete)', () => {
    // The Generate Next Round button must stay disabled when there
    // are no matches yet — pre-fix, returning true for [] would
    // enable the button before round 1 even exists.
    expect(isSwissRoundComplete([])).toBe(false);
    expect(isSwissRoundComplete(null)).toBe(false);
    expect(isSwissRoundComplete(undefined)).toBe(false);
  });
});

describe('canGenerateNextSwissRound', () => {
  const mkComp = (overrides) => ({
    format: 'swiss',
    swissRounds: 4,
    swissCurrentRound: 1,
    ...overrides,
  });
  const completedR1 = [
    { id: 'Swiss-R1-0', status: 'completed' },
    { id: 'Swiss-R1-1', status: 'completed' },
  ];
  const incompleteR1 = [
    { id: 'Swiss-R1-0', status: 'completed' },
    { id: 'Swiss-R1-1', status: 'scheduled' },
  ];

  it('true when format=swiss, round complete, more rounds remaining', () => {
    expect(canGenerateNextSwissRound(mkComp(), completedR1)).toBe(true);
  });

  it('false when format !== swiss', () => {
    expect(canGenerateNextSwissRound(mkComp({ format: 'mixed' }), completedR1)).toBe(false);
    expect(canGenerateNextSwissRound(mkComp({ format: 'playoffs' }), completedR1)).toBe(false);
  });

  it('false when current round still has incomplete matches', () => {
    expect(canGenerateNextSwissRound(mkComp(), incompleteR1)).toBe(false);
  });

  it('false when all configured rounds are done (current >= total)', () => {
    expect(canGenerateNextSwissRound(mkComp({ swissCurrentRound: 4 }), completedR1)).toBe(false);
    expect(canGenerateNextSwissRound(mkComp({ swissCurrentRound: 5 }), completedR1)).toBe(false);
  });

  it('false when no rounds configured', () => {
    expect(canGenerateNextSwissRound(mkComp({ swissRounds: 0 }), completedR1)).toBe(false);
  });

  it('false when current round is 0 (setup, round 1 not generated)', () => {
    // Operator hasn't hit Start yet — must press "Start competition"
    // first; the Generate Next Round button is not a substitute.
    expect(canGenerateNextSwissRound(mkComp({ swissCurrentRound: 0 }), [])).toBe(false);
  });

  it('false for null/missing competition', () => {
    expect(canGenerateNextSwissRound(null, completedR1)).toBe(false);
    expect(canGenerateNextSwissRound(undefined, completedR1)).toBe(false);
  });
});

describe('isSwissCompetitionComplete', () => {
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
  const incompleteR4 = [
    { id: 'Swiss-R4-0', status: 'completed' },
    { id: 'Swiss-R4-1', status: 'running' },
  ];

  it('true when current >= total AND final-round matches all complete', () => {
    expect(isSwissCompetitionComplete(mkComp(), completedR4)).toBe(true);
  });

  it('false when on the final round but not all matches completed', () => {
    expect(isSwissCompetitionComplete(mkComp(), incompleteR4)).toBe(false);
  });

  it('false when current round < total rounds', () => {
    expect(isSwissCompetitionComplete(mkComp({ swissCurrentRound: 3 }), completedR4)).toBe(false);
  });

  it('false when format !== swiss', () => {
    expect(isSwissCompetitionComplete(mkComp({ format: 'mixed' }), completedR4)).toBe(false);
  });
});

// mp-zoh Phase 4: inline schedule estimate formatting helper.
// formatCompMinutes converts a total-minutes integer to a human-readable
// string like "2h 03m". It is extracted as a pure export so it can be
// unit-tested without mounting the AdminSettings component.
describe('formatCompMinutes (mp-zoh)', () => {
  it('returns null for 0 (no estimate)', () => {
    expect(formatCompMinutes(0)).toBeNull();
  });

  it('returns null for negative values', () => {
    expect(formatCompMinutes(-1)).toBeNull();
  });

  it('returns null for non-finite values (NaN, Infinity)', () => {
    expect(formatCompMinutes(NaN)).toBeNull();
    expect(formatCompMinutes(Infinity)).toBeNull();
  });

  it('formats minutes-only (< 60 min)', () => {
    expect(formatCompMinutes(30)).toBe('30m');
    expect(formatCompMinutes(1)).toBe('1m');
    expect(formatCompMinutes(59)).toBe('59m');
  });

  it('formats exactly 1 hour as "1h 00m"', () => {
    expect(formatCompMinutes(60)).toBe('1h 00m');
  });

  it('pads single-digit minutes with leading zero', () => {
    expect(formatCompMinutes(63)).toBe('1h 03m');
    expect(formatCompMinutes(125)).toBe('2h 05m');
  });

  it('formats typical tournament durations', () => {
    expect(formatCompMinutes(120)).toBe('2h 00m');
    expect(formatCompMinutes(183)).toBe('3h 03m');
  });
});
