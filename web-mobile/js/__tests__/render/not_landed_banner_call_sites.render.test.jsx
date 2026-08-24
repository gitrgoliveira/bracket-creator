// bc-cse: notLandedBanner is asked at THREE explicit-tap call sites, and only
// one of them was pinned.
//
// team_editor_superseded.render.test.jsx covers the team editor's [Start
// match]. The other two — the INDIVIDUAL editor's [Start match]
// (admin_scoring_individual.jsx) and the team editor's kachinuki [Record bout]
// (admin_scoring_team.jsx) — could both be neutered to `const banner = null;`
// with the entire JS suite still green. That is the same class of gap the
// owner module exists to close: a rule stated at three sites, verified at one.
//
// Each case here asserts BOTH directions, because the whole reason the banner
// has an owner is that the two verdicts are both `applied:false` and their
// remedies are OPPOSITE. A clock refusal stored nothing and re-entering IS the
// fix; a superseded write lost to a newer stored result and re-entering would
// overwrite it. Showing the wrong one is worse than showing neither.

import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { SUPERSEDED_REASON, SUPERSEDED_ADVICE, CLOCK_SKEW_REASON_TEXT, CLOCK_SKEW_ADVICE } from '../../write_result.jsx';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
  // No write_result stubs: both editors import notLandedBanner from the leaf,
  // so the verdict AND the copy come from the real module, and the assertions
  // compare against the real exported strings.
};

const originals = {};
let ScoreEditorModal;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

beforeEach(() => {
  window.compMatches = () => [];
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    reopenMatch: vi.fn(),
    removeKachinukiBout: vi.fn(),
  };
});

const CLOCK_REFUSAL = { applied: false, reason: 'clock_skew', serverNowMs: 1 };
const SUPERSEDED = { applied: false, reason: 'superseded' };

async function renderEditor(match, onSubmit) {
  await act(async () => {
    render(
      <ScoreEditorModal
        match={match}
        onClose={vi.fn()}
        onSubmit={onSubmit}
        password="secret"
      />
    );
  });
}

// ---------------------------------------------------------------------------
// Site 2: the INDIVIDUAL editor's [Start match].
// ---------------------------------------------------------------------------
// Same shape as the team one: a status:"running" write, the shape
// _notifyScoreSuperseded stays silent for (a debounced autosave being
// superseded is routine noise), so this explicit tap must read the awaited
// result itself or it silently re-enables the button having saved nothing.

function scheduledIndividualMatch(overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'scheduled',
    phase: 'knockout',
    round: 'Semi-final',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ipponsA: [],
    ipponsB: [],
    hansokuA: 0,
    hansokuB: 0,
    ...overrides,
  };
}

describe('individual score editor: Start match reports which refusal it got (bc-cse)', () => {
  it('shows the clock copy on a clock refusal, never the superseded advice', async () => {
    await renderEditor(scheduledIndividualMatch(), vi.fn().mockResolvedValue(CLOCK_REFUSAL));

    await act(async () => { fireEvent.click(screen.getByText('Start match')); });

    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain(CLOCK_SKEW_REASON_TEXT);
      expect(alert.textContent).toContain(CLOCK_SKEW_ADVICE);
      // The load-bearing negative: the superseded advice warns AGAINST the one
      // action that would save this operator's work.
      expect(alert.textContent).not.toContain(SUPERSEDED_ADVICE);
    });
  });

  it('shows the superseded copy on a superseded refusal', async () => {
    await renderEditor(scheduledIndividualMatch(), vi.fn().mockResolvedValue(SUPERSEDED));

    await act(async () => { fireEvent.click(screen.getByText('Start match')); });

    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain(SUPERSEDED_REASON);
      expect(alert.textContent).toContain(SUPERSEDED_ADVICE);
      expect(alert.textContent).not.toContain(CLOCK_SKEW_REASON_TEXT);
    });
  });

  it('says nothing when the write lands', async () => {
    // Without this the site could "pass" by always warning.
    await renderEditor(scheduledIndividualMatch(), vi.fn().mockResolvedValue({ id: 'm1', status: 'running' }));
    await act(async () => { fireEvent.click(screen.getByText('Start match')); });
    expect(screen.queryByRole('alert')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Site 3: the team editor's kachinuki [Record bout].
// ---------------------------------------------------------------------------
// The hot path of a kachinuki encounter — tapped once per bout — and also a
// status:"running" write. A refusal here is the worst of the three to lose:
// the operator taps on down the roster believing each bout is recorded.

function runningKachinukiMatch(overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    compFormat: 'mixed',
    teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    // Bout 1 is SCORED, which is what enables [Record bout].
    subResults: [
      { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' },
    ],
    ...overrides,
  };
}

async function tapRecordBout(res) {
  await renderEditor(runningKachinukiMatch(), vi.fn().mockResolvedValue(res));
  const btn = await screen.findByRole('button', { name: 'Record bout' });
  expect(btn).not.toBeDisabled();
  await act(async () => { fireEvent.click(btn); });
}

describe('kachinuki Record bout reports which refusal it got (bc-cse)', () => {
  it('shows the clock copy on a clock refusal, never the superseded advice', async () => {
    await tapRecordBout(CLOCK_REFUSAL);

    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain(CLOCK_SKEW_REASON_TEXT);
      expect(alert.textContent).toContain(CLOCK_SKEW_ADVICE);
      expect(alert.textContent).not.toContain(SUPERSEDED_ADVICE);
    });
  });

  it('shows the superseded copy on a superseded refusal', async () => {
    await tapRecordBout(SUPERSEDED);

    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain(SUPERSEDED_REASON);
      expect(alert.textContent).toContain(SUPERSEDED_ADVICE);
      expect(alert.textContent).not.toContain(CLOCK_SKEW_REASON_TEXT);
    });
  });

  it('says nothing when the bout write lands', async () => {
    await tapRecordBout({ id: 'm1', status: 'running', subResults: [] });
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
