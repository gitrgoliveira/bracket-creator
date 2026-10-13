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
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

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
});
