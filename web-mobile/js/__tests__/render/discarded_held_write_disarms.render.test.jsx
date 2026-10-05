// bc-cse (operator ruling 2026-10-05): after a held result is DISCARDED, the
// two-tap commit goes back to its unarmed label, so finishing again takes the
// usual two taps.
//
// A commit that is only QUEUED (offline, or a 5xx the server keeps refusing)
// leaves the button armed -- "Tap again to finish" -- which is fine while the
// write is pending: a tap re-sends it into the same queued entry, and the
// banner offers Retry now. But once the held write is gone because the
// operator DISCARDED it -- from the editor's own Discard held result
// (HeldWriteDiscard, admin_scoring_shared.jsx) or from the admin topbar's
// held-writes list (HeldWritesPanel, admin_shell.jsx) -- one more tap must
// not re-send what was just thrown away.
//
// Both doors are answered by the SAME edge: useClearPendingWhenNothingHeld
// (admin_scoring_shared.jsx) already clears the pending banner once this
// device holds no write for the match any more (`useMatchHeldWrite`'s
// `held` going false), landed or discarded from anywhere. The fix adds the
// disarm to that one callback in each editor, so there is one owner for
// "when", not three hand-copied effects.
//
// A write that LANDS takes the same edge but needs no extra case here: the
// match becomes complete and the armed label stops rendering at all ("Save
// correction" / nothing, since kachinukiBoutMode itself goes false) --
// verified per editor in a comment below and already covered for the
// two-tap mechanics by finish_arm_dwell.render.test.jsx.

import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap } from '../helpers/tap_events.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
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

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

let statusListeners;

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });

// Models the real api_client.jsx flow closely enough for this test: a write
// the server keeps refusing can be discarded from the editor
// (discardFailingHeldWrites), and either door -- that one, or the topbar's
// separate discardHeldWrite(key) -- ends the same way for THIS editor: the
// queue holds nothing for the match any more (hasHeldWrite -> false) and the
// sync-status subscribers are notified (_afterDiscard -> _recomputeSyncStatus
// -> notify), which is what useMatchHeldWrite re-reads on.
function installHeldWriteApi() {
  statusListeners = new Set();
  window.subscribeSyncStatus = (fn) => { statusListeners.add(fn); fn('syncing'); return () => statusListeners.delete(fn); };
  window.API.hasHeldWrite = vi.fn().mockReturnValue(true);
  window.API.heldWriteKeepsFailing = vi.fn().mockReturnValue(false);
  window.API.discardFailingHeldWrites = vi.fn(() => {
    window.API.hasHeldWrite.mockReturnValue(false);
    statusListeners.forEach((fn) => fn('synced'));
    return 1;
  });
}

// Door 2: the admin topbar's own discard (API.discardHeldWrite(key), a
// different code path this editor never calls) ends in the same state for
// this match: nothing held, sync status notified. The editor cannot tell the
// two doors apart, and must not need to.
function discardFromTopbar() {
  window.API.hasHeldWrite.mockReturnValue(false);
  statusListeners.forEach((fn) => fn('synced'));
}

const individualMatch = () => ({
  id: 'm-ind', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },
  sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: ['M'],
});

// Every numbered bout carries a result, so bc-tmfn's "unfought bout" refusal
// does not stand in front of Finish (mirrors finish_arm_dwell's fixture).
const teamMatch = () => ({
  id: 'm-team', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 3,
  sideA: { id: 'team-kyoto', name: 'Kyoto' },
  sideB: { id: 'team-osaka', name: 'Osaka' },
  subResults: [
    { position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: ['M'], winner: 'Osaka' },
    { position: 2, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake' },
    { position: 3, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake' },
  ],
});

const kachinukiMatch = () => ({
  id: 'm-kachi', compId: 'comp1', status: 'running', phase: 'bracket',
  round: 'Semi-final', matchNumber: 1, court: 'A',
  compKind: 'team', teamSize: 5, compFormat: 'knockout', teamMatchType: 'kachinuki',
  sideA: { id: 'team-A', name: 'Team A' },
  sideB: { id: 'team-B', name: 'Team B' },
  subResults: [
    { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M', 'K'], winner: 'B1' },
    { position: 2, sideA: 'A2', sideB: 'B1', ipponsA: ['M', 'K'], ipponsB: [], winner: 'A2' },
    { position: 3, sideA: 'A2', sideB: 'B2', ipponsA: [], ipponsB: [] },
  ],
});

const engiMatch = () => ({
  id: 'm-engi', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compEngi: true,
  sideA: { id: 'pa', name: 'Aoi - Haru', dojo: 'DojoA' },
  sideB: { id: 'pb', name: 'Bo - Cho', dojo: 'DojoB' },
  flagsA: 3, flagsB: 0,
});

async function mount(match) {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(
      match.compFormat === 'knockout'
        ? { id: 'comp1', config: { format: 'knockout', teamMatchType: 'kachinuki', naginata: false, players: [] } }
        : null,
    ),
    hasPendingTerminalWrite: vi.fn().mockReturnValue(false),
    recordDecision: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
  };
  installHeldWriteApi();
  // The commit resolves `{ queued: true }`: writeRetryable, so the editor
  // holds the pending banner and leaves the two-tap commit armed (today's
  // behaviour, unchanged by this fix).
  const onSubmit = vi.fn().mockResolvedValue({ queued: true });
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
  });
  return { onSubmit };
}

async function armAndConfirm(btn) {
  await pointerTap(btn);
  await wait(TAP_BOUNCE_MS + 50);
  await pointerTap(btn);
  await wait(10);
}

const SITES = [
  {
    name: 'individual Finish',
    match: individualMatch,
    button: () => screen.getByText(/^(Finish|Tap again to finish)$/),
    unarmed: 'Finish',
  },
  {
    name: 'team Finish',
    match: teamMatch,
    button: () => screen.getByText(/^(Finish|Tap again to finish)$/),
    unarmed: 'Finish',
  },
  {
    // bc-cse: required coverage per the task -- the kachinuki End match
    // button shares the team editor's one pendingWrite/useClearPendingWhenNothingHeld
    // wiring with Finish, so a discard must disarm it too.
    name: 'kachinuki End match',
    match: kachinukiMatch,
    button: () => screen.getByTestId('kachinuki-end-match-button'),
    unarmed: 'End match',
  },
  {
    name: 'engi Save',
    match: engiMatch,
    button: () => screen.getByTestId('engi-submit'),
    unarmed: 'Save result',
  },
];

describe.each(SITES)('bc-cse: a discarded held write disarms $name', ({ match, button, unarmed }) => {
  it('stays armed while the write is only queued (sanity check)', async () => {
    const { onSubmit } = await mount(match());
    const btn = button();
    await armAndConfirm(btn);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(btn.textContent).not.toBe(unarmed);
  });

  it("goes back to its unarmed label once the editor's own Discard held result is used", async () => {
    await mount(match());
    const btn = button();
    await armAndConfirm(btn);
    expect(btn.textContent).not.toBe(unarmed);

    // Cross the "keeps failing" threshold so Discard held result renders,
    // exactly as queued_write_banner.render.test.jsx's crossThreshold does.
    await act(async () => {
      window.API.heldWriteKeepsFailing.mockReturnValue(true);
      statusListeners.forEach((fn) => fn('server-error'));
    });
    const discardBtn = screen.getByTestId('held-write-discard');
    await act(async () => { fireEvent.click(discardBtn); });

    expect(btn.textContent).toBe(unarmed);
  });

  it('goes back to its unarmed label when the held write disappears as the topbar discard would make it', async () => {
    await mount(match());
    const btn = button();
    await armAndConfirm(btn);
    expect(btn.textContent).not.toBe(unarmed);

    await act(async () => { discardFromTopbar(); });

    expect(btn.textContent).toBe(unarmed);
  });
});
