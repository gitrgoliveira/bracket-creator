// bc-cse: a SCHEDULED match a competitor is barred from (withdrew earlier,
// or is kiken-injury and not yet reinstated) cannot be fought. Opening it in
// either editor -- how the operator reaches it, per ineligible_match.jsx's
// header -- must show BarredMatchNotice (admin_scoring_shared.jsx) where
// "Start match" would be, never a Start the server would just refuse.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
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
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
    reinstateCompetitor: vi.fn().mockResolvedValue({}),
  };
});

// Yamada (aka) vs Tanaka (shiro), a scheduled knockout Round 1 bout. Tanaka
// withdrew earlier (kiken-voluntary): the default win is awaited for Yamada.
const individualBarred = (over = {}) => ({
  id: 'm-r1-0', compId: 'comp1', status: 'scheduled', phase: 'bracket', round: 'Round 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
  ineligibleSides: { b: 'kiken-voluntary' },
  ...over,
});

// Kyoto (aka) vs Osaka (shiro), a scheduled team pool match. Kyoto withdrew
// injured (kiken-injury, reinstateable): both the default win and Reinstate
// must be offered.
const teamBarred = (over = {}) => ({
  id: 'm-pool-1', compId: 'comp1', status: 'scheduled', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 3,
  sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
  ineligibleSides: { a: 'kiken-injury' },
  ...over,
});

async function mount(match, props = {}) {
  let view;
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret" {...props} />);
  });
  return view;
}

describe('a scheduled barred match shows BarredMatchNotice instead of Start (bc-cse)', () => {
  it('individual editor: no Start match button; the note and default-win action are offered instead', async () => {
    await mount(individualBarred());
    expect(screen.queryByText('Start match')).toBeNull();
    const notice = screen.getByTestId('barred-match-notice');
    expect(notice.textContent).toContain('Tanaka withdrew: record the default win.');
    // kiken-voluntary is not reinstateable.
    expect(screen.queryByTestId('barred-match-reinstate')).toBeNull();
    const btn = screen.getByTestId('barred-match-default-win');
    expect(btn.textContent).toBe('Record default win for Yamada');

    await act(async () => { fireEvent.click(btn); });
    expect(window.API.recordDecision).toHaveBeenCalledWith('comp1', 'm-r1-0', {
      decision: 'fusensho', decisionBy: 'shiro', decisionReason: 'auto: Tanaka withdrawn',
    }, 'secret');
  });

  // bc-cse: BarredMatchNotice is a full-width block, lifted above
  // .score-nav__actions (a centred WRAPPING flex row of small buttons) --
  // not squeezed into one flex item alongside them.
  it('individual editor: the notice is not nested inside .score-nav__actions', async () => {
    await mount(individualBarred());
    const notice = screen.getByTestId('barred-match-notice');
    const actions = document.querySelector('.score-nav__actions');
    expect(actions.contains(notice)).toBe(false);
  });

  it('individual editor: a non-barred scheduled match still shows Start match', async () => {
    await mount(individualBarred({ ineligibleSides: undefined }));
    expect(screen.getByText('Start match')).toBeTruthy();
    expect(screen.queryByTestId('barred-match-notice')).toBeNull();
  });

  it('team editor: no Start match button; both the default-win action and Reinstate are offered for a kiken-injury withdrawal', async () => {
    await mount(teamBarred());
    expect(screen.queryByText('Start match')).toBeNull();
    const notice = screen.getByTestId('barred-match-notice');
    expect(notice.textContent).toContain('Kyoto withdrew injured: reinstate them or record the default win.');

    const awardBtn = screen.getByTestId('barred-match-default-win');
    expect(awardBtn.textContent).toBe('Record default win for Osaka');
    const reinstateBtn = screen.getByTestId('barred-match-reinstate');
    expect(reinstateBtn.textContent).toBe('Reinstate Kyoto');

    await act(async () => { fireEvent.click(reinstateBtn); });
    expect(window.API.reinstateCompetitor).toHaveBeenCalledWith('comp1', 'team-kyoto', 'secret');
  });

  // bc-cse: same structural rule for the team editor.
  it('team editor: the notice is not nested inside .score-nav__actions', async () => {
    await mount(teamBarred());
    const notice = screen.getByTestId('barred-match-notice');
    const actions = document.querySelector('.score-nav__actions');
    expect(actions.contains(notice)).toBe(false);
  });

  it('team editor: a non-barred scheduled match still shows Start match', async () => {
    await mount(teamBarred({ ineligibleSides: undefined }));
    expect(screen.getByText('Start match')).toBeTruthy();
    expect(screen.queryByTestId('barred-match-notice')).toBeNull();
  });
});

// bc-cse: "If both competitors withdraw, neither receives a win or points."
// A pool/league match can be recorded as drawn; a knockout match cannot (the
// bracket needs a winner), so it offers no action at all.
describe('both sides barred (bc-cse)', () => {
  it('team editor (pool): offers "Record as drawn", which posts an exact hikiwake body', async () => {
    // A real pool id: the server accepts this draw for pool/league ids only.
    await mount(teamBarred({ id: 'Pool 1-0', ineligibleSides: { a: 'kiken-injury', b: 'fusenpai' } }));
    const notice = screen.getByTestId('barred-match-notice');
    expect(notice.textContent).toContain('Both withdrew earlier: neither can fight this match.');
    // Neither single-sided action is offered: awaitedDefaultWin is null when
    // both sides are barred.
    expect(screen.queryByTestId('barred-match-default-win')).toBeNull();
    expect(screen.queryByTestId('barred-match-reinstate')).toBeNull();

    const btn = screen.getByTestId('barred-match-record-drawn');
    expect(btn.textContent).toBe('Record as drawn (neither can fight)');
    await act(async () => { fireEvent.click(btn); });
    expect(window.API.recordDecision).toHaveBeenCalledWith('comp1', 'Pool 1-0', {
      decision: 'hikiwake', decisionReason: 'auto: Kyoto and Osaka withdrawn',
    }, 'secret');
  });

  it('individual editor (knockout): no action at all, and the note says to correct the draw', async () => {
    await mount(individualBarred({ ineligibleSides: { a: 'kiken-voluntary', b: 'fusenpai' } }));
    const notice = screen.getByTestId('barred-match-notice');
    expect(notice.textContent).toBe('Neither can fight: correct the earlier withdrawal or the draw.');
    expect(screen.queryByTestId('barred-match-default-win')).toBeNull();
    expect(screen.queryByTestId('barred-match-reinstate')).toBeNull();
    expect(screen.queryByTestId('barred-match-record-drawn')).toBeNull();
    expect(notice.querySelectorAll('button').length).toBe(0);
  });
});
