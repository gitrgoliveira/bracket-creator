// bc-evts stage 3: once the server has a tap, the board is the server's. The
// answer's copy (API.latestDerivedMatch) stands in for a push not yet
// arrived, a tap counts once however both arrive, and a later change made
// elsewhere is followed; the newer copy wins by scoreVersion.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide, marks } from '../helpers/individual_editor.js';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />;

describe('the board follows the server once a tap is acknowledged', () => {
  it('the answer\'s copy shows the tap once, before and after the push', async () => {
    const r = render(el(bout()));
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    const [tap] = outbox.taps('c1', 'm1');
    const answered = bout({ ipponsB: ['M'], markIds: { a: [], b: [tap.id] }, scoreVersion: 4, modifiedAt: tap.stamp });

    // The answer lands before the push: the prop still says 0-0 at v3.
    await act(async () => { outbox.ack('c1', 'm1', answered, {}); });
    expect(marks(r.container), 'the answer\'s copy, the tap once').toHaveLength(1);

    // The push at the same version changes nothing.
    await act(async () => { r.rerender(el(answered)); });
    expect(marks(r.container)).toHaveLength(1);

    // A removal names the id the server gave the mark.
    await act(async () => { fireEvent.click(r.container.querySelector(`[aria-label="${marks(r.container)[0]}"]`)); });
    expect(window.API.recordTap.mock.calls[1][2]).toEqual({ kind: 'ippon.remove', pos: 0, side: 'b', target: tap.id });
  });

  // bc-evts stage 3 review 11, X4: match ids are name slugs, so a competition
  // deleted and made again (or imported) reuses them. An answer's copy of the
  // old match, other competitors on its sides, is never this match's board.
  it('an answer\'s copy whose sides are other competitors is not this board', async () => {
    outbox.ack('c1', 'm1', bout({
      sideA: { id: 'old1', name: 'Ito' }, sideB: { id: 'old2', name: 'Ishii' },
      sideAId: 'old1', sideBId: 'old2',
      ipponsA: ['M', 'K'], markIds: { a: ['x.1', 'x.2'], b: [] }, scoreVersion: 9,
    }), {});
    let r;
    await act(async () => { r = render(el(bout())); });
    expect(marks(r.container), 'the old match\'s points never show').toHaveLength(0);
  });

  it('a later change made elsewhere is followed, and an older answer never puts the board back', async () => {
    const r = render(el(bout()));
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => {
      outbox.ack('c1', 'm1', bout({ ipponsB: ['M'], markIds: { a: [], b: [tap.id] }, scoreVersion: 4, modifiedAt: tap.stamp }), {});
    });
    // Another device took the M off and struck Aka's K: v6 by push.
    const later = bout({ ipponsA: ['K'], ipponsB: [], markIds: { a: ['devB.2'], b: [] }, scoreVersion: 6, modifiedAt: tap.stamp + 50 });
    await act(async () => { r.rerender(el(later)); });
    expect(marks(r.container)).toEqual([expect.stringMatching(/^Aka slot \d: remove K$/)]);
  });
});
