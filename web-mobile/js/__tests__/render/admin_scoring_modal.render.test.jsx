import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { downstreamKnockoutRunningMessage } from '../../write_result.jsx';

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

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

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

// A TINTED surface carries no SHIRO/AKA badge (operator ruling 2026-09-20,
// bc-sccl, superseding bc-dnst's "named once in text, by the header badge").
// Both editors' halves are tinted -- --red-soft for Aka, --white-side plus the
// 45° hatch for Shiro -- so the pill was a second statement of the same fact.
// This file previously asserted the badges were PRESENT; it now pins their
// removal, and pins the sr-only label that has to replace them, because dropping
// the badge without it would leave colour as the only signal (DESIGN.md §4).
describe('ScoreEditorModal side labelling', () => {
  for (const [label, make] of [['individual', makeIndividualMatch], ['team', makeTeamMatch]]) {
    it(`renders no SHIRO/AKA pill on the ${label} editor, and names the side for a screen reader`, () => {
      const { container } = renderModal(make());
      expect(container.querySelector('.sb-side__badge')).toBeNull();
      const srText = [...container.querySelectorAll('.sb-side .sr-only')].map(e => e.textContent.trim());
      expect(srText).toContain('Shiro:');
      expect(srText).toContain('Aka:');
    });
  }
});

// bc-rfsw: a withdrawal re-recorded on a knockout match whose later match is
// being fought now is refused (409 downstream_knockout_running, operator
// decision 2026-09-27). The editor shows the operator's sentence on the
// decision error line, asks nothing first (no "Proceed anyway?"), and does not move the court on.
describe('ScoreEditorModal: a decision refused because a later match is being fought', () => {
  for (const [label, make] of [
    ['individual', () => makeIndividualMatch({ id: 'm-r1-0', compId: 'c1', status: 'running', phase: 'knockout', ipponsA: [], ipponsB: [] })],
    ['team', () => makeTeamMatch({ id: 'm-r1-0', compId: 'c1', status: 'running', phase: 'knockout', teamSize: 3 })],
  ]) it(`${label}: shows the sentence on the decision error line, with no confirm and no advance`, async () => {
    const sentence = downstreamKnockoutRunningMessage([{ id: 'm-r2-0', number: 3, label: 'Match 3 (Final)', court: 'A' }]);
    expect(sentence).toBe('Match 3 (Final) is being fought now on Shiaijo A. Finish it or send it back to the queue, then save this correction again.');
    const refusal = Object.assign(new Error(sentence), {
      code: 'downstream_knockout_running',
      downstreamKnockoutRunning: { matchId: 'm-r1-0', runningMatches: [{ id: 'm-r2-0', number: 3, label: 'Match 3 (Final)', court: 'A' }] },
    });
    const prevKiken = window.isKikenDecision;
    window.isKikenDecision = (d) => d === 'kiken' || d === 'kiken-voluntary' || d === 'kiken-injury';
    window.API.recordDecision = vi.fn().mockRejectedValue(refusal);
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const onAfterDecision = vi.fn().mockResolvedValue(undefined);
    try {
      await act(async () => {
        render(<ScoreEditorModal
          match={make()}
          onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} onAfterDecision={onAfterDecision} password="secret" />);
      });
      await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Kiken . Voluntary$/ })); });
      const form = document.querySelector('form.decision-prompt');
      fireEvent.click(form.querySelector('input[name="decision-side"][value="shiro"]'));
      await act(async () => { fireEvent.submit(form); });

      expect(screen.getByText(sentence)).toBeTruthy();
      expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
      expect(window.confirmDialog).not.toHaveBeenCalled();
      expect(onAfterDecision).not.toHaveBeenCalled();
    } finally {
      window.isKikenDecision = prevKiken;
    }
  });
});
