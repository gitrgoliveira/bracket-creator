// bc-ltap, rewritten for bc-evts stage 3: on the individual editor, taking a
// mark off and tapping it back while the first change is still on its way
// must never let that change's echo wipe the second. A running match is now
// scored by taps: each is its own event in the outbox, and the board is the
// server's copy with this device's pending taps folded on top, so an echo of
// an earlier change cannot take a later tap off the board.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, akaSide, marks, NOW, API } from '../helpers/individual_editor.js';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const el = (match, onSubmit = vi.fn()) => (
  <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" variant="inline" />
);
const tap = async (node) => { await act(async () => { fireEvent.click(node); }); };
const slot = (c, label) => c.querySelector(`[aria-label="${label}"]`);

describe('a tap made while an earlier change is on its way is never wiped by that change\'s echo (bc-ltap)', () => {
  it('remove a mark, tap it back before the removal\'s echo: the mark stays', async () => {
    const r = render(el(bout({ ipponsA: ['M'], markIds: { a: ['b:0:a:M:1'], b: [] } })));
    await tap(slot(r.container, marks(r.container)[0]));
    expect(marks(r.container), 'the removal shows').toHaveLength(0);
    await tap(letterBtn(akaSide(r.container), 'M'));
    expect(marks(r.container), 'the tap shows').toHaveLength(1);
    // The removal's echo (no mark) arrives while both taps are pending: the
    // removal finds nothing left to take, the add rides on top.
    await act(async () => { r.rerender(el(bout({ ipponsA: [], markIds: { a: [], b: [] }, scoreVersion: 4, modifiedAt: NOW + 10 }))); });
    expect(marks(r.container), 'the echo of the earlier removal does not wipe the newer tap').toHaveLength(1);
    expect(API.recordTap.mock.calls.map((c) => c[2].kind)).toEqual(['ippon.remove', 'ippon.add']);
    expect(API.recordScore).not.toHaveBeenCalled();
  });

  it('add a mark, take it off before the add\'s echo: the board stays empty', async () => {
    const r = render(el(bout()));
    await tap(letterBtn(akaSide(r.container), 'M'));
    await tap(slot(r.container, marks(r.container)[0]));
    expect(marks(r.container)).toHaveLength(0);
    const [add, removal] = outbox.taps('c1', 'm1');
    expect(removal).toMatchObject({ kind: 'ippon.remove', target: add.id });
    // The add's echo arrives, the removal still pending: still empty.
    await act(async () => { r.rerender(el(bout({ ipponsA: ['M'], markIds: { a: [add.id], b: [] }, scoreVersion: 4, modifiedAt: add.stamp }))); });
    expect(marks(r.container)).toHaveLength(0);
  });

  it('a change made elsewhere after the last tap is still followed', async () => {
    const r = render(el(bout()));
    await tap(letterBtn(akaSide(r.container), 'M'));
    const [add] = outbox.taps('c1', 'm1');
    await act(async () => { outbox.ack('c1', 'm1', bout({ ipponsA: ['M'], markIds: { a: [add.id], b: [] }, scoreVersion: 4, modifiedAt: add.stamp }), {}); });
    // Another device then struck Shiro's K.
    await act(async () => {
      r.rerender(el(bout({ ipponsA: ['M'], ipponsB: ['K'], markIds: { a: [add.id], b: ['devB.1'] }, scoreVersion: 5, modifiedAt: add.stamp + 100 })));
    });
    expect(marks(r.container).sort()).toEqual([
      expect.stringMatching(/^Aka slot \d: remove M$/),
      expect.stringMatching(/^Shiro slot \d: remove K$/),
    ]);
  });
});

describe('a match that is not running keeps nothing against another device', () => {
  it('a scheduled match: a net-zero fiddle holds nothing, so another device\'s copy is adopted', async () => {
    const r = render(el(bout({ status: 'scheduled' })));
    await tap(letterBtn(akaSide(r.container), 'M'));
    await tap(slot(r.container, marks(r.container)[0]));
    expect(API.recordTap).not.toHaveBeenCalled();
    await act(async () => { r.rerender(el(bout({ status: 'scheduled', ipponsA: ['K'], modifiedAt: NOW + 50 }))); });
    expect(marks(r.container)).toEqual([expect.stringMatching(/^Aka slot \d: remove K$/)]);
  });

  it('sent back to the queue with a tap pending: the board takes the scheduled copy', async () => {
    const r = render(el(bout()));
    await tap(letterBtn(akaSide(r.container), 'M'));
    await act(async () => { r.rerender(el(bout({ status: 'scheduled', ipponsA: ['K'], modifiedAt: NOW + 50 }))); });
    expect(marks(r.container)).toEqual([expect.stringMatching(/^Aka slot \d: remove K$/)]);
    expect(r.container.textContent).toContain('PRE-MATCH');
  });
});
