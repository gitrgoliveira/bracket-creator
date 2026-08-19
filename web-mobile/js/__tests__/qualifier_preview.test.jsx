import { describe, it, expect } from 'vitest';
import {
  nextPow2, fillBracketPoolCount, computeQualifierPreview, formatQualifierPreviewLine,
  EXTRA_QUALIFIERS_STANDARD, EXTRA_QUALIFIERS_LARGER_POOLS, EXTRA_QUALIFIERS_FILL_BRACKET,
  extraQualifiersRadioVisible, resetExtraQualifiersOnPoolModeChange,
  winnersForExtraQualifiersChange, winnersInputDisabled,
  extraQualifiersLabel, extraQualifiersHint,
} from '../qualifier_preview.jsx';

// bc-qual LP-5a: this module mirrors three Go pure functions
// (helper.PoolCount's floor(n/minSize) branch, state.Competition.
// QualifiersForPool's oversized test, helper.FillBracketPoolCount) so the
// admin_setup.jsx preview line never diverges from what the server will
// actually build. The known-table cases below are pinned against the SAME
// numbers bd bc-qual verified against 19WKC 2024 sheet data (fill-bracket)
// and hand-worked arithmetic (standard/larger-pools).

describe('EXTRA_QUALIFIERS_* constants', () => {
  it('mirror state.ExtraQualifiersNone/LargerPools/FillBracket byte-for-byte', () => {
    expect(EXTRA_QUALIFIERS_STANDARD).toBe('');
    expect(EXTRA_QUALIFIERS_LARGER_POOLS).toBe('larger-pools');
    expect(EXTRA_QUALIFIERS_FILL_BRACKET).toBe('fill-bracket');
  });
});

describe('nextPow2', () => {
  it('returns 1 for n <= 1 (including 0 and negative)', () => {
    expect(nextPow2(0)).toBe(1);
    expect(nextPow2(1)).toBe(1);
    expect(nextPow2(-5)).toBe(1);
  });
  it('returns the smallest power of two >= n', () => {
    expect(nextPow2(2)).toBe(2);
    expect(nextPow2(3)).toBe(4);
    expect(nextPow2(4)).toBe(4);
    expect(nextPow2(5)).toBe(8);
    expect(nextPow2(33)).toBe(64);
  });
  it('guards non-finite input', () => {
    expect(nextPow2(NaN)).toBe(1);
    expect(nextPow2(undefined)).toBe(1);
  });
});

describe('fillBracketPoolCount (mirrors helper.FillBracketPoolCount)', () => {
  it('19WKC Men\'s Team: 60 entrants, min 3 -> 16 pools, 0 drafts', () => {
    expect(fillBracketPoolCount(60, 3)).toEqual({ pools: 16, drafts: 0 });
  });
  it('19WKC Women\'s Team: 45 entrants, min 3 -> 14 pools, 2 drafts', () => {
    expect(fillBracketPoolCount(45, 3)).toEqual({ pools: 14, drafts: 2 });
  });
  it('19WKC Women\'s Individual: 203 entrants, min 3 -> 64 pools, 0 drafts', () => {
    expect(fillBracketPoolCount(203, 3)).toEqual({ pools: 64, drafts: 0 });
  });
  it('19WKC Men\'s Individual: 242 entrants, min 3 -> 64 pools, 0 drafts', () => {
    expect(fillBracketPoolCount(242, 3)).toEqual({ pools: 64, drafts: 0 });
  });
  it('hand-worked: 11 entrants, min 3 -> 3 pools, 1 draft', () => {
    expect(fillBracketPoolCount(11, 3)).toEqual({ pools: 3, drafts: 1 });
  });
  it('n exactly at minSize forms one pool, 0 drafts', () => {
    expect(fillBracketPoolCount(3, 3)).toEqual({ pools: 1, drafts: 0 });
  });
  it('returns null for invalid minSize', () => {
    expect(fillBracketPoolCount(60, 0)).toBeNull();
    expect(fillBracketPoolCount(60, -1)).toBeNull();
  });
  it('returns null when n is below minSize', () => {
    expect(fillBracketPoolCount(2, 3)).toBeNull();
  });
  it('returns null when no pool count fits (9 entrants at min 3)', () => {
    // Only P=3 is in range; needs 1 draft but has 0 oversized pools (9 = 3*3 exactly).
    expect(fillBracketPoolCount(9, 3)).toBeNull();
  });
});

describe('computeQualifierPreview: known table (bc-qual LP-5a)', () => {
  it('n=11, minSize=3, poolWinners=1: standard 3 pools/3 quals, larger-pools 5 quals, fill-bracket 3 pools + 1 draft = 4 quals, zero byes', () => {
    const r = computeQualifierPreview(11, 3, 1);
    expect(r.standard).toMatchObject({ pools: 3, qualifiers: 3, bracketSize: 4, byes: 1 });
    expect(r.largerPools).toMatchObject({ pools: 3, oversized: 2, qualifiers: 5, bracketSize: 8, byes: 3 });
    expect(r.fillBracket).toMatchObject({ pools: 3, drafts: 1, qualifiers: 4, bracketSize: 4, byes: 0 });
  });

  it('n=5, minSize=3 (degenerate: one pool of 5): oversized counts POOLS, not remainder players', () => {
    // Go's QualifiersForPool grants +1 per oversized POOL however far over
    // the minimum it is; the remainder (2) exceeds the pool count (1) here,
    // so an unclamped remainder would predict 3 qualifiers where the draw
    // sends 2 (Opus review pin, bc-qual LP-5a).
    const r = computeQualifierPreview(5, 3, 1);
    expect(r.standard).toMatchObject({ pools: 1, qualifiers: 1 });
    expect(r.largerPools).toMatchObject({ pools: 1, oversized: 1, qualifiers: 2 });
  });

  it('n=104, minSize=3, poolWinners=1: standard 34/34, larger-pools 36, fill-bracket 32/32/0', () => {
    const r = computeQualifierPreview(104, 3, 1);
    expect(r.standard).toMatchObject({ pools: 34, qualifiers: 34, bracketSize: 64, byes: 30, rounds: 6 });
    expect(r.largerPools).toMatchObject({ pools: 34, oversized: 2, qualifiers: 36, bracketSize: 64, byes: 28, rounds: 6 });
    expect(r.fillBracket).toMatchObject({ pools: 32, drafts: 0, qualifiers: 32, bracketSize: 32, byes: 0, rounds: 5 });
  });

  it('n=45, minSize=3, poolWinners=1: fill-bracket P=14+2 drafts=16 qualifiers, zero byes', () => {
    const r = computeQualifierPreview(45, 3, 1);
    expect(r.fillBracket).toMatchObject({ pools: 14, drafts: 2, qualifiers: 16, bracketSize: 16, byes: 0 });
  });

  it('n=60, minSize=3, poolWinners=1: fill-bracket P=16+0 drafts=16 qualifiers, zero byes', () => {
    const r = computeQualifierPreview(60, 3, 1);
    expect(r.fillBracket).toMatchObject({ pools: 16, drafts: 0, qualifiers: 16, bracketSize: 16, byes: 0 });
  });

  it('acceptance-criteria table: 104 entrants min 3 -> rounds 6/6/5', () => {
    const r = computeQualifierPreview(104, 3, 1);
    expect(r.standard.rounds).toBe(6);
    expect(r.largerPools.rounds).toBe(6);
    expect(r.fillBracket.rounds).toBe(5);
  });

  it('guards n=0 gracefully (all-null, no crash)', () => {
    const r = computeQualifierPreview(0, 3, 1);
    expect(r).toEqual({ standard: null, largerPools: null, fillBracket: null });
  });

  it('guards an unknown/non-finite roster gracefully', () => {
    expect(computeQualifierPreview(undefined, 3, 1)).toEqual({ standard: null, largerPools: null, fillBracket: null });
    expect(computeQualifierPreview(null, 3, 1)).toEqual({ standard: null, largerPools: null, fillBracket: null });
    expect(computeQualifierPreview(NaN, 3, 1)).toEqual({ standard: null, largerPools: null, fillBracket: null });
  });

  it('guards n below minSize gracefully', () => {
    expect(computeQualifierPreview(2, 3, 1)).toEqual({ standard: null, largerPools: null, fillBracket: null });
  });

  it('guards an invalid minSize gracefully', () => {
    expect(computeQualifierPreview(60, 0, 1)).toEqual({ standard: null, largerPools: null, fillBracket: null });
  });

  it('treats an unset/<=0 poolWinners as 1, same as the caller coupling forces for non-standard modes', () => {
    const withOne = computeQualifierPreview(11, 3, 1);
    const withZero = computeQualifierPreview(11, 3, 0);
    const withUndefined = computeQualifierPreview(11, 3, undefined);
    expect(withZero.standard).toEqual(withOne.standard);
    expect(withUndefined.standard).toEqual(withOne.standard);
  });

  it('standard mode multiplies by the given poolWinners (e.g. 2, the default when unset elsewhere)', () => {
    const r = computeQualifierPreview(11, 3, 2);
    expect(r.standard).toMatchObject({ pools: 3, qualifiers: 6, bracketSize: 8, byes: 2 });
  });
});

describe('formatQualifierPreviewLine', () => {
  it('renders the pools -> qualifiers -> bracket (byes) sentence', () => {
    const shape = { pools: 34, qualifiers: 36, bracketSize: 64, byes: 28, rounds: 6 };
    expect(formatQualifierPreviewLine(shape)).toBe('34 pools -> 36 qualifiers -> 64-slot knockout (28 byes)');
  });

  it('singularizes pool/qualifier/bye at count 1', () => {
    const shape = { pools: 1, qualifiers: 1, bracketSize: 1, byes: 1, rounds: 0 };
    expect(formatQualifierPreviewLine(shape)).toBe('1 pool -> 1 qualifier -> 1-slot knockout (1 bye)');
  });

  it('renders "no byes" at byes=0', () => {
    const shape = { pools: 16, qualifiers: 16, bracketSize: 16, byes: 0, rounds: 4 };
    expect(formatQualifierPreviewLine(shape)).toBe('16 pools -> 16 qualifiers -> 16-slot knockout (no byes)');
  });

  it('returns null for a null shape (the n<=0/unknown-roster guard)', () => {
    expect(formatQualifierPreviewLine(null)).toBeNull();
  });
});

// bc-qual LP-5a: pure coupling rules between the "Pool size is a" (poolMode)
// radio, the "Knockout qualifiers" (extraQualifiers) radio, and "Winners
// per pool" (winners). Shared by BOTH the competition CREATE form
// (admin_setup.jsx) and the competition SETTINGS page
// (admin_competition_settings.jsx) -- see their own doc comments for the
// rationale (why the UI must never be able to construct a request
// state.ValidateExtraQualifiers would reject).
describe('extraQualifiersRadioVisible', () => {
  it('visible only for format=mixed AND poolMode=min', () => {
    expect(extraQualifiersRadioVisible('mixed', 'min')).toBe(true);
  });
  it('hidden for poolMode=max, even on format=mixed', () => {
    expect(extraQualifiersRadioVisible('mixed', 'max')).toBe(false);
  });
  it('hidden for non-mixed formats, even at poolMode=min', () => {
    expect(extraQualifiersRadioVisible('playoffs', 'min')).toBe(false);
    expect(extraQualifiersRadioVisible('league', 'min')).toBe(false);
    expect(extraQualifiersRadioVisible('swiss', 'min')).toBe(false);
  });
});

describe('resetExtraQualifiersOnPoolModeChange', () => {
  it('a change TO "max" resets a non-standard value to standard', () => {
    expect(resetExtraQualifiersOnPoolModeChange('max', EXTRA_QUALIFIERS_LARGER_POOLS)).toBe(EXTRA_QUALIFIERS_STANDARD);
    expect(resetExtraQualifiersOnPoolModeChange('max', EXTRA_QUALIFIERS_FILL_BRACKET)).toBe(EXTRA_QUALIFIERS_STANDARD);
  });
  it('a change TO "max" is a no-op when already standard', () => {
    expect(resetExtraQualifiersOnPoolModeChange('max', EXTRA_QUALIFIERS_STANDARD)).toBe(EXTRA_QUALIFIERS_STANDARD);
  });
  it('a change TO "min" leaves the current selection alone', () => {
    expect(resetExtraQualifiersOnPoolModeChange('min', EXTRA_QUALIFIERS_LARGER_POOLS)).toBe(EXTRA_QUALIFIERS_LARGER_POOLS);
    expect(resetExtraQualifiersOnPoolModeChange('min', EXTRA_QUALIFIERS_FILL_BRACKET)).toBe(EXTRA_QUALIFIERS_FILL_BRACKET);
    expect(resetExtraQualifiersOnPoolModeChange('min', EXTRA_QUALIFIERS_STANDARD)).toBe(EXTRA_QUALIFIERS_STANDARD);
  });
  // Regression (bc-qual LP-5a round 2): a never-normalized/legacy
  // competition record can carry extraQualifiers: undefined; a change TO
  // "min" must normalize that to the explicit standard sentinel, not
  // propagate undefined (which would then fail a strict === comparison
  // against EXTRA_QUALIFIERS_STANDARD elsewhere).
  it('a change TO "min" normalizes an undefined/null current value to standard', () => {
    expect(resetExtraQualifiersOnPoolModeChange('min', undefined)).toBe(EXTRA_QUALIFIERS_STANDARD);
    expect(resetExtraQualifiersOnPoolModeChange('min', null)).toBe(EXTRA_QUALIFIERS_STANDARD);
  });
});

describe('winnersForExtraQualifiersChange (pool-winners coupling, both directions)', () => {
  it('selecting larger-pools forces winners to 1, from any prior value', () => {
    expect(winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_LARGER_POOLS, 2)).toBe(1);
    expect(winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_LARGER_POOLS, 5)).toBe(1);
    expect(winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_LARGER_POOLS, 1)).toBe(1);
  });
  it('selecting fill-bracket forces winners to 1, from any prior value', () => {
    expect(winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_FILL_BRACKET, 2)).toBe(1);
    expect(winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_FILL_BRACKET, 7)).toBe(1);
  });
  it('switching back to standard leaves the current winners value untouched (no restore of a prior value)', () => {
    expect(winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_STANDARD, 1)).toBe(1);
    expect(winnersForExtraQualifiersChange(EXTRA_QUALIFIERS_STANDARD, 3)).toBe(3);
  });
});

describe('winnersInputDisabled', () => {
  it('disabled for both non-standard modes', () => {
    expect(winnersInputDisabled(EXTRA_QUALIFIERS_LARGER_POOLS)).toBe(true);
    expect(winnersInputDisabled(EXTRA_QUALIFIERS_FILL_BRACKET)).toBe(true);
  });
  it('enabled (not disabled) for standard', () => {
    expect(winnersInputDisabled(EXTRA_QUALIFIERS_STANDARD)).toBe(false);
  });
  // Regression (bc-qual LP-5a round 2): the settings page seeds `local`
  // straight off the competition prop, which for a never-normalized/legacy
  // record can carry extraQualifiers: undefined rather than "". A bare
  // `!== EXTRA_QUALIFIERS_STANDARD` read that as non-standard and wrongly
  // disabled/locked Winners per pool for every pre-existing competition.
  it('enabled (not disabled) for undefined/null, same as standard', () => {
    expect(winnersInputDisabled(undefined)).toBe(false);
    expect(winnersInputDisabled(null)).toBe(false);
  });
});


describe('extraQualifiersLabel / extraQualifiersHint (operator copy, 2026-08-19)', () => {
  it('labels match the operator wording', () => {
    expect(extraQualifiersLabel(EXTRA_QUALIFIERS_STANDARD)).toBe('Standard');
    expect(extraQualifiersLabel(EXTRA_QUALIFIERS_LARGER_POOLS)).toBe('Oversized send +1');
    expect(extraQualifiersLabel(EXTRA_QUALIFIERS_FILL_BRACKET)).toBe('Fit the knockout');
  });
  it('hints match the operator wording, with the oversized size derived from the minimum', () => {
    expect(extraQualifiersHint(EXTRA_QUALIFIERS_STANDARD, 3)).toBe('Every pool sends the same top-N; the bracket is padded with byes.');
    expect(extraQualifiersHint(EXTRA_QUALIFIERS_LARGER_POOLS, 3)).toBe('Same pools; 4-person pools send their top 2.');
    expect(extraQualifiersHint(EXTRA_QUALIFIERS_LARGER_POOLS, 4)).toBe('Same pools; 5-person pools send their top 2.');
    expect(extraQualifiersHint(EXTRA_QUALIFIERS_FILL_BRACKET, 3)).toBe('Fewer, fatter pools; the bracket fills exactly, no byes.');
    expect(extraQualifiersHint(undefined, 3)).toBe('Every pool sends the same top-N; the bracket is padded with byes.');
  });
});
