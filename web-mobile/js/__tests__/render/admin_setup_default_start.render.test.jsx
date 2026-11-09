import React from 'react';
import { render, act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

function addMinutes(t, mins) {
  const [h, m] = t.split(':').map(Number);
  const total = h * 60 + m + mins;
  const hh = Math.floor(total / 60) % 24;
  const mm = total % 60;
  return `${String(hh).padStart(2, '0')}:${String(mm).padStart(2, '0')}`;
}

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  BrandingManager: Stub('BrandingManager'),
  SponsorsManager: Stub('SponsorsManager'),
  isNonPublicOrigin: () => false,
  buildCompetition: (cfg) => ({ ...cfg }),
  addMinutes,
  API: {
    fetchCalendar: vi.fn().mockResolvedValue({ days: [], courts: [], unusedPauses: [], competitions: [] }),
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    getNumberPrefixDefault: vi.fn().mockResolvedValue({ numberPrefix: '' }),
  },
};

let restoreGlobals;
let AdminCreateCompetition;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_setup.jsx');
  AdminCreateCompetition = window.AdminCreateCompetition;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.API.fetchCalendar = vi.fn().mockResolvedValue({ days: [], courts: [], unusedPauses: [], competitions: [] });
  window.API.estimateCompetitionSchedule = vi.fn().mockResolvedValue(null);
  window.API.getNumberPrefixDefault = vi.fn().mockResolvedValue({ numberPrefix: '' });
});

function makeTournament(overrides = {}) {
  return {
    name: 'Spring Taikai',
    date: '10-08-2026',
    durationDays: 1,
    courts: ['A', 'B', 'C'],
    competitions: [],
    ...overrides,
  };
}

function makeCalendar(competitions = [], days = [{ day: 1, date: '10-08-2026', breaks: [] }], courts = ['A', 'B', 'C']) {
  return {
    days,
    courts,
    unusedPauses: [],
    competitions,
  };
}

async function mountForm({ tournament = makeTournament(), onCreate = vi.fn(), password = 'test-password' } = {}) {
  let result;
  await act(async () => {
    result = render(
      <AdminCreateCompetition
        tournament={tournament}
        onCancel={noop}
        onCreate={onCreate}
        onLogout={noop}
        onViewerMode={noop}
        password={password}
      />
    );
  });
  return { ...result, onCreate };
}

const clickPill = async (container, label) => {
  const pill = Array.from(container.querySelectorAll('button.radio-pill'))
    .find((b) => b.textContent.trim() === label);
  expect(pill, `pill "${label}" not found`).not.toBeUndefined();
  await act(async () => { fireEvent.click(pill); });
};

describe('AdminCreateCompetition default start time stacking (bc-seqc)', () => {
  it('(a) stacks after same-day competition on court A using calendar window end', async () => {
    window.API.fetchCalendar = vi.fn().mockResolvedValue(
      makeCalendar([
        {
          id: 'comp-1',
          name: 'Morning Individual',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            windows: { A: { start: 540, end: 675 } }, // 11:15
          },
        },
      ])
    );
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-1', name: 'Morning Individual', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
      ],
    });
    const { container } = await mountForm({ tournament, password: 'test-password' });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('11:15');
    });
    expect(window.API.fetchCalendar).toHaveBeenCalledWith(
      'test-password',
      expect.any(AbortSignal)
    );
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(b) ignores same-day competition on court C (not chosen) and keeps 09:00', async () => {
    const calendarFn = vi.fn().mockResolvedValue(
      makeCalendar([
        {
          id: 'comp-c',
          name: 'Court C Only',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['C'],
            windows: { C: { start: 540, end: 675 } },
          },
        },
      ])
    );
    window.API.fetchCalendar = calendarFn;
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-c', name: 'Court C Only', date: '10-08-2026', startTime: '09:00', courts: ['C'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    expect(container.querySelector('#create-start-time').value).toBe('09:00');
    expect(calendarFn).toHaveBeenCalled();
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(c) picks the latest end across multiple sharing competitions (12:00 wins)', async () => {
    window.API.fetchCalendar = vi.fn().mockResolvedValue(
      makeCalendar([
        {
          id: 'comp-a',
          name: 'Competition A',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            windows: { A: { start: 540, end: 630 } }, // 10:30
          },
        },
        {
          id: 'comp-b',
          name: 'Competition B',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['B'],
            windows: { B: { start: 540, end: 720 } }, // 12:00
          },
        },
      ])
    );
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-a', name: 'Competition A', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
        { id: 'comp-b', name: 'Competition B', date: '10-08-2026', startTime: '09:00', courts: ['B'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('12:00');
    });
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(c2) an end past midnight ("24:05") is no start time: it counts as no end', async () => {
    window.API.fetchCalendar = vi.fn().mockResolvedValue(
      makeCalendar([
        {
          id: 'comp-a',
          name: 'Competition A',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            windows: { A: { start: 540, end: 630 } }, // 10:30
          },
        },
        {
          id: 'comp-b',
          name: 'Competition B',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['B'],
            windows: { B: { start: 1300, end: 24 * 60 + 5 } }, // 24:05 (1445)
          },
        },
      ])
    );
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-a', name: 'Competition A', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
        { id: 'comp-b', name: 'Competition B', date: '10-08-2026', startTime: '09:00', courts: ['B'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('10:30');
    });
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(d) stacks after an undrawn sharing competition using its placement window end (09:30)', async () => {
    window.API.fetchCalendar = vi.fn().mockResolvedValue(
      makeCalendar([
        {
          id: 'comp-undrawn',
          name: 'Undrawn Competition',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            windows: { A: { start: 540, end: 570 } }, // 09:30
          },
        },
      ])
    );
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-undrawn', name: 'Undrawn Competition', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('09:30');
    });
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(e) deselecting court B updates the start time from 12:00 to 10:30', async () => {
    window.API.fetchCalendar = vi.fn().mockResolvedValue(
      makeCalendar([
        {
          id: 'comp-a',
          name: 'Competition A',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            windows: { A: { start: 540, end: 630 } }, // 10:30
          },
        },
        {
          id: 'comp-b',
          name: 'Competition B',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['B'],
            windows: { B: { start: 540, end: 720 } }, // 12:00
          },
        },
      ])
    );
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-a', name: 'Competition A', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
        { id: 'comp-b', name: 'Competition B', date: '10-08-2026', startTime: '09:00', courts: ['B'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('12:00');
    });

    await clickPill(container, 'Shiaijo (court) B');
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('10:30');
    });
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(f) typing a start time latches and is not overwritten by subsequent court toggles', async () => {
    window.API.fetchCalendar = vi.fn().mockResolvedValue(
      makeCalendar([
        {
          id: 'comp-a',
          name: 'Competition A',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            windows: { A: { start: 540, end: 630 } },
          },
        },
        {
          id: 'comp-b',
          name: 'Competition B',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['B'],
            windows: { B: { start: 540, end: 720 } },
          },
        },
      ])
    );
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-a', name: 'Competition A', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
        { id: 'comp-b', name: 'Competition B', date: '10-08-2026', startTime: '09:00', courts: ['B'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('12:00');
    });

    const timeInput = container.querySelector('#create-start-time');
    await act(async () => {
      fireEvent.change(timeInput, { target: { value: '14:00' } });
    });
    expect(timeInput.value).toBe('14:00');

    await clickPill(container, 'Shiaijo (court) B');
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    expect(timeInput.value).toBe('14:00');
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(g) displays updated court hint text without stale phrase', async () => {
    const { container } = await mountForm();
    const hint = Array.from(container.querySelectorAll('.field__hint'))
      .find((el) => el.textContent.includes('Concurrency for this competition'));
    expect(hint).toBeDefined();
    expect(hint.textContent).toContain('Competitions on the same shiaijo (court) run one after another');
    expect(hint.textContent).toContain('when you draw, this one starts after the other competitions');
    expect(hint.textContent).toContain('The start time above is filled in that way until you change it.');
    expect(container.textContent).not.toContain('the schedule prevents conflicts');
  });

  it('(h) switching from a shared court back to a free court returns the start time to 09:00, while a hand-edited time stays', async () => {
    window.API.fetchCalendar = vi.fn().mockResolvedValue(
      makeCalendar([
        {
          id: 'comp-a',
          name: 'Competition A',
          date: '10-08-2026',
          placement: {
            placed: true,
            day: 1,
            courts: ['A'],
            windows: { A: { start: 540, end: 630 } },
          },
        },
      ])
    );
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-a', name: 'Competition A', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('10:30');
    });

    // Deselect court A so only court B (unshared) remains selected
    await clickPill(container, 'Shiaijo (court) A');
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('09:00');
    });

    // Hand-type a time
    const timeInput = container.querySelector('#create-start-time');
    await act(async () => {
      fireEvent.change(timeInput, { target: { value: '14:00' } });
    });
    expect(timeInput.value).toBe('14:00');

    // Re-select court A (now shared again) - hand-edited time must stay 14:00
    await clickPill(container, 'Shiaijo (court) A');
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50));
    });
    expect(timeInput.value).toBe('14:00');
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(i) error path: keeps 09:00 default when fetchCalendar rejects', async () => {
    window.API.fetchCalendar = vi.fn().mockRejectedValue(new Error('Network error'));
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-1', name: 'Morning Individual', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('09:00');
    });
    expect(window.API.fetchCalendar).toHaveBeenCalledWith(
      expect.any(String),
      expect.any(AbortSignal)
    );
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(j) aborts in-flight fetchCalendar on date change and skips fetch on blank date', async () => {
    const signals = [];
    window.API.fetchCalendar = vi.fn((_pw, signal) => {
      signals.push(signal);
      return new Promise(() => {}); // never settles
    });
    const tournament = makeTournament({
      date: '10-08-2026',
      durationDays: 2,
      courts: ['A', 'B'],
      competitions: [],
    });
    const { container } = await mountForm({ tournament });
    const firstSignal = signals[0];
    expect(firstSignal).toBeDefined();
    expect(firstSignal.aborted).toBe(false);

    // Switch day from Day 1 to Day 2
    const daySelect = container.querySelector('#create-day');
    await act(async () => {
      fireEvent.change(daySelect, { target: { value: '11-08-2026' } });
    });
    expect(firstSignal.aborted).toBe(true);
    expect(signals.length).toBeGreaterThan(1);

    // Blank date skips fetch entirely
    const blankTournament = makeTournament({
      date: '',
      durationDays: 1,
      courts: ['A'],
    });
    const fetchCalendarMock = vi.fn().mockResolvedValue(makeCalendar());
    window.API.fetchCalendar = fetchCalendarMock;
    await mountForm({ tournament: blankTournament });
    expect(fetchCalendarMock).not.toHaveBeenCalled();
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });

  it('(k) clears stackPending flag once fetchCalendar resolves', async () => {
    let resolveFetch;
    window.API.fetchCalendar = vi.fn().mockImplementation(() => new Promise((resolve) => {
      resolveFetch = resolve;
    }));
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [],
    });
    const { container } = await mountForm({ tournament });
    expect(window.API.fetchCalendar).toHaveBeenCalled();

    await act(async () => {
      resolveFetch(
        makeCalendar([
          {
            id: 'c1',
            date: '10-08-2026',
            placement: {
              placed: true,
              day: 1,
              courts: ['A'],
              windows: { A: { start: 540, end: 660 } },
            },
          },
        ])
      );
    });

    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('11:00');
    });
    expect(window.API.estimateCompetitionSchedule).not.toHaveBeenCalled();
  });
});
