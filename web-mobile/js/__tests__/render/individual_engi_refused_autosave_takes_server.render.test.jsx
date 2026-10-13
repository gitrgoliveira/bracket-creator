// A REFUSED autosave leaves the editor's edit stamp set (individual board stamp, engi flags
// stamp), so the board kept the operator's value while the server and every other surface
// showed the server's, and nothing re-adopted: a refusal broadcasts nothing. The editor now
// takes the server's CURRENT copy when the refusal arrives (landedAutosave's onRefused) and
// clears the stamp; a QUEUED answer keeps the board and the stamp, since that write will
// land later. See "The edit-time adopt rule" in .claude/rules/match-results-and-marks.md.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { EngiScoreEditorModal } from '../../admin_scoring_engi.jsx';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn(),
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

const NOW = 1_800_000_000_000;
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(NOW); });
afterEach(() => { vi.useRealTimers(); });

const REFUSED = { applied: false, reason: 'superseded' };
const QUEUED = { queued: true };

const tap = async (node) => { await act(async () => { fireEvent.click(node); }); };
const wait = async (ms) => { await act(async () => { vi.advanceTimersByTime(ms); }); };
// The save's answer is read in a microtask after the timer fires.
const settle = async () => { for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); }); };

describe('individual editor: a refused autosave takes the server\'s copy', () => {
  const bout = (overrides = {}) => ({
    id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
    ipponsA: [], ipponsB: [], modifiedAt: NOW - 5000,
    ...overrides,
  });
  const el = (match, onSubmit) => (
    <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" variant="inline" />
  );
  const marks = (c) => [...c.querySelectorAll('[aria-label*=" slot "]')]
    .map((s) => s.getAttribute('aria-label'))
    .filter((l) => /: remove /.test(l));
  const menButton = (side) => [...side.querySelectorAll('.ipt-btn')].find((b) => b.textContent.trim() === 'M');

  // Aka taps M; the server's copy (another device's Shiro M, stamped before the tap) is held
  // by the stamp; the autosave then comes back with `answer`.
  async function tapThenAnswer(answer) {
    const onSubmit = vi.fn().mockResolvedValue(answer);
    let r;
    await act(async () => { r = render(el(bout(), onSubmit)); });
    await tap(menButton(r.container.querySelector('.sb-side--aka')));
    expect(marks(r.container), 'precondition: the tap shows').toHaveLength(1);
    await act(async () => { r.rerender(el(bout({ ipponsB: ['M'], modifiedAt: NOW - 1 }), onSubmit)); });
    expect(marks(r.container).map((l) => l.split(' ')[0]), 'precondition: the older copy is held').toEqual(['Aka']);
    await wait(350);
    await settle();
    expect(onSubmit, 'precondition: the autosave went out').toHaveBeenCalled();
    return r;
  }

  it('a superseded answer puts the server\'s mark on the board and drops the operator\'s', async () => {
    const r = await tapThenAnswer(REFUSED);
    expect(marks(r.container).map((l) => l.split(' ')[0])).toEqual(['Shiro']);
  });

  it('a nothing-handed-back answer (a thrown refusal the host reported) does the same', async () => {
    const r = await tapThenAnswer(undefined);
    expect(marks(r.container).map((l) => l.split(' ')[0])).toEqual(['Shiro']);
  });

  it('a queued answer keeps the board: that write will land', async () => {
    const r = await tapThenAnswer(QUEUED);
    expect(marks(r.container).map((l) => l.split(' ')[0])).toEqual(['Aka']);
  });

  it('a tap made after the refused save was built is kept', async () => {
    let settleFirst;
    const onSubmit = vi.fn().mockImplementationOnce(() => new Promise((res) => { settleFirst = res; }))
      .mockResolvedValue({ id: 'm1', status: 'running' });
    let r;
    await act(async () => { r = render(el(bout(), onSubmit)); });
    await tap(menButton(r.container.querySelector('.sb-side--aka')));
    await act(async () => { r.rerender(el(bout({ ipponsB: ['M'], modifiedAt: NOW - 1 }), onSubmit)); });
    await wait(350); // the first save is out and unanswered
    vi.setSystemTime(NOW + 400);
    await tap(menButton(r.container.querySelector('.sb-side--aka'))); // a second Aka mark, after it was built
    await act(async () => { settleFirst(REFUSED); });
    await settle();
    expect(marks(r.container).filter((l) => l.startsWith('Aka')), 'the newer edit is still owed').not.toHaveLength(0);
  });
});

describe('engi editor: a refused autosave takes the server\'s copy', () => {
  const engi = (overrides = {}) => ({
    id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    sideA: { id: 'p-aka', name: 'Aka Pair 1 - Aka Pair 2', dojo: 'Aka Dojo' },
    sideB: { id: 'p-shiro', name: 'Shiro Pair 1 - Shiro Pair 2', dojo: 'Shiro Dojo' },
    flagsA: 0, flagsB: 0, modifiedAt: NOW - 5000,
    ...overrides,
  });
  const el = (match, onSubmit) => (
    <EngiScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} variant="inline" />
  );
  const count = (r, side) => r.getByTestId(`engi-${side}-count`).textContent;

  async function tapThenAnswer(answer) {
    const onSubmit = vi.fn().mockResolvedValue(answer);
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r.getByTestId('engi-aka-inc'));
    expect(count(r, 'aka'), 'precondition: the tap shows').toBe('1');
    // Another device's count, stamped before the tap: held by the stamp.
    await act(async () => { r.rerender(el(engi({ flagsB: 1, modifiedAt: NOW - 1 }), onSubmit)); });
    expect([count(r, 'aka'), count(r, 'shiro')], 'precondition: the older copy is held').toEqual(['1', '0']);
    await wait(350);
    await settle();
    expect(onSubmit, 'precondition: the autosave went out').toHaveBeenCalled();
    return r;
  }

  it('a superseded answer puts the server\'s counts on the board', async () => {
    const r = await tapThenAnswer(REFUSED);
    expect([count(r, 'aka'), count(r, 'shiro')]).toEqual(['0', '1']);
  });

  it('a nothing-handed-back answer does the same', async () => {
    const r = await tapThenAnswer(undefined);
    expect([count(r, 'aka'), count(r, 'shiro')]).toEqual(['0', '1']);
  });

  it('a queued answer keeps the board', async () => {
    const r = await tapThenAnswer(QUEUED);
    expect([count(r, 'aka'), count(r, 'shiro')]).toEqual(['1', '0']);
  });

  it('a tap made after the refused save was built is kept', async () => {
    let settleFirst;
    const onSubmit = vi.fn().mockImplementationOnce(() => new Promise((res) => { settleFirst = res; }))
      .mockResolvedValue({ id: 'm1', status: 'running' });
    let r;
    await act(async () => { r = render(el(engi(), onSubmit)); });
    await tap(r.getByTestId('engi-aka-inc'));
    await act(async () => { r.rerender(el(engi({ flagsB: 1, modifiedAt: NOW - 1 }), onSubmit)); });
    await wait(350);
    vi.setSystemTime(NOW + 400);
    await tap(r.getByTestId('engi-aka-inc')); // after the first save was built
    await act(async () => { settleFirst(REFUSED); });
    await settle();
    expect(count(r, 'aka'), 'the newer edit is still owed').toBe('2');
  });
});
