// A bout row's edit stamp (lastRowEditRef, bc-kclr) holds the operator's row against a
// server copy stamped before the tap. It stands only while an edit is unsent: a point
// struck and taken back inside the autosave debounce changes nothing on the server, so
// the write does not name the bout and nothing ever moves the match's stamp past the tap
// (the client stamps a write `now - age`). Another device whose clock runs a little
// behind then scores the same bout, its write applies, the match's modifiedAt stays
// below the tap stamp, and the sheet kept the operator's row forever while every other
// surface showed the other device's. Once a save built AFTER the row's latest edit has
// landed, the sheet holds nothing the server lacks, so the stamp is cleared and the
// server's copy is followed again. The individual editor does the same with its board
// stamp (individual_editor_tap_back_kept.render.test.jsx, case 4).

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

const kachinukiMatch = (subResults, modifiedAt = 0) => ({
  id: 'k1', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A', modifiedAt,
  compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: 'kachinuki',
  sideA: { id: 'team-A', name: 'Team A' },
  sideB: { id: 'team-B', name: 'Team B' },
  subResults,
});

const strikeAkaMen = async () => {
  await act(async () => { fireEvent.keyDown(window, { key: 'M', shiftKey: true }); });
};
async function tapMark(side, letter) {
  const mark = [...document.querySelectorAll(`.team-sub-match:not(.team-sub-match--readonly) .tsm-center-pts--${side} .editor-side__pt`)]
    .find((b) => b.textContent === letter);
  expect(mark, `a scored ${letter} on ${side}`).toBeTruthy();
  await act(async () => { fireEvent.click(mark); });
}
const filled = (side) => document.querySelectorAll(`.team-sub-match:not(.team-sub-match--readonly) .tsm-center-pts--${side} .editor-side__pt--filled`).length;

// The other device scored Shiro's K on bout 1; its write left the match's stamp at `modifiedAt`.
const otherDeviceScoredShiro = (modifiedAt) =>
  kachinukiMatch([{ position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: ['K'] }], modifiedAt);

describe('a bout row stamp stands only while an edit is unsent', () => {
  it('follows a server copy stamped below the tap once the autosave built after it has landed', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    let utils;
    const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;
    await act(async () => { utils = render(el(kachinukiMatch([]))); });
    // Strike and take back inside the debounce: the sheet returns to what the server shows.
    await strikeAkaMen();
    await tapMark('aka', 'M');
    const tappedAt = Date.now();
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(onSubmit, 'the autosave was sent').toHaveBeenCalled();

    await act(async () => { utils.rerender(el(otherDeviceScoredShiro(tappedAt - 1))); });
    expect(filled('shiro'), "the other device's point shows").toBe(1);
  });

  it('keeps the row while an edit made after the landed save is still owed', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true });
    let utils;
    const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;
    await act(async () => { utils = render(el(kachinukiMatch([]))); });
    await strikeAkaMen();
    await tapMark('aka', 'M');
    await act(async () => { vi.advanceTimersByTime(400); });
    // A new tap, still inside its debounce: the server cannot know it.
    await strikeAkaMen();
    const tappedAt = Date.now();

    await act(async () => { utils.rerender(el(otherDeviceScoredShiro(tappedAt - 1))); });
    expect(filled('aka'), 'the unsent point stays').toBe(1);
    expect(filled('shiro'), 'and the older copy does not replace the row').toBe(0);
  });

  it('keeps the row when the save that landed was built before the latest tap', async () => {
    let settle;
    const onSubmit = vi.fn().mockImplementationOnce(() => new Promise((r) => { settle = r; }))
      .mockResolvedValue({ applied: true });
    let utils;
    const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;
    await act(async () => { utils = render(el(kachinukiMatch([]))); });
    await strikeAkaMen();
    await tapMark('aka', 'M');
    await act(async () => { vi.advanceTimersByTime(400); });
    expect(onSubmit, 'the first save is out and unanswered').toHaveBeenCalledTimes(1);
    // A tap made while that save is out, then the save lands.
    await strikeAkaMen();
    const tappedAt = Date.now();
    await act(async () => { settle({ applied: true }); });

    await act(async () => { utils.rerender(el(otherDeviceScoredShiro(tappedAt - 1))); });
    expect(filled('aka'), 'the tap the landed save never carried stays').toBe(1);
    expect(filled('shiro')).toBe(0);
  });

  it('keeps the row when the save was refused', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: false, reason: 'superseded' });
    let utils;
    const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />;
    await act(async () => { utils = render(el(kachinukiMatch([]))); });
    await strikeAkaMen();
    await tapMark('aka', 'M');
    const tappedAt = Date.now();
    await act(async () => { vi.advanceTimersByTime(400); });

    await act(async () => { utils.rerender(el(otherDeviceScoredShiro(tappedAt - 1))); });
    expect(filled('shiro'), 'a refused save landed nothing, so the stamp stands').toBe(0);
  });
});
