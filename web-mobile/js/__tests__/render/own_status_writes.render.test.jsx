// Stage 5 review 4, P8, P8b and P9b: the editor's own status writes and its
// kept draft, through the ONE owner (own_status.jsx, useOwnStatus).
//
// P8/P8b: an own Start is out, the feed shows the match running from another
// device, then the Start is turned down: the draft the Start carried is kept
// with D1's note (another device did start it), no refusal note stands over
// the running board, and when the match then finishes the draft is dropped
// with its note and the editor shows the server's result, never the draft.
// P9b: while an own Start is out, Start is not offered again, a restamp of
// the feed included; once it is turned down Start is offered with the reason,
// and a second Start that lands is this editor's own: no "another device"
// note, nothing sent twice.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, bout, letterBtn, shiroSide, marks, NOW,
} from '../helpers/individual_editor.js';
import { teamBout, boutRow, ipponBtn, marks as boutMarks } from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const el = (m, onSubmit) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const button = (t) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === t);
const kept = () => screen.queryByTestId('draft-kept-notice');
const dropped = () => screen.queryByTestId('draft-dropped-notice');
const refusedNote = () => screen.queryByTestId('start-refused-notice');
const SUP = { applied: false, reason: 'superseded', heldGroups: ['result'] };
const starts = (n) => {
  const answers = [];
  const settles = Array.from({ length: n }, () => new Promise((res) => { answers.push(res); }));
  let i = 0;
  const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: settles[i++] }));
  return { onSubmit, answer: (k, b) => act(async () => { answers[k](b); await settles[k]; }) };
};

describe('P8: an own Start turned down after another device started the match', () => {
  it('individual: the draft is kept with the note; the finish drops it with its note and shows the result', async () => {
    const s = starts(1);
    const r = render(el(bout({ status: 'scheduled', scoreVersion: 2 }), s.onSubmit));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    await tap(button('Start match'));
    r.rerender(el(bout({ status: 'running', ipponsA: ['K'], modifiedAt: NOW + 50, scoreVersion: 4, markIds: { a: ['o1'] } }), s.onSubmit));
    await s.answer(0, SUP);
    expect(kept(), 'the start was another device\'s: the draft is kept').not.toBeNull();
    expect(refusedNote(), 'no refusal note over a running match').toBeNull();
    expect(marks(r.container).join(' ')).toMatch(/M/);
    r.rerender(el(bout({ status: 'completed', ipponsA: ['K', 'K'], winner: 'Yamada', modifiedAt: NOW + 90, scoreVersion: 7, markIds: { a: ['o1', 'o2'] } }), s.onSubmit));
    expect(dropped()).not.toBeNull();
    expect(marks(r.container).join(' '), 'the finished match shows the result, not the draft').not.toMatch(/M/);
    r.rerender(el(bout({ status: 'completed', ipponsA: ['K', 'K'], winner: 'Yamada', modifiedAt: NOW + 95, scoreVersion: 8, markIds: { a: ['o1', 'o2'] } }), s.onSubmit));
    expect(marks(r.container).join(' ')).not.toMatch(/M/);
  });

  it('team (P8b): the same', async () => {
    const s = starts(1);
    const r = render(el(teamBout({ status: 'scheduled', phase: 'pool' }), s.onSubmit));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(button('Start match'));
    r.rerender(el(teamBout({ status: 'running', phase: 'pool', modifiedAt: NOW + 50, scoreVersion: 6 }), s.onSubmit));
    await s.answer(0, SUP);
    expect(kept()).not.toBeNull();
    expect(refusedNote()).toBeNull();
    expect(boutMarks(r.container, 0, 'aka')).toEqual(['M']);
    const rows = [boutRow(1, { ipponsB: ['K'], winner: 'Osaka' }), boutRow(2, { decision: 'hikiwake' }), boutRow(3, { decision: 'hikiwake' })];
    r.rerender(el(teamBout({ status: 'completed', phase: 'pool', winner: 'Osaka', modifiedAt: NOW + 90, scoreVersion: 9, subResults: rows }), s.onSubmit));
    expect(dropped()).not.toBeNull();
    expect(boutMarks(r.container, 0, 'aka')).toEqual([]);
    expect(boutMarks(r.container, 0, 'shiro')).toEqual(['K']);
  });
});

describe('P9b: one own Start at a time', () => {
  it('individual: no second Start while one is out; after its refusal, the next Start is the editor\'s own', async () => {
    const s = starts(2);
    const r = render(el(bout({ status: 'scheduled', scoreVersion: 2 }), s.onSubmit));
    await tap(button('Start match'));
    r.rerender(el(bout({ status: 'scheduled', scoreVersion: 3, modifiedAt: NOW + 10 }), s.onSubmit));
    expect(button('Start match'), 'the first Start is still out').toBeUndefined();
    await s.answer(0, SUP);
    expect(button('Start match'), 'turned down: Start is offered again').toBeDefined();
    expect(refusedNote()).not.toBeNull();
    await tap(letterBtn(shiroSide(r.container), 'K'));
    await tap(button('Start match'));
    expect(s.onSubmit.mock.calls[1][0].ipponsB).toEqual(['K']);
    await s.answer(1, { applied: true });
    r.rerender(el(bout({ status: 'running', ipponsB: ['K'], modifiedAt: NOW + 60, scoreVersion: 5, markIds: { b: ['k1'] } }), s.onSubmit));
    expect(kept(), 'its own start: no "another device" note').toBeNull();
    expect(API.recordTap, 'nothing sent twice').not.toHaveBeenCalled();
  });
});
