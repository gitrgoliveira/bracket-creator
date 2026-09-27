// bc-fsnp: the reason under a refused Fusensho shows from the refused tap
// until the operator's next edit to a bout row, and never comes back by itself.
//
// The tapped side was held in state and never cleared, so the reason showed
// again whenever the row returned to the refused state: clear the winner's
// points (the reason hides, since the fusensho is allowed again), strike them
// again, and the reason was back with no tap on Fusensho. The operator's edits
// clear it (updateSub); the server re-seed must not, or the operator's own
// autosave coming back would hide it the moment it appeared.

import React from 'react';
import { render, act, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

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

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.API.recordScore.mockClear();
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function makeMatch(subResults = []) {
  return {
    id: 'tm-fus',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto' },
    sideB: { id: 'team-osaka', name: 'Osaka' },
    subResults,
  };
}

const editor = (match) => (
  <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />
);

async function mount(match) {
  let utils;
  await act(async () => { utils = render(editor(match)); });
  return utils;
}

// Shiro (sideB) is the left side of each bout, Aka (sideA) the right one.
const boutSide = (n, color) => document.querySelectorAll('.team-sub-match')[n - 1].querySelector(`.team-sub-match__side--${color}`);
const ipponButton = (n, color, letter) => [...boutSide(n, color).querySelectorAll('button.ipt-btn')].find((b) => b.textContent === letter);
const fusenshoButton = (n, color) => boutSide(n, color).querySelector('[data-testid="scoring-modal-fusensho-button"]');
const refusalNote = (n, color) => boutSide(n, color).querySelector('[data-testid="scoring-modal-fusensho-refused"]');
const markSlot = (n, color, letter) => [...document.querySelectorAll('.team-sub-match')[n - 1].querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)].find((b) => b.textContent === letter);

async function tap(el) {
  await act(async () => { fireEvent.click(el); });
}

// Shiro wins bout 1 2-0, then the operator taps Aka's Fusensho: refused, and
// the reason shows under Aka.
async function refuseAkaFusenshoOnBout1() {
  await tap(ipponButton(1, 'shiro', 'M'));
  await tap(ipponButton(1, 'shiro', 'K'));
  await tap(fusenshoButton(1, 'aka'));
  expect(refusalNote(1, 'aka').textContent).toBe('SHIRO already won this bout: clear their points first');
}

describe('bc-fsnp: a refused Fusensho explains itself once, until the next edit', () => {
  it('clearing the winner\'s point and striking it again does not bring the reason back', async () => {
    await mount(makeMatch());
    await refuseAkaFusenshoOnBout1();

    // The operator clears one of Shiro's points: the fusensho is allowed now.
    await tap(markSlot(1, 'shiro', 'K'));
    expect(fusenshoButton(1, 'aka').getAttribute('aria-disabled')).toBeNull();
    expect(refusalNote(1, 'aka')).toBeNull();

    // And strikes it again: refused once more, but nobody tapped Fusensho.
    await tap(ipponButton(1, 'shiro', 'K'));
    expect(fusenshoButton(1, 'aka').getAttribute('aria-disabled')).toBe('true');
    expect(refusalNote(1, 'aka')).toBeNull();

    // A new tap on it explains again.
    await tap(fusenshoButton(1, 'aka'));
    expect(refusalNote(1, 'aka')).not.toBeNull();
  });

  it('an edit to another bout hides it, though bout 1 still refuses', async () => {
    await mount(makeMatch());
    await refuseAkaFusenshoOnBout1();

    await tap(ipponButton(2, 'shiro', 'M'));
    expect(fusenshoButton(1, 'aka').getAttribute('aria-disabled')).toBe('true');
    expect(refusalNote(1, 'aka')).toBeNull();
  });

  it('a server update to the match does not hide it', async () => {
    // Bout 1 as the server holds it: Shiro won 2-0.
    const bout1 = { position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: ['M', 'K'], winner: 'Osaka', decision: '' };
    const { rerender } = await mount(makeMatch([bout1]));
    await tap(fusenshoButton(1, 'aka'));
    expect(refusalNote(1, 'aka')).not.toBeNull();

    // A write lands and the board follows it (here a point on bout 2). This is
    // the editor adopting the server, not the operator editing a row.
    const bout2 = { position: 2, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: '', decision: '' };
    await act(async () => { rerender(editor(makeMatch([bout1, bout2]))); });
    // The board did follow it.
    expect(markSlot(2, 'aka', 'M')).toBeTruthy();
    expect(refusalNote(1, 'aka')).not.toBeNull();
  });
});
