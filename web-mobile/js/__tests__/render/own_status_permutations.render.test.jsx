// The permutation gate of a score editor's own status writes and its kept
// draft (stage 5 review 4: the area rebuilt as ONE owner, own_status.jsx;
// extended in review 5). Every combination of
//   - the editor's own status writes: none, a Start, a reopen (Clear
//     withdrawal and reopen), a reopen through the downstream confirm, a
//     requeue-and-reopen (the court-busy remedy), a representative-bout add on
//     a finished knockout encounter; one, or two in a row;
//   - each one's answer: landed, refused, held (applied:false) or discarded,
//     arriving before or after the feed moves, or after the match left
//     running again; a landed one the feed shows, or never shows running;
//   - the feed's moves: another device starts or reopens the match, restamps
//     it, finishes it or sends it back, or nothing;
//   - a draft typed before, or none; a kept draft saved or discarded;
// is driven through the REAL individual, engi and team editors against a fake
// server that applies only the groups a write NAMES (`changed`), as the real
// one does, and after every step these hold:
//   I1 the board never shows a draft as the server's scoreline;
//   I2 the draft's point reaches the server EXACTLY once when the draft is the
//      operator's still at the end (by the editor's own Start, by a Start sent
//      again after one turned down, or by Save), and never twice;
//   I3 a typed draft is never lost without a note;
//   I4 Start is offered exactly when the server's copy is scheduled and no own
//      Start is current;
//   I5 no note claims "another device" for the editor's own write, and the
//      Start refusal note stands only over a scheduled match.
import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, bout, letterBtn, shiroSide, NOW,
} from '../helpers/individual_editor.js';
import { teamBout, ipponBtn, marks as boutMarks } from '../helpers/team_editor.js';
import { engiBout } from '../helpers/engi_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());

const OUTCOMES = ['landed', 'refused', 'held', 'discarded'];
const SUP = { applied: false, reason: 'superseded', heldGroups: ['result'] };

const tap = (node) => act(async () => { fireEvent.click(node); });
const flush = async () => { for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); }); };
const button = (t) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === t);
const kept = () => !!screen.queryByTestId('draft-kept-notice');
const dropped = () => !!screen.queryByTestId('draft-dropped-notice');
const refusedNote = () => !!screen.queryByTestId('start-refused-notice');
const letters = (arr) => (arr || []).filter((l) => /^[MKDTH]$/.test(l)).slice().sort();
const sameMarks = (x, y) => JSON.stringify(x) === JSON.stringify(y);
const named = (patch, g) => !Array.isArray(patch.changed) || patch.changed.includes(g);

// An own write the outbox holds: its answer is the scenario's to give.
function ownWriteAnswerer() {
  const writes = [];
  const make = (opts) => {
    let resolve; let reject;
    const settled = new Promise((res, rej) => { resolve = res; reject = rej; });
    settled.catch(() => {});
    writes.push({ resolve, reject, onRefusal: opts && opts.onRefusal, answered: false });
    return { applied: true, pending: true, settled };
  };
  const answer = async (outcome) => {
    const w = writes.find((x) => !x.answered);
    if (!w) return false;
    w.answered = true;
    await act(async () => {
      if (outcome === 'landed') w.resolve({ applied: true });
      else if (outcome === 'held') w.resolve(SUP);
      else if (outcome === 'discarded') w.resolve({ discarded: true });
      else {
        const e = new Error('The match could not be changed.');
        if (w.onRefusal) w.onRefusal(e);
        w.reject(e);
      }
      await Promise.resolve();
    });
    await flush();
    return true;
  };
  return { make, answer, writes };
}

function shownIndividual(container) {
  const out = { a: [], b: [] };
  for (const s of container.querySelectorAll('[aria-label*=" slot "]')) {
    const m = /^(Shiro|Aka) slot \d+: remove (.+)$/.exec(s.getAttribute('aria-label') || '');
    if (m && /^[MKDTH]$/.test(m[2])) out[m[1] === 'Aka' ? 'a' : 'b'].push(m[2]);
  }
  out.a.sort(); out.b.sort();
  return out;
}

// The families: an editor with its fixtures, its draft gesture, how a Start
// patch lands on the fake server (only the groups it names), and how a Save's
// taps land.
const INDIVIDUAL = {
  name: 'individual',
  typeDraft: async (c) => { await tap(letterBtn(shiroSide(c), 'M')); },
  shown: (c) => shownIndividual(c),
  draftMarks: (server) => ({ a: letters(server.a), b: [...letters(server.b), 'M'].sort() }),
  draftCount: (server) => letters(server.b).filter((l) => l === 'M').length,
  applyStart: (server, patch) => ({
    ...server,
    ...(named(patch, 'points') ? { a: patch.ipponsA || [], b: patch.ipponsB || [] } : {}),
    ...(named(patch, 'result') ? { status: 'running' } : {}),
  }),
  applyTaps: (server, taps) => ({
    ...server,
    a: [...server.a, ...taps.filter((e) => e.kind === 'ippon.add' && e.side === 'a').map((e) => e.letter)],
    b: [...server.b, ...taps.filter((e) => e.kind === 'ippon.add' && e.side === 'b').map((e) => e.letter)],
  }),
  match: (server) => (server.withdrawal
    ? {
      id: 'm-r1-0', compId: 'c1', phase: 'knockout', round: 'Round 1', court: 'A',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
      status: server.status, ipponsA: server.a, ipponsB: server.b, hansokuA: 0, hansokuB: 0,
      ...(server.status === 'completed' && server.decided ? { decision: 'fusenpai', decisionBy: 'shiro', winner: { id: 'p1', name: 'Yamada' } } : {}),
      ...(server.status === 'completed' && !server.decided ? { winner: { id: 'p1', name: 'Yamada' } } : {}),
      modifiedAt: server.at, scoreVersion: server.v,
    }
    : bout({ status: server.status, ipponsA: server.a, ipponsB: server.b, modifiedAt: server.at, scoreVersion: server.v,
      ...(server.status === 'completed' ? { winner: 'Yamada' } : {}) })),
};
const teamRows = (server) => [1, 2, 3].map((p) => ({
  position: p, sideA: '', sideB: '', ipponsA: p === 1 ? server.a : [], ipponsB: p === 1 ? server.b : [], hansokuA: 0, hansokuB: 0, winner: '',
  decision: (server.status === 'completed' || server.knockout) && p > 1 ? 'hikiwake' : '',
}));
const TEAM = {
  name: 'team',
  typeDraft: async (c) => { await tap(ipponBtn(c, 0, 'aka', 'M')); },
  shown: (c) => ({ a: letters(boutMarks(c, 0, 'aka')), b: letters(boutMarks(c, 0, 'shiro')) }),
  draftMarks: (server) => ({ a: [...letters(server.a), 'M'].sort(), b: letters(server.b) }),
  draftCount: (server) => letters(server.a).filter((l) => l === 'M').length,
  applyStart: (server, patch) => {
    const row = (patch.subResults || []).find((s) => s.position === 1);
    return {
      ...server,
      ...(named(patch, 'bout:1') && row ? { a: row.ipponsA || [], b: row.ipponsB || [] } : {}),
      ...(named(patch, 'result') ? { status: 'running' } : {}),
    };
  },
  applyTaps: (server, taps) => ({
    ...server,
    a: [...server.a, ...taps.filter((e) => e.kind === 'ippon.add' && e.pos === 1 && e.side === 'a').map((e) => e.letter)],
    b: [...server.b, ...taps.filter((e) => e.kind === 'ippon.add' && e.pos === 1 && e.side === 'b').map((e) => e.letter)],
  }),
  match: (server) => teamBout({
    status: server.status, phase: server.knockout ? 'bracket' : 'pool', modifiedAt: server.at, scoreVersion: server.v,
    subResults: [...teamRows(server), ...(server.rep ? [{ position: -1, sideA: 'Kyoto', sideB: 'Osaka', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: 'daihyosen' }] : [])],
    ...(server.status === 'completed' ? { winner: 'Osaka' } : {}),
  }),
};
// Engi: flags, not marks; `a` holds Aka's flags as 'F', `b` Shiro's.
const ENGI = {
  name: 'engi',
  typeDraft: async (c) => { await tap(c.querySelector('[data-testid="engi-shiro-inc"]')); },
  shown: (c) => {
    const n = (id) => Number((c.querySelector(`[data-testid="${id}"]`) || {}).textContent || 0);
    return { a: Array(n('engi-aka-count')).fill('F'), b: Array(n('engi-shiro-count')).fill('F') };
  },
  draftMarks: (server) => ({ a: [...server.a], b: [...server.b, 'F'] }),
  draftCount: (server) => server.b.length,
  applyStart: (server) => server,
  applyTaps: (server, taps) => ({
    ...server,
    a: [...server.a, ...taps.filter((e) => e.kind === 'flag.add' && e.side === 'a').map(() => 'F')],
    b: [...server.b, ...taps.filter((e) => e.kind === 'flag.add' && e.side === 'b').map(() => 'F')],
  }),
  match: (server) => engiBout({ status: server.status, flagsA: server.a.length, flagsB: server.b.length, modifiedAt: server.at, scoreVersion: server.v,
    ...(server.status === 'completed' ? { winner: 'Aka One - Aka Two' } : {}) }),
};
const fmarks = (fam, s) => (fam === ENGI ? { a: [...s.a], b: [...s.b] } : { a: letters(s.a), b: letters(s.b) });

// Foreign moves of the feed, from the status they apply to.
const FOREIGN = {
  start: { from: 'scheduled', apply: (s, fam) => ({ ...s, status: 'running', a: [...s.a, fam === ENGI ? 'F' : 'K'] }) },
  // A reopen clears a withdrawal's maru, as the server's does.
  reopen: { from: 'completed', apply: (s) => ({ ...s, status: 'running', ...(s.withdrawal ? { a: [], b: [] } : {}), decided: false }) },
  restamp: { from: null, apply: (s) => ({ ...s }) },
  finish: { from: 'running', apply: (s, fam) => ({ ...s, status: 'completed', a: [...s.a, fam === ENGI ? 'F' : 'K'] }) },
  sendback: { from: 'running', apply: (s) => ({ ...s, status: 'scheduled' }) },
};

// run: one combination; the list of invariants it broke.
async function run(fam, sc) {
  resetOutbox();
  const writes = ownWriteAnswerer();
  API.reopenMatch = vi.fn((_c, _m, _pw, opts) => writes.make(opts));
  API.requeueBlockerAndReopen = vi.fn((_c, _m, _bc, _bm, _pw, opts) => writes.make(opts));
  API.recordDaihyosen = vi.fn((_c, _m, _pw, opts) => writes.make(opts));
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  const onSubmit = vi.fn((patch) => (patch.status === 'running' ? Promise.resolve(writes.make()) : Promise.resolve({ applied: true })));
  let server;
  if (sc.start === 'scheduled') server = { status: 'scheduled', a: [], b: [], at: NOW, v: 2 };
  else if (fam === TEAM) server = { status: 'completed', a: [], b: [], at: NOW, v: 2, knockout: true };
  else if (fam === ENGI) server = { status: 'completed', a: ['F', 'F', 'F'], b: [], at: NOW, v: 2 };
  else server = { status: 'completed', a: ['○', '○'], b: [], at: NOW, v: 2, withdrawal: true, decided: true };
  const bad = [];
  const el = () => <ScoreEditorModal match={fam.match(server)} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
  const r = render(el());
  const feed = (next) => { server = { ...next, at: server.at + 10, v: server.v + 1 }; r.rerender(el()); };
  // Simulation state.
  let draft = false; // a typed draft that is the operator's still
  let draftBase = null; // the server's copy it was typed over
  let mustLand = false; // a scheduled draft whose point must reach the server once
  let ownStartCurrent = false;
  let ownWriteOut = false;
  let lastRunningCause = null; // 'own' | 'foreign'
  const check = (step) => {
    const shown = fam.shown(r.container);
    const serverMarks = fmarks(fam, server);
    const k = kept();
    const ownCorrection = draft && draftBase && server.status === draftBase.status && sameMarks(shown, fam.draftMarks(fmarks(fam, draftBase)));
    if (!k && server.status !== 'scheduled' && !ownStartCurrent && !ownCorrection && !sameMarks(shown, serverMarks)) {
      bad.push(`${step}: I1 ${server.status} board ${JSON.stringify(shown)} is not the server's ${JSON.stringify(serverMarks)}`);
    }
    if (draft && !k && !dropped() && !ownStartCurrent && !ownWriteOut && !sameMarks(shown, fam.draftMarks(fmarks(fam, draftBase)))) {
      bad.push(`${step}: I3 the draft is gone with no note (board ${JSON.stringify(shown)})`);
    }
    if (fam !== ENGI) {
      const offered = !!button('Start match');
      const shouldOffer = server.status === 'scheduled' && !ownStartCurrent;
      if (offered !== shouldOffer) bad.push(`${step}: I4 Start ${offered ? 'offered' : 'not offered'} (server ${server.status}, own Start current ${ownStartCurrent})`);
    }
    if (k && lastRunningCause === 'own') bad.push(`${step}: I5 the kept note claims another device for this editor's own write`);
    if (refusedNote() && server.status !== 'scheduled') bad.push(`${step}: I5 the Start refusal note stands over a ${server.status} match`);
    if (fam.draftCount(server) > 1 && fam !== ENGI) bad.push(`${step}: I2 the draft's point is on the server ${fam.draftCount(server)} times`);
    if (dropped()) { draft = false; mustLand = false; }
  };
  const foreign = (name) => {
    const f = FOREIGN[name];
    if (!f || (f.from !== null && f.from !== server.status)) return false;
    const prev = server.status;
    feed(f.apply(server, fam));
    if (server.status === 'running' && prev !== 'running') lastRunningCause = 'foreign';
    check(`feed ${name}`);
    return true;
  };

  await flush();
  if (sc.draft) {
    await fam.typeDraft(r.container);
    draft = true; draftBase = { ...server }; mustLand = sc.start === 'scheduled' && fam !== ENGI;
  }
  check('draft');
  let carried = null;
  for (const [i, w] of sc.writes.entries()) {
    const label = `${w.kind}${i + 1}:${w.outcome}/${w.timing}${w.skip ? '/skipped' : ''}`;
    if (w.kind === 'start') {
      if (!button('Start match')) { check(`${label} not offered`); break; }
      await tap(button('Start match'));
      ownStartCurrent = true;
      carried = onSubmit.mock.calls.at(-1)[0];
    } else if (w.kind === 'rep') {
      const add = screen.queryByTestId('scoring-modal-daihyosen-button');
      if (!add || add.disabled) { check(`${label} not offered`); break; }
      await tap(add);
      // The sheet shows its own change running until the answer: like an own
      // Start, Start is not offered over it.
      ownStartCurrent = true;
    } else {
      const reopen = screen.queryByTestId('clear-withdrawal-reopen');
      if (!reopen || reopen.disabled) { check(`${label} not offered`); break; }
      if (w.kind === 'requeue') {
        API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(new Error('Court A already has a running match.'),
          { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'c1' }));
        await tap(reopen);
        await flush();
        const rq = screen.queryByTestId('withdrawal-reopen-requeue-button');
        if (!rq) { bad.push(`${label}: no court-busy remedy offered`); break; }
        await tap(rq);
        API.reopenMatch = vi.fn((_c, _m, _pw, opts) => writes.make(opts));
      } else {
        await tap(reopen);
        if (w.kind === 'confirm') {
          // The knockout's next match was played: the reopen is refused, the
          // operator confirms, and the forced reopen is the write answered.
          await flush();
          const first = writes.writes.at(-1);
          first.answered = true;
          const e = Object.assign(new Error('downstream played'), { downstreamKnockoutPlayed: { blockingMatchId: 'm9', blockingMatches: [], displaced: [] } });
          await act(async () => { first.onRefusal(e); first.reject(e); await Promise.resolve(); });
          await flush();
        }
      }
    }
    ownWriteOut = true;
    await flush();
    check(`${label} sent`);
    // The write landed, as the feed shows it.
    const ownMove = () => {
      if (w.kind === 'start') {
        server = fam.applyStart(server, carried);
        if (server.status === 'running') draft = false;
      } else if (w.kind === 'rep') {
        server = { ...server, status: 'running', rep: true };
        // The operator's own change replaces the correction draft they held.
        draft = false;
      } else {
        server = { ...server, status: 'running', a: [], b: [], decided: false };
        draft = false; // the operator's own reopen replaces the correction draft
      }
      feed(server);
      if (server.status === 'running') lastRunningCause = 'own';
      ownStartCurrent = false;
      check(`${label} on the feed`);
    };
    // The write landed, but another device finished the match before the feed
    // ever showed it running.
    const skippedMove = () => {
      if (w.kind === 'start') server = fam.applyStart(server, carried);
      else { draft = false; server = { ...server, a: [], b: [], decided: false }; }
      if (fam.draftCount(server) > 0) { draft = false; }
      feed({ ...server, status: 'completed', a: [...server.a, fam === ENGI ? 'F' : 'K'] });
      ownStartCurrent = false;
      check(`${label} finished elsewhere before the feed showed it`);
    };
    const answer = async () => {
      await writes.answer(w.outcome);
      ownWriteOut = false;
      if ((w.kind === 'start' || w.kind === 'rep') && w.outcome !== 'landed') ownStartCurrent = false;
      check(`${label} answered`);
    };
    if (w.outcome === 'landed') {
      if (w.timing === 'before') { await answer(); if (w.skip) skippedMove(); else ownMove(); } else { if (w.skip) skippedMove(); else ownMove(); await answer(); }
    } else if (w.timing === 'afterLeft') {
      // Another device moves it to running and out again before the answer.
      foreign(server.status === 'scheduled' ? 'start' : 'reopen');
      foreign(w.foreign === 'finish' ? 'finish' : 'sendback');
      await answer();
    } else if (w.timing === 'before') {
      await answer();
      foreign(w.foreign);
    } else {
      foreign(w.foreign);
      await answer();
    }
  }
  for (const name of [].concat(sc.after)) foreign(name);
  // The operator settles what is left: a kept draft saved or discarded, or a
  // scheduled draft started again.
  if (kept()) {
    if (sc.final === 'discard') {
      await tap(screen.getByTestId('draft-kept-discard'));
      draft = false; mustLand = false;
    } else {
      const before = API.recordTap.mock.calls.length;
      await tap(screen.getByTestId('draft-kept-save'));
      await tap(screen.getByTestId('draft-kept-save'));
      server = fam.applyTaps(server, API.recordTap.mock.calls.slice(before).map((c) => c[2]));
      draft = false;
    }
    check('final save or discard');
  } else if (mustLand && draft && server.status === 'scheduled' && button('Start match')) {
    await tap(button('Start match'));
    ownStartCurrent = true;
    carried = onSubmit.mock.calls.at(-1)[0];
    await writes.answer('landed');
    server = fam.applyStart(server, carried);
    feed(server);
    ownStartCurrent = false;
    if (server.status === 'running') draft = false;
    check('final Start');
  }
  if (mustLand && fam.draftCount(server) !== 1 && !dropped()) {
    bad.push(`end: I2 the draft's point reached the server ${fam.draftCount(server)} times (server ${JSON.stringify(fmarks(fam, server))})`);
  }
  cleanup();
  return bad;
}

function* scenarios(fam) {
  for (const draft of [false, true]) {
    for (const final of draft ? ['save', 'discard'] : ['save']) {
      // No own write: only the feed moves.
      for (const after of [['start'], ['start', 'finish'], ['start', 'sendback'], ['restamp'], ['none']]) {
        yield { start: 'scheduled', draft, final, writes: [], after };
      }
      if (fam === ENGI) {
        for (const after of [['reopen'], ['reopen', 'finish'], ['reopen', 'sendback'], ['restamp']]) yield { start: 'completed', draft, final, writes: [], after };
        continue;
      }
      // One own Start, or two when the first does not land.
      for (const outcome of OUTCOMES) {
        const timings = outcome === 'landed' ? ['before', 'after'] : ['before', 'after', 'afterLeft'];
        for (const timing of timings) {
          const foreigns = outcome === 'landed' ? ['shown', 'skipped'] : (timing === 'afterLeft' ? ['none', 'finish'] : ['start', 'restamp', 'none']);
          for (const foreignName of foreigns) {
            for (const after of [['finish'], ['sendback'], ['none']]) {
              const w = { kind: 'start', outcome, timing, foreign: foreignName, skip: foreignName === 'skipped' };
              yield { start: 'scheduled', draft, final, writes: [w], after };
              if (outcome === 'landed' || timing === 'afterLeft' || foreignName !== 'restamp') continue;
              for (const outcome2 of OUTCOMES) {
                yield { start: 'scheduled', draft, final, writes: [w, { kind: 'start', outcome: outcome2, timing: 'after', foreign: 'start' }], after };
              }
            }
          }
        }
      }
      // A status write over a finished match.
      const kinds = fam === INDIVIDUAL ? ['reopen', 'requeue', 'confirm'] : ['rep'];
      for (const kind of kinds) {
        for (const outcome of OUTCOMES) {
          for (const timing of outcome === 'landed' ? ['before', 'after'] : ['before', 'after', 'afterLeft']) {
            const foreigns = outcome === 'landed' ? ['shown', 'skipped'] : (timing === 'afterLeft' ? ['none', 'finish'] : ['reopen', 'restamp', 'none']);
            for (const foreignName of foreigns) {
              for (const after of [['finish'], ['sendback'], ['none']]) {
                yield { start: 'completed', draft, final, writes: [{ kind, outcome, timing, foreign: foreignName, skip: foreignName === 'skipped' }], after };
              }
            }
          }
        }
      }
    }
  }
}

const describeName = (sc) => `${sc.start}${sc.draft ? `+draft(${sc.final})` : ''} ${sc.writes.map((w) => `${w.kind}:${w.outcome}/${w.timing}/${w.foreign}`).join(' ') || 'no own write'} then ${[].concat(sc.after).join('+')}`;

describe.each([[INDIVIDUAL], [TEAM], [ENGI]])('own status writes and the kept draft: every combination (%s)', (fam) => {
  it(`holds every invariant (${fam.name})`, async () => {
    const failures = [];
    let n = 0;
    for (const sc of scenarios(fam)) {
      n += 1;
      const bad = await run(fam, sc);
      if (bad.length) failures.push(`${describeName(sc)}: ${bad[0]}`);
    }
    expect({ summary: `${n} combinations, ${failures.length} failing`, first: failures.slice(0, 10) })
      .toEqual({ summary: `${n} combinations, 0 failing`, first: [] });
  }, 900_000);
});
