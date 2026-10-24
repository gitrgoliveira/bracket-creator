// Operator decision R1 (2026-10-17): "If I don't save it, the ui should warn
// me it will be discarded." An editor's own action that does not carry the
// draft (a reopen, the court-busy Queue it and reopen, a decision, the team
// sheet's representative-bout add or remove over a finished match) asks first
// while the editor holds unsaved entries: Cancel keeps them and sends nothing,
// Discard entries sends the write exactly as before and the board shows the
// server's copy. With nothing unsaved it asks nothing.
//
// The engi editor offers none of these actions (no reopen, no decision, no
// representative bout), so it has nothing to warn on; the last case pins that.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import {
  installIndividualEditor, resetOutbox, API, bout, letterBtn, shiroSide, marks, NOW,
} from '../helpers/individual_editor.js';
import { teamBout, boutRow, ipponBtn, marks as boutMarks } from '../helpers/team_editor.js';
import { engiBout } from '../helpers/engi_editor.js';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => {
  resetOutbox();
  API.reopenMatch = vi.fn().mockResolvedValue({ applied: true });
  API.requeueBlockerAndReopen = vi.fn().mockResolvedValue({ applied: true });
  API.recordDecision = vi.fn().mockResolvedValue({ applied: true, status: 'completed' });
});

const el = (m) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue({ applied: true })} password="pw" variant="inline" />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const flush = async () => { for (let i = 0; i < 5; i += 1) await act(async () => { await Promise.resolve(); }); };
const WARNING = /^Your unsaved entries will be discarded\. /;
const warnings = () => window.confirmDialog.mock.calls.filter(([o]) => WARNING.test(o?.message || ''));
const answerWarning = (ok) => { window.confirmDialog = vi.fn().mockResolvedValue(ok); };

// A knockout match Tanaka (Shiro) did not appear for, recorded as fusenpai.
const withdrawal = (over = {}) => ({
  id: 'm-r1-0', compId: 'c1', phase: 'knockout', round: 'Round 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  status: 'completed', ipponsA: ['○', '○'], ipponsB: [], hansokuA: 0, hansokuB: 0,
  decision: 'fusenpai', decisionBy: 'shiro', winner: { id: 'p1', name: 'Yamada' },
  modifiedAt: NOW, scoreVersion: 2,
  ...over,
});

describe('individual editor: Clear fusenpai and reopen over unsaved entries', () => {
  it('asks first; Cancel keeps the entries and sends nothing', async () => {
    const r = render(el(withdrawal()));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'M'));
    answerWarning(false);
    await tap(screen.getByTestId('clear-withdrawal-reopen'));
    await flush();
    expect(warnings()).toHaveLength(1);
    expect(warnings()[0][0]).toEqual(expect.objectContaining({
      message: 'Your unsaved entries will be discarded. Reopen anyway?', confirmLabel: 'Discard entries', cancelLabel: 'Keep entries',
    }));
    expect(API.reopenMatch).not.toHaveBeenCalled();
    expect(marks(r.container).join(' '), 'the entry is still on the board').toMatch(/M/);
    expect(screen.getByTestId('clear-withdrawal-reopen').disabled).toBe(false);
  });

  it('asks first; Discard entries sends the reopen and the board shows the server\'s copy', async () => {
    const r = render(el(withdrawal()));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'M'));
    answerWarning(true);
    await tap(screen.getByTestId('clear-withdrawal-reopen'));
    await flush();
    expect(warnings()).toHaveLength(1);
    expect(API.reopenMatch).toHaveBeenCalledTimes(1);
    r.rerender(el(withdrawal({ status: 'running', ipponsA: [], decision: '', decisionBy: '', winner: null, modifiedAt: NOW + 20, scoreVersion: 3 })));
    await flush();
    expect(marks(r.container).join(' '), 'the discarded entry is gone').not.toMatch(/M/);
    expect(screen.queryByTestId('draft-kept-notice')).toBeNull();
  });

  it('asks nothing with no unsaved entries', async () => {
    render(el(withdrawal()));
    await flush();
    await tap(screen.getByTestId('clear-withdrawal-reopen'));
    await flush();
    expect(warnings()).toHaveLength(0);
    expect(API.reopenMatch).toHaveBeenCalledTimes(1);
  });

  it('Discard entries drops them at once: a refused reopen leaves the server\'s copy, and its remedy asks nothing', async () => {
    const r = render(el(withdrawal()));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'M'));
    API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(new Error('Court A already has a running match.'),
      { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'c1' }));
    answerWarning(true);
    await tap(screen.getByTestId('clear-withdrawal-reopen'));
    await flush();
    expect(warnings()).toHaveLength(1);
    expect(marks(r.container).join(' '), 'the discarded entry is gone though the reopen was refused').not.toMatch(/M/);
    answerWarning(false);
    await tap(screen.getByTestId('withdrawal-reopen-requeue-button'));
    await flush();
    expect(warnings(), 'nothing unsaved is left to ask about').toHaveLength(0);
    expect(API.requeueBlockerAndReopen).toHaveBeenCalledTimes(1);
  });

  it('Queue it and reopen asks over entries typed after the refused reopen', async () => {
    const r = render(el(withdrawal()));
    await flush();
    API.reopenMatch = vi.fn().mockRejectedValue(Object.assign(new Error('Court A already has a running match.'),
      { code: 'court_busy', court: 'A', matchId: 'm-blk', compId: 'c1' }));
    await tap(screen.getByTestId('clear-withdrawal-reopen'));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'M'));
    answerWarning(false);
    await tap(screen.getByTestId('withdrawal-reopen-requeue-button'));
    await flush();
    expect(warnings().map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Queue it and reopen anyway?']);
    expect(API.requeueBlockerAndReopen).not.toHaveBeenCalled();
    expect(marks(r.container).join(' ')).toMatch(/M/);
    answerWarning(true);
    await tap(screen.getByTestId('withdrawal-reopen-requeue-button'));
    await flush();
    expect(API.requeueBlockerAndReopen).toHaveBeenCalledTimes(1);
  });
});

describe('individual editor: a decision over unsaved entries', () => {
  const finished = () => bout({ status: 'completed', ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' }, phase: 'knockout', round: 'Round 1' });
  const recordKiken = async () => {
    await tap(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
    await tap(document.querySelector('input[name="decision-side"][value="aka"]'));
    await act(async () => { fireEvent.submit(document.querySelector('form.decision-prompt')); });
    await flush();
  };

  it('asks first; Cancel keeps the entries and sends nothing', async () => {
    const r = render(el(finished()));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'K'));
    answerWarning(false);
    await recordKiken();
    expect(warnings().map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Record the kiken anyway?']);
    expect(API.recordDecision).not.toHaveBeenCalled();
    expect(marks(r.container).join(' ')).toMatch(/K/);
  });

  it('asks first; Discard entries records the decision', async () => {
    const r = render(el(finished()));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'K'));
    answerWarning(true);
    await recordKiken();
    expect(warnings()).toHaveLength(1);
    expect(API.recordDecision).toHaveBeenCalledTimes(1);
  });

  it('asks nothing with no unsaved entries', async () => {
    render(el(finished()));
    await flush();
    await recordKiken();
    expect(warnings()).toHaveLength(0);
    expect(API.recordDecision).toHaveBeenCalledTimes(1);
  });
});

// A finished three-bout knockout team match Osaka won; bouts 2 and 3 drawn.
const teamFinished = (over = {}) => teamBout({
  status: 'completed', modifiedAt: NOW, scoreVersion: 2, winner: 'Osaka',
  subResults: [boutRow(1, { ipponsB: ['K'], winner: 'Osaka' }), boutRow(2, { decision: 'hikiwake' }), boutRow(3, { decision: 'hikiwake' })],
  ...over,
});

describe('team editor: Add representative bout over unsaved entries', () => {
  beforeEach(() => {
    API.recordDaihyosen = vi.fn().mockResolvedValue({ applied: true, status: 'running' });
  });

  it('asks first; Cancel keeps the entries and sends nothing', async () => {
    const r = render(el(teamFinished()));
    await flush();
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    answerWarning(false);
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    await flush();
    expect(warnings().map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Add the representative bout anyway?']);
    expect(API.recordDaihyosen).not.toHaveBeenCalled();
    expect(boutMarks(r.container, 0, 'aka')).toContain('M');
  });

  it('asks first; Discard entries adds the bout and the board shows the server\'s copy', async () => {
    const r = render(el(teamFinished()));
    await flush();
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    answerWarning(true);
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    await flush();
    expect(warnings()).toHaveLength(1);
    expect(API.recordDaihyosen).toHaveBeenCalledTimes(1);
    expect(boutMarks(r.container, 0, 'aka'), 'the discarded entry is gone').not.toContain('M');
    expect(boutMarks(r.container, 0, 'shiro')).toContain('K');
  });

  it('asks nothing with no unsaved entries', async () => {
    render(el(teamFinished()));
    await flush();
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    await flush();
    expect(warnings()).toHaveLength(0);
    expect(API.recordDaihyosen).toHaveBeenCalledTimes(1);
  });

  it('a queued sheet asks nothing: the Start before the add carries its entries', async () => {
    const r = render(el(teamBout({ status: 'scheduled', modifiedAt: NOW, scoreVersion: 2 })));
    await flush();
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    await flush();
    expect(warnings()).toHaveLength(0);
    expect(API.recordDaihyosen).toHaveBeenCalledTimes(1);
  });
});

describe('team editor: Remove representative bout over unsaved entries', () => {
  const withRep = () => teamFinished({
    winner: 'Osaka',
    subResults: [boutRow(1), boutRow(2, { decision: 'hikiwake' }), boutRow(3, { decision: 'hikiwake' }),
      // Unscored: Remove is offered only before the bout has a point.
      boutRow(-1, { sideA: 'Kyoto', sideB: 'Osaka', decision: 'daihyosen' })],
  });
  beforeEach(() => {
    API.removeDaihyosen = vi.fn().mockResolvedValue({ applied: true, status: 'running' });
  });

  it('asks first; Cancel keeps the entries and sends nothing; Discard entries removes it', async () => {
    const r = render(el(withRep()));
    await flush();
    await tap(ipponBtn(r.container, 0, 'aka', 'M'));
    answerWarning(false);
    await tap(screen.getByTestId('team-daihyosen-remove'));
    await flush();
    expect(warnings().map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Remove the representative bout anyway?']);
    expect(API.removeDaihyosen).not.toHaveBeenCalled();
    expect(boutMarks(r.container, 0, 'aka')).toContain('M');
    answerWarning(true);
    await tap(screen.getByTestId('team-daihyosen-remove'));
    await flush();
    expect(API.removeDaihyosen).toHaveBeenCalledTimes(1);
    expect(boutMarks(r.container, 0, 'aka')).not.toContain('M');
  });
});

describe('team editor: Clear kiken and reopen over unsaved entries', () => {
  // A finished pool team match Kyoto (Aka) withdrew from after bout 1.
  const teamWithdrawal = () => teamBout({
    status: 'completed', phase: 'pool', poolName: 'Pool 1', modifiedAt: NOW, scoreVersion: 2,
    subResults: [boutRow(1, { ipponsA: ['M'], winner: 'Kyoto' }), boutRow(2), boutRow(3)],
    decision: 'kiken-voluntary', decisionBy: 'aka', winner: { id: 'tb', name: 'Osaka' }, ipponsB: ['○', '○'],
  });

  it('asks first; Cancel keeps the entries and sends nothing; Discard entries reopens', async () => {
    const r = render(el(teamWithdrawal()));
    await flush();
    await tap(ipponBtn(r.container, 1, 'shiro', 'K'));
    answerWarning(false);
    await tap(screen.getByTestId('clear-withdrawal-reopen'));
    await flush();
    expect(warnings().map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Reopen anyway?']);
    expect(API.reopenMatch).not.toHaveBeenCalled();
    expect(boutMarks(r.container, 1, 'shiro')).toContain('K');
    answerWarning(true);
    await tap(screen.getByTestId('clear-withdrawal-reopen'));
    await flush();
    expect(API.reopenMatch).toHaveBeenCalledTimes(1);
  });
});

describe('engi editor', () => {
  it('offers no reopen, decision or representative bout, so nothing asks', async () => {
    const r = render(el(engiBout({ status: 'completed', flagsA: 3, flagsB: 0, modifiedAt: NOW, scoreVersion: 2 })));
    await flush();
    await tap(r.container.querySelector('[data-testid="engi-shiro-inc"]'));
    for (const id of ['clear-withdrawal-reopen', 'scoring-modal-kiken-voluntary-button', 'scoring-modal-daihyosen-button']) {
      expect(screen.queryByTestId(id)).toBeNull();
    }
    expect([...document.querySelectorAll('button')].some((b) => /Reopen/.test(b.textContent))).toBe(false);
  });
});

// Stage 5 review 8.
describe('a decision over a kept draft (finding 1)', () => {
  for (const from of ['scheduled', 'completed']) {
    it(`${from}: Discard entries drops the kept draft, so no "another device" note follows the own decision`, async () => {
      let m = from === 'scheduled'
        ? bout({ status: 'scheduled', modifiedAt: NOW, scoreVersion: 1 })
        : bout({ status: 'completed', ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' }, modifiedAt: NOW, scoreVersion: 1 });
      const r = render(el(m));
      await flush();
      await tap(letterBtn(shiroSide(r.container), 'K'));
      m = { ...m, status: 'running', winner: null, modifiedAt: NOW + 10, scoreVersion: 2 };
      r.rerender(el(m));
      await flush();
      expect(screen.queryByTestId('draft-kept-notice')).not.toBeNull();
      answerWarning(true);
      await tap(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
      await tap(document.querySelector('input[name="decision-side"][value="aka"]'));
      await act(async () => { fireEvent.submit(document.querySelector('form.decision-prompt')); });
      await flush();
      expect(warnings()).toHaveLength(1);
      expect(API.recordDecision).toHaveBeenCalledTimes(1);
      expect(screen.queryByTestId('draft-kept-notice'), 'the kept draft went with Discard entries').toBeNull();
      m = { ...m, status: 'completed', decision: 'kiken-voluntary', decisionBy: 'aka', winner: { id: 'p2', name: 'Tanaka' }, modifiedAt: NOW + 20, scoreVersion: 3 };
      r.rerender(el(m));
      await flush();
      expect(screen.queryByTestId('draft-dropped-notice')).toBeNull();
      expect(screen.queryByTestId('draft-kept-notice')).toBeNull();
    });
  }

  it('Keep entries keeps the kept draft and sends nothing', async () => {
    let m = bout({ status: 'scheduled', modifiedAt: NOW, scoreVersion: 1 });
    const r = render(el(m));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'K'));
    m = { ...m, status: 'running', modifiedAt: NOW + 10, scoreVersion: 2 };
    r.rerender(el(m));
    await flush();
    answerWarning(false);
    await tap(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
    await tap(document.querySelector('input[name="decision-side"][value="aka"]'));
    await act(async () => { fireEvent.submit(document.querySelector('form.decision-prompt')); });
    await flush();
    expect(API.recordDecision).not.toHaveBeenCalled();
    expect(screen.queryByTestId('draft-kept-notice')).not.toBeNull();
    expect(marks(r.container).join(' ')).toMatch(/K/);
  });
});

describe('a decision after Discard entries carries the server\'s overtime (finding 2)', () => {
  it('individual: an unsaved overtime period is not recorded with the kiken', async () => {
    const m = bout({ status: 'completed', ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' }, phase: 'knockout', round: 'Round 1' });
    render(el(m));
    await flush();
    const pill = screen.queryByTestId('scoring-modal-encho-pill');
    if (pill) await tap(pill);
    const cb = screen.queryByTestId('scoring-modal-encho-checkbox');
    if (cb) await tap(cb);
    const inc = document.querySelector('[aria-label="Increase overtime period count"]');
    if (inc) await tap(inc);
    answerWarning(true);
    await tap(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
    await tap(document.querySelector('input[name="decision-side"][value="aka"]'));
    await act(async () => { fireEvent.submit(document.querySelector('form.decision-prompt')); });
    await flush();
    expect(warnings(), 'the overtime change is unsaved: the warning is asked').toHaveLength(1);
    expect(API.recordDecision).toHaveBeenCalledTimes(1);
    expect(API.recordDecision.mock.calls[0][2].encho, 'the told-discarded overtime is not sent').toBeUndefined();
  });

  it('individual: the server\'s overtime stays on a decision over another unsaved change', async () => {
    const m = bout({ status: 'completed', ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' }, encho: { periodCount: 1 }, phase: 'knockout', round: 'Round 1' });
    const r = render(el(m));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'K'));
    answerWarning(true);
    await tap(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
    await tap(document.querySelector('input[name="decision-side"][value="aka"]'));
    await act(async () => { fireEvent.submit(document.querySelector('form.decision-prompt')); });
    await flush();
    expect(API.recordDecision.mock.calls[0][2].encho).toEqual({ periodCount: 1 });
  });
});

describe('the barred-match notice asks too (finding 6)', () => {
  it('Record fusensho over unsaved entries asks first; Keep entries sends nothing', async () => {
    const m = bout({ status: 'scheduled', ineligibleSides: { b: 'kiken-voluntary' }, modifiedAt: NOW, scoreVersion: 1 });
    const r = render(el(m));
    await flush();
    const btn = () => [...document.querySelectorAll('button')].find((b) => b.textContent.trim().startsWith('Record fusensho'));
    if (!btn()) throw new Error('no Record fusensho button');
    // A typed draft, where the editor offers one with the notice standing.
    const k = letterBtn(shiroSide(r.container), 'K');
    if (k && !k.disabled) await tap(k);
    expect(marks(r.container).join(' '), 'the draft is typed').toContain('K');
    answerWarning(false);
    await tap(btn());
    await flush();
    expect(warnings().map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Record the fusensho anyway?']);
    expect(API.recordDecision).not.toHaveBeenCalled();
    answerWarning(true);
    await tap(btn());
    await flush();
    expect(API.recordDecision).toHaveBeenCalledTimes(1);
  });
});

describe('the editors publish their unsaved entries to the host (finding 3)', () => {
  const host = (m, ref) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue({ applied: true })} password="pw" variant="inline" hostUnsavedRef={ref} />;
  it('individual, team and engi: the host reads the editor\'s isDirty, and nothing once it is gone', async () => {
    for (const [m, type] of [
      [bout({ status: 'completed', ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' } }), async (c) => tap(letterBtn(shiroSide(c), 'K'))],
      [teamFinished(), async (c) => tap(ipponBtn(c, 0, 'aka', 'M'))],
      [engiBout({ status: 'completed', flagsA: 3, flagsB: 0, modifiedAt: NOW, scoreVersion: 2 }), async (c) => tap(c.querySelector('[data-testid="engi-shiro-inc"]'))],
    ]) {
      const ref = { current: null };
      const r = render(host(m, ref));
      await flush();
      expect(typeof ref.current).toBe('function');
      expect(ref.current()).toBe(false);
      await type(r.container);
      expect(ref.current()).toBe(true);
      r.unmount();
      expect(ref.current).toBeNull();
    }
  });
});

describe('a decision after Discard entries on a running match (finding 2)', () => {
  it('carries the overtime on the board, this device\'s pending tap included', async () => {
    const m = bout({ status: 'running', modifiedAt: NOW, scoreVersion: 3, phase: 'knockout', round: 'Round 1' });
    render(el(m));
    await flush();
    const pill = screen.queryByTestId('scoring-modal-encho-pill');
    if (pill) await tap(pill);
    const cb = screen.queryByTestId('scoring-modal-encho-checkbox');
    if (cb) await tap(cb);
    const inc = document.querySelector('[aria-label="Increase overtime period count"]');
    if (inc) await tap(inc);
    // The Mark draw toggle is local until Finish: the unsaved entry here.
    const draw = screen.queryByTestId('scoring-modal-mark-draw');
    if (!draw || draw.disabled) throw new Error('no Mark draw on the running board');
    await tap(draw);
    answerWarning(true);
    await tap(screen.getByTestId('scoring-modal-kiken-voluntary-button'));
    await tap(document.querySelector('input[name="decision-side"][value="aka"]'));
    await act(async () => { fireEvent.submit(document.querySelector('form.decision-prompt')); });
    await flush();
    expect(warnings()).toHaveLength(1);
    const sent = API.recordDecision.mock.calls[0][2].encho;
    const tapped = API.recordTap.mock.calls.filter(([, , ev]) => ev && ev.kind === 'encho.add').length;
    expect(tapped, 'the overtime went as taps').toBeGreaterThan(0);
    expect(sent ? sent.periodCount : 0, 'the overtime the board shows').toBe(tapped);
  });
});

describe('Discard entries on a leave: the exit sees nothing unsaved (stage 5 review 11, minor 1)', () => {
  const host = (m, ref) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue({ applied: true })} password="pw" variant="inline" hostUnsavedRef={ref} />;
  it('individual, team and engi: `go` runs after the answer, asks once, and reads the editor as clean', async () => {
    for (const [m, type] of [
      [bout({ status: 'completed', ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' } }), async (c) => tap(letterBtn(shiroSide(c), 'K'))],
      [teamFinished(), async (c) => tap(ipponBtn(c, 0, 'aka', 'M'))],
      [engiBout({ status: 'completed', flagsA: 3, flagsB: 0, modifiedAt: NOW, scoreVersion: 2 }), async (c) => tap(c.querySelector('[data-testid="engi-shiro-inc"]'))],
    ]) {
      const ref = { current: null };
      const r = render(host(m, ref));
      await flush();
      await type(r.container);
      expect(ref.current()).toBe(true);
      answerWarning(true);
      let seenByGo = null;
      let went = 0;
      await act(async () => {
        const { leaveThen, holdsUnsaved } = await import('../../unsaved_entries.jsx');
        leaveThen(ref, () => { went += 1; seenByGo = holdsUnsaved(ref); }, () => true);
        for (let i = 0; i < 6; i += 1) await Promise.resolve();
      });
      expect(went).toBe(1);
      expect(seenByGo, 'the exit would ask a second time if it read the editor as still unsaved').toBe(false);
      expect(warnings()).toHaveLength(1);
      await flush();
      r.unmount();
    }
  });
  it('a new entry made after the discard is unsaved again', async () => {
    const ref = { current: null };
    const r = render(host(bout({ status: 'completed', ipponsA: ['M'], winner: { id: 'p1', name: 'Yamada' } }), ref));
    await flush();
    await tap(letterBtn(shiroSide(r.container), 'K'));
    answerWarning(true);
    await act(async () => {
      const { confirmLeave } = await import('../../unsaved_entries.jsx');
      await confirmLeave(ref, () => true);
    });
    await flush();
    expect(ref.current()).toBe(false);
    await tap(letterBtn(shiroSide(r.container), 'K'));
    expect(ref.current()).toBe(true);
    r.unmount();
  });
});
