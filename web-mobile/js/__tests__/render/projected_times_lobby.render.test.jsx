import React from 'react';
import { render, cleanup, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { courtPaceLookup, paceKey } from '../../court_pace.jsx';

const STUBBED_GLOBALS = {
  AppRouter: {
    useQuery: () => ({ court: 'all' }),
    parseSearch: (s) => {
      const q = {};
      const params = new URLSearchParams(s);
      for (const [k, v] of params.entries()) q[k] = v;
      return q;
    },
  },
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
};

let restoreGlobals;
let DisplayRoute;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../bracket.jsx');
  ({ DisplayRoute } = await import('../../display.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  vi.useFakeTimers();
  window.history.pushState(null, '', '/display?court=all');
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
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

  const m1Running = {
    id: 'c1-m2',
    court: 'A',
    status: 'running',
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
    poolMatches: [m1Done, m1Running],
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

  return { c1, c2, m1Done, m1Running, m2Done, m2Sched };
}

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
      {
        id: 'm1-running',
        court: 'A',
        status: 'running',
        scheduledAt: '09:10',
        sideA: { id: 'p3', name: 'Sato', dojo: 'Kyoto' },
        sideB: { id: 'p4', name: 'Suzuki', dojo: 'Nagoya' },
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
      sideA: { id: 'p5', name: 'Takahashi', dojo: 'Fukuoka' },
      sideB: { id: 'p6', name: 'Watanabe', dojo: 'Sapporo' },
    },
  ],
});

describe('Projected times on standalone lobby display (route /display?court=all)', () => {
  it('renders projected time ~10:50 in the lobby cell for a scheduled match when court is behind schedule', () => {
    vi.setSystemTime(new Date(2026, 9, 29, 10, 40, 0));
    const { c1, c2 } = makeScenario({ drift: 15 });
    const tournament = { name: 'Tournament', courts: ['A'] };

    const lookup = courtPaceLookup([c1, c2], { now: 10 * 60 + 40, today: '29-10-2026' });
    const expected = lookup.byMatch.get(paceKey('c2', 'c2-m2'));
    expect(expected).toBeDefined();
    expect(expected.display.projected).toBe(true);
    expect(expected.display.text).toBe('~10:50');
    expect(expected.display.ariaLabel).toBe('about 10:50');

    const { container } = render(
      <DisplayRoute tournament={tournament} competitions={[c1, c2]} />
    );

    const projected = container.querySelector('.pace-projected');
    expect(projected).not.toBeNull();
    expect(projected.querySelector('[aria-hidden="true"]').textContent).toBe('~10:50');
    expect(projected.querySelector('.sr-only').textContent).toBe('about 10:50');

    const cell = projected.closest('.lobby-cell');
    expect(cell).not.toBeNull();
    expect(cell.textContent).not.toContain('10:35');
  });

  it('renders projected time ~09:50 matching public schedule assertion', () => {
    vi.setSystemTime(new Date(2026, 9, 29, 9, 30, 0));
    const c1 = makeComp1(20);
    const c2 = makeComp2();
    const tournament = { name: 'Tournament', courts: ['A'] };

    const lookup = courtPaceLookup([c1, c2], { now: 9 * 60 + 30, today: '29-10-2026' });
    const expected = lookup.byMatch.get(paceKey('c2', 'm2'));
    expect(expected).toBeDefined();
    expect(expected.display.projected).toBe(true);
    expect(expected.display.text).toBe('~09:50');
    expect(expected.display.ariaLabel).toBe('about 09:50');

    const { container } = render(
      <DisplayRoute tournament={tournament} competitions={[c1, c2]} />
    );

    const projected = container.querySelector('.pace-projected');
    expect(projected).not.toBeNull();
    expect(projected.querySelector('[aria-hidden="true"]').textContent).toBe('~09:50');
    expect(projected.querySelector('.sr-only').textContent).toBe('about 09:50');

    const cell = projected.closest('.lobby-cell');
    expect(cell).not.toBeNull();
    expect(cell.textContent).not.toContain('09:30');
  });

  it('shows planned time without pace-projected element when court is on time', () => {
    vi.setSystemTime(new Date(2026, 9, 29, 10, 25, 0));
    const { c1, c2 } = makeScenario({ drift: 0 });
    const tournament = { name: 'Tournament', courts: ['A'] };

    const { container } = render(
      <DisplayRoute tournament={tournament} competitions={[c1, c2]} />
    );

    expect(container.querySelector('.pace-projected')).toBeNull();
    const cells = container.querySelectorAll('.lobby-cell');
    expect(cells.length).toBeGreaterThanOrEqual(2);
    expect(cells[1].textContent).toContain('10:35');
  });

  it('advancing timers by 60000 ms triggers pace recomputation', async () => {
    vi.setSystemTime(new Date(2026, 9, 29, 10, 45, 0));
    const { c1, c2 } = makeScenario({ drift: 15 });
    const tournament = { name: 'Tournament', courts: ['A'] };

    const { container } = render(
      <DisplayRoute tournament={tournament} competitions={[c1, c2]} />
    );

    const projBefore = container.querySelector('.pace-projected');
    expect(projBefore).not.toBeNull();
    expect(projBefore.querySelector('[aria-hidden="true"]').textContent).toBe('~10:50');

    await act(async () => {
      vi.advanceTimersByTime(60000);
    });

    const projAfter = container.querySelector('.pace-projected');
    expect(projAfter).not.toBeNull();
  });
});
