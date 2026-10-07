import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// bc-crpn: the court console starts the next match only through
// onSubmitAndNext and onAfterDecision. A completed match shown as a
// correction must reach neither: Save correction calls onSubmit and never
// onSubmitAndNext, in the individual and the team editor (the engi editor is
// pinned in admin_scoring_engi.render.test.jsx). That is why the console needs
// no start gate on those two doors; if this changes, it needs one.

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
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

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

const buttonLike = (re) => [...document.querySelectorAll('button')].find((b) => re.test(b.textContent || ''));

describe('Save correction saves the match alone (bc-crpn)', () => {
  it('individual editor: onSubmit, never onSubmitAndNext', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    const onSubmitAndNext = vi.fn().mockResolvedValue({ applied: true });
    const match = {
      id: 'm1', compId: 'c1', status: 'completed', phase: 'pool', poolName: 'Pool 1', court: 'A',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
      ipponsA: ['M'], ipponsB: [], hansokuA: 0, hansokuB: 0,
      winner: { id: 'p1', name: 'Yamada' },
    };
    await act(async () => {
      render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} password="" />);
    });
    await act(async () => { fireEvent.click(buttonLike(/Save correction/)); });
    const prompt = document.querySelector('.reason-prompt');
    expect(prompt, 'a correction asks for its reason').toBeTruthy();
    await act(async () => { fireEvent.click(prompt.querySelector('button[type="submit"]')); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].status).toBe('completed');
    expect(onSubmitAndNext).not.toHaveBeenCalled();
  });

  it('team editor: onSubmit, never onSubmitAndNext', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    const onSubmitAndNext = vi.fn().mockResolvedValue({ applied: true });
    const match = {
      id: 'm-pool-1', compId: 'comp1', status: 'completed', phase: 'pool', poolName: 'Pool 1', court: 'A',
      compKind: 'team', teamSize: 3,
      sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
      decision: 'kiken-voluntary', decisionBy: 'aka', decisionReason: 'knee',
      winner: { id: 'team-osaka', name: 'Osaka' }, ipponsB: ['○', '○'],
      subResults: [{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Kyoto', decision: '' }],
    };
    await act(async () => {
      render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} password="" />);
    });
    await act(async () => { fireEvent.click(buttonLike(/Save correction/)); });
    const confirm = buttonLike(/Tap again|Confirm/);
    if (confirm) await act(async () => { fireEvent.click(confirm); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].status).toBe('completed');
    expect(onSubmitAndNext).not.toHaveBeenCalled();
  });
});
