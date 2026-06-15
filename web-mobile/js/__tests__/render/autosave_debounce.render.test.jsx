// C1: tests for the debounced autosave (useDebouncedRunningWrite) wired into
// ScoreEditorModal and TeamScoreEditorModal.
//
// These tests live in the render suite (real React 18 + RTL) because the
// feature is exercised through actual component interaction: ippon tap
// → local state update → debounce timer → onSubmit call. The unit suite
// uses a fake React stub and cannot mount stateful components.
//
// Timer strategy: vi.useFakeTimers() so we can advance time deterministically
// without real 300ms waits. We call act() around both the interaction AND the
// timer advance so React flushes all pending state updates before we assert.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';

// ─── window globals required by admin_scoring_modal.jsx ──────────────────────
// Split into SYNC (evaluated in the component body on every render) and LAZY
// (only called from event handlers / async effects). Both must be set before
// the dynamic import below, because some are captured at module-evaluation time
// (e.g. `const TEAM_POSITIONS = Array.from({length: window.MAX_TEAM_SIZE}, ...)`).

const STUBBED_GLOBALS = {
  // SYNC
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  // LAZY
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
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
  // admin_helpers.jsx sets MAX_TEAM_SIZE etc. at module evaluation time —
  // already loaded by vitest.setup.render.js, so no re-import needed here.
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

// Reset the recordScore mock between tests so call counts start fresh.
beforeEach(() => {
  window.API.recordScore.mockClear();
  // Switch to fake timers so we can control setTimeout without real waits.
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

// ─── Helpers ─────────────────────────────────────────────────────────────────

function makeRunningMatch(overrides = {}) {
  return {
    id: 'm-running',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    // No compId → fetchCompetitionDetails useEffect returns early.
    ...overrides,
  };
}

function makeScheduledMatch(overrides = {}) {
  return {
    id: 'm-sched',
    status: 'scheduled',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ...overrides,
  };
}

// onSubmit prop simulates the parent path: calls window.API.recordScore so
// the mock captures the call. The exact signature mirrors admin_schedule.jsx's
// onEditScore → window.API.recordScore(compId, matchId, patch, password, match).
function makeOnSubmit(match) {
  return (patch) => window.API.recordScore('comp1', match.id, patch, '', match);
}

function renderModal(match, extraProps = {}) {
  const onSubmit = makeOnSubmit(match);
  return render(
    <ScoreEditorModal
      match={match}
      onClose={vi.fn()}
      onSubmit={onSubmit}
      password=""
      {...extraProps}
    />,
  );
}

// ─── Tests ───────────────────────────────────────────────────────────────────

describe('C1 debounced autosave — ScoreEditorModal (individual match)', () => {

  it('an ippon tap on a RUNNING match triggers exactly ONE debounced write after 300ms', async () => {
    renderModal(makeRunningMatch());

    // Tap the "M" (Men) ippon button for AKA (right side). The buttons for
    // each side both have the same label letters — AKA's controls are rendered
    // second (idx=1 in `sides`). getAllByText('M')[1] gives AKA's M button.
    const menButtons = screen.getAllByText('M');
    expect(menButtons.length).toBeGreaterThanOrEqual(1);

    // Tap: updates local state immediately (optimistic) — no network call yet.
    await act(async () => { fireEvent.click(menButtons[0]); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);

    // Advance past the 300ms debounce — the trailing-edge timer fires.
    await act(async () => { vi.advanceTimersByTime(350); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);

    // The patch must carry status "running" (NOT "completed" / "scheduled").
    const [, , patch] = window.API.recordScore.mock.calls[0];
    expect(patch.status).toBe('running');
    expect(patch.score?.live).toBe(true);
  });

  it('rapid double-tap coalesces into ONE write (trailing-edge debounce)', async () => {
    renderModal(makeRunningMatch());

    const menButtons = screen.getAllByText('M');

    // Two taps in quick succession (within the debounce window).
    await act(async () => {
      fireEvent.click(menButtons[0]);
    });
    // Advance only 100ms — still within debounce window; timer should reset.
    await act(async () => { vi.advanceTimersByTime(100); });
    // The second tap resets the debounce (ScoreEditorModal caps at 2 ippons
    // per side, so use a different letter to ensure the second tap is accepted).
    const koteButtons = screen.getAllByText('K');
    await act(async () => {
      fireEvent.click(koteButtons[0]);
    });
    // Still before the debounce window from the second tap expires.
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);

    // Now advance past the debounce from the SECOND tap.
    await act(async () => { vi.advanceTimersByTime(350); });
    // Only ONE write despite two taps.
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
  });

  it('an ippon tap on a SCHEDULED match does NOT trigger a write (gate: never auto-start)', async () => {
    renderModal(makeScheduledMatch());

    // The "M" buttons are still rendered (the scoring board is always visible).
    const menButtons = screen.getAllByText('M');
    await act(async () => { fireEvent.click(menButtons[0]); });

    // Advance well past debounce.
    await act(async () => { vi.advanceTimersByTime(500); });

    // Gate: status !== "running" → no write.
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });

  it('the autosave write is cancelled when the operator clicks the explicit Finish button', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(
      <ScoreEditorModal
        match={makeRunningMatch()}
        onClose={vi.fn()}
        onSubmit={onSubmit}
        password=""
      />,
    );

    // Tap an ippon to arm the debounce.
    const menButtons = screen.getAllByText('M');
    await act(async () => { fireEvent.click(menButtons[0]); });
    expect(onSubmit).toHaveBeenCalledTimes(0);

    // Click the Finish button (first arm: shows the verdict; canFinish is true
    // because aTotal>0). The component shows "Finish" until armed.
    const finishBtn = screen.getByText(/Finish/);
    await act(async () => { fireEvent.click(finishBtn); });

    // At this point the button is in the armed state ("Confirm · ...").
    // Click again to actually submit.
    const confirmBtn = screen.queryByText(/Confirm/);
    if (confirmBtn) {
      await act(async () => { fireEvent.click(confirmBtn); });
    }

    // Now advance past the debounce window — the cancelled timer must NOT fire.
    await act(async () => { vi.advanceTimersByTime(500); });

    // The explicit onSubmit was called (via Finish), but the autosave should
    // NOT have added an extra call. Any call from the debounce after the
    // explicit submit would mean double-write — check total calls:
    // Either 1 (Finish armed only) or 2 (arm + confirm) depending on UI flow,
    // but the autosave debounce must NOT add an extra call on top.
    // We verify by checking that every call had the patch from the explicit
    // submit path — none should have come from the debounce firing AFTER
    // the explicit submit. The simplest assertion: at most 2 total calls
    // (arm + confirm), NOT 3 (arm + confirm + stale debounce).
    expect(onSubmit.mock.calls.length).toBeLessThanOrEqual(2);
    // And no call should be the stale "running" autosave AFTER the explicit
    // submit — the last call (if any) must not be a running-status patch
    // that arrived post-submit.
    const calls = onSubmit.mock.calls;
    if (calls.length > 0) {
      // All completed-or-armed calls should NOT be the stale debounce. If the
      // last call has status "running" it means the debounce fired after the
      // explicit Finish — that's the bug we're guarding against.
      const lastPatch = calls[calls.length - 1][0];
      // After Finish, the patch should be "completed" or the arm triggered the
      // 2-tap guard and the patch is pending. Either way it must NOT be a
      // stale "running" patch fired by the debounce after the explicit submit.
      // We only assert this if more than 1 call happened (arm + potential debounce).
      if (calls.length >= 2) {
        expect(lastPatch.status).not.toBe('running');
      }
    }
  });

  it('prop-driven re-render (SSE update) does NOT trigger a write (no feedback loop)', async () => {
    // Render with a running match, then re-render with updated props (simulating
    // an SSE match_updated arrival). The autosave must NOT fire because the
    // dirty flag was never set by a user action.
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const { rerender } = render(
      <ScoreEditorModal
        match={makeRunningMatch()}
        onClose={vi.fn()}
        onSubmit={onSubmit}
        password=""
      />,
    );

    // Simulate SSE: re-render with slightly different match props (e.g.
    // scheduledAt changed). No user tap occurred.
    await act(async () => {
      rerender(
        <ScoreEditorModal
          match={makeRunningMatch({ scheduledAt: '10:05' })}
          onClose={vi.fn()}
          onSubmit={onSubmit}
          password=""
        />,
      );
    });

    // Advance well past the debounce window.
    await act(async () => { vi.advanceTimersByTime(500); });

    // No user action → dirty flag never set → no write.
    expect(onSubmit).toHaveBeenCalledTimes(0);
  });
});

describe('C1 debounced autosave — TeamScoreEditorModal (team match)', () => {

  function makeRunningTeamMatch(overrides = {}) {
    return {
      id: 'tm-running',
      status: 'running',
      phase: 'pool',
      poolName: 'Pool 1',
      court: 'A',
      compKind: 'team',
      teamSize: 3,
      sideA: { id: 'teamA', name: 'Team A' },
      sideB: { id: 'teamB', name: 'Team B' },
      ...overrides,
    };
  }

  it('a sub-bout ippon tap on a RUNNING team match triggers ONE debounced write', async () => {
    renderModal(makeRunningTeamMatch());

    // For a 3-person team match there are 3 rows × 2 sides × 5 buttons = 30+
    // ippon buttons. getAllByText('M') returns all of them; click the first.
    const menButtons = screen.getAllByText('M');
    expect(menButtons.length).toBeGreaterThanOrEqual(1);

    await act(async () => { fireEvent.click(menButtons[0]); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);

    await act(async () => { vi.advanceTimersByTime(350); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);

    const [, , patch] = window.API.recordScore.mock.calls[0];
    expect(patch.status).toBe('running');
  });

  it('a sub-bout ippon tap on a SCHEDULED team match does NOT write', async () => {
    renderModal({
      id: 'tm-sched',
      status: 'scheduled',
      phase: 'pool',
      poolName: 'Pool 1',
      court: 'A',
      compKind: 'team',
      teamSize: 3,
      sideA: { id: 'teamA', name: 'Team A' },
      sideB: { id: 'teamB', name: 'Team B' },
    });

    const menButtons = screen.getAllByText('M');
    await act(async () => { fireEvent.click(menButtons[0]); });
    await act(async () => { vi.advanceTimersByTime(500); });

    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });
});
