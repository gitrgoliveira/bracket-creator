// Stage 5 review 5: an own write the server did not land leaves no trace in
// what the editor names next, and an own reopen's token never stays out for
// good.
//
// - A retried own Start names the draft's groups again (the first Start's
//   build is forgotten when it is turned down), individual and team.
// - While an own Start is out, the board never shows its override over a feed
//   that moved since (another device started the match and sent it back): the
//   feed's copy shows, and the editor's reset reads the feed, not the override.
// - A knockout reopen that goes through the downstream confirm is settled by
//   its forced write; a reopen that landed but that the feed never showed
//   running is retired at the feed's next copy. Either way the next reopen by
//   another device is foreign, and a correction draft is kept with the note.
import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, bout, letterBtn, shiroSide, NOW,
} from '../helpers/individual_editor.js';
import { teamBout, ipponBtn } from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { resetOutbox(); });

const tap = (node) => act(async () => { fireEvent.click(node); });
const flush = async () => { for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); }); };
const button = (t) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === t);
const kept = () => !!screen.queryByTestId('draft-kept-notice');
const dropped = () => !!screen.queryByTestId('draft-dropped-notice');
const SUP = { applied: false, reason: 'superseded', heldGroups: ['result'] };
function shown(container) {
  const out = { a: [], b: [] };
  for (const s of container.querySelectorAll('[aria-label*=" slot "]')) {
    const m = /^(Shiro|Aka) slot \d+: remove (.+)$/.exec(s.getAttribute('aria-label') || '');
    if (m && /^[MKDTH]$/.test(m[2])) out[m[1] === 'Aka' ? 'a' : 'b'].push(m[2]);
  }
  return out;
}
function answerer() {
  const writes = [];
  const make = (opts) => {
    let resolve; let reject;
    const settled = new Promise((res, rej) => { resolve = res; reject = rej; });
    settled.catch(() => {});
    writes.push({ resolve, reject, onRefusal: opts && opts.onRefusal });
    return { applied: true, pending: true, settled };
  };
  return { make, writes, answer: (i, body) => act(async () => { writes[i].resolve(body); await Promise.resolve(); }) };
}

describe('a retried own Start names the draft again', () => {
  it('individual: the retry names the points', async () => {
    const w = answerer();
    const onSubmit = vi.fn(() => Promise.resolve(w.make()));
    const el = (m) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
    const r = render(el(bout({ status: 'scheduled', scoreVersion: 2 })));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    await tap(button('Start match'));
    await w.answer(0, SUP);
    await flush();
    await tap(button('Start match'));
    const retry = onSubmit.mock.calls[1][0];
    expect(retry.ipponsB).toEqual(['M']);
    expect(retry.changed, 'the retry names the points the first Start carried').toEqual(expect.arrayContaining(['points', 'result']));
  });

  it('team: the retry names the bout', async () => {
    const w = answerer();
    const onSubmit = vi.fn(() => Promise.resolve(w.make()));
    const el = (m) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
    const r = render(el(teamBout({ status: 'scheduled', phase: 'pool' })));
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(button('Start match'));
    await w.answer(0, SUP);
    await flush();
    await tap(button('Start match'));
    expect(onSubmit.mock.calls[1][0].changed).toEqual(expect.arrayContaining(['bout:1', 'result']));
  });

  it('individual: started and sent back elsewhere while the Start was out, the board shows the feed, and the retry names a change', async () => {
    const w = answerer();
    const onSubmit = vi.fn(() => Promise.resolve(w.make()));
    const el = (m) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
    const r = render(el(bout({ status: 'scheduled', scoreVersion: 2 })));
    await tap(letterBtn(shiroSide(r.container), 'M'));
    await tap(button('Start match'));
    r.rerender(el(bout({ status: 'running', ipponsA: ['K'], modifiedAt: NOW + 10, scoreVersion: 3 })));
    r.rerender(el(bout({ status: 'scheduled', ipponsA: ['K'], modifiedAt: NOW + 20, scoreVersion: 4 })));
    await flush();
    expect(shown(r.container), 'the feed\'s K shows, not the Start\'s draft').toEqual({ a: ['K'], b: [] });
    await w.answer(0, SUP);
    await flush();
    expect(dropped(), 'the draft the feed moved past is said').toBe(true);
    await tap(button('Start match'));
    expect(onSubmit.mock.calls[1][0].changed).toContain('result');
  });
});

describe('an own reopen is settled however it lands', () => {
  const match = (s) => ({
    id: 'm-r1-0', compId: 'c1', phase: 'knockout', round: 'Round 1', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
    status: s.status, ipponsA: s.a, ipponsB: s.b, hansokuA: 0, hansokuB: 0,
    ...(s.status === 'completed' && s.decided ? { decision: 'fusenpai', decisionBy: 'shiro', winner: { id: 'p1', name: 'Yamada' } } : {}),
    ...(s.status === 'completed' && !s.decided ? { winner: { id: 'p1', name: 'Yamada' } } : {}),
    modifiedAt: s.at, scoreVersion: s.v,
  });

  async function scenario({ downstream, skipRunning }) {
    const w = answerer();
    API.reopenMatch = vi.fn((_c, _m, _pw, opts) => w.make(opts));
    let server = { status: 'completed', a: ['○', '○'], b: [], at: NOW, v: 2, decided: true };
    const el = () => <ScoreEditorModal match={match(server)} onClose={vi.fn()} onSubmit={vi.fn(() => Promise.resolve({ applied: true }))} password="pw" variant="inline" />;
    const r = render(el());
    const feed = (next) => { server = { ...next, at: server.at + 10, v: server.v + 1 }; r.rerender(el()); };
    await flush();
    await tap(screen.getByTestId('clear-withdrawal-reopen'));
    await flush();
    if (downstream) {
      const e = Object.assign(new Error('downstream played'), { downstreamKnockoutPlayed: { blockingMatchId: 'm9', blockingMatches: [], displaced: [] } });
      await act(async () => { w.writes[0].onRefusal(e); w.writes[0].reject(e); await Promise.resolve(); });
      await flush();
      expect(API.reopenMatch, 'the confirmed reopen is sent again').toHaveBeenCalledTimes(2);
      await w.answer(1, { applied: true });
    } else {
      await w.answer(0, { applied: true });
    }
    await flush();
    if (skipRunning) {
      // Another device finished the reopened match before this feed caught up.
      feed({ ...server, status: 'completed', a: ['K'], b: [], decided: false });
    } else {
      feed({ ...server, status: 'running', a: [], b: [], decided: false });
      await flush();
      expect(kept()).toBe(false);
      feed({ ...server, status: 'completed', a: ['K'], b: [], decided: false });
    }
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'M'));
    feed({ ...server, status: 'running' });
    await flush();
    expect(kept(), 'another device\'s reopen keeps the correction draft').toBe(true);
    expect(shown(r.container)).toEqual({ a: ['K'], b: ['M'] });
    cleanup();
  }

  it('a reopen through the downstream confirm', async () => { await scenario({ downstream: true }); });
  it('a reopen that landed but the feed never showed running', async () => { await scenario({ skipRunning: true }); });
});
