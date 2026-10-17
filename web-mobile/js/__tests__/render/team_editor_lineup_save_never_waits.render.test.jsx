// bc-evts stage 3, review round 1, M5: "ALL goes to the outbox ... but never
// blocks the UI" (operator ruling). A name picked in a bout row is saved as an
// entry of the match's outbox: the pick shows at once, the box is never disabled
// while the save is out, a refusal the server gives later is said in the row and
// the side read again, and the identity warning ("Lineup saved, but ...") waits
// until the save has landed.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { answered } from '../helpers/team_members.js';

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
    putMatchLineup: vi.fn(),
    // The server answers a member write with the member it holds, stamped.
    renameTeamMember: vi.fn(async (_c, _t, id, name) => answered({ id, index: Number(id.slice(1)) }, { name })),
    addTeamMember: vi.fn(async (_c, _t, name) => answered({ id: 'new-1', index: 6 }, { name })),
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

// A pending answer, as the outbox gives: `settled` resolves when the test says.
function pendingSave() {
  let resolve;
  const settled = new Promise((r) => { resolve = r; });
  const calls = [];
  window.API.putMatchLineup = vi.fn((...args) => {
    calls.push(args);
    return Promise.resolve({ applied: true, pending: true, key: 'c:m1\u001fx', settled });
  });
  return { calls, land: (lineup) => act(async () => { resolve(lineup); await settled; }) };
}

describe('team editor: a lineup save never waits on the server (bc-evts M5)', () => {
  it('shows the pick at once and keeps the box enabled while the save is out', async () => {
    pendingSave();
    const { container } = await mountFivePerson();
    await typeName(jihoShiroInput(container), 'Kai Mori');
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const input = jihoShiroInput(container);
    expect(input.disabled).toBe(false);
    expect(input.value).toBe('Kai Mori');
    expect(notices(container)).toHaveLength(0);
  });

  it('says a refusal the server gives later in the row, and reads the side again', async () => {
    const save = pendingSave();
    const { container } = await mountFivePerson();
    await typeName(jihoShiroInput(container), 'Kai Mori');
    const reads = window.API.fetchLineupInForce.mock.calls.length;
    const opts = save.calls[0][7];
    let claimed;
    await act(async () => { claimed = opts.onRefusal(new Error('Kai Mori is already at Chuken.')); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(claimed).toBe(true);
    expect(notices(container)).toHaveLength(1);
    expect(notices(container)[0].textContent).toBe('Kai Mori is already at Chuken.');
    expect(notices(container)[0].closest('.team-sub-match')).toBe(jihoRow(container));
    expect(window.API.fetchLineupInForce.mock.calls.length).toBeGreaterThan(reads);
  });

  it('says the identity warning only once the save has landed', async () => {
    window.API.addTeamMember = vi.fn(async () => { throw new Error('offline'); });
    const save = pendingSave();
    const { container } = await mountFivePerson();
    await typeName(jihoShiroInput(container), 'Newcomer');
    expect(notices(container)).toHaveLength(0);
    await save.land({ positions: { senpo: 'Ren Abe', jiho: 'Newcomer' }, memberIds: { senpo: 'b1' } });
    expect(notices(container)).toHaveLength(1);
    expect(notices(container)[0].textContent).toMatch(/^Lineup saved, but/);
  });
});
