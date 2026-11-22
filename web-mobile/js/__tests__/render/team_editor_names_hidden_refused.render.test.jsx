// bc-hnam: the team score sheet refuses a competition read with its
// competitors' names hidden.
//
// The sheet seeds each bout's fighters from the server's rows, and a masked
// read names a team member by their label ("T10.1") where the name goes. A
// save from such a sheet would store the label as the fighter's name, so the
// sheet shows the refusal alone, with no control that writes: a masked read
// (namesHidden), or an admin route's read refused as masked
// (refuseMaskedForOrganiser, code names_hidden).

import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    fetchMatchLineups: vi.fn().mockResolvedValue({}),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
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

const MATCH = {
  id: 'm1',
  compId: 'comp1',
  status: 'running',
  phase: 'bracket',
  court: 'A',
  compKind: 'team',
  teamSize: 3,
  compFormat: 'knockout',
  teamMatchType: 'fixed',
  round: 'Final',
  matchNumber: 1,
  sideA: { id: 'team-A', name: 'Team A' },
  sideB: { id: 'team-B', name: 'Team B' },
};

async function mount() {
  let utils;
  await act(async () => {
    utils = render(<ScoreEditorModal match={MATCH} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="pw" />);
  });
  return utils;
}

describe('team editor: a read with names hidden is refused', () => {
  it('scores as usual when the competition shows its names', async () => {
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
      id: 'comp1', config: { format: 'knockout', teamMatchType: 'fixed', players: [] },
    });
    const { container, queryByTestId } = await mount();
    expect(queryByTestId('team-editor-names-hidden')).toBeNull();
    expect(container.querySelector('button.ipt-btn')).not.toBeNull();
  });

  it('shows the refusal alone when the read came back masked', async () => {
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
      id: 'comp1', namesHidden: true, config: { format: 'knockout', teamMatchType: 'fixed', players: [] },
    });
    const { container, getByTestId } = await mount();
    expect(getByTestId('team-editor-names-hidden').textContent).toMatch(/names hidden/);
    expect(container.querySelector('button.ipt-btn')).toBeNull();
    expect(container.querySelectorAll('.team-sub-match')).toHaveLength(0);
  });

  it('shows the refusal when an admin route refused the read as masked', async () => {
    const refused = Object.assign(new Error('masked'), { code: 'names_hidden' });
    window.API.fetchCompetitionDetails = vi.fn().mockRejectedValue(refused);
    const warn = vi.spyOn(console, 'warn');
    const { container, getByTestId } = await mount();
    expect(getByTestId('team-editor-names-hidden')).toBeTruthy();
    expect(container.querySelector('button.ipt-btn')).toBeNull();
    warn.mockClear();
  });
});
