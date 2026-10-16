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
import { beforeEach as beforeEachTest } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

// bc-evts stage 3: a running individual match is scored by taps handed to
// the outbox (API.recordTap); each test gets a fresh fake of that surface.
beforeEachTest(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';
import { CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE } from '../../write_result.jsx';

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
  // bc-evts stage 3: the individual editor has no debounced autosave. A tap on
  // a running match is handed to the outbox at once (API.recordTap) and no
  // running score write is ever sent; the team and engi editors below keep
  // theirs until stages 4 and 5.
  const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);

  it('an ippon tap on a RUNNING match is one tap, sent at once, and never a running write', async () => {
    renderModal(makeRunningMatch());
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    expect(taps()).toEqual([expect.objectContaining({ kind: 'ippon.add', letter: 'M' })]);
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });

  it('a match completed right after a tap gets no running write', async () => {
    const running = makeRunningMatch();
    const { rerender } = renderModal(running);
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    const completed = { ...running, status: 'completed' };
    await act(async () => {
      rerender(<ScoreEditorModal match={completed} onClose={vi.fn()} onSubmit={makeOnSubmit(completed)} password="" />);
    });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });

  it('two quick taps are two taps, each its own event', async () => {
    renderModal(makeRunningMatch());
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    await act(async () => { vi.advanceTimersByTime(100); });
    await act(async () => { fireEvent.click(screen.getAllByText('K')[0]); });
    expect(taps().map((e) => e.letter)).toEqual(['M', 'K']);
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });

  it('an ippon tap on a SCHEDULED match sends nothing (gate: never auto-start)', async () => {
    renderModal(makeScheduledMatch());
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    await act(async () => { vi.advanceTimersByTime(500); });
    expect(taps()).toEqual([]);
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });

  it('Finish after a tap sends the one finishing write', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<ScoreEditorModal match={makeRunningMatch()} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    await act(async () => { fireEvent.click(screen.getByText(/Finish/)); });
    await act(async () => { fireEvent.click(screen.getByText(/Tap again/)); });
    await act(async () => { vi.advanceTimersByTime(500); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].status).toBe('completed');
  });

  it('prop-driven re-render (SSE update) sends nothing (no feedback loop)', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const { rerender } = render(<ScoreEditorModal match={makeRunningMatch()} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    await act(async () => {
      rerender(<ScoreEditorModal match={makeRunningMatch({ scheduledAt: '10:05' })} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    });
    await act(async () => { vi.advanceTimersByTime(500); });
    expect(onSubmit).toHaveBeenCalledTimes(0);
    expect(taps()).toEqual([]);
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

  it('sends nothing', async () => {
    renderModal(makeRunningMatch());

    // Both sides start with two empty slots; the aria-label is the contract.
    const empties = screen.getAllByLabelText(/slot \d: empty/);
    expect(empties.length).toBeGreaterThanOrEqual(2);

    await act(async () => { fireEvent.click(empties[0]); });
    await act(async () => { vi.advanceTimersByTime(500); });

    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
    expect(window.API.recordTap).toHaveBeenCalledTimes(0);
  });

  it('still sends a removal when the tap actually clears a scored mark', async () => {
    // The guard must not be over-broad: clearing a REAL mark is the documented
    // way to take a strike back. bc-evts stage 3: it is an ippon.remove tap
    // naming the mark (here the pending tap's own id).
    renderModal(makeRunningMatch());

    const menButtons = screen.getAllByText('M');
    await act(async () => { fireEvent.click(menButtons[0]); });
    expect(window.API.recordTap).toHaveBeenCalledTimes(1);
    const { id } = window.API.recordTap.mock.results[0].value;

    // That slot now holds a mark, so its label changes from "empty" to "remove".
    const filled = screen.getAllByLabelText(/slot \d: remove /);
    expect(filled.length).toBeGreaterThanOrEqual(1);

    await act(async () => { fireEvent.click(filled[0]); });
    expect(window.API.recordTap).toHaveBeenCalledTimes(2);
    expect(window.API.recordTap.mock.calls[1][2]).toMatchObject({ kind: 'ippon.remove', target: id });
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
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
    expect(circle.title).toBe('Awarded by fusensho: use Fusensho to undo');
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

  // bc-evts stage 3: the individual editor's tap is in the outbox as it is
  // made, so leaving by Next or → owes nothing and writes nothing.
  it.each([
    ['the Next button', viaButton],
    ['the → key', viaKey],
  ])('individual editor: %s right after a tap keeps the tap and writes nothing', async (_via, goNext) => {
    let view;
    const onNext = vi.fn(() => view.unmount());
    view = renderModal(makeRunningMatch(), { nextMatch: neighbour, onNext });
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    await act(goNext);
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(onNext).toHaveBeenCalledTimes(1);
    expect(window.API.recordTap).toHaveBeenCalledTimes(1);
    expect(window.API.recordScore).toHaveBeenCalledTimes(0);
  });

  it.each([
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

  it('adoptServerSubs builds the override from the LATEST match prop, not the one captured at click time', async () => {
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

// ---------------------------------------------------------------------------
// bc-dhas: the add and remove are stamped, so their outcome settles the hold
// ---------------------------------------------------------------------------

// The representative-bout row exactly as the add returns it (the handler's
// placeholder, pinned by TestDaihyosenRowAsTheAddReturnsItSurvivesAScoreWrite).
function serverDaihyosenRow() {
  return { position: -1, sideA: '', sideB: '', ipponsA: null, ipponsB: null, hansokuA: 0, hansokuB: 0, winner: '', decision: 'daihyosen' };
}

function failedBanner() { return document.querySelector('.pending-write-banner--failed'); }

// A request the test answers when it chooses.
function deferred() {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

// A running copy older than the add's answer (a push or refetch that read the
// data before the add committed) is not the match after it, whatever its log.
describe('bc-dhas: an older running copy does not take the added row off the sheet', () => {
  beforeEach(() => {
    window.API.recordDaihyosen = vi.fn();
    window.API.removeDaihyosen = vi.fn();
  });

  const hikiwakeBout1 = { position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake' };

  async function addLanded() {
    const base = makeKnockoutTeamMatch({ modifiedAt: 1000 });
    window.API.recordDaihyosen.mockResolvedValue({ ...base, subResults: [serverDaihyosenRow()], modifiedAt: 2000 });
    const view = renderModal(base);
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(screen.queryByTestId('team-daihyosen-remove')).toBeTruthy();
    const show = async (m) => {
      await act(async () => { view.rerender(<ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={makeOnSubmit(m)} password="" />); });
      await settle();
    };
    return { base, show };
  }

  it('keeps the row, and the next tap writes it', async () => {
    const { base, show } = await addLanded();
    await show({ ...base, subResults: [hikiwakeBout1], modifiedAt: 1500 });
    expect(screen.queryByTestId('team-daihyosen-remove')).toBeTruthy();

    window.API.recordScore.mockClear();
    await act(async () => { fireEvent.click(tieBoutButton(1)); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(window.API.recordScore.mock.calls[0][2].subResults.some((s) => s.position === -1)).toBe(true);
  });

  it.each([
    ['a copy as new as the answer', { modifiedAt: 2000 }],
    ['an unstamped copy', { modifiedAt: 0 }],
    ['a copy no longer running', { modifiedAt: 1500, status: 'completed', winner: 'Team A' }],
  ])('%s is the match the sheet shows', async (_what, fields) => {
    const { base, show } = await addLanded();
    await show({ ...base, subResults: [hikiwakeBout1], ...fields });
    expect(screen.queryByTestId('team-daihyosen-remove')).toBeNull();
  });
});

describe('bc-dhas: a refused add or remove is reported and changes nothing', () => {
  beforeEach(() => {
    window.API.recordDaihyosen = vi.fn();
    window.API.removeDaihyosen = vi.fn();
  });

  it('a remove refused for the clock shows the clock banner and keeps the row', async () => {
    window.API.removeDaihyosen.mockResolvedValue({ applied: false, reason: 'clock_skew', message: 'Not saved.' });
    renderModal(makeMatchWithDaihyosen());
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();

    expect(failedBanner()?.textContent).toBe(`Not saved: ${CLOCK_SKEW_REASON_TEXT}. ${CLOCK_SKEW_ADVICE}`);
    expect(screen.getByTestId('team-daihyosen-remove')).toBeTruthy();
  });
});

// A host whose match prop takes no push (the Scores tab's list within its
// jittered reload, or the court console with SSE down) can see Add then
// Remove with no refresh in between. The Add adopts its log over the prop;
// the Remove's log equals the prop again, and adopting it must take the
// Add's override away, or the removed row stays on the sheet, a second
// Remove answers "No daihyosen to remove", and the next tap saves the row back.
describe('bc-dhas: Add then Remove with the match prop never refreshed', () => {
  beforeEach(() => {
    window.API.recordDaihyosen = vi.fn().mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [serverDaihyosenRow()] });
    window.API.removeDaihyosen = vi.fn().mockResolvedValue({ subResults: [] });
  });

  it('the removed row leaves the sheet and no later write brings it back', async () => {
    renderModal(makeKnockoutTeamMatch());
    await act(async () => { fireEvent.click(tieBoutButton(0)); });
    await act(async () => { fireEvent.click(tieBoutButton(1)); });
    await act(async () => { fireEvent.click(tieBoutButton(2)); });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(subMatchRows()).toHaveLength(4);

    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();
    expect(window.API.removeDaihyosen).toHaveBeenCalledTimes(1);
    expect(subMatchRows()).toHaveLength(3);
    expect(screen.queryByTestId('team-daihyosen-remove')).toBeNull();
    expect(screen.getByTestId('scoring-modal-daihyosen-button')).toBeTruthy();

    window.API.recordScore.mockClear();
    await act(async () => { fireEvent.click(tieBoutButton(0)); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(window.API.recordScore.mock.calls[0][2].subResults.some((s) => s.position === -1)).toBe(false);
  });
});

// The organiser's Add is offered on a match that is not running as well. A
// queued match is saved first, which starts it through the score path's court
// and eligibility checks; a finished one is judged on its stored bouts, since
// a running write there is answered stale (and the admin host would tell the
// organiser to reopen a match that has no reopen).
describe('bc-dhas: the sheet is saved before an add unless the match has finished', () => {
  beforeEach(() => {
    window.API.recordDaihyosen = vi.fn().mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [serverDaihyosenRow()] });
  });

  it('an add on a finished match sends no running write first', async () => {
    renderModal(makeKnockoutTeamMatch({ status: 'completed' }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();

    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });

  it('an add on a queued match saves the sheet first, as a running write', async () => {
    renderModal(makeKnockoutTeamMatch({ status: 'scheduled' }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(window.API.recordScore.mock.calls[0][2].status).toBe('running');
    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);
  });
});

// The override that shows an adopted answer gives way to a prop at least as
// new as that answer, whatever the prop's log reads: a refetch can bring a log
// that reads as it did before the add (another device removed the row first),
// which no comparison of content can tell from no change at all.
describe('bc-dhas: an adopted answer gives way to a prop at least as new', () => {
  function renderLive(match) {
    const onSubmit = makeOnSubmit(match);
    const onClose = vi.fn();
    const view = render(<ScoreEditorModal match={match} onClose={onClose} onSubmit={onSubmit} password="" />);
    return (next) => view.rerender(<ScoreEditorModal match={next} onClose={onClose} onSubmit={onSubmit} password="" />);
  }

  function lastWrittenRepBout() {
    const calls = window.API.recordScore.mock.calls;
    return calls[calls.length - 1][2].subResults.find((s) => s.position === -1);
  }

  it('a refetch newer than the add, with the log as it was, takes the added row away', async () => {
    window.API.recordDaihyosen = vi.fn().mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [serverDaihyosenRow()], modifiedAt: 2000 });
    const rerender = renderLive(makeKnockoutTeamMatch({ modifiedAt: 1000 }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(subMatchRows()).toHaveLength(4);

    await act(async () => { rerender(makeKnockoutTeamMatch({ modifiedAt: 3000 })); });
    await settle();

    expect(subMatchRows()).toHaveLength(3);
    expect(screen.queryByTestId('team-daihyosen-remove')).toBeNull();
    window.API.recordScore.mockClear();
    await act(async () => { fireEvent.click(tieBoutButton(0)); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(lastWrittenRepBout()).toBeUndefined();
  });

  // The control: the echo of the save made before the add can come back
  // after the add's answer. It is older than the answer, so the row stays.
  it('a refetch older than the add keeps the added row', async () => {
    window.API.recordDaihyosen = vi.fn().mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [serverDaihyosenRow()], modifiedAt: 2000 });
    const rerender = renderLive(makeKnockoutTeamMatch({ modifiedAt: 1000 }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();

    await act(async () => { rerender(makeKnockoutTeamMatch({ modifiedAt: 1500 })); });
    await settle();

    expect(subMatchRows()).toHaveLength(4);
    expect(screen.getByTestId('team-daihyosen-remove')).toBeTruthy();
  });

  // Another device scored the new row and its push arrived before this
  // page's own answer. The answer is older: shown over the prop, its unscored
  // row would be written back over the other device's point.
  it('an answer older than the prop when it lands does not hide the newer row', async () => {
    const add = deferred();
    window.API.recordDaihyosen = vi.fn().mockReturnValue(add.promise);
    const rerender = renderLive(makeKnockoutTeamMatch({ modifiedAt: 1000 }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    await act(async () => { rerender(makeKnockoutTeamMatch({ modifiedAt: 3000, subResults: [{ ...serverDaihyosenRow(), ipponsA: ['M'], ipponsB: [] }] })); });
    await act(async () => { add.resolve({ ...makeKnockoutTeamMatch(), subResults: [serverDaihyosenRow()], modifiedAt: 2000 }); });
    await settle();

    window.API.recordScore.mockClear();
    await act(async () => { fireEvent.click(tieBoutButton(0)); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(lastWrittenRepBout()?.ipponsA).toEqual(['M']);
  });
});

// A send-back (or requeue, or reopen) keeps the bout log and is stamped by the
// server's clock, which can read older than this device's stamped answer. Add
// then Remove before the add reached the prop leaves the prop holding the very
// log the remove answered with, so the send-back changes neither the log nor
// (past the answer) the stamp, and the sheet kept showing a sent-back match as
// running. The prop's status now lets it through once its log is the
// answer's own; a status change on a different log still does not.
describe("bc-dhas: a status change the answer's log already holds is shown", () => {
  function renderLive(match) {
    const onSubmit = makeOnSubmit(match);
    const view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
    return (next) => view.rerender(<ScoreEditorModal match={next} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
  }

  beforeEach(() => {
    window.API.recordDaihyosen = vi.fn().mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [serverDaihyosenRow()], modifiedAt: 2000 });
    window.API.removeDaihyosen = vi.fn().mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [], modifiedAt: 3000 });
  });

  it('Add then Remove, then a send-back stamped below the answer: the sheet shows it queued', async () => {
    const rerender = renderLive(makeKnockoutTeamMatch({ modifiedAt: 1000 }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();
    expect(window.API.removeDaihyosen).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('Start match')).toBeNull();

    await act(async () => { rerender(makeKnockoutTeamMatch({ status: 'scheduled', modifiedAt: 1500 })); });
    await settle();

    expect(screen.getByText('Start match')).toBeTruthy();
    expect(subMatchRows()).toHaveLength(3);
  });

  // The control: after the add alone, a queued copy read before it carries
  // the log from before the add. Letting it through would take the added row
  // off the sheet, which is what the override is there to stop.
  it('Add, then a queued copy with the log from before it: the added row stays', async () => {
    const rerender = renderLive(makeKnockoutTeamMatch({ modifiedAt: 1000 }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(subMatchRows()).toHaveLength(4);

    await act(async () => { rerender(makeKnockoutTeamMatch({ status: 'scheduled', modifiedAt: 500 })); });
    await settle();

    expect(subMatchRows()).toHaveLength(4);
    expect(screen.getByTestId('team-daihyosen-remove')).toBeTruthy();
  });
});

// A newer stamped answer is the match as the add or remove left it: its status
// and the team match's overtime are the sheet's too, not only its bout log.
describe('bc-dhas: the sheet takes a newer answer\'s status and overtime', () => {
  beforeEach(() => {
    window.API.recordDaihyosen = vi.fn();
    window.API.removeDaihyosen = vi.fn();
  });

  // The organiser's add on a finished match leaves it running. Read as still
  // finished, a point tapped on the new bout saved nothing, and Close lost it.
  it('a point tapped right after an add on a finished match is saved as a running write', async () => {
    const add = deferred();
    window.API.recordDaihyosen.mockReturnValue(add.promise);
    renderModal(makeKnockoutTeamMatch({ status: 'completed', modifiedAt: 1000 }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(window.API.recordScore, 'no save before an add on a finished match').not.toHaveBeenCalled();
    await act(async () => { add.resolve({ ...makeKnockoutTeamMatch(), status: 'running', subResults: [serverDaihyosenRow()], modifiedAt: 2000 }); });
    await settle();

    await act(async () => { fireEvent.click(ipponButton(subMatchRows()[3], 'shiro', 'M')); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    const patch = window.API.recordScore.mock.calls[0][2];
    expect(patch.status).toBe('running');
    expect(patch.subResults.find((s) => s.position === -1)).toBeTruthy();
  });

});

// bc-hlck: an autosave is never stamped older than the match its tap was made
// against, and never floored by a change that arrived AFTER the tap (operator
// ruling 2026-09-27: a tap made before another device's change stays older
// than it). The tap records the shown match's stamp as `seenModifiedAt`,
// which recordScore floors the stamp by and never sends.
describe('bc-hlck: an autosave carries the stamp of the match as it was at the tap', () => {
  // bc-evts stage 3: a tap is stamped when it is made, floored by the match
  // the board showed then (recordTap's `match` argument).
  it('a tap is floored by the match shown at the tap, and a later tap by the newer one', async () => {
    const first = makeRunningMatch({ modifiedAt: 1_000_000 });
    const { rerender } = renderModal(first);
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    expect(window.API.recordTap.mock.calls[0][4].modifiedAt).toBe(1_000_000);
    // Another device's change reaches this editor.
    const moved = { ...first, modifiedAt: 2_000_000 };
    await act(async () => {
      rerender(<ScoreEditorModal match={moved} onClose={vi.fn()} onSubmit={makeOnSubmit(moved)} password="" />);
    });
    await act(async () => { fireEvent.click(screen.getAllByText('K')[0]); });
    expect(window.API.recordTap.mock.calls[1][4].modifiedAt).toBe(2_000_000);
  });
});

// bc-hlck in the team editor: its autosave carries the shown match's stamp
// from the tap, and the representative bout's add is floored by the match as
// the sheet showed it (the trailing argument).
describe('bc-hlck: the team editor floors its writes by the match it shows', () => {
  beforeEach(() => {
    window.API.recordDaihyosen = vi.fn();
    window.API.removeDaihyosen = vi.fn();
  });

  it('an autosave carries the shown stamp from the tap', async () => {
    renderModal(makeTeamMatch({ modifiedAt: 1_000_000 }));
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(window.API.recordScore.mock.calls[0][2].seenModifiedAt).toBe(1_000_000);
  });

  it('the representative bout add passes the shown stamp', async () => {
    window.API.recordDaihyosen.mockResolvedValue({ ...makeKnockoutTeamMatch(), subResults: [daihyosenRow()] });
    renderModal(makeKnockoutTeamMatch({ modifiedAt: 1_000_000 }));
    await act(async () => { fireEvent.click(tieBoutButton(0)); });
    await act(async () => { fireEvent.click(tieBoutButton(1)); });
    await act(async () => { fireEvent.click(tieBoutButton(2)); });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);
    expect(window.API.recordDaihyosen.mock.calls[0][3]).toBe(1_000_000);
  });
});
