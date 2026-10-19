// Record bout on a QUEUED kachinuki encounter (bc-evts stage 6). The sheet
// offers Record bout on a scheduled encounter as it does on a running one
// (isKachinukiBoutMode: not finished, no representative bout), and its running
// write starts the match on the server and records the bout. That start is the
// editor's OWN: it must never read as another device's start ("This match was
// started on another device", DraftKeptNotice, which also took Record bout
// off the sheet), in either order of the feed and the answer.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, NOW, kachinukiBout, boutRow, ipponBtn, rows,
} from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const el = (m, onSubmit) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const button = (t) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === t);
const kept = () => screen.queryByTestId('draft-kept-notice');

// One pending answer, settled by the test.
const recordOnce = () => {
  let settle;
  const settled = new Promise((res) => { settle = res; });
  const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled }));
  return { onSubmit, answer: (b) => act(async () => { settle(b); await settled; }) };
};

// The encounter as the server holds it once Record bout landed: bout 1 won by
// Aka's K1 and recorded, the next pairing (K1 against O2) appended.
const recordedRows = [
  boutRow(1, { sideA: 'K1', sideB: 'O1', sideAMemberId: 'k1', sideBMemberId: 'o1', ipponsA: ['M', 'M'], winner: 'K1', winnerMemberId: 'k1', recorded: true }),
  boutRow(2, { sideA: 'K1', sideB: 'O2', sideAMemberId: 'k1', sideBMemberId: 'o2' }),
];
const runningAfterRecord = () => kachinukiBout({
  status: 'running', phase: 'bracket', modifiedAt: NOW + 50, scoreVersion: 6, subResults: recordedRows,
});

async function recordFromQueue(r, s) {
  await tap(ipponBtn(r.container, 0, 'aka', 'M'));
  await tap(ipponBtn(r.container, 0, 'aka', 'M'));
  const record = button('Record bout');
  expect(record, 'Record bout is offered on the queued encounter').toBeDefined();
  expect(record.disabled).toBe(false);
  await tap(record);
  expect(s.onSubmit).toHaveBeenCalledTimes(1);
  expect(s.onSubmit.mock.calls[0][0]).toMatchObject({ status: 'running', kachinukiBoutFinal: true });
}

describe('Record bout on a queued kachinuki encounter is the editor\'s own start', () => {
  it('the feed shows the start before the answer: no "another device" note, and Record bout stays', async () => {
    const s = recordOnce();
    const r = render(el(kachinukiBout({ status: 'scheduled', phase: 'bracket', scoreVersion: 2 }), s.onSubmit));
    await recordFromQueue(r, s);
    r.rerender(el(runningAfterRecord(), s.onSubmit));
    expect(kept(), 'its own start: no "started on another device" note').toBeNull();
    await s.answer({ applied: true, subResults: recordedRows, modifiedAt: NOW + 50 });
    expect(kept()).toBeNull();
    expect(button('Record bout'), 'the sheet stays in bout mode with Record bout').toBeDefined();
    expect(rows(r.container).length).toBe(2);
  });

  it('the answer comes before the feed: the same', async () => {
    const s = recordOnce();
    const r = render(el(kachinukiBout({ status: 'scheduled', phase: 'bracket', scoreVersion: 2 }), s.onSubmit));
    await recordFromQueue(r, s);
    await s.answer({ applied: true, subResults: recordedRows, modifiedAt: NOW + 50 });
    r.rerender(el(runningAfterRecord(), s.onSubmit));
    expect(kept(), 'its own start: no "started on another device" note').toBeNull();
    expect(button('Record bout')).toBeDefined();
    expect(rows(r.container).length).toBe(2);
  });
});
