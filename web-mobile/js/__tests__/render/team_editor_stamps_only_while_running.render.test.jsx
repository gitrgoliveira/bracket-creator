// The team sheet's bout-row and representative-pick edit stamps (lastRowEditRef,
// lastRepPickEditRef, bc-kclr) hold the operator's rows against a server copy stamped before
// the edit. Like the individual and engi editors' stamps they are taken only while the match
// is RUNNING, because only then is the edit autosaved and a landed save able to clear them,
// and they are cleared when the match leaves running. And a REFUSED autosave (nothing handed
// back, or applied:false) takes the server's current copy for the rows it carried: it stores
// and broadcasts nothing, so nothing else would bring the sheet back to what every other
// surface shows. A QUEUED answer keeps the sheet and the stamps: that write will land. See
// "The edit-time adopt rule" in .claude/rules/match-results-and-marks.md.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const SQUADS = {
  t1: [{ id: 'm1a', name: 'Alice', index: 1 }, { id: 'm2a', name: 'Brenda', index: 2 }],
  t2: [{ id: 'm1b', name: 'Carol', index: 1 }, { id: 'm2b', name: 'Dana', index: 2 }],
};

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1', config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
    fetchSquads: vi.fn().mockResolvedValue(SQUADS),
    fetchTeamLineup: vi.fn().mockResolvedValue({ saved: false, positions: {} }),
    fetchLineupInForce: vi.fn().mockResolvedValue({ saved: false, positions: {} }),
    fetchMatchHistory: vi.fn().mockResolvedValue([]),
    recordScore: vi.fn().mockResolvedValue(undefined),
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

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  await import('../../admin_lineup.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { vi.useRealTimers(); });

const REFUSED = { applied: false, reason: 'superseded' };
const QUEUED = { queued: true };

// A completed match is a CORRECTION: its rows are fixed-position, tappable from the start.
const kachinukiMatch = (subResults, modifiedAt = 0, status = 'running') => ({
  id: 'k1', compId: 'comp1', status, phase: 'pool', poolName: 'Pool 1', court: 'A', modifiedAt,
  compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: status === 'completed' ? 'fixed' : 'kachinuki',
  ...(status === 'completed' ? { winner: { id: 'team-A', name: 'Team A' } } : {}),
  sideA: { id: 'team-A', name: 'Team A' },
  sideB: { id: 'team-B', name: 'Team B' },
  subResults,
});
// The other device scored Shiro's K on bout 1; its write left the match's stamp at `modifiedAt`.
const otherDeviceScoredShiro = (modifiedAt, status = 'running') =>
  kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: ['K'] }], modifiedAt, status);

const strikeAkaMen = async () => {
  await act(async () => { fireEvent.keyDown(window, { key: 'M', shiftKey: true }); });
};
// A correction has no live bout for the keyboard to strike: tap Aka's M input on bout 1.
const tapAkaMenInput = async () => {
  const row = document.querySelector('.team-sub-match:not(.team-sub-match--readonly)');
  const men = [...row.querySelectorAll('.ipt-btn')].filter((b) => b.textContent === 'M');
  await act(async () => { fireEvent.click(men[men.length - 1]); });
};
async function tapMark(side, letter) {
  const mark = [...document.querySelectorAll(`.team-sub-match:not(.team-sub-match--readonly) .tsm-center-pts--${side} .editor-side__pt`)]
    .find((b) => b.textContent === letter);
  expect(mark, `a scored ${letter} on ${side}`).toBeTruthy();
  await act(async () => { fireEvent.click(mark); });
}
const filled = (side) => document.querySelectorAll(`.team-sub-match:not(.team-sub-match--readonly) .tsm-center-pts--${side} .editor-side__pt--filled`).length;
const settle = async () => { for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); }); };
const el = (match, onSubmit) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;

describe('the stamps are taken only while the match is running', () => {
  it.each(['scheduled', 'completed'])('a net-zero fiddle on a %s match holds nothing against another device', async (status) => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    let utils;
    await act(async () => { utils = render(el(kachinukiMatch([], 0, status), onSubmit)); });
    await (status === 'completed' ? tapAkaMenInput() : strikeAkaMen());
    await tapMark('aka', 'M');
    const tappedAt = Date.now();
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(onSubmit, 'a match that is not running is not autosaved').not.toHaveBeenCalled();

    await act(async () => { utils.rerender(el(otherDeviceScoredShiro(tappedAt - 1, status), onSubmit)); });
    expect(filled('shiro'), "the other device's point shows").toBe(1);
  });

  it('a stamp taken while running goes when the match leaves running inside the debounce', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    let utils;
    await act(async () => { utils = render(el(kachinukiMatch([]), onSubmit)); });
    await strikeAkaMen();
    await tapMark('aka', 'M');
    const tappedAt = Date.now();
    // Sent back to the queue elsewhere before the debounce fired: the write is dropped.
    await act(async () => { utils.rerender(el(kachinukiMatch([], 5, 'scheduled'), onSubmit)); });
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(onSubmit).not.toHaveBeenCalled();

    await act(async () => { utils.rerender(el(otherDeviceScoredShiro(tappedAt - 1, 'scheduled'), onSubmit)); });
    expect(filled('shiro'), "the other device's point shows").toBe(1);
  });
});

describe('a refused autosave takes the server\'s copy of the rows it carried', () => {
  // Aka strikes M; another device's copy (Shiro K, stamped before the tap) is held by the
  // stamp; the autosave then comes back with `answer`.
  async function strikeThenAnswer(answer) {
    const onSubmit = vi.fn().mockResolvedValue(answer);
    let utils;
    await act(async () => { utils = render(el(kachinukiMatch([]), onSubmit)); });
    await strikeAkaMen();
    const tappedAt = Date.now();
    expect(filled('aka'), 'precondition: the strike shows').toBe(1);
    await act(async () => { utils.rerender(el(otherDeviceScoredShiro(tappedAt - 1), onSubmit)); });
    expect([filled('aka'), filled('shiro')], 'precondition: the older copy is held').toEqual([1, 0]);
    await act(async () => { vi.advanceTimersByTime(400); });
    await settle();
    expect(onSubmit, 'precondition: the autosave went out').toHaveBeenCalled();
    return utils;
  }

  it('a superseded answer puts the server\'s point on the row and drops the operator\'s', async () => {
    await strikeThenAnswer(REFUSED);
    expect([filled('aka'), filled('shiro')]).toEqual([0, 1]);
  });

  it('a nothing-handed-back answer does the same', async () => {
    await strikeThenAnswer(undefined);
    expect([filled('aka'), filled('shiro')]).toEqual([0, 1]);
  });

  it('a queued answer keeps the sheet: that write will land', async () => {
    await strikeThenAnswer(QUEUED);
    expect([filled('aka'), filled('shiro')]).toEqual([1, 0]);
  });

  it('a clock_skew answer keeps the sheet: the tap is owed to the next save, not wiped', async () => {
    // recordScore keeps a running clock_skew refusal silent and relearns the clock.
    await strikeThenAnswer({ applied: false, reason: 'clock_skew' });
    expect([filled('aka'), filled('shiro')]).toEqual([1, 0]);
  });

  it('a strike made after the refused save was built is kept', async () => {
    let settleFirst;
    const onSubmit = vi.fn().mockImplementationOnce(() => new Promise((res) => { settleFirst = res; }))
      .mockResolvedValue({ applied: true });
    let utils;
    await act(async () => { utils = render(el(kachinukiMatch([]), onSubmit)); });
    await strikeAkaMen();
    const tappedAt = Date.now();
    await act(async () => { utils.rerender(el(otherDeviceScoredShiro(tappedAt - 1), onSubmit)); });
    await act(async () => { vi.advanceTimersByTime(400); }); // the first save is out and unanswered
    await strikeAkaMen(); // after it was built
    await act(async () => { settleFirst(REFUSED); });
    await settle();
    expect(filled('aka'), 'the newer edit is still owed').toBeGreaterThan(0);
  });

  // The representative bout (position -1) is taken PART by part: the server's row score for the
  // bout the save carried, and the pick of each side whose pick the save carried. The other
  // side's pick was not carried, so it stays the operator's (composeRepRow).
  describe('the representative bout', () => {
    const FOUGHT = [1, 2, 3].map((position) => ({
      position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake',
    }));
    const repRow = (extra) => ({
      position: -1, sideA: 'Team A', sideB: 'Team B', ipponsA: [], ipponsB: [], winner: '', decision: 'daihyosen', ...extra,
    });
    const repMatch = (row, modifiedAt = 0) => ({
      id: 'k-rep', compId: 'comp1', status: 'running', phase: 'knockout', court: 'A', modifiedAt,
      compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: 'fixed',
      sideA: { id: 't1', name: 'Team A' }, sideB: { id: 't2', name: 'Team B' },
      subResults: [...FOUGHT, row],
    });
    const dhInput = (side) => screen.queryAllByLabelText(/^Daihyosen (SHIRO|AKA) player$/)
      .find((e) => e.getAttribute('aria-label').includes(side));
    const repRowEl = () => dhInput('AKA').closest('.team-sub-match');
    const repFilled = (side) => repRowEl().querySelectorAll(`.tsm-center-pts--${side} .editor-side__pt--filled`).length;

    async function pickShiro(name) {
      await act(async () => { fireEvent.focus(dhInput('SHIRO')); fireEvent.click(dhInput('SHIRO')); });
      await settle();
      const opt = screen.getByText(name, { selector: '.pmf__opt-name' });
      await act(async () => { fireEvent.click(opt.closest('button')); });
      await settle();
    }

    async function repEditThenAnswer(answer) {
      const onSubmit = vi.fn().mockResolvedValue(answer);
      let utils;
      // Aka (t1) already holds Alice; the sheet starts level.
      const start = repMatch(repRow({ sideAMemberId: 'm1a' }));
      await act(async () => { utils = render(el(start, onSubmit)); });
      await settle();
      // Aka strikes M on the representative bout and Shiro's representative is picked: two edits.
      const men = [...repRowEl().querySelectorAll('.ipt-btn')].filter((b) => b.textContent === 'M');
      await act(async () => { fireEvent.click(men[men.length - 1]); });
      await pickShiro('Carol');
      const tappedAt = Date.now();
      expect([repFilled('aka'), dhInput('SHIRO').value, dhInput('AKA').value], 'precondition: both edits show')
        .toEqual([1, 'Carol', 'Alice']);
      // Another device's copy, stamped before the tap, so the stamps hold the operator's row: it
      // scored Shiro and picked Dana for Shiro; Aka's pick stays Alice (the side the save did not
      // carry: an unedited pick is followed by the ordinary adopt, so only a take that wrongly
      // reached it, or dropped the sheet's, would show here).
      const served = repMatch(repRow({ ipponsB: ['K'], sideAMemberId: 'm1a', sideBMemberId: 'm2b' }), tappedAt - 1);
      await act(async () => { utils.rerender(el(served, onSubmit)); });
      expect([repFilled('aka'), repFilled('shiro')], 'precondition: the older copy is held').toEqual([1, 0]);
      await act(async () => { vi.advanceTimersByTime(400); });
      await settle();
      expect(onSubmit, 'precondition: the autosave went out').toHaveBeenCalled();
      return onSubmit;
    }

    it.each([['a superseded answer', REFUSED], ['a nothing-handed-back answer', undefined]])(
      '%s takes the server\'s score and the picked side\'s pick, and keeps the other side\'s pick',
      async (_name, answer) => {
        const onSubmit = await repEditThenAnswer(answer);
        const sent = onSubmit.mock.calls[0][0].subResults.find((s) => s.position === -1);
        // The save carried Aka's point and Shiro's pick, never Aka's pick.
        expect(sent.ipponsA).toEqual(['M']);
        expect(sent.sideBMemberId).toBe('m1b');
        expect([repFilled('aka'), repFilled('shiro')], 'the server\'s score on the row').toEqual([0, 1]);
        expect(dhInput('SHIRO').value, 'the picked side shows the server\'s pick').toBe('Dana');
        expect(dhInput('AKA').value, 'the side the save did not carry keeps the operator\'s pick').toBe('Alice');
      });
  });
});
