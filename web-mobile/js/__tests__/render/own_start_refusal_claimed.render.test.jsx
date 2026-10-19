// An editor's own Start turned down by the server is told ONCE (bc-evts stage
// 6). The editor says it beside the board (StartRefusedNotice), so it claims
// the refusal from the outbox (onRefusal returning true, `_claimRefusal`),
// which then raises no `rejected` alert with the same sentence. Before, the
// Start reached the outbox with no claim, and the operator read the sentence
// twice: in the notice and in the toast.
//
// The fake host hands the editor's opts on as every host does; a refusal is
// delivered as the outbox delivers it: the claims first, then `settled`
// rejects with the same error.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, NOW, bout,
} from '../helpers/individual_editor.js';
import { teamBout, kachinukiBout, ipponBtn } from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const el = (m, onSubmit) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const button = (t) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === t);
const refusedNote = () => screen.queryByTestId('start-refused-notice');

const BUSY = 'Shiaijo A is running Match 1 (Quarterfinals). Finish it or send it back to the queue first.';

// One write through a fake outbox door: pending at once; `refuse` claims and
// then rejects, answering whether the editor claimed it.
const outboxDoor = () => {
  let reject;
  const settled = new Promise((_res, rej) => { reject = rej; });
  settled.catch(() => {});
  const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled }));
  const refuse = async () => {
    const opts = onSubmit.mock.calls[0][1];
    const err = new Error(BUSY);
    let claimed = false;
    await act(async () => {
      claimed = !!(opts && typeof opts.onRefusal === 'function' && opts.onRefusal(err) === true);
      reject(err);
      await settled.catch(() => {});
    });
    return claimed;
  };
  return { onSubmit, refuse };
};

describe('an own Start turned down is told once, beside the board', () => {
  it('individual: the refusal is claimed and the notice says it', async () => {
    const d = outboxDoor();
    render(el(bout({ status: 'scheduled', scoreVersion: 2 }), d.onSubmit));
    await tap(button('Start match'));
    expect(await d.refuse(), 'the editor claims the refusal it says').toBe(true);
    expect(refusedNote()).not.toBeNull();
    expect(refusedNote().textContent).toContain(BUSY);
  });

  it('team: the same', async () => {
    const d = outboxDoor();
    render(el(teamBout({ status: 'scheduled', phase: 'pool' }), d.onSubmit));
    await tap(button('Start match'));
    expect(await d.refuse()).toBe(true);
    expect(refusedNote()).not.toBeNull();
    expect(refusedNote().textContent).toContain(BUSY);
  });

  it('kachinuki Record bout on a queued encounter (its start): the same', async () => {
    const d = outboxDoor();
    const r = render(el(kachinukiBout({ status: 'scheduled', phase: 'bracket', scoreVersion: 2 }), d.onSubmit));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(button('Record bout'));
    expect(d.onSubmit.mock.calls[0][0]).toMatchObject({ status: 'running', kachinukiBoutFinal: true });
    expect(await d.refuse()).toBe(true);
    expect(refusedNote()).not.toBeNull();
    expect(refusedNote().textContent).toContain(BUSY);
  });

  it('kachinuki Record bout on a running encounter is no start: the outbox tells its refusal', async () => {
    const d = outboxDoor();
    const r = render(el(kachinukiBout({ status: 'running', phase: 'bracket', modifiedAt: NOW, scoreVersion: 4 }), d.onSubmit));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(button('Record bout'));
    expect(await d.refuse(), 'nothing on the sheet says it, so it is not claimed').toBe(false);
    expect(refusedNote()).toBeNull();
  });
});
