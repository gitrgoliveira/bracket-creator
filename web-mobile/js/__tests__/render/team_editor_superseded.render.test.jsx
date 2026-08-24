// bc-lww1: the team score editor had NO not-saved surface at all.
//
// Two separate gaps met here. TeamScoreEditorModal never subscribed to the
// terminal-write-failed channel (0 occurrences), and the individual editor,
// which does subscribe, early-returns <TeamScoreEditorModal> BEFORE its own
// banner JSX - so its subscription fired, set state, and rendered nothing.
//
// On top of that, "Start match" and kachinuki "Record bout" submit with
// status:"running", and recordScore deliberately stays SILENT on a superseded
// running write (a 300ms debounced autosave being superseded is routine noise
// and would otherwise produce a toast storm). Those two are explicit operator
// taps rather than autosaves, so they check the awaited result themselves.
//
// Net effect before the fix: the operator tapped, the server stored nothing
// because another device held a newer result, and the button simply re-enabled
// with no banner, no toast and no state change.

import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { writeWasSuperseded, SUPERSEDED_REASON, SUPERSEDED_ADVICE } from '../../write_result.jsx';

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
  // The REAL predicate and the REAL copy: the point of the test is that the
  // call site consults them, so stubbing them would prove nothing.
  writeWasSuperseded,
  SUPERSEDED_REASON,
  SUPERSEDED_ADVICE,
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
      config: { format: 'playoffs', teamMatchType: 'fixed', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    reopenMatch: vi.fn(),
  };
});

// SCHEDULED, so the editor offers "Start match" - the write that goes out as
// status:"running" and is therefore not announced by the shared channel.
function scheduledTeamMatch(overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'scheduled',
    phase: 'bracket',
    round: 'Final',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    compFormat: 'playoffs',
    teamMatchType: 'fixed',
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    subResults: [],
    ...overrides,
  };
}

async function renderEditor(onSubmit, props = {}) {
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal
        match={scheduledTeamMatch()}
        onClose={vi.fn()}
        onSubmit={onSubmit}
        password="secret"
        {...props}
      />
    );
  });
  return utils;
}

describe('team score editor: a superseded explicit tap reaches the screen (bc-lww1)', () => {
  it('shows the not-saved banner when Start match is superseded', async () => {
    // 200 {applied:false}: another device already holds a newer result, so the
    // server stored nothing and this tap changed no state anywhere.
    const onSubmit = vi.fn().mockResolvedValue({ applied: false, reason: 'superseded' });
    await renderEditor(onSubmit);

    await act(async () => { fireEvent.click(screen.getByText('Start match')); });

    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain('Not saved');
      // The advice is the load-bearing half: every OTHER write failure ends in
      // "re-enter the result", which here would overwrite the newer result that
      // won. The operator has to look at what is recorded first.
      expect(alert.textContent).toContain('Check the recorded result');
      expect(alert.textContent).not.toContain('Re-enter the result and submit again');
    });
  });

  it('shows no banner when the write lands', async () => {
    // The counter-case: without it the fix could "pass" by always warning.
    const onSubmit = vi.fn().mockResolvedValue({ id: 'm1', status: 'running' });
    await renderEditor(onSubmit);

    await act(async () => { fireEvent.click(screen.getByText('Start match')); });

    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('surfaces a permanently-rejected queued write through the shared channel', async () => {
    // The other half of the gap: this editor never subscribed at all, so a
    // terminal write rejected long after the fact went unreported here even
    // though the individual and engi editors both show it.
    // Collect EVERY subscriber, not just the last. Both editors subscribe here
    // (ScoreEditorModal wraps the team one and keeps its own subscription even
    // though it early-returns before its banner), and React runs child effects
    // before parent ones - so keeping only the most recent callback would
    // deliver to the individual editor's dead listener and prove nothing.
    const subscribers = [];
    window.subscribeTerminalWriteFailed = (fn) => { subscribers.push(fn); return () => {}; };
    try {
      await renderEditor(vi.fn().mockResolvedValue(undefined));
      expect(subscribers.length).toBeGreaterThan(0);

      await act(async () => {
        for (const fn of subscribers) {
          fn({ compID: 'comp1', matchID: 'm1', kind: 'score', status: 400, reason: 'save rejected' });
        }
      });

      await waitFor(() => {
        expect(screen.getByRole('alert').textContent).toContain('Not saved: save rejected');
      });
    } finally {
      delete window.subscribeTerminalWriteFailed;
    }
  });

  it('ignores a failure reported for a DIFFERENT match', async () => {
    const subscribers = [];
    window.subscribeTerminalWriteFailed = (fn) => { subscribers.push(fn); return () => {}; };
    try {
      await renderEditor(vi.fn().mockResolvedValue(undefined));
      await act(async () => {
        for (const fn of subscribers) {
          fn({ compID: 'comp1', matchID: 'someone-else', kind: 'score', status: 400, reason: 'nope' });
        }
      });
      expect(screen.queryByRole('alert')).toBeNull();
    } finally {
      delete window.subscribeTerminalWriteFailed;
    }
  });
});
