// bc-evts stage 3: the public self-run score sheet is the same editor with
// the EMPTY password (viewer_match.jsx renders ScoreEditorModal with
// password "" under selfReport), so a participant's tap goes to the outbox
// with "" and a running match there writes no PUT /score either.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide, akaSide, API } from '../helpers/individual_editor.js';
import { engiBout, counts, btn } from '../helpers/engi_editor.js';

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

// bc-evts stage 4: the engi editor is the same sheet's for an engi match, so a
// participant's flag is a tap with the empty password too, and the server's
// refusals (a self-run participant's limits) are said beside the board in the
// server's own sentence.
describe('a participant scores an engi match from the public page', () => {
  let outbox;
  beforeEach(() => { outbox = resetOutbox(); });
  const sheet = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="" selfReport />;

  it('every flag tap carries the empty password, and nothing else is written', async () => {
    const r = render(sheet(engiBout()));
    await act(async () => { fireEvent.click(btn(r.container, 'engi-shiro-inc')); });
    await act(async () => { fireEvent.click(btn(r.container, 'engi-aka-inc')); });
    await act(async () => { fireEvent.click(btn(r.container, 'engi-shiro-dec')); });
    expect(API.recordTap.mock.calls.map((c) => c[2].kind)).toEqual(['flag.add', 'flag.add', 'flag.remove']);
    for (const call of API.recordTap.mock.calls) expect(call[3]).toBe('');
    expect(API.recordScore).not.toHaveBeenCalled();
    expect(counts(r.container)).toEqual([0, 1]);
    // The organiser's view of the match's history is not on the public sheet.
    expect(r.container.querySelector('[data-testid="match-history"]')).toBeNull();
  });

  it('a flag the server refuses a participant is said in the server\'s sentence', async () => {
    const r = render(sheet(engiBout({ flagsB: 3 })));
    await act(async () => { fireEvent.click(btn(r.container, 'engi-aka-inc')); });
    const [tap] = outbox.taps('c1', 'm1');
    // errResultFinalized's sentence (internal/mobileapp/handlers_match.go).
    const sentence = 'This match result has already been reported. Contact the tournament organizer to correct it.';
    await act(async () => {
      outbox.ack('c1', 'm1', null, { [tap.id]: { outcome: 'refused', reason: 'result_finalized', message: sentence } });
    });
    expect(counts(r.container), 'the board is back on the server\'s count').toEqual([3, 0]);
    expect(r.container.querySelector('[data-testid="tap-not-applied-note"]').textContent).toBe(sentence);
  });
});
