// bc-ktih: an unscored kachinuki bout never reads as tied. The footer's End
// hint is worked out from the bout End would judge, which is the LAST SCORED
// bout; after Record bout appends the next pairing that is still the bout just
// recorded, so a 1-1 recorded on bout 1 used to make the fresh, unscored bout 2
// read "Tied bout. End match records a drawn encounter. Record bout retires
// both and brings the next pair up." (seen after a tie that left Aka's last
// fighter on, but any recorded tie did it). The unscored bout shows the normal
// "Score this bout to enable Record bout" hint instead, and a scored tie on the
// live bout still reads "Tied bout".
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

// Bout 1: Sato (Shiro) and Nomura (Aka, their taisho) drew 1-1, recorded.
// Aka has nobody behind Nomura, so the advance kept Nomura on against Ito.
const TIED_RECORDED = {
  position: 1, sideA: 'Nomura', sideB: 'Sato', decision: 'hikiwake',
  ipponsA: ['M'], ipponsB: ['K'], recorded: true,
};
const APPENDED = { position: 2, sideA: 'Nomura', sideB: 'Ito', ipponsA: [], ipponsB: [] };

function kachinukiMatch(subResults, phase = 'pool') {
  return {
    id: 'm1', compId: 'comp1', status: 'running', phase, poolName: 'Pool 1', court: 'A',
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

describe('an unscored kachinuki bout after a recorded tie', () => {
  it('never reads as tied and shows the Score-this-bout hint (pool)', async () => {
    await mount(kachinukiMatch([TIED_RECORDED, APPENDED]));
    const end = screen.queryByTestId('kachinuki-end-hint');
    expect(end ? end.textContent : '').not.toMatch(/Tied bout/);
    expect(screen.getByTestId('kachinuki-record-hint').textContent)
      .toBe('Score this bout to enable Record bout, or End match to finish on the last scored bout.');
  });

  it('never reads as tied in a knockout, and does not offer End match there', async () => {
    await mount(kachinukiMatch([TIED_RECORDED, APPENDED], 'bracket'));
    const end = screen.queryByTestId('kachinuki-end-hint');
    expect(end ? end.textContent : '').not.toMatch(/Tied bout|Record bout brings/);
    expect(screen.getByTestId('kachinuki-record-hint').textContent)
      .toBe('Score this bout to enable Record bout. No draws in a knockout: End match needs a bout with a point.');
  });

  it('still reads "Tied bout" for a tie on the live bout itself', async () => {
    const liveTie = { ...APPENDED, decision: 'hikiwake', ipponsA: ['M'], ipponsB: ['K'] };
    await mount(kachinukiMatch([TIED_RECORDED, liveTie]));
    expect(screen.getByTestId('kachinuki-end-hint').textContent).toMatch(/^Tied bout\. End match records a drawn encounter\./);
    expect(screen.queryByTestId('kachinuki-record-hint')).toBeNull();
  });
});
