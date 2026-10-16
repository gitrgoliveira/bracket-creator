// bc-evts stage 3: the public self-run score sheet is the same editor with
// the EMPTY password (viewer_match.jsx renders ScoreEditorModal with
// password "" under selfReport), so a participant's tap goes to the outbox
// with "" and a running match there writes no PUT /score either.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide, akaSide, API } from '../helpers/individual_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

describe('a participant scores from the public page', () => {
  it('every tap carries the empty password', async () => {
    const r = render(<ScoreEditorModal match={bout()} onClose={vi.fn()} onSubmit={vi.fn()} password="" selfReport />);
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    await act(async () => { fireEvent.click(letterBtn(akaSide(r.container), 'K')); });
    await act(async () => { fireEvent.click(r.container.querySelector('[data-testid="scoring-modal-hansoku-aka"] .foul-counter__btn--inc')); });
    expect(API.recordTap).toHaveBeenCalledTimes(3);
    for (const call of API.recordTap.mock.calls) expect(call[3]).toBe('');
    expect(API.recordScore).not.toHaveBeenCalled();
    // The public sheet offers no hantei and no withdrawal (bc-dhas).
    expect(r.container.querySelector('[data-testid="scoring-modal-hantei-row"]')).toBeNull();
  });
});
