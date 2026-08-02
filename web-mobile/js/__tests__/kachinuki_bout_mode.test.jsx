// Operator-led kachinuki contract (mp-gmcg). While a kachinuki encounter is
// being fought, the modal shows TWO always-visible actions: [Record bout] (a
// running write flagged kachinukiBoutFinal) and [End match] (an explicit
// completed write whose outcome is DERIVED from the last scored bout — no
// picker). Completion is operator-led: the engine never auto-finalizes, so a
// running match carrying a "kachinuki-exhaustion" sub decision (e.g. after a
// reopen) stays in bout mode. The knockout no-draw rule (koTieBlocked) must
// not gate the bout submit: a bout hikiwake is a legitimate result that
// retires both players; End match carries its own knockout-tie gate via
// deriveKachinukiEndOutcome.
//
// The component itself is not mounted here (vitest does not exercise the big
// modals); the decisions are pure helpers tested directly, same pattern as
// resolveMatchLineup.

import { describe, it, expect } from 'vitest';
import {
  isKachinukiBoutMode,
  isKoTieBlocked,
  canReopenKachinukiMatch,
  deriveKachinukiEndOutcome,
  buildKachinukiEndEntries,
  subBoutHasBeenPlayed,
  kachinukiEnchoAvailable,
  kachinukiBandModel,
} from '../admin_scoring_team.jsx';
import { applyFoulIncrement } from '../admin_scoring_shared.jsx';

describe('isKachinukiBoutMode', () => {
  it('is true while a kachinuki match is being fought', () => {
    expect(isKachinukiBoutMode({ isKachinuki: true, isComplete: false, hasDaihyosen: false })).toBe(true);
  });

  it('is false for fixed-order team matches', () => {
    expect(isKachinukiBoutMode({ isKachinuki: false, isComplete: false, hasDaihyosen: false })).toBe(false);
  });

  it('is false for corrections (completed match keeps Finish semantics)', () => {
    expect(isKachinukiBoutMode({ isKachinuki: true, isComplete: true, hasDaihyosen: false })).toBe(false);
  });

  it('stays TRUE on a running match even when a sub carries kachinuki-exhaustion (operator-led: roster data is advisory)', () => {
    // The OLD auto-finalize contract exited bout mode on exhaustion. Now
    // completion is only the operator tapping End match, so a running match is
    // always live bout-by-bout scoring regardless of any advisory sub decision.
    expect(isKachinukiBoutMode({ isKachinuki: true, isComplete: false, hasDaihyosen: false })).toBe(true);
  });

  it('is false when a legacy daihyosen row exists (its completion goes through Finish)', () => {
    expect(isKachinukiBoutMode({ isKachinuki: true, isComplete: false, hasDaihyosen: true })).toBe(false);
  });
});

describe('koTieBlocked does not gate the bout submit', () => {
  it('a tied knockout kachinuki mid-match is bout mode, where koTieBlocked is not consulted', () => {
    // Tied IV/PW after a bout-1 hikiwake: koTieBlocked would read this as a
    // forbidden knockout draw, but the match is NOT being completed.
    const blocked = isKoTieBlocked({ isKnockoutPhase: true, teamWinner: null, isComplete: false });
    expect(blocked).toBe(true); // the completion rule itself is unchanged
    const boutMode = isKachinukiBoutMode({ isKachinuki: true, isComplete: false, hasDaihyosen: false });
    expect(boutMode).toBe(true); // and bout mode bypasses it by replacing the action
  });
});

describe('canReopenKachinukiMatch', () => {
  it('renders only on a COMPLETED kachinuki match', () => {
    expect(canReopenKachinukiMatch({ isKachinuki: true, isComplete: true })).toBe(true);
  });
  it('is false on a running kachinuki match (nothing to reopen)', () => {
    expect(canReopenKachinukiMatch({ isKachinuki: true, isComplete: false })).toBe(false);
  });
  it('is false for non-kachinuki (backend 400s the endpoint; button must not render)', () => {
    expect(canReopenKachinukiMatch({ isKachinuki: false, isComplete: true })).toBe(false);
  });
});

// The single played-bout primitive (operator input determines the bout
// outcome): buildKachinukiEndEntries feeds End derivation exactly the bouts
// subBoutHasBeenPlayed admits, so End, the wire filter, and the encho target
// can never disagree about which bout is last.
describe('buildKachinukiEndEntries', () => {
  const sub = (over) => ({ aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: false, encho: 0, ...over });

  it('drops untouched rows (auto-appended / manual placeholders never reach derivation)', () => {
    const entries = buildKachinukiEndEntries([sub({ aPts: ['M'] }), sub()], -1);
    expect(entries).toHaveLength(1);
    expect(entries[0].position).toBe(1);
  });

  it('drops the daihyosen row regardless of its content', () => {
    const entries = buildKachinukiEndEntries([sub({ aPts: ['M'] }), sub({ aPts: ['K'] })], 1);
    expect(entries).toHaveLength(1);
    expect(entries[0].position).toBe(1);
  });

  it('keeps a fouls-only bout: input is input (a bout fought to time with only a hansoku is a hikiwake)', () => {
    const subs = [sub({ aPts: ['M'] }), sub({ bFouls: 1 })];
    expect(subBoutHasBeenPlayed(subs[1])).toBe(true); // same primitive
    const entries = buildKachinukiEndEntries(subs, -1);
    expect(entries.map(e => e.position)).toEqual([1, 2]);
  });

  it('maps draw, fusensho, and encho onto the wire shape', () => {
    const entries = buildKachinukiEndEntries(
      [sub({ draw: true }), sub({ fusensho: 'a', aPts: ['○', '○'] }), sub({ encho: 2 })], -1);
    expect(entries[0].decision).toBe('hikiwake');
    expect(entries[1].decision).toBe('fusensho');
    expect(entries[2].encho).toEqual({ periodCount: 2 });
  });
});

describe('deriveKachinukiEndOutcome', () => {
  const bout = (position, over) => ({ position, ipponsA: [], ipponsB: [], ...over });
  const sub = (over) => ({ aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: false, encho: 0, ...over });

  it('blocks with no-bouts when nothing is recorded', () => {
    expect(deriveKachinukiEndOutcome({ subResults: [], isKnockoutPhase: false }))
      .toEqual({ kind: 'blocked', reason: 'no-bouts' });
    // Composition: a fresh match's untouched rows are filtered by the
    // builder before derivation ever sees them.
    expect(deriveKachinukiEndOutcome({
      subResults: buildKachinukiEndEntries([sub(), sub()], -1),
      isKnockoutPhase: true,
    })).toEqual({ kind: 'blocked', reason: 'no-bouts' });
  });

  it('a trailing untouched auto-appended bout does not mask the decisive bout (composition)', () => {
    const subs = [sub({ aPts: ['M'] }), sub()]; // bout 2 appended, never touched
    expect(deriveKachinukiEndOutcome({
      subResults: buildKachinukiEndEntries(subs, -1),
      isKnockoutPhase: true,
    })).toEqual({ kind: 'win', winnerSide: 'a' });
  });

  it('one outstanding foul on the LAST bout: fought, 0-0, tied — pool draw / knockout blocked (operator input determines the bout outcome)', () => {
    // With the auto-award, a live counter only ever holds ONE outstanding
    // foul (the 2nd discharges into an opponent H). A lone foul proves the
    // bout was fought but awards nothing: End reads it as 0-0 = hikiwake,
    // never skipping back to the previous bout.
    const subs = [sub({ aPts: ['M'] }), sub({ bFouls: 1 })];
    expect(deriveKachinukiEndOutcome({
      subResults: buildKachinukiEndEntries(subs, -1),
      isKnockoutPhase: false,
    })).toEqual({ kind: 'draw' });
    expect(deriveKachinukiEndOutcome({
      subResults: buildKachinukiEndEntries(subs, -1),
      isKnockoutPhase: true,
    })).toEqual({ kind: 'blocked', reason: 'knockout-tie' });
  });

  it('2 fouls become a point: the auto-awarded H is a winner for End derivation (composition with applyFoulIncrement)', () => {
    // Side B picks up two hansoku; the shared primitive discharges them
    // into an H point for side A and resets the counter. That point is
    // all End needs — fouls never influence the outcome any other way.
    let fouls = 0;
    let aPts = [];
    ({ fouls, opponentPts: aPts } = applyFoulIncrement(fouls, aPts));
    expect(fouls).toBe(1);
    ({ fouls, opponentPts: aPts } = applyFoulIncrement(fouls, aPts));
    expect(aPts).toEqual(['H']);
    expect(fouls).toBe(0);
    const subs = [sub({ aPts, bFouls: fouls })];
    expect(deriveKachinukiEndOutcome({
      subResults: buildKachinukiEndEntries(subs, -1),
      isKnockoutPhase: true,
    })).toEqual({ kind: 'win', winnerSide: 'a' });
  });

  it('wins for side A when the last scored bout has more A ippons', () => {
    const subs = [bout(1, { ipponsB: ['M'] }), bout(2, { ipponsA: ['M', 'D'] })];
    expect(deriveKachinukiEndOutcome({ subResults: subs, isKnockoutPhase: true }))
      .toEqual({ kind: 'win', winnerSide: 'a' });
  });

  it('wins for side B when the last scored bout has more B ippons', () => {
    const subs = [bout(1, { ipponsA: ['M'] }), bout(2, { ipponsB: ['M', 'K'] })];
    expect(deriveKachinukiEndOutcome({ subResults: subs, isKnockoutPhase: false }))
      .toEqual({ kind: 'win', winnerSide: 'b' });
  });

  it('maps a fusensho/winner-name win back to a side even with equal ippon counts', () => {
    const subs = [bout(1, { sideA: 'Red', sideB: 'White', winner: 'White', decision: 'fusensho' })];
    expect(deriveKachinukiEndOutcome({ subResults: subs, isKnockoutPhase: true }))
      .toEqual({ kind: 'win', winnerSide: 'b' });
  });

  it('draws on a tied last bout in pools/league', () => {
    const subs = [bout(1, { ipponsA: ['M'], ipponsB: ['K'] })];
    expect(deriveKachinukiEndOutcome({ subResults: subs, isKnockoutPhase: false }))
      .toEqual({ kind: 'draw' });
    // An explicit hikiwake decision is likewise a draw.
    const drawn = [bout(1, { decision: 'hikiwake' })];
    expect(deriveKachinukiEndOutcome({ subResults: drawn, isKnockoutPhase: false }))
      .toEqual({ kind: 'draw' });
  });

  it('blocks a tied last bout in a knockout (no draws): continue with next bout or encho', () => {
    const subs = [bout(1, { ipponsA: ['M'], ipponsB: ['K'] })];
    expect(deriveKachinukiEndOutcome({ subResults: subs, isKnockoutPhase: true }))
      .toEqual({ kind: 'blocked', reason: 'knockout-tie' });
  });

  it('treats a 0-0 bout sent to encho as live-tied (blocked), not unscored', () => {
    const subs = [bout(1, { encho: { periodCount: 1 } })];
    expect(deriveKachinukiEndOutcome({ subResults: subs, isKnockoutPhase: true }))
      .toEqual({ kind: 'blocked', reason: 'knockout-tie' });
  });

  it('ignores daihyosen (-1) and non-positive rows, keying off the last POSITIVE bout', () => {
    const subs = [bout(2, { ipponsA: ['M'] }), bout(-1, { ipponsB: ['K', 'D'] })];
    expect(deriveKachinukiEndOutcome({ subResults: subs, isKnockoutPhase: true }))
      .toEqual({ kind: 'win', winnerSide: 'a' });
  });
});

// kachinukiBandModel: the summary band's content model (light instrument
// panel; brief confirmed 2026-08-02). Bout-log facts only while running —
// never a verdict (the old IV/PW-derived "AKA WIN" contradicted the End
// gate mid-match); the verdict returns on completion from the MATCH winner.
describe('kachinukiBandModel', () => {
  const sub = (over) => ({ aPts: [], bPts: [], aFouls: 0, bFouls: 0, fusensho: '', draw: false, encho: 0, ...over });
  const names = [
    { aName: 'Alpha Senpo', bName: 'Bravo Senpo' },
    { aName: 'Alpha Senpo', bName: 'Bravo Chuken' },
    { aName: 'Alpha Chuken', bName: 'Bravo Taisho' },
  ];
  const base = {
    daihyosenIdx: -1, isComplete: false, matchWinner: '', matchDecision: '',
    sideAName: 'Team Alpha', sideBName: 'Team Bravo',
    namesAt: (idx) => names[idx] || {},
  };

  it('fresh match: bout number only, no fabricated fact, no verdict', () => {
    const kb = kachinukiBandModel({ ...base, subs: [sub()], currentBout: 1 });
    expect(kb).toEqual({ headline: 'BOUT 1', fact: '' });
  });

  it('after a win: last-bout fact with stays-on, never a verdict while running', () => {
    const kb = kachinukiBandModel({
      ...base, subs: [sub({ aPts: ['M', 'K'] }), sub()], currentBout: 2,
    });
    expect(kb.headline).toBe('BOUT 2');
    expect(kb.fact).toBe('Alpha Senpo beat Bravo Senpo · stays on');
    expect(kb.verdict).toBeUndefined();
  });

  it('a streak reads as a bout-log fact ("stays on, 2 wins")', () => {
    const kb = kachinukiBandModel({
      ...base, subs: [sub({ aPts: ['M'] }), sub({ aPts: ['D'] }), sub()], currentBout: 3,
    });
    expect(kb.fact).toBe('Alpha Senpo beat Bravo Chuken · stays on, 2 wins');
  });

  it('a tied last bout: hikiwake fact, both retired', () => {
    const kb = kachinukiBandModel({
      ...base, subs: [sub({ aPts: ['M'] }), sub({ draw: true }), sub()], currentBout: 3,
    });
    expect(kb.headline).toBe('BOUT 3');
    expect(kb.fact).toBe('Last: hikiwake · both retired');
  });

  it('completed: verdict from the MATCH winner (last-bout rule), not IV lead', () => {
    const kb = kachinukiBandModel({
      ...base, subs: [sub({ aPts: ['M'] })],
      isComplete: true, matchWinner: 'Team Bravo', matchDecision: 'kachinuki-exhaustion',
    });
    expect(kb.headline).toBe('FINAL · 1 BOUT');
    expect(kb.verdict).toBe('SHIRO WIN');
    expect(kb.verdictSide).toBe('shiro');
  });

  it('completed draw: hikiwake verdict', () => {
    const kb = kachinukiBandModel({
      ...base, subs: [sub({ draw: true }), sub({ draw: true })],
      isComplete: true, matchWinner: '', matchDecision: 'hikiwake',
    });
    expect(kb.verdict).toBe('DRAW');
    expect(kb.verdictSide).toBe('draw');
  });

  it('nameless bootstrap bout degrades to side labels, not blank facts', () => {
    const kb = kachinukiBandModel({
      ...base, subs: [sub({ bPts: ['M'] })], namesAt: () => ({}), currentBout: 2,
    });
    expect(kb.fact).toBe('Shiro beat Aka · stays on');
  });
});

// Whether a tied pairing must be fought to a result (e.g. the taisho must be
// defeated) is OPERATOR DISCRETION, never derived from the phase — so the
// Encho affordance renders for every tied last bout, pools included.
describe('kachinukiEnchoAvailable', () => {
  it('available on a tied last bout in pools/league (End would record a draw; fighting on is the operator\'s call)', () => {
    expect(kachinukiEnchoAvailable({ kind: 'draw' })).toBe(true);
  });
  it('available on a tied last bout in a knockout (End is blocked)', () => {
    expect(kachinukiEnchoAvailable({ kind: 'blocked', reason: 'knockout-tie' })).toBe(true);
  });
  it('not available when nothing is recorded', () => {
    expect(kachinukiEnchoAvailable({ kind: 'blocked', reason: 'no-bouts' })).toBe(false);
  });
  it('not available when the last bout already has a winner', () => {
    expect(kachinukiEnchoAvailable({ kind: 'win', winnerSide: 'a' })).toBe(false);
  });
  it('not available outside bout mode (null outcome)', () => {
    expect(kachinukiEnchoAvailable(null)).toBe(false);
  });
});
