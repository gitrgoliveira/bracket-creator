import { describe, it, expect } from 'vitest';
import { buildLiveIpponResult } from '../admin_competition.jsx';

// Copilot finding on PR #103: LiveMatchPanel's scoreboard mode supports
// 2-ippon wins (and 2-1 with loser points), but the old recordWinner
// only ever recorded a 1-ippon result (winnerPts=1, single-letter
// array). The fix lifts the result-building logic into the pure
// buildLiveIpponResult helper and consumes the full points arrays.
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
