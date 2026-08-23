import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// The operator most likely to notice a lost encounter is the one who opens it
// to score it and finds the bout rows empty. This is the surface that answers
// their actual question ("did this never get scored?") and names the repair.
//
// It is placed inside the team editor's shared `inner`, which renders BOTH the
// wide overlay and the narrow shiaijo inline panel, so both variants are
// asserted here: a placement that reached only one of them has happened before.

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
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

function teamMatch(overrides = {}) {
  return {
    id: 'm2',
    status: 'scheduled',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 'team-A', name: 'Kenshikan' },
    sideB: { id: 'team-B', name: 'Sanshukan' },
    ...overrides,
  };
}

const NOTE = /could not be read from the results\s+file/;

function renderEditor(match, variant) {
  return render(
    <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="" variant={variant} />
  );
}

describe('team score editor: unreadable sub-bout cell', () => {
  it('tells the operator why the bout rows are empty, in the overlay', () => {
    renderEditor(teamMatch({ subResultsUnreadable: true }), 'modal');
    const note = screen.getByText(NOTE);
    expect(note).toBeTruthy();
    // It must promise the right thing: the text is still on disk, and saving
    // from here is the repair. An operator told only "unreadable" would
    // reasonably assume the bouts are gone.
    expect(note.textContent).toMatch(/still in the file/);
    expect(note.textContent).toMatch(/Entering the bouts here replaces it/);
  });

  it('shows it in the narrow shiaijo inline panel too', () => {
    // Same `inner`, so this cannot diverge from the overlay by construction.
    // Asserted anyway: that shared-inner property is what the placement relies
    // on, and a future refactor that splits them would break it silently.
    renderEditor(teamMatch({ subResultsUnreadable: true }), 'inline');
    expect(screen.getByText(NOTE)).toBeTruthy();
  });

  it('says nothing when the encounter read fine', () => {
    renderEditor(teamMatch(), 'modal');
    expect(screen.queryByText(NOTE)).toBeNull();
  });

  it('says nothing for an encounter that simply has no bouts yet', () => {
    // An unscored team match also shows empty rows. The notice must describe a
    // PARSE failure only, never the ordinary pre-match state.
    renderEditor(teamMatch({ status: 'running', subResults: [] }), 'modal');
    expect(screen.queryByText(NOTE)).toBeNull();
  });
});
