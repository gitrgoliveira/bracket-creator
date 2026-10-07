// bc-dnst: the team sheet's clear-a-mark hint.
//
// Operator ruling 2026-09-16: a team encounter carries ONE hint, placed after
// the team names and before the first bout. The individual board states the
// same sentence under its own marks, but the team sheet repeats a row per
// bout, so repeating it there would be noise. It is SHOWN on the same condition
// as the individual board's: only while there is a mark to clear. Operator
// ruling 2026-10-06, "Hold its space": otherwise it is not removed but held
// hidden, so the bout rows never move when it comes and goes.
//
// What this pins, and why each part would rot silently otherwise:
//   1. Held hidden with no marks: still in the layout, not announced. Without
//      this the hint could become unconditional, or go back to being added
//      and removed, and nothing would fail.
//   2. Shown once a bout carries a mark.
//   3. ORDER: after the team header, before the first bout row. The ruling is
//      about placement, so asserting mere presence would pass on a hint
//      rendered at the bottom of the sheet.
//   4. One only, however many bouts are scored.
//   5. The hantei mark does not raise it: that mark names a verdict and
//      carries its own "click to undo" title, which is a different sentence.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { cssBlock, readStylesheet } from '../helpers/source.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
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

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

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

// Operator ruling 2026-10-06, "Hold its space": the hint is always in the layout and is
// hidden while no mark exists to clear, so the bout rows never move when it comes and
// goes. Hidden is visibility:hidden (the .holds-space utility), which keeps the box and
// takes the element out of the accessibility tree: it is not announced. jsdom loads no
// stylesheet, so the class is what the DOM shows, and the rule is pinned below.
const HELD = 'holds-space';
const heldBy = (hint) => hint.classList.contains(HELD);

describe('team editor: the clear-a-mark hint', () => {
  it('is rendered but held hidden while no bout carries a mark, so it keeps its space', async () => {
    const { container } = await mount(null);
    const hint = container.querySelector(HINT);
    expect(hint, 'always in the layout').not.toBeNull();
    expect(heldBy(hint), 'hidden, not announced').toBe(true);
    expect(hint.textContent, 'the same words').toBe('Tap a scored mark to clear it');
  });

  it('is shown once a bout carries a mark, with the same words as the individual board', async () => {
    const { container } = await mount(ONE_MARK);
    const hint = container.querySelector(HINT);
    expect(hint).not.toBeNull();
    expect(hint.textContent).toBe('Tap a scored mark to clear it');
    expect(heldBy(hint)).toBe(false);
    // Same owner as the individual board's line, not a second style.
    expect(hint.classList.contains('sb-hint')).toBe(true);
  });

  it('is the same element before and after the first mark, in the same place: nothing under it moves', async () => {
    const { container } = await mount(null);
    const before = container.querySelector(HINT);
    const row = container.querySelector('.team-sub-match');
    expect(before.nextElementSibling, 'the bouts follow it').toBe(container.querySelector('.team-bouts-scroll'));

    const shiroM = [...row.querySelectorAll('.team-sub-match__side--shiro button.ipt-btn')].find((b) => b.textContent === 'M');
    await act(async () => { fireEvent.click(shiroM); });

    const after = container.querySelector(HINT);
    expect(after, 'one element throughout, not one that is added').toBe(before);
    expect(heldBy(after), 'now shown').toBe(false);
    expect(after.nextElementSibling).toBe(container.querySelector('.team-bouts-scroll'));
  });

  it('is held again when the last mark is taken back', async () => {
    const { container } = await mount(null);
    const row = container.querySelector('.team-sub-match');
    const shiroM = () => [...row.querySelectorAll('.team-sub-match__side--shiro button.ipt-btn')].find((b) => b.textContent === 'M');
    await act(async () => { fireEvent.click(shiroM()); });
    expect(heldBy(container.querySelector(HINT))).toBe(false);

    // The scored mark is the button a tap takes back.
    await act(async () => { fireEvent.click(row.querySelector('button[title="Click to remove"]')); });

    expect(heldBy(container.querySelector(HINT))).toBe(true);
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
    expect(heldBy(container.querySelector(HINT))).toBe(true);
  });

  it('holds its space through visibility, which keeps the box and leaves the accessibility tree', () => {
    const rule = cssBlock(readStylesheet(), `.${HELD}`);
    expect(rule, `rule .${HELD} exists`).not.toBeNull();
    expect(rule).toMatch(/visibility:\s*hidden/);
    expect(rule, 'display:none would give the space back').not.toMatch(/display:\s*none/);
  });
});
