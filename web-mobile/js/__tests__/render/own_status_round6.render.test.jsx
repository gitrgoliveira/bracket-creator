// Stage 5 review 6: a representative-bout add or remove on a RUNNING team
// match is no own status write (the match is running before and after it), so
// nothing another device does to the match while it is out says anything
// about a draft. It was begun as one: a tap echo restamping the match, or a
// finish elsewhere, read as the match leaving under the draft typed before the
// Start (which the Start carried, and which landed), and the change's refusal
// then showed "Your unsaved entries were dropped" over a match that lost
// nothing.
import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, API, NOW } from '../helpers/individual_editor.js';
import { teamBout, ipponBtn, marks as boutMarks } from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const tap = (node) => act(async () => { fireEvent.click(node); });
const flush = async () => { for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); }); };
const button = (t) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === t);
const notes = () => ['draft-dropped-notice', 'draft-kept-notice', 'start-refused-notice'].filter((id) => screen.queryByTestId(id));

function answerer() {
  const writes = [];
  const make = (opts) => {
    let resolve; let reject;
    const settled = new Promise((res, rej) => { resolve = res; reject = rej; });
    settled.catch(() => {});
    writes.push({ resolve, reject, onRefusal: opts && opts.onRefusal });
    return { applied: true, pending: true, settled };
  };
  return { make, writes };
}

const rows = (s) => [1, 2, 3].map((p) => ({
  position: p, sideA: '', sideB: '', ipponsA: p === 1 ? s.a : [], ipponsB: p === 1 ? s.b : [], hansokuA: 0, hansokuB: 0, winner: '', decision: p > 1 ? 'hikiwake' : '',
}));
const mk = (s) => teamBout({
  status: s.status, phase: 'bracket', modifiedAt: s.at, scoreVersion: s.v,
  subResults: [...rows(s), ...(s.rep ? [{ position: -1, sideA: 'Kyoto', sideB: 'Osaka', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: 'daihyosen' }] : [])],
});

describe('a representative-bout change on a running team match is no own status write', () => {
  for (const next of ['running', 'completed']) {
    it(`a draft typed before the Start raises no note when the add is refused after the match is ${next === 'running' ? 'restamped' : 'finished'} elsewhere`, async () => {
      const w = answerer();
      API.recordDaihyosen = vi.fn((_c, _m, _pw, opts) => w.make(opts));
      const onSubmit = vi.fn((patch) => (patch.status === 'running' ? Promise.resolve(w.make()) : Promise.resolve({ applied: true })));
      let server = { status: 'scheduled', a: [], b: [], at: NOW, v: 2 };
      const el = () => <ScoreEditorModal match={mk(server)} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
      const r = render(el());
      const feed = (s) => { server = { ...s, at: server.at + 10, v: server.v + 1 }; r.rerender(el()); };
      await flush();
      // A tied draft on bout 1, then Start, which lands and the feed shows.
      await tap(ipponBtn(r.container, 0, 'aka', 'M'));
      await tap(ipponBtn(r.container, 0, 'shiro', 'M'));
      await tap(button('Start match'));
      await flush();
      await act(async () => { w.writes[0].resolve({ applied: true }); await Promise.resolve(); });
      await flush();
      feed({ ...server, status: 'running', a: ['M'], b: ['M'] });
      await flush();
      expect(notes()).toEqual([]);
      const add = screen.getByTestId('scoring-modal-daihyosen-button');
      expect(add.disabled).toBe(false);
      await tap(add);
      await flush();
      const repWrite = w.writes.at(-1);
      expect(API.recordDaihyosen).toHaveBeenCalledTimes(1);
      // Another device's tap echo restamps the match, or it finishes it.
      feed({ ...server, status: next });
      await flush();
      expect(notes()).toEqual([]);
      await act(async () => {
        const e = new Error('No representative bout: the encounter is not tied.');
        if (repWrite.onRefusal) repWrite.onRefusal(e);
        repWrite.reject(e);
        await Promise.resolve();
      });
      await flush();
      expect(notes(), 'nothing was dropped and nothing the operator typed is held').toEqual([]);
      expect({ a: boutMarks(r.container, 0, 'aka'), b: boutMarks(r.container, 0, 'shiro') }).toEqual({ a: ['M'], b: ['M'] });
      cleanup();
    });
  }
});
