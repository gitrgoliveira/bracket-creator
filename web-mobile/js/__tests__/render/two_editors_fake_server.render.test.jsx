// bc-evts stage 3: two devices scoring one running match both land. Device A
// taps Shiro M; device B's Aka K reaches the server first. A fake server
// running the JS replay port (score_events.jsx) folds both. Under the old
// snapshot autosave, A's whole-board write could carry no K and the echo of
// one board could wipe the other's point; a tap names only its own change.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, letterBtn, shiroSide, marks, NOW } from '../helpers/individual_editor.js';
import { serverFold } from '../helpers/fake_outbox.js';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />;

describe('Shiro M on device A and Aka K on device B, inside one echo, both land', () => {
  it('A\'s board shows B\'s K under its own pending M, and the server ends holding both', async () => {
    let server = bout({ markIds: { a: [], b: [] } });
    let r;
    await act(async () => { r = render(el(server)); });

    // Device A: Shiro M, still in A's outbox.
    await act(async () => { fireEvent.click(letterBtn(shiroSide(r.container), 'M')); });
    expect(marks(r.container)).toEqual([expect.stringMatching(/^Shiro slot \d: remove M$/)]);

    // Device B's Aka K reaches the server first; its push reaches A.
    server = serverFold(server, [{ kind: 'ippon.add', pos: 0, side: 'a', letter: 'K', id: 'devB.1', stamp: NOW + 1 }]).match;
    await act(async () => { r.rerender(el(server)); });
    expect(marks(r.container).sort(), 'B\'s K, with A\'s pending M on top').toEqual([
      expect.stringMatching(/^Aka slot \d: remove K$/),
      expect.stringMatching(/^Shiro slot \d: remove M$/),
    ]);

    // A's batch reaches the server, which folds it onto the copy holding K.
    const folded = serverFold(server, outbox.taps('c1', 'm1'));
    expect(Object.values(folded.outcomes)).toEqual(['applied']);
    server = folded.match;
    expect(server.ipponsA).toEqual(['K']);
    expect(server.ipponsB).toEqual(['M']);
    await act(async () => { outbox.ack('c1', 'm1', server, {}); });
    expect(marks(r.container), 'after the answer: both, once each').toHaveLength(2);
    await act(async () => { r.rerender(el(server)); });
    expect(marks(r.container), 'after the push: both, once each').toHaveLength(2);
  });
});
