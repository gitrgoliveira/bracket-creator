// kachinukiVisiblePositions: which bout slots the team scoring modal
// renders for a kachinuki match. Regression guard for the UAT bug where
// a just-started match rendered ZERO rows: the server never creates
// bout 1 (MaybeAdvanceKachinuki only appends bouts 2+), so filtering
// positions to server subResults left a fresh match unscoreable.

import { describe, it, expect } from 'vitest';
import { kachinukiVisiblePositions } from '../admin_scoring_team.jsx';

// 3-person team, no daihyosen: positions "1".."3".
const POSITIONS = ['1', '2', '3'];

// isPlayedAt from a per-canonical-index boolean array.
const playedAt = (flags) => (idx) => !!flags[idx];

describe('kachinukiVisiblePositions', () => {
  it('bootstraps bout 1 on a fresh match (empty server log)', () => {
    expect(kachinukiVisiblePositions({
      positions: POSITIONS, daihyosenIdx: -1, subResults: [],
      isComplete: false, isPlayedAt: playedAt([]),
    })).toEqual(['1']);
  });

  it('bootstraps bout 1 when subResults is missing entirely', () => {
    expect(kachinukiVisiblePositions({
      positions: POSITIONS, daihyosenIdx: -1, subResults: undefined,
      isComplete: false, isPlayedAt: playedAt([]),
    })).toEqual(['1']);
  });

  it('shows the last bout when every server bout is already scored', () => {
    const subResults = [{ position: 1 }, { position: 2 }, { position: 3 }];
    expect(kachinukiVisiblePositions({
      positions: POSITIONS, daihyosenIdx: -1, subResults,
      isComplete: false, isPlayedAt: playedAt([true, true, true]),
    })).toEqual(['3']);
  });

  it('shows the unplayed server placeholder (bout 3 appended, not yet scored)', () => {
    const subResults = [{ position: 1 }, { position: 2 }, { position: 3 }];
    expect(kachinukiVisiblePositions({
      positions: POSITIONS, daihyosenIdx: -1, subResults,
      isComplete: false, isPlayedAt: playedAt([true, true, false]),
    })).toEqual(['3']);
  });

  it('shows the first unplayed bout, not a later one', () => {
    const subResults = [{ position: 1 }, { position: 2 }];
    expect(kachinukiVisiblePositions({
      positions: POSITIONS, daihyosenIdx: -1, subResults,
      isComplete: false, isPlayedAt: playedAt([true, false, false]),
    })).toEqual(['2']);
  });

  it('completed (correction) shows every server bout, never phantom slots', () => {
    const subResults = [{ position: 1 }, { position: 2 }];
    expect(kachinukiVisiblePositions({
      positions: POSITIONS, daihyosenIdx: -1, subResults,
      isComplete: true, isPlayedAt: playedAt([true, true, false]),
    })).toEqual(['1', '2']);
  });

  it('always includes the daihyosen slot while running', () => {
    const positions = ['1', '2', '3', 'daihyosen'];
    const subResults = [{ position: 1 }, { position: 2 }, { position: 3 }, { position: -1 }];
    expect(kachinukiVisiblePositions({
      positions, daihyosenIdx: 3, subResults,
      isComplete: false, isPlayedAt: playedAt([true, true, true, false]),
    })).toEqual(['3', 'daihyosen']);
  });

  it('includes the daihyosen slot in correction mode too', () => {
    const positions = ['1', '2', 'daihyosen'];
    const subResults = [{ position: 1 }, { position: 2 }, { position: -1 }];
    expect(kachinukiVisiblePositions({
      positions, daihyosenIdx: 2, subResults,
      isComplete: true, isPlayedAt: playedAt([true, true, true]),
    })).toEqual(['1', '2', 'daihyosen']);
  });
});

// UAT repro (Bug A): bout 1 recorded as a 0-0 hikiwake carries decision
// "hikiwake" and no points. The modal seeds its local sub with
// draw: existing.decision === "hikiwake", so subBoutHasBeenPlayed marks
// it PLAYED and the current bout is the appended placeholder, NOT bout 1.
// (Seeding draw:false made bout 1 look unplayed; one M click + autosave
// then rewrote the recorded draw and dropped the placeholder.)
import { subBoutHasBeenPlayed } from '../admin_scoring_team.jsx';

describe('recorded hikiwake bout counts as played (UAT repro)', () => {
  it('a draw-seeded sub is played', () => {
    expect(subBoutHasBeenPlayed({ aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: true })).toBe(true);
  });

  it('current bout is the appended placeholder, not the recorded hikiwake', () => {
    // Local subs as seeded from the server log: bout 1 draw:true (decision
    // "hikiwake"), bout 2 untouched placeholder, bout 3 never created.
    const subs = [
      { aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: true },
      { aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: false },
      { aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: false },
    ];
    const subResults = [
      { position: 1, decision: 'hikiwake' },
      { position: 2 },
    ];
    expect(kachinukiVisiblePositions({
      positions: POSITIONS, daihyosenIdx: -1, subResults,
      isComplete: false, isPlayedAt: (idx) => subBoutHasBeenPlayed(subs[idx]),
    })).toEqual(['2']);
  });
});
