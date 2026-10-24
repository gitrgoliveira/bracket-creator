// editorErr is the team editor's inline error surface for the daihyosen
// add/remove POSTs. An inline lineup-position save that is refused or fails
// reports in the bout row it was made in instead (see
// team_editor_lineup_refusal_at_row.render.test.jsx), not here.
//
// It used to be rendered INSIDE the "add a representative bout" block, whose
// guard is `if (hasDaihyosen || !isKnockoutPhase || isKachinuki) return null`.
// Every state that could SET the error from somewhere else therefore hid it:
// removing an existing daihyosen (hasDaihyosen is true, so the block that
// would have shown the failure is gone) and, while a failed lineup save still
// reported here, the kachinuki flow (isKachinuki is true). The operator saw
// the button return to rest and nothing else — a silent failure on a write
// they believe landed.
//
// Pinned here on the remove-daihyosen path because it needs no lineup wiring:
// the assertion is simply that the failure REACHES THE SCREEN.

import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { SUPERSEDED_REASON, SUPERSEDED_ADVICE } from '../../write_result.jsx';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
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

  // An error that says nothing still says what was not done, the same way on
  // both buttons (the remove used to show nothing at all).
  it.each([
    ['a remove', 'removeDaihyosen', 'team-daihyosen-remove', 'The representative bout was not removed', undefined],
    ['an add', 'recordDaihyosen', 'scoring-modal-daihyosen-button', 'The representative bout was not added',
      { subResults: [{ position: 1, sideA: 'Team A', sideB: 'Team B', ipponsA: ['M'], ipponsB: ['K'], winner: '' }] }],
  ])('%s failing with no message says what was not done', async (_what, api, button, sentence, matchOverrides) => {
    window.API[api] = vi.fn().mockRejectedValue(new Error(''));
    await renderEditor(matchOverrides ? { match: matchWithDaihyosen(matchOverrides), onSubmit: vi.fn().mockResolvedValue({}) } : {});
    await act(async () => { fireEvent.click(screen.getByTestId(button)); });
    await waitFor(() => {
      expect(screen.getByTestId('team-editor-error').textContent).toBe(sentence);
    });
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

  // bc-evts (the governing rule): the pre-save and the add or remove are
  // entries of this match in the outbox, sent strictly in that order, and
  // nothing waits for the pre-save's answer. A pre-save only queued does not
  // stop the change (it is queued behind it); one the server turns away says
  // so in the not-saved banner, and the change behind it is judged by the
  // server on the bouts it holds.
  const SUPERSEDED = { applied: false, reason: 'superseded' };
  const failedBannerText = () => document.querySelector('.pending-write-banner--failed')?.textContent;
  const noDaihyosenYet = () => matchWithDaihyosen({
    subResults: [
      { position: 1, sideA: 'Team A', sideB: 'Team B', ipponsA: ['M'], ipponsB: ['K'], winner: '' },
    ],
  });

  it('a queued pre-save on Add does not stop the add: it is sent behind it', async () => {
    await renderEditor({ match: noDaihyosenYet(), onSubmit: vi.fn().mockResolvedValue({ queued: true }) });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('team-editor-error')).toBeNull();
  });

  it('a queued pre-save on Remove does not stop the remove: it is sent behind it', async () => {
    await renderEditor({ onSubmit: vi.fn().mockResolvedValue({ queued: true }) });
    // Arm the debounce on bout 2 (unscored, never the daihyosen row) so
    // onRemoveDaihyosen's own pre-save condition (hadPending || isDirty) holds.
    await act(async () => { fireEvent.click(ipponButton(subMatchRows()[1], 'shiro', 'M')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    expect(window.API.removeDaihyosen).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('team-editor-error')).toBeNull();
  });

  // Only a queued (scheduled) match is saved before an add, to start it
  // (bc-evts stage 5: a running sheet's taps are already ahead of the add).
  it('a superseded pre-save on Add shows the not-saved banner; the add was already handed over', async () => {
    const onSubmit = vi.fn().mockResolvedValue(SUPERSEDED);
    await renderEditor({ match: { ...noDaihyosenYet(), status: 'scheduled' }, onSubmit });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await waitFor(() => {
      expect(failedBannerText()).toBe(`Not applied: ${SUPERSEDED_REASON}. ${SUPERSEDED_ADVICE}`);
    });
    expect(onSubmit.mock.invocationCallOrder[0]).toBeLessThan(window.API.recordDaihyosen.mock.invocationCallOrder[0]);
  });

  // A remove saves no sheet first: the point struck before it is a tap, ahead
  // of the remove in the outbox (bc-evts stage 5). The match holds no row for
  // bout 2 yet, and the tap makes it on the server, so nothing else is written.
  it('a point struck right before Remove is a tap ahead of the remove, and no sheet is saved first', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    await renderEditor({ onSubmit });
    await act(async () => { fireEvent.click(ipponButton(subMatchRows()[1], 'shiro', 'M')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await waitFor(() => expect(window.API.removeDaihyosen).toHaveBeenCalledTimes(1));
    expect(onSubmit).not.toHaveBeenCalled();
    expect(window.API.recordTap.mock.invocationCallOrder[0]).toBeLessThan(window.API.removeDaihyosen.mock.invocationCallOrder[0]);
  });
});

// bc-evts stage 5: a running team match is scored by taps handed to the outbox
// (API.recordTap); each test gets a fresh fake of that surface, whose pending
// taps the board folds on. Registered last, so it lands on the API the hooks
// above set up.
beforeEach(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });
