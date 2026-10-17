// bc-dscn: "Discard unsaved scoring changes?" may only appear where closing
// would really discard something.
//
// Three ways it used to lie:
//  - On a RUNNING match every scoring edit was once autosaved (300ms debounce), so
//    the autosave landed while the prompt waited and "Discard" discarded
//    nothing. Closing now saves any edit still inside the debounce window at
//    once and closes without asking.
//  - On the inline court console the host cannot close (canClose=false,
//    onClose a no-op), yet Esc raised the prompt anyway. Esc is now ignored
//    there, and handleDismiss refuses to run.
//  - On a team match the dispatcher's own Esc listener also fired and closed
//    the editor past the team editor's prompt. It now stands aside.
//
// A scheduled match (a draft) and a correction keep the prompt, and so does
// a running individual match whose hikiwake toggle moved, because
// buildPatch("running") does not carry the draw, and a running kachinuki match
// with a changed unplayed row, which that patch drops. A hantei ARM alone is a
// mode, not a result: closing drops it with no prompt and no write.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { beforeEach as beforeEachTest } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

// bc-evts stage 3: a running individual match is scored by taps handed to
// the outbox (API.recordTap); each test gets a fresh fake of that surface.
beforeEachTest(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });

const STUBBED_GLOBALS = {
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
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
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

function individualMatch(status) {
  return {
    id: `m-${status}`,
    status,
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
  };
}

function teamMatch(status) {
  return {
    id: `t-${status}`,
    status,
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 'teamA', name: 'Team A' },
    sideB: { id: 'teamB', name: 'Team B' },
  };
}

// Closing unmounts the editor, as every host does: the unmount is what writes
// an edit not yet sent.
function mount(match, props = {}) {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  let utils;
  const onClose = vi.fn(() => utils.unmount());
  utils = render(
    <ScoreEditorModal match={match} onSubmit={onSubmit} password="" {...props} onClose={onClose} />,
  );
  return { ...utils, onSubmit, onClose };
}

// Resolves to fireEvent's return value: false when a handler called
// preventDefault, i.e. when the editor claimed the key.
const pressEscape = async () => {
  let notPrevented;
  await act(async () => { notPrevented = fireEvent.keyDown(document.body, { key: 'Escape' }); });
  return notPrevented;
};
const clickClose = () => act(async () => { fireEvent.click(screen.getByText('✕ Close')); });

describe('bc-dscn: individual editor', () => {
  it('a RUNNING match closes without the prompt and saves the pending edit now', async () => {
    // bc-evts stage 3: the point is a tap, in the outbox the moment it is
    // made, so closing asks nothing and writes nothing more.
    const { onSubmit, onClose } = mount(individualMatch('running'));
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    expect(window.API.recordTap).toHaveBeenCalledTimes(1);

    await clickClose();

    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
    await act(async () => { vi.advanceTimersByTime(350); });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(window.API.recordTap).toHaveBeenCalledTimes(1);
  });

  it('an untouched RUNNING match closes without writing', async () => {
    const { onSubmit, onClose } = mount(individualMatch('running'));
    await clickClose();
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('a SCHEDULED match (a draft) still prompts before discarding', async () => {
    const { onClose } = mount(individualMatch('scheduled'));
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    await clickClose();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalledTimes(1); // the stub confirms
  });

  it('a RUNNING match with the hikiwake toggle moved still prompts (the running patch carries no draw)', async () => {
    const { onClose, onSubmit } = mount(individualMatch('running'));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-mark-draw')); });
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await clickClose();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  // The arm is a mode, not a result: flushing it would send a freshly stamped
  // write with an unchanged scoreline, which can win last-write-wins over
  // another device's older queued result.
  it('a RUNNING match with only the hantei ARMED closes with no prompt and no write', async () => {
    const { onSubmit, onClose } = mount(individualMatch('running'));
    await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-hantei-arm')); });
    await clickClose();
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('Esc on the inline court console (canClose=false) never prompts', async () => {
    // Scheduled, so a dirty editor WOULD prompt if Esc reached handleDismiss:
    // only the canClose gate can keep the dialog away here.
    const { onClose } = mount(individualMatch('scheduled'), { variant: 'inline', canClose: false });
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    // Esc is left to whatever has focus (e.g. an open fighter list), not claimed.
    expect(await pressEscape()).toBe(true);
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe('bc-dscn: team editor', () => {
  // bc-evts stage 5: a running team match is scored by taps, each in the
  // outbox as it is made, so closing owes nothing and asks nothing.
  it('a RUNNING team match closes without the prompt: its tap went as it was made', async () => {
    const { onSubmit, onClose } = mount({ ...teamMatch('running'), subResults: [1, 2, 3].map((position) => ({ position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: '' })) });
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    expect(window.API.recordTap.mock.calls.map((c) => c[2])).toEqual([expect.objectContaining({ kind: 'ippon.add', pos: 1, letter: 'M' })]);

    await clickClose();

    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
    await act(async () => { vi.advanceTimersByTime(350); });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('a RUNNING team match with only the daihyosen hantei ARMED closes with no prompt and no write', async () => {
    const match = {
      ...teamMatch('running'),
      subResults: [
        ...[1, 2, 3].map((position) => ({ position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake' })),
        { position: -1, sideA: 'Team A', sideB: 'Team B', decision: 'daihyosen' },
      ],
    };
    const { onSubmit, onClose } = mount(match);
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-hantei-arm')); });
    await clickClose();
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  // A fighter picked on the current bout rides the bout, not a lineup PUT.
  // The running patch used to drop every unplayed row, so the pick was not
  // carried by a flush and closing had to prompt. Since bc-evts stage 5 the pick
  // is a bout.fighter tap, in the outbox as it is made, so closing asks nothing.
  it('a RUNNING kachinuki match with a fighter picked on the unscored current bout keeps the pick on close', async () => {
    const savedAPI = window.API;
    window.API = {
      ...savedAPI,
      fetchCompetitionDetails: vi.fn().mockResolvedValue({
        id: 'comp1', config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
      }),
      fetchSquads: vi.fn().mockResolvedValue({
        'team-A': [{ id: 'm-kept', index: 1, name: 'Kept Winner' }, { id: 'm-fresh', index: 9, name: 'Fresh Fighter' }],
        'team-B': [{ id: 'm-shiro', index: 1, name: 'Shiro One' }],
      }),
    };
    try {
      const match = {
        id: 'k1', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
        compKind: 'team', teamSize: 5, compFormat: 'mixed', teamMatchType: 'kachinuki',
        sideA: { id: 'team-A', name: 'Team A' },
        sideB: { id: 'team-B', name: 'Team B' },
        subResults: [
          ...[1, 2, 3, 4, 5].map((position) => ({ position, sideA: `Aka ${position}`, sideB: `Shiro ${position}`, ipponsA: ['M'], ipponsB: [] })),
          { position: 6, sideA: 'Kept Winner', sideB: '', sideAMemberId: 'm-kept', ipponsA: [], ipponsB: [] },
        ],
      };
      let utils;
      await act(async () => { utils = mount(match); });
      const akaInput = document.querySelector('.team-sub-match__side--aka input');
      await act(async () => { fireEvent.focus(akaInput); });
      const fresh = Array.from(document.querySelectorAll('.team-sub-match__side--aka .pmf__option'))
        .find((b) => b.textContent.includes('Fresh Fighter'));
      expect(fresh, 'expected "Fresh Fighter" to be offered').toBeTruthy();
      await act(async () => { fireEvent.click(fresh); });

      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await clickClose();
      expect(window.confirmDialog).not.toHaveBeenCalled();
      expect(utils.onClose).toHaveBeenCalledTimes(1);
      expect(utils.onSubmit).not.toHaveBeenCalled();
      expect(window.API.recordTap.mock.calls.map((c) => c[2])).toEqual([
        { kind: 'bout.fighter', pos: 6, side: 'a', name: 'Fresh Fighter', memberId: 'm-fresh' },
      ]);
    } finally {
      window.API = savedAPI;
    }
  });

  it('a SCHEDULED team match still prompts before discarding', async () => {
    mount(teamMatch('scheduled'));
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    await clickClose();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
  });

  // The dispatcher (ScoreEditorModal) registers its own keydown listener before
  // it hands a team match to TeamScoreEditorModal. That listener used to act as
  // well, so Esc closed the team editor straight past its own prompt, and a
  // point key wrote a phantom individual-shaped result on a team match.
  it('Esc on a dirty SCHEDULED team match waits for the prompt (declined: stays open)', async () => {
    const { onClose } = mount(teamMatch('scheduled'));
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await pressEscape();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('a point key on a team match writes no match-level ippons', async () => {
    const { onSubmit } = mount(teamMatch('running'));
    await act(async () => { fireEvent.keyDown(document.body, { key: 'm' }); });
    await act(async () => { vi.advanceTimersByTime(350); });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Esc on the inline court console (canClose=false) never prompts', async () => {
    const { onClose } = mount(teamMatch('scheduled'), { variant: 'inline', canClose: false });
    await act(async () => { fireEvent.click(screen.getAllByText('M')[0]); });
    // Esc is left to whatever has focus (e.g. an open fighter list), not claimed.
    expect(await pressEscape()).toBe(true);
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });
});

// bc-kbhn: each editor passes the hint what its keydown handler really does.
describe('bc-kbhn: the editors list only the keys that act on their host', () => {
  const hintKbds = () => [...screen.getByTestId('scoring-modal-shortcut-hint').querySelectorAll('kbd')].map((k) => k.textContent);

  it('individual, court console (inline, canClose=false, no nav): no arrows, no Esc', () => {
    mount(individualMatch('running'), { variant: 'inline', canClose: false });
    expect(hintKbds()).not.toContain('←');
    expect(hintKbds()).not.toContain('Esc');
  });

  it('individual, Scores tab with a neighbour: arrows and Esc', () => {
    mount(individualMatch('running'), { nextMatch: individualMatch('scheduled'), onPrev: vi.fn(), onNext: vi.fn() });
    expect(hintKbds()).toEqual(expect.arrayContaining(['←', '→', 'Esc']));
  });

  it('individual, callbacks wired but no neighbour: no arrows (the keys do nothing)', async () => {
    const onPrev = vi.fn();
    const onNext = vi.fn();
    mount(individualMatch('running'), { onPrev, onNext });
    expect(hintKbds()).not.toContain('←');
    await act(async () => { fireEvent.keyDown(document.body, { key: 'ArrowLeft' }); });
    expect(onPrev).not.toHaveBeenCalled();
  });

  it('team, court console (inline, canClose=false, no nav): no arrows, no Esc', () => {
    mount(teamMatch('running'), { variant: 'inline', canClose: false });
    expect(screen.queryByTestId('scoring-modal-shortcut-hint')).toBeNull(); // team match (no keyboard scoring): nothing left to list
  });
});
