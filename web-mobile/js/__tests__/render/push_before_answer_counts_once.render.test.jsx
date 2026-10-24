// bc-evts stage 3, G1: a push that carries this device's taps can arrive
// before the taps' own answer. The board folds the pending taps on the
// server's copy, so a tap the copy already holds must not be folded again: a
// foul that lands no H and an overtime period leave no mark id, so the copy
// names per event session the highest seq its log holds (eventSeqs).
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout } from '../helpers/individual_editor.js';
import { serverFold } from '../helpers/fake_outbox.js';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />;
const fouls = (c) => c.querySelector('[data-testid="scoring-modal-hansoku-aka"] .foul-counter__num').textContent.trim();
const overtime = (c) => c.querySelector('.encho-row__count')?.textContent.trim() || '';

describe('a push before the answer counts each tap once', () => {
  it('a foul and an overtime period the push already holds show once, before and after the answer', async () => {
    const start = bout();
    const r = render(el(start));
    await act(async () => { fireEvent.click(r.container.querySelector('[data-testid="scoring-modal-hansoku-aka"] .foul-counter__btn--inc')); });
    await act(async () => { fireEvent.click(r.container.querySelector('[data-testid="scoring-modal-encho-pill"]')); });
    await act(async () => { fireEvent.click(r.container.querySelector('[data-testid="scoring-modal-encho-checkbox"]')); });
    const taps = outbox.taps('c1', 'm1');
    expect(taps.map((t) => t.kind)).toEqual(['foul.add', 'encho.add']);
    expect(fouls(r.container), 'the pending foul').toBe('1');
    expect(overtime(r.container), 'the pending period').toBe('×1');

    // The server logged both; its push lands before their answer.
    const pushed = serverFold(start, taps).match;
    expect(pushed.eventSeqs).toEqual({ dev1: 2 });
    await act(async () => { r.rerender(el(pushed)); });
    expect(fouls(r.container), 'the foul once').toBe('1');
    expect(overtime(r.container), 'the period once').toBe('×1');

    // Then the answer: nothing moves.
    await act(async () => { outbox.ack('c1', 'm1', pushed, {}); });
    expect(fouls(r.container)).toBe('1');
    expect(overtime(r.container)).toBe('×1');
  });

  it('a pending tap the push does not hold yet still shows', async () => {
    const start = bout();
    const r = render(el(start));
    const inc = () => r.container.querySelector('[data-testid="scoring-modal-hansoku-aka"] .foul-counter__btn--inc');
    await act(async () => { fireEvent.click(inc()); });
    await act(async () => { fireEvent.click(r.container.querySelector('[data-testid="scoring-modal-encho-pill"]')); });
    await act(async () => { fireEvent.click(r.container.querySelector('[data-testid="scoring-modal-encho-checkbox"]')); });
    const [foul] = outbox.taps('c1', 'm1');
    // The push holds the foul only (seq 1); the period (seq 2) is still pending.
    const pushed = serverFold(start, [foul]).match;
    await act(async () => { r.rerender(el(pushed)); });
    expect(fouls(r.container)).toBe('1');
    expect(overtime(r.container)).toBe('×1');
  });
});
