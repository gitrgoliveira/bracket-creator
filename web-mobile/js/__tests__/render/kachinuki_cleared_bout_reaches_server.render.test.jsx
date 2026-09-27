// bc-kclr: a mark taken back on a kachinuki bout came back. The team editor's
// patch drops every kachinuki row subBoutHasBeenPlayed rejects (so untouched
// trailing positions never reach the wire), and a bout the operator cleared
// back to 0-0 is exactly such a row, so the write simply left it out; the
// server merge (mergeKachinukiSubResults) keeps any stored row a payload omits,
// so the stored point survived every save and came back on reload. A row the
// operator cleared, whose stored row still has a result, is now sent as an
// explicit empty row, which the merge stores; rows never played stay out.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
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

const kachinukiMatch = (subResults, modifiedAt = 0) => ({
  id: 'k1', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A', modifiedAt,
  compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: 'kachinuki',
  sideA: { id: 'team-A', name: 'Team A' },
  sideB: { id: 'team-B', name: 'Team B' },
  subResults,
});

async function mount(match) {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
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
  it('the autosave carries the cleared bout as an explicit empty row', async () => {
    const onSubmit = await mount(kachinukiMatch([
      { position: 1, sideA: 'Aka 1', sideB: 'Shiro 1', ipponsA: ['M'], ipponsB: [] },
    ]));
    await tapMark('aka', 'M');
    await act(async () => { vi.advanceTimersByTime(400); });

    expect(onSubmit).toHaveBeenCalled();
    const patch = onSubmit.mock.calls[onSubmit.mock.calls.length - 1][0];
    expect(patch.status).toBe('running');
    const bout1 = rowAt(patch, 1);
    expect(bout1, 'the cleared bout must be on the wire, or the stored point survives').toBeTruthy();
    expect(bout1.ipponsA).toEqual([]);
    expect(bout1.ipponsB).toEqual([]);
    expect(bout1.decision || '').toBe('');
    // Positions nobody ever played stay off the wire, as before.
    expect(rowAt(patch, 2)).toBeUndefined();
    expect(rowAt(patch, 3)).toBeUndefined();
  });

  // The case the first fix missed, found in the browser: the court feed lags
  // the editor, so when a point is struck and taken back within a moment,
  // this board never saw the point stored. The clear must be written anyway.
  const strikeAkaMen = async () => {
    await act(async () => { fireEvent.keyDown(window, { key: 'M', shiftKey: true }); });
  };
  const lastPatch = (onSubmit) => onSubmit.mock.calls[onSubmit.mock.calls.length - 1][0];

  it('writes the clear even when the court feed never showed the stored point', async () => {
    const onSubmit = await mount(kachinukiMatch([]));
    await strikeAkaMen();
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(rowAt(lastPatch(onSubmit), 1).ipponsA, 'precondition: the point was written').toEqual(['M']);

    await tapMark('aka', 'M');
    await act(async () => { vi.advanceTimersByTime(400); });
    const bout1 = rowAt(lastPatch(onSubmit), 1);
    expect(bout1, 'the bout being fought is always sent').toBeTruthy();
    expect(bout1.ipponsA).toEqual([]);
  });

  it('a lagging snapshot carrying the point does not put it back', async () => {
    // Each write is stamped when it is sent (server_clock.jsx's frame; the
    // offset is 0 here), and a snapshot of it carries that stamp.
    const sentAt = [];
    const onSubmit = vi.fn().mockImplementation(() => { sentAt.push(Date.now()); return Promise.resolve(undefined); });
    let utils;
    const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;
    await act(async () => { utils = render(el(kachinukiMatch([]))); });
    await strikeAkaMen();
    await act(async () => { vi.advanceTimersByTime(400); });
    await tapMark('aka', 'M');
    // The feed now delivers the snapshot of the EARLIER write, with the point:
    // written before the clear, so it cannot know about it.
    await act(async () => { vi.advanceTimersByTime(150); });
    await act(async () => {
      utils.rerender(el(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [] }], sentAt[0])));
    });
    const filled = document.querySelectorAll('.team-sub-match:not(.team-sub-match--readonly) .tsm-center-pts--aka .editor-side__pt--filled');
    expect(filled.length, 'the point taken back stays taken back').toBe(0);
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(rowAt(lastPatch(onSubmit), 1).ipponsA, 'and the write carries the clear, not the stale point').toEqual([]);
  });

  // Review finding: the guard used to be a time window, so a change another
  // device saved to the same bout right after the operator's edit was never
  // adopted. It is keyed on the snapshot's stamp now: written after the edit,
  // it is news, and a row the operator has put back where it was follows it.
  it('a change saved on another device after the edit is adopted', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    let utils;
    const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;
    await act(async () => { utils = render(el(kachinukiMatch([]))); });
    await strikeAkaMen();
    await act(async () => { vi.advanceTimersByTime(400); });
    await tapMark('aka', 'M');
    await act(async () => { vi.advanceTimersByTime(400); });
    // Another device records Shiro's K on bout 1, well inside the old window.
    await act(async () => {
      utils.rerender(el(kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: ['K'] }], Date.now())));
    });
    const shiroFilled = document.querySelectorAll('.team-sub-match:not(.team-sub-match--readonly) .tsm-center-pts--shiro .editor-side__pt--filled');
    expect(shiroFilled.length, "the other device's point shows").toBe(1);
  });

  // The close prompt existed because a cleared bout could not be flushed
  // (runningPatchDropsAnEdit). Now it can, so closing flushes it and asks
  // nothing.
  it('closing right after the clear flushes it with no discard prompt', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    await act(async () => {
      render(<ScoreEditorModal match={kachinukiMatch([
        { position: 1, sideA: 'Aka 1', sideB: 'Shiro 1', ipponsA: ['M'], ipponsB: [] },
      ])} onClose={onClose} onSubmit={onSubmit} password="" />);
    });
    await tapMark('aka', 'M');
    await act(async () => { fireEvent.click(screen.getByText('✕ Close')); });

    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(rowAt(onSubmit.mock.calls[0][0], 1).ipponsA).toEqual([]);
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
