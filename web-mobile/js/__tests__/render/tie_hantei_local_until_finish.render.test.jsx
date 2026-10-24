// bc-evts stage 3 (D7): on a running match the tie (hikiwake) and the hantei
// pick are results, not taps. They stay on this editor until Finish sends
// them through the score door; the leave rule asks about them alone, while a
// running board holding only taps leaves without asking.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide, API } from '../helpers/individual_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const mount = (match, props = {}) => {
  const onClose = vi.fn();
  const onSubmit = vi.fn().mockResolvedValue({ applied: true, pending: true });
  const r = render(<ScoreEditorModal match={match} onClose={onClose} onSubmit={onSubmit} password="pw" {...props} />);
  return { r, onClose, onSubmit };
};
const byTestId = (r, id) => r.container.querySelector(`[data-testid="${id}"]`);
const finishBtn = (r) => [...r.container.querySelectorAll('button')].find((b) => /^(Finish|Tap again to finish)$/.test(b.textContent.trim()));
const finish = async (r) => {
  await act(async () => { fireEvent.click(finishBtn(r)); });
  await act(async () => { fireEvent.click(finishBtn(r)); });
};
const closeBtn = (r) => [...r.container.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Cancel');

describe('the tie and the hantei pick stay local until Finish', () => {
  it('Mark draw sends nothing; leaving asks; Finish sends the hikiwake', async () => {
    const { r, onClose, onSubmit } = mount(bout());
    await act(async () => { fireEvent.click(byTestId(r, 'scoring-modal-mark-draw')); });
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(API.recordScore).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();

    window.confirmDialog = vi.fn().mockResolvedValue(false);
    await act(async () => { fireEvent.click(closeBtn(r)); });
    expect(window.confirmDialog, 'the draw is unsaved, so leaving asks').toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();

    await finish(r);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ status: 'completed', winner: null, score: { type: 'hikiwake' } });
  });

  it('the hantei pick sends nothing until Finish, which sends the verdict', async () => {
    const { r, onSubmit } = mount(bout({ ipponsA: ['K'], ipponsB: ['M'], markIds: { a: ['x.1'], b: ['x.2'] } }));
    await act(async () => { fireEvent.click(byTestId(r, 'scoring-modal-hantei-arm')); });
    await act(async () => { fireEvent.click(byTestId(r, 'scoring-modal-hantei-shiro')); });
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
    await finish(r);
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ status: 'completed', decidedByHantei: true, winner: { id: 'p2' } });
  });

  it('a running board holding only taps leaves at once, asking nothing', async () => {
    const { r, onClose } = mount(bout());
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    await act(async () => { fireEvent.click(closeBtn(r)); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
