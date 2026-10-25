import React from 'react';
import { render, act, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { pointerTap } from '../helpers/tap_events.js';

const CALL_TITLE = 'Announce this match to spectators and competitors';

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches, court) => (matches || []).filter((m) => (m.court || '') === (court || '')),
  tournamentMatches: (t) => (t && t.competitions || []).flatMap((c) => (c.poolMatches || []).map((m) => ({ ...m, compId: c.id, compName: c.name || '' }))),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchCourtMatches: vi.fn(),
    subscribeToEvents: vi.fn(),
    sendAnnouncement: vi.fn(),
    deleteAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
    reinstateCompetitor: vi.fn().mockResolvedValue({}),
    subscribeLocalMatchPatch: vi.fn(() => () => {}),
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

let sseHandler = null;
let nextAnnId = 0;
let localPatchListeners = new Set();

function emitLocalMatchPatch(compId, matchId, fields) {
  const event = {
    type: 'match_updated',
    local: true,
    data: { competitionId: compId, matchId: matchId, result: { id: matchId, ...fields } },
  };
  for (const cb of localPatchListeners) cb(event);
}

beforeEach(() => {
  nextAnnId = 0;
  sseHandler = null;
  localPatchListeners.clear();
  window.API.sendAnnouncement = vi.fn(async () => ({ id: `ann-${++nextAnnId}`, message: 'Call sent' }));
  window.API.deleteAnnouncement = vi.fn(async () => {});
  window.API.subscribeToEvents = vi.fn((cb) => {
    sseHandler = cb;
    return () => { sseHandler = null; };
  });
  window.API.subscribeLocalMatchPatch = vi.fn((cb) => {
    localPatchListeners.add(cb);
    return () => { localPatchListeners.delete(cb); };
  });
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

function makeComp(matchOverrides = {}) {
  return {
    id: 'c1',
    name: 'Cup',
    status: 'pools',
    poolMatches: [
      {
        id: 'm1',
        court: 'A',
        status: 'scheduled',
        phase: 'pool',
        poolName: 'Pool 1',
        scheduledAt: '10:00',
        sideA: { id: 'p1', name: 'Yamada' },
        sideB: { id: 'p2', name: 'Tanaka' },
        ...matchOverrides,
      },
      {
        id: 'm2',
        court: 'A',
        status: 'scheduled',
        phase: 'pool',
        poolName: 'Pool 1',
        scheduledAt: '10:05',
        sideA: { id: 'p3', name: 'Suzuki' },
        sideB: { id: 'p4', name: 'Sato' },
      },
    ],
  };
}

async function mountConsoleAndCallMatch(initialComp = makeComp()) {
  window.API.fetchCourtMatches = vi.fn().mockResolvedValue([initialComp]);
  render(
    <AdminShiaijoPage
      tournament={{ name: 'Test Tournament', courts: ['A', 'B'], competitions: [initialComp] }}
      court="A"
      onBack={vi.fn()}
      onEditScore={vi.fn().mockResolvedValue({ applied: true })}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password=""
      showToast={vi.fn()}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />
  );
  await act(async () => {
    await Promise.resolve();
  });
  const callBtn = screen.getAllByTitle(CALL_TITLE)[0];
  await pointerTap(callBtn);
  await act(async () => {
    await Promise.resolve();
  });
  expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
  return { annId: `ann-${nextAnnId}` };
}

describe('Court call withdrawn on move', () => {
  it('a. withdraws call when match_updated names another court at top level', async () => {
    const { annId } = await mountConsoleAndCallMatch();
    await act(async () => {
      sseHandler({
        type: 'match_updated',
        data: { competitionId: 'c1', matchId: 'm1', court: 'B' },
      });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith(annId, '');
  });

  it('b. withdraws call when match_updated names another court in result.court', async () => {
    const { annId } = await mountConsoleAndCallMatch();
    await act(async () => {
      sseHandler({
        type: 'match_updated',
        data: { competitionId: 'c1', result: { id: 'm1', court: 'B' } },
      });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith(annId, '');
  });

  it('c. withdraws call when a refetch lists the scheduled match on another court without push', async () => {
    const { annId } = await mountConsoleAndCallMatch();
    const compMoved = makeComp({ court: 'B' });
    window.API.fetchCourtMatches.mockResolvedValueOnce([compMoved]);
    await act(async () => {
      screen.getByRole('button', { name: /refresh/i }).click();
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith(annId, '');
  });

  it('d. leaves call alone when a refetch no longer lists the competition at all', async () => {
    await mountConsoleAndCallMatch();
    window.API.fetchCourtMatches.mockResolvedValueOnce([]);
    await act(async () => {
      screen.getByRole('button', { name: /refresh/i }).click();
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });

  it('e. leaves call alone when match_updated carries no court', async () => {
    await mountConsoleAndCallMatch();
    await act(async () => {
      sseHandler({
        type: 'match_updated',
        data: { competitionId: 'c1', matchId: 'm1', result: { id: 'm1', status: 'scheduled' } },
      });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });

  it('f. keeps call when push names own court and refetch lists on own court', async () => {
    await mountConsoleAndCallMatch();
    await act(async () => {
      sseHandler({
        type: 'match_updated',
        data: { competitionId: 'c1', matchId: 'm1', court: 'A' },
      });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

    window.API.fetchCourtMatches.mockResolvedValueOnce([makeComp({ court: 'A' })]);
    await act(async () => {
      screen.getByRole('button', { name: /refresh/i }).click();
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });

  it('g. leaves call alone while refetch is unresolved or answers empty', async () => {
    await mountConsoleAndCallMatch();
    let resolveRefetch;
    const pendingPromise = new Promise((resolve) => {
      resolveRefetch = resolve;
    });
    window.API.fetchCourtMatches.mockReturnValueOnce(pendingPromise);

    await act(async () => {
      screen.getByRole('button', { name: /refresh/i }).click();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

    await act(async () => {
      resolveRefetch([]);
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });

  it('h. withdraws exactly once when both move push and refetch arrive', async () => {
    const { annId } = await mountConsoleAndCallMatch();
    await act(async () => {
      sseHandler({
        type: 'match_updated',
        data: { competitionId: 'c1', matchId: 'm1', court: 'B' },
      });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith(annId, '');

    const compMoved = makeComp({ court: 'B' });
    window.API.fetchCourtMatches.mockResolvedValueOnce([compMoved]);
    await act(async () => {
      screen.getByRole('button', { name: /refresh/i }).click();
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
  });

  it('i. withdraws call when a refetch lists the match on another court already running', async () => {
    const { annId } = await mountConsoleAndCallMatch();
    const compMoved = makeComp({ court: 'B', status: 'running' });
    window.API.fetchCourtMatches.mockResolvedValueOnce([compMoved]);
    await act(async () => {
      screen.getByRole('button', { name: /refresh/i }).click();
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith(annId, '');
  });

  it('j. a local optimistic court patch alone does NOT withdraw the call', async () => {
    await mountConsoleAndCallMatch();
    await act(async () => {
      emitLocalMatchPatch('c1', 'm1', { court: 'B' });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });

  it('k. after the server push or refetch shows the match on the other court, the call IS withdrawn exactly once', async () => {
    const { annId } = await mountConsoleAndCallMatch();
    await act(async () => {
      emitLocalMatchPatch('c1', 'm1', { court: 'B' });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

    // Server push arrives
    await act(async () => {
      sseHandler({
        type: 'match_updated',
        data: { competitionId: 'c1', matchId: 'm1', court: 'B' },
      });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
    expect(window.API.deleteAnnouncement).toHaveBeenCalledWith(annId, '');

    // Subsequent refetch also showing it on court B does not withdraw again
    const compMoved = makeComp({ court: 'B' });
    window.API.fetchCourtMatches.mockResolvedValueOnce([compMoved]);
    await act(async () => {
      screen.getByRole('button', { name: /refresh/i }).click();
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
  });

  it('l. a refused move (the court put back) leaves the call up', async () => {
    await mountConsoleAndCallMatch();
    await act(async () => {
      emitLocalMatchPatch('c1', 'm1', { court: 'B' });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

    // Refusal reverts the court via _revertLocalFields
    await act(async () => {
      emitLocalMatchPatch('c1', 'm1', { court: 'A' });
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

    // Refetch showing match still on court A leaves call up
    window.API.fetchCourtMatches.mockResolvedValueOnce([makeComp({ court: 'A' })]);
    await act(async () => {
      screen.getByRole('button', { name: /refresh/i }).click();
      await Promise.resolve();
    });
    expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();
  });
});
