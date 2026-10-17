// bc-evts stage 3: Finish on a running match is built from the board, this
// device's pending taps included, and handed to the score door at once (the
// outbox adds the basis naming those taps as the finish's own). The editor
// carries on without waiting for the network: the door answers pending.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide, akaSide } from '../helpers/individual_editor.js';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const finishBtn = (r) => [...r.container.querySelectorAll('button')].find((b) => /^(Finish|Tap again to finish)$/.test(b.textContent.trim()));

describe('Finish carries the pending taps', () => {
  it('the finishing patch holds the board with every pending tap, sent at once', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true, pending: true });
    const r = render(<ScoreEditorModal match={bout({ ipponsA: ['K'], markIds: { a: ['s.1'], b: [] } })} onClose={vi.fn()} onSubmit={onSubmit} password="pw" />);
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'D')); });
    expect(outbox.taps('c1', 'm1'), 'precondition: both taps still pending').toHaveLength(2);

    await act(async () => { fireEvent.click(finishBtn(r)); });
    await act(async () => { fireEvent.click(finishBtn(r)); });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    expect(patch).toMatchObject({ status: 'completed', ipponsA: ['K'], ipponsB: ['M', 'D'], winner: { id: 'p2' } });
    expect(patch.changed).toEqual(expect.arrayContaining(['points', 'result']));
    // The door answered pending: the editor does not sit in "Saving…".
    expect(r.container.textContent).not.toContain('Saving…');
  });

  // bc-evts stage 3 review 11, m1: the finish says which server copy its board
  // showed, the score-events answer's when the board took it, so its basis
  // never claims a copy the screen did not show.
  it('the finish names the server copy its board showed', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true, pending: true });
    const r = render(<ScoreEditorModal match={bout()} onClose={vi.fn()} onSubmit={onSubmit} password="pw" />);
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => {
      outbox.ack('c1', 'm1', bout({ ipponsB: ['M'], markIds: { a: [], b: [tap.id] }, scoreVersion: 4, modifiedAt: tap.stamp }), {});
    });
    await act(async () => { fireEvent.keyDown(window, { key: 'Enter' }); });
    expect(onSubmit.mock.calls[0][0].seenScoreVersion).toBe(4);
  });

  it('a board that did not take the answer\'s copy names the host\'s', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true, pending: true });
    const r = render(<ScoreEditorModal match={bout()} onClose={vi.fn()} onSubmit={onSubmit} password="pw" />);
    await act(async () => { fireEvent.click(letterBtn(akaSide(r.container), 'K')); });
    await act(async () => { fireEvent.keyDown(window, { key: 'Enter' }); });
    expect(onSubmit.mock.calls[0][0].seenScoreVersion).toBe(3);
  });

  it('a tap on the other side keeps its own place in the finish', async () => {
    const onSubmit = vi.fn().mockResolvedValue({ applied: true, pending: true });
    const r = render(<ScoreEditorModal match={bout()} onClose={vi.fn()} onSubmit={onSubmit} password="pw" />);
    await act(async () => { fireEvent.click(letterBtn(akaSide(r.container), 'K')); });
    await act(async () => { fireEvent.keyDown(window, { key: 'Enter' }); });
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ status: 'completed', ipponsA: ['K'], ipponsB: [], winner: { id: 'p1' } });
  });
});
