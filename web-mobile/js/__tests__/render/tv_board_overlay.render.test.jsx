// bc-evts stage 3 (operator S1 a): a display of the court on the SAME device
// (another tab, the HDMI output) shows exactly the operator's board, pending
// taps included. The editor publishes its board through the court bridge on
// every tap and every outcome; the display applies it as a match_updated
// patch, and a newer server copy it already holds wins by scoreVersion.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide } from '../helpers/individual_editor.js';
import { bridge, applyPatchToTree } from '../../court_bridge.jsx';

let ScoreEditorModal;
let restore;
let outbox;
let publish;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => {
  outbox = resetOutbox();
  publish = vi.spyOn(bridge, 'publish').mockImplementation(() => {});
});
afterEach(() => { publish.mockRestore(); });

const patches = () => publish.mock.calls.filter((c) => c[0] === 'patch');
const lastResult = () => patches().at(-1)[3].result;
const tree = (match) => ({ competitions: [{ id: 'c1', poolMatches: [match] }] });

describe('the operator\'s board on a display of the same device', () => {
  it('each tap and each outcome publishes the board, pending taps included', async () => {
    const r = render(<ScoreEditorModal match={bout()} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />);
    const before = patches().length;
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    expect(patches().length).toBe(before + 1);
    const [court, comp] = patches().at(-1).slice(1, 3);
    expect([court, comp]).toEqual(['A', 'c1']);
    expect(lastResult()).toMatchObject({ id: 'm1', status: 'running', ipponsB: ['M'], scoreVersion: 3, sideAId: 'p1', sideBId: 'p2' });

    // The display, holding the server's 0-0, shows the pending M.
    const shown = applyPatchToTree(tree(bout()), { type: 'patch', court: 'A', compId: 'c1', payload: patches().at(-1)[3] });
    expect(shown.competitions[0].poolMatches[0].ipponsB).toEqual(['M']);

    // The server refused the tap (the bout was decided elsewhere): the board,
    // and so the display, drops it.
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => { outbox.ack('c1', 'm1', null, { [tap.id]: { outcome: 'capped' } }); });
    expect(lastResult().ipponsB).toEqual([]);
  });

  it('a display already holding a newer server copy keeps it', () => {
    const newer = bout({ ipponsA: ['K'], scoreVersion: 5 });
    const overlay = { result: { id: 'm1', status: 'running', ipponsB: ['M'], ipponsA: [], scoreVersion: 3 } };
    const shown = applyPatchToTree(tree(newer), { type: 'patch', court: 'A', compId: 'c1', payload: overlay });
    expect(shown.competitions[0].poolMatches[0].ipponsA).toEqual(['K']);
  });

  it('a match that is not running publishes nothing', async () => {
    const r = render(<ScoreEditorModal match={bout({ status: 'scheduled' })} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />);
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    expect(patches()).toEqual([]);
  });
});
