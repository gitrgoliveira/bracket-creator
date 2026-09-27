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
import { installWindowStubs } from '../helpers/stub_globals.js';
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';

// window globals required by admin_scoring_modal.jsx
// Split into SYNC (evaluated in the component body on every render) and LAZY
// (only called from event handlers or async effects). Both must be set before
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
    // A landed body: saveRunningSheet (admin_scoring_team.jsx) and Start
    // match read a falsy result as the host having already reported a
    // failure, so the default here must be truthy.
    recordScore: vi.fn().mockResolvedValue({}),
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

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  // admin_helpers.jsx sets MAX_TEAM_SIZE etc. at module evaluation time;
  // already loaded by vitest.setup.render.js, so no re-import needed here.
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

// Reset the recordScore mock between tests so call counts start fresh.
beforeEach(() => {
  window.API.recordScore.mockClear();
  // Switch to fake timers so we can control setTimeout without real waits.
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

// Helpers

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

// Tests

describe('C1 debounced autosave: ScoreEditorModal (individual match)', () => {

  it('an ippon tap on a RUNNING match triggers exactly ONE debounced write after 300ms', async () => {
    renderModal(makeRunningMatch());

    // Tap an "M" (Men) ippon button. Both sides render an identical M button;
    // this test is side-agnostic: any ippon tap on a running match must
    // schedule exactly one debounced autosave, so we click the first one.
    const menButtons = screen.getAllByText('M');
    expect(menButtons.length).toBeGreaterThanOrEqual(1);

    // Tap: updates local state immediately (optimistic). No network call yet.
    await act(async () => { fireEvent.click(menButtons[0]); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);

    // Advance past the 300ms debounce; the trailing-edge timer fires.
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);

    // The patch must carry status "running" (NOT "completed" / "scheduled").
    const [, , patch] = window.API.recordScore.mock.calls[0];
    expect(patch.status).toBe('running');
    expect(patch.score?.live).toBe(true);
  });

  it('does NOT fire a running autosave if the match completes during the debounce window', async () => {
    const running = makeRunningMatch();
    const { rerender } = renderModal(running);

    const menButtons = screen.getAllByText('M');
    await act(async () => { fireEvent.click(menButtons[0]); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);

    // The match is completed out from under the operator (an SSE update or
    // another operator) BEFORE the 300ms debounce fires; re-render completed.
    const completed = { ...running, status: 'completed' };
    await act(async () => {
      rerender(
        <ScoreEditorModal
          match={completed}
          onClose={vi.fn()}
          onSubmit={makeOnSubmit(completed)}
          password=""
        />,
      );
    });

    // Advance past the debounce. The fire-time isRunning re-check (gate 3) must
    // suppress the now-stale running write so it can't regress the completed
    // result.
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });

  it('rapid double-tap coalesces into ONE write (trailing-edge debounce)', async () => {
    renderModal(makeRunningMatch());

    const menButtons = screen.getAllByText('M');

    // Two taps in quick succession (within the debounce window).
    await act(async () => {
      fireEvent.click(menButtons[0]);
    });
    // Advance only 100ms; still within debounce window. Timer should reset.
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
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
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

    // Click the Finish button (first arm: the label flips to the explicit
    // "Tap again to finish" instruction; canFinish is true because aTotal>0).
    const finishBtn = screen.getByText(/Finish/);
    await act(async () => { fireEvent.click(finishBtn); });

    // Armed state — the label is now "Tap again to finish". Tap it again to
    // actually submit; the explicit finish must CANCEL the pending autosave.
    const confirmBtn = screen.queryByText(/Tap again/);
    if (confirmBtn) {
      await act(async () => { fireEvent.click(confirmBtn); });
    }

    // Now advance past the debounce window; the cancelled timer must NOT fire.
    await act(async () => { vi.advanceTimersByTime(500); });

    // The explicit onSubmit was called (via Finish), but the autosave should
    // NOT have added an extra call. Any call from the debounce after the
    // explicit submit would mean double-write; check total calls:
    // Either 1 (Finish armed only) or 2 (arm + confirm) depending on UI flow,
    // but the autosave debounce must NOT add an extra call on top.
    // We verify by checking that every call had the patch from the explicit
    // submit path; none should have come from the debounce firing AFTER
    // the explicit submit. The simplest assertion: at most 2 total calls
    // (arm + confirm), NOT 3 (arm + confirm + stale debounce).
    expect(onSubmit.mock.calls.length).toBeLessThanOrEqual(2);
    // And no call should be the stale "running" autosave AFTER the explicit
    // submit; the last call (if any) must not be a running-status patch
    // that arrived post-submit.
    const calls = onSubmit.mock.calls;
    if (calls.length > 0) {
      // All completed-or-armed calls should NOT be the stale debounce. If the
      // last call has status "running" it means the debounce fired after the
      // explicit Finish. That's the bug we're guarding against.
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

describe('C1 debounced autosave: TeamScoreEditorModal (team match)', () => {

  it('a sub-bout ippon tap on a RUNNING team match triggers ONE debounced write', async () => {
    renderModal(makeTeamMatch());

    // For a 3-person team match there are 3 rows × 2 sides × 5 buttons = 30+
    // ippon buttons. getAllByText('M') returns all of them; click the first.
    const menButtons = screen.getAllByText('M');
    expect(menButtons.length).toBeGreaterThanOrEqual(1);

    await act(async () => { fireEvent.click(menButtons[0]); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);

    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
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

// bc-rvfx: a tap that changes NOTHING must not reach the wire.
//
// addPt has always guarded its no-op (the side already at the 2-ippon cap);
// removePt did not, and an UNFILLED slot is still ENABLED -- the grid disables
// only on decidedByHantei, and the slot's own aria-label announces it as
// "empty". So tapping one filtered nothing out, marked dirty anyway, and 300ms
// later sent a full running-match PUT stamped NOW.
//
// That is not merely wasteful. The write can beat another device's correctly
// entered result still sitting in its offline queue (stamped at ENQUEUE, so
// older) on ApplyByTimestamp, and that operator is then told
// {"applied": false, "reason": "superseded"} and specifically NOT to re-enter.
// A tap on an empty cell can therefore cost a real result.
describe('bc-rvfx: tapping an EMPTY ippon slot is a no-op', () => {

  it('does not schedule an autosave write', async () => {
    renderModal(makeRunningMatch());

    // Both sides start with two empty slots; the aria-label is the contract.
    const empties = screen.getAllByLabelText(/slot \d: empty/);
    expect(empties.length).toBeGreaterThanOrEqual(2);

    await act(async () => { fireEvent.click(empties[0]); });
    await act(async () => { vi.advanceTimersByTime(500); });

    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });

  it('still writes when the tap actually clears a scored mark', async () => {
    // The guard must not be over-broad: clearing a REAL mark is the documented
    // way to take a strike back, and it must still autosave.
    renderModal(makeRunningMatch());

    const menButtons = screen.getAllByText('M');
    await act(async () => { fireEvent.click(menButtons[0]); });
    await act(async () => { vi.advanceTimersByTime(500); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);

    // That slot now holds a mark, so its label changes from "empty" to "remove".
    const filled = screen.getAllByLabelText(/slot \d: remove /);
    expect(filled.length).toBeGreaterThanOrEqual(1);

    await act(async () => { fireEvent.click(filled[0]); });
    await act(async () => { vi.advanceTimersByTime(500); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(2);
  });
});

// bc-emsl: the team sheet's twin of the rule above. A tap on an empty mark
// slot, or on a default-win circle, went through setPts, which ends a Tie or a
// Fusensho and autosaves: one stray tap silently un-tied a bout on the server.
describe('bc-emsl: a team-sheet tap that clears nothing writes nothing', () => {
  it('tapping an EMPTY mark slot keeps the bout\'s Tie and sends nothing', async () => {
    renderModal(makeTeamMatch({ id: 'tm-emsl' }));
    await act(async () => { fireEvent.click(tieBoutButton(0)); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);

    const empty = markSlots(subMatchRows()[0], 'shiro').find((b) => b.textContent === '·');
    expect(empty).toBeTruthy();
    await act(async () => { fireEvent.click(empty); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(tieBoutButton(0).textContent).toContain('✓ Tie');
  });

  it('tapping a default-win circle keeps the Fusensho and sends nothing', async () => {
    renderModal(makeTeamMatch({ id: 'tm-emsl' }));
    const akaFusensho = subMatchRows()[0].querySelector('.team-sub-match__side--aka [data-testid="scoring-modal-fusensho-button"]');
    await act(async () => { fireEvent.click(akaFusensho); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);

    const circle = markSlots(subMatchRows()[0], 'aka').find((b) => b.textContent === '\u25CB');
    expect(circle).toBeTruthy();
    expect(circle.title).toBe('Default win: use Fusensho to undo');
    await act(async () => { fireEvent.click(circle); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(subMatchRows()[0].querySelector('.team-sub-match__side--aka [data-testid="scoring-modal-fusensho-button"]').textContent).toContain('✓ Fusensho');
  });

  it('still removes a real mark and autosaves', async () => {
    renderModal(makeTeamMatch({ id: 'tm-emsl' }));
    const shiroM = ipponButton(subMatchRows()[0], 'shiro', 'M');
    await act(async () => { fireEvent.click(shiroM); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);

    const mark = markSlots(subMatchRows()[0], 'shiro').find((b) => b.textContent === 'M');
    expect(mark.title).toBe('Click to remove');
    await act(async () => { fireEvent.click(mark); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(2);
    expect(markSlots(subMatchRows()[0], 'shiro').some((b) => b.textContent === 'M')).toBe(false);
  });
});

describe('an edit inside the autosave window survives Prev/Next', () => {
  // Prev/Next re-key the editor in its host, which unmounts it. Leaving a
  // RUNNING match (leaveEditor's common case) does not cancel the debounce
  // itself; the unmount effect inside useDebouncedRunningWrite is what fires
  // the pending write, the same mechanism the page-hide flush uses. An ippon
  // tapped just before either one used to be lost; now the unmount writes it.
  const neighbour = { id: 'm-next', sideA: { name: 'Sato' }, sideB: { name: 'Ito' } };
  const viaButton = async () => { fireEvent.click(screen.getByText('Next →')); };
  const viaKey = async () => { fireEvent.keyDown(window, { key: 'ArrowRight' }); };

  it.each([
    ['individual', 'the Next button', makeRunningMatch, viaButton],
    ['individual', 'the → key', makeRunningMatch, viaKey],
    ['team', 'the Next button', makeTeamMatch, viaButton],
    ['team', 'the → key', makeTeamMatch, viaKey],
  ])('%s editor: %s within 300ms of a tap writes the tap', async (_kind, _via, match, goNext) => {
    let view;
    const onNext = vi.fn(() => view.unmount());
    view = renderModal(match(), { nextMatch: neighbour, onNext });
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    await act(goNext);
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(onNext).toHaveBeenCalledTimes(1);
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(window.API.recordScore.mock.calls[0][2].status).toBe('running');
  });
});

// ---------------------------------------------------------------------------
// bc-dhas: Add/Remove representative bout races the pending autosave
// ---------------------------------------------------------------------------
//
// onDaihyosen (Add) and onRemoveDaihyosen (Remove) are explicit writes, like
// Finish, so each must cancel a pending debounced autosave FIRST (as
// doSubmit does) rather than let a stale timer fire afterward with a
// snapshot built before the row changed. Both directions raced: an Add
// followed at once by a scoring tap could have the tap's own stale write
// erase the just-added row (the bead's own repro); a Remove sent with an
// edit still pending could have that edit's later write resurrect the row
// the operator just removed (R1/R2 in the bead's plan).
//
// These mount the REAL TeamScoreEditorModal (via the ScoreEditorModal
// dispatcher, the same route every production mount site uses) under fake
// timers. `settle` flushes the handlers' internal awaits: Promise
// microtasks run for real even under vi.useFakeTimers(), so repeated
// `await act(async () => { await Promise.resolve(); })` ticks drain a
// chain of resolved-mock awaits without a real wait (the same idiom
// admin_shiaijo.render.test.jsx uses under fake timers; `waitFor`'s own
// polling never fires here since it schedules through the faked clock).

function makeTeamMatch(overrides = {}) {
  return {
    id: 'tm-running', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3,
    sideA: { id: 'teamA', name: 'Team A' }, sideB: { id: 'teamB', name: 'Team B' },
    ...overrides,
  };
}

// Knockout defaults layered on makeTeamMatch's pool-match shape: same team
// shape, a bracket match instead of a pool one.
function makeKnockoutTeamMatch(overrides = {}) {
  return makeTeamMatch({
    id: 'tm-ko', compId: 'comp1', phase: 'bracket', round: 'Final',
    compFormat: 'knockout', teamMatchType: 'fixed', subResults: [],
    ...overrides,
  });
}

function daihyosenRow(overrides = {}) {
  return {
    position: -1, sideA: 'Team A', sideB: 'Team B', ipponsA: [], ipponsB: [], winner: '',
    decision: 'daihyosen',
    ...overrides,
  };
}

function makeMatchWithDaihyosen(overrides = {}) {
  return makeKnockoutTeamMatch({ subResults: [daihyosenRow()], ...overrides });
}

async function settle(times = 8) {
  for (let i = 0; i < times; i++) {
    await act(async () => { await Promise.resolve(); });
  }
}

function subMatchRows() { return [...document.querySelectorAll('.team-sub-match')]; }
function tieBoutButton(idx) { return subMatchRows()[idx].querySelector('[data-testid="scoring-modal-tie-button"]'); }
function ipponButton(rowEl, color, letter) {
  return [...rowEl.querySelectorAll(`.team-sub-match__side--${color} button.ipt-btn`)].find((b) => b.textContent === letter);
}
function markSlots(rowEl, color) {
  return [...rowEl.querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)];
}
function markSlot(rowEl, color, letter) {
  return markSlots(rowEl, color).find((b) => b.textContent === letter);
}

describe('bc-dhas: Add/Remove representative bout races the pending autosave', () => {
  beforeEach(() => {
    // Reset per test: the file-level beforeEach only clears recordScore.
    window.API.recordDaihyosen = vi.fn();
    window.API.removeDaihyosen = vi.fn();
  });

  it('T1: Add inside the debounce window cancels the pending autosave first', async () => {
    window.API.recordDaihyosen.mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [daihyosenRow()] });
    renderModal(makeKnockoutTeamMatch());

    // Tie every bout, the last tap right before Add (mirrors the bead's own
    // three-Ties-then-Add repro): one pending autosave timer is left armed.
    await act(async () => { fireEvent.click(tieBoutButton(0)); });
    await act(async () => { fireEvent.click(tieBoutButton(1)); });
    await act(async () => { fireEvent.click(tieBoutButton(2)); });

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();

    // The explicit save landed; recordDaihyosen was posted.
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);

    // Red on e7cb3af0: the pending timer (armed by the last Tie, never
    // cancelled by onDaihyosen) fires here with a snapshot built before the
    // row existed.
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
  });

  it('T2: a tap right after the add carries the DH row (adopted via setMatchOverride)', async () => {
    window.API.recordDaihyosen.mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [daihyosenRow()] });
    renderModal(makeKnockoutTeamMatch());

    await act(async () => { fireEvent.click(tieBoutButton(2)); });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    window.API.recordScore.mockClear();

    // A tap made AFTER the add resolves: its debounced write, 350ms later,
    // must carry the newly adopted position -1 row.
    await act(async () => { fireEvent.click(tieBoutButton(0)); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const [, , patch] = window.API.recordScore.mock.calls[0];
    // Red on e7cb3af0: matchOverride is never set, so the local board still
    // has no daihyosen row (and the Add button, not Remove, would still show).
    expect(patch.subResults.some((s) => s.position === -1)).toBe(true);
  });

  it('T2b: a tap made while the add request is still in flight is kept, with the row', async () => {
    let view;
    // Same pattern as "an edit inside the autosave window survives Prev/Next"
    // above: a host whose onClose unmounts the editor.
    const onClose = vi.fn(() => view.unmount());
    let resolveDaihyosen;
    window.API.recordDaihyosen.mockImplementation(() => new Promise((resolve) => { resolveDaihyosen = resolve; }));
    view = renderModal(makeKnockoutTeamMatch(), { onClose });

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle(); // the explicit save lands; recordDaihyosen is still pending

    // A tap made WHILE the POST is in flight.
    await act(async () => { fireEvent.click(tieBoutButton(0)); });

    await act(async () => {
      resolveDaihyosen({ ...makeKnockoutTeamMatch(), subResults: [daihyosenRow()] });
    });
    await settle();

    // Red on e7cb3af0: onDaihyosen calls onClose() on success, which
    // unmounts this host; the unmount effect then fires the pending tap
    // built from the pre-adopt state (no row).
    expect(onClose).not.toHaveBeenCalled();

    window.API.recordScore.mockClear();
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();

    expect(onClose).not.toHaveBeenCalled();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const [, , patch] = window.API.recordScore.mock.calls[0];
    expect(patch.subResults.some((s) => s.position === -1)).toBe(true);
    expect(patch.subResults.find((s) => s.position === 1)?.decision).toBe('hikiwake');
  });

  it('F6: adoptServerSubs builds the override from the LATEST match prop, not the one captured at click time', async () => {
    let resolveDaihyosen;
    window.API.recordDaihyosen.mockImplementation(() => new Promise((resolve) => { resolveDaihyosen = resolve; }));
    const m1 = makeKnockoutTeamMatch();
    const { rerender } = renderModal(m1);

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle(); // the pre-save lands; recordDaihyosen is still pending

    // The parent catches up BEFORE the POST resolves (an SSE push racing it):
    // the dh row is already on the prop by the time the request answers.
    const dh = daihyosenRow();
    const m2 = { ...m1, subResults: [dh] };
    await act(async () => {
      rerender(<ScoreEditorModal match={m2} onClose={vi.fn()} onSubmit={makeOnSubmit(m2)} password="" />);
    });

    // The request finally resolves with the SAME row the prop already
    // carries: adoptServerSubs's closure over the ORIGINAL `match` (m1,
    // captured when onDaihyosen was created at click time) is now two
    // renders stale.
    await act(async () => { resolveDaihyosen({ subResults: [dh] }); });
    await settle();

    // The parent then finishes the match: SAME subResults as m2 (only
    // status/winner differ), so matchSubsKey -- subResults content only --
    // does not change between m2 and m3. If a stale override is already
    // shadowing the prop, the override-clearing effect has no key change to
    // fire on and never removes it.
    const m3 = { ...m2, status: 'completed', winner: 'Team A' };
    await act(async () => {
      rerender(<ScoreEditorModal match={m3} onClose={vi.fn()} onSubmit={makeOnSubmit(m3)} password="" />);
    });

    // Red without the fix: adoptServerSubs reverts every field but
    // subResults to m1 (status: "running"), so the editor keeps showing the
    // running UI (no CORRECTION pill) even though the match completed.
    expect(screen.queryByText('CORRECTION')).toBeTruthy();
  });

  it('A6: a scoring tap mid-request holds the autosave until the request resolves and the row is adopted', async () => {
    let resolveDaihyosen;
    window.API.recordDaihyosen.mockImplementation(() => new Promise((resolve) => { resolveDaihyosen = resolve; }));
    renderModal(makeKnockoutTeamMatch());

    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle(); // the pre-save lands; recordDaihyosen is still pending
    window.API.recordScore.mockClear();

    // A tap made WHILE the POST is in flight arms the debounce.
    await act(async () => { fireEvent.click(tieBoutButton(0)); });

    // The debounce outlives the request: without the hold, this is exactly
    // where the timer fires with a patch built before adoptServerSubs ran.
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    // Red without the hold: the timer already fired here and sent a patch
    // with no position -1 row.
    expect(window.API.recordScore).not.toHaveBeenCalled();

    await act(async () => {
      resolveDaihyosen({ ...makeKnockoutTeamMatch(), subResults: [daihyosenRow()] });
    });
    await settle();

    // The FIRST write after the request resolves carries the row: the
    // deferred edit fires once the render adopting it has landed.
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const [, , patch] = window.API.recordScore.mock.calls[0];
    expect(patch.subResults.some((s) => s.position === -1)).toBe(true);
  });

  it('T3a: Remove inside the debounce window stays removed (onClose does not unmount)', async () => {
    window.API.removeDaihyosen.mockResolvedValue({ subResults: [] });
    const onClose = vi.fn();
    renderModal(makeMatchWithDaihyosen(), { onClose });

    // A point on bout 1 (not the DH row) arms the debounce; Remove at once.
    await act(async () => { fireEvent.click(ipponButton(subMatchRows()[0], 'shiro', 'M')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();

    // Red on e7cb3af0: onRemoveDaihyosen never saves first, so this is 0.
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(window.API.removeDaihyosen).toHaveBeenCalledTimes(1);
    const saveOrder = window.API.recordScore.mock.invocationCallOrder[0];
    const deleteOrder = window.API.removeDaihyosen.mock.invocationCallOrder[0];
    expect(saveOrder).toBeLessThan(deleteOrder);
    // Red on e7cb3af0: the old code always calls onClose() after a
    // successful remove.
    expect(onClose).not.toHaveBeenCalled();

    // The debounce was cancelled before the pre-remove save, so nothing is
    // left pending to fire a stale write after the delete.
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('T3b: Remove inside the debounce window stays removed (onClose unmounts the host)', async () => {
    window.API.removeDaihyosen.mockResolvedValue({ subResults: [] });
    let view;
    const onClose = vi.fn(() => view.unmount());
    view = renderModal(makeMatchWithDaihyosen(), { onClose });

    await act(async () => { fireEvent.click(ipponButton(subMatchRows()[0], 'shiro', 'M')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(window.API.removeDaihyosen).toHaveBeenCalledTimes(1);
    const saveOrder = window.API.recordScore.mock.invocationCallOrder[0];
    const deleteOrder = window.API.removeDaihyosen.mock.invocationCallOrder[0];
    expect(saveOrder).toBeLessThan(deleteOrder);
    // Red on e7cb3af0: onClose() unmounts this host at once, and the
    // unmount's own write (built from the pre-removal state) resurrects
    // the row server-side (R1).
    expect(onClose).not.toHaveBeenCalled();

    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('T4: a pending DH point take-back is saved before the DELETE', async () => {
    window.API.removeDaihyosen.mockResolvedValue({ subResults: [] });
    const match = makeMatchWithDaihyosen();
    const { rerender } = renderModal(match);
    const dhRowEl = () => subMatchRows()[3]; // bout1, bout2, bout3, then DH

    // Strike a point on the DH row itself and let it autosave.
    await act(async () => { fireEvent.click(ipponButton(dhRowEl(), 'shiro', 'M')); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const [, , firstPatch] = window.API.recordScore.mock.calls[0];
    const dhAfterStrike = firstPatch.subResults.find((s) => s.position === -1);

    // The server round-trips the struck point back as the new baseline.
    const recordedMatch = makeMatchWithDaihyosen({ subResults: [dhAfterStrike] });
    await act(async () => {
      rerender(
        <ScoreEditorModal match={recordedMatch} onClose={vi.fn()} onSubmit={makeOnSubmit(recordedMatch)} password="" />
      );
    });
    window.API.recordScore.mockClear();

    // Take the point back, then Remove at once.
    await act(async () => { fireEvent.click(markSlot(dhRowEl(), 'shiro', 'M')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();

    // Red on e7cb3af0: the DELETE goes first, so the take-back is lost.
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const [, , patch] = window.API.recordScore.mock.calls[0];
    const dh = patch.subResults.find((s) => s.position === -1);
    expect(dh.ipponsA).toEqual([]);
    expect(dh.ipponsB).toEqual([]);
    const saveOrder = window.API.recordScore.mock.invocationCallOrder[0];
    const deleteOrder = window.API.removeDaihyosen.mock.invocationCallOrder[0];
    expect(saveOrder).toBeLessThan(deleteOrder);
  });

  it('T4b: a pending DH hantei cancel is saved before the DELETE (markless arrays, not omitted)', async () => {
    window.API.removeDaihyosen.mockResolvedValue({ subResults: [] });
    const match = makeMatchWithDaihyosen();
    const { rerender } = renderModal(match);

    // Pick a hantei winner (SHIRO = pickDaihyosenHantei("b") = sideB) and
    // let it autosave.
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-hantei-arm')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-hantei-shiro')); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);

    // The server round-trips the recorded verdict back as the new baseline
    // (the normalizeMatch-derived shape every real mount receives, same
    // fixture convention as team_daihyosen_silence.render.test.jsx).
    const recordedMatch = makeMatchWithDaihyosen({
      subResults: [daihyosenRow({ winner: 'Team B', ipponsB: ['Ht'], decidedByHantei: true })],
    });
    await act(async () => {
      rerender(
        <ScoreEditorModal match={recordedMatch} onClose={vi.fn()} onSubmit={makeOnSubmit(recordedMatch)} password="" />
      );
    });
    window.API.recordScore.mockClear();

    // Cancel the verdict, then Remove at once.
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-hantei-cancel')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();

    // Red on e7cb3af0: the DELETE goes first, so the cancel is lost.
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const [, , patch] = window.API.recordScore.mock.calls[0];
    const dh = patch.subResults.find((s) => s.position === -1);
    // Touched (armed flipped true -> false): explicit arrays, not omitted
    // (the daihyosenSilent branch is for a row nothing is known about).
    expect('ipponsA' in dh).toBe(true);
    expect('ipponsB' in dh).toBe(true);
    expect(dh.ipponsA.includes('Ht')).toBe(false);
    expect(dh.ipponsB.includes('Ht')).toBe(false);
    const saveOrder = window.API.recordScore.mock.invocationCallOrder[0];
    const deleteOrder = window.API.removeDaihyosen.mock.invocationCallOrder[0];
    expect(saveOrder).toBeLessThan(deleteOrder);
  });
});
