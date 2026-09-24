// bc-kbrw: a double tap on a fought kachinuki bout opened the bout above it.
//
// Tapping a fought bout row expands it for correction under the finger, so
// the second tap of a double tap landed on whatever moved there and opened
// ANOTHER bout. For DONE_BOUT_OPEN_TAP_GUARD_MS after a row opens, a pointer
// click on the bout list is swallowed in the capture phase. Pointer clicks
// carry detail >= 1 (fireEvent.click defaults to 0, which is what a click
// synthesized from the keyboard carries, so the tests pass detail explicitly).
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { DONE_BOUT_OPEN_TAP_GUARD_MS } from '../../admin_scoring_team.jsx';

const STUBBED_GLOBALS = {
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'knockout', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
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

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { vi.useRealTimers(); });

// Two fought bouts and the current one (same shape as the config matrix's
// "tapping a different fought bout" fixture).
async function renderEncounter() {
  await act(async () => {
    render(
      <ScoreEditorModal
        match={{
          id: 'm1', compId: 'comp1', status: 'running', phase: 'bracket',
          round: 'Semi-final', matchNumber: 1, court: 'A',
          compKind: 'team', teamSize: 5, compFormat: 'knockout', teamMatchType: 'kachinuki',
          sideA: { id: 'team-A', name: 'Team A' },
          sideB: { id: 'team-B', name: 'Team B' },
          subResults: [
            { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M', 'K'], winner: 'B1' },
            { position: 2, sideA: 'A2', sideB: 'B1', ipponsA: ['M', 'K'], ipponsB: [], winner: 'A2' },
            { position: 3, sideA: 'A2', sideB: 'B2', ipponsA: [], ipponsB: [] },
          ],
        }}
        onClose={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        password=""
      />,
    );
  });
}

const tap = (el) => act(async () => { fireEvent.click(el, { detail: 1 }); });
const isOpen = (idx) => !!screen.queryByTestId(`kachinuki-done-collapse-${idx}`);

describe('bc-kbrw: a double tap on a fought bout opens that bout only', () => {
  it('the second tap, inside the window, does not open another bout', async () => {
    await renderEncounter();
    await tap(screen.getByTestId('kachinuki-done-bout-1'));
    expect(isOpen(1)).toBe(true);

    // The second tap of the double tap lands on a different row.
    await tap(screen.getByTestId('kachinuki-done-bout-0'));
    expect(isOpen(1)).toBe(true);
    expect(isOpen(0)).toBe(false);
  });

  it('a deliberate tap after the window opens the other bout', async () => {
    await renderEncounter();
    await tap(screen.getByTestId('kachinuki-done-bout-1'));
    await act(async () => { vi.advanceTimersByTime(DONE_BOUT_OPEN_TAP_GUARD_MS + 50); });
    await tap(screen.getByTestId('kachinuki-done-bout-0'));
    expect(isOpen(0)).toBe(true);
    expect(isOpen(1)).toBe(false);
  });

  it('a keyboard-synthesized click (detail 0) is never swallowed', async () => {
    await renderEncounter();
    await tap(screen.getByTestId('kachinuki-done-bout-1'));
    await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-done-bout-0'), { detail: 0 }); });
    expect(isOpen(0)).toBe(true);
  });
});
