// mp-gmcg: mistake recovery on a completed kachinuki team encounter.
//
// The operator-visible contract pinned here, all three parts of it:
//
//   1. REOPEN IS ONE TAP. The operator ended the match BY MISTAKE and is
//      standing at the shiaijo with the competitors still there. Nothing may
//      stand between the tap and the reopen — no prompt, no reason, no arm.
//   2. ENDING IT AGAIN ASKS FOR NO REASON EITHER (operator ruling
//      2026-09-25: a match can be reopened without any reason, and nothing is
//      gated on that). [End match] and the withdrawal panel behave on a
//      reopened encounter exactly as on any running one.
//   3. A BUSY COURT IS NOT A DEAD END. The court-busy 409 names the match
//      holding the court; the panel offers to send it back to the queue and
//      retry. That is DESTRUCTIVE (it clears that match's score), so the
//      consequence is on screen before the tap, and neither half of the
//      retry may strand the UI when it fails.

import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
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
  // The conflict panel resolves the blocker through compMatchesForCompetition
  // (viewer_utils.jsx), which recombines the split detail payload. Stub both
  // seams so this test pins the PANEL's behaviour, not which helper it picks.
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
  window.compMatches = () => [];
  window.compMatchesForCompetition = () => [];
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
    requeueBlockerAndReopen: vi.fn().mockResolvedValue(true),
    removeKachinukiBout: vi.fn().mockResolvedValue({ id: 'm1', subResults: [] }),
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
// carrying the server's reopenPending stamp, which asks the operator for
// nothing.
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
  it('posts immediately, with no reason and no prompt, and keeps the editor open', async () => {
    const onClose = vi.fn();
    await renderEditor({ onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    // No gate of any kind: the tap IS the reopen.
    expect(screen.queryByText('Reason for reopening')).toBeNull();
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
    // No reason and no downstream confirmation: a reason here would mean the
    // operator was asked for one.
    expect(window.API.reopenMatch).toHaveBeenCalledWith('comp1', 'm1', 'secret', { reason: '', force: false });
    // The editor follows the match to running in place, as Clear withdrawal
    // and reopen does, so what the reopen reports can still be read here.
    await waitFor(() => expect(screen.getByTestId('kachinuki-reopen-button').textContent).toBe('Reopened'));
    expect(onClose).not.toHaveBeenCalled();
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

  it('stays disabled after a successful tap so a double-tap cannot fire a second reopen', async () => {
    // mp-gmcg: the editor stays open, so the completed snapshot lingers
    // through the SSE refetch window. The button must stay disabled until the
    // match flips to running, or a second tap posts a reopen the server
    // rejects as "not completed" (409).
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await waitFor(() => expect(window.API.reopenMatch).toHaveBeenCalledTimes(1));
    expect(screen.getByTestId('kachinuki-reopen-button').disabled).toBe(true);
    // A second tap is a no-op: no further server call.
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
  });

  // The match_updated broadcast is what flips the prop to running. When it
  // never arrives (a dropped event, no refetch), the success response is the
  // only word the editor gets, so it must not go on saying "Reopening…" as if
  // the request were still in flight: it says the reopen landed, and still
  // refuses a second post.
  it('says Reopened on the success response even when the running match never arrives', async () => {
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await waitFor(() => expect(window.API.reopenMatch).toHaveBeenCalledTimes(1));
    const button = screen.getByTestId('kachinuki-reopen-button');
    await waitFor(() => expect(button.textContent).toBe('Reopened'));
    expect(button.textContent).not.toBe('Reopening…');
    expect(button.disabled).toBe(true);
  });
});

describe('kachinuki reopen: a later knockout match already fought', () => {
  // The dialog stub is shared across the file; put back the default answer.
  afterEach(() => { window.confirmDialog = vi.fn().mockResolvedValue(true); });

  // What api_client's reopenFailureError throws for the 409
  // downstream_knockout_played refusal of a reopen.
  function reopenDownstreamRefusal() {
    const e = new Error('reopening match "m1" would change the winner ...');
    e.downstreamKnockoutPlayed = {
      matchId: 'm1',
      blockingMatchId: 'm-r2-0',
      blockingMatches: [{ id: 'm-r2-0', number: 3 }],
      displaced: 'Team A',
      qualifierChange: [],
      reopen: true,
    };
    return e;
  }

  it('asks in reopen words, and on confirm keeps the editor open to name what else it reopened', async () => {
    const onClose = vi.fn();
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    window.API.reopenMatch = vi.fn()
      .mockRejectedValueOnce(reopenDownstreamRefusal())
      .mockResolvedValueOnce({ reopenedMatches: [{ id: 'm-r2-0', number: 3 }] });
    const utils = await renderEditor({ onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });

    await waitFor(() => expect(window.confirmDialog).toHaveBeenCalledTimes(1));
    const { message, confirmLabel } = window.confirmDialog.mock.calls[0][0];
    expect(message).toContain('Reopening this match also reopens Match 3');
    expect(message.toLowerCase()).not.toContain('correction');
    expect(confirmLabel).toBe('Reopen both');

    await waitFor(() => expect(window.API.reopenMatch).toHaveBeenCalledTimes(2));
    expect(window.API.reopenMatch.mock.calls[1][3]).toEqual({ reason: '', force: true });
    await waitFor(() => expect(screen.getByTestId('kachinuki-reopen-notice').textContent)
      .toBe('Match 3 was reopened: it must be fought and scored again.'));
    expect(onClose).not.toHaveBeenCalled();

    // The notice is read once the match is running again, which is when the
    // operator looks for it: the flip removes the Reopen control, not it.
    await act(async () => {
      utils.rerender(
        <ScoreEditorModal match={reopenedKachinukiMatch()} onClose={onClose}
          onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret" />
      );
    });
    expect(screen.queryByTestId('kachinuki-reopen-button')).toBeNull();
    expect(screen.getByTestId('kachinuki-reopen-notice').textContent)
      .toBe('Match 3 was reopened: it must be fought and scored again.');
    expect(onClose).not.toHaveBeenCalled();
  });

  it('says the reopen was cancelled, not a correction, when the operator declines', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    window.API.reopenMatch = vi.fn().mockRejectedValue(reopenDownstreamRefusal());
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await waitFor(() => expect(screen.getByTestId('kachinuki-reopen-error').textContent)
      .toBe('Reopen cancelled: this match and the later result it depends on were left unchanged.'));
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('kachinuki-reopen-button').textContent).toBe('Reopen match');
  });
});

describe('kachinuki reopen: ending it again asks for no reason', () => {
  it('[End match] on a reopened encounter arms and ends as on any running one', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await renderEditor({ match: reopenedKachinukiMatch(), onSubmit });

    fireEvent.click(screen.getByTestId('kachinuki-end-match-button'));
    expect(screen.queryByText('Reason for reopening')).toBeNull();
    expect(onSubmit).not.toHaveBeenCalled();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-end-match-button')); });

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const patch = onSubmit.mock.calls[0][0];
    expect(patch.status).toBe('completed');
    expect(patch.correctionReason).toBeUndefined();
  });

  it('does not prompt on a running encounter that was never reopened', async () => {
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
    const blocking = [
      { id: 'm-r1-1', sideA: { name: 'Team C' }, sideB: { name: 'Team D' } },
    ];
    window.compMatches = () => blocking;
    window.compMatchesForCompetition = () => blocking;
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
    // bc-rawm: the server's own raw sentence is GONE, not shown alongside the
    // heading -- it named the same internal id and told the operator to
    // "finish that match before reopening", the opposite of this panel's own
    // remedy button.
    expect(panel.textContent).not.toContain('Finish that match before reopening');
    expect(panel.textContent).not.toContain('m-r1-1');
    expect(screen.getByTestId('kachinuki-reopen-requeue-button')).toBeTruthy();
  });

  it('requeues the blocker and reopens the target in ONE atomic call', async () => {
    const onClose = vi.fn();
    // The one-tap reopen surfaces the court_busy conflict; the remedy is then
    // the single requeue-and-reopen call (mp-gmcg A4).
    window.API.reopenMatch = vi.fn().mockRejectedValueOnce(courtBusyError());
    window.API.requeueBlockerAndReopen = vi.fn().mockResolvedValue(true);
    await renderEditor({ onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await screen.findByTestId('kachinuki-reopen-conflict');

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-requeue-button')); });

    // ONE server call: target=(comp1, m1), blocker=(comp1, m-r1-1). No second
    // reopenMatch call — the requeue and reopen commit together server-side.
    expect(window.API.requeueBlockerAndReopen).toHaveBeenCalledWith('comp1', 'm1', 'comp1', 'm-r1-1', 'secret', { reason: '', force: false });
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByTestId('kachinuki-reopen-conflict')).toBeNull());
    expect(onClose).not.toHaveBeenCalled();
  });

  it('clears the panel and shows the message when the atomic remedy is refused', async () => {
    // A completed blocker (409) is not a court_busy, so the stale conflict panel
    // is dropped and the server's sentence is shown; the operator can tap Reopen
    // again (the court may now be free).
    window.API.reopenMatch = vi.fn().mockRejectedValue(courtBusyError());
    window.API.requeueBlockerAndReopen = vi.fn().mockRejectedValue(new Error('match already completed'));
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await screen.findByTestId('kachinuki-reopen-conflict');

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-requeue-button')); });

    await waitFor(() => {
      expect(screen.getByTestId('kachinuki-reopen-error').textContent).toContain('match already completed');
    });
    expect(screen.queryByTestId('kachinuki-reopen-conflict')).toBeNull();
    expect(screen.getByTestId('kachinuki-reopen-button').textContent).toBe('Reopen match');
  });

  it('re-offers the remedy when a DIFFERENT match has since taken the court', async () => {
    // The atomic remedy fails with a court_busy naming a NEW blocker: the panel
    // re-offers the remedy rather than dead-ending. bc-rawm: the panel no
    // longer prints the blocking matchId as text (no label was sent for it
    // here, and window.compMatchesForCompetition's fixture only knows
    // 'm-r1-1', so the fetched blockerLabel cannot resolve the new one
    // either), so this proves the new conflict took hold BEHAVIOURALLY:
    // tapping the remedy again targets the NEW blocker's id, not the stale
    // one.
    window.API.reopenMatch = vi.fn().mockRejectedValue(courtBusyError());
    const newBlocker = courtBusyError();
    newBlocker.matchId = 'm-r1-2';
    newBlocker.message = 'Court A already has a running match (m-r1-2). Finish that match before reopening this one.';
    window.API.requeueBlockerAndReopen = vi.fn()
      .mockRejectedValueOnce(newBlocker)
      .mockResolvedValue(true);
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await screen.findByTestId('kachinuki-reopen-conflict');

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-requeue-button')); });

    // Still a conflict panel — the remedy is re-offered, not a dead end.
    await screen.findByTestId('kachinuki-reopen-conflict');
    expect(screen.getByTestId('kachinuki-reopen-requeue-button')).toBeTruthy();

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-requeue-button')); });
    expect(window.API.requeueBlockerAndReopen).toHaveBeenLastCalledWith(
      'comp1', 'm1', 'comp1', 'm-r1-2', 'secret', { reason: '', force: false },
    );
  });

  // POST /decision is the other way to end a match, and after a reason-less
  // reopen it asks for no reason either: the panel is the same as on any
  // running match, with Record available at once.
  it('asks for no reason on a fusenpai after a reason-less reopen', async () => {
    await renderEditor({ match: reopenedKachinukiMatch() });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-fusenpai-button')); });

    expect(screen.queryByTestId('decision-reason')).toBeNull();
    expect(screen.queryByText(/ending it again needs a reason/)).toBeNull();
    expect(screen.getByRole('button', { name: 'Record' }).disabled).toBe(false);
  });

  it('leaves the reason optional when the match was not reopened', async () => {
    await renderEditor({ match: completedKachinukiMatch({ status: 'running', winner: null }) });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-fusenpai-button')); });

    expect(screen.queryByTestId('decision-reason')).toBeNull();
    expect(screen.getByRole('button', { name: 'Record' }).disabled).toBe(false);
  });

  it('recovers when the atomic remedy fails on the target (downstream fought)', async () => {
    const onClose = vi.fn();
    window.API.reopenMatch = vi.fn().mockRejectedValueOnce(courtBusyError());
    window.API.requeueBlockerAndReopen = vi.fn()
      .mockRejectedValue(new Error('cannot reopen: a downstream knockout match has already been fought'));
    await renderEditor({ onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await screen.findByTestId('kachinuki-reopen-conflict');

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-requeue-button')); });

    // Not stuck: the failure reaches the operator, the (now irrelevant) conflict
    // panel is gone, and Reopen is tappable again. Nothing partially applied —
    // the requeue and reopen commit together server-side.
    await waitFor(() => {
      expect(screen.getByTestId('kachinuki-reopen-error').textContent)
        .toBe('cannot reopen: a downstream knockout match has already been fought');
    });
    expect(screen.queryByTestId('kachinuki-reopen-conflict')).toBeNull();
    expect(screen.getByTestId('kachinuki-reopen-button').textContent).toBe('Reopen match');
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe('kachinuki completed correction is Reopen-only (no IV/PW Save correction)', () => {
  // A completed kachinuki match must NOT offer the generic "Save correction"
  // button. That path builds the completed patch from teamWinner — the IV/PW
  // leader — which is the exact rule kachinuki does NOT use (it is decided by
  // the LAST scored bout). Correcting a finalized kachinuki result goes through
  // Reopen instead: back to bout mode, then End match re-derives from the last
  // bout via deriveKachinukiEndOutcome. Without the guard a "Save correction"
  // could silently rewrite a drawn/again-decided encounter to the IV winner.
  it('a completed kachinuki match shows Reopen but not Save correction', async () => {
    await renderEditor(); // default match is a completed kachinuki encounter
    expect(screen.getByTestId('kachinuki-reopen-button')).toBeTruthy();
    expect(screen.queryByText('Save correction')).toBeNull();
  });

  it('a completed NON-kachinuki team match still shows Save correction', async () => {
    // Scoping control: the suppression is kachinuki-only. A regular team match
    // keeps the generic correction (its winner IS the IV/PW leader) and has no
    // Reopen affordance.
    await renderEditor({ match: completedKachinukiMatch({ teamMatchType: 'regular' }) });
    expect(screen.getByText('Save correction')).toBeTruthy();
    expect(screen.queryByTestId('kachinuki-reopen-button')).toBeNull();
  });
});

// mp-gmcg: [× Remove this bout] is the explicit undo for a pairing appended by
// mistake on a RUNNING kachinuki encounter. It renders only when the current
// bout is an unscored EXTRA (a prior bout was scored) — exactly the row the
// End-match strip would drop — and DELETEs it server-side.
describe('kachinuki [× Remove this bout] undoes a bout added by mistake', () => {
  function runningWithAppendedBout(overrides = {}) {
    return completedKachinukiMatch({
      status: 'running',
      winner: null,
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' },
        { position: 2, sideA: 'A1', sideB: 'B2', ipponsA: [], ipponsB: [] },
      ],
      ...overrides,
    });
  }

  it('renders on an unscored appended bout, DELETEs it, and adopts the shorter log', async () => {
    // The server returns the post-strip match (bout 1 only). The parent does
    // NOT refresh this snapshot for an out-of-band mutation, so the modal must
    // adopt the shorter log itself — proven by the button vanishing (the
    // current bout is now the SCORED bout 1, which is not removable).
    window.API.removeKachinukiBout = vi.fn().mockResolvedValue({
      id: 'm1', subResults: [{ position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' }],
    });
    await renderEditor({ match: runningWithAppendedBout() });
    expect(screen.getByTestId('kachinuki-remove-bout-button')).toBeTruthy();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-remove-bout-button')); });
    expect(window.API.removeKachinukiBout).toHaveBeenCalledWith('comp1', 'm1', 'secret');
    await waitFor(() => expect(screen.queryByTestId('kachinuki-remove-bout-button')).toBeNull());
  });

  it('does NOT render on the bootstrap bout 1 (nothing appended yet)', async () => {
    await renderEditor({
      match: runningWithAppendedBout({
        subResults: [{ position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: [] }],
      }),
    });
    expect(screen.queryByTestId('kachinuki-remove-bout-button')).toBeNull();
    expect(window.API.removeKachinukiBout).not.toHaveBeenCalled();
  });

  it('surfaces a failure inline and does not close the editor', async () => {
    const onClose = vi.fn();
    window.API.removeKachinukiBout = vi.fn().mockRejectedValue(new Error('no unscored bout to remove'));
    await renderEditor({ match: runningWithAppendedBout(), onClose });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-remove-bout-button')); });
    await waitFor(() => {
      expect(screen.getByTestId('kachinuki-remove-bout-error').textContent).toBe('no unscored bout to remove');
    });
    expect(onClose).not.toHaveBeenCalled();
  });

  // mp-gmcg review F1: removing a bout must not leave subsRaw shorter than the
  // teamSize-floored positionCount. If it does, the render-only extension patches
  // the gap without committing, and the NEXT Record-bout's updateSub(curIdx)
  // becomes an out-of-range no-op — so a score on the re-appended bout silently
  // vanishes. Drive the full sequence: remove → server catches up → Record
  // re-appends bout 2 → score it, and assert the score registers.
  //
  // NOTE (review C1): the intermediate `rerender(propWith(boutOneOnly))` below
  // simulates a host that re-syncs the `match` PROP down to the post-removal
  // length before Record grows it again. Neither real host actually does this
  // (admin_schedule_score_editor.jsx's openMatch is untouched by a removal;
  // admin_shiaijo.jsx remounts the whole modal on a length change instead) — so
  // this test pins the id+length effect's OWN behaviour when the prop genuinely
  // moves through that state, but does not by itself prove the real modal-host
  // sequence works. See the "adopts a Record-bout append directly" test below
  // for that sequence, where the prop's length never changes at all.
  it('records a score on a bout re-appended after a removal (subsRaw stays sized to the floor)', async () => {
    const boutOneOnly = [{ position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' }];
    window.API.removeKachinukiBout = vi.fn().mockResolvedValue({ id: 'm1', subResults: boutOneOnly });
    const utils = await renderEditor({ match: runningWithAppendedBout() });

    // Remove the trailing unscored bout 2; the current bout becomes scored bout 1.
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-remove-bout-button')); });
    await waitFor(() => expect(screen.queryByTestId('kachinuki-remove-bout-button')).toBeNull());

    const propWith = (subResults) => (
      <ScoreEditorModal
        match={completedKachinukiMatch({ status: 'running', winner: null, subResults })}
        onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret"
      />
    );
    // The parent's snapshot catches up to the removal (log length 2 → 1, which
    // clears the local override), then Record re-appends bout 2 (1 → 2).
    await act(async () => { utils.rerender(propWith(boutOneOnly)); });
    await act(async () => {
      utils.rerender(propWith([...boutOneOnly, { position: 2, sideA: 'A1', sideB: 'B2', ipponsA: [], ipponsB: [] }]));
    });

    // Bout 2 is the unplayed current bout → the Record-bout hint is shown.
    expect(screen.queryByTestId('kachinuki-record-hint')).not.toBeNull();
    // Score it via the keyboard (Shiro men). Pre-fix this was a no-op.
    await act(async () => { fireEvent.keyDown(window, { key: 'm' }); });
    await waitFor(() => expect(screen.queryByTestId('kachinuki-record-hint')).toBeNull());
  });

  // mp-gmcg review C1: the REAL admin_schedule_score_editor.jsx modal-host
  // sequence — openMatch (and so the `match` prop) is untouched by a removal,
  // and Record-bout's own response spreads a fresh subResults of the SAME
  // length back onto it (remove: L→L-1 in the override only; Record:
  // L-1→L again), so the id+length effect's deps never change and the
  // override could freeze on the pre-append state forever. The fix adopts
  // Record's own response directly into the override, independent of the prop
  // ever visibly changing — proven here by NEVER rerendering the match prop
  // for the whole sequence.
  it('adopts a Record-bout append directly into the override when the parent prop never changes', async () => {
    const boutOneOnly = [{ position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' }];
    window.API.removeKachinukiBout = vi.fn().mockResolvedValue({ id: 'm1', subResults: boutOneOnly });
    // Mirrors the FIXED admin_schedule_score_editor.jsx onSubmit, which now
    // returns `res` (containing the post-advance subResults) from its
    // kachinukiBoutFinal branch instead of silently dropping it.
    const boutOneAndNew = [...boutOneOnly, { position: 2, sideA: 'A1', sideB: 'B3', ipponsA: [], ipponsB: [] }];
    const onSubmit = vi.fn().mockResolvedValue({ subResults: boutOneAndNew });
    await renderEditor({ match: runningWithAppendedBout(), onSubmit });

    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-remove-bout-button')); });
    await waitFor(() => expect(screen.queryByTestId('kachinuki-remove-bout-button')).toBeNull());
    // Bout 1 (the surviving bout) is already scored → no "unplayed" hint yet,
    // and Record bout is enabled.
    expect(screen.queryByTestId('kachinuki-record-hint')).toBeNull();
    expect(screen.getByRole('button', { name: 'Record bout' })).not.toBeDisabled();

    // The `match` prop is NEVER rerendered from here on: this is the exact
    // modal-host behaviour the bug depends on.
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Record bout' })); });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));

    // Pre-fix: the override stays frozen on the 1-bout post-removal state, so
    // the editor still thinks bout 1 (already scored) is current — Record
    // stays enabled and the hint stays absent, i.e. this assertion is the one
    // that catches the freeze.
    await waitFor(() => expect(screen.queryByTestId('kachinuki-record-hint')).not.toBeNull());
    expect(screen.getByRole('button', { name: 'Record bout' })).toBeDisabled();
  });

  // mp-gmcg review F4: the local override that hides the removed bout must
  // survive a same-content snapshot reload. An SSE refresh hands back a NEW
  // match object with identical (stale) content while the parent list catches
  // up; keying the override reset on object identity cleared it and flashed the
  // removed bout back. Key on id + log length instead.
  it('does not flash the removed bout back on a same-content snapshot reload', async () => {
    window.API.removeKachinukiBout = vi.fn().mockResolvedValue({
      id: 'm1', subResults: [{ position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' }],
    });
    const utils = await renderEditor({ match: runningWithAppendedBout() });
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-remove-bout-button')); });
    await waitFor(() => expect(screen.queryByTestId('kachinuki-remove-bout-button')).toBeNull());

    // A fresh object, SAME stale two-bout content (the delete has not yet
    // propagated to the parent's list). The removed bout must stay gone.
    await act(async () => {
      utils.rerender(
        <ScoreEditorModal
          match={runningWithAppendedBout()}
          onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret"
        />
      );
    });
    expect(screen.queryByTestId('kachinuki-remove-bout-button')).toBeNull();
  });
});

// mp-gmcg review: the Encho affordance targets the last SCORED bout, which
// diverges from the current EDITABLE bout the moment Record advances the
// encounter. Offering it after the advance let a tap write overtime onto a
// now read-only past bout while the appended bout hung unscored.
describe('kachinuki Encho is offered only on the current tied bout', () => {
  // A tie needs operator input to count as played: equal ippons (1-1).
  const tiedBout = (pos, a = 'A1', b = 'B1') => (
    { position: pos, sideA: a, sideB: b, ipponsA: ['M'], ipponsB: ['M'] }
  );

  it('shows the Encho button when the tied bout is the current bout', async () => {
    await renderEditor({
      match: completedKachinukiMatch({ status: 'running', winner: null, subResults: [tiedBout(1)] }),
    });
    expect(screen.getByTestId('kachinuki-encho-button')).toBeTruthy();
  });

  it('hides the Encho button once Record has appended the next pairing', async () => {
    await renderEditor({
      match: completedKachinukiMatch({
        status: 'running', winner: null,
        // Bout 1 tied (last SCORED), bout 2 appended and unplayed (current).
        subResults: [tiedBout(1), { position: 2, sideA: 'A1', sideB: 'B2', ipponsA: [], ipponsB: [] }],
      }),
    });
    // The End outcome still reads "draw" (it judges the last SCORED bout), so
    // the pre-fix gate kachinukiEnchoAvailable would still render the button;
    // the fix additionally requires last-scored === current-editable.
    expect(screen.queryByTestId('kachinuki-encho-button')).toBeNull();
    // The hint must not advertise the now-hidden button either.
    const hint = screen.queryByTestId('kachinuki-end-hint');
    if (hint) expect(hint.textContent).not.toContain('Encho keeps');
  });

  // bc-kten (operator ruling 2026-09-25): only the last bout, taisho against
  // taisho, may go to encho. With both lineups in force the editor knows who
  // each taisho is (kachinukiTaishoPairing, the twin of the server's rule).
  describe('only taisho against taisho (bc-kten)', () => {
    const lineupFor = (p) => ({ positions: { 1: `${p}1`, 2: `${p}2`, 3: `${p}3` } });
    beforeEach(() => {
      window.API.fetchMatchLineup = vi.fn().mockImplementation(async (_c, teamId) => (
        teamId === 'team-A' ? lineupFor('A') : teamId === 'team-B' ? lineupFor('B') : null
      ));
    });
    const drawn = (pos) => ({ position: pos, sideA: `A${pos}`, sideB: `B${pos}`, ipponsA: [], ipponsB: [], decision: 'hikiwake' });

    it('withholds Encho from a tie between the first fighters', async () => {
      await renderEditor({
        match: completedKachinukiMatch({ status: 'running', winner: null, subResults: [tiedBout(1)] }),
      });
      await waitFor(() => expect(window.API.fetchMatchLineup).toHaveBeenCalledTimes(2));
      await waitFor(() => expect(screen.queryByTestId('kachinuki-encho-button')).toBeNull());
      const hint = screen.queryByTestId('kachinuki-end-hint');
      if (hint) expect(hint.textContent).not.toContain('Encho keeps');
    });

    it('offers Encho when the two taisho are tied', async () => {
      await renderEditor({
        match: completedKachinukiMatch({
          status: 'running', winner: null,
          subResults: [drawn(1), drawn(2), tiedBout(3, 'A3', 'B3')],
        }),
      });
      await waitFor(() => expect(window.API.fetchMatchLineup).toHaveBeenCalledTimes(2));
      expect(screen.getByTestId('kachinuki-encho-button')).toBeTruthy();
    });
  });
});

// mp-gmcg review: a per-bout kachinuki encho also bumped the MATCH-level
// enchoPeriodCount, which enchoBlock() serialised onto EVERY completed branch
// — including the End-as-draw one. That persisted `encho` alongside decision
// "hikiwake", a contradiction the display only swallowed because X beats (E).
describe('kachinuki End-match omits the match-level encho on a drawn end', () => {
  const decisiveBout = { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M', 'M'], ipponsB: [], winner: 'A1', encho: { periodCount: 1 } };

  async function endMatch(onSubmit) {
    // Non-reopened running match → ordinary arm/confirm (two taps), no prompt.
    fireEvent.click(screen.getByTestId('kachinuki-end-match-button'));
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-end-match-button')); });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    return onSubmit.mock.calls[0][0];
  }

  it('keeps the encounter (E) on a DECISIVE end where a bout went to overtime', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await renderEditor({
      match: completedKachinukiMatch({
        status: 'running', winner: null,
        encho: { periodCount: 1 }, // seeds enchoPeriodCount
        subResults: [decisiveBout],
      }),
      onSubmit,
    });
    const patch = await endMatch(onSubmit);
    expect(patch.decision).toBe('kachinuki-exhaustion');
    expect(patch.encho?.periodCount).toBe(1);
  });

  it('drops the encounter (E) on a DRAWN end even when an earlier bout had overtime', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await renderEditor({
      match: completedKachinukiMatch({
        status: 'running', winner: null,
        encho: { periodCount: 1 }, // an earlier bout went to overtime
        // Last SCORED bout is a tie → End records a drawn encounter (pool phase).
        subResults: [decisiveBout, { position: 2, sideA: 'A2', sideB: 'B2', ipponsA: ['M'], ipponsB: ['M'] }],
      }),
      onSubmit,
    });
    const patch = await endMatch(onSubmit);
    expect(patch.decision).toBe('hikiwake');
    expect(patch.encho).toBeUndefined();
  });
});
