// Stage 6 review 10, m4: the court console forgets an answered status move
// (a finish, a decision, a send back) of a match only when the TOURNAMENT no
// longer holds that match. The owner of the moves is the tab's
// (api_client.jsx), so opening court B must not wipe court A's answered moves,
// and a finished match moved to another court is not gone.
import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { createOwnStatusMoves } from '../helpers/own_status_moves.js';

const outbox = createOwnStatusMoves();
const forgetNotIn = vi.fn();

const onA = {
  id: 'Pool A-1', compId: 'c1', compName: 'Cup', status: 'completed', phase: 'pool',
  court: 'A', scheduledAt: '10:00', modifiedAt: 100,
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
};
const onB = {
  id: 'Pool A-2', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool',
  court: 'B', scheduledAt: '10:10', modifiedAt: 100,
  sideA: { id: 'p3', name: 'Sato' }, sideB: { id: 'p4', name: 'Suzuki' },
};

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: () => null,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  // The aggregate holds every court's matches; the court's feed only its own.
  tournamentMatches: ({ competitions }) => (competitions || []).flatMap((c) => c.matchesForTest || []),
  filterMatchesByCourt: (matches, court) => matches.filter((m) => m.court === court),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
    forgetOwnStatusMovesNotIn: forgetNotIn,
    ...outbox.api,
  },
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  compMatches: () => [],
};

let restoreGlobals;
let AdminShiaijoPage;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});
afterAll(() => restoreGlobals());

describe('the court console forgets a status move', () => {
  it('only for a match the tournament no longer holds, whatever court is open', async () => {
    const tournament = { name: 'T', courts: ['A', 'B'], competitions: [{ id: 'c1', name: 'Cup', matchesForTest: [onA, onB] }] };
    await act(async () => {
      render(
        <AdminShiaijoPage
          tournament={tournament} court="B" onBack={vi.fn()} onEditScore={vi.fn()} onMoveCourt={vi.fn()}
          onLogout={vi.fn()} onViewerMode={vi.fn()} password="" showToast={vi.fn()} tweaks={{}} onSwitchCourt={vi.fn()}
        />,
      );
    });
    expect(forgetNotIn).toHaveBeenCalled();
    for (const [held] of forgetNotIn.mock.calls) {
      const ids = held.map((m) => `${m.compId}:${m.id}`);
      expect(ids, 'court A\'s match is still the tournament\'s').toContain('c1:Pool A-1');
      expect(ids).toContain('c1:Pool A-2');
    }
  });
});
