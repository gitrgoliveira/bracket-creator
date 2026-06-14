import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// Window globals required by admin_scoring_modal.jsx (and its transitive
// import admin_scoring_shared.jsx). Divide into:
//  SYNC  — called synchronously in the component body on every render
//  LAZY  — only called inside event handlers or async effects
//
// All are set before the dynamic import so module-level capture lines like
//   const TEAM_POSITIONS = Array.from({length: window.MAX_TEAM_SIZE}, ...)
// resolve to real values rather than undefined.
const STUBBED_GLOBALS = {
  // SYNC — called in the component body on every render
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  // LAZY — only reached from event handlers / async effects
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  // Glossary components — used by admin_scoring_shared.jsx
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const originals = {};

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_scoring_modal.jsx');
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

function makeIndividualMatch(overrides = {}) {
  return {
    id: 'm1',
    status: 'scheduled',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    // No compId → fetchCompetitionDetails useEffect returns early (guard: if (!m.compId) return)
    ...overrides,
  };
}

function makeTeamMatch(overrides = {}) {
  return {
    id: 'm2',
    status: 'scheduled',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 5,
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    ...overrides,
  };
}

describe('ScoreEditorModal render-smoke', () => {
  it('renders individual scheduled match without throwing', () => {
    const ScoreEditorModal = window.ScoreEditorModal;
    expect(() =>
      render(
        <ScoreEditorModal
          match={makeIndividualMatch()}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password=""
        />
      )
    ).not.toThrow();
  });

  it('renders individual running match without throwing', () => {
    const ScoreEditorModal = window.ScoreEditorModal;
    expect(() =>
      render(
        <ScoreEditorModal
          match={makeIndividualMatch({ status: 'running' })}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password=""
        />
      )
    ).not.toThrow();
  });

  it('renders individual completed match (correction mode) without throwing', () => {
    const ScoreEditorModal = window.ScoreEditorModal;
    expect(() =>
      render(
        <ScoreEditorModal
          match={makeIndividualMatch({
            status: 'completed',
            ipponsA: ['M'],
            ipponsB: [],
            winner: { id: 'p1', name: 'Yamada' },
          })}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password=""
        />
      )
    ).not.toThrow();
  });

  it('renders individual pool match without throwing', () => {
    const ScoreEditorModal = window.ScoreEditorModal;
    expect(() =>
      render(
        <ScoreEditorModal
          match={makeIndividualMatch({ phase: 'pool', poolName: 'Pool 2' })}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password=""
        />
      )
    ).not.toThrow();
  });

  it('renders individual knockout match without throwing', () => {
    const ScoreEditorModal = window.ScoreEditorModal;
    expect(() =>
      render(
        <ScoreEditorModal
          match={makeIndividualMatch({ phase: 'knockout', round: 'Semi-final' })}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password=""
        />
      )
    ).not.toThrow();
  });

  it('renders team scheduled match (routes to TeamScoreEditorModal) without throwing', () => {
    const ScoreEditorModal = window.ScoreEditorModal;
    expect(() =>
      render(
        <ScoreEditorModal
          match={makeTeamMatch()}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password=""
        />
      )
    ).not.toThrow();
  });

  it('renders team running match without throwing', () => {
    const ScoreEditorModal = window.ScoreEditorModal;
    expect(() =>
      render(
        <ScoreEditorModal
          match={makeTeamMatch({ status: 'running' })}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password=""
        />
      )
    ).not.toThrow();
  });

  it('renders team completed match without throwing', () => {
    const ScoreEditorModal = window.ScoreEditorModal;
    expect(() =>
      render(
        <ScoreEditorModal
          match={makeTeamMatch({ status: 'completed' })}
          onClose={vi.fn()}
          onSubmit={vi.fn()}
          password=""
        />
      )
    ).not.toThrow();
  });
});
