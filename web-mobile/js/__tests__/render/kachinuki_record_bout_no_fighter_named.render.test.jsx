// bc-kno1 (operator decision Q6 (b)): Record bout does not wait for a named
// fighter. With no lineup the server seats bout 1 by member number, a nameless
// member is a fighter, and the advance appends the next member against the
// winner, so the old gate ("Pick or type who fights this bout to enable Record
// bout.") held back a tap the server answers. It was removed with its hint.
import React from 'react';
import { render, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

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

// Seeded nameless members, as a fresh team holds them.
const blank = (p) => [1, 2, 3, 4, 5].map((i) => ({ id: `${p}${i}`, index: i, name: '' }));

function stubApi(squads) {
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
    fetchSquads: vi.fn().mockResolvedValue(squads),
    fetchLineupInForce: vi.fn().mockResolvedValue(null),
    fetchKachinukiRoster: vi.fn().mockResolvedValue(null),
    ...makeFakeOutbox().api,
  };
}

beforeEach(() => stubApi({}));

function kachinukiMatch(subResults) {
  return {
    id: 'm1', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A', number: 'T1' }, sideB: { id: 'team-B', name: 'Team B', number: 'T2' },
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
  it('is enabled on a played bout naming nobody, and no hint asks for a fighter', async () => {
    await mount(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [] }]));
    expect(recordBout().disabled).toBe(false);
    expect(recordBout().getAttribute('title')).toBeNull();
    expect(screen.queryByTestId('kachinuki-nofighter-hint')).toBeNull();
    expect(document.body.textContent).not.toContain('Pick or type who fights');
  });

  it('is enabled on a played bout of two nameless members, shown by number', async () => {
    stubApi({ 'team-A': blank('a'), 'team-B': blank('b') });
    await mount(kachinukiMatch([{
      position: 1, sideA: '', sideAMemberId: 'a1', sideB: '', sideBMemberId: 'b1', ipponsA: ['M'], ipponsB: [],
    }]));
    expect(recordBout().disabled).toBe(false);
    expect(screen.queryByTestId('kachinuki-nofighter-hint')).toBeNull();
    // Shiro is the match's side B (T2), Aka side A (T1).
    expect(screen.getByTestId('team-sub-match-member-label-shiro').textContent).toBe('T2.1');
    expect(screen.getByTestId('team-sub-match-member-label-aka').textContent).toBe('T1.1');
  });

  it('still waits until the bout is scored', async () => {
    await mount(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [] }]));
    expect(recordBout().disabled).toBe(true);
    expect(recordBout().getAttribute('title')).toBe('Nothing recorded for this bout yet');
  });
});
