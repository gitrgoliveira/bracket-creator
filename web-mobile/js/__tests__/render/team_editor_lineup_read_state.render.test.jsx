// A name picked in a bout row saves the WHOLE lineup for the match, and every
// later match of the team inherits it (the carry rule). So the sheet must never
// compose that write on a lineup it has not read, or has read and let go stale:
//
//   - both teams' lineups are read together, and a team whose lineup could not
//     be read is told apart from one with nothing saved (null is a read);
//   - a side whose lineup was never read refuses the pick, in the row, and
//     sends nothing;
//   - every write re-reads that side first and is composed on what the server
//     holds now; when the server cannot be asked, or a save of the lineup is
//     still waiting in the outbox, it is composed on the lineup held here, and
//     putMatchLineup queues it when offline;
//   - a lineup change announced for this competition is followed, but never in
//     the middle of this sheet's own write for that side;
//   - a notice for a kachinuki bout still shows, right after its row, once that
//     bout is read-only.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { FETCH_TIMEOUT_MS } from '../../write_result.jsx';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {},
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const NOT_READ = 'The lineup could not be read, so this name was not saved. Close and reopen the match.';

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

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 0)); });

beforeEach(() => {
  window.compMatches = () => [];
  window.compMatchesForCompetition = () => [];
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'knockout', teamMatchType: 'fixed', naginata: false, players: [] },
    }),
    fetchSquads: vi.fn().mockResolvedValue({
      'team-A': members('a', ['A One', 'A Two', 'A Three', 'A Four', 'A Five']),
      'team-B': members('b', SHIRO_NAMES),
    }),
    fetchLineupInForce: vi.fn(async () => null),
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

const fivePersonMatch = () => ({
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
});

async function mountFivePerson() {
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal match={fivePersonMatch()} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" />
    );
  });
  await flush();
  return utils;
}

const jihoRow = (container) => container.querySelectorAll('.team-sub-match')[1];
const jihoInput = (container, color) => jihoRow(container).querySelector(`.team-sub-match__side--${color} input`);
const notices = (container) => container.querySelectorAll('[data-testid="team-editor-lineup-warning"]');
const readsOf = (teamId) => window.API.fetchLineupInForce.mock.calls.filter((c) => c[1] === teamId).length;

async function typeName(input, name) {
  await act(async () => {
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: name } });
    fireEvent.keyDown(input, { key: 'Enter' });
  });
  await flush();
}

const announceLineupChange = (detail) => act(async () => {
  window.dispatchEvent(new CustomEvent('lineup-updated', detail === undefined ? {} : { detail }));
});

describe('team editor: both teams\' lineups are read together', () => {
  it('asks for the Aka and the Shiro lineup before either answers', async () => {
    const gates = {};
    window.API.fetchLineupInForce = vi.fn((_c, teamId) => new Promise((resolve) => { gates[teamId] = resolve; }));
    await mountFivePerson();

    expect(window.API.fetchLineupInForce.mock.calls.map((c) => c[1]).sort()).toEqual(['team-A', 'team-B']);

    await act(async () => { gates['team-A'](null); gates['team-B'](null); });
  });

  it('keeps the side that could be read when the other cannot', async () => {
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (teamId === 'team-B') throw new Error('offline');
      return null;
    });
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'aka'), 'A Two');
    expect(window.API.putMatchLineup, 'the readable side still saves').toHaveBeenCalledTimes(1);
    expect(window.API.putMatchLineup.mock.calls[0][1]).toBe('team-A');
    expect(notices(container)).toHaveLength(0);

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    expect(window.API.putMatchLineup, 'the unreadable side does not').toHaveBeenCalledTimes(1);
    expect(notices(container)).toHaveLength(1);
  });
});

describe('team editor: a side whose lineup was not read never writes one', () => {
  it('refuses the pick in its row and sends nothing', async () => {
    window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error('offline'));
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(notices(container)).toHaveLength(1);
    const notice = notices(container)[0];
    expect(notice.textContent).toBe(NOT_READ);
    expect(notice.getAttribute('role')).toBe('alert');
    expect(notice.getAttribute('data-tone')).toBe('error');
    expect(notice.classList.contains('alert--error')).toBe(true);
    expect(notice.closest('.team-sub-match')).toBe(jihoRow(container));
    expect(notice.closest('.team-sub-match__side--shiro')).not.toBeNull();
  });

  it('treats "nothing saved" as a read: the pick is written on an empty lineup', async () => {
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(notices(container)).toHaveLength(0);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(window.API.putMatchLineup.mock.calls[0][3]).toEqual({ jiho: 'Kai Mori' });
  });

  it('stays refused until a lineup has been read, and can write once a change announced is read', async () => {
    let healthy = false;
    window.API.fetchLineupInForce = vi.fn(async () => {
      if (!healthy) throw new Error('offline');
      return null;
    });
    const { container } = await mountFivePerson();
    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();

    // The server is back, but nothing on this sheet has read the lineup yet.
    healthy = true;
    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(notices(container)[0].textContent).toBe(NOT_READ);

    await announceLineupChange({ competitionId: 'comp1' });
    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(notices(container)).toHaveLength(0);
  });
});

describe('team editor: a lineup write is composed on the lineup the server holds now', () => {
  const OPEN = { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } };
  const NOW = { positions: { senpo: 'Ren Abe', fukusho: 'Rin Ota' }, memberIds: { senpo: 'b1', fukusho: 'b4' } };

  it.each([
    ['the outbox cannot be asked', undefined],
    ['no save of the lineup is queued', () => false],
  ])('re-reads first, so a position set elsewhere since opening is not written away (%s)', async (_what, queuedLineupSave) => {
    window.API.queuedLineupSave = queuedLineupSave;
    let shiroReads = 0;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (teamId !== 'team-B') return null;
      shiroReads += 1;
      return shiroReads === 1 ? OPEN : NOW;
    });
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, teamId, matchId, positions, , memberIds] = window.API.putMatchLineup.mock.calls[0];
    expect([teamId, matchId]).toEqual(['team-B', 'm1']);
    expect(positions).toEqual({ senpo: 'Ren Abe', fukusho: 'Rin Ota', jiho: 'Kai Mori' });
    expect(memberIds).toEqual({ senpo: 'b1', fukusho: 'b4', jiho: 'b2' });
  });

  it('writes on the lineup it holds when the lineup cannot be read again, and does not refuse', async () => {
    let shiroReads = 0;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (teamId !== 'team-B') return null;
      shiroReads += 1;
      if (shiroReads > 1) throw new Error('offline');
      return OPEN;
    });
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(notices(container)).toHaveLength(0);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, teamId, matchId, positions, , memberIds] = window.API.putMatchLineup.mock.calls[0];
    expect([teamId, matchId]).toEqual(['team-B', 'm1']);
    expect(positions).toEqual({ senpo: 'Ren Abe', jiho: 'Kai Mori' });
    expect(memberIds).toEqual({ senpo: 'b1', jiho: 'b2' });
  });

  it('keeps writing on the lineup it holds, pick after pick, while the server cannot be reached', async () => {
    let reachable = true;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (!reachable) throw new Error('offline');
      return teamId === 'team-B' ? OPEN : null;
    });
    window.API.putMatchLineup = vi.fn(async () => ({ queued: true }));
    const { container } = await mountFivePerson();
    reachable = false;

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    const chuken = container.querySelectorAll('.team-sub-match')[2].querySelector('.team-sub-match__side--shiro input');
    await typeName(chuken, 'Yui Sato');

    expect(notices(container)).toHaveLength(0);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(2);
    expect(window.API.putMatchLineup.mock.calls[1][3]).toEqual({ senpo: 'Ren Abe', jiho: 'Kai Mori', chuken: 'Yui Sato' });
  });

  it('writes on the lineup it holds, and frees the boxes, when the lineup is not answered in time', async () => {
    let shiroReads = 0;
    window.API.fetchLineupInForce = vi.fn((_c, teamId) => {
      if (teamId !== 'team-B') return Promise.resolve(null);
      shiroReads += 1;
      return shiroReads === 1 ? Promise.resolve(OPEN) : new Promise(() => {});
    });
    const { container } = await mountFivePerson();

    vi.useFakeTimers();
    try {
      const input = jihoInput(container, 'shiro');
      await act(async () => {
        fireEvent.focus(input);
        fireEvent.change(input, { target: { value: 'Kai Mori' } });
        fireEvent.keyDown(input, { key: 'Enter' });
      });
      expect(input.disabled, 'the boxes wait while the lineup is read').toBe(true);
      expect(window.API.putMatchLineup).not.toHaveBeenCalled();
      await act(async () => { vi.advanceTimersByTime(FETCH_TIMEOUT_MS); });
      await act(async () => { await Promise.resolve(); });
    } finally {
      vi.useRealTimers();
    }

    expect(notices(container)).toHaveLength(0);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(window.API.putMatchLineup.mock.calls[0][3]).toEqual({ senpo: 'Ren Abe', jiho: 'Kai Mori' });
    expect(jihoInput(container, 'shiro').disabled).toBe(false);
  });

  it('shows the lineup it re-read, not the one from opening, after a write it refused', async () => {
    let shiroReads = 0;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (teamId !== 'team-B') return null;
      shiroReads += 1;
      return shiroReads === 1 ? { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } }
        : { positions: { senpo: 'Kai Mori', chuken: 'Ren Abe' }, memberIds: { senpo: 'b2', chuken: 'b1' } };
    });
    const { container } = await mountFivePerson();
    const senpoInput = () => container.querySelectorAll('.team-sub-match')[0].querySelector('.team-sub-match__side--shiro input');
    expect(senpoInput().value).toBe('Ren Abe');

    // Ren Abe is at chuken in the lineup it re-reads, so this pick is refused.
    await typeName(jihoInput(container, 'shiro'), 'Ren Abe');

    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(notices(container)[0].textContent).toBe('Ren Abe is already at Chuken.');
    expect(senpoInput().value, 'the rows show the lineup it re-read').toBe('Kai Mori');
  });
});

// A save of the lineup still waiting in the outbox (API.queuedLineupSave) is not
// on the server yet, so the server's copy lacks that edit and putMatchLineup
// would replace the queued save with whatever the next write is composed on.
describe('team editor: a save of this lineup is still waiting in the outbox', () => {
  const OPEN = { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } };
  const SERVER_COPY = { positions: { senpo: 'Ren Abe', fukusho: 'Rin Ota' }, memberIds: { senpo: 'b1', fukusho: 'b4' } };
  const senpoInput = (container) => container.querySelectorAll('.team-sub-match')[0].querySelector('.team-sub-match__side--shiro input');

  function serverAnswering(copy) {
    let shiroReads = 0;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (teamId !== 'team-B') return null;
      shiroReads += 1;
      return shiroReads === 1 ? OPEN : copy;
    });
  }

  it('composes the write on the lineup it holds, and does not read the server\'s copy', async () => {
    window.API.queuedLineupSave = vi.fn(() => true);
    serverAnswering(SERVER_COPY);
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(window.API.queuedLineupSave).toHaveBeenCalledWith('comp1', 'team-B', { matchId: 'm1' });
    expect(readsOf('team-B'), 'only the read at opening').toBe(1);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, teamId, , positions, , memberIds] = window.API.putMatchLineup.mock.calls[0];
    expect(teamId).toBe('team-B');
    expect(positions).toEqual({ senpo: 'Ren Abe', jiho: 'Kai Mori' });
    expect(memberIds).toEqual({ senpo: 'b1', jiho: 'b2' });
  });

  it('asks about the side it writes, and only that side is composed on the held lineup', async () => {
    window.API.queuedLineupSave = vi.fn((_c, teamId) => teamId === 'team-B');
    serverAnswering(SERVER_COPY);
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'aka'), 'A Two');

    expect(window.API.queuedLineupSave).toHaveBeenCalledWith('comp1', 'team-A', { matchId: 'm1' });
    expect(readsOf('team-A'), 'Aka has nothing queued: it reads again before writing').toBe(2);
    expect(window.API.putMatchLineup.mock.calls[0][1]).toBe('team-A');
  });

  it('keeps showing the lineup it holds when a read answers with the server\'s copy meanwhile', async () => {
    let queued = false;
    let serverCopy = OPEN;
    window.API.queuedLineupSave = vi.fn(() => queued);
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => (teamId === 'team-B' ? serverCopy : null));
    const { container } = await mountFivePerson();
    expect(senpoInput(container).value).toBe('Ren Abe');

    queued = true;
    serverCopy = { positions: { senpo: 'Kai Mori' }, memberIds: { senpo: 'b2' } };
    await announceLineupChange({ competitionId: 'comp1' });
    expect(readsOf('team-B'), 'it still reads').toBe(2);
    expect(senpoInput(container).value, 'but the server\'s copy lacks the queued edit').toBe('Ren Abe');

    queued = false;
    await announceLineupChange({ competitionId: 'comp1' });
    expect(senpoInput(container).value, 'and follows the server again once the save is sent').toBe('Kai Mori');
  });

  it('still shows the lineup read at opening when a save was already queued then', async () => {
    window.API.queuedLineupSave = vi.fn(() => true);
    serverAnswering(SERVER_COPY);
    const { container } = await mountFivePerson();

    expect(senpoInput(container).value).toBe('Ren Abe');
    expect(notices(container)).toHaveLength(0);
  });
});

describe('team editor: it follows a lineup change announced for this competition', () => {
  const OPEN = { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } };
  const CHANGED = { positions: { senpo: 'Kai Mori' }, memberIds: { senpo: 'b2' } };
  const senpoInput = (container) => container.querySelectorAll('.team-sub-match')[0].querySelector('.team-sub-match__side--shiro input');

  it('reads both teams again and shows what it reads', async () => {
    let announced = false;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => (
      teamId === 'team-B' ? (announced ? CHANGED : OPEN) : null
    ));
    const { container } = await mountFivePerson();
    expect(senpoInput(container).value).toBe('Ren Abe');
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);

    announced = true;
    await announceLineupChange({ competitionId: 'comp1' });

    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([2, 2]);
    expect(senpoInput(container).value).toBe('Kai Mori');
  });

  it('keeps the lineup it holds when the read that follows an announcement fails', async () => {
    let healthy = true;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (!healthy) throw new Error('offline');
      return teamId === 'team-B' ? OPEN : null;
    });
    const { container } = await mountFivePerson();

    healthy = false;
    await announceLineupChange({ competitionId: 'comp1' });

    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([2, 2]);
    expect(senpoInput(container).value).toBe('Ren Abe');
    expect(notices(container)).toHaveLength(0);
  });

  it('keeps the read that began last when an earlier one answers after it', async () => {
    const reads = [];
    window.API.fetchLineupInForce = vi.fn((_c, teamId) => {
      const gate = deferred();
      reads.push({ teamId, ...gate });
      return gate.promise;
    });
    const { container } = await mountFivePerson();
    await announceLineupChange({ competitionId: 'comp1' });
    expect(reads.map((r) => r.teamId)).toEqual(['team-A', 'team-B', 'team-A', 'team-B']);

    await act(async () => { reads[3].resolve(CHANGED); reads[2].resolve(null); });
    await act(async () => { reads[1].resolve(OPEN); reads[0].resolve(null); });

    expect(senpoInput(container).value).toBe('Kai Mori');
  });

  it('ignores a change announced for another competition', async () => {
    await mountFivePerson();
    await announceLineupChange({ competitionId: 'another-comp' });
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);
  });

  it('reads again when the announcement names no competition', async () => {
    await mountFivePerson();
    await announceLineupChange(undefined);
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([2, 2]);
  });

  it('stops listening when the editor closes', async () => {
    const { unmount } = await mountFivePerson();
    unmount();
    await announceLineupChange({ competitionId: 'comp1' });
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);
  });

  it('leaves a side alone while this sheet is writing its lineup, reads it after, and does not wait to read the other', async () => {
    const put = deferred();
    window.API.putMatchLineup = vi.fn(() => put.promise);
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    // Mount read each team once; the pick's own re-read took Shiro's second.
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 2]);

    await announceLineupChange({ competitionId: 'comp1' });
    expect(readsOf('team-A'), 'the side nobody is writing is read at once').toBe(2);
    expect(readsOf('team-B'), 'the side being written waits').toBe(2);

    await act(async () => { put.resolve({}); });
    await flush();
    expect(readsOf('team-B'), 'and is read once its write has settled').toBe(3);
    expect(readsOf('team-A')).toBe(2);
  });

  it('does not read a side again after a write when nothing was announced meanwhile', async () => {
    const { container } = await mountFivePerson();
    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 2]);
  });
});

// A kachinuki encounter whose bouts 1-5 are fought and whose current bout is
// position 6, beyond the 5-person team, so both its pickers take the manual
// path and a typed name over a picked blank member renames that member.
describe('team editor: a notice for a kachinuki bout shows once that bout is read-only', () => {
  const SQUAD_A = [
    { id: 'm-kept', index: 1, name: 'Kept Winner' },
    { id: 'm-blank', index: 7, name: '' },
  ];
  const SQUAD_B = [{ id: 'm-shiro', index: 1, name: 'Shiro One' }];

  const fought = [1, 2, 3, 4, 5].map((position) => ({
    position, sideA: `Aka ${position}`, sideB: `Shiro ${position}`, ipponsA: ['M'], ipponsB: [],
  }));
  const kachinukiMatch = (subResults) => ({
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 5,
    compFormat: 'mixed',
    teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A', number: 'T5' },
    sideB: { id: 'team-B', name: 'Team B', number: 'T9' },
    subResults,
  });
  const bout6 = { position: 6, sideA: 'Aka 5', sideB: '', ipponsA: [], ipponsB: [] };
  const bout6Fought = { ...bout6, sideB: 'Shiro 6', ipponsA: ['M'] };
  const bout7 = { position: 7, sideA: 'Aka 5', sideB: '', ipponsA: [], ipponsB: [] };

  const editorProps = { onClose: vi.fn(), onSubmit: vi.fn().mockResolvedValue(undefined), password: 'secret' };

  beforeEach(() => {
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    });
    window.API.fetchSquads = vi.fn().mockResolvedValue({ 'team-A': SQUAD_A, 'team-B': SQUAD_B });
  });

  async function mountKachinuki(subResults) {
    let utils;
    await act(async () => { utils = render(<ScoreEditorModal match={kachinukiMatch(subResults)} {...editorProps} />); });
    await flush();
    return utils;
  }
  const currentRow = () => document.querySelector('.team-sub-match:not(.team-sub-match--readonly)');
  // A read-only row is a role="button", whose children are presentational to
  // assistive tech: its notice follows it as a sibling, so it is announced.
  const noticeAfter = (rowTestId) => {
    const row = document.querySelector(`[data-testid="${rowTestId}"]`);
    const next = row && row.nextElementSibling;
    return next && next.getAttribute('data-testid') === 'team-editor-lineup-warning' ? next : null;
  };
  const expectOutsideTheButton = (rowTestId, notice) => {
    expect(document.querySelector(`[data-testid="${rowTestId}"]`).getAttribute('role')).toBe('button');
    expect(document.querySelector(`[data-testid="${rowTestId}"]`).contains(notice)).toBe(false);
    expect(notice.closest('[role="button"]')).toBeNull();
  };

  it('a failed rename that answers after the bout was recorded shows right after that bout\'s row', async () => {
    const rename = deferred();
    window.API.renameTeamMember = vi.fn(() => rename.promise);
    const { rerender } = await mountKachinuki([...fought, bout6]);

    // Pick the blank member for Aka, then type its name: the member is renamed.
    const akaInput = () => currentRow().querySelector('.team-sub-match__side--aka input');
    await act(async () => { fireEvent.focus(akaInput()); });
    const blank = [...document.querySelectorAll('.team-sub-match__side--aka .pmf__option')].find((b) => b.textContent.includes('T5.7'));
    await act(async () => { fireEvent.click(blank); });
    await act(async () => {
      fireEvent.focus(akaInput());
      fireEvent.change(akaInput(), { target: { value: 'Ito' } });
      fireEvent.keyDown(akaInput(), { key: 'Enter' });
    });
    expect(window.API.renameTeamMember).toHaveBeenCalledTimes(1);

    // The bout is scored and recorded before the rename answers: it is read-only.
    const aka = [...currentRow().querySelectorAll('.team-sub-match__side--aka button.ipt-btn')].find((b) => b.textContent === 'M');
    await act(async () => { fireEvent.click(aka); });
    await act(async () => {
      rerender(<ScoreEditorModal match={kachinukiMatch([...fought, bout6Fought, bout7])} {...editorProps} />);
    });
    expect(document.querySelector('[data-testid="kachinuki-done-bout-5"]'), 'bout 6 is read-only now').not.toBeNull();
    expect(document.querySelectorAll('[data-testid="team-editor-lineup-warning"]')).toHaveLength(0);

    await act(async () => { rename.reject(new Error('could not rename')); });
    await flush();

    const notice = noticeAfter('kachinuki-done-bout-5');
    expect(notice, 'the notice follows the read-only row').not.toBeNull();
    expect(notice.textContent).toMatch(/^"Ito" was used for this bout, but the team member could not be renamed\./);
    expect(notice.getAttribute('role')).toBe('status');
    expect(notice.getAttribute('data-tone')).toBe('warn');
    expect(notice.classList.contains('alert--warn')).toBe(true);
    expectOutsideTheButton('kachinuki-done-bout-5', notice);
    expect(document.querySelectorAll('[data-testid="team-editor-lineup-warning"]')).toHaveLength(1);
  });

  it('a refusal on the first bout still shows, right after its row, when that bout is recorded', async () => {
    window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error('offline'));
    const first = { position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [] };
    const { rerender } = await mountKachinuki([first]);

    const shiroInput = () => currentRow().querySelector('.team-sub-match__side--shiro input');
    await act(async () => { fireEvent.focus(shiroInput()); });
    const pick = [...document.querySelectorAll('.team-sub-match__side--shiro .pmf__option')][0];
    expect(pick, 'the list offers a team member').toBeTruthy();
    await act(async () => { fireEvent.click(pick); });
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(document.querySelector('[data-testid="team-editor-lineup-warning"]').textContent).toBe(NOT_READ);

    const aka = [...currentRow().querySelectorAll('.team-sub-match__side--aka button.ipt-btn')].find((b) => b.textContent === 'M');
    await act(async () => { fireEvent.click(aka); });
    await act(async () => {
      rerender(<ScoreEditorModal
        match={kachinukiMatch([{ ...first, ipponsA: ['M'], sideA: 'Aka 1', sideB: 'Shiro 1' }, { position: 2, sideA: 'Aka 1', sideB: '', ipponsA: [], ipponsB: [] }])}
        {...editorProps}
      />);
    });

    expect(document.querySelector('[data-testid="kachinuki-done-bout-0"]'), 'bout 1 is read-only now').not.toBeNull();
    const notice = noticeAfter('kachinuki-done-bout-0');
    expect(notice, 'the refusal is still shown, after the bout it was made on').not.toBeNull();
    expect(notice.textContent).toBe(NOT_READ);
    expect(notice.getAttribute('role')).toBe('alert');
    expect(notice.getAttribute('data-tone')).toBe('error');
    expectOutsideTheButton('kachinuki-done-bout-0', notice);
  });
});
