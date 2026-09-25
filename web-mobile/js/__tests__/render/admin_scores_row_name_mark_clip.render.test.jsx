// bc-cse (fix #4): the admin Scores tab's own row (admin_schedule_score_editor
// .jsx, class .name) wraps a NumberedName clip element beside a Kiken/Fus.
// mark inside a block-ellipsis cell -- a long name used to clip the mark off
// the end (Shiro, trailing) or push it past the visible width (Aka, leading).
// See viewer_match.jsx's VSchedItem comment for the full mechanism; this pins
// the SAME structural fix (msb-name--labelled) landed on THIS host.
import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const side = (id, name, number) => ({ id, name, number });

// A completed team match: Shiro (the long name) withdrew (decisionBy
// "shiro"), so window.teamMatchMarks (the REAL bracket.jsx implementation --
// admin_schedule_score_editor.jsx transitively loads it via
// admin_schedule_lineup.jsx -> admin_scoring_shared.jsx, so a stub here would
// be clobbered by bracket.jsx's own `window.teamMatchMarks = teamMatchMarks`
// at import time) credits the Kiken mark to Shiro's (long) name.
const TEAM_MATCH = {
  id: 'm-team-1', compId: 'c1', status: 'completed', court: 'A', decision: 'kiken-voluntary', decisionBy: 'shiro',
  subResults: [{ position: 1 }],
  sideA: side('team-aka', 'Aoki Dojo', 'T1'),
  sideB: side('team-shiro', 'A Very Long Shiro Team Name That Should Clip', 'T2'),
  winner: { id: 'team-aka', name: 'Aoki Dojo' },
};

const STUBS = {
  ScoreEditorModal: () => <div data-testid="score-editor" />,
  AdminTopbar: ({ children }) => <div>{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: () => '',
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  compMatches: () => [TEAM_MATCH],
  startPatch: () => ({ status: 'running', winner: null }),
  confirmDialog: vi.fn().mockResolvedValue(true),
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
  },
};

let restore, AdminScoreEditor;

beforeAll(async () => {
  window.scrollTo = vi.fn();
  restore = installWindowStubs(STUBS);
  const mod = await import('../../admin_schedule_score_editor.jsx');
  AdminScoreEditor = mod.AdminScoreEditor;
});
afterAll(() => restore());

function mount() {
  return render(
    <AdminScoreEditor
      t={{ competitions: [{ id: 'c1', name: 'Cup' }] }}
      onEditScore={vi.fn()}
      onMoveCourt={null}
      password="pw"
      showToast={vi.fn()}
    />
  );
}

describe('admin Scores row: the mark survives clipping (bc-cse #4)', () => {
  it('the .name cell carries msb-name--labelled, and the mark is a real element, not part of the ellipsised text', () => {
    const { container } = mount();
    const nameCells = [...container.querySelectorAll('.name')];
    expect(nameCells.length).toBeGreaterThanOrEqual(2);
    for (const cell of nameCells) {
      expect(cell.classList.contains('msb-name--labelled')).toBe(true);
    }

    // Shiro carries the Kiken mark: a real sb-result-mark element.
    const mark = container.querySelector('.sb-result-mark');
    expect(mark).toBeTruthy();
    expect(mark.textContent).toBe('Kiken');

    // The mark is a SIBLING of the name, not appended to its text run: the
    // long Shiro name text is present verbatim, with no "Kiken" folded in.
    const shiroCell = nameCells.find((c) => c.textContent.includes('A Very Long Shiro'));
    expect(shiroCell).toBeTruthy();
    expect(shiroCell.contains(mark)).toBe(true);
    const numberedText = shiroCell.querySelector('.numbered-name__text');
    expect(numberedText).toBeTruthy();
    expect(numberedText.textContent).not.toContain('Kiken');
  });
});
