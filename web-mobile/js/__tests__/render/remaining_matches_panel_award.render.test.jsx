// bc-rawm: RemainingMatchesPanel.award referenced an undefined `wname`
// (ReferenceError on every click, admin_scoring_shared.jsx). The unit suite
// never caught it because it mounts no real React and never invokes the
// click handler; only a REAL render, actually clicked, executes the function
// body. This drives the individual editor through a genuine kiken flow (the
// only door that opens the panel) and then clicks "Award default win to
// opponent", pinning both that it no longer throws and the exact
// recordDecision arguments: fusensho (the match-level default win this panel
// shares with the queue row's Record default win, defaultWinDecisionBodyForSide),
// decisionBy naming the withdrawn competitor's side, and a decisionReason
// naming them.

import React from 'react';
import { render, act, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  // Unlike the config-matrix / withdrawal-reopen render suites (which stub
  // this false because their kiken fixtures are already-completed matches,
  // never SUBMITTED through it), this test drives a LIVE kiken submit and
  // depends on the branch this predicate gates (makeSubmitDecision,
  // admin_scoring_shared.jsx): true routes to RemainingMatchesPanel, the one
  // door this file exists to test.
  isKikenDecision: (k) => typeof k === 'string' && k.startsWith('kiken'),
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  poolLabel: (m) => m.poolName || 'Pool',
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

// Yamada (aka) vs Tanaka (shiro), fresh 0-0, a knockout Round 1 match.
const match = {
  id: 'm-r1-0', compId: 'comp1', status: 'running', phase: 'knockout', round: 'Round 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },
  sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
};

// A second, still-SCHEDULED match of Tanaka's, in a different pool, for the
// panel to list and the operator to award.
const remaining = {
  id: 'Pool A-3', compId: 'comp1', phase: 'pool', poolName: 'Pool A', court: 'B', status: 'scheduled',
  sideA: { id: 'p2', name: 'Tanaka' },
  sideB: { id: 'p3', name: 'Suzuki' },
};

beforeEach(() => {
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({ id: 'comp1', config: { format: 'knockout', naginata: false, players: [] } }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ id: 'm-r1-0', status: 'completed' }),
  };
  window.compMatchesForCompetition = vi.fn().mockReturnValue([remaining]);
});

async function mount(props = {}) {
  let view;
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="secret" {...props} />);
  });
  return view;
}

async function recordTanakaKiken(props = {}) {
  await mount(props);
  await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-kiken-voluntary-button')); });
  // defaultSide="shiro" on DecisionPrompt: Tanaka (sideB) is the withdrawn
  // side without touching the radio.
  const record = [...document.querySelectorAll('.decision-prompt button')].find((b) => b.textContent === 'Record');
  await act(async () => { fireEvent.click(record); });
  await waitFor(() => expect(window.API.recordDecision).toHaveBeenCalledTimes(1));
}

describe('RemainingMatchesPanel.award (bc-rawm)', () => {
  it('lists the remaining scheduled match and awards fusensho with the withdrawn side and a reason naming them', async () => {
    await recordTanakaKiken();

    // The panel opened, fetched, and listed Tanaka's remaining match.
    await waitFor(() => expect(screen.getByText('Suzuki')).toBeTruthy());
    const award = screen.getByText('Award default win to opponent');
    expect(award.disabled).toBe(false);

    // The click must not throw (the ReferenceError this test exists to
    // catch would surface here) and must record the right decision.
    await act(async () => { fireEvent.click(award); });

    expect(window.API.recordDecision).toHaveBeenCalledTimes(2);
    const [compId, matchId, body, password] = window.API.recordDecision.mock.calls[1];
    expect(compId).toBe('comp1');
    expect(matchId).toBe('Pool A-3');
    // fusensho: the one default-win shape the panel and the queue row share.
    expect(body.decision).toBe('fusensho');
    // Tanaka occupies sideA of the remaining match, so decisionBy names AKA
    // -- the withdrawn competitor's side, exactly as fusenpai's did.
    expect(body.decisionBy).toBe('aka');
    expect(body.decisionReason).toBe('auto: Tanaka withdrawn');
    expect(password).toBe('secret');

    // The awarded match drops off the list; nothing left to walk.
    await waitFor(() => expect(screen.queryByText('Suzuki')).toBeNull());
    expect(screen.getByText('No remaining matches.')).toBeTruthy();
  });

  // bc-kfup: a kiken recorded as a correction can land after the withdrawn
  // competitor's next match has started. That match still has to be closed,
  // so the panel lists it; a completed one has nothing left to award.
  it('lists a remaining match that is already running, and never a completed one', async () => {
    window.compMatchesForCompetition = vi.fn().mockReturnValue([
      remaining,
      { ...remaining, id: 'Pool A-4', status: 'running', sideB: { id: 'p4', name: 'Watanabe' } },
      { ...remaining, id: 'Pool A-5', status: 'completed', sideB: { id: 'p5', name: 'Ito' } },
    ]);
    await recordTanakaKiken();
    await waitFor(() => expect(screen.getByText('Watanabe')).toBeTruthy());
    expect(screen.getByText('Suzuki')).toBeTruthy();
    expect(screen.queryByText('Ito')).toBeNull();
    expect(screen.getAllByText('Award default win to opponent')).toHaveLength(2);
  });

  // Review finding: a detail read that predates the kiken write still shows
  // the kiken's own match running. Listing it would offer a default win that
  // replaces the kiken and restores the competitor it barred.
  it('never lists the match the kiken was just recorded on', async () => {
    window.compMatchesForCompetition = vi.fn().mockReturnValue([
      remaining,
      { ...match, id: 'm-r1-0', status: 'running', phase: 'knockout' },
    ]);
    await recordTanakaKiken();
    await waitFor(() => expect(screen.getByText('Suzuki')).toBeTruthy());
    expect(screen.getAllByText('Award default win to opponent')).toHaveLength(1);
  });

  // bc-kpnl: the kiken completes the match, so a host that follows live court
  // state (the shiaijo console) moves on and unmounts this editor, panel and
  // all. onWithdrawal is the host's cue to pin the match; the panel's close
  // is its onClose.
  it('tells the host a kiken landed, and the panel close calls the host onClose', async () => {
    const onWithdrawal = vi.fn();
    const onClose = vi.fn();
    await recordTanakaKiken({ onWithdrawal, onClose });
    expect(onWithdrawal).toHaveBeenCalledTimes(1);
    expect(onWithdrawal.mock.calls[0][0]).toMatchObject({ id: 'p2', name: 'Tanaka' });
    await waitFor(() => expect(screen.getByText('Suzuki')).toBeTruthy());
    expect(onClose).not.toHaveBeenCalled();
    const close = [...document.querySelectorAll('.remaining-matches button')].find((b) => b.textContent === '✕');
    await act(async () => { fireEvent.click(close); });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(document.querySelector('.remaining-matches')).toBeNull();
  });

  it('the team editor tells the host the same way', async () => {
    const onWithdrawal = vi.fn();
    const teamMatch = {
      id: 'm-t1', compId: 'comp1', status: 'running', phase: 'bracket', round: 'Round 1', court: 'A',
      compKind: 'team', teamSize: 3,
      sideA: { id: 't1', name: 'Kyoto' }, sideB: { id: 't2', name: 'Osaka' },
      subResults: [],
    };
    window.compMatchesForCompetition = vi.fn().mockReturnValue([]);
    await act(async () => {
      render(<ScoreEditorModal match={teamMatch} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)}
        onWithdrawal={onWithdrawal} password="secret" />);
    });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-kiken-voluntary-button')); });
    const record = [...document.querySelectorAll('.decision-prompt button')].find((b) => b.textContent === 'Record');
    await act(async () => { fireEvent.click(record); });
    await waitFor(() => expect(window.API.recordDecision).toHaveBeenCalledTimes(1));
    expect(onWithdrawal).toHaveBeenCalledTimes(1);
    expect(document.querySelector('.remaining-matches')).toBeTruthy();
  });

  it('shows no remaining matches when Tanaka has none scheduled', async () => {
    window.compMatchesForCompetition = vi.fn().mockReturnValue([]);
    await recordTanakaKiken();
    await waitFor(() => expect(screen.getByText('No remaining matches.')).toBeTruthy());
  });

  // bc-cse: notLandedBanner, not writeWasSuperseded alone -- that predicate
  // is TRUE for both the superseded AND clock_skew refusal, so a clock_skew
  // award used to read "a newer result for this match is already recorded",
  // which sends the operator to check a result that was never written.
  it('a clock_skew refusal on the award shows the clock copy, not the superseded one', async () => {
    await recordTanakaKiken();
    await waitFor(() => expect(screen.getByText('Suzuki')).toBeTruthy());

    window.API.recordDecision = vi.fn().mockResolvedValue({ applied: false, reason: 'clock_skew' });
    const award = screen.getByText('Award default win to opponent');
    await act(async () => { fireEvent.click(award); });

    await waitFor(() => expect(screen.getByText(/this device's clock was out of step with the server/)).toBeTruthy());
    expect(screen.queryByText(/a newer result for this match is already recorded/)).toBeNull();
    // Not dropped from the list: the operator can retry once resynced.
    expect(screen.getByText('Suzuki')).toBeTruthy();
  });
});
