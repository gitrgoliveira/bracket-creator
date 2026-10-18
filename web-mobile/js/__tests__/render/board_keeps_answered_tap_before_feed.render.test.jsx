// bc-evts stage 6 (a defect the journeys found): the board is the server's
// match plus this device's taps AT ALL TIMES. A match this console has just
// started is shown running from the queue row it started (the court console's
// `started`, or the editor's own Start) until the court's feed shows the start.
// A never-scored queue row carries no score version and no stamp, so the
// answer to a tap, which carries both, was never taken over it: the tap left
// the outbox and vanished from the board until the feed caught up (100 to
// 350 ms, and for good when the device went offline first).
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide, marks, NOW } from '../helpers/individual_editor.js';
import { teamBout, boutRow, ipponBtn, marks as boutMarks } from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const el = (match, props = {}) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" {...props} />;
// The queue row of a match nobody has scored: no score log, no stamp.
const queued = { status: 'scheduled', scoreVersion: 0, modifiedAt: 0 };

describe('an answered tap stays on the board before the court\'s feed shows the start', () => {
  it('individual, started from the console\'s Up next card: the answer\'s copy shows the tap', async () => {
    const r = render(el(bout(queued), { started: true }));
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    expect(marks(r.container), 'the pending tap').toHaveLength(1);
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => {
      outbox.ack('c1', 'm1', bout({ ipponsB: ['M'], markIds: { a: [], b: [tap.id] }, scoreVersion: 2, modifiedAt: NOW + 5 }), {});
    });
    expect(marks(r.container), 'the answered tap, from the answer\'s copy').toHaveLength(1);
  });

  it('individual, the host\'s copy running with no score version or stamp: the answer\'s copy shows the tap', async () => {
    const r = render(el(bout({ scoreVersion: 0, modifiedAt: 0 })));
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => {
      outbox.ack('c1', 'm1', bout({ ipponsB: ['M'], markIds: { a: [], b: [tap.id] }, scoreVersion: 2, modifiedAt: NOW + 5 }), {});
    });
    expect(marks(r.container)).toHaveLength(1);
  });

  it('team, started from the console\'s Up next card: the answer\'s copy shows the tap on its bout', async () => {
    const r = render(el(teamBout({ ...queued, subResults: [] }), { started: true }));
    await act(async () => { fireEvent.click(ipponBtn(r.container, 1, 'aka', 'K')); });
    expect(boutMarks(r.container, 1, 'aka'), 'the pending tap').toEqual(['K']);
    const [tap] = outbox.taps('c1', 'tm1');
    await act(async () => {
      outbox.ack('c1', 'tm1', teamBout({
        subResults: [boutRow(2, { ipponsA: ['K'], winner: 'Kyoto', markIds: { a: [tap.id] } })], scoreVersion: 2, modifiedAt: NOW + 5,
      }), {});
    });
    expect(boutMarks(r.container, 1, 'aka'), 'the answered tap, from the answer\'s copy').toEqual(['K']);
  });
});
