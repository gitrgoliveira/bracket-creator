// bc-drev (operator decision D4 b): an editor's own Start of a drawn
// competition's first match asks "Start <competition>?" BEFORE the editor
// shows the match as running. Before, the owner began the start first, so the
// board showed the match running, and Start hidden, behind the open question,
// and a Not yet snapped it back. Now a Not yet leaves the board as it was
// (Start still offered, nothing begun, nothing sent), and agreeing sends the
// start once.
//
// The team sheet's representative-bout add on a queued match saves the sheet
// first, a start in all but name: it asks the same question before anything is
// shown or queued, so an add never reaches the outbox ahead of the answer.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, bout, API } from '../helpers/individual_editor.js';
import { teamBout, kachinukiBout, ipponBtn } from '../helpers/team_editor.js';
import { noteFirstResultCompetitions, resetFirstResultConfirm } from '../../first_result_confirm.jsx';

let ScoreEditorModal;
let restore;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => {
  resetOutbox();
  resetFirstResultConfirm();
  noteFirstResultCompetitions([{ id: 'c1', name: 'Women', status: 'draw-ready', poolMatches: [], bracket: null }]);
});
afterEach(() => resetFirstResultConfirm());

const el = (m, onSubmit) => <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} password="pw" variant="inline" />;
const tap = (node) => act(async () => { fireEvent.click(node); });
const startButton = () => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Start match');
const settle = () => act(async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); });

describe.each([
  ['individual', () => bout({ status: 'scheduled', scoreVersion: 2 })],
  ['team', () => teamBout({ status: 'scheduled', phase: 'pool', scoreVersion: 2 })],
])('%s: the Start asks before the editor shows the match as running', (_name, makeMatch) => {
  it('Not yet leaves Start offered and sends nothing', async () => {
    const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }));
    window.confirmDialog = vi.fn(() => new Promise(() => {})); // the question stays open
    render(el(makeMatch(), onSubmit));
    await tap(startButton());
    await settle();
    // While the question is open the board is as it was.
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].title).toBe('Start Women?');
    expect(startButton(), 'Start is still offered').toBeTruthy();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('Not yet, answered, sends nothing and still offers Start', async () => {
    const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }));
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    render(el(makeMatch(), onSubmit));
    await tap(startButton());
    await settle();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(startButton()).toBeTruthy();
    expect(screen.queryByTestId('start-refused-notice')).toBeNull();
  });

  it('agreed, the start is sent once and the competition is not asked about again', async () => {
    const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }));
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    render(el(makeMatch(), onSubmit));
    await tap(startButton());
    await settle();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ status: 'running' });
  });

  it('a competition already started asks nothing and starts in the same tick', async () => {
    noteFirstResultCompetitions([{ id: 'c1', name: 'Women', status: 'pools', poolMatches: [], bracket: null }]);
    const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }));
    render(el(makeMatch(), onSubmit));
    await act(async () => { fireEvent.click(startButton()); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});

describe('Record bout on a queued kachinuki encounter (its start) asks first', () => {
  const queued = () => kachinukiBout({ status: 'scheduled', phase: 'bracket', scoreVersion: 2 });
  const recordButton = () => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Record bout');
  const scoreTwo = async (r) => { await tap(ipponBtn(r.container, 0, 'aka', 'M')); await tap(ipponBtn(r.container, 0, 'aka', 'M')); };

  it('Not yet begins no start and sends nothing, and Start and Record bout stay offered', async () => {
    const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }));
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const r = render(el(queued(), onSubmit));
    await scoreTwo(r);
    await tap(recordButton());
    await settle();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].title).toBe('Start Women?');
    expect(onSubmit).not.toHaveBeenCalled();
    expect(startButton(), 'Start is still offered').toBeTruthy();
    expect(recordButton()).toBeTruthy();
  });

  it('agreed, the bout is recorded once and the competition is not asked again', async () => {
    const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }));
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const r = render(el(queued(), onSubmit));
    await scoreTwo(r);
    await tap(recordButton());
    await settle();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ status: 'running', kachinukiBoutFinal: true });
  });
});

describe('the team sheet\'s representative-bout add over a queued match asks first', () => {
  const queuedKnockout = () => teamBout({ status: 'scheduled', compFormat: 'knockout', teamMatchType: 'fixed', scoreVersion: 2 });

  it('Not yet adds nothing and saves nothing', async () => {
    const onSubmit = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }));
    API.recordDaihyosen = vi.fn(() => Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }));
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    render(el(queuedKnockout(), onSubmit));
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    await settle();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].title).toBe('Start Women?');
    expect(onSubmit).not.toHaveBeenCalled();
    expect(API.recordDaihyosen).not.toHaveBeenCalled();
  });

  it('agreed, the sheet is saved first and then the bout is added, once', async () => {
    const order = [];
    const onSubmit = vi.fn(() => { order.push('start'); return Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }); });
    API.recordDaihyosen = vi.fn(() => { order.push('add'); return Promise.resolve({ applied: true, pending: true, settled: new Promise(() => {}) }); });
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    render(el(queuedKnockout(), onSubmit));
    await tap(screen.getByTestId('scoring-modal-daihyosen-button'));
    await settle();
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(order).toEqual(['start', 'add']);
  });
});
