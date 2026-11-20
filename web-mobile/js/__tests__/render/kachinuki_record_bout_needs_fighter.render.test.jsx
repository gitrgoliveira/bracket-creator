// Kachinuki Record bout with no fighter named on the live bout: the server
// works the next pairing out from who fought and skips a bout naming nobody, so
// the button waits for a fighter and the footer says why.
import React from 'react';
import { render, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
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

beforeEach(() => {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    fetchSquads: vi.fn().mockResolvedValue({}),
    fetchLineupInForce: vi.fn().mockResolvedValue(null),
    fetchKachinukiRoster: vi.fn().mockResolvedValue(null),
  };
});


function kachinukiMatch(subResults) {
  return {
    id: 'm1', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A' }, sideB: { id: 'team-B', name: 'Team B' },
    subResults,
  };
}

async function mount(match) {
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="secret" />);
  });
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
}

const recordBout = () => screen.getByRole('button', { name: 'Record bout' });

describe('Record bout on a kachinuki bout that names no fighter', () => {
  it('waits for a fighter and says so, instead of sending a write the server skips', async () => {
    await mount(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [] }]));
    expect(recordBout().disabled).toBe(true);
    expect(screen.getByTestId('kachinuki-nofighter-hint').textContent)
      .toBe('Pick or type who fights this bout to enable Record bout.');
  });

  it('is open once a fighter is named', async () => {
    await mount(kachinukiMatch([{ position: 1, sideA: 'Sato', sideB: '', ipponsA: ['M'], ipponsB: [] }]));
    expect(recordBout().disabled).toBe(false);
    expect(screen.queryByTestId('kachinuki-nofighter-hint')).toBeNull();
  });
});
