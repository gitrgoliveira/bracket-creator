// Every scoring edit on a running match is saved on its own, the overtime
// count, a tie-break bout's rep pick and a representative bout's verdict
// included, and a key that changes nothing saves nothing.
//
// The editors once wrote a running match through a debounced autosave that
// only an operator handler armed. Both kendo editors handed the overtime
// control (EnchoControl) the bare state setter, so an overtime change reached
// the server only with the next point, a close or Prev/Next: never on its own,
// and never across a reload. The rep pickers of a team pool tie-break bout had
// the same gap. Since bc-evts (stage 3 individual, stage 4 engi, stage 5 team)
// each such change is a tap handed to the outbox as it is made; the engi
// editor's counts are pinned in engi_editor_taps.render.test.jsx.

import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { beforeEach as beforeEachTest } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

// bc-evts stage 3: a running individual match is scored by taps handed to
// the outbox (API.recordTap); each test gets a fresh fake of that surface.
beforeEachTest(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });
import { serverFold } from '../helpers/fake_outbox.js';

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
  writes = [];
  window.API.recordScore.mockReset();
  window.API.recordScore.mockImplementation((_c, _m, patch) => {
    writes.push(patch);
    return Promise.resolve({ applied: true });
  });
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
// Long past the 300 ms debounce there used to be: nothing is written after it.
const settle = () => act(async () => { vi.advanceTimersByTime(350); });
const pageHide = () => act(async () => { window.dispatchEvent(new Event('pagehide')); });
const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);

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
  id: 'tm-run', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 3,
  sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
  subResults: [1, 2, 3].map((position) => ({ position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: '' })),
});
// A knockout encounter with a representative bout: the overtime count then
// belongs to that bout (daihyosenEnchoFields), not to the match.
const teamWithDaihyosen = () => ({
  ...team(), id: 'tm-dh', phase: 'bracket', round: 'Final',
  subResults: [...team().subResults, { position: -1, sideA: 'Kyoto', sideB: 'Osaka', ipponsA: [], ipponsB: [], winner: '', decision: 'daihyosen' }],
});

describe('the overtime count is saved on its own, like a point', () => {
  // bc-evts stage 5: the team editor's count is a tap per period too, on the
  // encounter (pos 0) or, once a representative bout exists, on that bout.
  it.each([
    ['team match', team, 0],
    ['team match with a daihyosen', teamWithDaihyosen, -1],
  ])('%s: ticking Encho and adding a period are each an encho.add tap', async (_kind, make, pos) => {
    await mount(make());
    await startOvertime();
    await addOvertimePeriod();
    expect(taps()).toEqual([{ kind: 'encho.add', pos }, { kind: 'encho.add', pos }]);
    await settle();
    expect(writes).toHaveLength(0);
  });

  // bc-evts stage 3: a running individual match is scored by taps, the
  // overtime count among them: one encho.add per period, as it is made.
  it('individual match: ticking Encho and adding a period are each an encho.add tap', async () => {
    await mount(individual());
    await startOvertime();
    await addOvertimePeriod();
    expect(window.API.recordTap.mock.calls.map((c) => c[2])).toEqual([
      { kind: 'encho.add', pos: 0 },
      { kind: 'encho.add', pos: 0 },
    ]);
    await settle();
    expect(writes).toHaveLength(0);
  });

  it.each([
    ['team match', team],
  ])('%s: a change is in the outbox as it is made, so the page going away owes nothing', async (_kind, make) => {
    await mount(make());
    await startOvertime();
    expect(taps()).toEqual([{ kind: 'encho.add', pos: 0 }]);
    await pageHide();
    await settle();
    expect(writes).toHaveLength(0);
  });
});

describe('a team editor keeps an overtime tap the server has not caught up with', () => {
  // Every overtime tap is sent at once, so each one comes back from the server
  // a moment later. The team editor once followed the server's count whatever
  // it held, so the save of the first tap, arriving after a second tap, put the
  // count back to one. Now the board is the server's copy with the pending taps
  // folded on, and a copy holding a tap counts it once.
  const shownCount = () => document.querySelector('.encho-row__count')?.textContent;

  it('a second tap made before the first one comes back stands', async () => {
    const { rerender } = await mount(team());
    await startOvertime();
    const first = window.API.pendingTaps('comp1', 'tm-run')[0];
    await addOvertimePeriod();
    // The server's copy holding the first tap comes back while the second is pending.
    const echoed = serverFold(team(), [first], { team: true }).match;
    await act(async () => { rerender(editorFor(echoed)); });
    expect(shownCount()).toBe('×2');
    expect(taps()).toEqual([{ kind: 'encho.add', pos: 0 }, { kind: 'encho.add', pos: 0 }]);
    expect(writes).toHaveLength(0);
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
  // bc-evts stage 3: on a running bout each pick is a pool.rep tap.
  const repBout = () => ({
    ...individual(), id: 'Pool 1-DH-1',
    sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
    repIsTeam: true, repRosterA: ['Kato', 'Mori'], repRosterB: ['Sato', 'Ito'],
  });
  const pick = (testId, value) => act(async () => { fireEvent.change(screen.getByTestId(testId), { target: { value } }); });
  const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);

  it('each pick is a pool.rep tap naming the fighter, with nothing else written', async () => {
    await mount(repBout());
    await pick('rep-shiro-select', 'Sato');
    await pick('rep-aka-select', 'Mori');
    await settle();
    expect(taps()).toEqual([
      { kind: 'pool.rep', pos: 0, side: 'b', name: 'Sato' },
      { kind: 'pool.rep', pos: 0, side: 'a', name: 'Mori' },
    ]);
    expect(writes).toHaveLength(0);
  });

  const shownPick = (testId) => screen.getByTestId(testId).value;

  it('a second pick made before the first one comes back stands', async () => {
    const { rerender } = await mount(repBout());
    await pick('rep-shiro-select', 'Sato');
    await pick('rep-shiro-select', 'Ito');
    // The first pick comes back from the server while the second is pending.
    await act(async () => { rerender(editorFor({ ...repBout(), repPlayerB: 'Sato' })); });
    expect(shownPick('rep-shiro-select')).toBe('Ito');
  });

  it('a pick pending on one side does not stop the other side following the server', async () => {
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
    expect(taps()).toEqual([]);
  });
});

// The engi editor's half (a key at a bound wrote a running save) moved with
// its autosave in bc-evts stage 4: a running engi match is scored by taps, and
// engi_editor_taps.render.test.jsx pins that such a key sends nothing.

describe('a daihyosen verdict on a running encounter is saved on its own', () => {
  // Picking a side, and withdrawing it, are taps like a point (bc-evts stage 5,
  // Q11), so leaving the editor or reloading keeps them and the TV board shows
  // them at once. The arm alone is a mode and writes nothing.
  const recorded = () => ({
    ...teamWithDaihyosen(),
    subResults: [...team().subResults, { position: -1, sideA: 'Kyoto', sideB: 'Osaka', ipponsA: [], ipponsB: ['Ht'], winner: 'Osaka', decision: 'daihyosen', decidedByHantei: true }],
  });
  const arm = () => click(screen.getByTestId('team-daihyosen-hantei-arm'));
  const pick = (side) => click(screen.getByTestId(`team-daihyosen-hantei-${side}`));

  it('picking a side writes the verdict', async () => {
    await mount(teamWithDaihyosen());
    await arm();
    // Each button names its colour, for an operator who does not know the words.
    expect(screen.getByTestId('team-daihyosen-hantei-shiro').textContent).toBe('SHIRO (White) wins');
    expect(screen.getByTestId('team-daihyosen-hantei-aka').textContent).toBe('AKA (Red) wins');
    await pick('shiro');
    expect(taps()).toEqual([{ kind: 'daihyosen.verdict', pos: -1, side: 'b' }]);
    expect(screen.getByTestId('team-daihyosen-ht-shiro')).toBeTruthy();
    await settle();
    expect(writes).toHaveLength(0);
  });

  it('Cancel writes the withdrawal', async () => {
    await mount(recorded());
    await click(screen.getByTestId('team-daihyosen-hantei-cancel'));
    expect(taps()).toEqual([{ kind: 'daihyosen.verdict', pos: -1, value: 'clear' }]);
    await settle();
    expect(writes).toHaveLength(0);
  });

  it('a second pick made before the first one comes back stands', async () => {
    const { rerender } = await mount(teamWithDaihyosen());
    await arm();
    await pick('shiro');
    const first = window.API.pendingTaps('comp1', 'tm-dh')[0];
    await pick('aka');
    // The server's copy holding the first pick comes back while the second is pending.
    const echoed = serverFold(teamWithDaihyosen(), [first], { team: true }).match;
    await act(async () => { rerender(editorFor(echoed)); });
    expect(screen.getByTestId('team-daihyosen-hantei-aka').className).toContain('btn--primary');
    expect(taps().map((e) => e.side)).toEqual(['b', 'a']);
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
