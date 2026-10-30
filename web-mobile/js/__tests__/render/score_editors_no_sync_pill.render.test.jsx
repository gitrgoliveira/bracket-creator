// bc-sypl: the individual, team and engi score editors carry no sync pill.
// The operator's held-write signals are the editors' own pending banner and the
// topbar's held-writes count, so a pill in the editor head was a second, louder
// voice for the same state. The component is deleted, not hidden: no element of
// it renders in an editor, inline or overlay, whatever the sync status says,
// and the barrel no longer exports it.

import React from 'react';
import { render, act, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let barrel;
let ScoreEditorModal;
let EngiScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  barrel = await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
  ({ EngiScoreEditorModal } = await import('../../admin_scoring_engi.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue({ applied: true }),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    reopenMatch: vi.fn(),
    ...makeFakeOutbox().api,
  };
  // A status that would have drawn a loud pill if one still existed.
  window.subscribeSyncStatus = (fn) => { fn('offline'); return () => {}; };
});

afterEach(() => {
  cleanup();
  delete window.subscribeSyncStatus;
});

const individual = (over = {}) => ({
  id: 'm-ind', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ...over,
});
const team = (over = {}) => ({
  id: 'm-team', compId: 'comp1', status: 'running', phase: 'bracket', round: 'Final', court: 'A',
  compKind: 'team', teamSize: 3, compFormat: 'knockout', teamMatchType: 'fixed',
  sideA: { id: 'team-A', name: 'Team A' }, sideB: { id: 'team-B', name: 'Team B' },
  subResults: [],
  ...over,
});
const engi = (over = {}) => ({
  id: 'm-engi', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compEngi: true,
  sideA: { id: 'p1', name: 'Ito - Abe' }, sideB: { id: 'p2', name: 'Ono - Sato' },
  ...over,
});

const onSubmit = vi.fn().mockResolvedValue({ applied: true });

async function mountEditor(Editor, match, variant) {
  const props = variant ? { variant } : {};
  await act(async () => {
    render(<Editor match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" {...props} />);
  });
}

function expectNoPill() {
  expect(document.querySelector('[data-testid="sync-status-pill"]')).toBeNull();
  expect(document.querySelector('[class*="sync-pill"]')).toBeNull();
  const text = document.body.textContent;
  expect(text).not.toMatch(/Syncing…/);
  expect(text).not.toMatch(/Sign in to save/);
}

const EDITORS = [
  ['individual', () => ScoreEditorModal, individual],
  ['team', () => ScoreEditorModal, team],
  ['engi', () => EngiScoreEditorModal, engi],
];

describe('score editors carry no sync pill (bc-sypl)', () => {
  for (const [name, editorOf, makeMatch] of EDITORS) {
    for (const status of ['running', 'scheduled']) {
      it(`${name} editor, overlay, ${status} match: no pill`, async () => {
        await mountEditor(editorOf(), makeMatch({ status }));
        expect(document.querySelector('.editor-modal__title, [data-testid]')).not.toBeNull();
        expectNoPill();
      });
      it(`${name} editor, inline, ${status} match: no pill`, async () => {
        await mountEditor(editorOf(), makeMatch({ status }), 'inline');
        expectNoPill();
      });
    }
  }

  it('the barrel no longer exports SyncStatusPill', () => {
    expect(barrel.SyncStatusPill).toBeUndefined();
    expect('SyncStatusPill' in barrel).toBe(false);
  });
});
