// bc-dnst: the team sheet's clear-a-mark hint.
//
// Operator ruling 2026-09-16: a team encounter carries ONE hint, placed after
// the team names and before the first bout. The individual board states the
// same sentence under its own marks, but the team sheet repeats a row per
// bout, so repeating it there would be noise. It shows on the SAME condition
// as the individual board's: only while there is a mark to clear.
//
// What this pins, and why each part would rot silently otherwise:
//   1. Absent with no marks. Without this the hint could become unconditional
//      and nothing would fail.
//   2. Present once a bout carries a mark.
//   3. ORDER: after the team header, before the first bout row. The ruling is
//      about placement, so asserting mere presence would pass on a hint
//      rendered at the bottom of the sheet.
//   4. One only, however many bouts are scored.
//   5. The hantei mark does not raise it: that mark names a verdict and
//      carries its own "click to undo" title, which is a different sentence.

import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
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

const HINT = '[data-testid="team-scoring-clear-hint"]';

function teamMatch(subResults) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: 'bracket',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    compFormat: 'knockout',
    teamMatchType: 'fixed',
    round: 'Semi-final',
    matchNumber: 1,
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    ...(subResults ? { subResults } : {}),
  };
}

async function mount(subResults) {
  window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
    id: 'comp1',
    config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] },
  });
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal
        match={teamMatch(subResults)}
        onClose={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        password=""
      />
    );
  });
  return utils;
}

// One scored bout: Shiro took a men, Aka nothing.
const ONE_MARK = [
  { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M'] },
];
const THREE_MARKS = [
  { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M'] },
  { position: 2, sideA: 'A2', sideB: 'B2', ipponsA: ['K'], ipponsB: [] },
  { position: 3, sideA: 'A3', sideB: 'B3', ipponsA: ['D'], ipponsB: ['M'] },
];

describe('team editor: the clear-a-mark hint', () => {
  it('does not render while no bout carries a mark', async () => {
    const { container } = await mount(null);
    expect(container.querySelectorAll(HINT)).toHaveLength(0);
  });

  it('renders once a bout carries a mark, with the same words as the individual board', async () => {
    const { container } = await mount(ONE_MARK);
    const hint = container.querySelector(HINT);
    expect(hint).not.toBeNull();
    expect(hint.textContent).toBe('Tap a scored mark to clear it');
    // Same owner as the individual board's line, not a second style.
    expect(hint.classList.contains('sb-hint')).toBe(true);
  });

  it('sits after the team names and before the first bout', async () => {
    const { container } = await mount(ONE_MARK);
    const hint = container.querySelector(HINT);
    const header = container.querySelector('.sb-match');
    const firstBout = container.querySelector('.team-sub-match');
    expect(header).not.toBeNull();
    expect(firstBout).not.toBeNull();
    // Node.compareDocumentPosition: DOCUMENT_POSITION_FOLLOWING === 4.
    expect(header.compareDocumentPosition(hint) & 4).toBeTruthy();
    expect(hint.compareDocumentPosition(firstBout) & 4).toBeTruthy();
  });

  it('renders exactly one hint however many bouts are scored', async () => {
    const { container } = await mount(THREE_MARKS);
    expect(container.querySelectorAll(HINT)).toHaveLength(1);
  });

  it('is not raised by a hantei mark alone, which undoes through its own control', async () => {
    // A daihyosen row decided by hantei: the Ht mark is non-scoring, so
    // realIppons drops it and there is no tappable ippon anywhere.
    const { container } = await mount([
      { position: -1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['Ht'] },
    ]);
    expect(container.querySelectorAll(HINT)).toHaveLength(0);
  });
});
