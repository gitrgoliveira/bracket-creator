// Kachinuki submission contract (UAT gap): while a kachinuki encounter is
// being fought, the modal's primary action is "Record bout" (a running
// write flagged kachinukiBoutFinal), NEVER a match completion, and the
// knockout no-draw rule (koTieBlocked) must not block it: a bout hikiwake
// is a legitimate result that retires both players. Finish/complete
// semantics only return for corrections and for the tied-after-exhaustion
// daihyosen resolution.
//
// The component itself is not mounted here (vitest does not exercise the
// big modals); the mode decision is a pure helper tested directly, same
// pattern as resolveMatchLineup.

import { describe, it, expect } from 'vitest';
import { isKachinukiBoutMode, isKoTieBlocked } from '../admin_scoring_team.jsx';

describe('isKachinukiBoutMode', () => {
  it('is true while a kachinuki match is being fought', () => {
    expect(isKachinukiBoutMode({ isKachinuki: true, isComplete: false, exhausted: false, hasDaihyosen: false })).toBe(true);
  });

  it('is false for fixed-order team matches', () => {
    expect(isKachinukiBoutMode({ isKachinuki: false, isComplete: false, exhausted: false, hasDaihyosen: false })).toBe(false);
  });

  it('is false for corrections (completed match keeps Finish semantics)', () => {
    expect(isKachinukiBoutMode({ isKachinuki: true, isComplete: true, exhausted: false, hasDaihyosen: false })).toBe(false);
  });

  it('is false once one team is exhausted (tie resolution keeps Finish semantics)', () => {
    expect(isKachinukiBoutMode({ isKachinuki: true, isComplete: false, exhausted: true, hasDaihyosen: false })).toBe(false);
  });

  it('is false when a daihyosen row exists (its completion goes through Finish)', () => {
    expect(isKachinukiBoutMode({ isKachinuki: true, isComplete: false, exhausted: false, hasDaihyosen: true })).toBe(false);
  });
});

describe('koTieBlocked does not gate the bout submit', () => {
  it('a tied knockout kachinuki mid-match is bout mode, where koTieBlocked is not consulted', () => {
    // Tied IV/PW after a bout-1 hikiwake: koTieBlocked would read this as
    // a forbidden knockout draw, but the match is NOT being completed.
    const blocked = isKoTieBlocked({ isKnockoutPhase: true, teamWinner: null, isComplete: false });
    expect(blocked).toBe(true); // the completion rule itself is unchanged
    const boutMode = isKachinukiBoutMode({ isKachinuki: true, isComplete: false, exhausted: false, hasDaihyosen: false });
    expect(boutMode).toBe(true); // and bout mode bypasses it by replacing the action
  });
});
