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
//   - a draft typed before, or none; a kept draft saved or discarded, or
//     any draft still the operator's dropped by the host through the discard
//     the editor publishes (the court console's Send back to queue over
//     unsaved entries, stage 5 review 9: at once, no note, nothing sent);
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
// A second family (review 6, `runRunningRep`) drives the team sheet's
// representative-bout add and remove on a RUNNING match, which moves no
// status: no note at all, whatever another device does while it is out.
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
const UNSAVED_WARNING = /^Your unsaved entries will be discarded\. /;
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
  // R1: the unsaved-entries warning is answered Discard entries, unless a
  // case declines it first (declineFirst); every other confirm is agreed.
  let declining = false;
  window.confirmDialog = vi.fn(async (o) => !(declining && UNSAVED_WARNING.test((o && o.message) || '')));
  const onSubmit = vi.fn((patch) => (patch.status === 'running' ? Promise.resolve(writes.make()) : Promise.resolve({ applied: true })));
  let server;
  if (sc.start === 'scheduled') server = { status: 'scheduled', a: [], b: [], at: NOW, v: 2 };
  else if (fam === TEAM) server = { status: 'completed', a: [], b: [], at: NOW, v: 2, knockout: true };
  else if (fam === ENGI) server = { status: 'completed', a: ['F', 'F', 'F'], b: [], at: NOW, v: 2 };
  else server = { status: 'completed', a: ['○', '○'], b: [], at: NOW, v: 2, withdrawal: true, decided: true };
  const bad = [];
  API.recordDecision = vi.fn().mockResolvedValue({ applied: true, status: 'completed' });
  // The host's handle on the editor's unsaved entries (R1, stage 5 review 8).
  const hostRef = { current: null };
  const el = () => <ScoreEditorModal match={fam.match(server)} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" hostUnsavedRef={hostRef} />;
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
    // H: a host leaving the editor now asks exactly when a draft is the
    // operator's still (R1): the editor publishes its unsaved entries.
    if (!ownStartCurrent && !ownWriteOut) {
      const says = !!(hostRef.current && hostRef.current());
      if (says !== draft) bad.push(`${step}: H a host leave would ${says ? '' : 'not '}ask (draft ${draft})`);
    }
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

  // R1: an own status write over a draft asks first, and only then.
  const warned = () => window.confirmDialog.mock.calls.filter(([o]) => UNSAVED_WARNING.test((o && o.message) || '')).length;
  const warnedOnce = (label, before) => {
    const n = warned() - before;
    const want = draft && !kept() ? 1 : 0;
    if (n !== want) bad.push(`${label}: R1 the unsaved-entries warning was asked ${n} times (draft ${draft})`);
    // Discard entries: the draft goes at once (stage 5 review 8).
    if (n > 0) { draft = false; mustLand = false; }
  };
  const sentCount = () => writes.writes.length + API.reopenMatch.mock.calls.length
    + API.requeueBlockerAndReopen.mock.calls.length + API.recordDaihyosen.mock.calls.length;
  // The operator first answers Keep entries: nothing is sent and the draft
  // stays on the board (I3, checked here with no own write out).
  const declineFirst = async (w, label, node) => {
    if (!w.cancelFirst || !draft) return;
    const sent = sentCount();
    const asked = warned();
    declining = true;
    await tap(node());
    await flush();
    declining = false;
    if (warned() - asked !== 1) bad.push(`${label}: R1 Keep entries was never offered`);
    if (sentCount() !== sent) bad.push(`${label}: R1 Keep entries sent the write`);
    check(`${label} kept entries`);
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
      await declineFirst(w, label, () => screen.queryByTestId('scoring-modal-daihyosen-button'));
      const asked = warned();
      await tap(add);
      warnedOnce(label, asked);
      // The sheet shows its own change running until the answer: like an own
      // Start, Start is not offered over it.
      ownStartCurrent = true;
    } else {
      const reopen = screen.queryByTestId('clear-withdrawal-reopen');
      if (!reopen || reopen.disabled) { check(`${label} not offered`); break; }
      if (w.kind === 'requeue') {
        API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(new Error('Court A already has a running match.'),
          { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'c1' }));
        await declineFirst(w, label, () => screen.queryByTestId('clear-withdrawal-reopen'));
        const askedFirst = warned();
        await tap(reopen);
        await flush();
        // Discard entries at the reopen drops the draft at once, so the
        // remedy after its refusal has nothing left to ask about.
        warnedOnce(label, askedFirst);
        const rq = screen.queryByTestId('withdrawal-reopen-requeue-button');
        if (!rq) { bad.push(`${label}: no court-busy remedy offered`); break; }
        const asked = warned();
        await tap(rq);
        warnedOnce(label, asked);
        API.reopenMatch = vi.fn((_c, _m, _pw, opts) => writes.make(opts));
      } else {
        await declineFirst(w, label, () => screen.queryByTestId('clear-withdrawal-reopen'));
        const asked = warned();
        await tap(reopen);
        warnedOnce(label, asked);
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
  if (sc.final === 'host' && draft) {
    // The host drops the editor's entries through the discard the editor
    // publishes beside its unsaved state (the court console's Send back to
    // queue over unsaved entries, R1; stage 5 review 9, m1): they go at once,
    // with no note, nothing sent, and the board the server's.
    const h = hostRef.current;
    if (!h || typeof h.discard !== 'function') bad.push('final host: the editor publishes no discard');
    else {
      const sent = sentCount();
      const taps = API.recordTap.mock.calls.length;
      await act(async () => { h.discard(); });
      await flush();
      draft = false; mustLand = false;
      if (kept()) bad.push('final host: the kept draft stayed after the host discarded it');
      if (dropped()) bad.push('final host: a dropped note after the host discarded the draft');
      if (hostRef.current && hostRef.current()) bad.push('final host: the editor still holds unsaved entries');
      if (sentCount() !== sent || API.recordTap.mock.calls.length !== taps) bad.push('final host: the discard sent a write');
      const shown = fam.shown(r.container);
      if (!sameMarks(shown, fmarks(fam, server))) bad.push(`final host: board ${JSON.stringify(shown)} is not the server's ${JSON.stringify(fmarks(fam, server))}`);
      check('final host discard');
    }
  } else if (kept()) {
    if (sc.final === 'decide') {
      // The operator records a kiken over the kept draft and chooses Discard
      // entries (R1, stage 5 review 8): the kept draft goes at once, and the
      // own decision is never followed by a note that another device dropped it.
      const k = screen.queryByTestId('scoring-modal-kiken-voluntary-button');
      if (!k) bad.push('final decide: no decision offered over the kept draft');
      else {
        const asked = warned();
        await tap(k);
        await tap(document.querySelector('input[name="decision-side"][value="aka"]'));
        await act(async () => { fireEvent.submit(document.querySelector('form.decision-prompt')); });
        await flush();
        if (warned() - asked !== 1) bad.push('final decide: R1 was not asked over the kept draft');
        if (API.recordDecision.mock.calls.length !== 1) bad.push(`final decide: ${API.recordDecision.mock.calls.length} decisions sent`);
        if (kept()) bad.push('final decide: the kept draft stayed after Discard entries');
        draft = false; mustLand = false;
        feed({ ...server, status: 'completed', decided: true });
        if (dropped()) bad.push('final decide: a dropped note follows the own decision');
        if (kept()) bad.push('final decide: a kept note over the decided match');
      }
    } else if (sc.final === 'discard') {
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

// The running sheet's representative-bout family (stage 5 review 6): an add
// or remove on a RUNNING team match moves no status, so it is no own status
// write, and nothing another device does to the match while it is out may
// raise a note: the draft typed before the Start (when there is one) went
// with the Start, which landed. Each combination of add or remove x a restamp
// or a finish elsewhere x landed, refused, held or discarded x the answer
// before or after the feed's move, with a draft before the Start or none,
// holds after every step: no note at all, Start never offered, and the board
// shows the server's bout 1.
async function runRunningRep(sc) {
  resetOutbox();
  const writes = ownWriteAnswerer();
  API.recordDaihyosen = vi.fn((_c, _m, _pw, opts) => writes.make(opts));
  API.removeDaihyosen = vi.fn((_c, _m, _pw, _at, opts) => writes.make(opts));
  const onSubmit = vi.fn((patch) => (patch.status === 'running' ? Promise.resolve(writes.make()) : Promise.resolve({ applied: true })));
  let server = { status: sc.draft ? 'scheduled' : 'running', a: [], b: [], at: NOW, v: 2, knockout: true, rep: sc.kind === 'remove' };
  const bad = [];
  const el = () => <ScoreEditorModal match={TEAM.match(server)} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
  const r = render(el());
  const feed = (next) => { server = { ...next, at: server.at + 10, v: server.v + 1 }; r.rerender(el()); };
  const check = (step) => {
    for (const id of ['draft-dropped-notice', 'draft-kept-notice', 'start-refused-notice']) {
      if (screen.queryByTestId(id)) bad.push(`${step}: a note (${id}) over a match that lost nothing`);
    }
    if (button('Start match')) bad.push(`${step}: Start offered over a ${server.status} match`);
    const shown = TEAM.shown(r.container);
    const serverMarks = fmarks(TEAM, server);
    if (!sameMarks(shown, serverMarks)) bad.push(`${step}: board ${JSON.stringify(shown)} is not the server's ${JSON.stringify(serverMarks)}`);
  };
  await flush();
  if (sc.draft) {
    // A tied draft on bout 1, then the operator's Start, which lands.
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(ipponBtn(r.container, 0, 'shiro', 'M'));
    await tap(button('Start match'));
    const carried = onSubmit.mock.calls.at(-1)[0];
    await writes.answer('landed');
    feed(TEAM.applyStart(server, carried));
  }
  await flush();
  check('running');
  const btn = screen.queryByTestId(sc.kind === 'add' ? 'scoring-modal-daihyosen-button' : 'team-daihyosen-remove');
  if (!btn || btn.disabled) { bad.push(`${sc.kind} not offered on the running sheet`); cleanup(); return bad; }
  await tap(btn);
  await flush();
  const foreign = () => {
    feed(sc.foreign === 'finish' ? { ...server, status: 'completed' } : { ...server });
    check(`feed ${sc.foreign}`);
  };
  const answer = async () => {
    await writes.answer(sc.outcome);
    // The server's copy of a change that landed on the running match.
    if (sc.outcome === 'landed' && server.status === 'running') feed({ ...server, rep: sc.kind === 'add' });
    await flush();
    check(`${sc.kind} ${sc.outcome}`);
  };
  if (sc.timing === 'before') { await answer(); foreign(); } else { foreign(); await answer(); }
  cleanup();
  return bad;
}

// runChain (stage 5 review 7): two own status writes over ONE move to
// running, the second made before the first is answered or shown: a Start and
// then a representative-bout add (`startRep`), the queued sheet's add with the
// pre-save Start it sends first (`presave`), and a finished sheet's add and
// then remove (`addRemove`). The move is the editor's own while either write
// has landed or is still out, so in no order of the answers and the feed does
// a note claim another device, a kept draft offer to send the draft again, or
// a stale draft come back over a move that landed; while the writes are out a
// queued sheet still shows its draft.
async function runChain(sc) {
  resetOutbox();
  const writes = ownWriteAnswerer();
  API.recordDaihyosen = vi.fn((_c, _m, _pw, _at, opts) => writes.make(opts));
  API.removeDaihyosen = vi.fn((_c, _m, _pw, _at, opts) => writes.make(opts));
  const onSubmit = vi.fn((patch) => (patch.status === 'running' ? Promise.resolve(writes.make()) : Promise.resolve({ applied: true })));
  const queued = sc.family !== 'addRemove';
  let server = { status: queued ? 'scheduled' : 'completed', a: [], b: [], at: NOW, v: 2, knockout: true };
  const bad = [];
  const el = () => <ScoreEditorModal match={TEAM.match(server)} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
  const r = render(el());
  const feed = (next) => { server = { ...next, at: server.at + 10, v: server.v + 1 }; r.rerender(el()); };
  const draftMarks = { a: ['M'], b: [] };
  const check = (step) => {
    for (const id of ['draft-dropped-notice', 'draft-kept-notice']) {
      if (screen.queryByTestId(id)) bad.push(`${step}: a note (${id}) over the editor's own move`);
    }
    if (refusedNote() && server.status !== 'scheduled') bad.push(`${step}: the Start refusal note stands over a ${server.status} match`);
    if (TEAM.draftCount(server) > 1) bad.push(`${step}: the draft's point is on the server ${TEAM.draftCount(server)} times`);
  };
  await flush();
  if (sc.draft) await tap(ipponBtn(r.container, 0, 'aka', 'M'));
  const tapTestId = async (id, label) => {
    const b = screen.queryByTestId(id);
    if (!b || b.disabled) { bad.push(`${label} not offered`); return false; }
    await tap(b);
    await flush();
    return true;
  };
  if (sc.family === 'startRep') {
    if (!button('Start match')) { bad.push('Start not offered'); cleanup(); return bad; }
    await tap(button('Start match'));
    await flush();
    if (!(await tapTestId('scoring-modal-daihyosen-button', 'add'))) { cleanup(); return bad; }
  } else if (sc.family === 'presave') {
    if (!(await tapTestId('scoring-modal-daihyosen-button', 'add'))) { cleanup(); return bad; }
  } else {
    if (!(await tapTestId('scoring-modal-daihyosen-button', 'add'))) { cleanup(); return bad; }
    if (!(await tapTestId('team-daihyosen-remove', 'remove'))) { cleanup(); return bad; }
  }
  if (writes.writes.length !== 2) { bad.push(`${writes.writes.length} writes sent, not 2`); cleanup(); return bad; }
  const carried = queued ? onSubmit.mock.calls.at(-1)[0] : null;
  check('both sent');
  if (queued && sc.draft && !sameMarks(TEAM.shown(r.container), draftMarks)) {
    bad.push(`both sent: the draft is not on the board while its Start is out (${JSON.stringify(TEAM.shown(r.container))})`);
  }
  // What the server holds once the writes that landed are applied.
  let next = { ...server };
  if (queued) {
    if (sc.o1 === 'landed') next = TEAM.applyStart(next, carried);
    if (sc.o2 === 'landed') next = { ...next, status: 'running', rep: true };
  } else {
    if (sc.o1 === 'landed') next = { ...next, status: 'running', rep: true };
    if (sc.o2 === 'landed') next = { ...next, status: 'running', rep: false };
  }
  const anyLanded = sc.o1 === 'landed' || sc.o2 === 'landed';
  const answerAt = async (i, outcome) => {
    const w = writes.writes[i];
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
    check(`write ${i + 1} ${outcome}`);
  };
  // The pre-save Start is sent first, then the add: write 1 and write 2.
  const order = sc.order === 'firstFirst' ? [[0, sc.o1], [1, sc.o2]] : [[1, sc.o2], [0, sc.o1]];
  const showFeed = () => { if (anyLanded) { feed(next); check('feed'); } };
  if (sc.feed === 'before') showFeed();
  for (const [i, o] of order) await answerAt(i, o);
  if (sc.feed === 'after') showFeed();
  await flush();
  check('end');
  const shown = TEAM.shown(r.container);
  if (anyLanded && !sameMarks(shown, fmarks(TEAM, server))) {
    bad.push(`end: board ${JSON.stringify(shown)} is not the server's ${JSON.stringify(fmarks(TEAM, server))}`);
  }
  if (!anyLanded && queued && sc.draft && !sameMarks(shown, draftMarks)) {
    bad.push(`end: nothing landed and the draft is gone (board ${JSON.stringify(shown)})`);
  }
  if (queued && sc.draft && sc.o1 === 'landed' && TEAM.draftCount(server) !== 1) {
    bad.push(`end: the draft's point reached the server ${TEAM.draftCount(server)} times`);
  }
  cleanup();
  return bad;
}

describe('two own status writes over one move: every combination (team)', () => {
  it('never reads the editor\'s own move as another device\'s', async () => {
    const failures = [];
    let count = 0;
    for (const family of ['startRep', 'presave', 'addRemove']) {
      for (const draft of [false, true]) {
        for (const o1 of OUTCOMES) {
          for (const o2 of OUTCOMES) {
            // The second write lands only on what the first made: an add after
            // a Start that did not land, or a remove after an add that did
            // not, is refused by the server.
            if (o1 !== 'landed' && o2 === 'landed') continue;
            for (const feed of ['before', 'after']) {
              for (const order of ['firstFirst', 'secondFirst']) {
                const sc = { family, draft, o1, o2, feed, order };
                count += 1;
                const bad = await runChain(sc);
                if (bad.length) failures.push(`${family}${draft ? '+draft' : ''} ${o1}/${o2} feed ${feed} ${order}: ${bad[0]}`);
              }
            }
          }
        }
      }
    }
    expect({ summary: `${count} combinations, ${failures.length} failing`, first: failures.slice(0, 10) })
      .toEqual({ summary: `${count} combinations, 0 failing`, first: [] });
  }, 600_000);
});

function* scenarios(fam) {
  for (const draft of [false, true]) {
    for (const final of draft ? (fam === ENGI ? ['save', 'discard', 'host'] : ['save', 'discard', 'decide', 'host']) : ['save']) {
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
        // R1: over a draft the operator first answers Keep entries, then
        // taps again and discards them.
        if (!draft) continue;
        for (const outcome of OUTCOMES) {
          const w = { kind, outcome, timing: 'before', foreign: outcome === 'landed' ? 'shown' : 'none', skip: false, cancelFirst: true };
          for (const after of [['finish'], ['none']]) yield { start: 'completed', draft, final, writes: [w], after };
        }
      }
    }
  }
}

const describeName = (sc) => `${sc.start}${sc.draft ? `+draft(${sc.final})` : ''} ${sc.writes.map((w) => `${w.kind}:${w.outcome}/${w.timing}/${w.foreign}${w.cancelFirst ? '/keep-first' : ''}`).join(' ') || 'no own write'} then ${[].concat(sc.after).join('+')}`;

describe('the running sheet\'s representative-bout change: every combination (team)', () => {
  it('raises no note, never offers Start and shows the server\'s board', async () => {
    const failures = [];
    let count = 0;
    for (const draft of [false, true]) {
      for (const kind of ['add', 'remove']) {
        for (const foreign of ['restamp', 'finish']) {
          for (const outcome of OUTCOMES) {
            for (const timing of ['before', 'after']) {
              const sc = { draft, kind, foreign, outcome, timing };
              count += 1;
              const bad = await runRunningRep(sc);
              if (bad.length) failures.push(`${draft ? 'draft+Start' : 'running'} ${kind}:${outcome}/${timing} ${foreign}: ${bad[0]}`);
            }
          }
        }
      }
    }
    expect(failures, `${failures.length} of ${count} combinations failed:\n${failures.join('\n')}`).toEqual([]);
  }, 300_000);
});

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
