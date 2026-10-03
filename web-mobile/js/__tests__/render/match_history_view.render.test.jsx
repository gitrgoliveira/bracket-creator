// bc-mrgc: the score editors' History disclosure. Closed by default, fetched
// when opened, one line per write in the order the server answers (oldest
// change first), and a change kept rather than applied shown with a short
// readable form of its value. It changes nothing in the editor and is not on
// a self-run participant's sheet.

import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const at = (h, m, s) => new Date(2026, 9, 3, h, m, s).getTime();

const ENTRIES = [
  { matchId: 'm-ind', door: 'score', stamp: at(10, 0, 1), receivedAt: at(10, 0, 1), changed: ['result'], outcomes: { result: 'applied' } },
  {
    matchId: 'm-ind', door: 'score', stamp: at(10, 0, 5), receivedAt: at(10, 3, 0), changed: ['points', 'bout:2'],
    outcomes: { points: 'held', 'bout:2': 'held' },
    held: {
      points: { ipponsA: ['H'], ipponsB: ['M', 'K'], hansokuA: 0, hansokuB: 1 },
      'bout:2': { position: 2, ipponsA: [], ipponsB: ['M'], winner: 'Osaka', decision: '' },
    },
  },
  {
    matchId: 'm-ind', door: 'decision', stamp: at(10, 1, 0), receivedAt: at(10, 1, 0), changed: ['result'], outcomes: { result: 'held' },
    held: { result: { status: 'completed', winner: 'Yamada', decision: 'kiken-voluntary', decisionBy: 'aka' } },
  },
];

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    notePendingEdit: vi.fn(),
    fetchMatchHistory: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;
let view;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
  view = await import('../../match_history_view.jsx');
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.API.fetchMatchHistory.mockReset();
  window.API.fetchMatchHistory.mockResolvedValue(ENTRIES);
  window.API.recordScore.mockClear();
});
afterEach(() => cleanup());

const match = (over = {}) => ({
  id: 'm-ind', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ...over,
});

async function mountEditor(props = {}) {
  await act(async () => {
    render(<ScoreEditorModal match={match()} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" {...props} />);
  });
}
const openHistory = () => act(async () => { fireEvent.click(screen.getByTestId('match-history-toggle')); });

describe('the History disclosure in the score editor', () => {
  it('is closed by default and fetches nothing until opened', async () => {
    await mountEditor();
    expect(screen.getByTestId('match-history')).toBeTruthy();
    expect(screen.queryByTestId('match-history-body')).toBeNull();
    expect(window.API.fetchMatchHistory).not.toHaveBeenCalled();
  });

  it('fetches on open and lists every write in the order answered, held values readable', async () => {
    await mountEditor();
    await openHistory();
    expect(window.API.fetchMatchHistory).toHaveBeenCalledWith('comp1', 'm-ind', 'pw');
    const rows = screen.getAllByTestId('match-history-entry').map((li) => li.textContent);
    expect(rows).toHaveLength(3);
    expect(rows[0]).toBe('10:00:01 Score sheet saved, changed the result');
    expect(rows[1]).toBe(
      '10:00:05 Score sheet saved, changed points, bout 2'
      + 'Kept in history: points, MK vs H, fouls Shiro 1, Aka 0'
      + 'Kept in history: bout 2, M vs –');
    expect(rows[2]).toBe('10:01:00 Decision recorded, changed the result'
      + 'Kept in history: the result, Kiken – Voluntary by Aka');
  });

  it('opening it changes nothing in the editor: no write, no unsaved change', async () => {
    const onClose = vi.fn();
    await mountEditor({ onClose, match: match({ status: 'completed', ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' } }) });
    await openHistory();
    expect(window.API.recordScore).not.toHaveBeenCalled();
    // Closing an untouched completed match asks nothing.
    await act(async () => { fireEvent.click(screen.getByText('✕ Close')); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  });

  it('is not on a self-run participant\'s sheet', async () => {
    await mountEditor({ selfReport: true, password: '' });
    expect(screen.queryByTestId('match-history')).toBeNull();
  });

  it('says so when there is nothing yet', async () => {
    window.API.fetchMatchHistory.mockResolvedValue([]);
    await mountEditor();
    await openHistory();
    expect(screen.getByTestId('match-history-body').textContent).toBe('Nothing recorded for this match yet.');
  });
});

describe('heldValueText', () => {
  it.each([
    ['encho', { periodCount: 2 }, 'overtime'],
    ['encho', null, 'no overtime'],
    ['flags', { flagsA: 1, flagsB: 2 }, 'flags Shiro 2, Aka 1'],
    ['result', { status: 'completed', decision: 'hikiwake' }, 'draw'],
    ['result', { status: 'completed', decision: 'fusensho', decisionBy: 'shiro' }, 'Default win (fusensho) against Shiro'],
    ['result', { status: 'completed', winner: 'Tanaka' }, 'Tanaka won'],
    ['bout:-1', null, 'no bout'],
  ])('%s %j reads "%s"', (group, value, words) => {
    expect(view.heldValueText(group, value)).toBe(words);
  });
});

// bc-mrgc phase 3: a write kept whole by one rule (the server's rev guard:
// an older revision of this board) says why, in the server's own words.
describe('historyEntryView: the reason a whole write was kept', () => {
  it('shows the reason the server recorded', () => {
    const v = view.historyEntryView({
      matchId: 'm-ind', door: 'score', stamp: at(10, 2, 0), receivedAt: at(10, 4, 0), changed: ['points'],
      outcomes: { points: 'held' }, held: { points: { ipponsA: ['K'], ipponsB: [], hansokuA: 0, hansokuB: 0 } },
      reason: 'older revision of this board',
    });
    expect(v.reason).toBe('Not applied: older revision of this board');
    expect(v.held).toHaveLength(1);
  });

  it('shows none for an entry judged group by group', () => {
    expect(view.historyEntryView(ENTRIES[1]).reason).toBeNull();
  });

  it('lists no held line for a group the server recorded as unchanged (an echo)', () => {
    const v = view.historyEntryView({
      matchId: 'm-ind', door: 'decision', stamp: at(10, 1, 0), receivedAt: at(10, 1, 0),
      changed: ['result', 'points', 'encho'], outcomes: { result: 'held', points: 'held', encho: 'unchanged' },
      held: { result: { status: 'completed', decision: 'kiken-voluntary', decisionBy: 'aka', winner: 'Yamada' }, points: { ipponsA: [], ipponsB: ['○', '○'], hansokuA: 0, hansokuB: 0 } },
    });
    expect(v.held.map((h) => h.group)).toEqual(['result', 'points']);
    expect(v.held.map((h) => h.text).join(' ')).not.toMatch(/overtime/);
  });
});
