// bc-evts stage 3, G2 (bc-dtfn: a double tap is two complete clicks). The
// team editor's Add representative bout, Remove representative bout, Record
// bout and Remove this bout no longer wait for their answer, so the sheet
// changes at once and the second click of a double tap lands on whatever took
// the button's place. The bounce of the tap that pressed one of them never
// presses it, or its replacement, again (tap_guard.jsx); a tap made after
// TAP_BOUNCE_MS is a tap.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';

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

// A door's answer that never comes: nothing waits on it.
const pendingWrite = () => ({ applied: true, pending: true, key: 'k', settled: new Promise(() => {}) });

beforeEach(() => {
  vi.useFakeTimers();
  window.API.recordScore = vi.fn(() => Promise.resolve(pendingWrite()));
  window.API.recordDaihyosen = vi.fn(() => Promise.resolve(pendingWrite()));
  window.API.removeDaihyosen = vi.fn(() => Promise.resolve(pendingWrite()));
  window.API.removeKachinukiBout = vi.fn(() => Promise.resolve(pendingWrite()));
});
afterEach(() => { vi.useRealTimers(); });

const knockoutTeamMatch = (over = {}) => ({
  id: 'tm-ko', compId: 'comp1', status: 'running', phase: 'bracket', round: 'Final', court: 'A',
  compKind: 'team', teamSize: 3, compFormat: 'knockout', teamMatchType: 'fixed',
  sideA: { id: 'teamA', name: 'Team A' }, sideB: { id: 'teamB', name: 'Team B' },
  subResults: [], ...over,
});
const repRow = () => ({ position: -1, sideA: 'Team A', sideB: 'Team B', ipponsA: [], ipponsB: [], winner: '', decision: 'daihyosen' });
const kachinuki = (subResults) => knockoutTeamMatch({ phase: 'pool', compFormat: 'mixed', teamMatchType: 'kachinuki', subResults });

function renderSheet(match) {
  return render(
    <ScoreEditorModal match={match} onClose={vi.fn()} password=""
      onSubmit={(patch) => window.API.recordScore('comp1', match.id, patch, '', match)} />,
  );
}
async function settle(times = 8) {
  for (let i = 0; i < times; i++) await act(async () => { await Promise.resolve(); });
}
// A pointer tap (detail > 0); the second of a double tap carries detail 2.
async function tap(el, detail = 1) {
  expect(el, 'the control is on the sheet').not.toBeNull();
  await act(async () => { fireEvent.click(el, { detail }); });
  await settle();
}
const recordBout = () => [...document.querySelectorAll('button')].find((b) => /^(Record bout|Saving…)$/.test(b.textContent.trim()));
const boutFinals = () => window.API.recordScore.mock.calls.filter((c) => c[2] && c[2].kachinukiBoutFinal);

describe('a double tap on a representative-bout change makes one change', () => {
  it('Add representative bout adds one, and its bounce does not press Remove', async () => {
    renderSheet(knockoutTeamMatch());
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);
    await tap(screen.queryByTestId('team-daihyosen-remove'), 2);
    expect(window.API.removeDaihyosen, 'the bounce lands on Remove and does nothing').not.toHaveBeenCalled();
    expect(screen.queryByTestId('team-daihyosen-remove')).not.toBeNull();

    await act(async () => { vi.advanceTimersByTime(TAP_BOUNCE_MS + 10); });
    await tap(screen.queryByTestId('team-daihyosen-remove'));
    expect(window.API.removeDaihyosen, 'a tap after the bounce window is a tap').toHaveBeenCalledTimes(1);
  });

  it('Remove representative bout removes it, and its bounce does not press Add', async () => {
    renderSheet(knockoutTeamMatch({ subResults: [repRow()] }));
    await tap(screen.getByTestId('team-daihyosen-remove'));
    expect(window.API.removeDaihyosen).toHaveBeenCalledTimes(1);
    await tap(screen.queryByTestId('scoring-modal-daihyosen-button'), 2);
    expect(window.API.recordDaihyosen, 'the bounce lands on Add and does nothing').not.toHaveBeenCalled();

    await act(async () => { vi.advanceTimersByTime(TAP_BOUNCE_MS + 10); });
    await tap(screen.queryByTestId('scoring-modal-daihyosen-button'));
    expect(window.API.recordDaihyosen).toHaveBeenCalledTimes(1);
  });
});

describe('a double tap on a kachinuki bout change makes one change', () => {
  it('Record bout records one bout', async () => {
    renderSheet(kachinuki([{ position: 1, sideA: 'A One', sideB: 'B One', ipponsA: ['M', 'M'], ipponsB: [], winner: 'A One' }]));
    await tap(recordBout());
    expect(boutFinals()).toHaveLength(1);
    await tap(recordBout(), 2);
    expect(boutFinals(), 'the bounce records nothing').toHaveLength(1);
  });

  it('Remove this bout removes one bout', async () => {
    renderSheet(kachinuki([
      { position: 1, sideA: 'A One', sideB: 'B One', ipponsA: ['M', 'M'], ipponsB: [], winner: 'A One' },
      { position: 2, sideA: 'A One', sideB: 'B Two', ipponsA: [], ipponsB: [] },
      { position: 3, sideA: 'A One', sideB: 'B Three', ipponsA: [], ipponsB: [] },
    ]));
    await tap(screen.getByTestId('kachinuki-remove-bout-button'));
    expect(window.API.removeKachinukiBout).toHaveBeenCalledTimes(1);
    // Bout 2 is now the trailing empty bout: Remove this bout is under the finger.
    await tap(screen.queryByTestId('kachinuki-remove-bout-button'), 2);
    expect(window.API.removeKachinukiBout, 'the bounce removes nothing').toHaveBeenCalledTimes(1);

    await act(async () => { vi.advanceTimersByTime(TAP_BOUNCE_MS + 10); });
    await tap(screen.queryByTestId('kachinuki-remove-bout-button'));
    expect(window.API.removeKachinukiBout, 'a tap after the bounce window is a tap').toHaveBeenCalledTimes(2);
  });
});
