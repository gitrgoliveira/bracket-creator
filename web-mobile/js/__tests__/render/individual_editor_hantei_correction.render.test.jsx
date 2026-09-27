import React from 'react';
import { render, fireEvent, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// bc-htcr: a hantei verdict recorded for the wrong side could not be
// corrected. The hantei buttons wrote straight through, with no correction
// reason, so the server refused the completed -> completed write
// ("correcting a completed match result requires a non-empty
// correctionReason") and the wrong verdict stood. There was no other way:
// Save correction is off while the hantei is set, and cancelling the hantei
// leaves a tie a knockout refuses. A hantei tap on a completed match is a
// correction like any other, so it asks for the reason first and carries it.

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

// A finished knockout bout, 1-1, that the judges gave to Yamada (AKA).
const hanteiForYamada = (overrides = {}) => ({
  id: 'm1',
  compId: 'c1',
  status: 'completed',
  phase: 'knockout',
  round: 'Semi-final',
  court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, // AKA
  sideB: { id: 'p2', name: 'Tanaka' }, // SHIRO
  ipponsA: ['M'],
  ipponsB: ['K'],
  hansokuA: 0,
  hansokuB: 0,
  decidedByHantei: true,
  winner: { id: 'p1', name: 'Yamada' },
  ...overrides,
});

const confirmReason = async () => {
  const prompt = document.querySelector('.reason-prompt');
  expect(prompt, 'the correction asks for its reason').toBeTruthy();
  await act(async () => { fireEvent.click(prompt.querySelector('button[type="submit"]')); });
};

describe('correcting a hantei verdict to the other side (bc-htcr)', () => {
  it('asks for the reason first, then writes the new verdict with it', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal match={hanteiForYamada()} onClose={vi.fn()} onSubmit={onSubmit} password="" />);

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-hantei-shiro')); });
    expect(onSubmit, 'nothing is written before the reason is given').not.toHaveBeenCalled();

    await confirmReason();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.winner).toMatchObject({ id: 'p2', name: 'Tanaka' });
    expect(patch.decidedByHantei).toBe(true);
    expect(patch.status).toBe('completed');
    expect(patch.correctionReason, 'the reason rides the hantei write').toBeTruthy();
    // The tied scoreline the verdict rests on is kept.
    expect(patch.ipponsA).toEqual(['M']);
    expect(patch.ipponsB).toEqual(['K']);
  });

  it('cancelling the reason writes nothing', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal match={hanteiForYamada()} onClose={vi.fn()} onSubmit={onSubmit} password="" />);

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-hantei-shiro')); });
    const cancel = [...document.querySelectorAll('.reason-prompt button')].find((b) => b.textContent === 'Cancel');
    await act(async () => { fireEvent.click(cancel); });
    expect(document.querySelector('.reason-prompt')).toBeNull();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('a verdict on a match still being fought is not a correction and asks nothing', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal
      match={hanteiForYamada({ status: 'running', decidedByHantei: false, winner: null })}
      onClose={vi.fn()} onSubmit={onSubmit} password="" />);

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-hantei-arm')); });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-hantei-aka')); });
    expect(document.querySelector('.reason-prompt')).toBeNull();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].correctionReason).toBeUndefined();
  });
});
