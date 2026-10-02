// A not-saved banner offers no Retry (writeRetryable, write_result.jsx).
//
// The individual and engi editors kept the write they last queued and offered
// it as "Retry" beside the not-saved banner once the queue's replay was
// refused, and the individual editor did the same for a decision the server
// refused (superseded, or refused for the clock). Every such refusal is
// answered the same way however often the same write is sent: a newer result
// is stored (and a re-send, stamped now, would overwrite it), the clock was
// already resynced and the write re-sent, the match has finished, the shiaijo
// is busy. Only a QUEUED write is fixed by sending it again, and the pending
// banner's "Retry now" stays for that one.
//
// The team editor never offered Retry: it keeps no write to re-send.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { SUPERSEDED_REASON, SUPERSEDED_ADVICE } from '../../write_result.jsx';
import { keyboardClick } from '../helpers/tap_events.js';

// The terminal-failure channel api_client.jsx publishes, captured so a test
// can raise what the queue's refused replay (or a refused decision) raises.
let terminalListeners = new Set();
const raiseTerminalFailure = (info) => act(async () => { for (const fn of terminalListeners) fn(info); });

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (d) => d === 'kiken' || d === 'kiken-voluntary' || d === 'kiken-injury',
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
  subscribeTerminalWriteFailed: (fn) => { terminalListeners.add(fn); return () => terminalListeners.delete(fn); },
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
  terminalListeners = new Set();
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    hasPendingTerminalWrite: vi.fn().mockReturnValue(false),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDecision: vi.fn(),
    putMatchLineup: vi.fn(),
  };
});

const individualMatch = {
  id: 'm-ind', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: ['M'], hansokuA: 0, hansokuB: 0,
};
const engiMatch = {
  id: 'm-engi', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compEngi: true,
  sideA: { id: 'pa', name: 'Aoi - Haru', dojo: 'DojoA' }, sideB: { id: 'pb', name: 'Bo - Cho', dojo: 'DojoB' },
  flagsA: 3, flagsB: 0,
};

const EDITORS = [
  { name: 'individual', match: individualMatch, commit: () => screen.getByText(/^(Finish|Tap again to finish)$/) },
  { name: 'engi', match: engiMatch, commit: () => screen.getByTestId('engi-submit') },
];

// What the queue's replay can be refused with, as api_client.jsx raises it.
const REFUSALS = [
  ['superseded', { kind: 'score', status: 200, reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE }],
  ['refused on its merits (a 409 sentence)', { kind: 'score', status: 409, reason: 'This match has already finished.', sentence: true }],
];

const retryButton = () => screen.queryByRole('button', { name: /^Retry( now)?$/ });

describe.each(EDITORS)('$name editor: a refused write offers no Retry', ({ match, commit }) => {
  it.each(REFUSALS)('a queued finish, then its replay %s', async (_how, payload) => {
    const onSubmit = vi.fn().mockResolvedValue({ queued: true });
    await act(async () => {
      render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    });
    // Keyboard clicks: never read as a bounce, so arm then commit.
    await keyboardClick(commit());
    await keyboardClick(commit());
    expect(onSubmit).toHaveBeenCalledTimes(1);
    // Queued: the one write a re-send can land, so Retry now is offered.
    expect(screen.getByText(/will keep retrying until it lands/)).toBeTruthy();
    expect(retryButton()?.textContent).toBe('Retry now');

    await raiseTerminalFailure({ compID: match.compId, matchID: match.id, ...payload });

    expect(screen.getByRole('alert').textContent).toContain('Not saved');
    expect(retryButton(), 'no Retry beside a refusal').toBeNull();
  });
});

describe('individual editor: a refused decision offers no Retry', () => {
  async function recordWithdrawal() {
    await act(async () => {
      render(<ScoreEditorModal match={individualMatch} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} onAfterDecision={vi.fn()} password="secret" />);
    });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Kiken . Voluntary$/ })); });
    const form = document.querySelector('form.decision-prompt');
    expect(form, 'the withdrawal form opened').not.toBeNull();
    fireEvent.click(form.querySelector('input[name="decision-side"][value="aka"]'));
    await act(async () => { fireEvent.submit(form); });
  }

  it('superseded: the not-saved banner, no "will keep retrying", no Retry', async () => {
    // What api_client.jsx's recordDecision does with a 200 {applied:false}:
    // announce it on the terminal-failure channel, then hand the body back.
    window.API.recordDecision = vi.fn(async (compID, matchID) => {
      for (const fn of terminalListeners) fn({ compID, matchID, kind: 'score', status: 200, reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE });
      return { applied: false, reason: 'superseded' };
    });
    await recordWithdrawal();
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('alert').textContent).toContain(SUPERSEDED_REASON);
    expect(screen.queryByText(/will keep retrying until it lands/)).toBeNull();
    expect(retryButton()).toBeNull();
  });

  // The decision submit itself must not call a refusal pending: the channel
  // above is what reports it, and without it the pending banner said "will
  // keep retrying until it lands" over a write that never will, with Retry
  // now beside it.
  it('refused for the clock: never the pending banner, with or without the channel', async () => {
    window.API.recordDecision = vi.fn().mockResolvedValue({ applied: false, reason: 'clock_skew' });
    await recordWithdrawal();
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
    expect(screen.queryByText(/will keep retrying until it lands/)).toBeNull();
    expect(retryButton()).toBeNull();
  });

  it('queued: the pending banner keeps Retry now, which sends the decision again', async () => {
    window.API.recordDecision = vi.fn().mockResolvedValue({ queued: true });
    await recordWithdrawal();
    expect(screen.getByText(/will keep retrying until it lands/)).toBeTruthy();
    await act(async () => { fireEvent.click(retryButton()); });
    expect(window.API.recordDecision).toHaveBeenCalledTimes(2);
  });
});
