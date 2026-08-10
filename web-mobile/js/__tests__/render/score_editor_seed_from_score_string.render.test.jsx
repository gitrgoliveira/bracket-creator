// Regression: ScoreEditorModal must seed its ippon slots from the scoreA /
// scoreB STRINGS when the ippon arrays are absent.
//
// THE BUG (found in browser UAT, HIGH, silent data loss). The editor seeded
// only from m.ipponsA / m.ipponsB:
//
//   const seedAPts = m.ipponsA?.filter(...) || (m.score?.type === "ippon" && ...);
//
// state.MatchResult (pool / league) carries ipponsA / ipponsB on the wire, but
// state.BracketMatch (knockout) carries ONLY ScoreA / ScoreB strings
// (internal/state/models.go — no ippon slices on the bracket type). So a
// KNOCKOUT match re-read from the server arrived with no ippon arrays at all:
// the editor opened EMPTY on a match that already had points, and the
// operator's next tap saved over them — a recorded ippon lost mid-match.
//
// The fix parses the string as a fallback, which is what every DISPLAY surface
// already did (admin_shiaijo.jsx, match_scoreboard.jsx, bracket.jsx); the
// editor was the one surface that did not.
//
// Two things this file deliberately pins beyond the headline case:
//   * the score.type === "ippon" branch must stay REACHABLE as the last
//     resort. It is guarded by `cleanA.length ? ... : ...` and NOT by `||`,
//     because an empty array is truthy and `||` would make it dead code.
//     "seeds from score.ippons" below fails if anyone "simplifies" it back.
//   * window.ipponsFromScore is the REAL implementation, published as a side
//     effect of importing bracket.jsx (the idiom of vsched_bout_middle.test.jsx
//     and how the browser reaches it). Stubbing it would make these assertions
//     circular — in particular the "MK (H1)" case only passes because the real
//     helper strips the backend's hansoku suffix before splitting.
//
// Render project (real React 18 + jsdom): the seeding runs in the component
// body and is only observable through the mounted slot buttons, so there is no
// lighter home that pins the same behaviour. Mirrors the global-stub setup of
// admin_scoring_modal.render.test.jsx.

import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

const STUBBED_GLOBALS = {
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const originals = {};
let ScoreEditorModal;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  // Side-effect import: publishes the REAL window.ipponsFromScore that the
  // editor's fallback calls. Not stubbed on purpose (see header).
  await import('../../bracket.jsx');
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

const SIDE_A = { id: 'p1', name: 'Yamada' }; // Aka  (right column)
const SIDE_B = { id: 'p2', name: 'Tanaka' }; // Shiro (left column)

// A knockout match exactly as state.BracketMatch reaches the client: scoreA /
// scoreB strings, NO ipponsA / ipponsB, still running so there is no winner.
function knockoutMatch(overrides = {}) {
  return {
    id: 'bm1',
    status: 'running',
    phase: 'bracket',
    round: 'Quarter-final',
    court: 'A',
    sideA: SIDE_A,
    sideB: SIDE_B,
    // No compId → the fetchCompetitionDetails effect returns early.
    ...overrides,
  };
}

// Empty slots render "·" (a middle dot placeholder), filled ones the letter.
function slotsOf(container, color) {
  return Array.from(container.querySelectorAll(`.sb-side--${color} .sb-slot`))
    .map((b) => b.textContent);
}

// sides[] in admin_scoring_individual.jsx is [b → shiro, a → aka]:
// sideB is Shiro (left), sideA is Aka (right), matching the app-wide convention.
const akaSlots = (c) => slotsOf(c, 'aka');
const shiroSlots = (c) => slotsOf(c, 'shiro');

function renderEditor(match) {
  return render(
    <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="" />
  );
}

describe('ScoreEditorModal ippon seeding', () => {
  it('is a sanity check that the REAL ipponsFromScore is in play', () => {
    expect(typeof window.ipponsFromScore).toBe('function');
    expect(window.ipponsFromScore('MK')).toEqual(['M', 'K']);
    expect(window.ipponsFromScore('MK (H1)')).toEqual(['M', 'K']);
  });

  // ── THE bug case ──────────────────────────────────────────────────────────
  it('seeds Aka from scoreA when a knockout match carries no ippon arrays', () => {
    const { container } = renderEditor(knockoutMatch({ scoreA: 'M' }));
    // Before the fix these read ['·', '·'] and the next tap overwrote the M.
    expect(akaSlots(container)).toEqual(['M', '·']);
    expect(shiroSlots(container)).toEqual(['·', '·']);
  });

  it('seeds Shiro from scoreB when a knockout match carries no ippon arrays', () => {
    const { container } = renderEditor(knockoutMatch({ scoreB: 'K' }));
    expect(shiroSlots(container)).toEqual(['K', '·']);
    expect(akaSlots(container)).toEqual(['·', '·']);
  });

  it('seeds both sides from their score strings', () => {
    const { container } = renderEditor(knockoutMatch({ scoreA: 'M', scoreB: 'K' }));
    expect(akaSlots(container)).toEqual(['M', '·']);
    expect(shiroSlots(container)).toEqual(['K', '·']);
  });

  it('strips the backend hansoku suffix rather than splitting it into slots', () => {
    // engine/scoring.go formatScore appends " (H1)". A naive split would seed
    // ["M", " ", "(", "H", "1", ")"]; the real ipponsFromScore strips it.
    const { container } = renderEditor(knockoutMatch({ scoreA: 'M (H1)' }));
    expect(akaSlots(container)).toEqual(['M', '·']);
  });

  // ── no regression on the pool / league shape ──────────────────────────────
  it('still seeds from ipponsA / ipponsB when the arrays are present', () => {
    const { container } = renderEditor({
      id: 'pm1',
      status: 'running',
      phase: 'pool',
      poolName: 'Pool 1',
      court: 'A',
      sideA: SIDE_A,
      sideB: SIDE_B,
      ipponsA: ['M', 'K'],
      ipponsB: ['D'],
    });
    expect(akaSlots(container)).toEqual(['M', 'K']);
    expect(shiroSlots(container)).toEqual(['D', '·']);
  });

  it('prefers the ippon arrays over a disagreeing score string', () => {
    const { container } = renderEditor(knockoutMatch({
      ipponsA: ['M', 'K'],
      scoreA: 'D',
      ipponsB: [],
      scoreB: 'T',
    }));
    expect(akaSlots(container)).toEqual(['M', 'K']);
    // An explicitly EMPTY ipponsB still wins over scoreB: the array is the
    // authoritative source when the wire carries one.
    expect(shiroSlots(container)).toEqual(['·', '·']);
  });

  // ── the last-resort branch must stay reachable ────────────────────────────
  it('seeds from score.ippons when there is neither an array nor a score string', () => {
    // GUARD against replacing `cleanA.length ? ... : ...` with `cleanA || ...`:
    // cleanA is [] here, which is TRUTHY, so `||` would return the empty array
    // and this branch would become dead code — slots would read ['·', '·'].
    const { container } = renderEditor(knockoutMatch({
      status: 'completed',
      winner: SIDE_A,
      score: { type: 'ippon', ippons: ['M', 'K'] },
    }));
    expect(akaSlots(container)).toEqual(['M', 'K']);
    expect(shiroSlots(container)).toEqual(['·', '·']);
  });

  it('attributes score.ippons to the winning side only', () => {
    const { container } = renderEditor(knockoutMatch({
      status: 'completed',
      winner: SIDE_B,
      score: { type: 'ippon', ippons: ['D'] },
    }));
    expect(shiroSlots(container)).toEqual(['D', '·']);
    expect(akaSlots(container)).toEqual(['·', '·']);
  });

  // ── the "•" placeholder filter applies to BOTH sources ────────────────────
  it('filters the "•" placeholder out of the ippon arrays', () => {
    const { container } = renderEditor(knockoutMatch({
      ipponsA: ['•', 'M'],
      ipponsB: ['•', '•'],
    }));
    expect(akaSlots(container)).toEqual(['M', '·']);
    expect(shiroSlots(container)).toEqual(['·', '·']);
  });

  it('filters the "•" placeholder out of a parsed score string', () => {
    const { container } = renderEditor(knockoutMatch({ scoreA: '•M', scoreB: '•' }));
    expect(akaSlots(container)).toEqual(['M', '·']);
    // scoreB parses to ["•"], which filters to empty — and with no winner /
    // score.ippons to fall back on, Shiro stays empty rather than showing "•".
    expect(shiroSlots(container)).toEqual(['·', '·']);
  });
});
