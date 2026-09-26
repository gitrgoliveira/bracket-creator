// bc-fsnp: a per-bout fusensho keeps the points the other side had already
// struck (FIK Art. 32; recording-decisions.md: "Any point the withdrawing side
// had already scored stays valid"), and undoing it after a reload drops the
// default-win circles without touching those points.
//
// It used to REPLACE the scoreline: the other side's strikes were erased, and
// after a reload (the editor does not carry the pre-fusensho snapshot) the
// undo left the two circles behind as ordinary points.

import React from 'react';
import { render, act, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';
import { DEFAULT_WIN_IPPON } from '../../result_slot.jsx';

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

beforeEach(() => {
  window.API.recordScore.mockClear();
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const CIRCLES = [DEFAULT_WIN_IPPON, DEFAULT_WIN_IPPON];

function makeMatch(subResults = []) {
  return {
    id: 'tm-fus',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto' },
    sideB: { id: 'team-osaka', name: 'Osaka' },
    subResults,
  };
}

async function mount(match) {
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={(p) => window.API.recordScore('comp1', match.id, p, '', match)} password="" />);
  });
}

// Bout 1's side panel. Shiro (b, ipponsB) is the left side, Aka (a, ipponsA)
// the right one.
const bout1Side = (color) => document.querySelectorAll('.team-sub-match')[0].querySelector(`.team-sub-match__side--${color}`);
const ipponButton = (color, letter) => [...bout1Side(color).querySelectorAll('button.ipt-btn')].find((b) => b.textContent === letter);
const fusenshoButton = (color) => bout1Side(color).querySelector('[data-testid="scoring-modal-fusensho-button"]');
// The mark slots sit in the centre column, each side's pair beside the middle.
const markSlots = (color) => [...document.querySelectorAll('.team-sub-match')[0].querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)];

async function tap(el) {
  await act(async () => { fireEvent.click(el); });
}

async function settle() {
  await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
}

function lastBout1() {
  const calls = window.API.recordScore.mock.calls;
  expect(calls.length).toBeGreaterThan(0);
  const [, , patch] = calls[calls.length - 1];
  return (patch.subResults || []).find((s) => s.position === 1);
}

describe('bc-fsnp: a per-bout fusensho keeps the other side\'s struck points', () => {
  it('writes the circles for the winner and keeps the loser\'s kote', async () => {
    await mount(makeMatch());
    await tap(ipponButton('shiro', 'K'));
    await settle();
    await tap(fusenshoButton('aka'));
    await settle();
    const row = lastBout1();
    expect(row.decision).toBe('fusensho');
    expect(row.ipponsA).toEqual(CIRCLES);
    expect(row.ipponsB).toEqual(['K']);
  });

  it('after a reload, the undo drops the circles and keeps the kote', async () => {
    // The row as the server stores it after the write above. The remounted
    // editor seeds fusensho from it but has no pre-fusensho snapshot.
    await mount(makeMatch([
      { position: 1, sideA: '', sideB: '', ipponsA: CIRCLES, ipponsB: ['K'], winner: 'Kyoto', decision: 'fusensho' },
    ]));
    expect(fusenshoButton('aka').textContent).toContain('✓ Fusensho');
    await tap(fusenshoButton('aka'));
    await settle();
    const row = lastBout1();
    expect(row.decision || '').not.toBe('fusensho');
    expect(row.ipponsA).toEqual([]);
    expect(row.ipponsB).toEqual(['K']);
  });

  it('correcting the loser\'s point ends the fusensho without leaving circles behind', async () => {
    await mount(makeMatch());
    await tap(ipponButton('shiro', 'K'));
    await settle();
    await tap(fusenshoButton('aka'));
    await settle();
    // Tap Shiro's K mark to remove it, the operator's way to fix a point.
    const kSlot = markSlots('shiro').find((b) => b.textContent === 'K');
    await tap(kSlot);
    await settle();
    const row = lastBout1();
    expect(row.decision || '').toBe('');
    expect(row.ipponsA).toEqual([]);
    expect(row.ipponsB).toEqual([]);
  });

  it('refuses a fusensho against a side that already won the bout, and sends nothing', async () => {
    await mount(makeMatch());
    await tap(ipponButton('shiro', 'M'));
    await tap(ipponButton('shiro', 'K'));
    await settle();
    const aka = fusenshoButton('aka');
    expect(aka.disabled).toBe(true);
    expect(aka.title).toMatch(/SHIRO already won this bout/);
    window.API.recordScore.mockClear();
    await tap(aka);
    await settle();
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });
});
