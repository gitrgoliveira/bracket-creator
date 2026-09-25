// bc-kcsh: "Opening Correct on a match decided by kiken or fusenpai (team and
// individual) shows the recorded decision and the winner before any edit."
// A match has ONE result and every surface asking for it shows the same one,
// and the score editors are such surfaces. Two gaps this pins:
//   - the TEAM band read its verdict off the bouts, which the withdrawal
//     ended, so a won match read "DRAW"; it now states the recorded winner
//     and decision while the withdrawal is in force. IV/PW (bc-tmfn) now
//     include a default-win CREDIT for every numbered bout the withdrawal
//     left with no result of its own -- IV+1/PW+2 to the OTHER side from
//     decisionBy, same as every other surface (team_default_credit.jsx) --
//     on top of whatever WAS actually fought before the withdrawal landed;
//   - neither editor put the Kiken/Fus. result mark beside the withdrawn
//     side. It rides beside that side's name (WithdrawalMarkedName, from
//     sideMarks + withdrawnKeyOf), on the inner side, never in the centre.

import React from 'react';
import { render, act, screen, fireEvent } from '@testing-library/react';
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
    // bc-tmfn: bout 1 was actually fought (aka/Kyoto won it: IV 1, PW 1).
    // Bouts 2 and 3 (teamSize=3) carry no result of their own, so they are
    // credited to shiro/Osaka -- the OTHER side from decisionBy="aka" --
    // IV+1/PW+2 each: IV 2, PW 4.
    const stats = [...document.querySelectorAll('.team-summary__stats')].map((n) => n.textContent);
    expect(stats).toEqual(['IV: 2 · PW: 4', 'IV: 1 · PW: 1']);
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

// bc-cse: a match-level fusensho (the OTHER competitor already withdrew
// elsewhere, this match defaults to the opponent) is inside withdrawalInForce
// too, but sideMarks' fusensho arm puts "Fus." on the WINNER, not the loser --
// the opposite of kiken/fusenpai. WithdrawalMarkedName has to place whichever
// mark belongs to the rendered side, not always read .loser.
describe('a match-level fusensho marks the winner, not the barred competitor', () => {
  it('team editor: the credited team carries Fus., not the barred one', async () => {
    await mount(teamWithdrawal({
      decision: 'fusensho', decisionBy: 'aka',
      winner: { id: 'team-osaka', name: 'Osaka' },
    }));
    expect(screen.getByTestId('team-summary-result').textContent).toBe('SHIRO WIN');
    expectMarkBeside('shiro', 'Fus.');
  });

  it('individual editor: the credited competitor carries Fus., not the barred one', async () => {
    await mount(individualWithdrawal({
      decision: 'fusensho', decisionBy: 'shiro',
      winner: { id: 'p-aoki', name: 'Aoki Taro' }, ipponsA: [], ipponsB: [],
    }));
    expectMarkBeside('aka', 'Fus.');
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

// bc-tmfn: DecisionPrompt (admin_scoring_shared.jsx) has no slot for extra
// copy, so the consequence is stated beside it in admin_scoring_team.jsx
// instead -- generically (the credited TEAM's name isn't known until the
// operator picks a side inside the prompt's own radio), naming the concrete
// bout count.
describe('team editor: the withdrawal confirm states the default-win consequence', () => {
  it('names the bout count once a decision kind is picked', async () => {
    // Running, no decision yet: bout 1 fought, bouts 2-3 (teamSize=3) not.
    await mount(teamWithdrawal({ status: 'running', decision: '', decisionBy: '', winner: null, ipponsB: [] }));
    fireEvent.click(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
    const note = screen.getByTestId('decision-consequence-note');
    expect(note.textContent).toContain('2–0');
    expect(note.textContent).toContain('each of the 2 bouts');
  });

  // bc-cse: team_default_credit.jsx excludes kachinuki from the default-win
  // credit on purpose (bouts are appended one at a time; the encounter ends
  // on an explicit End match, never a match-level decision standing in for
  // unplayed slots), so this note must stay silent there even with unscored
  // bouts on the board -- it would otherwise promise a credit that never
  // lands.
  it('is silent on a kachinuki encounter, which the default-win credit excludes', async () => {
    await mount(teamWithdrawal({
      status: 'running', decision: '', decisionBy: '', winner: null, ipponsB: [],
      teamMatchType: 'kachinuki',
    }));
    fireEvent.click(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
    expect(screen.queryByTestId('decision-consequence-note')).toBeNull();
  });

  it('is silent once every bout already has a result', async () => {
    await mount(teamWithdrawal({
      status: 'running', decision: '', decisionBy: '', winner: null, ipponsB: [],
      subResults: [
        { position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Kyoto', decision: '' },
        { position: 2, sideA: '', sideB: '', ipponsA: [], ipponsB: ['K'], winner: 'Osaka', decision: '' },
        { position: 3, sideA: '', sideB: '', ipponsA: ['D'], ipponsB: [], winner: 'Kyoto', decision: '' },
      ],
    }));
    fireEvent.click(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
    expect(screen.queryByTestId('decision-consequence-note')).toBeNull();
  });
});
