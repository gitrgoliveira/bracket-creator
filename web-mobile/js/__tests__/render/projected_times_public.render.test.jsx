import React from 'react';
import { render, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { courtPaceLookup } from '../../court_pace.jsx';

const STUBBED_GLOBALS = {
  hasBothSides: () => true,
  matchScoreStr: () => '',
  bracketRoundLabel: () => 'Round 1',
  formatDate: (d) => d,
  StatusBadge: () => null,
  EmptyState: ({ title }) => <div>{title}</div>,
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
  useEscapeToClose: () => {},
  ResizeObserver: class { observe() {} disconnect() {} },
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null), recordScore: vi.fn() },
};

let restoreGlobals;
let ViewerSchedule;
let ViewerCompetition;
let ViewerHome;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ ViewerSchedule } = await import('../../viewer_schedule.jsx'));
  ({ ViewerCompetition } = await import('../../viewer_competition.jsx'));
  ({ ViewerHome } = await import('../../viewer_home.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 9, 29, 9, 30, 0));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const makeComp1 = (finishMinutesLate = 20) => {
  const finishMs = new Date(2026, 9, 29, 9, 10 + finishMinutesLate, 0).getTime();
  return {
    id: 'c1',
    name: 'Morning Cup',
    date: '29-10-2026',
    startTime: '09:00',
    slotMinutes: 10,
    courts: ['A'],
    status: 'pools',
    poolMatches: [
      {
        id: 'm1',
        court: 'A',
        status: 'completed',
        scheduledAt: '09:00',
        sideA: { id: 'p1', name: 'Yamada', dojo: 'Tokyo' },
        sideB: { id: 'p2', name: 'Tanaka', dojo: 'Osaka' },
        modifiedAt: finishMs,
      },
    ],
  };
};

const makeComp2 = () => ({
  id: 'c2',
  name: 'Afternoon Cup',
  date: '29-10-2026',
  startTime: '09:30',
  slotMinutes: 10,
  courts: ['A'],
  status: 'pools',
  poolMatches: [
    {
      id: 'm2',
      court: 'A',
      status: 'scheduled',
      scheduledAt: '09:30',
      sideA: { id: 'p3', name: 'Sato', dojo: 'Kyoto' },
      sideB: { id: 'p4', name: 'Suzuki', dojo: 'Nagoya' },
    },
  ],
});

const makeTournament = (c1, c2) => ({
  name: 'Tournament',
  date: '29-10-2026',
  courts: ['A'],
  competitions: [c1, c2],
});

describe('Projected times on public surfaces (bc-pace)', () => {
  it('case a: ViewerSchedule on shared court shows projected time ~09:50 instead of planned 09:30', () => {
    const c1 = makeComp1(20);
    const c2 = makeComp2();
    const tournament = makeTournament(c1, c2);
    const lookup = courtPaceLookup(tournament.competitions, { now: 9 * 60 + 30, today: '29-10-2026' });

    render(<ViewerSchedule tournament={tournament} onBack={vi.fn()} tweaks={{ showDojo: true }} paceLookup={lookup} />);

    const projected = document.querySelector('.pace-projected');
    expect(projected).toBeTruthy();
    expect(projected.querySelector('[aria-hidden="true"]').textContent).toBe('~09:50');
    expect(projected.querySelector('.sr-only').textContent).toBe('about 09:50');

    // The waiting match's plan text 09:30 must not be shown.
    const timeContainers = [...document.querySelectorAll('.tw-match__time')];
    const waitingTime = timeContainers.find((el) => el.contains(projected));
    expect(waitingTime).toBeTruthy();
    expect(waitingTime.textContent).not.toContain('09:30');
  });

  it('case b: ViewerCompetition for the second competition shows the projected time from the tournament lookup', () => {
    const c1 = makeComp1(20);
    const c2 = makeComp2();
    const tournament = makeTournament(c1, c2);
    const lookup = courtPaceLookup(tournament.competitions, { now: 9 * 60 + 30, today: '29-10-2026' });

    render(
      <ViewerCompetition
        tournament={tournament}
        competition={c2}
        pools={[]}
        poolMatches={c2.poolMatches}
        standings={{}}
        bracket={null}
        onBack={vi.fn()}
        authed={false}
        tweaks={{ cardVariant: 1, showDojo: true }}
        activeTab="overview"
        onTabChange={vi.fn()}
        paceLookup={lookup}
      />
    );

    const projected = document.querySelector('.pace-projected');
    expect(projected).toBeTruthy();
    expect(projected.querySelector('[aria-hidden="true"]').textContent).toBe('~09:50');
    expect(projected.querySelector('.sr-only').textContent).toBe('about 09:50');
  });


  it('case b2: a bout the live detail already shows running keeps its plan, though the list lookup still projects it', () => {
    const c1 = makeComp1(20);
    const c2 = makeComp2();
    const tournament = makeTournament(c1, c2);
    const lookup = courtPaceLookup(tournament.competitions, { now: 9 * 60 + 30, today: '29-10-2026' });
    const live = { ...c2, poolMatches: [{ ...c2.poolMatches[0], status: 'running' }] };

    render(
      <ViewerCompetition
        tournament={tournament}
        competition={live}
        pools={[]}
        poolMatches={live.poolMatches}
        standings={{}}
        bracket={null}
        onBack={vi.fn()}
        authed={false}
        tweaks={{ cardVariant: 1, showDojo: true }}
        activeTab="overview"
        onTabChange={vi.fn()}
        paceLookup={lookup}
      />
    );

    expect(document.body.textContent).toContain('Sato');
    expect(document.querySelector('.pace-projected')).toBeNull();
  });

  it('case h: the Home page Up next list shows the same projected time as the schedule', () => {
    const c1 = makeComp1(20);
    const c2 = makeComp2();
    const tournament = makeTournament({ ...c1, players: [] }, { ...c2, players: [] });
    const lookup = courtPaceLookup(tournament.competitions, { now: 9 * 60 + 30, today: '29-10-2026' });

    render(<ViewerHome tournament={tournament} onSelectCompetition={vi.fn()} paceLookup={lookup} />);

    const title = [...document.querySelectorAll('.viewer__upnext-title')][0];
    expect(title).toBeTruthy();
    const projected = document.querySelector('.pace-projected');
    expect(projected).toBeTruthy();
    expect(projected.querySelector('[aria-hidden="true"]').textContent).toBe('~09:50');
  });

  it('case c1: with no paceLookup prop, plan text is shown unchanged and no .pace-projected exists', () => {
    const c1 = makeComp1(20);
    const c2 = makeComp2();
    const tournament = makeTournament(c1, c2);

    render(<ViewerSchedule tournament={tournament} onBack={vi.fn()} tweaks={{ showDojo: true }} />);

    expect(document.querySelector('.pace-projected')).toBeNull();
    const times = [...document.querySelectorAll('.tw-match__time')].map((el) => el.textContent.trim());
    expect(times).toContain('09:30');
  });

  it('case c2: when match projection is within 5 minutes of plan, plan text is shown unchanged without .pace-projected', () => {
    const c1 = makeComp1(3);
    const c2 = makeComp2();
    const tournament = makeTournament(c1, c2);
    const lookup = courtPaceLookup(tournament.competitions, { now: 9 * 60 + 13, today: '29-10-2026' });

    render(<ViewerSchedule tournament={tournament} onBack={vi.fn()} tweaks={{ showDojo: true }} paceLookup={lookup} />);

    expect(document.querySelector('.pace-projected')).toBeNull();
    const times = [...document.querySelectorAll('.tw-match__time')].map((el) => el.textContent.trim());
    expect(times).toContain('09:30');
  });
});
