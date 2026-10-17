// The permutation gate of a score editor's own status writes and its kept
// draft (stage 5 review 4: the area rebuilt as ONE owner, own_status.jsx,
// after three rounds of patches each opened new edges). Every combination of
//   - the editor's own status writes: none, a Start, a reopen (Clear
//     withdrawal and reopen) or a requeue-and-reopen (the court-busy remedy),
//     one, or two in a row;
//   - each one's answer: landed, refused, held (applied:false) or discarded,
//     arriving before or after the feed moves;
//   - the feed's moves: another device starts or reopens the match, restamps
//     it, finishes it or sends it back, or nothing;
//   - a draft typed before, or none;
// is driven through the REAL individual and team editors, and after every step
// these hold:
//   I1 the board never shows a draft as the server's scoreline (a running or
//      finished match shows the server's marks unless the kept-draft note is
//      up);
//   I2 no point is ever sent twice by the editor's own Start plus a Save;
//   I3 a typed draft is never lost without a note (it is on the board, or the
//      dropped or kept note says so, or the editor's own write that carried
//      it is out or landed);
//   I4 Start is offered exactly when the server's copy is scheduled and no own
//      Start is current;
//   I5 no note claims "another device" for the editor's own write, and the
//      Start refusal note stands only over a scheduled match.
// Each family collects the combinations that break an invariant and asserts
// there are none.
import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, bout, letterBtn, shiroSide, NOW,
} from '../helpers/individual_editor.js';
import { teamBout, ipponBtn, marks as boutMarks } from '../helpers/team_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());

const OUTCOMES = ['landed', 'refused', 'held', 'discarded'];
const SUP = { applied: false, reason: 'superseded', heldGroups: ['result'] };

const tap = (node) => act(async () => { fireEvent.click(node); });
const flush = async () => { for (let i = 0; i < 4; i++) await act(async () => { await Promise.resolve(); }); };
const button = (t) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === t);
const kept = () => !!screen.queryByTestId('draft-kept-notice');
const dropped = () => !!screen.queryByTestId('draft-dropped-notice');
const refusedNote = () => !!screen.queryByTestId('start-refused-notice');

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

// The scoring letters a board shows per side: {a: [...], b: [...]}.
function shownIndividual(container) {
  const out = { a: [], b: [] };
  for (const s of container.querySelectorAll('[aria-label*=" slot "]')) {
    const m = /^(Shiro|Aka) slot \d+: remove (.+)$/.exec(s.getAttribute('aria-label') || '');
    if (m && /^[MKDTH]$/.test(m[2])) out[m[1] === 'Aka' ? 'a' : 'b'].push(m[2]);
  }
  out.a.sort(); out.b.sort();
  return out;
}
const letters = (arr) => (arr || []).filter((l) => /^[MKDTH]$/.test(l)).slice().sort();
const sameMarks = (x, y) => JSON.stringify(x) === JSON.stringify(y);

// One family: an editor kind with its fixtures and gestures.
const INDIVIDUAL = {
  name: 'individual',
  // The draft: Shiro M (side b).
  typeDraft: async (c) => { await tap(letterBtn(shiroSide(c), 'M')); },
  shown: (c) => shownIndividual(c),
  draftMarks: (server) => ({ a: letters(server.a), b: [...letters(server.b), 'M'].sort() }),
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
const TEAM = {
  name: 'team',
  typeDraft: async (c) => { await tap(ipponBtn(c, 0, 'aka', 'M')); },
  shown: (c) => ({ a: letters(boutMarks(c, 0, 'aka')), b: letters(boutMarks(c, 0, 'shiro')) }),
  draftMarks: (server) => ({ a: [...letters(server.a), 'M'].sort(), b: letters(server.b) }),
  match: (server) => teamBout({
    status: server.status, phase: 'pool', modifiedAt: server.at, scoreVersion: server.v,
    subResults: [1, 2, 3].map((p) => ({ position: p, sideA: '', sideB: '', ipponsA: p === 1 ? server.a : [], ipponsB: p === 1 ? server.b : [], hansokuA: 0, hansokuB: 0, winner: '', decision: server.status === 'completed' && p > 1 ? 'hikiwake' : '' })),
    ...(server.status === 'completed' ? { winner: 'Osaka' } : {}),
  }),
};

// Foreign moves of the feed, from the status they apply to.
const FOREIGN = {
  start: { from: 'scheduled', apply: (s) => ({ ...s, status: 'running', a: [...s.a, 'K'] }) },
  // A reopen clears the withdrawal's maru, as the server's does.
  reopen: { from: 'completed', apply: (s) => ({ ...s, status: 'running', a: [], b: [], decided: false }) },
  restamp: { from: null, apply: (s) => ({ ...s }) },
  finish: { from: 'running', apply: (s) => ({ ...s, status: 'completed', a: [...s.a, 'K'] }) },
  sendback: { from: 'running', apply: (s) => ({ ...s, status: 'scheduled' }) },
};

// run: one combination; the list of invariants it broke.
async function run(fam, sc) {
  resetOutbox();
  const writes = ownWriteAnswerer();
  API.reopenMatch = vi.fn((_c, _m, _pw, opts) => writes.make(opts));
  API.requeueBlockerAndReopen = vi.fn((_c, _m, _bc, _bm, _pw, opts) => writes.make(opts));
  const onSubmit = vi.fn((patch) => (patch.status === 'running' ? Promise.resolve(writes.make()) : Promise.resolve({ applied: true })));
  let server = sc.start === 'scheduled'
    ? { status: 'scheduled', a: [], b: [], at: NOW, v: 2 }
    : { status: 'completed', a: ['○', '○'], b: [], at: NOW, v: 2, withdrawal: true, decided: true };
  const bad = [];
  const el = () => <ScoreEditorModal match={fam.match(server)} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
  const r = render(el());
  const feed = (next) => { server = { ...next, at: server.at + 10, v: server.v + 1 }; r.rerender(el()); };
  // Simulation state.
  let draft = false; // a typed draft that is the operator's still
  let draftBase = null; // the server's copy it was typed over
  let ownStartCurrent = false; // an own Start unanswered, or landed and not yet on the feed
  let ownWriteOut = false; // any own status write unanswered: its draft waits on the answer
  let lastRunningCause = null; // 'own' | 'foreign'
  let carried = null; // what the own Start that landed carried
  let sentByStart = 0; // M points the own Start put on the server
  const check = (step) => {
    const shown = fam.shown(r.container);
    const serverMarks = { a: letters(server.a), b: letters(server.b) };
    const k = kept();
    // A correction draft typed over a finished match is the operator's own,
    // shown while the match stays where it was typed.
    const ownCorrection = draft && draftBase && server.status === draftBase.status && sameMarks(shown, fam.draftMarks(draftBase));
    if (!k && server.status !== 'scheduled' && !ownStartCurrent && !ownCorrection && !sameMarks(shown, serverMarks)) {
      bad.push(`${step}: I1 ${server.status} board ${JSON.stringify(shown)} is not the server's ${JSON.stringify(serverMarks)}`);
    }
    if (draft && !k && !dropped() && !ownStartCurrent && !ownWriteOut && !sameMarks(shown, fam.draftMarks(draftBase))) {
      bad.push(`${step}: I3 the draft is gone with no note (board ${JSON.stringify(shown)})`);
    }
    const offered = !!button('Start match');
    const shouldOffer = server.status === 'scheduled' && !ownStartCurrent;
    if (offered !== shouldOffer) bad.push(`${step}: I4 Start ${offered ? 'offered' : 'not offered'} (server ${server.status}, own Start current ${ownStartCurrent})`);
    if (k && lastRunningCause === 'own') bad.push(`${step}: I5 the kept note claims another device for this editor's own write`);
    if (refusedNote() && server.status !== 'scheduled') bad.push(`${step}: I5 the Start refusal note stands over a ${server.status} match`);
    if (dropped()) draft = false;
  };

  // The editor's first reads (the competition's details, which settle the
  // withdrawal's Clear) answer.
  await flush();
  if (sc.draft) { await fam.typeDraft(r.container); draft = true; draftBase = { ...server }; }
  check('draft');
  for (const [i, w] of sc.writes.entries()) {
    const label = `${w.kind}${i + 1}:${w.outcome}/${w.timing}`;
    // The own write itself.
    if (w.kind === 'start') {
      if (!button('Start match')) { check(`${label} not offered`); break; }
      await tap(button('Start match'));
      ownStartCurrent = true;
      carried = onSubmit.mock.calls.at(-1)[0];
    } else {
      const reopen = screen.queryByTestId('clear-withdrawal-reopen');
      if (!reopen || reopen.disabled) { check(`${label} not offered`); break; }
      if (w.kind === 'requeue') {
        // The reopen is turned down for a busy court, which offers the remedy.
        API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(new Error('Court A already has a running match.'),
          { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'c1' }));
        await tap(reopen);
        await flush();
        const rq = screen.queryByTestId('withdrawal-reopen-requeue-button');
        if (!rq) { bad.push(`${label}: no court-busy remedy offered: ${[...document.querySelectorAll('[data-testid]')].map((n) => n.getAttribute('data-testid')).filter((t) => /reopen|withdrawal/.test(t)).join(',')} calls ${API.reopenMatch.mock.calls.length}`); break; }
        await tap(rq);
        API.reopenMatch = vi.fn((_c, _m, _pw, opts) => writes.make(opts));
      } else {
        await tap(reopen);
      }
    }
    ownWriteOut = true;
    await flush();
    check(`${label} sent`);
    const ownMove = () => {
      if (w.kind === 'start') {
        server = { ...server, status: 'running', b: [...server.b, ...letters(carried.ipponsB)], a: [...server.a, ...letters(carried.ipponsA)] };
        sentByStart += [...letters(carried.ipponsA), ...letters(carried.ipponsB)].filter((l) => l === 'M').length;
        if (draft) draft = false; // the Start carried it
      } else {
        server = { ...server, status: 'running', a: [], b: [], decided: false };
        // The operator's own reopen replaces the correction draft they held.
        draft = false;
      }
      feed(server);
      lastRunningCause = 'own';
      ownStartCurrent = false;
    };
    const answer = async () => {
      await writes.answer(w.outcome);
      ownWriteOut = false;
      if (w.kind === 'start' && w.outcome !== 'landed') ownStartCurrent = false;
      check(`${label} answered`);
    };
    const move = () => {
      if (w.outcome === 'landed') { ownMove(); check(`${label} on the feed`); return; }
      const f = FOREIGN[w.foreign];
      if (f && (f.from === null || f.from === server.status)) {
        const prev = server.status;
        feed(f.apply(server));
        if (server.status === 'running' && prev !== 'running') lastRunningCause = 'foreign';
        check(`${label} feed ${w.foreign}`);
      }
    };
    if (w.timing === 'before') { await answer(); move(); } else { move(); await answer(); }
    if (w.outcome === 'landed' && w.kind !== 'start' && draft) draft = false; // the editor's own reopen
  }
  // A last foreign move, then Save of a kept draft.
  const f = FOREIGN[sc.after];
  if (f && (f.from === null || f.from === server.status)) {
    const prev = server.status;
    feed(f.apply(server));
    if (server.status === 'running' && prev !== 'running') lastRunningCause = 'foreign';
    check(`after ${sc.after}`);
  }
  if (kept()) {
    await tap(screen.getByTestId('draft-kept-save'));
    await tap(screen.getByTestId('draft-kept-save'));
    const sent = API.recordTap.mock.calls.map((c) => c[2]).filter((e) => e.kind === 'ippon.add' && e.letter === 'M').length;
    if (sentByStart + sent > 1) bad.push(`save: I2 the draft's M was sent ${sentByStart + sent} times`);
  }
  cleanup();
  return bad;
}

function* scenarios(fam) {
  for (const draft of [false, true]) {
    // No own write: only the feed moves.
    for (const first of ['start', 'restamp', 'none']) {
      for (const after of ['finish', 'sendback', 'restamp', 'none']) {
        yield { start: 'scheduled', draft, writes: first === 'start' || first === 'restamp' ? [] : [], after: first === 'none' ? after : first, then: after };
      }
    }
    // One own Start, or two when the first does not land.
    for (const outcome of OUTCOMES) {
      for (const timing of ['before', 'after']) {
        for (const foreign of ['start', 'restamp', 'none']) {
          for (const after of ['finish', 'sendback', 'restamp', 'none']) {
            yield { start: 'scheduled', draft, writes: [{ kind: 'start', outcome, timing, foreign }], after };
            if (outcome === 'landed' || foreign === 'start') continue;
            for (const outcome2 of OUTCOMES) {
              yield { start: 'scheduled', draft, writes: [{ kind: 'start', outcome, timing, foreign }, { kind: 'start', outcome: outcome2, timing: 'after', foreign: 'start' }], after };
            }
          }
        }
      }
    }
    if (fam !== INDIVIDUAL) continue;
    // A reopen or a requeue-and-reopen of a match a withdrawal decided.
    for (const kind of ['reopen', 'requeue']) {
      for (const outcome of OUTCOMES) {
        for (const timing of ['before', 'after']) {
          for (const foreign of ['reopen', 'restamp', 'none']) {
            for (const after of ['finish', 'sendback', 'none']) {
              yield { start: 'completed', draft, writes: [{ kind, outcome, timing, foreign }], after };
            }
          }
        }
      }
    }
  }
}

const describeName = (sc) => `${sc.start}${sc.draft ? '+draft' : ''} ${sc.writes.map((w) => `${w.kind}:${w.outcome}/${w.timing}/${w.foreign}`).join(' ') || 'no own write'} then ${sc.after}`;

describe.each([[INDIVIDUAL], [TEAM]])('own status writes and the kept draft: every combination (%s)', (fam) => {
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
  }, 600_000);
});
