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
import { writeWasSuperseded, writeWasRefusedForClock, SUPERSEDED_REASON, SUPERSEDED_ADVICE, CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE } from '../../write_result.jsx';

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
  writeWasRefusedForClock,
  CLOCK_SKEW_REASON_TEXT,
  CLOCK_SKEW_ADVICE,
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

// The finish confirmation is a two-tap control: the first tap arms it ("Tap
// again to finish"), the second submits. A submit that comes back superseded
// used to leave it ARMED, so the operator was one tap from re-sending -- which
// is the one thing the banner tells them not to do, and which would WIN,
// because a re-submit carries a fresh stamp and beats the newer result it
// overwrites. Found in browser verification, not by any test.
describe('team score editor: a failed write disarms the finish confirmation', () => {
  function runningTeamMatch() {
    return {
      id: 'm1', compId: 'comp1', status: 'running', phase: 'bracket',
      round: 'Final', court: 'A', compKind: 'team', teamSize: 3,
      compFormat: 'playoffs', teamMatchType: 'fixed',
      sideA: { id: 'team-A', name: 'Team A' },
      sideB: { id: 'team-B', name: 'Team B' },
      subResults: [
        { position: 1, sideA: 'Team A', sideB: 'Team B', ipponsA: ['M', 'M'], ipponsB: [], winner: 'Team A' },
      ],
    };
  }

  it('reverts an armed finish button when the write is reported not saved', async () => {
    const subscribers = [];
    window.subscribeTerminalWriteFailed = (fn) => { subscribers.push(fn); return () => {}; };
    try {
      await act(async () => {
        render(
          <ScoreEditorModal
            match={runningTeamMatch()}
            onClose={vi.fn()}
            onSubmit={vi.fn().mockResolvedValue(undefined)}
            password="secret"
          />
        );
      });

      const finish = () => [...document.querySelectorAll('button')]
        .find((b) => /Finish|Tap again to finish/.test(b.textContent || ''));
      expect(finish()).toBeTruthy();

      // Arm it, exactly as the operator's first tap does.
      await act(async () => { fireEvent.click(finish()); });
      expect(finish().textContent).toMatch(/Tap again to finish/);

      // The write comes back not saved.
      await act(async () => {
        for (const fn of subscribers) {
          fn({ compID: 'comp1', matchID: 'm1', kind: 'score', status: 200, reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE });
        }
      });

      // Disarmed: re-arming has to be a deliberate act again.
      expect(finish().textContent).not.toMatch(/Tap again to finish/);
      expect(screen.getByRole('alert').textContent).toContain('Not saved');
    } finally {
      delete window.subscribeTerminalWriteFailed;
    }
  });
});

// The team editor renders SyncStatusPill, but that pill deliberately shows
// nothing once a match is finished - so a COMPLETED team result that could only
// be queued (flaky connection) sat on screen looking saved, with nothing saying
// it had not reached the server. That is the moment an operator is most likely
// to walk away from the court believing the result is in.
describe('team score editor: a queued completed write says so', () => {
  it('shows the not-sent-yet banner when the write was only queued', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ queued: true });
    await renderEditor(onSubmit);

    await act(async () => { fireEvent.click(screen.getByText('Start match')); });

    const banner = await screen.findByRole('status');
    expect(banner.textContent).toContain('Not sent yet');
    // Distinct from the refused case: this one WILL sync, so it must not tell
    // the operator to go and check what is recorded.
    expect(banner.textContent).not.toContain('Check the recorded result');
  });

  it('shows no pending banner when the write lands', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ id: 'm1', status: 'running' });
    await renderEditor(onSubmit);
    await act(async () => { fireEvent.click(screen.getByText('Start match')); });
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('a refusal replaces the pending banner rather than stacking with it', async () => {
    // Both can be true in sequence for one write: queued first, refused later
    // when it finally reaches the server. Two banners saying different things
    // about the same result would be worse than either alone.
    const subscribers = [];
    window.subscribeTerminalWriteFailed = (fn) => { subscribers.push(fn); return () => {}; };
    try {
      const onSubmit = vi.fn().mockResolvedValue({ queued: true });
      await renderEditor(onSubmit);
      await act(async () => { fireEvent.click(screen.getByText('Start match')); });
      expect(await screen.findByRole('status')).toBeTruthy();

      await act(async () => {
        for (const fn of subscribers) {
          fn({ compID: 'comp1', matchID: 'm1', kind: 'score', status: 200, reason: SUPERSEDED_REASON, advice: SUPERSEDED_ADVICE });
        }
      });

      expect(screen.getByRole('alert').textContent).toContain('Not saved');
      expect(screen.queryByRole('status')).toBeNull();
    } finally {
      delete window.subscribeTerminalWriteFailed;
    }
  });
});

// A clock_skew refusal and a superseded drop are BOTH applied:false, and until
// this branch existed every surface showed the superseded copy for both. That
// copy is actively harmful for a clock refusal: it says "check the recorded
// result / do not re-submit" when nothing was recorded and re-submitting is
// exactly the remedy (the client has already resynced its clock). These pin
// the split at the editor, which no unit test reaches - the api_client tests
// stop at the response, and this branch lives in the component.
describe('team score editor: a clock refusal shows the clock copy, not the superseded copy', () => {
  it('Start match refused for clock_skew names the clock and invites re-entry', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: false, reason: 'clock_skew', serverNowMs: 1 });
    await renderEditor(onSubmit);

    await act(async () => { fireEvent.click(screen.getByText('Start match')); });

    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain(CLOCK_SKEW_REASON_TEXT);
      expect(alert.textContent).toContain(CLOCK_SKEW_ADVICE);
      // The load-bearing negative: the superseded advice warns AGAINST the
      // very action the clock copy asks for, so leaking it here sends the
      // operator hunting for a recorded result that does not exist.
      expect(alert.textContent).not.toContain(SUPERSEDED_ADVICE);
    });
  });

  it('a superseded refusal still shows the superseded copy (the branch is additive)', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: false, reason: 'superseded' });
    await renderEditor(onSubmit);
    await act(async () => { fireEvent.click(screen.getByText('Start match')); });
    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain(SUPERSEDED_REASON);
      expect(alert.textContent).not.toContain(CLOCK_SKEW_REASON_TEXT);
    });
  });
});
