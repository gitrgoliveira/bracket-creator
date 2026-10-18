// bc-kclr: a mark taken back on a kachinuki bout came back. The team editor's
// patch drops every kachinuki row subBoutHasBeenPlayed rejects (so untouched
// trailing positions never reach the wire), and a bout the operator cleared
// back to 0-0 is exactly such a row, so the write simply left it out; the
// server merge (mergeKachinukiSubResults) keeps any stored row a payload omits,
// so the stored point survived every save and came back on reload. A row the
// operator cleared, whose stored row still has a result, was then sent as an
// explicit empty row. Since bc-evts stage 5 a running match is scored by taps:
// taking a mark back is one ippon.remove naming the mark, which no merge can
// drop, and the board (the server's copy with the pending taps folded on)
// never shows a lagging copy's point over it.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
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
    recordScore: vi.fn().mockResolvedValue({ applied: true }),
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

const kachinukiMatch = (subResults, modifiedAt = 0) => ({
  id: 'k1', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A', modifiedAt,
  compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: 'kachinuki',
  sideA: { id: 'team-A', name: 'Team A' },
  sideB: { id: 'team-B', name: 'Team B' },
  subResults,
});

async function mount(match) {
  const onSubmit = vi.fn().mockResolvedValue({ applied: true });
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
  });
  return onSubmit;
}

// Tap the scored mark itself (the editor's "tap a scored mark to clear it").
async function tapMark(side, letter) {
  const mark = [...document.querySelectorAll(`.team-sub-match:not(.team-sub-match--readonly) .tsm-center-pts--${side} .editor-side__pt`)]
    .find((b) => b.textContent === letter);
  expect(mark, `a scored ${letter} on ${side}`).toBeTruthy();
  await act(async () => { fireEvent.click(mark); });
}

const rowAt = (patch, position) => (patch.subResults || []).find((s) => s.position === position);

describe('a mark taken back on a kachinuki bout reaches the server (bc-kclr)', () => {
  const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);
  const filled = (side) => document.querySelectorAll(`.team-sub-match:not(.team-sub-match--readonly) .tsm-center-pts--${side} .editor-side__pt--filled`).length;

  it('taking the mark back is one ippon.remove naming it, and nothing else is written', async () => {
    const onSubmit = await mount(kachinukiMatch([
      { position: 1, sideA: 'Aka 1', sideB: 'Shiro 1', ipponsA: ['M'], ipponsB: [], markIds: { a: ['srv-m'] } },
    ]));
    await tapMark('aka', 'M');
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(taps()).toEqual([{ kind: 'ippon.remove', pos: 1, side: 'a', target: 'srv-m' }]);
    expect(filled('aka')).toBe(0);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('a stored mark the copy names no id for is named as the server\'s first baseline names it', async () => {
    await mount(kachinukiMatch([
      { position: 1, sideA: 'Aka 1', sideB: 'Shiro 1', ipponsA: ['M'], ipponsB: [] },
    ]));
    await tapMark('aka', 'M');
    expect(taps()).toEqual([{ kind: 'ippon.remove', pos: 1, side: 'a', target: 'b:1:a:M:0' }]);
    expect(filled('aka')).toBe(0);
  });

  // The court feed lags the editor: a point struck and taken back within a
  // moment was never seen stored by this board. The removal names the tap's
  // own mark, and the bout's row (which the server does not hold yet) is written
  // ahead of both.
  const strikeAkaMen = async () => {
    await act(async () => { fireEvent.keyDown(window, { key: 'M', shiftKey: true }); });
  };

  it('a point struck and taken back on a first bout the server never held: the two taps, and no row write', async () => {
    const onSubmit = await mount(kachinukiMatch([]));
    await strikeAkaMen();
    const { id } = window.API.recordTap.mock.results[0].value;
    await tapMark('aka', 'M');
    expect(taps()).toEqual([
      { kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' },
      { kind: 'ippon.remove', pos: 1, side: 'a', target: id },
    ]);
    // The server makes the first bout's row at its first tap (stage 6).
    expect(onSubmit, 'no row write ahead of the taps').not.toHaveBeenCalled();
    expect(filled('aka')).toBe(0);
  });

  it('a lagging copy carrying the point does not put it back', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    let utils;
    const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;
    await act(async () => { utils = render(el(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [] }]))); });
    await strikeAkaMen();
    const { id } = window.API.recordTap.mock.results[0].value;
    await tapMark('aka', 'M');
    // The feed now delivers the copy the server made of the strike alone: the
    // removal is still pending, so it folds over that copy.
    await act(async () => {
      utils.rerender(el({ ...kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: '', markIds: { a: [id] } }]), eventSeqs: { dev1: 1 } }));
    });
    expect(filled('aka'), 'the point taken back stays taken back').toBe(0);
  });

  it('a change saved on another device after the edit shows', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    let utils;
    const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;
    await act(async () => { utils = render(el(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: [] }]))); });
    await strikeAkaMen();
    await tapMark('aka', 'M');
    // Another device records Shiro's K on bout 1.
    await act(async () => {
      utils.rerender(el(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: ['K'] }], Date.now())));
    });
    expect(filled('shiro'), "the other device's point shows").toBe(1);
    expect(filled('aka')).toBe(0);
  });

  // The close prompt existed because a cleared bout could not be flushed. Now
  // its removal went as it was made, so closing asks nothing and owes nothing.
  it('closing right after the clear asks nothing and writes nothing more', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    let view;
    const onClose = vi.fn(() => view.unmount());
    await act(async () => {
      view = render(<ScoreEditorModal match={kachinukiMatch([
        { position: 1, sideA: 'Aka 1', sideB: 'Shiro 1', ipponsA: ['M'], ipponsB: [], markIds: { a: ['srv-m'] } },
      ])} onClose={onClose} onSubmit={onSubmit} password="" />);
    });
    await tapMark('aka', 'M');
    await act(async () => { fireEvent.click(screen.getByText('✕ Close')); });

    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(taps()).toEqual([{ kind: 'ippon.remove', pos: 1, side: 'a', target: 'srv-m' }]);
  });

  it('End match after clearing the current bout stores no stale mark on it', async () => {
    const onSubmit = await mount(kachinukiMatch([
      { position: 1, sideA: 'Aka 1', sideB: 'Shiro 1', ipponsA: ['M', 'M'], ipponsB: [], winner: 'Aka 1' },
      { position: 2, sideA: 'Aka 1', sideB: 'Shiro 2', ipponsA: [], ipponsB: ['K'] },
    ]));
    await tapMark('shiro', 'K');
    const end = document.querySelector('[data-testid="kachinuki-end-match-button"]');
    expect(end).toBeTruthy();
    await act(async () => { fireEvent.click(end); });
    await act(async () => { fireEvent.click(end); });
    await act(async () => { vi.advanceTimersByTime(400); });

    const completed = onSubmit.mock.calls.map((c) => c[0]).find((p) => p.status === 'completed');
    expect(completed, 'End match wrote the encounter').toBeTruthy();
    const bout2 = rowAt(completed, 2);
    expect(bout2, 'the cleared current bout is sent, so the server drops it as unscored').toBeTruthy();
    expect(bout2.ipponsB).toEqual([]);
  });
});

// bc-evts stage 5: a running team match is scored by taps handed to the outbox
// (API.recordTap); each test gets a fresh fake of that surface, whose pending
// taps the board folds on. Registered last, so it lands on the API the hooks
// above set up.
beforeEach(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });
