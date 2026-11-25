// F4: kachinuki's End match holds its width between its two taps. Armed, it
// reads "Tap again: <outcome>" -- longer than "End match" -- so without a
// reserved width the button grew when tapped and the second tap of a double tap
// could land beside it (or the label wrapped at 390 px). Like the team sheet's
// Fusensho, it carries .holds-label with data-reserve: the longest of its armed
// labels, laid hidden in the same grid cell as the shown text, so the button is
// as wide in both states. jsdom does no layout, so this pins the mechanism
// (class, reserve, one shown text) and the real width is checked in a browser.
// No Finish warning belongs here: the public Finish and End match gain no note.
import React from 'react';
import { render, act, screen, fireEvent } from '@testing-library/react';
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

const endButton = () => screen.getByTestId('kachinuki-end-match-button');

describe('kachinuki End match holds its width between its two taps', () => {
  // A scored 1-1 bout on the live row: End match is enabled and would record a draw.
  const TIED_LIVE = {
    position: 1, sideA: 'Nomura', sideB: 'Sato', decision: 'hikiwake',
    ipponsA: ['M'], ipponsB: ['K'],
  };
  // Aka's Nomura won the live bout: End match would record "Aka wins".
  const WON_LIVE = {
    position: 1, sideA: 'Nomura', sideB: 'Sato', decision: 'fought',
    ipponsA: ['M'], ipponsB: [],
  };

  it('reserves room for the longest armed label while it still reads End match', async () => {
    await mount(kachinukiMatch([TIED_LIVE]));
    const end = endButton();
    expect(end.classList.contains('holds-label')).toBe(true);
    expect(end.querySelector('.holds-label__text').textContent).toBe('End match');
    expect(end.textContent).toBe('End match');
    const reserve = end.getAttribute('data-reserve');
    // The armed labels are "Tap again: Aka wins", "Tap again: Shiro wins" and
    // "Tap again: Draw (hikiwake)": the reserve is the longest of them.
    expect(reserve).toBe('Tap again: Draw (hikiwake)');
  });

  it('keeps the same reserve when armed, with the shown text still one label', async () => {
    await mount(kachinukiMatch([TIED_LIVE]));
    const before = endButton().getAttribute('data-reserve');
    await act(async () => { fireEvent.click(endButton()); });
    const armed = endButton();
    expect(armed.textContent).toBe('Tap again: Draw (hikiwake)');
    expect(armed.getAttribute('data-reserve')).toBe(before);
    expect(armed.classList.contains('holds-label')).toBe(true);
    expect(armed.querySelector('.holds-label__text').textContent).toBe('Tap again: Draw (hikiwake)');
  });

  it('holds the same reserve when the armed label is the shorter "Aka wins"', async () => {
    await mount(kachinukiMatch([WON_LIVE]));
    const before = endButton().getAttribute('data-reserve');
    await act(async () => { fireEvent.click(endButton()); });
    expect(endButton().textContent).toBe('Tap again: Aka wins');
    expect(endButton().getAttribute('data-reserve')).toBe(before);
    expect(before.length).toBeGreaterThanOrEqual('Tap again: Shiro wins'.length);
  });
});
