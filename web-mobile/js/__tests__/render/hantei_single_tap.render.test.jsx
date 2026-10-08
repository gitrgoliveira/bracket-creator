import React from 'react';
import { render, fireEvent, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

// bc-htsd: the SHIRO wins / AKA wins buttons only PICK a side. Finish (under
// the two-tap guard) commits, so a mis-tap on the neighbouring button cannot
// record the opposite verdict and advance the bracket. Mirrors the team
// editor's daihyosen hantei.

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
afterEach(() => { vi.useRealTimers(); });

const tied = (overrides = {}) => ({
  id: 'm1', status: 'running', phase: 'knockout', round: 'Semi-final', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },   // AKA
  sideB: { id: 'p2', name: 'Tanaka' },   // SHIRO
  ipponsA: ['M'], ipponsB: ['K'], hansokuA: 0, hansokuB: 0,
  ...overrides,
});

// Most steps are keyboard-style clicks (no bounce guard); the Finish two-tap
// below is driven as a real finger would, through the bounce window.
const click = keyboardClick;
const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });
const finishBtn = () => [...document.querySelectorAll('.score-nav button')].find((b) => /Finish|Tap again/.test(b.textContent));
const armAndPick = async (testid) => {
  await click(screen.getByTestId('scoring-modal-hantei-arm'));
  await click(screen.getByTestId(testid));
};

describe('hantei side buttons only pick (bc-htsd)', () => {
  it('each button names its colour and competitor, for an operator who does not know SHIRO and AKA', async () => {
    render(<ScoreEditorModal match={tied()} onClose={vi.fn()} onSubmit={vi.fn()} password="" />);
    await click(screen.getByTestId('scoring-modal-hantei-arm'));
    expect(screen.getByTestId('scoring-modal-hantei-shiro').textContent).toBe('SHIRO (White) wins: Tanaka');
    expect(screen.getByTestId('scoring-modal-hantei-aka').textContent).toBe('AKA (Red) wins: Yamada');
  });

  it('picking a side commits nothing; Finish, tapped twice, commits exactly the pick', async () => {
    vi.useFakeTimers();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const onSubmitAndNext = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal match={tied()} onClose={vi.fn()} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} password="" />);
    await armAndPick('scoring-modal-hantei-aka');
    expect(onSubmit).not.toHaveBeenCalled();
    expect(onSubmitAndNext).not.toHaveBeenCalled();
    expect(screen.getByTestId('scoring-modal-hantei-aka').getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByTestId('scoring-modal-hantei-shiro').getAttribute('aria-pressed')).toBe('false');

    await pointerTap(finishBtn());
    expect(onSubmitAndNext, 'the first Finish tap only arms').not.toHaveBeenCalled();
    await pointerTap(finishBtn());
    expect(onSubmitAndNext, 'the bounce of the arming tap does not commit').not.toHaveBeenCalled();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(finishBtn());
    expect(onSubmitAndNext).toHaveBeenCalledTimes(1);
    expect(onSubmit).not.toHaveBeenCalled();
    const patch = onSubmitAndNext.mock.calls[0][0];
    expect(patch.winner).toMatchObject({ id: 'p1', name: 'Yamada' });
    expect(patch.decidedByHantei).toBe(true);
    expect(patch.status).toBe('completed');
    expect(patch.ipponsA).toEqual(['M']);
    expect(patch.ipponsB).toEqual(['K']);
  });

  it('re-picking the other side before committing changes the committed side', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal match={tied()} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    await armAndPick('scoring-modal-hantei-aka');
    await click(finishBtn()); // armed
    await click(screen.getByTestId('scoring-modal-hantei-shiro'));
    expect(finishBtn().textContent, 'changing the pick disarms Finish').not.toMatch(/Tap again/);
    await click(finishBtn());
    expect(onSubmit).not.toHaveBeenCalled();
    await click(finishBtn());
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].winner).toMatchObject({ id: 'p2', name: 'Tanaka' });
  });

  it('Finish needs a pick, even on a 0-0 tie', async () => {
    render(<ScoreEditorModal match={tied({ ipponsA: [], ipponsB: [] })} onClose={vi.fn()} onSubmit={vi.fn()} password="" />);
    await click(screen.getByTestId('scoring-modal-hantei-arm'));
    expect(finishBtn().disabled).toBe(true);
    expect(finishBtn().textContent).not.toMatch(/Needs a winner/);
    expect(screen.getByTestId('scoring-modal-hantei-hint').textContent, 'a disabled Finish says why').toBe('Pick the hantei winner, then finish.');
    await click(screen.getByTestId('scoring-modal-hantei-shiro'));
    expect(finishBtn().disabled).toBe(false);
    expect(screen.queryByTestId('scoring-modal-hantei-hint')).toBeNull();
  });

  it('a verdict recorded on another device is adopted as the pick', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const { rerender } = render(<ScoreEditorModal match={tied()} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    await act(async () => { rerender(<ScoreEditorModal
      match={tied({ decidedByHantei: true, winner: { id: 'p2', name: 'Tanaka' } })}
      onClose={vi.fn()} onSubmit={onSubmit} password="" />); });
    expect(screen.getByTestId('scoring-modal-hantei-shiro').className).toContain('btn--primary');
    expect(screen.getByTestId('scoring-modal-hantei-aka').className).not.toContain('btn--primary');
  });

  it('the running autosave patch never carries the hantei verdict', async () => {
    vi.useFakeTimers();
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal match={tied({ ipponsA: [], ipponsB: [] })} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    const add = (side, letter) => [...document.querySelectorAll(`.sb-side--${side} .ipt-btn`)].find((b) => b.textContent === letter);
    await click(add('aka', 'M'));
    await click(add('shiro', 'K'));
    await armAndPick('scoring-modal-hantei-aka');
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(onSubmit).toHaveBeenCalled();
    for (const [patch] of onSubmit.mock.calls) {
      expect(patch.status).toBe('running');
      expect(patch.decidedByHantei).not.toBe(true);
    }
  });

  it('Enter commits only the picked side', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal match={tied()} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    await click(screen.getByTestId('scoring-modal-hantei-arm'));
    await act(async () => { fireEvent.keyDown(document, { key: 'Enter' }); });
    expect(onSubmit, 'no pick, no commit').not.toHaveBeenCalled();
    await click(screen.getByTestId('scoring-modal-hantei-shiro'));
    document.activeElement?.blur?.();
    await act(async () => { fireEvent.keyDown(document, { key: 'Enter' }); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].winner).toMatchObject({ id: 'p2', name: 'Tanaka' });
    expect(onSubmit.mock.calls[0][0].decidedByHantei).toBe(true);
  });

  it('leaving a running match with hantei only armed asks nothing', async () => {
    const onClose = vi.fn();
    window.confirmDialog.mockClear();
    render(<ScoreEditorModal match={tied()} onClose={onClose} onSubmit={vi.fn()} password="" />);
    await click(screen.getByTestId('scoring-modal-hantei-arm'));
    await act(async () => { fireEvent.keyDown(document, { key: 'Escape' }); });
    expect(window.confirmDialog, 'the arm alone is a mode: leaving asks nothing').not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('leaving a running match after picking a side prompts, and staying keeps the editor', async () => {
    const onClose = vi.fn();
    window.confirmDialog.mockClear();
    window.confirmDialog.mockResolvedValueOnce(false);
    render(<ScoreEditorModal match={tied()} onClose={onClose} onSubmit={vi.fn()} password="" />);
    await armAndPick('scoring-modal-hantei-aka');
    await act(async () => { fireEvent.keyDown(document, { key: 'Escape' }); });
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('a recorded hantei on an untied scoreline (legacy data) cannot be finished', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal
      match={tied({ status: 'completed', ipponsA: ['M'], ipponsB: [], decidedByHantei: true, winner: { id: 'p1', name: 'Yamada' } })}
      onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    const save = [...document.querySelectorAll('.score-nav button')].find((x) => x.textContent === 'Save correction');
    expect(save.disabled, 'a Save would send a hantei the server refuses on an untied line').toBe(true);
    // Not stuck: the row stays with its Cancel and says why, so the
    // operator can drop the hantei and correct the result.
    expect(screen.getByTestId('scoring-modal-hantei-hint').textContent).toMatch(/needs a tied score/);
    await click(screen.getByTestId('scoring-modal-hantei-cancel'));
    expect(screen.queryByTestId('scoring-modal-hantei-row'), 'untied and not armed: no hantei row').toBeNull();
  });
});
