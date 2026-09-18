// bc-dnst: a lineup vacancy is not flagged on the score sheet.
//
// Operator ruling 2026-09-16: a lineup is never required to be complete, so
// the sheet must not tell the operator to go and complete one. The editor
// used to show "SHIRO/AKA: Lineup incomplete, add the remaining players" for
// a five-person team with any position unset. It never blocked anything, but
// the sentence read as a requirement, and vacancies are legitimate play:
// team sizes are unregulated and the FIK five-person back-fill rule was
// removed in mp-gmcg.
//
// This pins the ABSENCE, which nothing else can: a removed warning leaves no
// failing test behind, so without this file the next person to "helpfully"
// restore it would see a green suite.

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
  resolveRoundIndex: () => 0,
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

async function mountFivePerson() {
  window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
    id: 'comp1',
    config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] },
  });
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal
        match={{
          id: 'm1',
          compId: 'comp1',
          status: 'running',
          phase: 'bracket',
          court: 'A',
          compKind: 'team',
          teamSize: 5,
          compFormat: 'knockout',
          teamMatchType: 'fixed',
          round: 'Semi-final',
          matchNumber: 1,
          sideA: { id: 'team-A', name: 'Team A' },
          sideB: { id: 'team-B', name: 'Team B' },
        }}
        onClose={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        password=""
      />
    );
  });
  return utils;
}

describe('team editor: a lineup vacancy is not flagged', () => {
  it('says nothing about an incomplete lineup for a five-person team with no lineup set', async () => {
    const { container } = await mountFivePerson();
    // The editor is genuinely mounted, so the absence below means something.
    expect(container.querySelectorAll('.team-sub-match').length).toBeGreaterThan(0);
    expect(container.textContent).not.toMatch(/incomplete/i);
    expect(container.textContent).not.toMatch(/add the remaining/i);
    expect(container.querySelectorAll('.tsm-lineup__incomplete')).toHaveLength(0);
  });

  it('still lets the operator score and finish with positions unset', async () => {
    const { container } = await mountFivePerson();
    const ippon = container.querySelector('button.ipt-btn');
    expect(ippon).not.toBeNull();
    // A vacancy must not disable scoring: the ruling is that nothing is
    // required, not merely that nothing is said.
    expect(ippon.disabled).toBe(false);
  });
});
