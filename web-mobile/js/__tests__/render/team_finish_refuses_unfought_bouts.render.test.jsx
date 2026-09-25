// bc-tmfn: Finish on a team match refuses while a numbered bout has no result
// (operator ruling 2026-09-24: every bout of a team match is fought). The
// refusal names the bouts in visible text, in the server's words, and sends
// nothing. Corrections are not exempt, except a correction to a match a
// withdrawal ended, which keeps that ruling (removing one is a reopen, pinned
// in recorded_withdrawal_reopen.render.test.jsx). Pure-helper cases live in
// unfinished_team_bouts.test.jsx; the server half in team_finish_gate_test.go.

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

beforeEach(() => { window.API.recordScore.mockClear(); });

const FOUGHT_BOUT_1 = { position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Kyoto', decision: '' };

function makeMatch(overrides = {}) {
  return {
    id: 'm-pool-1',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto' },
    sideB: { id: 'team-osaka', name: 'Osaka' },
    subResults: [FOUGHT_BOUT_1],
    ...overrides,
  };
}

async function mount(match) {
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />);
  });
}

const primaryButton = (re) => [...document.querySelectorAll('button')].find((b) => re.test(b.textContent || ''));

describe('team editor: Finish refuses while a bout has no result', () => {
  it('names the unfought bouts, does not arm, and sends nothing', async () => {
    await mount(makeMatch());
    expect(screen.queryByTestId('team-finish-unfinished-bouts')).toBeNull();

    await act(async () => { fireEvent.click(screen.getByText('Finish')); });

    const refusal = screen.getByTestId('team-finish-unfinished-bouts');
    expect(refusal.textContent).toBe('Bout 2 and Bout 3 have no result. Record a score, a Tie, or a Fusensho before finishing.');
    expect(refusal.textContent).not.toMatch(/lineup/i);
    expect(screen.queryByText('Tap again to finish')).toBeNull();
    expect(window.API.recordScore.mock.calls.filter(([, , p]) => p?.status === 'completed')).toHaveLength(0);
  });

  it('finishes once every bout has a result, a Tie included', async () => {
    await mount(makeMatch());
    await act(async () => { fireEvent.click(screen.getByText('Finish')); });
    expect(screen.getByTestId('team-finish-unfinished-bouts')).toBeTruthy();

    // Tie bouts 2 and 3 (one Tie button per numbered row, in bout order).
    // The refusal follows the board live.
    const ties = () => [...document.querySelectorAll('[data-testid="scoring-modal-tie-button"]')];
    expect(ties()).toHaveLength(3);
    await act(async () => { fireEvent.click(ties()[1]); });
    expect(screen.getByTestId('team-finish-unfinished-bouts').textContent).toMatch(/^Bout 3 has no result\./);
    await act(async () => { fireEvent.click(ties()[2]); });
    expect(screen.queryByTestId('team-finish-unfinished-bouts')).toBeNull();

    await act(async () => { fireEvent.click(screen.getByText('Finish')); });
    await act(async () => { fireEvent.click(screen.getByText('Tap again to finish')); });
    const completed = window.API.recordScore.mock.calls.filter(([, , p]) => p?.status === 'completed');
    expect(completed).toHaveLength(1);
    const numbered = completed[0][2].subResults.filter((s) => s.position > 0);
    expect(numbered.map((s) => s.decision)).toEqual(['', 'hikiwake', 'hikiwake']);
  });

  it('refuses a correction that would leave a bout without a result', async () => {
    await mount(makeMatch({ status: 'completed', winner: { id: 'team-kyoto', name: 'Kyoto' } }));
    const save = primaryButton(/Save correction/);
    expect(save).toBeTruthy();
    await act(async () => { fireEvent.click(save); });
    expect(screen.getByTestId('team-finish-unfinished-bouts').textContent)
      .toBe('Bout 2 and Bout 3 have no result. Record a score, a Tie, or a Fusensho before finishing.');
    expect(window.API.recordScore.mock.calls.filter(([, , p]) => p?.status === 'completed')).toHaveLength(0);
  });

  // Operator ruling 2026-09-24: "Save correction should just save what the
  // operator enters." A correction to a match a withdrawal ended keeps that
  // ruling on the server (engine.KeepsWithdrawalRuling), so the bouts nobody
  // fought after it do not refuse the save, and the write restates nothing
  // about the withdrawal.
  it.each(['kiken-voluntary', 'kiken-injury', 'kiken', 'fusenpai'])('saves a correction over a recorded %s without refusing or restating it', async (decision) => {
    await mount(makeMatch({
      status: 'completed', decision, decisionBy: 'aka', decisionReason: 'knee',
      winner: { id: 'team-osaka', name: 'Osaka' }, ipponsB: ['○', '○'],
    }));
    // Recovery path: a withdrawal recorded against the wrong team is re-decided
    // from the same editor, so the panel stays offered on the correction.
    expect(screen.getByTestId('scoring-modal-kiken-voluntary-button')).toBeTruthy();
    await act(async () => { fireEvent.click(primaryButton(/Save correction/)); });
    const confirm = primaryButton(/Tap again|Confirm/);
    if (confirm) await act(async () => { fireEvent.click(confirm); });
    expect(screen.queryByTestId('team-finish-unfinished-bouts')).toBeNull();
    const completed = window.API.recordScore.mock.calls.filter(([, , p]) => p?.status === 'completed');
    expect(completed).toHaveLength(1);
    const patch = completed[0][2];
    expect(patch.decision).toBeUndefined();
    expect(patch.decisionBy).toBeUndefined();
    expect(patch.decisionReason).toBeUndefined();
    expect(patch.subResults.filter((s) => s.position > 0)[0].ipponsA).toEqual(['M']);
    // The bouts favour Kyoto, the side that withdrew; the write names the
    // ruling's winner so no local bracket advance can move Kyoto on, and it
    // is not a draw (score.type maps to decision "hikiwake").
    expect(patch.winner?.name ?? patch.winner).toBe('Osaka');
    expect(patch.score.type).toBe('ippon');
  });

  it('sends an untouched bout as no result, not as a Tie', async () => {
    await mount(makeMatch({ status: 'scheduled', subResults: [] }));
    await act(async () => { fireEvent.click(primaryButton(/Start/)); });
    const running = window.API.recordScore.mock.calls.map(([, , p]) => p).find((p) => p?.status === 'running');
    expect(running).toBeTruthy();
    const numbered = running.subResults.filter((s) => s.position > 0);
    expect(numbered).toHaveLength(3);
    for (const s of numbered) expect(s.decision).toBe('');
  });
});
