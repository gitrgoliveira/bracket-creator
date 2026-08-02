// mp-gmcg: mistake recovery on a completed kachinuki team encounter.
//
// The operator-visible contract pinned here, all three parts of it:
//
//   1. REOPEN IS ONE TAP. The operator ended the match BY MISTAKE and is
//      standing at the shiaijo with the competitors still there. Nothing may
//      stand between the tap and the reopen — no prompt, no reason, no arm.
//   2. THE REASON IS COLLECTED ON THE WAY OUT. The write that completes a
//      reopened encounter carries a correctionReason (the server refuses it
//      otherwise), prompted for on [End match]. The requirement is driven by
//      the SERVER field m.reopenPending, never by local state: this editor
//      mounts per match, so an operator who reopens, walks off to another
//      court and comes back has no local memory of the reopen — exactly the
//      case the design exists to survive, and the case a local flag loses.
//   3. A BUSY COURT IS NOT A DEAD END. The court-busy 409 names the match
//      holding the court; the panel offers to send it back to the queue and
//      retry. That is DESTRUCTIVE (it clears that match's score), so the
//      consequence is on screen before the tap, and neither half of the
//      retry may strand the UI when it fails.

import React from 'react';
import { render, act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';

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
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const originals = {};
let ScoreEditorModal;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

beforeEach(() => {
  window.compMatches = () => [];
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    reopenMatch: vi.fn().mockResolvedValue(true),
    revertMatchToQueue: vi.fn().mockResolvedValue(true),
  };
});

// A COMPLETED kachinuki pool encounter: bout 1 was won by Aka, and the
// encounter was ended on it. This is the state the Reopen affordance exists
// for (ended too early / wrong result).
function completedKachinukiMatch(overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'completed',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    compFormat: 'mixed',
    teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    winner: 'Team A',
    subResults: [
      { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' },
    ],
    ...overrides,
  };
}

// The same encounter after a reopen: running again, bout log intact, and
// carrying the server's reopenPending stamp — the flag that makes the audit
// reason due on the next completing write.
function reopenedKachinukiMatch(overrides = {}) {
  return completedKachinukiMatch({
    status: 'running',
    winner: null,
    reopenPending: true,
    ...overrides,
  });
}

async function renderEditor(props = {}) {
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal
        match={completedKachinukiMatch()}
        onClose={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        password="secret"
        {...props}
      />
    );
  });
  return utils;
}

describe('kachinuki [Reopen match] is one tap', () => {
  it('posts immediately, with no reason and no prompt, and closes the editor', async () => {
    const onClose = vi.fn();
    await renderEditor({ onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    // No gate of any kind: the tap IS the reopen.
    expect(screen.queryByText('Reason for reopening')).toBeNull();
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
    // Three arguments: comp, match, password. A reason argument here would
    // mean the operator was asked for one.
    expect(window.API.reopenMatch).toHaveBeenCalledWith('comp1', 'm1', 'secret');
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('shows a plain-sentence 409 inline, verbatim, and stays open', async () => {
    const onClose = vi.fn();
    window.API.reopenMatch = vi.fn().mockRejectedValue(
      new Error('cannot reopen: a downstream knockout match has already been fought')
    );
    await renderEditor({ onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await waitFor(() => {
      expect(screen.getByTestId('kachinuki-reopen-error').textContent)
        .toBe('cannot reopen: a downstream knockout match has already been fought');
    });
    // Nothing was discarded, so the editor stays put.
    expect(onClose).not.toHaveBeenCalled();
    // Not a conflict: no remedy is offered for a downstream-fought match.
    expect(screen.queryByTestId('kachinuki-reopen-conflict')).toBeNull();
  });
});

describe('kachinuki reopen: the reason is collected on the way OUT', () => {
  it('[End match] on a reopened encounter prompts, then sends the reason as correctionReason', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await renderEditor({ match: reopenedKachinukiMatch(), onSubmit });

    // First tap opens the prompt INSTEAD of arming: its Confirm is the commit.
    fireEvent.click(screen.getByTestId('kachinuki-end-match-button'));
    expect(screen.getByText('Reason for reopening')).toBeTruthy();
    expect(onSubmit).not.toHaveBeenCalled();
    // The real reason is the default selection: an operator who ended by
    // mistake must not have to relabel it "Scoring error" or type it out.
    expect(screen.getByLabelText('Reason category').value).toBe('Ended by mistake');
    // The verdict being committed is shown: the prompt hides the End button
    // whose armed label would otherwise carry it.
    expect(screen.getByTestId('kachinuki-end-verdict').textContent).toContain('AKA WIN');

    // A free-text note so the asserted reason cannot be the default arriving
    // by accident.
    fireEvent.change(screen.getByLabelText('Reason note'), { target: { value: 'ended on the wrong bout' } });
    await act(async () => { fireEvent.click(screen.getByText('Confirm')); });

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.status).toBe('completed');
    expect(patch.correctionReason).toBe('Ended by mistake: ended on the wrong bout');
  });

  it('Cancel abandons the End write without submitting', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await renderEditor({ match: reopenedKachinukiMatch(), onSubmit });
    fireEvent.click(screen.getByTestId('kachinuki-end-match-button'));
    // Scoped to the prompt's own Cancel: while a reason prompt owns the footer
    // it must be the ONLY Cancel on screen, so this also pins that the
    // editor's nav row is hidden underneath it.
    const prompt = screen.getByText('Reason for reopening').closest('form');
    fireEvent.click(within(prompt).getByText('Cancel'));
    expect(onSubmit).not.toHaveBeenCalled();
    // Back to rest: End is available again for a second attempt.
    expect(screen.getByTestId('kachinuki-end-match-button')).toBeTruthy();
  });

  it('does not prompt on a running encounter that was never reopened', async () => {
    // The requirement rides the SERVER field. Without reopenPending the End
    // action keeps its ordinary arm/confirm and sends no correctionReason.
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await renderEditor({ match: reopenedKachinukiMatch({ reopenPending: false }), onSubmit });
    const end = screen.getByTestId('kachinuki-end-match-button');
    fireEvent.click(end);
    expect(screen.queryByText('Reason for reopening')).toBeNull();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-end-match-button')); });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit.mock.calls[0][0].correctionReason).toBeUndefined();
  });
});

describe('kachinuki reopen: a busy court gets a remedy, not a dead end', () => {
  function courtBusyError() {
    const e = new Error('Court A already has a running match (m-r1-1). Finish that match before reopening this one.');
    e.code = 'court_busy';
    e.court = 'A';
    e.matchId = 'm-r1-1';
    e.compId = 'comp1';
    return e;
  }

  beforeEach(() => {
    // The blocking match, so the panel can name the competitors the operator
    // is about to wipe a score from.
    window.compMatches = () => [
      { id: 'm-r1-1', sideA: { name: 'Team C' }, sideB: { name: 'Team D' } },
    ];
  });

  it('names the blocking match and states the destructive consequence before the action', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValue(courtBusyError());
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });

    const panel = await screen.findByTestId('kachinuki-reopen-conflict');
    // The blocking match, by court and by competitors: acting on an opaque
    // match id alone is how the wrong score gets wiped.
    await waitFor(() => expect(panel.textContent).toContain('Team D vs Team C'));
    expect(panel.textContent).toContain('Shiaijo A');
    // The warning is on screen, in words, BEFORE the operator commits.
    expect(screen.getByTestId('kachinuki-reopen-conflict-warning').textContent)
      .toContain('clears any score already entered for it');
    // The server's own sentence is not swallowed by our friendlier heading.
    expect(panel.textContent).toContain('Court A already has a running match (m-r1-1).');
    expect(screen.getByTestId('kachinuki-reopen-requeue-button')).toBeTruthy();
  });

  it('requeues the blocking match and retries the reopen', async () => {
    const onClose = vi.fn();
    window.API.reopenMatch = vi.fn()
      .mockRejectedValueOnce(courtBusyError())
      .mockResolvedValueOnce(true);
    await renderEditor({ onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await screen.findByTestId('kachinuki-reopen-conflict');

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-requeue-button')); });

    // The BLOCKING match is requeued (not this one), then the reopen retries.
    expect(window.API.revertMatchToQueue).toHaveBeenCalledWith('comp1', 'm-r1-1', 'secret');
    await waitFor(() => expect(window.API.reopenMatch).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('keeps the panel and explains itself when the requeue is refused', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValue(courtBusyError());
    window.API.revertMatchToQueue = vi.fn().mockRejectedValue(new Error('match already completed'));
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await screen.findByTestId('kachinuki-reopen-conflict');

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-requeue-button')); });

    await waitFor(() => {
      expect(screen.getByTestId('kachinuki-reopen-error').textContent).toContain('match already completed');
    });
    // The blocker is still there to clear, so the remedy stays offered and the
    // button comes back to rest rather than sticking on "Working…".
    expect(screen.getByTestId('kachinuki-reopen-conflict')).toBeTruthy();
    expect(screen.getByTestId('kachinuki-reopen-requeue-button').textContent).toBe('Clear its score, queue it, and reopen');
    // Only the first attempt reached the reopen endpoint.
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
  });

  it('recovers when the requeue lands but the retried reopen still fails', async () => {
    const onClose = vi.fn();
    window.API.reopenMatch = vi.fn()
      .mockRejectedValueOnce(courtBusyError())
      .mockRejectedValueOnce(new Error('cannot reopen: a downstream knockout match has already been fought'));
    await renderEditor({ onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await screen.findByTestId('kachinuki-reopen-conflict');

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-requeue-button')); });

    // Not stuck: the second failure reaches the operator, the (now irrelevant)
    // conflict panel is gone, and Reopen is tappable again.
    await waitFor(() => {
      expect(screen.getByTestId('kachinuki-reopen-error').textContent)
        .toBe('cannot reopen: a downstream knockout match has already been fought');
    });
    expect(screen.queryByTestId('kachinuki-reopen-conflict')).toBeNull();
    expect(screen.getByTestId('kachinuki-reopen-button').textContent).toBe('Reopen match');
    expect(onClose).not.toHaveBeenCalled();
  });
});
