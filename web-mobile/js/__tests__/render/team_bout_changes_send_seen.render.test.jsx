// bc-evts stage 3, review round 1, minor 7: every rule-bound write names what
// the screen showed (`seen`, the match's scoreVersion: the score log lines it
// was made against), so the server's basis judges it as the operator saw it:
// the representative bout's add and remove and a kachinuki bout's removal too.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
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

const lastOpts = (fn) => fn.mock.calls[fn.mock.calls.length - 1].at(-1);

describe('the team sheet\'s bout changes say what the sheet showed', () => {
  it('Add representative bout', async () => {
    renderSheet(knockoutTeamMatch({ scoreVersion: 7 }));
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    expect(lastOpts(window.API.recordDaihyosen)).toEqual(expect.objectContaining({ seen: 7 }));
  });

  it('Remove representative bout', async () => {
    renderSheet(knockoutTeamMatch({ scoreVersion: 9, subResults: [repRow()] }));
    await tap(screen.getByTestId('team-daihyosen-remove'));
    expect(lastOpts(window.API.removeDaihyosen)).toEqual(expect.objectContaining({ seen: 9 }));
  });

  it('Remove this bout', async () => {
    renderSheet({ ...kachinuki([
      { position: 1, sideA: 'A One', sideB: 'B One', ipponsA: ['M', 'M'], ipponsB: [], winner: 'A One' },
      { position: 2, sideA: 'A One', sideB: 'B Two', ipponsA: [], ipponsB: [] },
    ]), scoreVersion: 11 });
    await tap(screen.getByTestId('kachinuki-remove-bout-button'));
    expect(lastOpts(window.API.removeKachinukiBout)).toEqual(expect.objectContaining({ seen: 11 }));
  });
});
