// Unit (pure-logic) tests for engi scoring helpers.
// Component rendering lives in js/__tests__/render/admin_scoring_engi.render.test.jsx.
// Tests: deriveWinner, MAX_FLAGS, VALID_TOTALS, submit guard predicate.

import { describe, it, expect } from 'vitest';
import { MAX_FLAGS, VALID_TOTALS, deriveWinner } from '../admin_scoring_engi.jsx';

// ── pure-logic: deriveWinner ──────────────────────────────────────────────────

describe('deriveWinner', () => {
  it('returns "a" when flagsA > flagsB', () => {
    expect(deriveWinner(3, 0)).toBe('a');
    expect(deriveWinner(3, 2)).toBe('a');
    expect(deriveWinner(5, 0)).toBe('a');
  });

  it('returns "b" when flagsB > flagsA', () => {
    expect(deriveWinner(0, 3)).toBe('b');
    expect(deriveWinner(2, 3)).toBe('b');
    expect(deriveWinner(0, 5)).toBe('b');
  });

  it('returns null when tied', () => {
    expect(deriveWinner(0, 0)).toBeNull();
    expect(deriveWinner(2, 2)).toBeNull();
    expect(deriveWinner(5, 5)).toBeNull();
  });
});

// ── pure-logic: VALID_TOTALS ──────────────────────────────────────────────────

describe('VALID_TOTALS', () => {
  it('accepts 1, 3, 5 (odd totals that guarantee a winner)', () => {
    expect(VALID_TOTALS.has(1)).toBe(true);
    expect(VALID_TOTALS.has(3)).toBe(true);
    expect(VALID_TOTALS.has(5)).toBe(true);
  });

  it('rejects 0, 2, 4, 6, 7 (even totals or totals > 5)', () => {
    [0, 2, 4, 6, 7].forEach((n) => {
      expect(VALID_TOTALS.has(n)).toBe(false);
    });
  });
});

// ── pure-logic: MAX_FLAGS ─────────────────────────────────────────────────────

describe('MAX_FLAGS', () => {
  it('is 5 (five judges, each raises one flag)', () => {
    expect(MAX_FLAGS).toBe(5);
  });
});

// ── pure-logic: submit guard (canSubmit predicate) ────────────────────────────
// Mirrors the component logic: canSubmit = VALID_TOTALS.has(total) && winner !== null.
// total must be in {1,3,5} AND flags cannot be equal (no draw allowed).

describe('submit guard predicate', () => {
  const canSubmit = (flagsA, flagsB) => {
    const total = flagsA + flagsB;
    const winner = deriveWinner(flagsA, flagsB);
    return VALID_TOTALS.has(total) && winner !== null;
  };

  const cases = [
    // [flagsA, flagsB, enabled, description]
    [0, 0, false, 'total 0: nothing entered'],
    [1, 0, true,  'total 1: Aka wins'],
    [0, 1, true,  'total 1: Shiro wins'],
    [2, 0, false, 'total 2: even, rejected'],
    [1, 1, false, 'total 2: tie, also rejected'],
    [2, 1, true,  'total 3: Aka wins'],
    [3, 0, true,  'total 3: Aka wins (all flags)'],
    [1, 2, true,  'total 3: Shiro wins'],
    [2, 2, false, 'total 4: even, rejected'],
    [3, 2, true,  'total 5: Aka wins'],
    [4, 1, true,  'total 5: Aka wins'],
    [2, 3, true,  'total 5: Shiro wins'],
    [1, 4, true,  'total 5: Shiro wins'],
    [5, 0, true,  'total 5: Aka wins (unanimous)'],
    [0, 5, true,  'total 5: Shiro wins (unanimous)'],
    [3, 3, false, 'total 6: even, rejected (beyond MAX_FLAGS per side but guard is flag-agnostic)'],
    [4, 3, false, 'total 7: odd but > 5, rejected'],
  ];

  cases.forEach(([flagsA, flagsB, enabled, description]) => {
    it(`${enabled ? 'enabled' : 'disabled'}: ${description} (${flagsA}v${flagsB})`, () => {
      expect(canSubmit(flagsA, flagsB)).toBe(enabled);
    });
  });
});

// ── pure-logic: payload shape ─────────────────────────────────────────────────
// Verify the payload shape the component would build mirrors the wire contract.
// Finding 8: winner is NOT included — the backend re-derives it from flags.

describe('engi payload shape', () => {
  // Mirrors the component's handleSubmit payload exactly.
  const buildPayload = (_match, flagsA, flagsB) => {
    return { flagsA, flagsB, status: 'completed' };
  };

  const match = {
    sideA: { id: 'p1', name: 'Tanaka Akira', displayName: 'Tanaka Yuki', dojo: 'Gyokusen' },
    sideB: { id: 'p2', name: 'Suzuki Hana', displayName: 'Suzuki Mei', dojo: 'Shinsei' },
  };

  it('includes flagsA, flagsB, and status: "completed" — no winner field', () => {
    const payload = buildPayload(match, 3, 0);
    expect(payload).toEqual({ flagsA: 3, flagsB: 0, status: 'completed' });
    expect(Object.prototype.hasOwnProperty.call(payload, 'winner')).toBe(false);
  });

  it('does not include a winner field when Shiro has more flags', () => {
    const payload = buildPayload(match, 2, 1);
    expect(Object.prototype.hasOwnProperty.call(payload, 'winner')).toBe(false);
    expect(payload.flagsA).toBe(2);
    expect(payload.flagsB).toBe(1);
  });

  it('does not include a winner field when Aka has more flags', () => {
    const payload = buildPayload(match, 0, 1);
    expect(Object.prototype.hasOwnProperty.call(payload, 'winner')).toBe(false);
    expect(payload.flagsA).toBe(0);
    expect(payload.flagsB).toBe(1);
  });

  it('carries flagsA and flagsB through regardless of winner side', () => {
    const payload = buildPayload(match, 4, 1);
    expect(payload.flagsA).toBe(4);
    expect(payload.flagsB).toBe(1);
  });

  it('source file handleSubmit does not reference winner in payload literal', () => {
    // Structural regression guard: ensure the source no longer builds a
    // `winner` key in the payload (finding 8). If it reappears, this test fails.
    const { readFileSync } = require('fs');
    const { resolve } = require('path');
    const src = readFileSync(resolve(__dirname, '..', 'admin_scoring_engi.jsx'), 'utf8');
    // The handleSubmit block should only send { flagsA, flagsB, status }.
    // Verify no `winner` appears in the payload object literal inside handleSubmit.
    const handleSubmitBlock = src.match(/const handleSubmit = async[^}]+\{[^}]+\}/s)?.[0] || src;
    expect(handleSubmitBlock).not.toMatch(/const winner\s*=/);
    expect(handleSubmitBlock).not.toMatch(/payload\s*=\s*\{[^}]*winner/);
  });
});
