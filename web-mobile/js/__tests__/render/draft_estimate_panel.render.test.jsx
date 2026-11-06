import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { DraftEstimatePanel } from '../../draft_estimate.jsx';

const STUBBED_GLOBALS = {};

let restoreGlobals;

beforeAll(() => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
});

afterAll(() => {
  if (restoreGlobals) {
    restoreGlobals();
  }
});

function makeSampleEstimate(overrides = {}) {
  return {
    totalDurationMinutes: 248,
    bestCaseMinutes: 248,
    worstCaseMinutes: 248,
    perCourtMinutes: [248, 242],
    poolMatches: 12,
    knockoutMatches: 7,
    participants: 16,
    participantsSource: 'expected',
    timing: {
      multiplier: 1.5,
      allowancePct: 10,
      breaks: [
        { name: 'Opening ceremony', start: '09:30', end: '10:00' },
        { name: 'Lunch', start: '12:30', end: '13:30' },
      ],
    },
    warnings: [],
    ...overrides,
  };
}

describe('DraftEstimatePanel render tests', () => {
  it('says the competition can still be created when no estimate came back, and offers Try again', () => {
    const onRetry = vi.fn();
    const { getByTestId, getByRole } = render(
      <DraftEstimatePanel estimate={null} loading={false} error="Could not work out an estimate right now." onRetry={onRetry} shape={{ startTime: '09:30' }} />
    );
    const alert = getByTestId('draft-estimate-error');
    expect(alert.textContent).toContain('Could not work out an estimate right now. You can still create the competition.');
    expect(alert.textContent).not.toMatch(/Reload/);
    fireEvent.click(getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('renders headline duration for a sample estimate', () => {
    const estimate = makeSampleEstimate();
    const { getByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:30' }}
      />
    );

    const duration = getByTestId('draft-estimate-duration');
    expect(duration.textContent).toBe('Est. duration: about 4h 08m');

    const finish = getByTestId('draft-estimate-finish');
    expect(finish.textContent).toBe('Finishes about 13:38');
  });

  it('renders per-shiaijo line using chosen letters B, C', () => {
    const estimate = makeSampleEstimate({ perCourtMinutes: [248, 242] });
    const { getByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:30', courtLabels: ['B', 'C'] }}
      />
    );

    const perShiaijo = getByTestId('draft-estimate-per-shiaijo');
    expect(perShiaijo.textContent).toBe('Per shiaijo: B about 4h 08m · C about 4h 02m');
  });

  it('renders the pauses line when pauses exist', () => {
    const estimate = makeSampleEstimate();
    const { getByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:30' }}
      />
    );

    const timing = getByTestId('draft-estimate-timing');
    expect(timing.textContent).toContain(
      'Pauses that day: Opening ceremony 09:30 to 10:00, Lunch 12:30 to 13:30.'
    );
  });

  it('renders the no-pauses line when day has no pauses', () => {
    const estimate = makeSampleEstimate({
      timing: { multiplier: 1.5, allowancePct: 10, breaks: [] },
    });
    const { getByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:30' }}
      />
    );

    const timing = getByTestId('draft-estimate-timing');
    expect(timing.textContent).toContain('No pauses that day.');
  });

  it('renders past-midnight warn sentence when finish is after midnight', () => {
    const estimate = makeSampleEstimate({ totalDurationMinutes: 100 });
    const { getByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '23:00' }}
      />
    );

    const finish = getByTestId('draft-estimate-finish');
    expect(finish.textContent).toBe(
      'Finishes about 00:40, after midnight. Start it earlier or give it more shiaijo.'
    );
    expect(finish.className).toContain('draft-estimate__finish--warn');
  });

  it('renders too_many_shiaijo sentence and calls onChangeShiaijo prop on button click', async () => {
    const onChangeShiaijo = vi.fn();
    const estimate = makeSampleEstimate({
      warnings: [{ code: 'too_many_shiaijo', participants: 3, usable: 1, chosen: 2 }],
    });
    const { getByTestId, getByRole } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:30', participants: 3 }}
        onChangeShiaijo={onChangeShiaijo}
      />
    );

    const warning = getByTestId('draft-estimate-warning');
    expect(warning.textContent).toContain(
      'With 3 participants only 1 match can run at a time, so a second shiaijo would stand empty. Use 1 shiaijo.'
    );

    const btn = getByRole('button', { name: 'Change shiaijo' });
    await act(async () => {
      fireEvent.click(btn);
    });
    expect(onChangeShiaijo).toHaveBeenCalledTimes(1);
  });

  it('renders idle shiaijo wording when a court minute is 0', () => {
    const estimate = makeSampleEstimate({ perCourtMinutes: [55, 0] });
    const { getByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:30', courtLabels: ['A', 'B'] }}
      />
    );

    const perShiaijo = getByTestId('draft-estimate-per-shiaijo');
    expect(perShiaijo.textContent).toBe('Per shiaijo: A about 55m · B has no matches');
  });

  it('renders no timing line when estimate has no timing', () => {
    const estimate = makeSampleEstimate({ timing: null });
    const { queryByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:30' }}
      />
    );
    expect(queryByTestId('draft-estimate-timing')).toBeNull();
  });

  it('renders sent multiplier and allowance when timing is present', () => {
    const estimate = makeSampleEstimate({
      timing: { multiplier: 2.2, allowancePct: 15, breaks: [] },
    });
    const { getByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:30' }}
      />
    );
    const timing = getByTestId('draft-estimate-timing');
    expect(timing.textContent).toContain(
      "Uses this tournament's timing: each match counts as 2.2 times its clock time, plus 15% for the slowest shiaijo. No pauses that day."
    );
  });

  it('renders kachinuki range line without (average)', () => {
    const estimate = makeSampleEstimate({
      totalDurationMinutes: 120,
      bestCaseMinutes: 90,
      worstCaseMinutes: 150,
    });
    const { getByTestId } = render(
      <DraftEstimatePanel
        estimate={estimate}
        loading={false}
        error={null}
        shape={{ startTime: '09:00', teamMatchType: 'kachinuki' }}
      />
    );
    const kachinuki = getByTestId('draft-estimate-kachinuki');
    expect(kachinuki.textContent).toBe('Could end 10:30 to 11:30');
    expect(kachinuki.textContent).not.toContain('(average)');
  });
});
