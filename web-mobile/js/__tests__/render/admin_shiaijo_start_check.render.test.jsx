// bc-stpf: a pick of another bout checks the start before it sends the running
// bout back (operator ruling 2026-10-12, option A), and asks the operator when
// it cannot check (Q1, 2026-10-27).
//
// m1 runs on Shiaijo A; the Up next card holds m2. Starting m2 asks
// API.checkStart first: a refusal leaves m1 running and shows the reason on m2
// with one toast; an acceptance keeps today's path (send back, then the start
// queued `after` it); no answer, offline or unsent writes ask the operator,
// whose two answers either take that same path or send nothing.

import React from 'react';
import { render, act, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />,
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    deleteAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    revertMatchToQueue: vi.fn(),
    checkStart: vi.fn(),
    startCheckUntrusted: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
  },
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  compMatches: () => [],
};

let restoreGlobals;
let AdminShiaijoPage;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});

afterAll(() => restoreGlobals());

const REVERT_KEY = 'c1:m1\u001fsendback';

beforeEach(() => {
  window.API.revertMatchToQueue = vi.fn(async () => ({ applied: true, pending: true, key: REVERT_KEY, settled: Promise.resolve(true) }));
  window.API.checkStart = vi.fn(async () => ({ ok: true }));
  window.API.startCheckUntrusted = vi.fn(() => false);
});

afterEach(() => {
  cleanup();
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
});

const base = { compId: 'c1', court: 'A', status: 'scheduled', phase: 'pool', poolName: 'Pool 1' };
const matches = (firstStatus = 'running') => [
  { ...base, id: 'm1', status: firstStatus, scoreVersion: 4, ipponsA: ['M'], sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } },
  { ...base, id: 'm2', sideA: { id: 'p3', name: 'Suzuki' }, sideB: { id: 'p4', name: 'Sato' } },
  { ...base, id: 'm3', sideA: { id: 'p5', name: 'Ito' }, sideB: { id: 'p6', name: 'Kato' } },
];

function renderConsole(list = matches()) {
  window.tournamentMatches = () => list;
  window.filterMatchesByCourt = (ms) => ms;
  const onEditScore = vi.fn().mockResolvedValue({ applied: true, status: 'running' });
  const showToast = vi.fn();
  render(
    <AdminShiaijoPage
      tournament={{ name: 'Test Tournament', courts: ['A', 'B'], competitions: [] }}
      court="A"
      onBack={vi.fn()}
      onEditScore={onEditScore}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password="pw"
      showToast={showToast}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />,
  );
  return { onEditScore, showToast };
}

// The start writes the console made, as [matchId, patch].
const starts = (onEditScore) => onEditScore.mock.calls.filter((c) => c[2] && c[2].status === 'running').map((c) => [c[1], c[2]]);
const upNextStart = () => screen.getAllByRole('button', { name: 'Start match' })[0];
const settle = () => act(async () => { await Promise.resolve(); await Promise.resolve(); });
const errorToasts = (showToast) => showToast.mock.calls.filter((c) => c[1] === 'error');

describe('a pick of another bout checks the start before it sends the running bout back (bc-stpf)', () => {
  it('asks the server about the picked match on this court, with the password', async () => {
    renderConsole();
    await pointerTap(upNextStart());
    await settle();
    expect(window.API.checkStart).toHaveBeenCalledTimes(1);
    expect(window.API.checkStart).toHaveBeenCalledWith('c1', 'm2', 'A', 'pw');
  });

  it('a refused start sends nothing back, starts nothing and shows the reason on the picked match with one toast', async () => {
    const sentence = 'Suzuki is fighting now in Pool 1 · Match 9 on Shiaijo B. Finish that match first.';
    window.API.checkStart = vi.fn(async () => ({ refused: new Error(sentence) }));
    const { onEditScore, showToast } = renderConsole();
    await pointerTap(upNextStart());
    await settle();

    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    expect(starts(onEditScore)).toEqual([]);
    expect(errorToasts(showToast)).toEqual([[sentence, 'error']]);
    expect(screen.getAllByText(sentence).length).toBeGreaterThan(0);
    // The running bout keeps the live slot and the editor.
    expect(screen.getByTestId('score-editor').dataset.match).toBe('m1');
    expect(screen.queryByTestId('start-ask')).toBeNull();
  });

  it('a pick no longer waiting in the queue is refused the same way', async () => {
    const sentence = 'This match has already started or finished on another device. Check the court\'s list.';
    window.API.checkStart = vi.fn(async () => ({ refused: new Error(sentence) }));
    const { onEditScore, showToast } = renderConsole();
    await pointerTap(upNextStart());
    await settle();

    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    expect(starts(onEditScore)).toEqual([]);
    expect(errorToasts(showToast)).toEqual([[sentence, 'error']]);
    expect(screen.getAllByText(sentence).length).toBeGreaterThan(0);
    expect(screen.getByTestId('score-editor').dataset.match).toBe('m1');
  });

  it('an accepted start sends the running bout back with its score, then queues the start after it', async () => {
    const { onEditScore } = renderConsole();
    await pointerTap(upNextStart());
    await settle();

    expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
    const [comp, match, , opts] = window.API.revertMatchToQueue.mock.calls[0];
    expect([comp, match]).toEqual(['c1', 'm1']);
    // The bout as shown, so a display on this machine shows the send back at once.
    expect(opts.match).toEqual(expect.objectContaining({ id: 'm1', court: 'A' }));
    const started = starts(onEditScore);
    expect(started.map(([id]) => id)).toEqual(['m2']);
    expect(started[0][1].after).toBe(REVERT_KEY);
    expect(window.API.checkStart.mock.invocationCallOrder[0])
      .toBeLessThan(window.API.revertMatchToQueue.mock.invocationCallOrder[0]);
    expect(window.API.revertMatchToQueue.mock.invocationCallOrder[0])
      .toBeLessThan(onEditScore.mock.invocationCallOrder[0]);
  });

  describe('with no answer the operator is asked', () => {
    beforeEach(() => { window.API.checkStart = vi.fn(async () => ({ unknown: true })); });

    it('names the running bout and the picked match, and sends nothing until answered', async () => {
      const { onEditScore } = renderConsole();
      await pointerTap(upNextStart());
      await settle();
      const ask = screen.getByTestId('start-ask');
      expect(ask.textContent).toMatch(/could not check/i);
      expect(ask.textContent).toMatch(/Yamada/);
      expect(ask.textContent).toMatch(/Suzuki/);
      expect(ask.textContent).not.toMatch(/default win|squad/i);
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(starts(onEditScore)).toEqual([]);
    });

    it('"Send back and start" sends the running bout back, then queues the start after it', async () => {
      const { onEditScore } = renderConsole();
      await pointerTap(upNextStart());
      await settle();
      await keyboardClick(screen.getByRole('button', { name: 'Send back and start' }));
      await settle();

      expect(screen.queryByTestId('start-ask')).toBeNull();
      expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
      expect(window.API.revertMatchToQueue.mock.calls[0].slice(0, 2)).toEqual(['c1', 'm1']);
      const started = starts(onEditScore);
      expect(started.map(([id]) => id)).toEqual(['m2']);
      expect(started[0][1].after).toBe(REVERT_KEY);
    });

    it('"Keep it running" sends nothing, keeps the running bout live and clears the pick', async () => {
      const { onEditScore } = renderConsole();
      await pointerTap(upNextStart());
      await settle();
      await keyboardClick(screen.getByRole('button', { name: 'Keep it running' }));
      await settle();

      expect(screen.queryByTestId('start-ask')).toBeNull();
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(starts(onEditScore)).toEqual([]);
      expect(screen.getByTestId('score-editor').dataset.match).toBe('m1');
      // The pick is cleared: Start match is offered again and a new tap checks
      // again (a key press, which the tap guard never reads as a bounce).
      const again = upNextStart();
      expect(again.disabled).toBe(false);
      await keyboardClick(again);
      await settle();
      expect(window.API.checkStart).toHaveBeenCalledTimes(2);
    });

    it('Escape is "keep it running"', async () => {
      const { onEditScore } = renderConsole();
      await pointerTap(upNextStart());
      await settle();
      await act(async () => { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); });
      await settle();
      expect(screen.queryByTestId('start-ask')).toBeNull();
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(starts(onEditScore)).toEqual([]);
    });
  });

  it('offline or with unsent writes it asks at once, without a check', async () => {
    window.API.startCheckUntrusted = vi.fn(() => true);
    renderConsole();
    await pointerTap(upNextStart());
    await settle();
    expect(window.API.startCheckUntrusted).toHaveBeenCalledWith('c1');
    expect(window.API.checkStart).not.toHaveBeenCalled();
    expect(screen.getByTestId('start-ask')).toBeTruthy();
    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
  });

  it('with no running bout nothing is checked or asked', async () => {
    const { onEditScore } = renderConsole(matches('scheduled'));
    await pointerTap(upNextStart());
    await settle();
    expect(window.API.checkStart).not.toHaveBeenCalled();
    expect(window.API.startCheckUntrusted).not.toHaveBeenCalled();
    expect(screen.queryByTestId('start-ask')).toBeNull();
    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    expect(starts(onEditScore).map(([id]) => id)).toEqual(['m1']);
  });

  it('when the running bout is the picked match (isSame) no check is made and no question appears', async () => {
    const sameList = [
      { ...base, id: 'm1', status: 'running', scoreVersion: 4, ipponsA: ['M'], sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } },
      { ...base, id: 'm1', status: 'scheduled', scoreVersion: 4, sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } },
      { ...base, id: 'm2', sideA: { id: 'p3', name: 'Suzuki' }, sideB: { id: 'p4', name: 'Sato' } },
    ];
    const { onEditScore } = renderConsole(sameList);
    await pointerTap(upNextStart());
    await settle();

    expect(window.API.checkStart).not.toHaveBeenCalled();
    expect(screen.queryByTestId('start-ask')).toBeNull();
    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    const started = starts(onEditScore);
    expect(started.map(([id]) => id)).toEqual(['m1']);
    expect(started[0][1].after).toBeUndefined();
    expect(screen.getByTestId('score-editor').dataset.match).toBe('m1');
  });

  it('a second Start while the check is out is refused as in flight', async () => {
    let answer;
    window.API.checkStart = vi.fn(() => new Promise((resolve) => { answer = resolve; }));
    const { onEditScore, showToast } = renderConsole();
    await pointerTap(upNextStart());
    await settle();
    const others = screen.getAllByRole('button', { name: 'Start match' });
    await keyboardClick(others[others.length - 1]);
    await settle();
    expect(window.API.checkStart).toHaveBeenCalledTimes(1);
    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    expect(errorToasts(showToast).length).toBe(1);

    await act(async () => { answer({ ok: true }); });
    await settle();
    expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
    expect(starts(onEditScore).map(([id]) => id)).toEqual(['m2']);
  });

  it('Send back to queue tapped while the check is out opens no confirm, and the pick sends the bout back once', async () => {
    let answer;
    window.API.checkStart = vi.fn(() => new Promise((resolve) => { answer = resolve; }));
    const { onEditScore } = renderConsole();
    await pointerTap(upNextStart());
    await settle();
    await keyboardClick(screen.getByRole('button', { name: 'Send back to queue' }));
    await settle();
    expect(screen.queryByRole('dialog')).toBeNull();

    await act(async () => { answer({ ok: true }); });
    await settle();
    expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
    expect(starts(onEditScore).map(([id]) => id)).toEqual(['m2']);
  });

  it('a second Start while the question is open is refused as in flight', async () => {
    window.API.checkStart = vi.fn(async () => ({ unknown: true }));
    const { onEditScore } = renderConsole();
    await pointerTap(upNextStart());
    await settle();
    expect(screen.getByTestId('start-ask')).toBeTruthy();
    const others = screen.getAllByRole('button', { name: 'Start match' });
    await keyboardClick(others[others.length - 1]);
    await settle();
    expect(window.API.checkStart).toHaveBeenCalledTimes(1);
    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    expect(starts(onEditScore)).toEqual([]);
  });
});
