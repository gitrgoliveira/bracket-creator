// bc-lnrf: what a bout row's name box produces is shown IN that row.
//
// A name typed into a row can be refused (that member already holds another
// position), saved with a team-member identity warning, or fail to save. Those
// messages used to share one sheet-level slot rendered after every bout row and
// the IV/PW band, about 1359px down on an iPad, so the operator saw the typed
// name simply vanish. The notice now sits under the name box it answers.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {},
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_lineup.jsx');
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

const members = (p, names) => names.map((name, i) => ({ id: `${p}${i + 1}`, index: i + 1, name }));
const SHIRO_NAMES = ['Ren Abe', 'Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda'];

beforeEach(() => {
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] },
    }),
    fetchSquads: vi.fn().mockResolvedValue({
      'team-A': members('a', ['A One', 'A Two', 'A Three', 'A Four', 'A Five']),
      'team-B': members('b', SHIRO_NAMES),
    }),
    // Shiro (side B) already has Ren Abe at senpo.
    fetchLineupInForce: vi.fn(async (_c, teamId) => (
      teamId === 'team-B' ? { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } } : null
    )),
    putMatchLineup: vi.fn(async () => ({})),
    renameTeamMember: vi.fn(async () => true),
    addTeamMember: vi.fn(async (_c, _t, name) => ({ id: 'new-1', index: 6, name })),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    recordDecision: vi.fn(),
    hasPendingTerminalWrite: () => false,
    notePendingEdit: () => () => {},
  };
});

async function mountFivePerson() {
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
  await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
  return utils;
}

const jihoRow = (container) => container.querySelectorAll('.team-sub-match')[1];
const jihoShiroInput = (container) => jihoRow(container).querySelector('.team-sub-match__side--shiro input');

async function typeName(input, name) {
  await act(async () => {
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
  await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
}

const notices = (container) => container.querySelectorAll('[data-testid="team-editor-lineup-warning"]');

describe('team editor: a lineup refusal shows in the row the operator typed in (bc-lnrf)', () => {
  it('refuses a member already placed at another position, in the jiho row', async () => {
    const { container } = await mountFivePerson();
    await typeName(jihoShiroInput(container), 'Ren Abe');

    expect(window.API.putMatchLineup, 'nothing is written for a refusal').not.toHaveBeenCalled();
    expect(notices(container)).toHaveLength(1);
    const notice = notices(container)[0];
    expect(notice.textContent).toBe('Ren Abe is already at Senpo.');
    expect(notice.getAttribute('role')).toBe('alert');
    expect(notice.classList.contains('alert--error')).toBe(true);
    expect(notice.getAttribute('data-tone')).toBe('error');
    expect(notice.closest('.team-sub-match')).toBe(jihoRow(container));
    expect(notice.closest('.team-sub-match__side--shiro')).not.toBeNull();
    expect(container.querySelector('[data-testid="team-editor-error"]')).toBeNull();
  });

  it('shows the identity warning of a saved lineup in the row, as a status', async () => {
    window.API.addTeamMember = vi.fn(async () => { throw new Error('offline'); });
    const { container } = await mountFivePerson();
    await typeName(jihoShiroInput(container), 'Newcomer');

    expect(window.API.putMatchLineup, 'the lineup itself is saved').toHaveBeenCalled();
    expect(notices(container)).toHaveLength(1);
    const notice = notices(container)[0];
    expect(notice.textContent).toMatch(/^Lineup saved, but/);
    expect(notice.getAttribute('role')).toBe('status');
    expect(notice.classList.contains('alert--warn')).toBe(true);
    expect(notice.getAttribute('data-tone')).toBe('warn');
    expect(notice.closest('.team-sub-match')).toBe(jihoRow(container));
    expect(notice.closest('.team-sub-match__side--shiro')).not.toBeNull();
  });

  it('shows a failed lineup save in the row and not in the sheet error', async () => {
    window.API.putMatchLineup = vi.fn(async () => { throw new Error(''); });
    const { container } = await mountFivePerson();
    await typeName(jihoShiroInput(container), 'Kai Mori');

    expect(notices(container)).toHaveLength(1);
    const notice = notices(container)[0];
    expect(notice.textContent).toBe('Failed to update lineup');
    expect(notice.getAttribute('role')).toBe('alert');
    expect(notice.closest('.team-sub-match')).toBe(jihoRow(container));
    expect(container.querySelector('[data-testid="team-editor-error"]')).toBeNull();
  });

  it('clears the notice when the next lineup write starts', async () => {
    const { container } = await mountFivePerson();
    await typeName(jihoShiroInput(container), 'Ren Abe');
    expect(notices(container)).toHaveLength(1);

    await typeName(jihoShiroInput(container), 'Kai Mori');
    expect(notices(container)).toHaveLength(0);
  });
});
