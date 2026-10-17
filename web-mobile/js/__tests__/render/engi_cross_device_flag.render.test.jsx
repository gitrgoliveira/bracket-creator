// bc-evts stage 4 (plan F3, engi cross-device flag loss): two devices score
// one running engi match and both flags land. Device A taps Shiro +1; device
// B's Aka +1 reaches the server first. Under the old snapshot autosave A's
// whole-count write carried Aka 0 and could wipe B's flag, and the echo of
// one count could undo the other; a flag tap names only its own change. A
// fake server running the JS replay port (score_events.jsx) folds both.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, engiBout, counts, btn } from '../helpers/engi_editor.js';
import { serverFold } from '../helpers/fake_outbox.js';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const ENGI = { engi: true };
const el = (match) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" />;

describe('Shiro +1 on device A and Aka +1 on device B, inside one echo, both land', () => {
  it('A\'s board shows B\'s flag under its own pending one, and the server ends 1-1', async () => {
    let server = engiBout();
    let r;
    await act(async () => { r = render(el(server)); });

    // Device A: Shiro +1, still in A's outbox.
    await act(async () => { fireEvent.click(btn(r.container, 'engi-shiro-inc')); });
    expect(counts(r.container)).toEqual([1, 0]);

    // Device B's Aka +1 reaches the server first; its push reaches A.
    server = serverFold(server, [{ kind: 'flag.add', pos: 0, side: 'a', id: 'devB.1', session: 'devB', seq: 1, stamp: server.modifiedAt + 1 }], ENGI).match;
    await act(async () => { r.rerender(el(server)); });
    expect(counts(r.container), 'B\'s flag, with A\'s pending flag on top').toEqual([1, 1]);

    // A's batch reaches the server, which folds it onto the copy holding B's.
    const folded = serverFold(server, outbox.taps('c1', 'm1'), ENGI);
    expect(Object.values(folded.outcomes)).toEqual(['applied']);
    server = folded.match;
    expect([server.flagsB, server.flagsA]).toEqual([1, 1]);
    await act(async () => { outbox.ack('c1', 'm1', server, {}); });
    expect(counts(r.container), 'after the answer: each flag once').toEqual([1, 1]);
    await act(async () => { r.rerender(el(server)); });
    expect(counts(r.container), 'after the push: each flag once').toEqual([1, 1]);
  });

  it('a removal on A and an add on B converge in either arrival order', async () => {
    // Both devices start from 2-1 (Shiro 2, Aka 1). A takes a Shiro flag off,
    // B adds an Aka flag. Whatever reaches the server first, it ends 1-2.
    for (const aFirst of [true, false]) {
      outbox = resetOutbox();
      let server = engiBout({ flagsB: 2, flagsA: 1 });
      let r;
      await act(async () => { r = render(el(server)); });
      await act(async () => { fireEvent.click(btn(r.container, 'engi-shiro-dec')); });
      const bTap = { kind: 'flag.add', pos: 0, side: 'a', id: 'devB.1', session: 'devB', seq: 1, stamp: server.modifiedAt + 1 };
      const aTaps = outbox.taps('c1', 'm1');
      const order = aFirst ? [...aTaps, bTap] : [bTap, ...aTaps];
      server = serverFold(server, order, ENGI).match;
      await act(async () => { outbox.ack('c1', 'm1', server, {}); });
      await act(async () => { r.rerender(el(server)); });
      expect(counts(r.container), `A first: ${aFirst}`).toEqual([1, 2]);
      r.unmount();
    }
  });
});
