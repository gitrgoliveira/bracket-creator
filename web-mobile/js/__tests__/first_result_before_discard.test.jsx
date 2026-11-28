import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeSubmitDecision } from '../admin_scoring_shared.jsx';
import {
  noteFirstResultCompetitions, resetFirstResultConfirm, FIRST_RESULT_CANCELLED,
} from '../first_result_confirm.jsx';

// bc-drev (operator decision D4 b): a decision over unsaved entries meets two
// questions, "Start <competition>?" and "Your unsaved entries will be
// discarded". The first-result question comes FIRST, as BarredMatchNotice asks
// it, so a Not yet never discards what the editor holds: the operator keeps
// both the entries and the draft competition.

describe('makeSubmitDecision asks the first-result question before the discard question', () => {
  let savedAPI;
  let savedConfirm;
  beforeEach(() => {
    savedAPI = window.API;
    savedConfirm = window.confirmDialog;
    window.API = { recordDecision: vi.fn().mockResolvedValue({ applied: true }) };
    resetFirstResultConfirm();
    noteFirstResultCompetitions([{ id: 'c1', name: 'Women', status: 'draw-ready', poolMatches: [], bracket: null }]);
  });
  afterEach(() => {
    window.API = savedAPI;
    window.confirmDialog = savedConfirm;
    resetFirstResultConfirm();
  });

  const build = (over = {}) => {
    const setters = {
      mountedRef: { current: true },
      setDecisionSubmitting: vi.fn(),
      setDecisionErr: vi.fn(),
      setDecisionPromptKind: vi.fn(),
    };
    const onDiscard = vi.fn();
    const submit = makeSubmitDecision({
      match: { compId: 'c1', id: 'Pool 1-0', sideA: { id: 'pa', name: 'Aka' }, sideB: { id: 'pb', name: 'Shiro' } },
      enchoPeriodCount: 0, password: 'pw', onClose: vi.fn(), isComplete: false,
      unsaved: true, onDiscard, ...setters, ...over,
    });
    return { submit, onDiscard, ...setters };
  };

  it('Not yet to the first result discards nothing, asks nothing more and records nothing', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const { submit, onDiscard, setDecisionErr } = build();
    await submit('fusenpai', { decisionBy: 'shiro', decisionReason: '' });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].title).toBe('Start Women?');
    expect(onDiscard).not.toHaveBeenCalled();
    expect(window.API.recordDecision).not.toHaveBeenCalled();
    expect(setDecisionErr).toHaveBeenLastCalledWith(FIRST_RESULT_CANCELLED);
  });

  it('agreed, the discard question follows; Keep entries then records nothing and keeps the entries', async () => {
    window.confirmDialog = vi.fn().mockResolvedValueOnce(true).mockResolvedValueOnce(false);
    const { submit, onDiscard } = build();
    await submit('fusenpai', { decisionBy: 'shiro', decisionReason: '' });

    const titles = window.confirmDialog.mock.calls.map(([o]) => o.title);
    expect(titles[0]).toBe('Start Women?');
    expect(titles).toHaveLength(2);
    expect(onDiscard).not.toHaveBeenCalled();
    expect(window.API.recordDecision).not.toHaveBeenCalled();
  });

  it('agreed twice, the entries are discarded once and the decision recorded once', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const { submit, onDiscard } = build();
    await submit('fusenpai', { decisionBy: 'shiro', decisionReason: '' });

    // The first-result question is not asked a second time by the write itself.
    expect(window.confirmDialog).toHaveBeenCalledTimes(2);
    expect(onDiscard).toHaveBeenCalledTimes(1);
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
  });

  it('a competition already started asks only the discard question', async () => {
    noteFirstResultCompetitions([{ id: 'c1', name: 'Women', status: 'pools', poolMatches: [], bracket: null }]);
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const { submit, onDiscard } = build();
    await submit('fusenpai', { decisionBy: 'shiro', decisionReason: '' });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message).toMatch(/^Your unsaved entries will be discarded\. /);
    expect(onDiscard).toHaveBeenCalledTimes(1);
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
  });
});
