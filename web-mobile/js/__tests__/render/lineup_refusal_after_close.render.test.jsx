// bc-evts stage 3, review round 2, R2: a lineup save is an outbox entry and its
// refusal can come after the editor that made it has closed. An editor claims a
// refusal (onRefusal answers true) only when it says it; once it has closed it
// answers false, so the outbox raises its own alert and the refusal is never
// lost. Pinned on the at-court panel and the Lineups page.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

const SQUAD = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
  { id: 'mem-4', index: 4, name: 'Mori' },
];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const lineupFor = (extra) => ({ teamId: 'team-a', competitionId: 'comp-1', ...NAMES, saved: true, ...extra });
const A = { id: 'team-a', name: 'Team A' };
const B = { id: 'team-b', name: 'Team B' };
const C = { id: 'team-c', name: 'Team C' };
const match = (id, sideA, sideB) => ({ id, status: 'scheduled', sideA, sideB });
const POOL_MATCHES = [match('Pool A-0', A, B), match('Pool A-1', B, C), match('Pool A-2', A, C)];
const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', format: 'pools', status: 'active', teamSize: 3, players: [A, B, C] };
const PANEL_MATCHES = POOL_MATCHES.map((m) => ({ ...m, compId: 'comp-1', phase: 'pool', poolName: 'Pool A' }));

let saved;
let api;
let AdminTeamLineupsList;
let MatchLineupSideEditor;
// The onRefusal each save handed the outbox.
let refusals;

// The outbox's answer to a save: pending, never awaited.
const pending = () => ({ applied: true, pending: true, key: 'k', settled: new Promise(() => {}) });

beforeEach(async () => {
  sessionStorage.clear();
  saved = { API: window.API, confirmDialog: window.confirmDialog, compMatches: window.compMatches };
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  window.compMatches = () => [];
  refusals = [];
  const take = (opts) => { refusals.push(opts && opts.onRefusal); return Promise.resolve(pending()); };
  api = {
    fetchTeamLineup: vi.fn().mockResolvedValue(lineupFor({ round: 0, sourceRound: 0 })),
    fetchLineupInForce: vi.fn().mockResolvedValue(lineupFor({ matchId: 'Pool A-0', sourceMatchId: 'Pool A-0' })),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-a': SQUAD }),
    putTeamLineup: vi.fn((...args) => take(args[7])),
    putMatchLineup: vi.fn((...args) => take(args[7])),
    queuedLineupSave: vi.fn().mockReturnValue(false),
  };
  window.API = api;
  ({ AdminTeamLineupsList } = await import('../../admin_lineup.jsx'));
  ({ MatchLineupSideEditor } = await import('../../admin_schedule_lineup.jsx'));
});

afterEach(() => {
  for (const [key, value] of Object.entries(saved)) {
    if (value === undefined) delete window[key]; else window[key] = value;
  }
});

async function flush() {
  for (let i = 0; i < 6; i += 1) {
    await act(async () => { await Promise.resolve(); });
  }
}

async function typeName(utils, position, name) {
  await act(async () => {
    const input = utils.getByLabelText(`${position} player`);
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
}

async function click(button) {
  await act(async () => { fireEvent.click(button); });
  await flush();
}

describe('a lineup refusal that arrives after the editor closed reaches the outbox (R2)', () => {
  it('the at-court panel claims it while open, and not once closed', async () => {
    let utils;
    await act(async () => {
      utils = render(<MatchLineupSideEditor comp={COMP} team={A} match={PANEL_MATCHES[2]} allMatches={PANEL_MATCHES} password="pw" showToast={vi.fn()} />);
    });
    await flush();
    await typeName(utils, 1, 'Mori');
    await click(utils.getByRole('button', { name: /^Save lineup$/ }));
    expect(refusals).toHaveLength(1);
    expect(typeof refusals[0]).toBe('function');
    let claimed;
    await act(async () => { claimed = refusals[0](new Error('refused')); });
    expect(claimed, 'said in the open panel').toBe(true);
    utils.unmount();
    expect(refusals[0](new Error('refused')), 'a closed panel says nothing: the outbox must').toBe(false);
  });

  it('the Lineups page claims it while open, and not once closed', async () => {
    let utils;
    await act(async () => {
      utils = render(<AdminTeamLineupsList comp={COMP} poolMatches={POOL_MATCHES} password="pw" showToast={vi.fn()} />);
    });
    await flush();
    await act(async () => { fireEvent.change(utils.getByTestId('lineup-position-1'), { target: { value: 'mem-4' } }); });
    await click(utils.getByRole('button', { name: /^Save lineup$/ }));
    expect(refusals).toHaveLength(1);
    let claimed;
    await act(async () => { claimed = refusals[0](new Error('refused')); });
    expect(claimed).toBe(true);
    utils.unmount();
    expect(refusals[0](new Error('refused'))).toBe(false);
  });
});
