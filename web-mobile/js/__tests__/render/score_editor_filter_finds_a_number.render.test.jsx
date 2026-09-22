// bc-nsrc: the admin Scores page filter searches competitor NUMBERS
// (operator ruling 2026-09-22).
//
// This file exists because the ownership gate cannot protect this call site.
// check-competitor-search.mjs flags a `.number` read next to `.includes(` or
// `.startsWith(`; the shape this surface used to carry --
//
//     [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo]
//       .some((s) => (s || "").toLowerCase().includes(f))
//
// -- names no number at all, so re-inlining it would pass every check while
// silently dropping the rule. A behaviour test is the only thing that notices.
//
// Why the ruling went this way: every row on this page renders the competitor
// number as a chip, so before the change an operator could READ the column and
// not search it, which is the contradiction bc-nsrc was filed for.
import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const side = (name, dojo, number) => ({ name, dojo, number, id: `${number}-id` });

// Two scheduled matches, four competitors, numbers that make the ruling
// testable: K1 must not be dragged in by a search for K12.
const MATCHES = [
  {
    id: 'm1', compId: 'c1', status: 'scheduled', court: 'A', scheduledAt: '09:00',
    sideA: side('Ken Saito', 'Nara', 'K1'), sideB: side('Aiko Sato', 'Tokyo', 'K2'),
  },
  {
    id: 'm2', compId: 'c1', status: 'scheduled', court: 'A', scheduledAt: '09:15',
    sideA: side('Kenji Mori', 'Osaka', 'K12'), sideB: side('Sora Kimura', 'Kyoto', 'K11'),
  },
];

const STUBS = {
  ScoreEditorModal: () => null,
  AdminTopbar: ({ children }) => <div>{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: () => '',
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
// Each case mounts its own editor, so the previous one must go or the
// placeholder query finds two boxes.
beforeEach(() => cleanup());

// The visible row count: one open-button per listed match.
const rows = () => document.querySelectorAll('button.test-score-open').length;

async function mountAndType(query) {
  await act(async () => {
    render(
      <AdminScoreEditor
        t={{ competitions: [{ id: 'c1', name: 'Kumite Open' }] }}
        onEditScore={vi.fn()}
        onMoveCourt={null}
        password="pw"
        showToast={vi.fn()}
      />
    );
  });
  const box = screen.getByPlaceholderText(/Search player, team, dojo/);
  await act(async () => { fireEvent.input(box, { target: { value: query } }); });
  return box;
}

describe('the Scores filter finds a competitor by their number', () => {
  it('lists both matches when the filter is empty', async () => {
    await mountAndType('');
    expect(rows()).toBe(2);
  });

  it('a number WITH its prefix finds that one match', async () => {
    await mountAndType('K12');
    expect(rows(), 'only Kenji Mori\'s match').toBe(1);
    expect(screen.getByText('Kenji Mori')).toBeTruthy();
  });

  it('K12 does NOT drag in K1', async () => {
    // The operator ruling's sharp edge: once a digit is typed the match is
    // exact, so a shorter sibling number must not appear.
    await mountAndType('K12');
    expect(screen.queryByText('Ken Saito'), 'K1 is a different competitor').toBeNull();
  });

  it('a BARE number finds nothing: the prefix is required', async () => {
    await mountAndType('12');
    expect(rows()).toBe(0);
  });

  it('names still match on any fragment', async () => {
    await mountAndType('Kenji');
    expect(rows()).toBe(1);
  });

  it('dojos still match on any fragment', async () => {
    await mountAndType('Kyoto');
    expect(rows()).toBe(1);
  });
});
