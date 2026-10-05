// Every way out of a score editor keeps a tap still inside the autosave
// window, and asks first about what the autosave does not carry (operator
// rulings 2026-09-27).
//
// The unmount used to CANCEL a pending edit, and only Close and Prev/Next saved
// it first. Every other way out dropped it: another match picked on the court
// console, a correction opened, a court switch, or the console moving on by
// itself. The unmount now writes it, stamped with the time of the tap
// (editedPerf, see sync_queue.test.jsx for the stamp), so a tap made before
// another device finished the match is older than that finish and cannot
// reopen it. Discard is the one way out that saves nothing. Prev/Next ask
// "Discard unsaved scoring changes?" first whenever Close would.

import React from 'react';
import { render, act, fireEvent, screen, cleanup, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';

// Every patch recordScore was handed, in order.
let writes;

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    notePendingEdit: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
// The editors, by key: the kendo modal routes individual and team matches.
const EDITORS = {};
// The engi editor asks through ui.jsx's own dialog, which needs its host.
let DialogHost;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  EDITORS.kendo = window.ScoreEditorModal;
  ({ EngiScoreEditorModal: EDITORS.engi } = await import('../../admin_scoring_engi.jsx'));
  ({ DialogHost } = await import('../../ui.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  writes = [];
  window.API.recordScore.mockReset();
  window.API.recordScore.mockImplementation((_c, _m, patch) => {
    writes.push(patch);
    return Promise.resolve(undefined);
  });
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  vi.useFakeTimers();
});

afterEach(() => { cleanup(); vi.useRealTimers(); });

const individual = (status = 'running') => ({
  id: `m-${status}`, status, phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
});
const team = (over = {}) => ({
  id: 'tm-run', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 3,
  sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
  ...over,
});
const engi = (over = {}) => ({
  id: 'e-run', status: 'running', court: 'A',
  sideA: { id: 'pa', name: 'Aka One - Aka Two' }, sideB: { id: 'pb', name: 'Shiro One - Shiro Two' },
  ...over,
});

// Mounts an editor as a host does: onClose and onNext unmount it, which is
// the moment a pending tap is written.
async function mount(match, { editor = 'kendo', ...props } = {}) {
  const Editor = EDITORS[editor];
  let view;
  const onClose = vi.fn(() => view.unmount());
  const onNext = vi.fn(() => view.unmount());
  const onSubmit = (p) => window.API.recordScore('comp1', match.id, p, '', match);
  const element = (m) => (
    <>
      <DialogHost />
      <Editor match={m} onClose={onClose} onSubmit={onSubmit} password=""
        nextMatch={{ id: 'm-next', sideA: { name: 'Sato' }, sideB: { name: 'Ito' } }} onNext={onNext} {...props} />
    </>
  );
  await act(async () => { view = render(element(match)); });
  return { view, onClose, onNext, rerender: (m) => act(async () => { view.rerender(element(m)); }) };
}

const click = (el) => act(async () => { fireEvent.click(el); });
const settle = () => act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
const teamRow = (i) => [...document.querySelectorAll('.team-sub-match')][i];
const teamPoint = (i, side, letter) => [...teamRow(i).querySelectorAll(`.team-sub-match__side--${side} button.ipt-btn`)]
  .find((b) => b.textContent === letter);

describe('the unmount writes a tap still inside the autosave window', () => {
  it.each([
    ['individual', () => individual(), 'kendo', () => screen.getAllByText('M')[0]],
    ['team', () => team(), 'kendo', () => teamPoint(0, 'shiro', 'K')],
    ['engi', () => engi(), 'engi', () => screen.getByTestId('engi-aka-inc')],
  ])('%s editor: the host moving on by itself still saves it', async (_kind, make, editor, target) => {
    const { view } = await mount(make(), { editor });
    await click(target());
    expect(writes).toHaveLength(0);
    await act(async () => { view.unmount(); });
    expect(writes).toHaveLength(1);
    expect(writes[0].status).toBe('running');
    // Written once: the unmount took the timer with it.
    await settle();
    expect(writes).toHaveLength(1);
  });

  it('the write carries the time of the last tap, not of the unmount', async () => {
    const { view } = await mount(individual());
    await click(screen.getAllByText('M')[0]);
    await act(async () => { vi.advanceTimersByTime(100); });
    await click(screen.getAllByText('K')[1]);
    const lastTap = performance.now();
    await act(async () => { vi.advanceTimersByTime(100); });
    await act(async () => { view.unmount(); });
    expect(writes).toHaveLength(1);
    expect(writes[0].editedPerf).toBe(lastTap);
  });

  it('a write the debounce sends carries the time of the last tap too', async () => {
    await mount(individual());
    await click(screen.getAllByText('M')[0]);
    const lastTap = performance.now();
    await settle();
    expect(writes[0].editedPerf).toBe(lastTap);
  });
});

describe('Discard is the one way out that saves nothing', () => {
  it('discarding on a running match writes nothing when the editor goes', async () => {
    const { onClose } = await mount(individual());
    // A Tie is a result the running write does not carry, so Close asks.
    await click(screen.getByTestId('scoring-modal-mark-draw'));
    await click(screen.getByText('✕ Close'));
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledTimes(1);
    await settle();
    expect(writes).toHaveLength(0);
  });
});

describe('Prev/Next ask first whenever Close would', () => {
  const viaButton = () => click(screen.getByText('Next →'));
  const viaKey = () => act(async () => { fireEvent.keyDown(window, { key: 'ArrowRight' }); });

  it.each([['the Next button', viaButton], ['the → key', viaKey]])(
    'individual, a Tie toggled on a running match: %s asks, and staying keeps the editor',
    async (_via, goNext) => {
      const { onNext } = await mount(individual());
      await click(screen.getByTestId('scoring-modal-mark-draw'));
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await goNext();
      expect(window.confirmDialog).toHaveBeenCalledTimes(1);
      expect(onNext).not.toHaveBeenCalled();
    },
  );

  it('individual: Discard changes moves on and writes nothing', async () => {
    const { onNext } = await mount(individual());
    await click(screen.getByTestId('scoring-modal-mark-draw'));
    await viaButton();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onNext).toHaveBeenCalledTimes(1);
    await settle();
    expect(writes).toHaveLength(0);
  });

  it('individual: an unsaved change to a finished match asks', async () => {
    const { onNext } = await mount({ ...individual('completed'), ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' } });
    await click(screen.getAllByText('K')[1]);
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await viaButton();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onNext).not.toHaveBeenCalled();
  });

  it('team: an unsaved change to a finished match asks', async () => {
    const bout = (position, ipponsA, winner, decision = '') => ({ position, sideA: '', sideB: '', ipponsA, ipponsB: [], winner, decision });
    const { onNext } = await mount(team({
      status: 'completed', winner: { id: 'team-kyoto', name: 'Kyoto' },
      subResults: [bout(1, ['M'], 'Kyoto'), bout(2, [], '', 'hikiwake'), bout(3, [], '', 'hikiwake')],
    }));
    await click(teamPoint(0, 'shiro', 'K'));
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await viaButton();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onNext).not.toHaveBeenCalled();
  });

  it('engi: an unsaved flag change to a finished match asks', async () => {
    const { onNext } = await mount(engi({ status: 'completed', flagsA: 3, flagsB: 0 }), { editor: 'engi' });
    await click(screen.getByTestId('engi-shiro-inc'));
    await viaButton();
    // The editor is a dialog too: find the confirm by what it asks.
    const dialog = screen.getByText('Discard unsaved scoring changes?').closest('[role="dialog"]');
    await click(within(dialog).getByText('Cancel'));
    expect(onNext).not.toHaveBeenCalled();
  });

  it('a running match with a tap and nothing else moves on at once, and keeps the tap', async () => {
    const { onNext } = await mount(individual());
    await click(screen.getAllByText('M')[0]);
    await viaButton();
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onNext).toHaveBeenCalledTimes(1);
    expect(writes).toHaveLength(1);
  });
});

describe('the team re-seed follows every untouched bout, whatever else the update moved', () => {
  // A server update that also moved the overtime count queues that change
  // first, and React (these tests' renderer) then runs the re-seed's updater
  // a render later, after the previous board has moved on. Read there, every
  // untouched bout looked edited and kept its stale copy, which the next save
  // wrote back over the server's.
  const played = (position, ipponsA, winner) => ({ position, sideA: '', sideB: '', ipponsA, ipponsB: [], winner, decision: '' });
  const blank = (position) => ({ position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: '' });

  it('another device recording a bout and overtime in one save: the next autosave carries the bout', async () => {
    const start = team({ subResults: [blank(1), blank(2), blank(3)] });
    const { rerender } = await mount(start);
    await rerender(team({ encho: { periodCount: 1 }, subResults: [blank(1), played(2, ['M', 'M'], 'Kyoto'), blank(3)] }));
    await click(teamPoint(0, 'shiro', 'K'));
    await settle();
    expect(writes).toHaveLength(1);
    const bout2 = writes[0].subResults.find((s) => s.position === 2);
    expect(bout2.ipponsA).toEqual(['M', 'M']);
  });
});
