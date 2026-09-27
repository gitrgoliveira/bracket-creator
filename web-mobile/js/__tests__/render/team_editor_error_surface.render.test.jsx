// editorErr is the team editor's ONE inline error surface: the daihyosen
// add/remove POSTs and the inline lineup-position PUT all report through it.
//
// It used to be rendered INSIDE the "add a representative bout" block, whose
// guard is `if (hasDaihyosen || !isKnockoutPhase || isKachinuki) return null`.
// Every state that can SET the error from somewhere else therefore hid it:
// removing an existing daihyosen (hasDaihyosen is true, so the block that
// would have shown the failure is gone) and a failed lineup save in the
// kachinuki flow (isKachinuki is true). The operator saw the button return to
// rest and nothing else — a silent failure on a write they believe landed.
//
// Pinned here on the remove-daihyosen path because it needs no lineup wiring:
// the assertion is simply that the failure REACHES THE SCREEN.

import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
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
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    reopenMatch: vi.fn(),
  };
});

// A running knockout team encounter that already carries an UNSCORED
// daihyosen (wire position -1): the state the [Remove daihyosen] affordance
// exists for, and the state in which the old render site was unreachable.
function matchWithDaihyosen(overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: 'bracket',
    round: 'Final',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    compFormat: 'knockout',
    teamMatchType: 'fixed',
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    subResults: [
      { position: 1, sideA: 'Team A', sideB: 'Team B', ipponsA: ['M'], ipponsB: ['K'], winner: '' },
      { position: -1, sideA: 'Team A', sideB: 'Team B', ipponsA: [], ipponsB: [], winner: '', decision: 'daihyosen' },
    ],
    ...overrides,
  };
}

async function renderEditor(props = {}) {
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal
        match={matchWithDaihyosen()}
        onClose={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        password="secret"
        {...props}
      />
    );
  });
  return utils;
}


// Arms the debounce on a NUMBERED bout (never the daihyosen row itself) so
// onRemoveDaihyosen's own pre-save condition (hadPending || isDirty) is true.
// Same selectors as the bc-dhas block in autosave_debounce.render.test.jsx.
function subMatchRows() { return [...document.querySelectorAll('.team-sub-match')]; }
function ipponButton(rowEl, color, letter) {
  return [...rowEl.querySelectorAll(`.team-sub-match__side--${color} button.ipt-btn`)].find((b) => b.textContent === letter);
}

describe('team editor inline error surface', () => {
  it('shows a failed remove-daihyosen error on screen (it is not swallowed by the add-daihyosen guard)', async () => {
    window.API.removeDaihyosen = vi.fn().mockRejectedValue(new Error('daihyosen_scored'));
    const onClose = vi.fn();
    await renderEditor({ onClose });
    // Precondition: the ADD block is absent in this state, so the error can
    // only appear if it renders independently of it.
    expect(screen.queryByTestId('scoring-modal-daihyosen-button')).toBeNull();

    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });

    await waitFor(() => {
      expect(screen.getByTestId('team-editor-error').textContent)
        .toBe('Clear the daihyosen score before removing it');
    });
    // The editor stays open on failure: nothing was removed.
    expect(onClose).not.toHaveBeenCalled();
  });

  it('surfaces an unmapped server message verbatim rather than a generic notice', async () => {
    window.API.removeDaihyosen = vi.fn().mockRejectedValue(new Error('another operator is scoring this match'));
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await waitFor(() => {
      expect(screen.getByTestId('team-editor-error').textContent)
        .toBe('another operator is scoring this match');
    });
  });

  // saveRunningSheet's pre-save can come back QUEUED (offline / retryable
  // 5xx) rather than refused. notLandedBanner rightly says nothing about a
  // queue, so without dependentActionBlocked's sentence (the one the editor
  // said before saveRunningSheet existed, assertRunningWritePersisted /
  // score_not_synced, see git history at 26d12df2) the tap silently did
  // nothing.
  const QUEUED_MESSAGE = "Couldn't save the current scores (offline or server busy). Try again once the connection is back.";

  it('a queued pre-save on Add shows the not-yet-sent message and never posts the add', async () => {
    // No daihyosen row yet, so the Add affordance (not Remove) renders.
    const noDaihyosenYet = matchWithDaihyosen({
      subResults: [
        { position: 1, sideA: 'Team A', sideB: 'Team B', ipponsA: ['M'], ipponsB: ['K'], winner: '' },
      ],
    });
    await renderEditor({ match: noDaihyosenYet, onSubmit: vi.fn().mockResolvedValue({ queued: true }) });

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });

    await waitFor(() => {
      expect(screen.getByTestId('team-editor-error').textContent).toBe(QUEUED_MESSAGE);
    });
    expect(window.API.recordDaihyosen).not.toHaveBeenCalled();
  });

  it('a queued pre-save on Remove shows the not-yet-sent message and never posts the remove', async () => {
    await renderEditor({ onSubmit: vi.fn().mockResolvedValue({ queued: true }) });

    // Arm the debounce on bout 2 (unscored, never the daihyosen row) so
    // onRemoveDaihyosen's own pre-save condition (hadPending || isDirty) is
    // true and the queued pre-save actually runs.
    await act(async () => { fireEvent.click(ipponButton(subMatchRows()[1], 'shiro', 'M')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });

    await waitFor(() => {
      expect(screen.getByTestId('team-editor-error').textContent).toBe(QUEUED_MESSAGE);
    });
    expect(window.API.removeDaihyosen).not.toHaveBeenCalled();
  });
});
