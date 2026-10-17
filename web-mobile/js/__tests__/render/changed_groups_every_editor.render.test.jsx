// bc-mrgc (operator ruling 2026-10-03: "Nothing should be dropped. All events
// must be ordered."): every score write an editor sends names the groups it
// changed, so the server applies those and keeps every other group as stored.
//
// Naming too little loses the operator's edit (the server keeps the stored
// value of a group the write does not name); naming too much can put back a
// value another device changed since. Both halves are pinned here, through the
// real editors, for each kind of edit:
//   - an edit to one group names exactly that group;
//   - an autosave names no group it did not change, also when the match the
//     editor renders from has moved on a group the editor does not follow;
//   - a tap taken back before the first write's echo arrives still names the
//     group (the copy has not caught up, so a plain diff would see nothing);
//   - a value the copy only holds differently (an empty slot written as a
//     placeholder) is not a change.

import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { beforeEach as beforeEachTest } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';

// bc-evts stage 3: a running individual match is scored by taps handed to
// the outbox (API.recordTap); each test gets a fresh fake of that surface.
beforeEachTest(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap } from '../helpers/tap_events.js';
import { startPatch } from '../../start_match.jsx';

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
    return Promise.resolve({ applied: true });
  });
  window.API.notePendingEdit.mockReset();
  vi.useFakeTimers();
});

afterEach(() => { cleanup(); vi.useRealTimers(); });

const onSubmit = (p) => window.API.recordScore('comp1', 'm', p, '');
const editorFor = (match, extra = {}) => (
  <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" {...extra} />
);
async function mount(match, extra) {
  let utils;
  await act(async () => { utils = render(editorFor(match, extra)); });
  return utils;
}
const settle = () => act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });
const click = (el) => act(async () => { fireEvent.click(el); });
const lastWrite = () => writes[writes.length - 1];

const individual = (over = {}) => ({
  id: 'm-ind', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  ...over,
});
const ipponBtn = (side, letter) => [...document.querySelectorAll(`.sb-side--${side} button.ipt-btn`)].find((b) => b.textContent === letter);
const slotWith = (side, letter) => [...document.querySelectorAll('button')]
  .find((b) => new RegExp(`${side} slot \\d: remove ${letter}`).test(b.getAttribute('aria-label') || ''));
const startOvertime = async () => {
  await click(screen.getByTestId('scoring-modal-encho-pill'));
  await click(screen.getByTestId('scoring-modal-encho-checkbox'));
};
const finish = async (label = 'Finish') => {
  const btn = screen.getByText(label);
  await pointerTap(btn);
  await wait(TAP_BOUNCE_MS + 50);
  await pointerTap(btn);
  await wait(10);
};

describe('individual editor', () => {
  // bc-evts stage 3: a running individual match is scored by taps (the
  // outbox's recordTap), never by a running score write; the groups a write
  // names are the Finish's (or a correction's), worked out from the board the
  // editor shows: the server's copy with this device's pending taps.
  const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);

  it('a point is a tap, not a write; Finish names the result, and the points it has not seen stored', async () => {
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await settle();
    expect(writes).toHaveLength(0);
    expect(taps()).toEqual([expect.objectContaining({ kind: 'ippon.add', side: 'a', letter: 'M' })]);
    await finish();
    expect(lastWrite().status).toBe('completed');
    expect(lastWrite().changed).toEqual(['points', 'result']);
  });

  it('overtime is a tap; Finish names it with the result', async () => {
    await mount(individual({ ipponsA: ['M'] }));
    await startOvertime();
    await settle();
    expect(writes).toHaveLength(0);
    expect(taps()).toEqual([{ kind: 'encho.add', pos: 0 }]);
    await finish();
    expect(lastWrite().changed).toEqual(['result', 'encho']);
  });

  it('Finish on a scoreline already stored names the result alone', async () => {
    await mount(individual({ ipponsA: ['M'] }));
    await finish();
    expect(lastWrite().changed).toEqual(['result']);
  });

  it('Mark draw then Finish names the result alone (the scoreline stays 0-0)', async () => {
    await mount(individual());
    await click(screen.getByTestId('scoring-modal-mark-draw'));
    await settle();
    // The draw rides only the finishing write: nothing is sent before it.
    expect(writes).toHaveLength(0);
    expect(taps()).toEqual([]);
    await finish();
    expect(lastWrite().score.type).toBe('hikiwake');
    expect(lastWrite().changed).toEqual(['result']);
  });

  it('Finish on a tied 1-1 pool match names the result alone', async () => {
    await mount(individual({ ipponsA: ['M'], ipponsB: ['K'] }));
    await finish();
    expect(lastWrite().status).toBe('completed');
    expect(lastWrite().changed).toEqual(['result']);
  });

  it('Finish does not name a group the match moved on elsewhere while a tap was pending', async () => {
    // A point is struck, and before the server has it another device records
    // overtime. The board is the server's copy with the pending tap on top,
    // so it shows that overtime, and the Finish does not put "no overtime" back.
    const { rerender } = await mount(individual());
    await pointerTap(ipponBtn('shiro', 'K'));
    await act(async () => { rerender(editorFor(individual({ encho: { periodCount: 1 } }))); });
    await finish();
    expect(lastWrite().encho).toEqual({ periodCount: 1 });
    expect(lastWrite().changed).toEqual(['points', 'result']);
  });

  it('a reopen marked while a tap is pending: the Finish names the result and its points, nothing else', async () => {
    const { rerender } = await mount(individual());
    await pointerTap(ipponBtn('shiro', 'K'));
    await act(async () => { rerender(editorFor(individual({ reopenPending: true, encho: { periodCount: 1 } }))); });
    await finish();
    expect(lastWrite().encho).toEqual({ periodCount: 1 });
    expect(lastWrite().changed).toEqual(['points', 'result']);
  });

  // A reopen the server makes WITH a correction's reason (a downstream reopen, a
  // requalification, a pool-rank override) leaves the match scheduled with its
  // points and no reopenPending stamp. The editor opened on the finished result, so
  // that is the result it agrees with: finishing again rebuilds the same result and
  // must still say it changed it, or the server, told nothing changed, keeps the
  // match as it is and the editor closes as if it had finished.
  it('names the result when the match it opened finished is reopened with a reason elsewhere, and finished again', async () => {
    const { rerender } = await mount(individual({ status: 'completed', ipponsA: ['M', 'K'], winner: { id: 'p1', name: 'Yamada' } }));
    await act(async () => { rerender(editorFor(individual({ status: 'scheduled', ipponsA: ['M', 'K'] }))); });
    await finish();
    expect(lastWrite().status).toBe('completed');
    expect(lastWrite().changed).toContain('result');
  });

  it('names the result each time the server reopens the match the editor opened finished', async () => {
    // The agreement follows the server leaving the finished result every time, not once.
    const done = () => individual({ status: 'completed', ipponsA: ['M', 'K'], winner: { id: 'p1', name: 'Yamada' } });
    const reopened = () => individual({ status: 'scheduled', ipponsA: ['M', 'K'] });
    // The host hands nothing back for a finish (a landed one closes the editor), so the
    // editor stays armed for the second finish.
    window.API.recordScore.mockImplementation((_c, _m, patch) => { writes.push(patch); return Promise.resolve(undefined); });
    const { rerender } = await mount(done());
    await act(async () => { rerender(editorFor(reopened())); });
    await finish();
    expect(lastWrite().changed).toContain('result');
    await act(async () => { rerender(editorFor(done())); });
    await act(async () => { rerender(editorFor(reopened())); });
    const before = writes.length;
    await finish();
    expect(writes.length, 'the second finish is sent').toBeGreaterThan(before);
    expect(lastWrite().changed).toContain('result');
  });

  it('a point the server already holds is not named by the Finish', async () => {
    // The tap reached the server and its push came back: the board and the
    // match agree on the points, so the Finish names the result alone.
    const { rerender } = await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    const { id } = window.API.recordTap.mock.results[0].value;
    await act(async () => { rerender(editorFor(individual({ ipponsA: ['M'], markIds: { a: [id], b: [] }, scoreVersion: 1 }))); });
    await finish();
    expect(lastWrite().changed).toEqual(['result']);
  });

  it('a point taken back before the server has it: the Finish names nothing of it', async () => {
    await mount(individual({ ipponsB: ['K'] }));
    await pointerTap(ipponBtn('aka', 'M'));
    await pointerTap(slotWith('Aka', 'M'));
    expect(taps().map((e) => e.kind)).toEqual(['ippon.add', 'ippon.remove']);
    await finish();
    expect(lastWrite().ipponsA).toEqual([]);
    expect(lastWrite().changed).toEqual(['result']);
  });

  it('a placeholder slot the stored scoreline holds is not a change', async () => {
    // The server may hold an empty slot as "•"; the editor holds no slot.
    await mount(individual({ ipponsA: ['M', '•'] }));
    await finish();
    expect(lastWrite().changed).toEqual(['result']);
  });
});

describe('team editor', () => {
  const team = (over = {}) => ({
    id: 'm-team', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 3,
    sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
    ...over,
  });
  const row = (i) => [...document.querySelectorAll('.team-sub-match')][i];
  const boutBtn = (i, side, letter) => [...row(i).querySelectorAll(`.team-sub-match__side--${side} button.ipt-btn`)].find((b) => b.textContent === letter);

  it('scoring bout 2 names bout 2 alone', async () => {
    await mount(team());
    await pointerTap(boutBtn(1, 'shiro', 'K'));
    await settle();
    expect(writes).toHaveLength(1);
    expect(lastWrite().changed).toEqual(['bout:2']);
  });

  it('a bout the match already holds is not named when another bout is scored', async () => {
    await mount(team({
      subResults: [
        { position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: ['M'], winner: 'Osaka', decision: '' },
      ],
    }));
    await pointerTap(boutBtn(2, 'aka', 'D'));
    await settle();
    expect(lastWrite().changed).toEqual(['bout:3']);
  });

  it('a placeholder slot a stored bout holds is not a change to that bout', async () => {
    await mount(team({
      subResults: [
        { position: 1, sideA: '', sideB: '', ipponsA: ['M', '•'], ipponsB: [], winner: 'Kyoto', decision: '' },
      ],
    }));
    await pointerTap(boutBtn(1, 'shiro', 'K'));
    await settle();
    expect(lastWrite().changed).toEqual(['bout:2']);
  });
});

describe('engi editor', () => {
  // bc-evts stage 4: a running engi match is scored by flag taps (the
  // outbox's recordTap), never by a running score write; Save names what its
  // board holds that the server copy it agreed with does not.
  const engi = (over = {}) => ({
    id: 'm-engi', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compEngi: true,
    sideA: { id: 'p1', name: 'Ito - Abe' }, sideB: { id: 'p2', name: 'Ono - Sato' },
    ...over,
  });
  const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);

  it('a flag is a tap, not a write; Save names the flags and the result', async () => {
    await act(async () => {
      render(<EngiScoreEditorModal match={engi()} onClose={vi.fn()} onSubmit={onSubmit} />);
    });
    await click(screen.getByTestId('engi-aka-inc'));
    await settle();
    expect(writes).toHaveLength(0);
    expect(taps()).toEqual([{ kind: 'flag.add', pos: 0, side: 'a' }]);
    await finish('Save result');
    expect(lastWrite()).toMatchObject({ flagsA: 1, flagsB: 0, status: 'completed' });
    expect([...lastWrite().changed].sort()).toEqual(['flags', 'result']);
  });

  it('flags the server already holds are not named by the Save', async () => {
    const view = (m) => <EngiScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} />;
    let utils;
    await act(async () => { utils = render(view(engi())); });
    await act(async () => { utils.rerender(view(engi({ flagsA: 1 }))); });
    await finish('Save result');
    expect(lastWrite()).toMatchObject({ flagsA: 1, flagsB: 0, status: 'completed' });
    expect(lastWrite().changed).toEqual(['result']);
  });
});

describe('the start of a match', () => {
  it('names the result alone', () => {
    expect(startPatch().changed).toEqual(['result']);
  });
});

describe('a write applied in part', () => {
  // The server applied the write but kept some of its groups in the match's
  // history, because a newer change to them was recorded first. The editor's
  // flow carries on; a quiet note names what was kept.
  // bc-evts stage 3: the individual editor's write is its Finish (a running
  // match's points are taps), so its cases below finish the match.
  const note = () => screen.queryByTestId('kept-in-history-note');

  it('a Finish applied in part shows the note', async () => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve({ id: 'm-ind', status: 'completed', heldGroups: ['points'] });
    });
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await finish();
    expect(note()?.textContent).toBe("Kept in the match's history, not applied: points. A newer change to the same thing was recorded first.");
  });

  it('a team autosave applied in part names the bout, and the next clean save clears it', async () => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve(writes.length === 1
        ? { id: 'm-team', status: 'running', heldGroups: ['bout:2', 'encho'] }
        : { id: 'm-team', status: 'running' });
    });
    await mount({
      id: 'm-team', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
      compKind: 'team', teamSize: 3,
      sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
    });
    const row = () => [...document.querySelectorAll('.team-sub-match')][1];
    await pointerTap([...row().querySelectorAll('.team-sub-match__side--shiro button.ipt-btn')].find((b) => b.textContent === 'K'));
    await settle();
    expect(note()?.textContent).toBe("Kept in the match's history, not applied: bout 2 and overtime. A newer change to the same thing was recorded first.");
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap([...row().querySelectorAll('.team-sub-match__side--aka button.ipt-btn')].find((b) => b.textContent === 'M'));
    await settle();
    expect(note()).toBeNull();
  });

  // The closed-match rule (2026-10-15): a change made after the match was
  // finished is not applied, and the note says to use Correct result,
  // whether the rest of the write applied or not.
  it.each([
    ['applied in part', { id: 'm-ind', status: 'completed', heldGroups: ['points'], heldReason: 'match_closed' }],
    ['superseded', { applied: false, reason: 'superseded', heldGroups: ['points'], heldReason: 'match_closed' }],
  ])('a change turned away because the match is finished (%s) says to use Correct result', async (_how, answer) => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve(answer);
    });
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await finish();
    expect(note()?.textContent).toBe(
      "Kept in the match's history, not applied: points. The match is finished: to change the result, use Correct result.");
  });

  // An earlier-made finish that moved a later change to the history answers
  // APPLIED with displacedGroups and the match_closed reason: the note says
  // "Saved", never "not applied".
  it('a write that moved a later change to the history says Saved', async () => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve({ id: 'm-ind', status: 'completed', displacedGroups: ['points'], heldReason: 'match_closed' });
    });
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await finish();
    expect(note()?.textContent).toBe(
      "Saved. A later change to points was made after the match was finished, so it was moved to the match's history.");
  });

  // A later change moved for another reason (a representative's pick stamped after the
  // representative bout's removal) carries no reason, and the note words it plainly.
  it('a write that moved a later change to the history for no winner reason says only that', async () => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve({ id: 'm-ind', status: 'completed', displacedGroups: ['repPickB'] });
    });
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await finish();
    expect(note()?.textContent).toBe(
      "Saved. A later change to Shiro's pick for the representative bout was moved to the match's history.");
  });

  it('a superseded write shows the banner, not the note', async () => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve({ applied: false, reason: 'superseded', heldGroups: ['points'] });
    });
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await finish();
    expect(note()).toBeNull();
  });
});

// A value the editor ADOPTED from the server is the value it agrees with, and it is the
// last value the editor "sent" for that group, so:
//   - taking it back here is a change (B): the server keeps the other device's value
//     unless the write names the group, and the editor's mount-time value is not the
//     baseline any more;
//   - an edit of something else never names it (F): it is not a change, and naming it
//     would put the adopted value back under a newer stamp, over whatever was recorded
//     meanwhile.
// A value the editor HELD over a newer server value is neither (the existing tests above).
describe('an adopted value is agreed, never re-sent', () => {
  it('individual: a point adopted from another device and then taken back here is named (B)', async () => {
    // bc-evts stage 3: on a running individual match taking a point back is a
    // removal tap naming its mark; the Finish then names the points.
    const { rerender } = await mount(individual({ ipponsA: ['M'], markIds: { a: ['x.1'], b: [] } }));
    await act(async () => { rerender(editorFor(individual({ ipponsA: ['M'], ipponsB: ['K'], markIds: { a: ['x.1'], b: ['y.1'] } }))); });
    expect(slotWith('Shiro', 'K'), 'the point recorded elsewhere is shown').toBeTruthy();
    await pointerTap(slotWith('Shiro', 'K'));
    expect(window.API.recordTap.mock.calls.map((c) => c[2])).toEqual([{ kind: 'ippon.remove', pos: 0, side: 'b', target: 'y.1' }]);
    await finish();
    expect(lastWrite().ipponsB).toEqual([]);
    expect(lastWrite().changed, 'the server keeps the other device\'s point unless the write names the points').toContain('points');
  });

  it('individual: an unrelated edit after a point was adopted does not name the points (F)', async () => {
    const { rerender } = await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    const { id } = window.API.recordTap.mock.results[0].value;
    // Its own tap comes back, then another device scores Shiro's point.
    await act(async () => { rerender(editorFor(individual({ ipponsA: ['M'], markIds: { a: [id], b: [] }, scoreVersion: 1 }))); });
    await act(async () => { rerender(editorFor(individual({ ipponsA: ['M'], ipponsB: ['K'], markIds: { a: [id], b: ['y.1'] }, scoreVersion: 2 }))); });
    expect(slotWith('Shiro', 'K'), 'the point recorded elsewhere is shown').toBeTruthy();
    await startOvertime();
    await finish();
    expect(lastWrite().changed).toEqual(['result', 'encho']);
  });

  it('individual: an overtime count adopted and then switched off here is named (B)', async () => {
    const { rerender } = await mount(individual({ ipponsA: ['M'] }));
    await act(async () => { rerender(editorFor(individual({ ipponsA: ['M'], encho: { periodCount: 1 } }))); });
    await click(screen.getByTestId('scoring-modal-encho-checkbox'));
    expect(window.API.recordTap.mock.calls.map((c) => c[2])).toEqual([{ kind: 'encho.remove', pos: 0 }]);
    await finish();
    expect(lastWrite().encho).toBeUndefined();
    expect(lastWrite().changed).toContain('encho');
  });

  it('individual: a hantei verdict adopted from another device and then cancelled here names the points that carried it (B)', async () => {
    const tied = (over = {}) => individual({ ipponsA: ['M'], ipponsB: ['K'], ...over });
    const { rerender } = await mount(tied());
    // Another device records Yamada's win by hantei: the Ht mark rides in Aka's ippons.
    await act(async () => { rerender(editorFor(tied({ decidedByHantei: true, winner: { id: 'p1', name: 'Yamada' } }))); });
    await click(screen.getByTestId('scoring-modal-hantei-cancel'));
    await finish();
    expect(lastWrite().status).toBe('completed');
    expect(lastWrite().changed, 'the server keeps the Ht mark unless the write names the points it rides in').toContain('points');
    expect(lastWrite().changed).toContain('result');
  });

  // An adopt agrees a change group only for what it took. Agreeing moves BOTH baselines to
  // the server's value, so a group the adopt did not take whole (a point or a pick of
  // ours still being saved) would then read as unchanged when it is taken back.
  describe('an adopt that took only part of a group does not agree the whole', () => {
    // A recorded verdict whose winner the match cannot place (a legacy row): its points
    // carry no Ht mark, so the editor's markless running body can equal the baseline.
    const unplaced = (over = {}) => individual({ decidedByHantei: true, ...over });

    it('a hantei adopted while a point is still being saved: taking the point back is named (B)', async () => {
      // bc-evts stage 3: the point is a pending tap when the verdict arrives.
      const { rerender } = await mount(individual());
      await pointerTap(ipponBtn('aka', 'M'));
      const { id } = window.API.recordTap.mock.results[0].value;
      // Another device's verdict arrives before the tap's echo, then the point lands.
      await act(async () => { rerender(editorFor(unplaced())); });
      await act(async () => { rerender(editorFor(unplaced({ ipponsA: ['M'], markIds: { a: [id], b: [] }, scoreVersion: 1 }))); });
      // The armed verdict locks the slots: cancel it, then take the point back.
      await click(screen.getByTestId('scoring-modal-hantei-cancel'));
      await pointerTap(slotWith('Aka', 'M'));
      expect(window.API.recordTap.mock.calls.at(-1)[2]).toEqual({ kind: 'ippon.remove', pos: 0, side: 'a', target: id });
      // 0-0 can only finish as a draw.
      await click(screen.getByTestId('scoring-modal-mark-draw'));
      await finish();
      expect(lastWrite().ipponsA).toEqual([]);
      expect(lastWrite().changed, 'the server keeps the point unless the write names the points').toContain('points');
    });

    describe('the two picks of a representative bout are one group', () => {
      // bc-evts stage 3: on a running bout each pick is a pool.rep tap.
      const repBout = (over = {}) => individual({
        id: 'Pool 1-DH-1', sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
        repIsTeam: true, repRosterA: ['Kato', 'Mori'], repRosterB: ['Sato', 'Ito'], ...over,
      });
      const pick = (testId, value) => act(async () => { fireEvent.change(screen.getByTestId(testId), { target: { value } }); });

      it('a pick put back on one side while the other side follows the server is its own tap (B)', async () => {
        const { rerender } = await mount(repBout({ repPlayerB: 'Sato' }));
        await pick('rep-shiro-select', 'Ito');
        await act(async () => { rerender(editorFor(repBout({ repPlayerA: 'Mori', repPlayerB: 'Sato' }))); });
        await act(async () => { rerender(editorFor(repBout({ repPlayerA: 'Mori', repPlayerB: 'Ito' }))); });
        await pick('rep-shiro-select', 'Sato');
        expect(window.API.recordTap.mock.calls.map((c) => c[2])).toEqual([
          { kind: 'pool.rep', pos: 0, side: 'b', name: 'Ito' },
          { kind: 'pool.rep', pos: 0, side: 'b', name: 'Sato' },
        ]);
        expect(screen.getByTestId('rep-aka-select').value).toBe('Mori');
        expect(screen.getByTestId('rep-shiro-select').value).toBe('Sato');
      });

      it('both picks adopted in one update are agreed: the Finish does not name them (F)', async () => {
        const { rerender } = await mount(repBout({ repPlayerA: 'Kato', repPlayerB: 'Sato' }));
        await act(async () => { rerender(editorFor(repBout({ repPlayerA: 'Mori', repPlayerB: 'Ito' }))); });
        expect(screen.getByTestId('rep-aka-select').value).toBe('Mori');
        expect(screen.getByTestId('rep-shiro-select').value).toBe('Ito');
        await pointerTap(ipponBtn('aka', 'M'));
        await finish();
        expect(lastWrite().changed).not.toContain('rep');
      });
    });
  });

  it('engi: flags adopted from another device and then set back here are named (B)', async () => {
    // A draft (the match not running: a running one is scored by taps, bc-evts
    // stage 4): counts adopted from another device, then set back here to the
    // ones the editor opened with, are a change the Save names.
    const engiMatch = (over = {}) => ({
      id: 'm-engi', compId: 'comp1', status: 'scheduled', phase: 'pool', poolName: 'Pool 1', court: 'A',
      compEngi: true, flagsA: 2, flagsB: 1,
      sideA: { id: 'p1', name: 'Ito - Abe' }, sideB: { id: 'p2', name: 'Ono - Sato' },
      ...over,
    });
    const view = (m) => <EngiScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} />;
    let utils;
    await act(async () => { utils = render(view(engiMatch())); });
    await act(async () => { utils.rerender(view(engiMatch({ flagsA: 1, flagsB: 2 }))); });
    expect(screen.getByTestId('engi-aka-count').textContent, 'the count recorded elsewhere is shown').toBe('1');
    await click(screen.getByTestId('engi-aka-inc'));
    await click(screen.getByTestId('engi-shiro-dec'));
    await finish('Save result');
    expect(lastWrite()).toMatchObject({ flagsA: 2, flagsB: 1 });
    expect(lastWrite().changed).toContain('flags');
  });

  describe('team', () => {
    const team = (over = {}) => ({
      id: 'm-team', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
      compKind: 'team', teamSize: 3,
      sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
      ...over,
    });
    const bout = (position, over = {}) => ({ position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: '', ...over });
    const kyotoWins = (position) => bout(position, { ipponsA: ['M'], winner: 'Kyoto' });
    const osakaWins = (position) => bout(position, { ipponsB: ['K'], winner: 'Osaka' });
    const row = (i) => [...document.querySelectorAll('.team-sub-match')][i];
    const boutBtn = (i, side, letter) => [...row(i).querySelectorAll(`.team-sub-match__side--${side} button.ipt-btn`)].find((b) => b.textContent === letter);
    const placed = (i, color) => [...row(i).querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)].map((b) => b.textContent.trim());
    const markBtn = (i, color, letter) => [...row(i).querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)].find((b) => b.textContent.trim() === letter);
    const mountTeam = async (match) => {
      let utils;
      await act(async () => { utils = render(editorFor(match)); });
      return (m) => act(async () => { utils.rerender(editorFor(m)); });
    };

    it('a bout adopted from another device and then cleared here is named (B)', async () => {
      const rerenderWith = await mountTeam(team());
      await rerenderWith(team({ subResults: [osakaWins(2)] }));
      expect(placed(1, 'shiro'), 'the bout recorded elsewhere is shown').toContain('K');
      await pointerTap(markBtn(1, 'shiro', 'K'));
      await settle();
      expect(placed(1, 'shiro')).not.toContain('K');
      expect(lastWrite().changed, 'the server keeps the other device\'s bout unless the write names it').toContain('bout:2');
    });

    it('scoring one bout after another was adopted names the scored bout alone (F)', async () => {
      const rerenderWith = await mountTeam(team());
      await pointerTap(boutBtn(0, 'shiro', 'K'));
      await settle();
      expect(lastWrite().changed).toEqual(['bout:1']);
      // Its own write comes back, then another device scores bout 2.
      await rerenderWith(team({ subResults: [osakaWins(1)] }));
      await rerenderWith(team({ subResults: [osakaWins(1), kyotoWins(2)] }));
      expect(placed(1, 'aka'), 'the bout recorded elsewhere is shown').toContain('M');
      await pointerTap(boutBtn(2, 'aka', 'D'));
      await settle();
      expect(lastWrite().changed).toEqual(['bout:3']);
    });

    it('a bout the operator holds is named and a bout adopted beside it is not (held and agreed)', async () => {
      const rerenderWith = await mountTeam(team());
      await pointerTap(boutBtn(0, 'shiro', 'K'));
      await settle();
      // Another device moves bout 1 (the operator keeps the point struck here) and scores bout 2.
      await rerenderWith(team({ subResults: [kyotoWins(1), kyotoWins(2)] }));
      expect(placed(0, 'shiro'), 'the point struck here is still shown').toContain('K');
      expect(placed(1, 'aka'), 'the bout recorded elsewhere is shown').toContain('M');
      await startOvertime();
      await settle();
      expect(lastWrite().changed).toContain('bout:1');
      expect(lastWrite().changed, 'bout 2 is the other device\'s, adopted and not changed here').not.toContain('bout:2');
    });

    it('a bout whose point was struck and taken back here stays the other device\'s (held, not agreed)', async () => {
      // The saves are queued (offline): none has landed, so the row's edit stamp stands.
      window.API.recordScore.mockImplementation((_c, _m, patch) => { writes.push(patch); return Promise.resolve({ queued: true }); });
      const rerenderWith = await mountTeam(team());
      await pointerTap(boutBtn(0, 'shiro', 'K'));
      await settle();
      await pointerTap(markBtn(0, 'shiro', 'K'));
      await settle();
      // Another device scores bout 1 meanwhile: the row stays the operator's, which reads as it was.
      await rerenderWith(team({ subResults: [kyotoWins(1)] }));
      expect(placed(0, 'aka'), 'the row is still the operator\'s').not.toContain('M');
      await startOvertime();
      await settle();
      expect(lastWrite().changed, 'an edit held over a newer value is not put back').toEqual(['encho']);
    });

    it('an overtime count adopted from another device and then switched off here is named (B)', async () => {
      const rerenderWith = await mountTeam(team());
      await rerenderWith(team({ encho: { periodCount: 1 } }));
      await click(screen.getByTestId('scoring-modal-encho-checkbox'));
      await settle();
      expect(lastWrite().encho).toBeUndefined();
      expect(lastWrite().changed).toContain('encho');
    });
  });
});
