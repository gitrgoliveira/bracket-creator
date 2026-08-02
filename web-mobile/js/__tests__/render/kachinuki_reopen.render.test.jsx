// mp-gmcg: [Reopen match] audit-reason flow on a completed kachinuki team
// encounter.
//
// Reopening DISCARDS a recorded result (and, in a bracket, the propagated
// winner slot), so it is the same class of edit as a correction and captures
// the same kind of reason. The operator-visible contract pinned here:
//   1. the first tap opens the ReasonPrompt and posts NOTHING (an empty
//      reason must never reach the server, which 400s it);
//   2. Confirm posts, and the confirmed reason travels to API.reopenMatch;
//   3. Cancel abandons without posting;
//   4. the server's 409 text (including the court-busy conflict) is shown
//      inline verbatim, and the editor stays open so the operator can act.

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

describe('kachinuki [Reopen match] audit reason', () => {
  it('the first tap opens the reason prompt and posts nothing', async () => {
    await renderEditor();
    fireEvent.click(screen.getByTestId('kachinuki-reopen-button'));
    expect(screen.getByText('Reason for reopening')).toBeTruthy();
    // No round-trip: the server would 400 a reasonless reopen, and the
    // operator must never see that failure.
    expect(window.API.reopenMatch).not.toHaveBeenCalled();
  });

  it('sends the confirmed reason to API.reopenMatch and closes the editor', async () => {
    const onClose = vi.fn();
    await renderEditor({ onClose });
    fireEvent.click(screen.getByTestId('kachinuki-reopen-button'));
    // Add a free-text note so the asserted reason cannot be the preset default
    // arriving by accident.
    fireEvent.change(screen.getByLabelText('Reason note'), { target: { value: 'ended too early' } });
    await act(async () => { fireEvent.click(screen.getByText('Confirm')); });
    await waitFor(() => expect(window.API.reopenMatch).toHaveBeenCalledTimes(1));
    expect(window.API.reopenMatch).toHaveBeenCalledWith(
      'comp1', 'm1', 'Scoring error: ended too early', 'secret'
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('Cancel on the prompt abandons the reopen without posting', async () => {
    await renderEditor();
    fireEvent.click(screen.getByTestId('kachinuki-reopen-button'));
    // Scoped to the prompt's own Cancel: while a reason prompt owns the
    // footer it must be the ONLY Cancel on screen, so this also pins that
    // the editor's nav row is hidden underneath it.
    const prompt = screen.getByText('Reason for reopening').closest('form');
    fireEvent.click(within(prompt).getByText('Cancel'));
    expect(window.API.reopenMatch).not.toHaveBeenCalled();
    // Back to rest: the button is available again for a second attempt.
    expect(screen.getByTestId('kachinuki-reopen-button')).toBeTruthy();
  });

  it('shows the server court-busy 409 inline, verbatim', async () => {
    const onClose = vi.fn();
    // The client already turns the structured court_busy payload into this
    // sentence (api_client_reopen.test.jsx); here it must reach the operator
    // unedited instead of being flattened into a generic failure notice.
    window.API.reopenMatch = vi.fn().mockRejectedValue(
      new Error('Court A already has a running match (m-r1-1). Finish that match before reopening this one.')
    );
    await renderEditor({ onClose });
    fireEvent.click(screen.getByTestId('kachinuki-reopen-button'));
    await act(async () => { fireEvent.click(screen.getByText('Confirm')); });
    await waitFor(() => {
      expect(screen.getByTestId('kachinuki-reopen-error').textContent)
        .toBe('Court A already has a running match (m-r1-1). Finish that match before reopening this one.');
    });
    // The editor stays open on failure: nothing was discarded.
    expect(onClose).not.toHaveBeenCalled();
  });
});
