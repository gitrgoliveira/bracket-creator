import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// Window globals required by admin_scoring_modal.jsx (and its transitive
// import admin_scoring_shared.jsx). Divide into:
//  SYNC: called synchronously in the component body on every render
//  LAZY: only called inside event handlers or async effects
//
// All are set before the dynamic import so module-level capture lines like
//   const TEAM_POSITIONS = Array.from({length: window.MAX_TEAM_SIZE}, ...)
// resolve to real values rather than undefined.
const STUBBED_GLOBALS = {
  // SYNC: called in the component body on every render
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  // LAZY: only reached from event handlers / async effects
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
  // Glossary components: used by admin_scoring_shared.jsx
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
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

function renderModal(match) {
  return render(
    <ScoreEditorModal
      match={match}
      onClose={vi.fn()}
      onSubmit={vi.fn()}
      password=""
    />
  );
}

describe('ScoreEditorModal render-smoke', () => {
  it('renders individual scheduled match without throwing', () => {
    expect(() => renderModal(makeIndividualMatch())).not.toThrow();
  });

  it('renders individual running match without throwing', () => {
    expect(() => renderModal(makeIndividualMatch({ status: 'running' }))).not.toThrow();
  });

  it('renders individual completed match (correction mode) without throwing', () => {
    expect(() =>
      renderModal(makeIndividualMatch({
        status: 'completed',
        ipponsA: ['M'],
        ipponsB: [],
        winner: { id: 'p1', name: 'Yamada' },
      }))
    ).not.toThrow();
  });

  it('renders individual pool match without throwing', () => {
    expect(() => renderModal(makeIndividualMatch({ phase: 'pool', poolName: 'Pool 2' }))).not.toThrow();
  });

  it('renders individual knockout match without throwing', () => {
    expect(() => renderModal(makeIndividualMatch({ phase: 'knockout', round: 'Semi-final' }))).not.toThrow();
  });

  it('renders team scheduled match (routes to TeamScoreEditorModal) without throwing', () => {
    expect(() => renderModal(makeTeamMatch())).not.toThrow();
  });

  it('renders team running match without throwing', () => {
    expect(() => renderModal(makeTeamMatch({ status: 'running' }))).not.toThrow();
  });

  it('renders team completed match without throwing', () => {
    expect(() => renderModal(makeTeamMatch({ status: 'completed' }))).not.toThrow();
  });
});

// impeccable re-critique symmetry: the individual (kendo) and team editors now
// carry the same explicit SHIRO/AKA pill badge the Engi editor has, so the side
// is labelled identically across all three editors.
describe('ScoreEditorModal SHIRO/AKA side badges', () => {
  it('renders a framed Shiro badge and a solid Aka badge on the individual editor', () => {
    const { container } = renderModal(makeIndividualMatch());
    const shiro = container.querySelector('.sb-side--shiro .sb-side__badge--shiro');
    const aka = container.querySelector('.sb-side--aka .sb-side__badge--aka');
    expect(shiro).not.toBeNull();
    expect(aka).not.toBeNull();
    expect(shiro.textContent).toBe('Shiro');
    expect(aka.textContent).toBe('Aka');
  });

  it('renders the same badges on the team editor', () => {
    const { container } = renderModal(makeTeamMatch());
    expect(container.querySelector('.sb-side--shiro .sb-side__badge--shiro')?.textContent).toBe('Shiro');
    expect(container.querySelector('.sb-side--aka .sb-side__badge--aka')?.textContent).toBe('Aka');
  });
});
