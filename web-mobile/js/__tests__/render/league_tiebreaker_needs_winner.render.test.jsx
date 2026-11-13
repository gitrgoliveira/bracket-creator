// A league tie-breaker (-TB-) or daihyosen (-DH-) bout decides a place, so a
// drawn one decides nothing: the individual editor treats it as it treats a
// knockout match. The hikiwake toggle and the x key are off, a tied or drawn
// scoreline reads "Needs a winner", and hantei is still the way out. A mixed
// competition's pool tie-breaker is unchanged (it can still be saved drawn).

import React from 'react';
import { render, screen, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

beforeEach(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });

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

const bout = (id, compFormat, overrides = {}) => ({
  id, compId: 'comp1', compFormat, status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },
  sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
  ...overrides,
});

async function mount(match, props = {}) {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="secret" {...props} />);
  });
  return { onSubmit };
}

const addButton = (side, letter) => [...document.querySelectorAll(`.sb-side--${side} .ipt-btn`)].find((b) => b.textContent === letter);
const finishButton = () => [...document.querySelectorAll('.score-nav button')].find((b) => /^(Finish|Save correction|Needs a winner)/.test(b.textContent));
const drawToggle = () => screen.getByTestId('scoring-modal-mark-draw');

describe.each([
  ['a league tie-breaker', 'Pool A-TB-1'],
  ['a league daihyosen', 'Pool A-DH-1'],
])('%s needs a winner', (_label, id) => {
  it('cannot be marked as a draw: the toggle is off and says why', async () => {
    await mount(bout(id, 'league'));
    expect(drawToggle().disabled).toBe(true);
    expect(drawToggle().title).toBe('A tie-breaker needs a winner: fight encho, then decide by hantei');
  });

  it('the x key does nothing', async () => {
    await mount(bout(id, 'league'));
    await act(async () => { fireEvent.keyDown(window, { key: 'x' }); });
    expect(drawToggle().textContent).toBe('Mark draw');
    expect(finishButton().textContent).toBe('Finish');
  });

  it('a level scoreline cannot be finished or saved by Enter', async () => {
    const { onSubmit } = await mount(bout(id, 'league'));
    await act(async () => { fireEvent.click(addButton('aka', 'M')); });
    await act(async () => { fireEvent.click(addButton('shiro', 'M')); });
    const btn = finishButton();
    expect(btn.textContent).toBe('Needs a winner');
    expect(btn.disabled).toBe(true);
    await act(async () => { fireEvent.keyDown(window, { key: 'Enter' }); });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('a draw saved earlier cannot be saved again as a draw', async () => {
    await mount(bout(id, 'league', { status: 'completed', decision: 'hikiwake', winner: null }));
    const btn = finishButton();
    expect(btn.textContent).toBe('Needs a winner');
    expect(btn.disabled).toBe(true);
  });

  it('a score that breaks the tie finishes as usual', async () => {
    const { onSubmit } = await mount(bout(id, 'league'));
    await act(async () => { fireEvent.click(addButton('aka', 'M')); });
    expect(finishButton().disabled).toBe(false);
    await act(async () => { fireEvent.keyDown(window, { key: 'Enter' }); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it('can be decided by hantei', async () => {
    const { onSubmit } = await mount(bout(id, 'league'));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-hantei-arm')); });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-hantei-aka')); });
    const finishBtn = () => [...document.querySelectorAll('.score-nav button')].find((b) => /Finish|Tap again/.test(b.textContent));
    expect(finishBtn().disabled).toBe(false);
    await act(async () => { fireEvent.click(finishBtn()); });
    await act(async () => { fireEvent.click(finishBtn()); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].decidedByHantei).toBe(true);
  });
});

describe('a bout that does not need a winner is unchanged', () => {
  it('a mixed competition pool tie-breaker can still be marked as a draw', async () => {
    await mount(bout('Pool A-TB-1', 'mixed'));
    expect(drawToggle().disabled).toBe(false);
    expect(drawToggle().title).toBe('Mark as draw (hikiwake)');
    await act(async () => { fireEvent.click(drawToggle()); });
    expect(drawToggle().textContent).toBe('Cancel draw');
    expect(finishButton().textContent).not.toBe('Needs a winner');
  });

  it('an ordinary league bout can still be marked as a draw', async () => {
    await mount(bout('Pool A-3', 'league'));
    expect(drawToggle().disabled).toBe(false);
    await act(async () => { fireEvent.click(drawToggle()); });
    expect(drawToggle().textContent).toBe('Cancel draw');
  });
});
