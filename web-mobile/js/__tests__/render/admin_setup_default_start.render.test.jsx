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

async function mountForm({ tournament = makeTournament(), onCreate = vi.fn() } = {}) {
  let result;
  await act(async () => {
    result = render(
      <AdminCreateCompetition
        tournament={tournament}
        onCancel={noop}
        onCreate={onCreate}
        onLogout={noop}
        onViewerMode={noop}
        password=""
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
  it('(a) stacks after same-day competition on court A using courtEnds', async () => {
    window.API.estimateCompetitionSchedule = vi.fn().mockResolvedValue({
      totalDurationMinutes: 120,
      courtEnds: { A: '11:15' },
    });
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-1', name: 'Morning Individual', date: '10-08-2026', startTime: '09:00', courts: ['A'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    await waitFor(() => {
      expect(container.querySelector('#create-start-time').value).toBe('11:15');
    });
    expect(window.API.estimateCompetitionSchedule).toHaveBeenCalledWith(
      'comp-1',
      expect.any(String),
      expect.any(AbortSignal)
    );
  });

  it('(b) ignores same-day competition on court C (not chosen) and keeps 09:00', async () => {
    const estimateFn = vi.fn().mockResolvedValue({
      totalDurationMinutes: 120,
      courtEnds: { C: '11:15' },
    });
    window.API.estimateCompetitionSchedule = estimateFn;
    const tournament = makeTournament({
      courts: ['A', 'B', 'C'],
      competitions: [
        { id: 'comp-c', name: 'Court C Only', date: '10-08-2026', startTime: '09:00', courts: ['C'] },
      ],
    });
    const { container } = await mountForm({ tournament });
    expect(container.querySelector('#create-start-time').value).toBe('09:00');
    expect(estimateFn).not.toHaveBeenCalled();
  });

  it('(c) picks the latest end across multiple sharing competitions (12:00 wins)', async () => {
    window.API.estimateCompetitionSchedule = vi.fn((id) => {
      if (id === 'comp-a') return Promise.resolve({ courtEnds: { A: '10:30' } });
      if (id === 'comp-b') return Promise.resolve({ courtEnds: { B: '12:00' } });
      return Promise.resolve(null);
    });
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
  });

  it('(d) uses 30-minute floor for an undrawn sharing competition without courtEnds', async () => {
    window.API.estimateCompetitionSchedule = vi.fn().mockResolvedValue({
      totalDurationMinutes: 0,
    });
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
  });

  it('(e) deselecting court B updates the start time from 12:00 to 10:30', async () => {
    window.API.estimateCompetitionSchedule = vi.fn((id) => {
      if (id === 'comp-a') return Promise.resolve({ courtEnds: { A: '10:30' } });
      if (id === 'comp-b') return Promise.resolve({ courtEnds: { B: '12:00' } });
      return Promise.resolve(null);
    });
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
  });

  it('(f) typing a start time latches and is not overwritten by subsequent court toggles', async () => {
    window.API.estimateCompetitionSchedule = vi.fn((id) => {
      if (id === 'comp-a') return Promise.resolve({ courtEnds: { A: '10:30' } });
      if (id === 'comp-b') return Promise.resolve({ courtEnds: { B: '12:00' } });
      return Promise.resolve(null);
    });
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
});
