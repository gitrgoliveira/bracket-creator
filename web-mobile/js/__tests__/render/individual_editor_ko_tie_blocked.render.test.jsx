// bc-rawm: a tied INDIVIDUAL knockout bout (equal ippon counts, no hantei
// verdict, no recorded withdrawal) has no winner. buildPatch's own ippon-
// decided branch falls through to a hikiwake result with winner:null when
// the two sides are level -- a shape a knockout match can never legally
// carry -- so Finish (and Enter, and Finish + Start Next, all keyed on the
// same canFinish) must refuse it until the operator either scores past the
// tie or records hantei. The server refuses the identical shape on a
// completed correction (validateBracketCompletion), so the block holds
// there too, not just on the first Finish.
//
// Reuses isKoTieBlocked from admin_scoring_team.jsx (the team editor's
// identical "knockout match, no winner" rule), never a re-derivation of it.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
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

// Yamada (aka) vs Tanaka (shiro), a running knockout Round 1 bout.
const match = {
  id: 'm-r1-0', compId: 'comp1', status: 'running', phase: 'bracket', round: 'Round 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },
  sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
};

async function mount(props = {}) {
  let view;
  let onSubmit = vi.fn().mockResolvedValue(undefined);
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="secret" {...props} />);
  });
  return { ...view, onSubmit: props.onSubmit || onSubmit };
}

const addButton = (side, letter) => [...document.querySelectorAll(`.sb-side--${side} .ipt-btn`)].find((b) => b.textContent === letter);
const finishButton = () => [...document.querySelectorAll('.score-nav button')].find((b) => /^(Finish|Save correction|Needs a winner)/.test(b.textContent));

describe('a tied individual knockout bout blocks Finish (bc-rawm)', () => {
  it('1-1 disables Finish, changes its label, and states the reason', async () => {
    await mount();
    await act(async () => { fireEvent.click(addButton('aka', 'M')); });
    await act(async () => { fireEvent.click(addButton('shiro', 'M')); });

    const btn = finishButton();
    expect(btn.textContent).toBe('Needs a winner');
    expect(btn.disabled).toBe(true);
    expect(btn.title).toBe('Needs a winner: fight encho, then record hantei if still tied.');
  });

  it('a non-tied score (1-0) leaves Finish enabled, with its usual label', async () => {
    await mount();
    await act(async () => { fireEvent.click(addButton('aka', 'M')); });

    const btn = finishButton();
    expect(btn.textContent).toBe('Finish');
    expect(btn.disabled).toBe(false);
  });

  it('Enter does not submit while tied', async () => {
    const { onSubmit } = await mount();
    await act(async () => { fireEvent.click(addButton('aka', 'M')); });
    await act(async () => { fireEvent.click(addButton('shiro', 'M')); });
    await act(async () => { fireEvent.keyDown(window, { key: 'Enter' }); });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Enter submits once the tie is broken', async () => {
    const { onSubmit } = await mount();
    await act(async () => { fireEvent.click(addButton('aka', 'M')); });
    await act(async () => { fireEvent.keyDown(window, { key: 'Enter' }); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  // The server refuses the identical tied shape on a completed correction
  // (validateBracketCompletion), so the block must hold there too: a
  // completed match that now reads 1-1 (e.g. after the operator removed a
  // point) cannot Save correction either.
  it('blocks a completed correction that now reads 1-1 too', async () => {
    await mount({ match: { ...match, status: 'completed', ipponsA: ['M'], ipponsB: ['K'], winner: null } });
    const btn = finishButton();
    expect(btn.textContent).toBe('Needs a winner');
    expect(btn.disabled).toBe(true);
  });

  // A recorded withdrawal already has a winner regardless of the scoreline,
  // so it is exempt from the tie block: Save correction stays available.
  it('does not block a withdrawal-decided completed match even when its scoreline reads tied', async () => {
    await mount({
      match: {
        ...match, status: 'completed', decision: 'kiken-voluntary', decisionBy: 'shiro',
        ipponsA: ['○', '○'], ipponsB: [], winner: { id: 'p1', name: 'Yamada' },
      },
    });
    const btn = finishButton();
    expect(btn.textContent).toBe('Save correction');
    expect(btn.disabled).toBe(false);
  });

  // bc-cse: aTotal===bTotal is true by construction at a pristine 0-0 (no
  // points struck yet), so without the hasPointsOrDraw gate this read "Needs
  // a winner" on EVERY knockout bout before anything happened -- disabled
  // either way (canFinish already requires points), but the wording claimed
  // a tie that never occurred.
  it('a fresh 0-0 knockout bout (nothing scored yet) reads "Finish", not "Needs a winner"', async () => {
    await mount();
    const btn = finishButton();
    expect(btn.textContent).toBe('Finish');
    expect(btn.disabled).toBe(true); // still nothing to finish -- just not mislabeled
  });

  // Same reasoning for a match that has not even started: it reads 0-0 the
  // same way a fresh running match does.
  it('a SCHEDULED knockout match reads "Finish", not "Needs a winner", even at 0-0', async () => {
    await mount({ match: { ...match, status: 'scheduled' } });
    const btn = finishButton();
    expect(btn.textContent).toBe('Finish');
  });

  // A pool-phase individual match is not knockout, so the tie block never
  // engages there: a 1-1 pool bout is drawable, not "needs a winner".
  it('does not block a tied pool-phase match', async () => {
    await mount({ match: { ...match, phase: 'pool', poolName: 'Pool 1' } });
    await act(async () => { fireEvent.click(addButton('aka', 'M')); });
    await act(async () => { fireEvent.click(addButton('shiro', 'M')); });
    const btn = finishButton();
    expect(btn.textContent).not.toBe('Needs a winner');
  });
});
