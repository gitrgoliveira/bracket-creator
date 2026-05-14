import { describe, it, expect } from 'vitest';
import { buildLiveIpponResult, loadScoreboardPoints } from '../admin_competition.jsx';

// Copilot finding on PR #103: LiveMatchPanel's scoreboard mode supports
// 2-ippon wins (and 2-1 with loser points), but the old recordWinner
// only ever recorded a 1-ippon result (winnerPts=1, single-letter
// array). The fix lifts the result-building logic into the pure
// buildLiveIpponResult helper and consumes the full points arrays.
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

describe('buildLiveIpponResult', () => {
  describe('1-ippon win (tap / card mode contract)', () => {
    it('side A win, no letters passed → defaults to ["M"]', () => {
      const r = buildLiveIpponResult("a", SIDE_A, SIDE_B);
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
      const r = buildLiveIpponResult("b", SIDE_A, SIDE_B);
      expect(r.winner).toBe(SIDE_B);
      expect(r.ipponsA).toEqual([]);
      expect(r.ipponsB).toEqual(["M"]);
      expect(r.score.winnerPts).toBe(1);
      expect(r.score.ippons).toEqual(["M"]);
    });

    it('side A win with explicit single letter ["K"]', () => {
      const r = buildLiveIpponResult("a", SIDE_A, SIDE_B, ["K"]);
      expect(r.ipponsA).toEqual(["K"]);
      expect(r.ipponsB).toEqual([]);
      expect(r.score.winnerPts).toBe(1);
      expect(r.score.ippons).toEqual(["K"]);
    });

    it('empty array → falls back to ["M"] (the legacy default)', () => {
      // [] is empty/falsy-by-length so the helper substitutes ["M"].
      // Keeps the tap-mode "no letter at all" path working when a
      // caller accidentally hands in [] instead of undefined.
      const r = buildLiveIpponResult("a", SIDE_A, SIDE_B, []);
      expect(r.ipponsA).toEqual(["M"]);
      expect(r.score.winnerPts).toBe(1);
    });

    it('null winnerIppons → falls back to ["M"]', () => {
      const r = buildLiveIpponResult("a", SIDE_A, SIDE_B, null);
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
      const r = buildLiveIpponResult("a", SIDE_A, SIDE_B, ["M"], []);
      expect(r.winner).toBe(SIDE_A);
      expect(r.ipponsA).toEqual(["M"]);
      expect(r.ipponsB).toEqual([]);
      expect(r.score.winnerPts).toBe(1);
      expect(r.score.loserPts).toBe(0);
    });

    it('side B scoreboard 1-0 (time-up): symmetric to side A', () => {
      const r = buildLiveIpponResult("b", SIDE_A, SIDE_B, ["D"], []);
      expect(r.winner).toBe(SIDE_B);
      expect(r.ipponsA).toEqual([]);
      expect(r.ipponsB).toEqual(["D"]);
      expect(r.score.winnerPts).toBe(1);
      expect(r.score.loserPts).toBe(0);
    });
  });

  describe('2-ippon win (the Copilot finding)', () => {
    it('side A 2-0 win: ipponsA has both letters, ipponsB is empty', () => {
      const r = buildLiveIpponResult("a", SIDE_A, SIDE_B, ["M", "K"], []);
      expect(r.winner).toBe(SIDE_A);
      expect(r.ipponsA).toEqual(["M", "K"]);
      expect(r.ipponsB).toEqual([]);
      expect(r.score.winnerPts).toBe(2);
      expect(r.score.loserPts).toBe(0);
      expect(r.score.ippons).toEqual(["M", "K"]);
    });

    it('side B 2-0 win: ipponsB has both letters', () => {
      const r = buildLiveIpponResult("b", SIDE_A, SIDE_B, ["D", "T"], []);
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
      const r = buildLiveIpponResult("a", SIDE_A, SIDE_B, ["M", "K"], ["D"]);
      expect(r.ipponsA).toEqual(["M", "K"]);
      expect(r.ipponsB).toEqual(["D"]);
      expect(r.score.winnerPts).toBe(2);
      expect(r.score.loserPts).toBe(1);
    });

    it('side B 2-1 win: symmetric — loser letters land in ipponsA', () => {
      const r = buildLiveIpponResult("b", SIDE_A, SIDE_B, ["K", "T"], ["M"]);
      expect(r.ipponsA).toEqual(["M"]);
      expect(r.ipponsB).toEqual(["K", "T"]);
      expect(r.score.winnerPts).toBe(2);
      expect(r.score.loserPts).toBe(1);
    });
  });

  describe('schema invariants', () => {
    it('always sets status="completed" (live panel never schedules)', () => {
      expect(buildLiveIpponResult("a", SIDE_A, SIDE_B).status).toBe("completed");
      expect(buildLiveIpponResult("b", SIDE_A, SIDE_B, ["M", "K"]).status).toBe("completed");
    });

    it('always sets type="ippon" (hikiwake/hantei not supported here)', () => {
      // Draws go through admin_scoring_modal.jsx's full editor with the
      // hikiwake toggle. Hantei wins go through the card-mode hantei
      // button → onRecord("a"|"b", "hantei") path which doesn't hit
      // recordWinner's ippon builder at all.
      expect(buildLiveIpponResult("a", SIDE_A, SIDE_B).score.type).toBe("ippon");
    });

    it('fouls always zero (live panel doesn\'t expose hansoku)', () => {
      const r = buildLiveIpponResult("a", SIDE_A, SIDE_B, ["M", "K"]);
      expect(r.score.fouls).toEqual({ a: 0, b: 0 });
    });

    it('winner field is the correct side object', () => {
      expect(buildLiveIpponResult("a", SIDE_A, SIDE_B).winner).toBe(SIDE_A);
      expect(buildLiveIpponResult("b", SIDE_A, SIDE_B).winner).toBe(SIDE_B);
    });
  });
});

describe('loadScoreboardPoints', () => {
  // Companion bug to the 2-ippon truncation Copilot found. The previous
  // LiveMatchPanel useEffect loaded aPoints/bPoints from
  // `match.score.ippons` (winner-only) gated by `winner.id === sideX.id`,
  // which silently dropped the LOSER's letters on every render. Once
  // buildLiveIpponResult started writing 2-1 wins correctly (loser's
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

  describe('ippon-mode reads (the read side of buildLiveIpponResult writes)', () => {
    it('1-0 side A win round-trip: aPoints=["M"], bPoints=[]', () => {
      // buildLiveIpponResult("a", A, B, ["M"], []) wrote ipponsA=["M"], ipponsB=[].
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
      // ippon arrays. Live panel never writes "•" but data may round-trip
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
// propagate — the user sees stale data AND the next save of any other
// field clobbers the server value (saveNow spreads { ...c, ...next }).
//
// This structural test reads the source file and verifies the deps array
// contains every c.* field that the JSX accesses through `local.*`.
// It replaces a rendering test (which would need @testing-library/react,
// not available in the current vitest stub-React setup).
//
// If you add a new `local.foo` reference in AdminSettings' JSX, add
// `c.foo` to the useEffect deps AND add "foo" to EXPECTED_DEPS below.
import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));

describe('AdminSettings useEffect deps completeness (H3 regression)', () => {
  // Fields rendered via `local.*` in AdminSettings' JSX or consumed by
  // saveNow's `{ ...c, ...next }` spread. Each must appear as `c.<field>`
  // in the useEffect deps array so SSE-driven changes propagate.
  const EXPECTED_DEPS = [
    'id', 'name', 'date', 'startTime',
    'poolSize', 'poolWinners', 'poolSizeMode',
    'courts', 'roundRobin', 'withZekkenName',
    'teamSize', 'numberPrefix',
    'format', 'kind',
  ];

  it('useEffect deps include every field rendered via local.*', () => {
    const src = readFileSync(
      resolve(__dirname, '..', 'admin_competition.jsx'),
      'utf8'
    );

    // Find the sync-to-local useEffect. It's the one whose body contains
    // `{ ...prev, ...c }` — the distinctive merge shape. Extract the deps
    // array from the closing `}, [c.id, c.name, ...])`  line.
    const depsMatch = src.match(
      /\{ \.\.\.prev, \.\.\.c \}[\s\S]*?\}, \[([^\]]+)\]\)/
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
