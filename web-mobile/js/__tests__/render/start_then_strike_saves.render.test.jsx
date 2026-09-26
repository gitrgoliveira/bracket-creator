// bc-strt: a point struck right after Start match was not saved until the
// next edit. The editors autosave only a match they see as running, and the
// court list refetches a moment after each save, so for that moment the
// just-started match still read "scheduled" and the point stayed on this
// board alone. A start that has landed now makes the editor treat the
// scheduled snapshot it was made from as running: its own Start match, or
// the host's (`started`, the court console's Up next card).

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

// No compId on the individual match: the competition fetch stays out of it.
const individual = (overrides = {}) => ({
  id: 'm1', status: 'scheduled', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  modifiedAt: 1000,
  ...overrides,
});

const kachinuki = (overrides = {}) => ({
  id: 'k1', compId: 'comp1', status: 'scheduled', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 3, compFormat: 'mixed', teamMatchType: 'kachinuki',
  sideA: { id: 'team-A', name: 'Team A' }, sideB: { id: 'team-B', name: 'Team B' },
  subResults: [], modifiedAt: 1000,
  ...overrides,
});

const el = (match, onSubmit, props = {}) => (
  <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" {...props} />
);

// What a host returns for a write that reached the server: the stored
// match. A refused write throws inside the host, which reports it and
// returns nothing.
const LANDED = { status: 'running' };

// The writes after the Start match one: the autosaves.
const autosaves = (onSubmit) => onSubmit.mock.calls.map((c) => c[0]).filter((p) => p.status === 'running').slice(1);

const tapStart = async () => {
  await act(async () => { fireEvent.click(screen.getByText('Start match')); });
};
const strikeIndividualMen = async () => {
  await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
  await act(async () => { vi.advanceTimersByTime(400); });
};
const strikeKachinukiAkaMen = async () => {
  await act(async () => { fireEvent.keyDown(window, { key: 'M', shiftKey: true }); });
  await act(async () => { vi.advanceTimersByTime(400); });
};

describe('a point struck right after Start match saves at once (bc-strt)', () => {
  it('individual: the editor\'s own start lands, the list still says scheduled', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    await act(async () => { render(el(individual(), onSubmit)); });
    await tapStart();
    expect(onSubmit.mock.calls[0][0].status, 'precondition: Start match wrote running').toBe('running');

    await strikeIndividualMen();
    const saves = autosaves(onSubmit);
    expect(saves.length, 'the point is autosaved without waiting for the list').toBe(1);
    expect([...(saves[0].ipponsA || []), ...(saves[0].ipponsB || [])]).toContain('M');
  });

  it('individual: a start the host made (the Up next card) counts too', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await act(async () => { render(el(individual(), onSubmit, { started: true })); });
    await strikeIndividualMen();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].status).toBe('running');
  });

  it('a scheduled match nobody started still does not autosave', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await act(async () => { render(el(individual(), onSubmit)); });
    await strikeIndividualMen();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('a refused start leaves the match scheduled', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: false, reason: 'clock_skew' });
    await act(async () => { render(el(individual(), onSubmit)); });
    await tapStart();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    await strikeIndividualMen();
    expect(onSubmit, 'nothing was started, so nothing autosaves').toHaveBeenCalledTimes(1);
  });

  it('a later snapshot of the match (sent back to the queue) ends it', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    let utils;
    await act(async () => { utils = render(el(individual(), onSubmit)); });
    await tapStart();
    // The feed moves on: running, then sent back to the queue, which stamps it.
    await act(async () => { utils.rerender(el(individual({ status: 'running', modifiedAt: 2000 }), onSubmit)); });
    await act(async () => { utils.rerender(el(individual({ status: 'scheduled', modifiedAt: 3000 }), onSubmit)); });
    await strikeIndividualMen();
    expect(onSubmit, 'a queued match is not written as running').toHaveBeenCalledTimes(1);
  });

  it('the host\'s start ends with its snapshot as well', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await act(async () => { render(el(individual({ modifiedAt: 3000 }), onSubmit, { started: false })); });
    await strikeIndividualMen();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('kachinuki: the team editor\'s start lands, the list still says scheduled', async () => {
    const onSubmit = vi.fn().mockResolvedValue(LANDED);
    await act(async () => { render(el(kachinuki(), onSubmit)); });
    await tapStart();
    expect(onSubmit.mock.calls[0][0].status, 'precondition: Start match wrote running').toBe('running');

    await strikeKachinukiAkaMen();
    const saves = autosaves(onSubmit);
    expect(saves.length, 'the point is autosaved without waiting for the list').toBe(1);
    const bout1 = (saves[0].subResults || []).find((s) => s.position === 1);
    expect(bout1 && bout1.ipponsA).toEqual(['M']);
  });

  // The court is busy or a competitor is withdrawn: the server answers with
  // an error, the host shows it and returns nothing. Nothing was stored, so
  // the match is still waiting and a point struck now is not a running write.
  it('individual: a start the server refused with an error leaves the match scheduled', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await act(async () => { render(el(individual(), onSubmit)); });
    await tapStart();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(screen.getByText('Start match'), 'the operator can try again').toBeTruthy();
    await strikeIndividualMen();
    expect(onSubmit, 'nothing was started, so nothing autosaves').toHaveBeenCalledTimes(1);
  });

  it('kachinuki: a start the server refused with an error leaves the match scheduled', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    await act(async () => { render(el(kachinuki(), onSubmit)); });
    await tapStart();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(screen.getByText('Start match'), 'the operator can try again').toBeTruthy();
    await strikeKachinukiAkaMen();
    expect(onSubmit, 'nothing was started, so nothing autosaves').toHaveBeenCalledTimes(1);
  });
});
