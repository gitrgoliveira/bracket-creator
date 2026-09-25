// bc-tmfn: the admin Scores list places the match-level Kiken/Fus. mark a
// default-win decision gives a TEAM match beside the WITHDRAWN team's name
// (window.teamMatchMarks, bracket.jsx), never inside the shared score cell
// (teamIVPWScore is deliberately free of marks) and never doubled onto an
// individual match, whose own mark already rides inline in its score
// string.
//
// This mounts the REAL bracket.jsx (transitively, via
// admin_schedule_lineup.jsx -> admin_scoring_shared.jsx -> bracket.jsx,
// which sets window.teamMatchMarks/sideMarks/placeMarks during the dynamic
// import below) rather than stubbing teamMatchMarks: a stub set before that
// import is silently overwritten once bracket.jsx's own module-eval-time
// window assignments run, so this exercises the real sideMarks + placeMarks
// + sameCompetitor composition, not a fake.
import React from 'react';
import { render, act, screen, cleanup, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const side = (name, dojo, number) => ({ name, dojo, number, id: `${number}-id` });

// Shiro (sideB) withdrew: decisionBy="shiro" -> Aka (sideA, Tora A) is
// credited/wins, matching engine.RecordDecisionTx's decisionBy->Winner
// mapping. winner is the SAME object reference as sideA so sameCompetitor
// resolves it by id without ambiguity.
const toraA = side('Tora A', 'Nara', 'T1');
const toraB = side('Tora B', 'Tokyo', 'T2');
const TEAM_MATCH = {
  id: 'team-m', compId: 'c1', status: 'completed', court: 'A', scheduledAt: '09:00',
  sideA: toraA, sideB: toraB, winner: toraA,
  decision: 'kiken-voluntary', decisionBy: 'shiro',
  subResults: [{ position: 1, winner: 'Tora A' }, { position: 2 }],
};
const INDIVIDUAL_MATCH = {
  id: 'indiv-m', compId: 'c1', status: 'completed', court: 'A', scheduledAt: '09:15',
  sideA: side('Ken Saito', 'Nara', 'K1'), sideB: side('Aiko Sato', 'Tokyo', 'K2'),
  winner: 'Ken Saito', decision: 'kiken-voluntary', decisionBy: 'shiro',
};
const MATCHES = [TEAM_MATCH, INDIVIDUAL_MATCH];

const STUBS = {
  ScoreEditorModal: () => null,
  AdminTopbar: ({ children }) => <div>{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: (m) => (m.id === 'team-m' ? 'IV 0–2\nPW 0–4' : 'M vs Kiken'),
  boutMiddle: () => 'vs',
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  compMatches: () => MATCHES,
  startPatch: () => ({ status: 'running', winner: null }),
  confirmDialog: vi.fn().mockResolvedValue(true),
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null) },
};

let restore, AdminScoreEditor;

beforeAll(async () => {
  window.scrollTo = vi.fn();
  restore = installWindowStubs(STUBS);
  const mod = await import('../../admin_schedule_score_editor.jsx');
  AdminScoreEditor = mod.AdminScoreEditor;
});
afterAll(() => restore());
beforeEach(() => cleanup());

async function mount() {
  await act(async () => {
    render(
      <AdminScoreEditor
        t={{ competitions: [{ id: 'c1', name: 'Team Open' }] }}
        onEditScore={vi.fn()}
        onMoveCourt={null}
        password="pw"
        showToast={vi.fn()}
      />
    );
  });
}

describe('the Scores list places the default-win mark beside the withdrawn TEAM name', () => {
  it('shows "Kiken" on the team row, beside Tora B (Shiro, the side decisionBy names)', async () => {
    await mount();
    const teamRow = screen.getByText('Tora B').closest('.score-edit-row');
    expect(teamRow).toBeTruthy();
    const mark = within(teamRow).getByTestId('team-summary-mark-shiro');
    expect(mark.textContent).toBe('Kiken');
    // Tora A (Aka) is the credited winner: no mark on that side.
    expect(within(teamRow).queryByTestId('team-summary-mark-aka')).toBeNull();
  });

  it('does NOT double the mark on an individual row (it already rides inline in the score string)', async () => {
    await mount();
    const indivRow = screen.getByText('Ken Saito').closest('.score-edit-row');
    expect(indivRow).toBeTruthy();
    expect(within(indivRow).queryByTestId('team-summary-mark-shiro')).toBeNull();
    expect(within(indivRow).queryByTestId('team-summary-mark-aka')).toBeNull();
  });

  it('the score cell itself stays free of the mark (teamIVPWScore has none to carry)', async () => {
    await mount();
    const teamRow = screen.getByText('Tora B').closest('.score-edit-row');
    const scoreCell = teamRow.querySelector('.score-edit-row__scoreval');
    expect(scoreCell.textContent).not.toContain('Kiken');
  });
});
