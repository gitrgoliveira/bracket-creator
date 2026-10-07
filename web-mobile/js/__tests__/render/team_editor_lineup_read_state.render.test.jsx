// A name picked in a bout row saves the one position it names, and the server puts
// it on the lineup it holds when the save arrives (operator decision 2026-10-07,
// "Only changed positions"); every later match of the team inherits that lineup (the
// carry rule). The sheet composes nothing and re-reads nothing for the write. What it
// holds for a side is what a pick is checked against (one member at one position) and
// what the rows show, so:
//
//   - both teams' lineups are read together, and a team whose lineup could not
//     be read is told apart from one with nothing saved (null is a read);
//   - a pick on a side whose lineup was never read (its read is still out, or
//     failed) reads it first and saves; it is refused, in the row, with nothing
//     sent, only when that read fails, and a side never read is read again when
//     the connection returns;
//   - a pick on a side that was read is written at once, naming its position: no read
//     of the lineup is made for it, a position set elsewhere since opening stays, and
//     the lineup the server answers (the whole lineup it holds) is what the side shows;
//   - when the server cannot be reached, or a save of the lineup is already waiting in
//     the outbox, the pick is queued and shown on the lineup held; the API joins it to
//     the queued save;
//   - a read that began before a confirmed write never puts the old lineup back;
//   - a lineup change announced for this competition is followed, but never in
//     the middle of this sheet's own write for that side;
//   - a notice for a kachinuki bout still shows, right after its row, once that
//     bout is read-only.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { answered } from '../helpers/team_members.js';
import { lineupPutStubByTeam } from '../helpers/lineup_server.js';
import { FETCH_TIMEOUT_MS, QUEUED_NOTICE, QUEUED_UNSAVED_NOTICE } from '../../write_result.jsx';

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

const NOT_READ = 'The lineup could not be read, so this name was not saved. Check the connection and try again.';

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

// What the server holds by team, which a save lands its position on and answers whole
// (lineupPutStubByTeam); a test sets it to the lineup another device left.
let lineups;

beforeEach(() => {
  lineups = {};
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
    putMatchLineup: lineupPutStubByTeam(() => lineups),
    // The server answers a member write with the member it holds, stamped.
    renameTeamMember: vi.fn(async (_c, _t, id, name) => answered({ id, index: Number(id.slice(1)) || 1 }, { name })),
    addTeamMember: vi.fn(async (_c, _t, name) => answered({ id: 'new-1', index: 6 }, { name })),
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

  it('words the refusal as something to do, not as a reopen the editor may not offer', async () => {
    window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error('offline'));
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(notices(container)[0].textContent).toMatch(/Check the connection and try again\.$/);
    expect(notices(container)[0].textContent).not.toMatch(/reopen/i);
  });

  it('refuses the pick, and frees the boxes, when the read it makes is not answered in time', async () => {
    window.API.fetchLineupInForce = vi.fn(() => new Promise(() => {}));
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
      await act(async () => { vi.advanceTimersByTime(FETCH_TIMEOUT_MS); });
      await act(async () => { await Promise.resolve(); });
    } finally {
      vi.useRealTimers();
    }

    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(notices(container)[0].textContent).toBe(NOT_READ);
    expect(jihoInput(container, 'shiro').disabled).toBe(false);
  });
});

// The reads start only once the competition has been fetched, so on a slow link
// the first pick on a side can come while its first read is still out, and after
// a read that really failed nothing but a pick (or the connection returning, or
// a change announced) reads the side again.
describe('team editor: a pick on a side not yet read reads it first', () => {
  const OPEN = { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } };
  const senpoInput = (container) => container.querySelectorAll('.team-sub-match')[0].querySelector('.team-sub-match__side--shiro input');

  it('reads the lineup itself while the first read is still out, writes the one position, and refuses nothing', async () => {
    lineups['team-B'] = OPEN;
    let unanswered = 2;
    window.API.fetchLineupInForce = vi.fn((_c, teamId) => {
      if (unanswered > 0) {
        unanswered -= 1;
        return new Promise(() => {});
      }
      return Promise.resolve(teamId === 'team-B' ? OPEN : null);
    });
    const { container } = await mountFivePerson();
    expect(readsOf('team-B'), 'opening asked once').toBe(1);

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(notices(container)).toHaveLength(0);
    expect(readsOf('team-B'), 'and the pick asked for itself').toBe(2);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, teamId, matchId, positions, , memberIds, changed] = window.API.putMatchLineup.mock.calls[0];
    expect([teamId, matchId]).toEqual(['team-B', 'm1']);
    expect(positions, 'names the one position it changes').toEqual({ jiho: 'Kai Mori' });
    expect(memberIds).toEqual({ jiho: 'b2' });
    expect(changed).toEqual(['jiho']);
    expect(senpoInput(container).value, 'the side shows the lineup the server answered').toBe('Ren Abe');
  });

  it('reads the side again at the next pick after a read that failed, and saves once it can', async () => {
    lineups['team-B'] = OPEN;
    let healthy = false;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (!healthy) throw new Error('offline');
      return teamId === 'team-B' ? OPEN : null;
    });
    const { container } = await mountFivePerson();
    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
    expect(notices(container)[0].textContent).toBe(NOT_READ);

    healthy = true;
    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(notices(container)).toHaveLength(0);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(window.API.putMatchLineup.mock.calls[0][3]).toEqual({ jiho: 'Kai Mori' });
    expect(senpoInput(container).value, 'and the side shows the lineup the server answered').toBe('Ren Abe');
  });

  it('shows a side that was never read once a change is announced for it', async () => {
    let healthy = false;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (!healthy) throw new Error('offline');
      return teamId === 'team-B' ? OPEN : null;
    });
    const { container } = await mountFivePerson();
    expect(senpoInput(container).value).toBe('');

    healthy = true;
    await announceLineupChange({ competitionId: 'comp1' });

    expect(senpoInput(container).value).toBe('Ren Abe');
  });

});

describe('team editor: a side never read is read again when the connection returns', () => {
  const OPEN = { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } };
  const senpoInput = (container) => container.querySelectorAll('.team-sub-match')[0].querySelector('.team-sub-match__side--shiro input');
  const connectionReturns = () => act(async () => { window.dispatchEvent(new Event('online')); });

  it('reads each side that was never read, and shows what it reads', async () => {
    let healthy = false;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (!healthy) throw new Error('offline');
      return teamId === 'team-B' ? OPEN : null;
    });
    const { container } = await mountFivePerson();
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);
    expect(senpoInput(container).value).toBe('');

    healthy = true;
    await connectionReturns();

    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([2, 2]);
    expect(senpoInput(container).value).toBe('Ren Abe');
    expect(notices(container)).toHaveLength(0);
  });

  it('reads only the side that was never read', async () => {
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (teamId === 'team-B') throw new Error('offline');
      return null;
    });
    await mountFivePerson();

    await connectionReturns();

    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 2]);
  });

  it('does not read a side again once it was read', async () => {
    await mountFivePerson();
    await connectionReturns();
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);
  });

  it('reads nothing when the connection returns after the editor closed', async () => {
    window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error('offline'));
    const { unmount } = await mountFivePerson();
    unmount();
    await connectionReturns();
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);
  });
});

// A read begun by an announcement before the pick can still be out when the pick
// writes, and answers with the lineup from before the write.
describe('team editor: a confirmed write is not undone by a read that began before it', () => {
  const OPEN = { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } };

  it('keeps what was written when a read begun by an announcement before it answers afterwards', async () => {
    const reads = [];
    window.API.fetchLineupInForce = vi.fn((_c, teamId) => {
      if (teamId !== 'team-B') return Promise.resolve(null);
      if (reads.length === 0) { reads.push(null); return Promise.resolve(OPEN); }
      const gate = deferred();
      reads.push(gate);
      return gate.promise;
    });
    const { container } = await mountFivePerson();
    // Announced before the pick: its read is still out when the pick writes.
    await announceLineupChange({ competitionId: 'comp1' });
    expect(reads).toHaveLength(2);

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    expect(jihoInput(container, 'shiro').value).toBe('Kai Mori');

    await act(async () => { reads[1].resolve(OPEN); });
    await flush();

    expect(jihoInput(container, 'shiro').value, 'the lineup from before the write does not come back').toBe('Kai Mori');
  });
});

describe('team editor: a pick names the one position it changes', () => {
  const OPEN = { positions: { senpo: 'Ren Abe' }, memberIds: { senpo: 'b1' } };
  const NOW = { positions: { senpo: 'Ren Abe', fukusho: 'Rin Ota' }, memberIds: { senpo: 'b1', fukusho: 'b4' } };
  const shiroInputAt = (container, row) => container.querySelectorAll('.team-sub-match')[row].querySelector('.team-sub-match__side--shiro input');

  it('writes the one position and reads nothing for it, so a position set elsewhere since opening stays', async () => {
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => (teamId === 'team-B' ? OPEN : null));
    const { container } = await mountFivePerson();
    // Another device set fukusho after this sheet read the lineup.
    lineups['team-B'] = NOW;
    expect(shiroInputAt(container, 3).value, 'this sheet has not heard of it').toBe('');

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(readsOf('team-B'), 'the lineup is not read for the pick').toBe(1);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, teamId, matchId, positions, , memberIds, changed] = window.API.putMatchLineup.mock.calls[0];
    expect([teamId, matchId]).toEqual(['team-B', 'm1']);
    expect(positions).toEqual({ jiho: 'Kai Mori' });
    expect(memberIds).toEqual({ jiho: 'b2' });
    expect(changed).toEqual(['jiho']);
    // The server put it on the lineup it holds, and what it answered is what the side shows.
    expect(lineups['team-B'].positions).toEqual({ senpo: 'Ren Abe', fukusho: 'Rin Ota', jiho: 'Kai Mori' });
    expect(shiroInputAt(container, 3).value, 'the position another device set shows after the write').toBe('Rin Ota');
    expect(notices(container)).toHaveLength(0);
  });

  it('writes a cleared name as that position\'s empty name, which the server needs to be there', async () => {
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => (teamId === 'team-B' ? OPEN : null));
    lineups['team-B'] = OPEN;
    const { container } = await mountFivePerson();

    const clear = container.querySelectorAll('.team-sub-match')[0].querySelector('.team-sub-match__side--shiro .lineup-name__clear');
    await act(async () => { fireEvent.click(clear); });
    await flush();

    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, , , positions, , memberIds, changed] = window.API.putMatchLineup.mock.calls[0];
    expect(positions).toEqual({ senpo: '' });
    expect(memberIds).toBeUndefined();
    expect(changed).toEqual(['senpo']);
    expect(lineups['team-B'].positions, 'the server cleared it').toEqual({});
  });

  it('keeps writing, pick after pick, while the server cannot be reached: each is queued with its own position and shown', async () => {
    let reachable = true;
    window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => {
      if (!reachable) throw new Error('offline');
      return teamId === 'team-B' ? OPEN : null;
    });
    window.API.putMatchLineup = vi.fn(async () => ({ queued: true }));
    const { container } = await mountFivePerson();
    reachable = false;

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    const chuken = shiroInputAt(container, 2);
    await typeName(chuken, 'Yui Sato');

    // Each pick only reached the outbox, and says so in its row (the latest pick's: a
    // pick clears the notice before it); a notice that said it was saved, or none,
    // would have the operator believe it was.
    expect([...notices(container)].map((n) => n.textContent)).toEqual([QUEUED_NOTICE]);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(2);
    expect(window.API.putMatchLineup.mock.calls[0][3]).toEqual({ jiho: 'Kai Mori' });
    expect(window.API.putMatchLineup.mock.calls[1][3], 'its own position, not the lineup').toEqual({ chuken: 'Yui Sato' });
    expect(window.API.putMatchLineup.mock.calls[1][6]).toEqual(['chuken']);
    // Neither reached the server, and both are shown on the lineup held.
    expect(shiroInputAt(container, 0).value).toBe('Ren Abe');
    expect(jihoInput(container, 'shiro').value).toBe('Kai Mori');
    expect(chuken.value).toBe('Yui Sato');
  });

  describe('a pick that only reached the outbox is not reported as saved', () => {
    const noticeTexts = (container) => [...notices(container)].map((n) => n.textContent);

    it('says it is not sent yet, in the row, the way every held write is worded', async () => {
      window.API.putMatchLineup = vi.fn(async () => ({ queued: true }));
      const { container } = await mountFivePerson();

      await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

      expect(noticeTexts(container)).toEqual([QUEUED_NOTICE]);
      expect(notices(container)[0].getAttribute('data-tone'), 'a held write is amber, not an error').toBe('warn');
      expect(notices(container)[0].textContent, 'it leads with what is true: not sent yet').toMatch(/^Not sent yet/);
      expect(notices(container)[0].textContent).not.toMatch(/Lineup saved/);
    });

    it('says to keep the page open when the browser could not store it either', async () => {
      window.API.putMatchLineup = vi.fn(async () => ({ queued: true, persisted: false }));
      const { container } = await mountFivePerson();

      await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

      expect(noticeTexts(container)).toEqual([QUEUED_UNSAVED_NOTICE]);
    });

    it('does not say "Lineup saved" over a name that could not be linked to a team member either', async () => {
      window.API.putMatchLineup = vi.fn(async () => ({ queued: true }));
      window.API.addTeamMember = vi.fn().mockRejectedValue(new Error('offline'));
      const { container } = await mountFivePerson();

      await typeName(jihoInput(container, 'shiro'), 'Newcomer');

      expect(window.API.addTeamMember).toHaveBeenCalled();
      expect(noticeTexts(container)).toEqual([QUEUED_NOTICE]);
    });

    it('still says nothing for a pick the server took', async () => {
      const { container } = await mountFivePerson();

      await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

      expect(notices(container)).toHaveLength(0);
    });
  });

  // The pick is checked against the lineup the sheet holds: the one it read, kept by
  // what it announces and by its own writes. The server asks the same of the lineup it
  // composes, and refuses naming both positions when another device placed the member.
  describe('is checked against the lineup the sheet holds', () => {
    const PLACED = { positions: { senpo: 'Kai Mori', chuken: 'Ren Abe' }, memberIds: { senpo: 'b2', chuken: 'b1' } };

    it('refuses a member that lineup already places elsewhere, and sends nothing', async () => {
      window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => (teamId === 'team-B' ? PLACED : null));
      const { container } = await mountFivePerson();

      // Ren Abe is at chuken, so this pick is refused.
      await typeName(jihoInput(container, 'shiro'), 'Ren Abe');

      expect(window.API.putMatchLineup).not.toHaveBeenCalled();
      expect(notices(container)[0].textContent).toBe('Ren Abe is already at Chuken.');
      expect(shiroInputAt(container, 0).value).toBe('Kai Mori');
    });

    it('judges a pick against what a change announced since opening brought', async () => {
      let placed = false;
      window.API.fetchLineupInForce = vi.fn(async (_c, teamId) => (teamId === 'team-B' ? (placed ? PLACED : OPEN) : null));
      const { container } = await mountFivePerson();
      placed = true;
      await announceLineupChange({ competitionId: 'comp1' });
      expect(shiroInputAt(container, 2).value).toBe('Ren Abe');

      await typeName(jihoInput(container, 'shiro'), 'Ren Abe');

      expect(window.API.putMatchLineup).not.toHaveBeenCalled();
      expect(notices(container)[0].textContent).toBe('Ren Abe is already at Chuken.');
    });
  });
});

// A save of the lineup still waiting in the outbox is not on the server yet. The API
// joins a new save to it (one entry per lineup, the union of the positions), so the
// sheet names its own position and composes nothing on what is queued, whichever
// surface queued it (this sheet, the at-court panel or the Lineups page).
// API.queuedLineupSave only says whether one is queued.
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

  it('writes its own position alone and reads nothing for it', async () => {
    window.API.queuedLineupSave = vi.fn(() => true);
    serverAnswering(SERVER_COPY);
    const { container } = await mountFivePerson();

    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');

    expect(readsOf('team-B'), 'only the read at opening').toBe(1);
    expect(window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const [, teamId, , positions, , memberIds, changed] = window.API.putMatchLineup.mock.calls[0];
    expect(teamId).toBe('team-B');
    expect(positions).toEqual({ jiho: 'Kai Mori' });
    expect(memberIds).toEqual({ jiho: 'b2' });
    expect(changed).toEqual(['jiho']);
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

  // The announcement names the team it changed (teamId): only that team's lineup is read
  // again, by the side that has it, and nothing for a team that plays neither side.
  it('reads only the side whose team the announcement names', async () => {
    await mountFivePerson();

    await announceLineupChange({ competitionId: 'comp1', teamId: 'team-B' });
    expect([readsOf('team-A'), readsOf('team-B')], 'the Shiro team alone').toEqual([1, 2]);

    await announceLineupChange({ competitionId: 'comp1', teamId: 'team-A' });
    expect([readsOf('team-A'), readsOf('team-B')], 'the Aka team alone').toEqual([2, 2]);
  });

  it('reads nothing for an announcement that names a team playing neither side', async () => {
    await mountFivePerson();
    await announceLineupChange({ competitionId: 'comp1', teamId: 'team-elsewhere' });
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);
  });

  it('reads nothing when a change is announced after the editor closed', async () => {
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
    // Mount read each team once; the pick read nothing.
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);

    await announceLineupChange({ competitionId: 'comp1' });
    expect(readsOf('team-A'), 'the side nobody is writing is read at once').toBe(2);
    expect(readsOf('team-B'), 'the side being written waits').toBe(1);

    await act(async () => { put.resolve({ positions: { jiho: 'Kai Mori' }, memberIds: { jiho: 'b2' } }); });
    await flush();
    expect(readsOf('team-B'), 'and is read once its write has settled').toBe(2);
    expect(readsOf('team-A')).toBe(2);
  });

  it('does not read a side again after a write when nothing was announced meanwhile', async () => {
    const { container } = await mountFivePerson();
    await typeName(jihoInput(container, 'shiro'), 'Kai Mori');
    expect([readsOf('team-A'), readsOf('team-B')]).toEqual([1, 1]);
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
