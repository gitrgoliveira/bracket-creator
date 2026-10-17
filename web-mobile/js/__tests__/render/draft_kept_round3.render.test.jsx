// Stage 5 review 3, the kept draft's remaining edges.
//
// 1. This editor's own Start, refused when its answer comes (held as
//    superseded, refused for the clock, or turned away), is said beside the
//    board, the board falls back to the server's status with Start offered
//    again and the draft still shown, and a later start by another device
//    keeps the draft.
// 2. A correction draft on a finished match is kept, as D1 keeps a pre-match
//    one, when another device reopens the match: "This match was reopened on
//    another device...", Save sends its changes as taps, in all three editors.
// 3. The dropped note goes with the first edit of a new draft.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, bout, letterBtn, shiroSide, marks, NOW,
} from '../helpers/individual_editor.js';
import { engiBout, counts, btn } from '../helpers/engi_editor.js';
import { teamBout, boutRow, ipponBtn, marks as boutMarks } from '../helpers/team_editor.js';
import { START_SUPERSEDED_MESSAGE } from '../../start_match.jsx';

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
const refusedNote = () => screen.queryByTestId('start-refused-notice');
const button = (text) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const saveTwice = async () => { await tap(screen.getByTestId('draft-kept-save')); await tap(screen.getByTestId('draft-kept-save')); };
// A Start the outbox holds, whose answer the test gives.
const heldStart = () => {
  let answer;
  let refuse;
  const settled = new Promise((res, rej) => { answer = res; refuse = rej; });
  settled.catch(() => {});
  const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled }));
  return { onSubmit, answer: (b) => act(async () => { answer(b); await settled; }), refuse: (e) => act(async () => { refuse(e); await Promise.resolve(); await Promise.resolve(); }) };
};
const SUPERSEDED = { applied: false, reason: 'superseded', heldGroups: ['result'] };

describe('1: an own Start refused when its answer comes', () => {
  it('individual: said beside the board, Start offered again, the draft shown, and kept when another device starts', async () => {
    const s = heldStart();
    const scheduled = bout({ status: 'scheduled', scoreVersion: 2 });
    const r = render(el(scheduled, { onSubmit: s.onSubmit }));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    await tap(button('Start match'));
    expect(button('Start match'), 'the board shows the start at once').toBeUndefined();
    await s.answer(SUPERSEDED);
    expect(refusedNote()?.textContent).toContain(START_SUPERSEDED_MESSAGE);
    expect(button('Start match'), 'Start is offered again').toBeDefined();
    expect(marks(r.container).join(' '), 'the draft is still shown').toMatch(/M/);
    r.rerender(el(bout({ status: 'running', ipponsA: ['K'], modifiedAt: NOW + 50, scoreVersion: 4, markIds: { a: ['o1'] } }), { onSubmit: s.onSubmit }));
    expect(kept(), 'another device\'s start keeps the draft').not.toBeNull();
    expect(refusedNote(), 'the refusal note goes with the match running').toBeNull();
  });

  it('individual: a start turned away with the server\'s sentence says it', async () => {
    const s = heldStart();
    const r = render(el(bout({ status: 'scheduled', scoreVersion: 2 }), { onSubmit: s.onSubmit }));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    await tap(button('Start match'));
    await s.refuse(new Error('Shiaijo A is busy with another match.'));
    expect(refusedNote()?.textContent).toContain('Shiaijo A is busy with another match.');
    expect(button('Start match')).toBeDefined();
  });

  it('team: said beside the sheet, Start offered again, the draft shown', async () => {
    const s = heldStart();
    const r = render(el(teamBout({ status: 'scheduled', phase: 'pool' }), { onSubmit: s.onSubmit }));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(button('Start match'));
    await s.answer(SUPERSEDED);
    expect(refusedNote()?.textContent).toContain(START_SUPERSEDED_MESSAGE);
    expect(button('Start match')).toBeDefined();
    expect(boutMarks(r.container, 0, 'aka')).toEqual(['M']);
    r.rerender(el(teamBout({ status: 'running', phase: 'pool', modifiedAt: NOW + 50, scoreVersion: 6 }), { onSubmit: s.onSubmit }));
    expect(kept()).not.toBeNull();
  });
});

describe('2: a correction draft is kept when another device reopens the match', () => {
  const REOPENED = 'This match was reopened on another device. Your unsaved entries are still here: Save adds them to the match, Discard drops them.';

  it('individual', async () => {
    const done = bout({ status: 'completed', ipponsA: ['K'], winner: 'Yamada', modifiedAt: NOW, scoreVersion: 5, markIds: { a: ['o1'] } });
    const r = render(el(done));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    r.rerender(el(bout({ status: 'running', ipponsA: ['K'], modifiedAt: NOW + 50, scoreVersion: 6, markIds: { a: ['o1'] } })));
    expect(kept()?.textContent).toContain(REOPENED);
    expect(marks(r.container).join(' ')).toMatch(/M/);
    await saveTwice();
    expect(events()).toEqual([{ kind: 'ippon.add', pos: 0, side: 'b', letter: 'M' }]);
    expect(kept()).toBeNull();
  });

  it('engi', async () => {
    const r = render(el(engiBout({ status: 'completed', flagsA: 3, winner: 'Aka One - Aka Two', scoreVersion: 5 })));
    await tap(btn(r.container, 'engi-shiro-inc'));
    r.rerender(el(engiBout({ status: 'running', flagsA: 3, modifiedAt: NOW + 50, scoreVersion: 6 })));
    expect(kept()?.textContent).toContain(REOPENED);
    expect(counts(r.container)).toEqual([1, 3]);
    await saveTwice();
    expect(events()).toEqual([expect.objectContaining({ kind: 'flag.add', side: 'b' })]);
  });

  it('team', async () => {
    const rows = [boutRow(1, { ipponsB: ['K'], winner: 'Osaka' }), boutRow(2, { decision: 'hikiwake' }), boutRow(3, { decision: 'hikiwake' })];
    const r = render(el(teamBout({ status: 'completed', phase: 'pool', winner: 'Osaka', scoreVersion: 7, subResults: rows })));
    await tap(ipponBtn(r.container, 1, 'aka', 'M'));
    r.rerender(el(teamBout({ status: 'running', phase: 'pool', modifiedAt: NOW + 50, scoreVersion: 8, subResults: rows })));
    expect(kept()?.textContent).toContain(REOPENED);
    expect(boutMarks(r.container, 1, 'aka')).toEqual(['M']);
    await saveTwice();
    expect(events()).toEqual(expect.arrayContaining([{ kind: 'ippon.add', pos: 2, side: 'a', letter: 'M' }]));
  });
});

describe('3: the dropped note goes with the first edit of a new draft', () => {
  it('individual: sent back elsewhere, then a new point', async () => {
    const r = render(el(bout({ status: 'scheduled', scoreVersion: 2 })));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    r.rerender(el(bout({ status: 'running', ipponsA: ['K'], modifiedAt: NOW + 50, scoreVersion: 4, markIds: { a: ['o1'] } })));
    r.rerender(el(bout({ status: 'scheduled', ipponsA: ['K'], modifiedAt: NOW + 90, scoreVersion: 5, markIds: { a: ['o1'] } })));
    expect(dropped()).not.toBeNull();
    await tap(letterBtn(shiroSide(r.container), 'D'));
    expect(dropped()).toBeNull();
  });
});
