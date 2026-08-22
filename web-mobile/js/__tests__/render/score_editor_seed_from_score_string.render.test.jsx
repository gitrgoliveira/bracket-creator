// ScoreEditorModal ippon-slot seeding.
//
// HISTORY: this file used to pin a fallback that parsed the bracket-only
// ScoreA/ScoreB STRINGS (state.BracketMatch on the old wire) when the ippon
// ARRAYS were absent — a knockout match re-read from the server used to carry
// no ipponsA/ipponsB at all, so the editor opened EMPTY on a match that
// already had points and the operator's next tap saved over them (a
// recorded ippon lost mid-match, found in browser UAT, HIGH).
//
// That gap is now closed at the WIRE level, not in this editor: pool and
// bracket matches converge on one shape (ipponsA/ipponsB arrays; scoreA/
// scoreB strings never appear in any response), so every match arrives with
// its arrays already populated. The scoreA/scoreB-string parsing fallback
// (window.ipponsFromScore) was deleted from admin_scoring_individual.jsx
// along with the codec itself (bracket.jsx); this file keeps only the tests
// that still describe real seeding behaviour.
//
// Two things this file still pins:
//   * ippon ARRAYS are the authoritative source and win over anything else
//     on the match object.
//   * the score.type === "ippon" branch must stay REACHABLE as the last
//     resort (quick-score paths that set only score.ippons, no arrays). It
//     is guarded by `cleanA.length ? ... : ...` and NOT by `||`, because an
//     empty array is truthy and `||` would make it dead code.
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

// A knockout match exactly as state.BracketMatch reaches the client: ipponsA /
// ipponsB arrays (the same shape a pool match carries), still running so
// there is no winner.
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
  it('seeds from ipponsA / ipponsB when the arrays are present (bracket match)', () => {
    const { container } = renderEditor(knockoutMatch({
      ipponsA: ['M', 'K'],
      ipponsB: ['D'],
    }));
    expect(akaSlots(container)).toEqual(['M', 'K']);
    expect(shiroSlots(container)).toEqual(['D', '·']);
  });

  it('no regression on the pool / league shape: same seeding from ipponsA / ipponsB', () => {
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

  // Regression guard for the removed codec: a scoreA/scoreB-only match (the
  // pre-fix bracket wire shape) is no longer parsed at all. That shape never
  // arrives from the real server any more, so the editor must not silently
  // seed points from it — an unattributed "M" in scoreA with no ipponsA/B
  // and no score.ippons must render EMPTY slots, not a resurrected letter.
  it('does not seed from scoreA/scoreB (that shape never arrives on the wire)', () => {
    const { container } = renderEditor(knockoutMatch({ scoreA: 'M', scoreB: 'K' }));
    expect(akaSlots(container)).toEqual(['·', '·']);
    expect(shiroSlots(container)).toEqual(['·', '·']);
  });

  // ── the last-resort branch must stay reachable ────────────────────────────
  it('seeds from score.ippons when there is no ippon array', () => {
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

  // ── the "•" placeholder filter applies to the ippon arrays ───────────────
  it('filters the "•" placeholder out of the ippon arrays', () => {
    const { container } = renderEditor(knockoutMatch({
      ipponsA: ['•', 'M'],
      ipponsB: ['•', '•'],
    }));
    expect(akaSlots(container)).toEqual(['M', '·']);
    expect(shiroSlots(container)).toEqual(['·', '·']);
  });
});
