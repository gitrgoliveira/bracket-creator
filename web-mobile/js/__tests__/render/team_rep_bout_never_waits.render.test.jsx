// bc-evts stage 3, the governing rule: ALL goes to the outbox and NOTHING
// waits on it. The team editor's representative-bout add and remove, and the
// kachinuki bout removal, are outbox doors that answer pending at once: the
// sheet shows the change before any server answer (here, one that never
// comes), a tap made right after is handed to the outbox at once (bc-evts stage
// 5: the outbox sends it after the change, in order), and the server's answer,
// when it comes, is adopted, or a refusal puts the server's sheet back and says
// why.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    removeKachinukiBout: vi.fn(),
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
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});
afterAll(() => restoreGlobals());

// A door's pending answer whose server answer the test gives, or never does.
function pendingDoor() {
  let resolve, reject;
  const settled = new Promise((res, rej) => { resolve = res; reject = rej; });
  settled.catch(() => {});
  return { answer: { applied: true, pending: true, key: 'k', settled }, resolve, reject };
}
const pendingWrite = () => pendingDoor().answer;

beforeEach(() => {
  vi.useFakeTimers();
  window.API.recordScore = vi.fn(() => Promise.resolve(pendingWrite()));
  window.API.recordDaihyosen = vi.fn();
  window.API.removeDaihyosen = vi.fn();
  window.API.removeKachinukiBout = vi.fn();
});
afterEach(() => { vi.useRealTimers(); });

const knockoutTeamMatch = (over = {}) => ({
  id: 'tm-ko', compId: 'comp1', status: 'running', phase: 'bracket', round: 'Final', court: 'A',
  compKind: 'team', teamSize: 3, compFormat: 'knockout', teamMatchType: 'fixed',
  sideA: { id: 'teamA', name: 'Team A' }, sideB: { id: 'teamB', name: 'Team B' },
  subResults: [1, 2, 3].map((position) => ({ position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: '' })),
  ...over,
});
const repRow = (over = {}) => ({ position: -1, sideA: 'Team A', sideB: 'Team B', ipponsA: [], ipponsB: [], winner: '', decision: 'daihyosen', ...over });

function renderSheet(match) {
  return render(
    <ScoreEditorModal match={match} onClose={vi.fn()} password=""
      onSubmit={(patch) => window.API.recordScore('comp1', match.id, patch, '', match)} />,
  );
}
async function settle(times = 8) {
  for (let i = 0; i < times; i++) await act(async () => { await Promise.resolve(); });
}
const rows = () => [...document.querySelectorAll('.team-sub-match')];
const tieButton = (idx) => rows()[idx].querySelector('[data-testid="scoring-modal-tie-button"]');
const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);
const marksOf = (rowEl, color) => [...rowEl.querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)].map((b) => b.textContent).filter((t) => t !== '·');
// The bout rows the knockoutTeamMatch fixture holds.
const BOUTS = () => knockoutTeamMatch().subResults;

describe('the representative-bout add never waits for its answer', () => {
  it('shows the row at once, and a tap right after is a tap on the sheet with it', async () => {
    const add = pendingDoor();
    window.API.recordDaihyosen.mockResolvedValue(add.answer);
    renderSheet(knockoutTeamMatch());
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);
    // The answer never came, and the sheet already shows the row.
    expect(screen.queryByTestId('team-daihyosen-remove'), 'the added row is on the sheet').not.toBeNull();

    await act(async () => { fireEvent.click(tieButton(0)); });
    expect(taps(), 'the tap is not held behind the add').toEqual([{ kind: 'bout.decision', pos: 1, value: 'hikiwake' }]);
    expect(rows()).toHaveLength(4);
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });

  it('a refused add takes the row off again and says why', async () => {
    const add = pendingDoor();
    window.API.recordDaihyosen.mockResolvedValue(add.answer);
    renderSheet(knockoutTeamMatch());
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(screen.queryByTestId('team-daihyosen-remove')).not.toBeNull();
    await act(async () => { add.reject(new Error('not_tied')); });
    await settle();
    expect(screen.queryByTestId('team-daihyosen-remove'), 'the server\'s sheet is back').toBeNull();
    expect(document.body.textContent).toContain('Daihyosen needs a tie on IV and PW');
  });

  it('adopts the server\'s row when the answer comes', async () => {
    const add = pendingDoor();
    window.API.recordDaihyosen.mockResolvedValue(add.answer);
    renderSheet(knockoutTeamMatch({ modifiedAt: 1000 }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    await act(async () => { add.resolve({ ...knockoutTeamMatch(), modifiedAt: 2000, subResults: [...BOUTS(), repRow({ ipponsA: ['M'] })] }); });
    await settle();
    expect(marksOf(rows()[3], 'aka')).toEqual(['M']);
  });
});

describe('the representative-bout remove never waits for its answer', () => {
  it('takes the row off at once, and a tap right after is a tap on the sheet without it', async () => {
    const remove = pendingDoor();
    window.API.removeDaihyosen.mockResolvedValue(remove.answer);
    renderSheet(knockoutTeamMatch({ subResults: [...BOUTS(), repRow()] }));
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();
    expect(window.API.removeDaihyosen).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('team-daihyosen-remove'), 'the row is gone from the sheet').toBeNull();

    await act(async () => { fireEvent.click(tieButton(0)); });
    expect(taps()).toEqual([{ kind: 'bout.decision', pos: 1, value: 'hikiwake' }]);
    expect(rows()).toHaveLength(3);
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });
});

// Ported from autosave_debounce's bc-dhas sections when the hold retired:
// what the sheet must still get right with nothing waiting.
describe('the representative-bout change, in order and with the right sheet', () => {
  const ipponButton = (rowEl, color, letter) => [...rowEl.querySelectorAll(`.team-sub-match__side--${color} button.ipt-btn`)].find((b) => b.textContent === letter);
  // The overtime the sheet shows, or null when the stepper is folded away.
  const shownOvertime = () => document.querySelector('.encho-row__count')?.textContent ?? null;

  it('a tap made before the add is ahead of it in the outbox, and nothing is saved first', async () => {
    window.API.recordDaihyosen.mockResolvedValue(pendingWrite());
    renderSheet(knockoutTeamMatch());
    await act(async () => { fireEvent.click(tieButton(0)); });
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    expect(window.API.recordTap).toHaveBeenCalledTimes(1);
    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);
    expect(window.API.recordTap.mock.invocationCallOrder[0]).toBeLessThan(window.API.recordDaihyosen.mock.invocationCallOrder[0]);
    await act(async () => { vi.advanceTimersByTime(350); });
    await settle();
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });

  it('an add on a finished match saves nothing first, and a point on the new bout is a tap on the running match', async () => {
    window.API.recordDaihyosen.mockResolvedValue(pendingWrite());
    renderSheet(knockoutTeamMatch({ status: 'completed', modifiedAt: 1000 }));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(window.API.recordScore, 'no save before an add on a finished match').not.toHaveBeenCalled();
    await act(async () => { fireEvent.click(ipponButton(rows()[3], 'shiro', 'M')); });
    expect(taps()).toEqual([{ kind: 'ippon.add', pos: -1, side: 'b', letter: 'M' }]);
    await act(async () => { vi.advanceTimersByTime(350); });
    await settle();
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });

  it("an add does not show the team match's overtime as the new rep bout's", async () => {
    window.API.recordDaihyosen.mockResolvedValue(pendingWrite());
    renderSheet(knockoutTeamMatch({ encho: { periodCount: 1 } }));
    expect(shownOvertime()).toBe('×1');
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    expect(rows()).toHaveLength(4);
    expect(shownOvertime(), 'the stepper is the rep bout\'s now, which has none').toBeNull();
  });

  it("a remove shows neither the rep bout's overtime nor the cleared team overtime, before or after the answer", async () => {
    const remove = pendingDoor();
    window.API.removeDaihyosen.mockResolvedValue(remove.answer);
    renderSheet(knockoutTeamMatch({ encho: { periodCount: 1 }, modifiedAt: 1000, subResults: [...BOUTS(), repRow({ encho: { periodCount: 1 } })] }));
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();
    expect(rows()).toHaveLength(3);
    expect(shownOvertime(), 'before the answer').toBeNull();
    // The answer arrives before its push, and the sheet keeps it cleared.
    await act(async () => { remove.resolve({ ...knockoutTeamMatch(), status: 'running', subResults: BOUTS(), modifiedAt: 2000 }); });
    await settle();
    expect(shownOvertime(), 'after the answer').toBeNull();
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });

  it('a superseded add shows the not-saved banner and the row goes', async () => {
    const add = pendingDoor();
    window.API.recordDaihyosen.mockResolvedValue(add.answer);
    renderSheet(knockoutTeamMatch());
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    await act(async () => { add.resolve({ applied: false, reason: 'superseded' }); });
    await settle();
    expect(document.querySelector('.pending-write-banner--failed')).not.toBeNull();
    expect(screen.queryByTestId('team-daihyosen-remove')).toBeNull();
  });

  it('an editor closed right after an add owes nothing: its tap went as it was made', async () => {
    window.API.recordDaihyosen.mockResolvedValue(pendingWrite());
    const view = renderSheet(knockoutTeamMatch());
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
    await settle();
    await act(async () => { fireEvent.click(tieButton(0)); });
    await act(async () => { view.unmount(); });
    expect(taps()).toEqual([{ kind: 'bout.decision', pos: 1, value: 'hikiwake' }]);
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });
});

describe('the kachinuki bout removal never waits for its answer', () => {
  const PLAYED = [
    { position: 1, sideA: 'A One', sideB: 'B One', ipponsA: ['M', 'M'], ipponsB: [], winner: 'A One' },
    { position: 2, sideA: 'A One', sideB: '', ipponsA: [], ipponsB: [] },
  ];
  const kachinuki = () => knockoutTeamMatch({ phase: 'pool', compFormat: 'mixed', teamMatchType: 'kachinuki', subResults: PLAYED });

  it('drops the bout at once, and a refusal brings it back with the reason', async () => {
    const removal = pendingDoor();
    window.API.removeKachinukiBout.mockResolvedValue(removal.answer);
    renderSheet(kachinuki());
    const before = rows().length;
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-remove-bout-button')); });
    await settle();
    expect(window.API.removeKachinukiBout).toHaveBeenCalledTimes(1);
    expect(rows().length, 'the trailing bout is off the sheet before any answer').toBeLessThan(before);
    expect(document.body.textContent).not.toContain('Removing…');

    await act(async () => { removal.reject(new Error('Failed to remove bout')); });
    await settle();
    expect(rows().length, 'the server still holds it').toBe(before);
    expect(document.body.textContent).toContain('Failed to remove bout');
  });
});

// bc-evts stage 5: a running team match is scored by taps handed to the outbox
// (API.recordTap); each test gets a fresh fake of that surface, whose pending
// taps the board folds on. Registered last, so it lands on the API the hooks
// above set up.
beforeEach(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });
