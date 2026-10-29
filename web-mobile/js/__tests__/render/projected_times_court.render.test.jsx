import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TvDisplay } from '../../display_scoreboard.jsx';
import { courtPaceLookup, paceKey } from '../../court_pace.jsx';

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches, c) => (matches || []).filter((m) => !c || m.court === c),
  tournamentMatches: () => [],
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchCourtMatches: vi.fn().mockResolvedValue([]),
    subscribeToEvents: () => () => {},
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
    reinstateCompetitor: vi.fn().mockResolvedValue({}),
    revertMatchToQueue: vi.fn().mockResolvedValue(true),
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

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
});

function localStamp(h, m, s = 0) {
  return new Date(2026, 9, 29, h, m, s).getTime();
}

function makeScenario({ drift = 15 } = {}) {
  const compFinishMinutes = drift === 15 ? localStamp(10, 20) : localStamp(10, 5);

  const m1Done = {
    id: 'c1-m1',
    court: 'A',
    status: 'completed',
    scheduledAt: '10:00',
    modifiedAt: compFinishMinutes,
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
  };

  const m1Sched = {
    id: 'c1-m2',
    court: 'A',
    status: 'scheduled',
    scheduledAt: '10:30',
    sideA: { id: 'p3', name: 'Sato' },
    sideB: { id: 'p4', name: 'Suzuki' },
  };

  const m2Done = {
    id: 'c2-m1',
    court: 'A',
    status: 'completed',
    scheduledAt: '10:00',
    modifiedAt: compFinishMinutes,
    sideA: { id: 'p5', name: 'Takahashi' },
    sideB: { id: 'p6', name: 'Watanabe' },
  };

  const m2Sched = {
    id: 'c2-m2',
    court: 'A',
    status: 'scheduled',
    scheduledAt: '10:35',
    sideA: { id: 'p7', name: 'Ito' },
    sideB: { id: 'p8', name: 'Nakamura' },
  };

  const c1 = {
    id: 'c1',
    name: 'Competition 1',
    date: '29-10-2026',
    startTime: '09:00',
    slotMinutes: 5,
    status: 'running',
    format: 'pools',
    poolMatches: [m1Done, m1Sched],
  };

  const c2 = {
    id: 'c2',
    name: 'Competition 2',
    date: '29-10-2026',
    startTime: '10:00',
    slotMinutes: 5,
    status: 'running',
    format: 'pools',
    poolMatches: [m2Done, m2Sched],
  };

  return { c1, c2, m1Done, m1Sched, m2Done, m2Sched };
}

function renderConsole(tournament, court = 'A') {
  return render(
    <AdminShiaijoPage
      tournament={tournament}
      court={court}
      onBack={vi.fn()}
      onEditScore={vi.fn()}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password=""
      showToast={vi.fn()}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />
  );
}

describe('Court projected times on court surfaces (bc-pace)', () => {
  it('Console: the Up next card shows "~HH:MM" when behind, plan is not shown beside it, and second competition completed match counts toward drift', async () => {
    vi.setSystemTime(new Date(2026, 9, 29, 10, 40, 0));
    const { c1, c2, m1Done, m1Sched, m2Done, m2Sched } = makeScenario({ drift: 15 });

    const lookup = courtPaceLookup([c1, c2], { now: 10 * 60 + 40, today: '29-10-2026' });
    const expectedEntry = lookup.byMatch.get(paceKey('c1', 'c1-m2'));
    expect(expectedEntry).toBeDefined();
    expect(expectedEntry.display.projected).toBe(true);
    expect(expectedEntry.display.text).toBe('~10:45');
    expect(expectedEntry.display.ariaLabel).toBe('about 10:45');

    window.API.fetchCourtMatches = vi.fn().mockResolvedValue([c1, c2]);
    window.tournamentMatches = () => [
      { ...m1Done, compId: 'c1', compName: 'Competition 1' },
      { ...m1Sched, compId: 'c1', compName: 'Competition 1' },
      { ...m2Done, compId: 'c2', compName: 'Competition 2' },
      { ...m2Sched, compId: 'c2', compName: 'Competition 2' },
    ];

    let utils;
    await act(async () => {
      utils = renderConsole({ name: 'Tournament', courts: ['A'], competitions: [c1, c2] }, 'A');
    });

    const card = utils.container.querySelector('.shiaijo-upnext__card');
    expect(card).not.toBeNull();
    const timeEl = card.querySelector('.shiaijo-upnext__time');
    expect(timeEl).not.toBeNull();

    const projSpan = timeEl.querySelector('.pace-projected');
    expect(projSpan).not.toBeNull();
    expect(projSpan.textContent).toBe('~10:45');
    expect(projSpan.getAttribute('aria-label')).toBe('about 10:45');
    expect(timeEl.textContent).not.toContain('10:30');
  });

  it('Console: when the court is on time (projection within 5 minutes of plan), shows plan text unchanged and no .pace-projected element', async () => {
    vi.setSystemTime(new Date(2026, 9, 29, 10, 25, 0));
    const { c1, c2, m1Done, m1Sched, m2Done, m2Sched } = makeScenario({ drift: 0 });

    const lookup = courtPaceLookup([c1, c2], { now: 10 * 60 + 25, today: '29-10-2026' });
    const expectedEntry = lookup.byMatch.get(paceKey('c1', 'c1-m2'));
    expect(expectedEntry).toBeDefined();
    expect(expectedEntry.display.projected).toBe(false);

    window.API.fetchCourtMatches = vi.fn().mockResolvedValue([c1, c2]);
    window.tournamentMatches = () => [
      { ...m1Done, compId: 'c1', compName: 'Competition 1' },
      { ...m1Sched, compId: 'c1', compName: 'Competition 1' },
      { ...m2Done, compId: 'c2', compName: 'Competition 2' },
      { ...m2Sched, compId: 'c2', compName: 'Competition 2' },
    ];

    let utils;
    await act(async () => {
      utils = renderConsole({ name: 'Tournament', courts: ['A'], competitions: [c1, c2] }, 'A');
    });

    const card = utils.container.querySelector('.shiaijo-upnext__card');
    expect(card).not.toBeNull();
    const timeEl = card.querySelector('.shiaijo-upnext__time');
    expect(timeEl).not.toBeNull();

    expect(timeEl.querySelector('.pace-projected')).toBeNull();
    expect(timeEl.textContent).toContain('10:30');
  });

  it('Console: advancing fake timers by 60000 ms re-renders and the displayed projected time moves with the clock', async () => {
    vi.setSystemTime(new Date(2026, 9, 29, 10, 45, 0));
    const { c1, c2, m1Done, m1Sched, m2Done, m2Sched } = makeScenario({ drift: 15 });

    window.API.fetchCourtMatches = vi.fn().mockResolvedValue([c1, c2]);
    window.tournamentMatches = () => [
      { ...m1Done, compId: 'c1', compName: 'Competition 1' },
      { ...m1Sched, compId: 'c1', compName: 'Competition 1' },
      { ...m2Done, compId: 'c2', compName: 'Competition 2' },
      { ...m2Sched, compId: 'c2', compName: 'Competition 2' },
    ];

    let utils;
    await act(async () => {
      utils = renderConsole({ name: 'Tournament', courts: ['A'], competitions: [c1, c2] }, 'A');
    });

    const card = utils.container.querySelector('.shiaijo-upnext__card');
    expect(card).not.toBeNull();
    const timeEl = card.querySelector('.shiaijo-upnext__time');
    expect(timeEl.querySelector('.pace-projected').textContent).toBe('~10:45');

    await act(async () => {
      vi.advanceTimersByTime(60000);
    });

    const projSpanAfter = card.querySelector('.shiaijo-upnext__time .pace-projected');
    expect(projSpanAfter).not.toBeNull();
    expect(projSpanAfter.textContent).toBe('~10:46');
    expect(card.querySelector('.shiaijo-upnext__time').textContent).not.toContain('~10:45');
  });

  it('TV board: the NEXT line shows data-testid="tvd-next-time" when behind, and shows none when on time', () => {
    vi.setSystemTime(new Date(2026, 9, 29, 10, 40, 0));
    const { c1, c2 } = makeScenario({ drift: 15 });

    const { container: behindContainer } = render(
      <TvDisplay
        court="A"
        tournament={{ name: 'Tournament' }}
        competitions={[c1, c2]}
      />
    );

    const nextTimeEl = behindContainer.querySelector('[data-testid="tvd-next-time"]');
    expect(nextTimeEl).not.toBeNull();
    expect(nextTimeEl.className).toContain('pace-projected');
    expect(nextTimeEl.textContent).toBe('~10:50');
    expect(nextTimeEl.getAttribute('aria-label')).toBe('about 10:50');

    vi.setSystemTime(new Date(2026, 9, 29, 10, 25, 0));
    const { c1: c1OnTime, c2: c2OnTime } = makeScenario({ drift: 0 });

    const { container: onTimeContainer } = render(
      <TvDisplay
        court="A"
        tournament={{ name: 'Tournament' }}
        competitions={[c1OnTime, c2OnTime]}
      />
    );

    expect(onTimeContainer.querySelector('[data-testid="tvd-next-time"]')).toBeNull();
  });
});
