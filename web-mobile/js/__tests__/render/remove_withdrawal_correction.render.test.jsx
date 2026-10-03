// Remove withdrawal (operator ruling 2026-10-03: "An operator or admin can fix
// a mistake, but the fix must leave the match resolved"). A withdrawal or
// default win recorded by mistake on a finished match is removed in ONE save:
// Remove withdrawal in the recorded box (RecordedWithdrawal,
// admin_scoring_shared.jsx), enter the result as it was fought, Save
// correction. The match goes finished -> finished and never takes the court;
// the write carries clearWithdrawal so the server replaces the ruling instead
// of keeping it (engine.KeepsWithdrawalRuling). Clear withdrawal and reopen
// stays beside it for a match with fighting left. Kachinuki has no Save
// correction, so it is not offered there.

import React from 'react';
import { render, act, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { attemptScoreWrite } from '../../write_result.jsx';

const STUBBED_GLOBALS = {
  isHikiwake: (t) => t === 'hikiwake',
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  poolLabel: (m) => m.poolName || 'Pool',
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
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    reopenMatch: vi.fn().mockResolvedValue({ reopenedMatches: [] }),
    requeueBlockerAndReopen: vi.fn().mockResolvedValue({ reopenedMatches: [] }),
  };
});

// Yamada (aka) was given the default win when Tanaka (shiro) withdrew holding
// a men: the winner's points were replaced by the default-win maru.
function individualKiken(overrides = {}) {
  return {
    id: 'm-r1-0', compId: 'comp1', status: 'completed', phase: 'bracket', round: 'Round 1', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ipponsA: ['○', '○'], ipponsB: ['M'], hansokuA: 0, hansokuB: 0,
    decision: 'kiken-voluntary', decisionBy: 'shiro',
    winner: { id: 'p1', name: 'Yamada' },
    ...overrides,
  };
}

// A fixed-order team pool match Kyoto (aka) "withdrew" from after bout 1;
// bouts 2 and 3 were credited to Osaka and carry no result of their own.
function teamKiken(overrides = {}) {
  return {
    id: 'm-pool-1', compId: 'comp1', status: 'completed', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto' },
    sideB: { id: 'team-osaka', name: 'Osaka' },
    subResults: [{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Kyoto', decision: '' }],
    decision: 'kiken-voluntary', decisionBy: 'aka', decisionReason: 'knee',
    winner: { id: 'team-osaka', name: 'Osaka' }, ipponsB: ['○', '○'],
    ...overrides,
  };
}

async function mount(match, props = {}) {
  let view;
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret" {...props} />);
  });
  return view;
}

const slots = (side) => [...document.querySelectorAll(`.sb-slots--${side} .sb-slot`)];
const filled = (side) => slots(side).map((b) => b.textContent).filter((t) => t !== '·');
const addButton = (side, letter) => [...document.querySelectorAll(`.sb-side--${side} .ipt-btn`)].find((b) => b.textContent === letter);
const navButton = (label) => [...document.querySelectorAll('.score-nav button')].find((b) => b.textContent === label);

async function tap(el) { await act(async () => { fireEvent.click(el); }); }

// Save correction, then the reason prompt's Confirm (a correction to a
// finished match always carries a reason).
async function saveCorrection() {
  await tap(navButton('Save correction'));
  const confirm = [...document.querySelectorAll('.reason-prompt button')].find((b) => b.textContent === 'Confirm');
  await tap(confirm);
}

describe('individual editor: Remove withdrawal', () => {
  it('sits beside Clear withdrawal and reopen and says what it does', async () => {
    await mount(individualKiken());
    expect(screen.getByTestId('clear-withdrawal-reopen').textContent).toBe('Clear withdrawal and reopen');
    expect(screen.getByTestId('remove-withdrawal').textContent).toBe('Remove withdrawal');
    expect(screen.getByTestId('remove-withdrawal-consequence').textContent.replace(/\s+/g, ' ')).toBe(
      'Or remove it: the match stays finished, you enter the result as it was fought and save the correction. Tanaka can compete again.');
  });

  it("unlocks the winner's side, then Save correction sends the real result with clearWithdrawal", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(individualKiken(), { onSubmit });
    slots('aka').forEach((b) => expect(b.disabled).toBe(true));

    await tap(screen.getByTestId('remove-withdrawal'));
    // The maru is gone, the withdrawer's men stays, and both sides take entry.
    expect(filled('aka')).toEqual([]);
    expect(filled('shiro')).toEqual(['M']);
    expect(addButton('aka', 'M').disabled).toBe(false);
    expect(screen.getByTestId('remove-withdrawal-pending').textContent).toBe('The withdrawal will be removed when you save the correction.');
    expect(screen.getByTestId('remove-withdrawal-undo')).toBeTruthy();
    expect(screen.queryByTestId('clear-withdrawal-reopen')).toBeNull();
    expect(window.API.reopenMatch).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();

    // As it was fought: Yamada took men and kote, Tanaka's men stands.
    await tap(addButton('aka', 'M'));
    await tap(addButton('aka', 'K'));
    // Aka's slots are mirrored on screen, so compare without order.
    expect(filled('aka').sort()).toEqual(['K', 'M']);

    await saveCorrection();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.clearWithdrawal).toBe(true);
    expect(patch.status).toBe('completed');
    expect(patch.winner?.name).toBe('Yamada');
    expect(patch.ipponsA).toEqual(['M', 'K']);
    expect(patch.ipponsB).toEqual(['M']);
    expect(patch.decision).toBeUndefined();
    expect(patch.correctionReason).toBeTruthy();
  });

  it('a draw is a real result too (pool match): sent as hikiwake with clearWithdrawal', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(individualKiken({ phase: 'pool', ipponsB: [] }), { onSubmit });
    await tap(screen.getByTestId('remove-withdrawal'));
    await tap(screen.getByTestId('scoring-modal-mark-draw'));
    await saveCorrection();
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.clearWithdrawal).toBe(true);
    expect(patch.score.type).toBe('hikiwake');
    expect(patch.winner).toBeNull();
  });

  // The name stops carrying the ruling the board no longer shows: a Kiken
  // beside Tanaka under "will be removed" read as two answers at once.
  it("the withdrawn competitor's Kiken mark goes with the removal and comes back on Undo", async () => {
    await mount(individualKiken());
    expect(screen.getByTestId('withdrawal-mark-shiro').textContent).toBe('Kiken');
    await tap(screen.getByTestId('remove-withdrawal'));
    expect(screen.queryByTestId('withdrawal-mark-shiro')).toBeNull();
    expect(screen.queryByTestId('withdrawal-mark-aka')).toBeNull();
    await tap(screen.getByTestId('remove-withdrawal-undo'));
    expect(screen.getByTestId('withdrawal-mark-shiro').textContent).toBe('Kiken');
  });

  // A judges' decision is a real result too: decided after Remove, it goes
  // out through its own write (submitHantei), which must clear the ruling
  // exactly as Save correction does.
  it('a hantei decided after Remove replaces the ruling as well', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(individualKiken(), { onSubmit });
    await tap(screen.getByTestId('remove-withdrawal'));
    await tap(addButton('aka', 'K'));
    await tap(screen.getByTestId('scoring-modal-hantei-arm'));
    await tap(screen.getByTestId('scoring-modal-hantei-aka'));
    const confirm = [...document.querySelectorAll('.reason-prompt button')].find((b) => b.textContent === 'Confirm');
    await tap(confirm);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.decidedByHantei).toBe(true);
    expect(patch.winner?.name).toBe('Yamada');
    expect(patch.clearWithdrawal).toBe(true);
    expect(patch.correctionReason).toBeTruthy();
  });

  it('Undo puts the recorded result back, maru and locks included', async () => {
    await mount(individualKiken());
    await tap(screen.getByTestId('remove-withdrawal'));
    await tap(addButton('aka', 'D'));
    await tap(screen.getByTestId('remove-withdrawal-undo'));
    expect(filled('aka')).toEqual(['○', '○']);
    slots('aka').forEach((b) => expect(b.disabled).toBe(true));
    expect(addButton('aka', 'M').disabled).toBe(true);
    expect(screen.queryByTestId('remove-withdrawal-pending')).toBeNull();
    expect(screen.getByTestId('clear-withdrawal-reopen')).toBeTruthy();
  });

  it('a plain Save correction without Remove still keeps the withdrawal (no clearWithdrawal)', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(individualKiken(), { onSubmit });
    await tap(slots('shiro').find((b) => b.textContent === 'M'));
    await tap(addButton('shiro', 'K'));
    await saveCorrection();
    const patch = onSubmit.mock.calls[0][0];
    expect(patch).not.toHaveProperty('clearWithdrawal');
    expect(patch.ipponsA).toEqual(['○', '○']);
  });

  it('removing is an unsaved change: closing asks before discarding it', async () => {
    const onClose = vi.fn();
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    // The winner's cells hold no maru (an older record, shown with the
    // display fallback), so removing changes no point on the board: the
    // removal alone must count as unsaved.
    await mount(individualKiken({ ipponsA: [] }), { onClose });
    await tap(screen.getByTestId('remove-withdrawal'));
    await tap(navButton('Cancel'));
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message).toBe('Discard unsaved scoring changes?');
    expect(onClose).not.toHaveBeenCalled();
  });

  // The host's chokepoint (admin.jsx editMatchScore) routes every score write
  // through attemptScoreWrite: a knockout correction that changes who advanced
  // into a match already fought is refused, confirmed, and resent with force.
  // The resend must still be the removal, or the forced write would keep the
  // withdrawal it was meant to replace.
  it('a knockout correction refused for a later match already fought is resent with force and still clears', async () => {
    const recordScore = vi.fn()
      .mockRejectedValueOnce(Object.assign(new Error('refused'), {
        downstreamKnockoutPlayed: { matchId: 'm-r1-0', blockingMatchId: 'm-r2-0', blockingMatches: [{ id: 'm-r2-0', number: 2 }], displaced: 'Yamada' },
      }))
      .mockResolvedValueOnce({ applied: true });
    const confirmDialog = vi.fn().mockResolvedValue(true);
    const match = individualKiken();
    const onSubmit = vi.fn((result) => attemptScoreWrite({
      recordScore, confirmDialog, compId: 'comp1', matchId: match.id, result, password: 'secret', match,
    }));
    await mount(match, { onSubmit });
    await tap(screen.getByTestId('remove-withdrawal'));
    // Tanaka actually won: men and kote against Yamada's nothing.
    await tap(addButton('shiro', 'K'));
    await saveCorrection();
    await waitFor(() => expect(recordScore).toHaveBeenCalledTimes(2));
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    const [first, forced] = recordScore.mock.calls.map((c) => c[2]);
    expect(first.clearWithdrawal).toBe(true);
    expect(first.forceDownstreamReopen).toBeUndefined();
    expect(forced.clearWithdrawal).toBe(true);
    expect(forced.forceDownstreamReopen).toBe(true);
    expect(forced.winner?.name).toBe('Tanaka');
  });

  // A match-level fusensho whose barred competitor the server already reads
  // eligible again: the reopen copy says they can fight again, so the removal
  // sentence says the same outcome rather than nothing (one eligibility answer
  // in RecordedWithdrawal, read by both).
  it('a default win over a competitor already eligible again says they can compete again, as the reopen does', async () => {
    const fusensho = individualKiken({ decision: 'fusensho', ipponsB: [], withdrawnStatus: { eligible: true } });
    await mount(fusensho);
    await waitFor(() => expect(screen.getByTestId('remove-withdrawal').textContent).toBe('Remove default win'));
    expect(screen.getByTestId('clear-withdrawal-consequence').textContent).toContain('Tanaka can fight again');
    expect(screen.getByTestId('remove-withdrawal-consequence').textContent.replace(/\s+/g, ' ')).toBe(
      'Or remove it: the match stays finished, you enter the result as it was fought and save the correction. Tanaka can compete again.');
    await tap(screen.getByTestId('remove-withdrawal'));
    expect(screen.getByTestId('remove-withdrawal-consequence').textContent.replace(/\s+/g, ' ')).toBe(
      'Enter the result as it was fought, then save the correction. The match stays finished. Tanaka can compete again.');
  });

  it('a competitor barred by another match is not promised they can compete again', async () => {
    await mount(individualKiken({ decision: 'fusenpai', ipponsB: [], withdrawnStatus: { eligible: false, matchId: 'Pool A-0' } }));
    await waitFor(() => expect(screen.getByTestId('remove-withdrawal').textContent).toBe('Remove default win'));
    const text = screen.getByTestId('remove-withdrawal-consequence').textContent;
    expect(text).toContain('Tanaka stays withdrawn because of another match.');
    expect(text).not.toContain('can compete again');
  });
});

describe('team editor: Remove withdrawal', () => {
  const tieButtons = () => screen.getAllByTestId('scoring-modal-tie-button');

  it('sits beside Clear withdrawal and reopen', async () => {
    await mount(teamKiken());
    expect(screen.getByTestId('clear-withdrawal-reopen')).toBeTruthy();
    expect(screen.getByTestId('remove-withdrawal').textContent).toBe('Remove withdrawal');
    expect(screen.getByTestId('team-summary-decision')).toBeTruthy();
  });

  it('the credited bouts become rows to fill: Save correction is refused until they have a result', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(teamKiken(), { onSubmit });
    await tap(screen.getByTestId('remove-withdrawal'));
    // The band reads the bouts again, not the recorded ruling.
    expect(screen.queryByTestId('team-summary-decision')).toBeNull();
    await tap(navButton('Save correction'));
    expect(screen.getByTestId('team-finish-unfinished-bouts').textContent).toMatch(/Bouts? 2/);
    expect(document.querySelector('.reason-prompt')).toBeNull();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('with every bout given a result, Save correction sends the bouts\' result with clearWithdrawal', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(teamKiken(), { onSubmit });
    await tap(screen.getByTestId('remove-withdrawal'));
    // Bouts 2 and 3 were drawn.
    await tap(tieButtons()[1]);
    await tap(tieButtons()[2]);
    await saveCorrection();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.clearWithdrawal).toBe(true);
    expect(patch.status).toBe('completed');
    // Kyoto won bout 1 and the others were drawn: Kyoto wins on the bouts.
    expect(patch.winner?.name).toBe('Kyoto');
    expect(patch.subResults.map((s) => s.decision)).toEqual(['', 'hikiwake', 'hikiwake']);
  });

  it('Undo restores the recorded state; a plain Save correction keeps the withdrawal', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(teamKiken(), { onSubmit });
    await tap(screen.getByTestId('remove-withdrawal'));
    await tap(tieButtons()[1]);
    await tap(screen.getByTestId('remove-withdrawal-undo'));
    expect(screen.getByTestId('team-summary-decision')).toBeTruthy();
    expect(screen.getByTestId('clear-withdrawal-reopen')).toBeTruthy();
    await saveCorrection();
    const patch = onSubmit.mock.calls[0][0];
    expect(patch).not.toHaveProperty('clearWithdrawal');
    expect(patch.winner?.name).toBe('Osaka');
    // The tie made before Undo went with it.
    expect(patch.subResults.map((s) => s.decision)).toEqual(['', '', '']);
  });

  it("the withdrawn team's Kiken mark goes with the removal and comes back on Undo", async () => {
    await mount(teamKiken());
    expect(screen.getByTestId('withdrawal-mark-aka').textContent).toBe('Kiken');
    await tap(screen.getByTestId('remove-withdrawal'));
    expect(screen.queryByTestId('withdrawal-mark-aka')).toBeNull();
    expect(screen.queryByTestId('withdrawal-mark-shiro')).toBeNull();
    await tap(screen.getByTestId('remove-withdrawal-undo'));
    expect(screen.getByTestId('withdrawal-mark-aka').textContent).toBe('Kiken');
  });

  it('removing is an unsaved change: closing asks before discarding it', async () => {
    const onClose = vi.fn();
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await mount(teamKiken(), { onClose });
    await tap(screen.getByTestId('remove-withdrawal'));
    await tap(navButton('Cancel'));
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  // A knockout encounter whose only fought bout was drawn: the recorded
  // withdrawal decides it, so Save correction is open. Removing it hands the
  // result to the bouts, which tie, and a knockout cannot end tied: the
  // button and the tie block read the same input, so the button names the
  // block ("Needs a winner") instead of offering a Save correction it would
  // refuse.
  it('in a knockout, a removal whose bouts tie reads Needs a winner, not Save correction', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await mount(teamKiken({
      phase: 'bracket', round: 'Round 1', poolName: undefined,
      subResults: [{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake' }],
    }), { onSubmit });
    const saveButton = () => [...document.querySelectorAll('.score-nav button.btn--primary')][0];
    expect(saveButton().textContent).toBe('Save correction');
    expect(saveButton().disabled).toBe(false);

    await tap(screen.getByTestId('remove-withdrawal'));
    expect(saveButton().textContent).toBe('Needs a winner');
    expect(saveButton().disabled).toBe(true);

    // A point in bout 2 breaks the tie: the save is a correction again.
    const bout2Buttons = document.querySelectorAll('.team-sub-match__btns')[2];
    await tap([...bout2Buttons.querySelectorAll('.ipt-btn')].find((b) => b.textContent === 'M'));
    expect(saveButton().textContent).toBe('Save correction');
    expect(saveButton().disabled).toBe(false);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  // A finished kachinuki encounter has no Save correction (its result is the
  // last bout's, through End match), so the one-save fix has no write to
  // ride on: only Clear withdrawal and reopen is offered there.
  it('is not offered on a kachinuki encounter', async () => {
    await mount(teamKiken({ teamMatchType: 'kachinuki' }));
    expect(screen.getByTestId('clear-withdrawal-reopen')).toBeTruthy();
    expect(screen.queryByTestId('remove-withdrawal')).toBeNull();
    expect(screen.queryByTestId('remove-withdrawal-consequence')).toBeNull();
  });
});

// ONE reset rule, in useWithdrawalRemoval (admin_scoring_shared.jsx): a
// removal belongs to the match and the ruling it was made against, so a peer
// re-recording the ruling (here a different decision on the SAME side, which
// leaves the individual board's locked side where it was) ends a pending
// removal in BOTH editors and the ruling shows again. The individual board
// re-seeds (its winner side would otherwise lock over the operator's letters);
// the team editor keeps its bout edits, because an edit in progress survives a
// verdict adopted from another device, and saves them as a plain correction.
describe('a ruling re-recorded under a pending removal ends it in both editors', () => {
  async function rerender(view, match) {
    await act(async () => {
      view.rerender(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret" />);
    });
  }

  it('individual: the maru and the lock come back', async () => {
    const view = await mount(individualKiken());
    await tap(screen.getByTestId('remove-withdrawal'));
    await tap(addButton('aka', 'D'));
    expect(screen.getByTestId('remove-withdrawal-pending')).toBeTruthy();

    await rerender(view, individualKiken({ decision: 'kiken-injury' }));
    expect(screen.queryByTestId('remove-withdrawal-pending')).toBeNull();
    expect(screen.getByTestId('remove-withdrawal')).toBeTruthy();
    expect(filled('aka')).toEqual(['○', '○']);
    slots('aka').forEach((b) => expect(b.disabled).toBe(true));
    expect(screen.getByTestId('withdrawal-mark-shiro').textContent).toBe('Kiken');
  });

  it('team: the recorded ruling comes back, the bout edit stays', async () => {
    const view = await mount(teamKiken());
    await tap(screen.getByTestId('remove-withdrawal'));
    await tap(screen.getAllByTestId('scoring-modal-tie-button')[1]);
    expect(screen.queryByTestId('team-summary-decision')).toBeNull();

    await rerender(view, teamKiken({ decision: 'kiken-injury' }));
    expect(screen.queryByTestId('remove-withdrawal-pending')).toBeNull();
    expect(screen.getByTestId('team-summary-decision')).toBeTruthy();
    expect(screen.getByTestId('withdrawal-mark-aka').textContent).toBe('Kiken');
    // The tie made during the removal is kept; the save no longer clears the
    // ruling.
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await act(async () => {
      view.rerender(<ScoreEditorModal match={teamKiken({ decision: 'kiken-injury' })} onClose={vi.fn()} onSubmit={onSubmit} password="secret" />);
    });
    await saveCorrection();
    const patch = onSubmit.mock.calls[0][0];
    expect(patch).not.toHaveProperty('clearWithdrawal');
    expect(patch.subResults.map((s) => s.decision)).toEqual(['', 'hikiwake', '']);
  });
});
