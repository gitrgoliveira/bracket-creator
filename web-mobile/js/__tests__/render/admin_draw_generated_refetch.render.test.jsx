// bc-drev FR13: a swap in the draw (or a draw made or discarded) on another
// organiser device broadcasts draw_generated (draw_discarded), and the admin
// console reads the competition it shows again, so its Overview, Pools
// (preview) and Bracket (preview) show the draw as stored without a reload.
// Before, only the court console and the calendar listened for these events.
import React from 'react';
import { render, act, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let listener = null;
const detail = () => ({
  config: { id: 'c1', name: 'Open', status: 'draw-ready', format: 'mixed', kind: 'individual', courts: ['A'], players: [] },
  pools: [], poolMatches: [], standings: {}, bracket: null,
});

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import by admin.jsx.
  AdminCompetition: () => <div data-testid="competition" />,
  confirmDialog: vi.fn(),
  API: {
    subscribeToEvents: vi.fn(),
    fetchCompetitions: vi.fn(),
    fetchTournament: vi.fn(),
    fetchCompetitionDetails: vi.fn(),
    reconnectEvents: vi.fn(),
  },
};

let restore;
let AdminApp;

beforeAll(async () => {
  restore = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin.jsx');
  AdminApp = window.AdminApp;
});
afterAll(() => restore());

beforeEach(() => {
  listener = null;
  // No jitter: the refetch is scheduled at once and the list refresh at 500 ms.
  vi.spyOn(Math, 'random').mockReturnValue(0);
  window.API.subscribeToEvents = vi.fn((cb) => { listener = cb; return () => { listener = null; }; });
  window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue(detail());
  window.API.fetchCompetitions = vi.fn().mockResolvedValue([detail().config]);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

async function mount() {
  await act(async () => {
    render(
      <AdminApp
        tournament={{ name: 'Cup', courts: ['A'], competitions: [detail().config] }}
        onUpdate={vi.fn()} onLogout={vi.fn()} onViewerMode={vi.fn()} onPasswordChange={vi.fn()}
        tweaks={{}} password="pw" view={{ kind: 'competition', id: 'c1', section: 'overview' }} setView={vi.fn()}
        showToast={vi.fn()} authConfig={{}}
      />,
    );
  });
  expect(window.API.fetchCompetitionDetails).toHaveBeenCalledTimes(1);
}

const emit = (event, waitMs = 20) => act(async () => {
  listener(event);
  await new Promise((resolve) => { setTimeout(resolve, waitMs); });
});

describe('the admin console on a draw event', () => {
  for (const type of ['draw_generated', 'draw_discarded']) {
    it(`reads the competition it shows again on ${type}`, async () => {
      await mount();
      await emit({ type, seq: 1, data: { competitionId: 'c1' } });
      expect(window.API.fetchCompetitionDetails).toHaveBeenCalledTimes(2);
      expect(window.API.fetchCompetitionDetails).toHaveBeenLastCalledWith('c1');
    });

    it(`refreshes the competitions list, and not the competition shown, on ${type} for another competition`, async () => {
      await mount();
      await emit({ type, seq: 1, data: { competitionId: 'other' } }, 600);
      expect(window.API.fetchCompetitionDetails).toHaveBeenCalledTimes(1);
      expect(window.API.fetchCompetitions).toHaveBeenCalled();
    });
  }
});
