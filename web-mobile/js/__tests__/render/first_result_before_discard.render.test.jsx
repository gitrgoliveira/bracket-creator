import { act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { setupShiaijoConsole, probe, mountCourt, courtMatch } from '../helpers/shiaijo_console.js';
import { resetFirstResultConfirm } from '../../first_result_confirm.jsx';

setupShiaijoConsole();

// bc-drev (operator decision D4 b) with R1: a pick of another bout over the
// editor's unsaved entries meets two questions, "Start <competition>?" and
// "Your unsaved entries will be discarded". The first-result question comes
// FIRST, so a Not yet never discards what the editor holds (the same order
// BarredMatchNotice and makeSubmitDecision use).
describe('the court console asks the first-result question before the discard question', () => {
  const WARN = /^Your unsaved entries will be discarded\. /;
  let prevConfirm;
  beforeEach(() => { prevConfirm = window.confirmDialog; resetFirstResultConfirm(); });
  afterEach(() => { window.confirmDialog = prevConfirm; resetFirstResultConfirm(); });

  const mount = () => mountCourt(
    [courtMatch('m1', 'running', { modifiedAt: 1000 }), courtMatch('m2', 'scheduled')],
    { comps: [{ id: 'c1', name: 'Cup', status: 'draw-ready', poolMatches: [], bracket: null }] },
  );
  const tapStart = async (c) => {
    const card = c.utils.container.querySelector('.shiaijo-upnext__card');
    const start = [...card.querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));
    await act(async () => { start.click(); });
  };
  const holdUnsaved = () => {
    const discard = vi.fn();
    const unsaved = () => true;
    unsaved.discard = discard;
    probe.props.hostUnsavedRef.current = unsaved;
    return discard;
  };

  it('Not yet to the first result drops no entries and asks nothing more', async () => {
    const c = await mount();
    try {
      const discard = holdUnsaved();
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await tapStart(c);
      expect(window.confirmDialog).toHaveBeenCalledTimes(1);
      expect(window.confirmDialog.mock.calls[0][0].title).toBe('Start Cup?');
      expect(discard).not.toHaveBeenCalled();
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(c.editorMatch()).toBe('m1');
    } finally { c.restore(); }
  });

  it('agreed, the discard question follows; Keep entries then changes nothing', async () => {
    const c = await mount();
    try {
      const discard = holdUnsaved();
      window.confirmDialog = vi.fn().mockResolvedValueOnce(true).mockResolvedValueOnce(false);
      await tapStart(c);
      const asked = window.confirmDialog.mock.calls.map(([o]) => o.title || o.message);
      expect(asked[0]).toBe('Start Cup?');
      expect(window.confirmDialog.mock.calls.filter(([o]) => WARN.test((o && o.message) || ''))).toHaveLength(1);
      expect(discard).not.toHaveBeenCalled();
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(c.editorMatch()).toBe('m1');
    } finally { c.restore(); }
  });
});
