// A recorded decision closes its form, in both editors, whatever the host
// does next. A host that cannot move on (the court's next match refused to
// start, e.g. another competition's match is running there) leaves the
// decided match on screen; the form left open there offered Record again and
// hid the recorded decision. Before the Remaining matches panel was removed,
// a withdrawal closed its form on the panel's own path; a no-show never did.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (d) => d === 'kiken' || d === 'kiken-voluntary' || d === 'kiken-injury',
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
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

const stored = { status: 'completed', decision: 'kiken-voluntary', decisionBy: 'aka', winner: 'Tanaka', applied: true };

beforeEach(() => {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDecision: vi.fn().mockResolvedValue(stored),
    putMatchLineup: vi.fn(),
  };
});

const individual = {
  id: 'Pool A-1', compId: 'c1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
};
const team = {
  id: 'Pool A-1', compId: 'c1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A',
  compKind: 'team', teamSize: 3,
  sideA: { id: 't1', name: 'Kyoto' }, sideB: { id: 't2', name: 'Osaka' },
};

// Records a withdrawal by Aka through the editor's own form, with a host
// whose advance does not move the editor on.
async function recordWithdrawal(match) {
  const onAfterDecision = vi.fn().mockResolvedValue(undefined);
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} onAfterDecision={onAfterDecision} password="secret" />);
  });
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: /^Kiken . Voluntary$/ })); });
  const form = document.querySelector('form.decision-prompt');
  expect(form, 'the withdrawal form opened').not.toBeNull();
  fireEvent.click(form.querySelector('input[name="decision-side"][value="aka"]'));
  await act(async () => { fireEvent.submit(form); });
  return onAfterDecision;
}

describe('a recorded decision closes its form', () => {
  for (const [label, match] of [['individual', individual], ['team', team]]) {
    it(`${label}: the form closes once the withdrawal is stored`, async () => {
      const onAfterDecision = await recordWithdrawal(match);
      expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
      expect(onAfterDecision).toHaveBeenCalledTimes(1);
      expect(document.querySelector('form.decision-prompt')).toBeNull();
    });

    it(`${label}: the form stays open when the withdrawal did not land`, async () => {
      window.API.recordDecision = vi.fn().mockResolvedValue({ queued: true });
      const onAfterDecision = await recordWithdrawal(match);
      expect(onAfterDecision).not.toHaveBeenCalled();
      expect(document.querySelector('form.decision-prompt')).not.toBeNull();
    });
  }
});
