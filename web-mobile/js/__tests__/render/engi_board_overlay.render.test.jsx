// bc-evts stage 4 (operator S1 a): a display of the court on the SAME device
// (another tab, the HDMI output) shows exactly the engi operator's board,
// pending flag taps included, as it does the individual editor's
// (tv_board_overlay.render.test.jsx). The editor publishes its board through
// the court bridge on every tap and every outcome; the display applies it as a
// match_updated patch. The counts are said even at 0, which the server leaves
// out, so a flag taken back clears on the display.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, engiBout, btn, API } from '../helpers/engi_editor.js';
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
afterEach(() => { publish.mockRestore(); delete API.hasPendingTerminalWrite; });

const patches = () => publish.mock.calls.filter((c) => c[0] === 'patch');
const lastResult = () => patches().at(-1)[3].result;
const tree = (match) => ({ competitions: [{ id: 'c1', poolMatches: [match] }] });
const shownOn = (match, payload) => applyPatchToTree(tree(match), { type: 'patch', court: 'A', compId: 'c1', payload }).competitions[0].poolMatches[0];
const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />;

describe('the engi operator\'s board on a display of the same device', () => {
  it('each flag tap and each outcome publishes the board, pending taps included', async () => {
    const r = render(el(engiBout()));
    const before = patches().length;
    await act(async () => { fireEvent.click(btn(r.container, 'engi-shiro-inc')); });
    expect(patches().length).toBe(before + 1);
    expect(patches().at(-1).slice(1, 3)).toEqual(['A', 'c1']);
    expect(lastResult()).toMatchObject({ id: 'm1', status: 'running', flagsA: 0, flagsB: 1, scoreVersion: 3, sideAId: 'p1', sideBId: 'p2' });

    // The display, holding the server's 0-0, shows the pending flag.
    expect(shownOn(engiBout(), patches().at(-1)[3]).flagsB).toBe(1);

    // The server turned the tap away: the board, and so the display, drop it.
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => { outbox.ack('c1', 'm1', null, { [tap.id]: { outcome: 'matchClosed' } }); });
    expect(lastResult()).toMatchObject({ flagsA: 0, flagsB: 0 });
    expect(shownOn(engiBout({ flagsB: 1 }), patches().at(-1)[3]).flagsB, 'a count back at 0 clears on the display').toBe(0);
  });

  it('nothing is published once a finished result of the match is queued', async () => {
    API.hasPendingTerminalWrite = vi.fn().mockReturnValue(true);
    const r = render(el(engiBout()));
    const before = patches().length;
    await act(async () => { fireEvent.click(btn(r.container, 'engi-shiro-inc')); });
    expect(patches().length).toBe(before);
  });

  it('a match that is not running publishes nothing', async () => {
    const r = render(el(engiBout({ status: 'scheduled' })));
    await act(async () => { fireEvent.click(btn(r.container, 'engi-shiro-inc')); });
    expect(patches()).toHaveLength(0);
  });
});
