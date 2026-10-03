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
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap } from '../helpers/tap_events.js';
import { startPatch } from '../../admin_schedule_score_editor.jsx';

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
  it('a point names the points alone', async () => {
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await settle();
    expect(writes).toHaveLength(1);
    expect(lastWrite().changed).toEqual(['points']);
  });

  it('overtime names overtime alone', async () => {
    await mount(individual());
    await startOvertime();
    await settle();
    expect(lastWrite().changed).toEqual(['encho']);
  });

  it('Finish names the result, and the points it has not seen stored', async () => {
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await settle();
    await finish();
    expect(lastWrite().status).toBe('completed');
    expect(lastWrite().changed).toEqual(['points', 'result']);
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
    // The draw rides only the finishing write, so the running save it
    // schedules changes nothing.
    expect(writes.every((w) => w.changed.length === 0)).toBe(true);
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

  it('an autosave does not name a group the match moved on while the editor held an edit', async () => {
    // A point is struck, and before its save goes out another device records
    // overtime. The editor keeps its unsaved board (it does not re-seed over
    // an edit), so it still shows no overtime: its save must not put "no
    // overtime" back.
    const { rerender } = await mount(individual());
    await pointerTap(ipponBtn('shiro', 'K'));
    await act(async () => { rerender(editorFor(individual({ encho: { periodCount: 1 } }))); });
    await settle();
    expect(writes).toHaveLength(1);
    expect(lastWrite().encho).toBeUndefined();
    expect(lastWrite().changed).toEqual(['points']);
  });

  it('an editor that followed a change elsewhere does not name it back', async () => {
    // Nothing unsaved: the editor re-seeds to the overtime recorded
    // elsewhere, and its next point names the points alone.
    const { rerender } = await mount(individual());
    await act(async () => { rerender(editorFor(individual({ encho: { periodCount: 1 } }))); });
    await pointerTap(ipponBtn('shiro', 'K'));
    await settle();
    expect(lastWrite().encho).toEqual({ periodCount: 1 });
    expect(lastWrite().changed).toEqual(['points']);
  });

  it('a point taken back before the first write comes back still names the points', async () => {
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await settle();
    expect(lastWrite().changed).toEqual(['points']);
    // The copy the editor renders from has not caught up with the write: it
    // still reads 0-0, exactly what the board reads once the M is removed.
    await pointerTap(slotWith('Aka', 'M'));
    await settle();
    expect(writes).toHaveLength(2);
    expect(lastWrite().ipponsA).toEqual([]);
    expect(lastWrite().changed).toEqual(['points']);
  });

  it('a placeholder slot the stored scoreline holds is not a change', async () => {
    // The server may hold an empty slot as "•"; the editor holds no slot.
    await mount(individual({ ipponsA: ['M', '•'] }));
    await startOvertime();
    await settle();
    expect(lastWrite().changed).toEqual(['encho']);
  });

  it('after its own write comes back, an untouched group stays unnamed', async () => {
    const { rerender } = await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await settle();
    await act(async () => { rerender(editorFor(individual({ ipponsA: ['M'] }))); });
    await startOvertime();
    await settle();
    expect(lastWrite().changed).toEqual(['encho']);
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
  const engi = () => ({
    id: 'm-engi', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
    compEngi: true,
    sideA: { id: 'p1', name: 'Ito - Abe' }, sideB: { id: 'p2', name: 'Ono - Sato' },
  });

  it('a flag names the flags alone', async () => {
    await act(async () => {
      render(<EngiScoreEditorModal match={engi()} onClose={vi.fn()} onSubmit={onSubmit} />);
    });
    await click(screen.getByTestId('engi-aka-inc'));
    await settle();
    expect(writes).toHaveLength(1);
    expect(lastWrite().changed).toEqual(['flags']);
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
  const note = () => screen.queryByTestId('kept-in-history-note');

  it('an autosave applied in part shows the note, and the next clean save clears it', async () => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve(writes.length === 1 ? { id: 'm-ind', status: 'running', heldGroups: ['points'] } : { id: 'm-ind', status: 'running' });
    });
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await settle();
    expect(note()?.textContent).toBe("Kept in the match's history, not applied: points. A newer change to the same thing was recorded first.");
    await startOvertime();
    await settle();
    expect(note()).toBeNull();
  });

  it('a team autosave applied in part names the bout', async () => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve({ id: 'm-team', status: 'running', heldGroups: ['bout:2', 'encho'] });
    });
    await mount({
      id: 'm-team', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
      compKind: 'team', teamSize: 3,
      sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
    });
    const row = [...document.querySelectorAll('.team-sub-match')][1];
    await pointerTap([...row.querySelectorAll('.team-sub-match__side--shiro button.ipt-btn')].find((b) => b.textContent === 'K'));
    await settle();
    expect(note()?.textContent).toBe("Kept in the match's history, not applied: bout 2 and overtime. A newer change to the same thing was recorded first.");
  });

  it('a superseded write shows the banner, not the note', async () => {
    window.API.recordScore.mockImplementation((_c, _m, patch) => {
      writes.push(patch);
      return Promise.resolve({ applied: false, reason: 'superseded', heldGroups: ['points'] });
    });
    await mount(individual());
    await pointerTap(ipponBtn('aka', 'M'));
    await settle();
    expect(note()).toBeNull();
  });
});
