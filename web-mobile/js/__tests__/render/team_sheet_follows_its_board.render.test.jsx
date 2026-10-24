// bc-evts stage 5 review 1: the team sheet's own writes follow the copy its
// board is made from.
//
// C1: this device's own Start carries its pre-match draft (here a bout's
// Fusensho), and the sheet shows that draft until the feed holds the start; a
// tap made before the feed catches up writes no bout row over it. A kachinuki
// bout's row is written ahead of a tap only when neither the board's base (the
// newest server copy, a score-events answer included) nor this device's own
// Start holds it.
//
// J1: a representative-bout add or remove, and kachinuki Remove this bout,
// show at once even when a score-events answer newer than the host's copy is
// what the board is made from.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, teamBout, kachinukiBout, boutRow,
  rows, ipponBtn, fusenshoBtn,
} from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const pending = () => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) });
const el = (match, props = {}) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn(pending)} password="pw" variant="inline" {...props} />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const events = () => API.recordTap.mock.calls.map((c) => c[2]);
const button = (text) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === text);
const settle = async () => { for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); }); };

describe('C1: the own Start carries the draft, and a tap after it writes no row over it', () => {
  it('a pre-match Fusensho stays shown after Start, and the tap that follows is a tap alone', async () => {
    const onSubmit = vi.fn(pending);
    const r = render(el(teamBout({ status: 'scheduled', phase: 'pool', subResults: [] }), { onSubmit }));
    await tap(fusenshoBtn(r.container, 2, 'aka'));
    await tap(button('Start match'));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const start = onSubmit.mock.calls[0][0];
    expect(start.status).toBe('running');
    expect(start.subResults.find((s) => s.position === 3)).toMatchObject({ decision: 'fusensho' });
    await settle();
    expect(fusenshoBtn(r.container, 2, 'aka').textContent, 'the Start\'s fusensho is on the board').toContain('✓');
    await tap(ipponBtn(r.container, 0, 'shiro', 'M'));
    expect(events()).toEqual([{ kind: 'ippon.add', pos: 1, side: 'b', letter: 'M' }]);
    expect(onSubmit, 'no score write after the Start').toHaveBeenCalledTimes(1);
    expect(fusenshoBtn(r.container, 2, 'aka').textContent).toContain('✓');
  });

  it('a kachinuki bout the newest server copy holds is not written again ahead of a tap', async () => {
    const onSubmit = vi.fn(pending);
    const host = kachinukiBout({ subResults: [], scoreVersion: 4 });
    render(el(host, { onSubmit }));
    // A score-events answer newer than the host's copy holds bout 1 (another
    // device wrote it): the board is made from it.
    const row1 = boutRow(1, { sideA: 'K1', sideB: 'O1', sideAMemberId: 'k1', sideBMemberId: 'o1', ipponsB: ['K'], winner: 'O1', markIds: { b: ['x'] } });
    await act(async () => { outbox.ack('c1', 'km1', { ...host, subResults: [row1], scoreVersion: 6, modifiedAt: host.modifiedAt + 10 }); });
    await tap(ipponBtn(document.body, 0, 'aka', 'M'));
    expect(onSubmit, 'bout 1 is the server\'s: no row write').not.toHaveBeenCalled();
    expect(events()).toEqual([{ kind: 'ippon.add', pos: 1, side: 'a', letter: 'M' }]);
  });
});

describe('J1: a bout change shows over a newer score-events answer', () => {
  const repRow = { position: -1, sideA: 'Kyoto', sideB: 'Osaka', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: 'daihyosen' };
  const tied = [boutRow(1, { decision: 'hikiwake' }), boutRow(2, { decision: 'hikiwake' }), boutRow(3, { decision: 'hikiwake' })];

  it('Remove representative bout takes the row off at once', async () => {
    API.removeDaihyosen = vi.fn(pending);
    const host = teamBout({ subResults: [...tied, repRow] });
    render(el(host));
    await act(async () => { outbox.ack('c1', 'tm1', { ...host, scoreVersion: 6, modifiedAt: host.modifiedAt + 10 }); });
    await tap(screen.getByTestId('team-daihyosen-remove'));
    expect(API.removeDaihyosen).toHaveBeenCalledTimes(1);
    await settle();
    expect(screen.queryByTestId('team-daihyosen-remove'), 'the row and its Remove are gone').toBeNull();
    expect(rows(document.body)).toHaveLength(3);
  });

  it('Add representative bout shows the row at once', async () => {
    API.recordDaihyosen = vi.fn(pending);
    const host = teamBout({ subResults: tied });
    render(el(host));
    await act(async () => { outbox.ack('c1', 'tm1', { ...host, scoreVersion: 6, modifiedAt: host.modifiedAt + 10 }); });
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    expect(API.recordDaihyosen).toHaveBeenCalledTimes(1);
    await settle();
    expect(rows(document.body), 'the representative bout is on the sheet').toHaveLength(4);
    expect(screen.queryByTestId('team-daihyosen-remove')).not.toBeNull();
  });

  it('kachinuki Remove this bout takes the pairing off at once', async () => {
    API.removeKachinukiBout = vi.fn(pending);
    const recorded = boutRow(1, { sideA: 'K1', sideB: 'O1', sideAMemberId: 'k1', sideBMemberId: 'o1', ipponsA: ['M', 'K'], winner: 'K1', recorded: true });
    const next = boutRow(2, { sideA: 'K1', sideB: 'O2', sideAMemberId: 'k1', sideBMemberId: 'o2' });
    const host = kachinukiBout({ subResults: [recorded, next] });
    render(el(host));
    await act(async () => { outbox.ack('c1', 'km1', { ...host, scoreVersion: 6, modifiedAt: host.modifiedAt + 10 }); });
    const remove = screen.getByTestId('kachinuki-remove-bout-button');
    await tap(remove);
    expect(API.removeKachinukiBout).toHaveBeenCalledTimes(1);
    await settle();
    expect(screen.queryByTestId('kachinuki-remove-bout-button'), 'the pairing is gone').toBeNull();
  });
});
