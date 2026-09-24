// bc-kcsh: "Opening Correct on a match decided by kiken or fusenpai (team and
// individual) shows the recorded decision and the winner before any edit."
// A match has ONE result and every surface asking for it shows the same one,
// and the score editors are such surfaces. Two gaps this pins:
//   - the TEAM band read its verdict off the bouts, which the withdrawal
//     ended, so a won match read "DRAW"; it now states the recorded winner
//     and decision while the withdrawal is in force (IV/PW stay the
//     bout-derived standings figures, as on the viewer card);
//   - neither editor put the Kiken/Fus. result mark beside the withdrawn
//     side. It rides beside that side's name (WithdrawalMarkedName, from
//     sideMarks + withdrawnKeyOf), on the inner side, never in the centre.

import React from 'react';
import { render, act, screen } from '@testing-library/react';
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
    recordDecision: vi.fn(),
    reopenMatch: vi.fn(),
    requeueBlockerAndReopen: vi.fn(),
    putMatchLineup: vi.fn(),
  };
});

// A fixed-order team pool match Kyoto (aka) won bout 1 of, then withdrew
// from. The server stores the ruling's winner (Osaka) and its default-win
// maru; the unfought bouts stay blank.
function teamWithdrawal(overrides = {}) {
  return {
    id: 'm-pool-1', compId: 'comp1', status: 'completed', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto', number: 'T1' },
    sideB: { id: 'team-osaka', name: 'Osaka', number: 'T2' },
    subResults: [{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Kyoto', decision: '' }],
    decision: 'kiken-voluntary', decisionBy: 'aka',
    winner: { id: 'team-osaka', name: 'Osaka' }, ipponsB: ['○', '○'],
    ...overrides,
  };
}

// An individual pool match Endo (shiro) struck men in, then withdrew from.
function individualWithdrawal(overrides = {}) {
  return {
    id: 'm-pool-2', compId: 'comp2', status: 'completed', phase: 'pool', poolName: 'Pool A', court: 'D',
    sideA: { id: 'p-aoki', name: 'Aoki Taro', number: 'K1' },
    sideB: { id: 'p-endo', name: 'Endo Goro', number: 'K3' },
    ipponsA: ['○', '○'], ipponsB: ['M'], hansokuA: 0, hansokuB: 0,
    decision: 'kiken-voluntary', decisionBy: 'shiro',
    winner: { id: 'p-aoki', name: 'Aoki Taro' },
    ...overrides,
  };
}

async function mount(match) {
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret" />);
  });
}

const header = () => document.querySelector('.editor-modal__body .sb-match');
const sideName = (color) => header().querySelector(`.sb-side--${color} .sb-name`);

// The mark names ONE competitor: it sits in that side's name cell, on the
// inner side (across the name from the number chip), and nowhere else.
function expectMarkBeside(color, mark) {
  const other = color === 'shiro' ? 'aka' : 'shiro';
  const el = screen.getByTestId(`withdrawal-mark-${color}`);
  expect(el.textContent).toBe(mark);
  expect(sideName(color).contains(el)).toBe(true);
  expect(screen.queryByTestId(`withdrawal-mark-${other}`)).toBeNull();
  expect(header().querySelector('.sb-center').textContent).not.toContain(mark);
  const nameText = sideName(color).querySelector('.numbered-name__text');
  // DOCUMENT_POSITION_FOLLOWING (4): the second node follows the first.
  const markFollowsName = !!(nameText.compareDocumentPosition(el) & 4);
  expect(markFollowsName).toBe(color === 'shiro');
}

describe('team editor: correcting a match a withdrawal ended', () => {
  it('states the recorded winner and decision, not the bouts\' DRAW', async () => {
    await mount(teamWithdrawal());
    expect(screen.getByTestId('team-summary-result').textContent).toBe('SHIRO WIN');
    expect(screen.getByTestId('team-summary-decision').textContent).toBe('Kiken – Voluntary');
    // IV/PW are the bout-derived standings figures, as on the viewer card.
    const stats = [...document.querySelectorAll('.team-summary__stats')].map((n) => n.textContent);
    expect(stats).toEqual(['IV: 0 · PW: 0', 'IV: 1 · PW: 1']);
  });

  it('marks the withdrawn team beside its name, never in the centre', async () => {
    await mount(teamWithdrawal());
    expectMarkBeside('aka', 'Kiken');
  });

  it('a fusenpai names the no-show team and the other side as winner', async () => {
    await mount(teamWithdrawal({
      decision: 'fusenpai', decisionBy: 'shiro',
      winner: { id: 'team-kyoto', name: 'Kyoto' }, ipponsA: ['○', '○'], ipponsB: [],
    }));
    expect(screen.getByTestId('team-summary-result').textContent).toBe('AKA WIN');
    expect(screen.getByTestId('team-summary-decision').textContent).toBe('Fusenpai');
    expectMarkBeside('shiro', 'Fus.');
  });

  it('shows neither once no withdrawal is in force', async () => {
    await mount(teamWithdrawal({ status: 'running', decision: '', decisionBy: '', winner: null, ipponsB: [] }));
    expect(screen.queryByTestId('team-summary-decision')).toBeNull();
    expect(screen.queryByTestId('withdrawal-mark-aka')).toBeNull();
    expect(screen.queryByTestId('withdrawal-mark-shiro')).toBeNull();
  });
});

describe('individual editor: correcting a match a withdrawal ended', () => {
  it('marks the withdrawn competitor beside their name, never in the centre', async () => {
    await mount(individualWithdrawal());
    expectMarkBeside('shiro', 'Kiken');
  });

  it('a fusenpai marks the no-show with Fus.', async () => {
    await mount(individualWithdrawal({
      decision: 'fusenpai', decisionBy: 'aka',
      winner: { id: 'p-endo', name: 'Endo Goro' }, ipponsA: [], ipponsB: ['○', '○'],
    }));
    expectMarkBeside('aka', 'Fus.');
  });

  it('shows no mark once no withdrawal is in force', async () => {
    await mount(individualWithdrawal({ status: 'running', decision: '', decisionBy: '', winner: null, ipponsA: [] }));
    expect(screen.queryByTestId('withdrawal-mark-aka')).toBeNull();
    expect(screen.queryByTestId('withdrawal-mark-shiro')).toBeNull();
  });
});
