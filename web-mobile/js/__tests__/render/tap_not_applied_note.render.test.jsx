// bc-evts stage 3: a tap the server did not apply is said quietly in the
// editor (tapNotAppliedText); one refused because the server holds the match
// as not running shows the server's own sentence next to the board
// (operator S7 a). The next tap clears both.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide } from '../helpers/individual_editor.js';
import { tapNotAppliedText } from '../../write_result.jsx';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" />;
const byTestId = (r, id) => r.container.querySelector(`[data-testid="${id}"]`);
const tapM = async (r) => { await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); }); };

describe('a tap the server did not apply', () => {
  it('is named in a quiet note, which the next tap clears', async () => {
    const r = render(el(bout()));
    await tapM(r);
    const [tap] = outbox.taps('c1', 'm1');
    const capped = { outcome: 'capped' };
    await act(async () => { outbox.ack('c1', 'm1', null, { [tap.id]: capped }); });
    expect(byTestId(r, 'tap-not-applied-note').textContent).toBe(tapNotAppliedText(capped));
    expect(byTestId(r, 'tap-refusal')).toBeNull();
    await tapM(r);
    expect(byTestId(r, 'tap-not-applied-note')).toBeNull();
  });

  // Stage 6 review 5: a tap the server applied and a finish that never saw it
  // turned away afterwards is said the same way, when the outbox tells it.
  it('a tap applied first and turned away later by a finish is said on the board', async () => {
    const r = render(el(bout()));
    await tapM(r);
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => { outbox.ack('c1', 'm1', null, {}); });
    expect(byTestId(r, 'tap-not-applied-note'), 'its answer said applied').toBeNull();
    await act(async () => { outbox.tell('c1', 'm1', { id: tap.id, outcome: 'beforeBaseline' }); });
    expect(byTestId(r, 'tap-not-applied-note').textContent).toBe(
      "Not applied: the result was recorded without this change, so it was kept in the match's history. To change the result, tap Correct.");
  });

  it('uses the server\'s message when it sends one', async () => {
    const r = render(el(bout()));
    await tapM(r);
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => {
      outbox.ack('c1', 'm1', null, { [tap.id]: { outcome: 'matchClosed', reason: 'match_closed', message: 'The match is finished.' } });
    });
    expect(byTestId(r, 'tap-not-applied-note').textContent).toBe('The match is finished.');
  });

  it('refused as not running: the server\'s sentence stands next to the board', async () => {
    const r = render(el(bout()));
    await tapM(r);
    const [tap] = outbox.taps('c1', 'm1');
    const sentence = 'This match was sent back to the queue. Start it again to score it.';
    await act(async () => {
      outbox.ack('c1', 'm1', null, { [tap.id]: { outcome: 'notStarted', reason: 'match_sent_back', message: sentence } });
    });
    const refusal = byTestId(r, 'tap-refusal');
    expect(refusal.textContent).toBe(sentence);
    expect(refusal.closest('.scoring-board'), 'next to the board').toBeTruthy();
    expect(byTestId(r, 'tap-not-applied-note')).toBeNull();
  });
});
