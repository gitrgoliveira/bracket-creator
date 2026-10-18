// Operator decision D1 (a), stage 5 review 2: the edges of a kept draft.
//
// 1. Save on a bout the server holds no row for (a kachinuki first bout) writes
//    the row from the server's copy, position and fighters only, never the
//    draft's points: the draft's points go as taps, once.
// 2. A kept draft does not outlive the match leaving running on another device
//    (finished or sent back): the editor drops it, shows the server's copy and
//    says so, so a Save correction can never record the draft over the result.
// 3. This editor's own Start that is refused does not count as its own start:
//    when another device then starts the match, the draft is kept with its note.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, bout, letterBtn, shiroSide, marks, NOW,
} from '../helpers/individual_editor.js';
import { engiBout, counts, btn } from '../helpers/engi_editor.js';
import { teamBout, kachinukiBout, boutRow, ipponBtn, marks as boutMarks } from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const pending = () => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) });
const el = (match, props = {}) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn(pending)} password="pw" variant="inline" {...props} />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const events = () => API.recordTap.mock.calls.map((c) => c[2]);
const kept = () => screen.queryByTestId('draft-kept-notice');
const dropped = () => screen.queryByTestId('draft-dropped-notice');
const button = (text) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const saveTwice = async () => { await tap(screen.getByTestId('draft-kept-save')); await tap(screen.getByTestId('draft-kept-save')); };

describe('1: Save on a bout the server holds no row for counts each point once', () => {
  it('a kachinuki first bout: the row write carries no points, the M goes as one tap', async () => {
    const onSubmit = vi.fn(pending);
    const r = render(el(kachinukiBout({ status: 'scheduled', scoreVersion: 2 }), { onSubmit }));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    r.rerender(el(kachinukiBout({ status: 'running', subResults: [], modifiedAt: NOW + 50, scoreVersion: 3 }), { onSubmit }));
    await saveTwice();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const row = onSubmit.mock.calls[0][0].subResults.find((s) => s.position === 1);
    expect(row.ipponsA || [], 'the row write is the server\'s row, not the draft').toEqual([]);
    expect(row.ipponsB || []).toEqual([]);
    expect(row.winner || '').toBe('');
    expect(events()).toEqual([{ kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' }]);
  });
});

describe('2: a kept draft is dropped, and said so, when the match leaves running elsewhere', () => {
  const DROPPED = 'This match was finished or sent back to the queue on another device. Your unsaved entries were dropped.';

  it('individual: the finished match shows the server\'s score', async () => {
    const r = render(el(bout({ status: 'scheduled', scoreVersion: 2 })));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    r.rerender(el(bout({ status: 'running', ipponsA: ['K'], modifiedAt: NOW + 50, scoreVersion: 4, markIds: { a: ['o1'] } })));
    expect(kept()).not.toBeNull();
    r.rerender(el(bout({ status: 'completed', ipponsA: ['K', 'K'], winner: 'Yamada', modifiedAt: NOW + 90, scoreVersion: 6, markIds: { a: ['o1', 'o2'] } })));
    expect(kept()).toBeNull();
    expect(dropped()?.textContent).toContain(DROPPED);
    expect(marks(r.container).join(' '), 'the draft\'s M is gone').not.toMatch(/M/);
    expect(marks(r.container).join(' ')).toMatch(/K/);
  });

  it('engi: the counts are the server\'s', async () => {
    const r = render(el(engiBout({ status: 'scheduled' })));
    await tap(btn(r.container, 'engi-shiro-inc'));
    r.rerender(el(engiBout({ status: 'running', flagsA: 1, modifiedAt: NOW + 50, scoreVersion: 5 })));
    expect(kept()).not.toBeNull();
    r.rerender(el(engiBout({ status: 'completed', flagsA: 3, modifiedAt: NOW + 90, scoreVersion: 7, winner: 'Aka One - Aka Two' })));
    expect(dropped()).not.toBeNull();
    expect(counts(r.container)).toEqual([0, 3]);
  });

  it('team: the bouts are the server\'s', async () => {
    const r = render(el(teamBout({ status: 'scheduled', phase: 'pool' })));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    r.rerender(el(teamBout({ status: 'running', phase: 'pool', modifiedAt: NOW + 50, scoreVersion: 6 })));
    expect(kept()).not.toBeNull();
    r.rerender(el(teamBout({ status: 'completed', phase: 'pool', winner: 'Osaka', modifiedAt: NOW + 90, scoreVersion: 8,
      subResults: [boutRow(1, { ipponsB: ['K'], winner: 'Osaka' }), boutRow(2), boutRow(3)] })));
    expect(dropped()).not.toBeNull();
    expect(boutMarks(r.container, 0, 'aka'), 'the draft\'s M is gone').toEqual([]);
    expect(boutMarks(r.container, 0, 'shiro')).toEqual(['K']);
  });
});

describe('3: a refused own Start is not this editor\'s start', () => {
  it('individual: started elsewhere after the refusal, the draft is kept', async () => {
    const onSubmit = vi.fn(async () => null); // the host reported the refusal
    const r = render(el(bout({ status: 'scheduled', scoreVersion: 2 }), { onSubmit }));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    await tap(button('Start match'));
    r.rerender(el(bout({ status: 'running', ipponsA: ['K'], modifiedAt: NOW + 50, scoreVersion: 4, markIds: { a: ['o1'] } }), { onSubmit }));
    expect(kept()).not.toBeNull();
  });

  it('team: started elsewhere after the refusal, the draft is kept', async () => {
    const onSubmit = vi.fn(async () => null);
    const r = render(el(teamBout({ status: 'scheduled', phase: 'pool' }), { onSubmit }));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(button('Start match'));
    r.rerender(el(teamBout({ status: 'running', phase: 'pool', modifiedAt: NOW + 50, scoreVersion: 6 }), { onSubmit }));
    expect(kept()).not.toBeNull();
  });
});

// Stage 5 review 9, m2: a draft kept when another device reopens a withdrawal
// match shows what Save sends. The old ruling's circles are no entry of the
// operator's and Save never sends them as points, so the kept draft shows the
// reopened copy with the operator's own changes over it.
describe('4: a draft kept over a reopen elsewhere shows what Save sends', () => {
  const decided = bout({
    id: 'm-r1-0', phase: 'knockout', round: 'Round 1', status: 'completed', ipponsA: ['○', '○'], ipponsB: [],
    decision: 'fusenpai', decisionBy: 'shiro', winner: { id: 'p1', name: 'Yamada' }, scoreVersion: 2,
  });
  const reopened = bout({ id: 'm-r1-0', phase: 'knockout', round: 'Round 1', status: 'running', ipponsA: [], ipponsB: [], modifiedAt: NOW + 10, scoreVersion: 3 });

  it('individual: the old ruling\'s circles go, the typed M stays, and Save sends the M alone', async () => {
    const r = render(el(decided));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    r.rerender(el(reopened));
    expect(kept()).not.toBeNull();
    expect(marks(r.container)).toEqual(['Shiro slot 1: remove M']);
    await saveTwice();
    expect(events()).toEqual([{ kind: 'ippon.add', pos: 0, side: 'b', letter: 'M' }]);
    expect(marks(r.container)).toEqual(['Shiro slot 1: remove M']);
  });
});
