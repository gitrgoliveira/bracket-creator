// bc-kno1 (operator decision Q6 (b)): Record bout does not wait for a named
// fighter. With no lineup the server seats bout 1 by member number, a nameless
// member is a fighter, and the advance appends the next member against the
// winner, so the old gate ("Pick or type who fights this bout to enable Record
// bout.") held back a tap the server answers. It was removed with its hint.
import React from 'react';
import { render, act, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
import { toastSnapshot, dismissToast, resetToastQueue } from '../../toast_queue.jsx';

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

beforeEach(() => { resetToastQueue(); stubApi({}); });

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

// Under a roster warning (team-members.yaml or lineups.yaml unread) nobody is
// seated, so a played bout can name no fighter at all. Record bout saves it,
// the server adds no pairing from it and answers a plain 200: the sheet says so.
describe('Record bout on a bout that names no fighter, under a roster warning', () => {
  const WARNING = "Team members could not be read, so fighters are not seated by member number. Add each bout's pairing by hand.";
  const warned = {
    warning: WARNING,
    nextPairing: false,
    sideA: { lineupFound: false, on: { name: '', memberId: '' }, remaining: [], exhausted: false },
    sideB: { lineupFound: false, on: { name: '', memberId: '' }, remaining: [], exhausted: false },
  };

  async function tapRecord(match, roster) {
    window.API.fetchKachinukiRoster = vi.fn().mockResolvedValue(roster);
    await mount(match);
    await act(async () => { dismissToast(); });
    await act(async () => { fireEvent.click(recordBout()); });
    for (let i = 0; i < 4; i++) await act(async () => { await Promise.resolve(); });
  }

  // Under the warning a second Record bout adds no pairing either, even once the
  // fighters are named (the server works out no next fighter), so the sheet points to
  // the button that does add one.
  it('says the score is saved, no pairing was added, and to add the next bout by hand', async () => {
    await tapRecord(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [] }]), warned);
    expect(toastSnapshot()).toMatchObject({ type: 'warning' });
    expect(toastSnapshot().message).toBe('The score is saved, but no fighter is named on this bout, so no next pairing was added. Pick or type who fought and tap Record bout again, then tap + Add next bout manually and pick or type both fighters of the next bout.');
    expect(screen.getByTestId('kachinuki-add-bout-button').textContent, 'the button the sentence names').toContain('+ Add next bout manually');
    expect(screen.getByTestId('kachinuki-add-bout-button').disabled, 'and it is open').toBe(false);
  });

  it('says nothing for a bout that names a fighter', async () => {
    await tapRecord(kachinukiMatch([{ position: 1, sideA: 'Ueda', sideB: 'Kudo', ipponsA: ['M'], ipponsB: [] }]), warned);
    expect(toastSnapshot()).toBeNull();
  });

  it('says nothing without a warning, where the server seats the bout itself', async () => {
    await tapRecord(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [] }]), null);
    expect(toastSnapshot()).toBeNull();
  });
});
