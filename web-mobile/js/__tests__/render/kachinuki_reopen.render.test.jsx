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

  it('stays disabled after a successful tap so a double-tap cannot fire a second reopen', async () => {
    // mp-gmcg: onClose is a no-op in the inline (shiaijo) variant, so the
    // completed snapshot lingers through the SSE refetch window. The button
    // must stay disabled until the match flips to running, or a second tap
    // posts a reopen the server rejects as "not completed" (409).
    await renderEditor();
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    await waitFor(() => expect(window.API.reopenMatch).toHaveBeenCalledTimes(1));
    expect(screen.getByTestId('kachinuki-reopen-button').disabled).toBe(true);
    // A second tap is a no-op: no further server call.
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-reopen-button')); });
    expect(window.API.reopenMatch).toHaveBeenCalledTimes(1);
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

  // POST /decision is the OTHER way to finalize a match, so the server demands
  // the outstanding reason there too. The client has to ASK for it: the reason
  // box is normally rendered only for kiken, so on a reopened encounter a
  // fusenpai had no field at all and the operator met a 400 they could not
  // satisfy from the UI. These pin the prompt, not just the payload.
  it('asks for the reason on a fusenpai after a reason-less reopen', async () => {
    await renderEditor({ match: reopenedKachinukiMatch() });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-fusenpai-button')); });

    // The box exists at all (it does not for a non-reopened fusenpai) and says
    // it is required, naming the reopen as the cause.
    const input = screen.getByTestId('decision-reason');
    expect(input).toBeTruthy();
    expect(screen.getByText(/Reason \(required/)).toBeTruthy();
    expect(screen.getByText(/reopened, so ending it again needs a reason/)).toBeTruthy();

    // Record is held back while it is empty, so the operator cannot walk into
    // the server's rejection.
    const record = screen.getByRole('button', { name: 'Record' });
    expect(record.disabled).toBe(true);
    await act(async () => { fireEvent.input(input, { target: { value: '   ' } }); });
    expect(record.disabled).toBe(true);
    await act(async () => { fireEvent.input(input, { target: { value: 'Ended by mistake' } }); });
    expect(record.disabled).toBe(false);
  });

  it('leaves the reason optional when the match was not reopened', async () => {
    // Blast radius: the ordinary decision flow must stay reasonless. Gating
    // every decision would be a worse regression than the hole it closed.
    await renderEditor({ match: completedKachinukiMatch({ status: 'running', winner: null }) });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-fusenpai-button')); });

    expect(screen.queryByTestId('decision-reason')).toBeNull();
    expect(screen.getByRole('button', { name: 'Record' }).disabled).toBe(false);
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
