// bc-dsid: the withdrawal / no-show prompt preselects no side. Record stays
// off until the operator picks one, then names it. The submit-flow tests
// moved here from the fake-React admin_scoring_modal.test.jsx, each picking a
// side first.

import React from 'react';
import { render, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let restoreGlobals;
let DecisionPrompt;
let buildDecisionBody;
let submitDecisionRequest;
let withdrawalConsequence;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({
    isKikenDecision: (d) => d === 'kiken' || d === 'kiken-voluntary' || d === 'kiken-injury',
  });
  ({ DecisionPrompt, buildDecisionBody, submitDecisionRequest, withdrawalConsequence } =
    await import('../../admin_scoring_shared.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.API = { recordDecision: vi.fn().mockResolvedValue({}) };
});

function mount(props = {}) {
  const onSubmit = props.onSubmit || vi.fn();
  const utils = render(
    <DecisionPrompt
      kind="kiken-voluntary"
      sideA={{ name: 'Tora' }}
      sideB={{ name: 'Kuma' }}
      askReason={false}
      onCancel={vi.fn()}
      submitting={false}
      {...props}
      onSubmit={onSubmit}
    />,
  );
  return { ...utils, onSubmit };
}

const pick = (container, value) =>
  fireEvent.click(container.querySelector(`input[name="decision-side"][value="${value}"]`));
const recordButton = () => screen.getByRole('button', { name: /^Record/ });

describe('DecisionPrompt side picker', () => {
  it('starts with no side picked, Record off, and a hint', () => {
    const { container, onSubmit } = mount();
    for (const r of container.querySelectorAll('input[name="decision-side"]')) expect(r.checked).toBe(false);
    expect(recordButton().disabled).toBe(true);
    expect(recordButton().textContent).toBe('Record');
    expect(screen.getByTestId('decision-prompt-hint').textContent).toBe('Pick the side that withdrew.');
    fireEvent.submit(container.querySelector('form'));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('hint for a no-show reads "did not show up"', () => {
    mount({ kind: 'fusenpai' });
    expect(screen.getByTestId('decision-prompt-hint').textContent).toBe('Pick the side that did not show up.');
  });

  it('picking Aka enables Record, names the side and shows the consequence', () => {
    const { container, onSubmit } = mount();
    pick(container, 'aka');
    expect(recordButton().disabled).toBe(false);
    expect(recordButton().textContent).toBe('Record: AKA withdrew');
    expect(screen.queryByTestId('decision-prompt-hint')).toBeNull();
    expect(screen.getByTestId('decision-prompt-consequence').textContent)
      .toBe('Tora cannot fight again in this competition. Kuma wins this match.');
    fireEvent.submit(container.querySelector('form'));
    expect(onSubmit).toHaveBeenCalledWith({ decisionBy: 'aka', decisionReason: '' });
  });

  it('names SHIRO for a no-show, and the consequence follows the kind', () => {
    const { container } = mount({ kind: 'fusenpai' });
    pick(container, 'shiro');
    expect(recordButton().textContent).toBe('Record: SHIRO did not show up');
    expect(screen.getByTestId('decision-prompt-consequence').textContent)
      .toBe('Kuma cannot fight again in this competition. Tora wins this match.');
  });

  it('a kiken-injury consequence says the competitor can be reinstated', () => {
    const { container } = mount({ kind: 'kiken-injury' });
    pick(container, 'shiro');
    expect(screen.getByTestId('decision-prompt-consequence').textContent)
      .toBe('Kuma cannot fight again unless reinstated. Tora wins this match.');
    expect(withdrawalConsequence('kiken', 'A', 'B')).toBe('A cannot fight again in this competition. B wins this match.');
  });

  it('each option is named by its side word and its competitor', () => {
    mount();
    expect(screen.getByRole('radio', { name: 'SHIRO (White): Kuma' })).toBeTruthy();
    expect(screen.getByRole('radio', { name: 'AKA (Red): Tora' })).toBeTruthy();
  });

  it('is a no-op while submitting', () => {
    const { container, onSubmit } = mount({ submitting: true });
    pick(container, 'aka');
    fireEvent.submit(container.querySelector('form'));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('sends the trimmed reason when one is asked for', () => {
    const { container, onSubmit } = mount({ askReason: true });
    pick(container, 'shiro');
    fireEvent.input(screen.getByTestId('decision-reason'), { target: { value: '  injury ' } });
    fireEvent.submit(container.querySelector('form'));
    expect(onSubmit).toHaveBeenCalledWith({ decisionBy: 'shiro', decisionReason: 'injury' });
  });

  it('the parent flow: onSubmit -> submitDecisionRequest -> recordDecision', async () => {
    const onSubmit = vi.fn((payload) =>
      submitDecisionRequest('comp-1', 'match-1', 'kiken-voluntary', payload, 0, 'explicit-pw'),
    );
    const { container } = mount({ askReason: true, onSubmit });
    pick(container, 'aka');
    fireEvent.submit(container.querySelector('form'));
    await Promise.resolve();
    expect(window.API.recordDecision).toHaveBeenCalledWith(
      'comp-1', 'match-1', { decision: 'kiken-voluntary', decisionBy: 'aka' }, 'explicit-pw',
    );
  });

  it('parent flow includes encho.periodCount in the body when > 0', async () => {
    const onSubmit = vi.fn((payload) =>
      window.API.recordDecision('comp-1', 'match-1', buildDecisionBody('hikiwake', payload, 3), 'pw'),
    );
    const { container } = mount({ kind: 'hikiwake', onSubmit });
    pick(container, 'shiro');
    fireEvent.submit(container.querySelector('form'));
    await Promise.resolve();
    expect(window.API.recordDecision).toHaveBeenCalledWith(
      'comp-1', 'match-1', { decision: 'hikiwake', decisionBy: 'shiro', encho: { periodCount: 3 } }, 'pw',
    );
  });

  it('fusenpai: decisionBy is the ABSENT/LOSING side, not the winning side', () => {
    // Picking "shiro" means SHIRO did not show up, so AKA wins.
    const onSubmit = vi.fn((payload) => buildDecisionBody('fusenpai', payload, 0));
    const { container } = mount({ kind: 'fusenpai', sideA: { name: 'Hayashi' }, sideB: { name: 'Nakamura' }, onSubmit });
    pick(container, 'shiro');
    fireEvent.submit(container.querySelector('form'));
    expect(onSubmit).toHaveBeenCalledWith({ decisionBy: 'shiro', decisionReason: '' });
    expect(onSubmit.mock.results[0].value).toEqual({ decision: 'fusenpai', decisionBy: 'shiro' });
  });
});
