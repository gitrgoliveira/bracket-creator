// Every scoring edit on a running match is saved on its own, the overtime
// count and a tie-break bout's rep pick included, and a key that changes
// nothing saves nothing.
//
// useDebouncedRunningWrite (admin_scoring_autosave.jsx) writes a running match
// only after an operator handler calls markDirty. Both kendo editors handed the
// overtime control (EnchoControl) the bare state setter, so an overtime change
// reached the server only with the next point, a close or Prev/Next: never on
// its own, and never across a reload, because the page-hide flush writes only
// an edit whose timer is armed. The rep pickers of a team pool tie-break bout
// had the same gap. The engi editor had the opposite one: a key pressed at a
// bound armed the timer with no change, and a running write stamped now went
// out for nothing (bc-rvfx: such a write can beat another device's older
// queued result).

import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
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
  resolveRoundIndex: () => 0,
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
let ScoreEditorModal;
let EngiScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
  ({ EngiScoreEditorModal } = await import('../../admin_scoring_engi.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  writes = [];
  window.API.recordScore.mockReset();
  window.API.recordScore.mockImplementation((_c, _m, patch) => {
    writes.push(patch);
    return Promise.resolve(undefined);
  });
  window.API.notePendingEdit.mockReset();
  vi.useFakeTimers();
});

afterEach(() => { cleanup(); vi.useRealTimers(); });

const onSubmitFor = (match) => (p) => window.API.recordScore('comp1', match.id, p, '', match);
const editorFor = (match) => (
  <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmitFor(match)} password="" />
);

async function mount(match) {
  let utils;
  await act(async () => { utils = render(editorFor(match)); });
  return utils;
}

const click = (el) => act(async () => { fireEvent.click(el); });
const settle = () => act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
const pageHide = () => act(async () => { window.dispatchEvent(new Event('pagehide')); });

// Opens the collapsed Overtime pill and ticks "Encho started": 0 to 1 period.
async function startOvertime() {
  await click(screen.getByTestId('scoring-modal-encho-pill'));
  await click(screen.getByTestId('scoring-modal-encho-checkbox'));
}
const addOvertimePeriod = () => click(screen.getByLabelText('Increase overtime period count'));

const individual = () => ({
  id: 'm-run', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
});
const team = () => ({
  id: 'tm-run', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 3,
  sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
});
// A knockout encounter with a representative bout: the overtime count then
// belongs to that bout (daihyosenEnchoFields), not to the match.
const teamWithDaihyosen = () => ({
  ...team(), id: 'tm-dh', phase: 'bracket', round: 'Final',
  subResults: [{ position: -1, sideA: 'Kyoto', sideB: 'Osaka', ipponsA: [], ipponsB: [], winner: '', decision: 'daihyosen' }],
});

const matchEncho = (p) => p.encho;
const daihyosenEncho = (p) => (p.subResults || []).find((s) => s.position === -1)?.encho;

describe('the overtime count is saved on its own, like a point', () => {
  it.each([
    ['individual match', individual, matchEncho],
    ['team match', team, matchEncho],
    ['team match with a daihyosen', teamWithDaihyosen, daihyosenEncho],
  ])('%s: ticking Encho and adding a period each write the count', async (_kind, make, enchoOf) => {
    await mount(make());
    await startOvertime();
    // Held for the debounce window, as a point is.
    expect(writes).toHaveLength(0);
    await settle();
    expect(writes).toHaveLength(1);
    expect(writes[0].status).toBe('running');
    expect(enchoOf(writes[0])).toEqual({ periodCount: 1 });

    await addOvertimePeriod();
    await settle();
    expect(writes).toHaveLength(2);
    expect(enchoOf(writes[1])).toEqual({ periodCount: 2 });
  });

  it.each([
    ['individual match', individual],
    ['team match', team],
  ])('%s: a change and then the page going away is written at once, durably', async (_kind, make) => {
    await mount(make());
    await startOvertime();
    await pageHide();
    expect(writes).toHaveLength(1);
    expect(writes[0].durable).toBe(true);
    expect(writes[0].status).toBe('running');
    expect(writes[0].encho).toEqual({ periodCount: 1 });
    // The flush took the timer with it: nothing more when it would have fired.
    await settle();
    expect(writes).toHaveLength(1);
  });
});

describe('a team editor keeps an overtime tap its own save has not caught up with', () => {
  // Every overtime tap is saved now, so each one comes back from the server a
  // moment later. The team editor followed the server's count whatever it
  // held, so the save of the first tap, arriving after a second tap, put the
  // count back to one and the second save sent one again.
  const shownCount = () => document.querySelector('.encho-row__count')?.textContent;

  it('a second tap made before the first one\'s save comes back stands', async () => {
    const { rerender } = await mount(team());
    await startOvertime();
    await settle();
    await addOvertimePeriod();
    // The first save comes back while the second is still held.
    await act(async () => { rerender(editorFor({ ...team(), encho: { periodCount: 1 } })); });
    expect(shownCount()).toBe('×2');
    await settle();
    expect(writes.map((p) => p.encho)).toEqual([{ periodCount: 1 }, { periodCount: 2 }]);
  });

  it('an editor nobody touched still follows a count recorded elsewhere, and writes nothing', async () => {
    const { rerender } = await mount(team());
    await act(async () => { rerender(editorFor({ ...team(), encho: { periodCount: 2 } })); });
    expect(shownCount()).toBe('×2');
    await settle();
    expect(writes).toHaveLength(0);
  });
});

describe('a rep pick on a running tie-break bout is saved on its own', () => {
  // A team competition's pool tie-break bout is scored in the individual
  // editor, with a picker per side for the fighter each team sends.
  const repBout = () => ({
    ...individual(), id: 'Pool 1-DH-1',
    sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
    repIsTeam: true, repRosterA: ['Kato', 'Mori'], repRosterB: ['Sato', 'Ito'],
  });
  const pick = (testId, value) => act(async () => { fireEvent.change(screen.getByTestId(testId), { target: { value } }); });

  it('each pick writes the fighter, with nothing else entered', async () => {
    await mount(repBout());
    await pick('rep-shiro-select', 'Sato');
    await settle();
    expect(writes).toHaveLength(1);
    expect(writes[0].status).toBe('running');
    expect(writes[0].repPlayerB).toBe('Sato');

    await pick('rep-aka-select', 'Mori');
    await settle();
    expect(writes).toHaveLength(2);
    expect(writes[1]).toMatchObject({ repPlayerA: 'Mori', repPlayerB: 'Sato' });
  });

  // Each pick is saved now, so each comes back from the server a moment later,
  // and the editor followed the server's pick whatever it held.
  const shownPick = (testId) => screen.getByTestId(testId).value;

  it('a second pick made before the first one\'s save comes back stands', async () => {
    const { rerender } = await mount(repBout());
    await pick('rep-shiro-select', 'Sato');
    await settle();
    await pick('rep-shiro-select', 'Ito');
    // The first save comes back while the second is still held.
    await act(async () => { rerender(editorFor({ ...repBout(), repPlayerB: 'Sato' })); });
    expect(shownPick('rep-shiro-select')).toBe('Ito');
    await settle();
    expect(writes.map((p) => p.repPlayerB)).toEqual(['Sato', 'Ito']);
  });

  it('a pick still being saved on one side does not stop the other side following the server', async () => {
    const { rerender } = await mount(repBout());
    await pick('rep-shiro-select', 'Ito');
    await act(async () => { rerender(editorFor({ ...repBout(), repPlayerA: 'Mori' })); });
    expect(shownPick('rep-aka-select')).toBe('Mori');
    expect(shownPick('rep-shiro-select')).toBe('Ito');
  });

  it('an editor nobody touched still follows a pick recorded elsewhere, and writes nothing', async () => {
    const { rerender } = await mount(repBout());
    await act(async () => { rerender(editorFor({ ...repBout(), repPlayerB: 'Sato' })); });
    expect(shownPick('rep-shiro-select')).toBe('Sato');
    await settle();
    expect(writes).toHaveLength(0);
  });
});

describe('an engi key that changes no flag count writes nothing', () => {
  const engi = (flags) => ({
    id: 'e-run', status: 'running', court: 'A',
    sideA: { id: 'pa', name: 'Aka One - Aka Two' }, sideB: { id: 'pb', name: 'Shiro One - Shiro Two' },
    ...flags,
  });
  async function mountEngi(match) {
    await act(async () => {
      render(<EngiScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmitFor(match)} />);
    });
  }
  const key = (k) => act(async () => { fireEvent.keyDown(window, { key: k }); });

  it('an add key at the most flags a side can hold', async () => {
    await mountEngi(engi({ flagsA: 5, flagsB: 0 }));
    await key('a');
    await settle();
    expect(writes).toHaveLength(0);
    // A key that does change a count still saves.
    await key('s');
    await settle();
    expect(writes).toHaveLength(1);
    expect(writes[0]).toEqual({ flagsA: 5, flagsB: 1, status: 'running', editedPerf: expect.any(Number) });
  });

  it('Backspace with no flag left to take back', async () => {
    await mountEngi(engi({ flagsA: 0, flagsB: 0 }));
    await key('a');
    await settle();
    await key('Backspace');
    await settle();
    expect(writes.map((p) => p.flagsA)).toEqual([1, 0]);
    await key('Backspace');
    await settle();
    expect(writes).toHaveLength(2);
  });
});

describe('a daihyosen verdict on a running encounter is saved on its own', () => {
  // Picking a side, and withdrawing it, ride the running write like a point,
  // so leaving the editor or reloading keeps them. The server keeps a verdict
  // on a running encounter's representative bout (validateSubBout has no
  // status gate). The arm alone is a mode and writes nothing.
  const dhRow = (p) => (p.subResults || []).find((s) => s.position === -1);
  const recorded = () => ({
    ...teamWithDaihyosen(),
    subResults: [{ position: -1, sideA: 'Kyoto', sideB: 'Osaka', ipponsA: [], ipponsB: ['Ht'], winner: 'Osaka', decision: 'daihyosen', decidedByHantei: true }],
  });
  const arm = () => click(screen.getByTestId('team-daihyosen-hantei-arm'));
  const pick = (side) => click(screen.getByTestId(`team-daihyosen-hantei-${side}`));

  it('picking a side writes the verdict', async () => {
    await mount(teamWithDaihyosen());
    await arm();
    await pick('shiro');
    await settle();
    expect(writes).toHaveLength(1);
    expect(writes[0].status).toBe('running');
    expect(dhRow(writes[0])).toMatchObject({ decidedByHantei: true, winner: 'Osaka' });
  });

  it('Cancel writes the withdrawal', async () => {
    await mount(recorded());
    await click(screen.getByTestId('team-daihyosen-hantei-cancel'));
    await settle();
    expect(writes).toHaveLength(1);
    expect(dhRow(writes[0]).decidedByHantei).toBe(false);
  });

  it('a second pick made before the first one\'s save comes back stands', async () => {
    const { rerender } = await mount(teamWithDaihyosen());
    await arm();
    await pick('shiro');
    await settle();
    await pick('aka');
    // The first save comes back while the second is still held.
    await act(async () => { rerender(editorFor(recorded())); });
    await settle();
    expect(writes.map((p) => dhRow(p).winner)).toEqual(['Osaka', 'Kyoto']);
  });

  it('an editor nobody touched still follows a verdict recorded elsewhere, and writes nothing', async () => {
    const { rerender } = await mount(teamWithDaihyosen());
    await act(async () => { rerender(editorFor(recorded())); });
    expect(screen.getByTestId('team-daihyosen-hantei-shiro').className).toContain('btn--primary');
    await settle();
    expect(writes).toHaveLength(0);
  });

  it('the arm alone writes nothing', async () => {
    await mount(teamWithDaihyosen());
    await arm();
    await settle();
    expect(writes).toHaveLength(0);
  });
});
