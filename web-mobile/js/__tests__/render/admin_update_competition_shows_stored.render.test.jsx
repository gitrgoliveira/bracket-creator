// Stage 6 review 10, m10: a competition save (updateCompetition, admin.jsx)
// shows what it stored even when the refetch after it fails. The PUT answers
// the stored competition; the page applies it at once, so the seed box shows
// the saved seed and never the old one until a later push.
import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const probe = { props: null };

const detail = (seed) => ({
  config: {
    id: 'c1', name: 'Autumn Cup', status: 'setup', format: 'mixed', kind: 'individual', courts: ['A'],
    players: [{ id: 'p-1', name: 'Alice', dojo: 'D1', seed }],
  },
  pools: [], poolMatches: [], standings: {}, bracket: null,
});

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import by admin.jsx.
  AdminCompetition: (props) => { probe.props = props; return <div data-testid="competition" />; },
  confirmDialog: vi.fn(),
  API: {
    subscribeToEvents: vi.fn(() => () => {}),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    fetchCompetitionDetails: vi.fn(),
    updateCompetition: vi.fn(),
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

describe('a competition save', () => {
  it('shows the competition it stored when the refetch after it fails', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    window.API.fetchCompetitionDetails
      .mockResolvedValueOnce(detail(null))
      .mockRejectedValue(new Error('offline'));
    // The server answers the stored competition, players in its own shape.
    window.API.updateCompetition.mockResolvedValue({
      ...detail(null).config,
      players: [{ id: 'p-1', name: 'Alice', dojo: 'D1', seed: 3 }],
      warnings: [],
    });
    const tournament = { name: 'Cup', courts: ['A'], competitions: [detail(null).config] };
    await act(async () => {
      render(
        <AdminApp
          tournament={tournament}
          onUpdate={vi.fn()} onLogout={vi.fn()} onViewerMode={vi.fn()} onPasswordChange={vi.fn()}
          tweaks={{}} password="pw" view={{ kind: 'competition', id: 'c1', section: 'participants' }} setView={vi.fn()}
          showToast={vi.fn()} authConfig={{}}
        />,
      );
    });
    expect(probe.props.competition.players[0].seed ?? null).toBe(null);
    let warnings;
    await act(async () => {
      warnings = await probe.props.onUpdate({ ...probe.props.competition, players: [{ id: 'p-1', name: 'Alice', dojo: 'D1', seed: 3 }] });
    });
    expect(warnings).toEqual([]);
    expect(probe.props.competition.players[0].seed, 'the stored seed, though the refetch failed').toBe(3);
    expect(probe.props.competition.warnings, 'the answer\'s warnings never ride on the competition').toBeUndefined();
    logged.mockRestore();
  });
});
