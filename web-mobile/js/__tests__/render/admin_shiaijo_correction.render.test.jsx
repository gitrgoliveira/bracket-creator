import { act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { setupShiaijoConsole, probe, mountCourt, courtSide, courtMatch } from '../helpers/shiaijo_console.js';
import { startWhileCorrectingMessage, correctWhileRunningMessage } from '../../write_result.jsx';
import { scoreRowMatchName } from '../../pool_ids.jsx';

setupShiaijoConsole();

// bc-crpn (operator ruling 2026-09-27): the court console never holds a
// running match and an open correction through its own taps, in either order.
// Correct is refused while a bout is live; Start match is refused while a
// correction is open; a landed Save correction ends the correction. A match
// started from another screen mid-correction does NOT close it (operator
// decision 2026-10-07): the running match is listed beside it instead.
describe('a running match and an open correction never coexist on the console (bc-crpn)', () => {
  const tapCorrect = async (c, rowText) => {
    const row = [...c.utils.container.querySelectorAll('.shiaijo-qrow--complete')].find((r) => r.textContent.includes(rowText));
    const btn = [...row.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Correct');
    await act(async () => { btn.click(); });
    return row;
  };
  const tapUpNextStart = async (c) => {
    const card = c.utils.container.querySelector('.shiaijo-upnext__card');
    const btn = [...card.querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));
    await act(async () => { btn.click(); });
    return card;
  };
  const heading = (c) => c.utils.container.querySelector('.shiaijo-context__toggle').textContent;
  const rowOf = (c, text) => [...c.utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(text));
  const alertsWith = (c, text) => [...c.utils.container.querySelectorAll('[role="alert"]')].filter((a) => a.textContent.includes(text));

  it('a) Correct is refused while a bout runs on this court, and the notice goes with the bout', async () => {
    const m1 = courtMatch('m1', 'completed', { modifiedAt: 1000, winner: courtSide('m1-a', 'Aka m1') });
    const m2 = courtMatch('m2', 'running');
    const c = await mountCourt([m1, m2]);
    try {
      const row = await tapCorrect(c, 'Aka m1');
      const text = correctWhileRunningMessage({ court: 'A', label: scoreRowMatchName(m2) });
      expect(c.editorMatch()).toBe('m2');
      expect(heading(c)).not.toContain('Correcting');
      const alert = row.querySelector('[role="alert"]');
      expect(alert && alert.textContent).toBe(text);
      expect(c.showToast).toHaveBeenCalledWith(text, 'error');

      c.feed.current = [m1, { ...m2, status: 'completed', modifiedAt: 2000 }];
      await c.refresh();
      expect(alertsWith(c, text)).toHaveLength(0);
    } finally { c.restore(); }
  });

  it('b) Correct is refused in the window after this console\'s own Start, and while the start is in flight', async () => {
    const m1 = courtMatch('m1', 'completed', { modifiedAt: 1000 });
    const m2 = courtMatch('m2', 'scheduled', { modifiedAt: 100 });
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const c = await mountCourt([m1, m2], { onEditScore });
    try {
      await tapUpNextStart(c);
      expect(onEditScore).toHaveBeenCalledTimes(1);
      // The feed has NOT caught up: m2 still reads "scheduled".
      await tapCorrect(c, 'Aka m1');
      const text = correctWhileRunningMessage({ court: 'A', label: scoreRowMatchName(m2) });
      expect(c.showToast).toHaveBeenCalledWith(text, 'error');
      expect(heading(c)).not.toContain('Correcting');
    } finally { c.restore(); }

    const m1b = courtMatch('m1', 'completed', { modifiedAt: 1000 });
    const m2b = courtMatch('m2', 'scheduled', { modifiedAt: 100 });
    const pending = vi.fn().mockImplementation(() => new Promise(() => {}));
    const c2 = await mountCourt([m1b, m2b], { onEditScore: pending });
    try {
      await tapUpNextStart(c2);
      await tapCorrect(c2, 'Aka m1');
      expect(c2.showToast).toHaveBeenCalledWith(correctWhileRunningMessage({ court: 'A', label: scoreRowMatchName(m2b) }), 'error');
      expect(heading(c2)).not.toContain('Correcting');
    } finally { c2.restore(); }
  });

  it('c) Start is refused while a correction is open, on the Up next card and on an Upcoming row', async () => {
    const m1 = courtMatch('m1', 'completed', { modifiedAt: 1000 });
    const m2 = courtMatch('m2', 'scheduled');
    const m3 = courtMatch('m3', 'scheduled');
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const c = await mountCourt([m1, m2, m3], { onEditScore });
    try {
      await tapCorrect(c, 'Aka m1');
      expect(c.editorMatch()).toBe('m1');
      const text = startWhileCorrectingMessage({ label: scoreRowMatchName(m1) });

      const card = await tapUpNextStart(c);
      expect(onEditScore).not.toHaveBeenCalled();
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(c.editorMatch()).toBe('m1');
      expect(heading(c)).toContain('Correcting');
      expect(card.querySelector('.shiaijo-upnext__error[role="alert"]').textContent).toBe(text);
      expect(c.showToast).toHaveBeenCalledWith(text, 'error');

      const row3 = rowOf(c, 'Aka m3');
      const start3 = [...row3.querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));
      await act(async () => { start3.click(); });
      expect(onEditScore).not.toHaveBeenCalled();
      expect(rowOf(c, 'Aka m3').querySelector('[role="alert"]').textContent).toBe(text);
      // A newer tap replaces the older notice.
      expect(c.utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error')).toBeNull();
    } finally { c.restore(); }
  });

  it('f) Back to court cancels the correction, then Start works', async () => {
    const m1 = courtMatch('m1', 'completed', { modifiedAt: 1000 });
    const m2 = courtMatch('m2', 'scheduled');
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const c = await mountCourt([m1, m2], { onEditScore });
    try {
      await tapCorrect(c, 'Aka m1');
      await tapUpNextStart(c);
      expect(onEditScore).not.toHaveBeenCalled();
      await act(async () => { c.utils.getByRole('button', { name: /back to court/i }).click(); });
      expect(heading(c)).not.toContain('Correcting');
      expect(alertsWith(c, 'Save the correction of')).toHaveLength(0);
      await tapUpNextStart(c);
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(onEditScore.mock.calls[0][1]).toBe('m2');
    } finally { c.restore(); }
  });

  it('d) a landed Save correction ends the correction, then Start works; a queued one keeps it open', async () => {
    const m1 = courtMatch('m1', 'completed', { modifiedAt: 1000 });
    const m2 = courtMatch('m2', 'scheduled');
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const c = await mountCourt([m1, m2], { onEditScore });
    try {
      await tapCorrect(c, 'Aka m1');
      expect(c.utils.queryByRole('button', { name: /back to court/i })).toBeTruthy();
      await act(async () => { await probe.props.onSubmit({ status: 'completed' }); });
      expect(c.utils.queryByRole('button', { name: /back to court/i })).toBeNull();
      expect(heading(c)).not.toContain('Correcting');
      await tapUpNextStart(c);
      expect(onEditScore).toHaveBeenCalledTimes(2);
      expect(onEditScore.mock.calls[1][1]).toBe('m2');
    } finally { c.restore(); }

    const queued = vi.fn().mockResolvedValue({ queued: true });
    const c2 = await mountCourt([courtMatch('m1', 'completed', { modifiedAt: 1000 }), courtMatch('m2', 'scheduled')], { onEditScore: queued });
    try {
      await tapCorrect(c2, 'Aka m1');
      await act(async () => { await probe.props.onSubmit({ status: 'completed' }); });
      expect(c2.utils.queryByRole('button', { name: /back to court/i })).toBeTruthy();
      expect(heading(c2)).toContain('Correcting');
    } finally { c2.restore(); }

    // A write that is not a finish (a running patch) never ends it either.
    const c3 = await mountCourt([courtMatch('m1', 'completed', { modifiedAt: 1000 })], { onEditScore: vi.fn().mockResolvedValue({ applied: true }) });
    try {
      await tapCorrect(c3, 'Aka m1');
      await act(async () => { await probe.props.onSubmit({ status: 'running' }); });
      expect(heading(c3)).toContain('Correcting');
    } finally { c3.restore(); }
  });

  it('g) a Save correction answered after another correction was opened leaves that one open', async () => {
    let answer;
    const onEditScore = vi.fn().mockImplementation(() => new Promise((r) => { answer = r; }));
    const c = await mountCourt([
      courtMatch('m1', 'completed', { modifiedAt: 1000 }),
      courtMatch('m3', 'completed', { modifiedAt: 1100 }),
    ], { onEditScore });
    try {
      await tapCorrect(c, 'Aka m1');
      const saveM1 = probe.props.onSubmit;
      let saving;
      await act(async () => { saving = saveM1({ status: 'completed' }); });
      await tapCorrect(c, 'Aka m3');
      expect(c.editorMatch()).toBe('m3');
      await act(async () => { answer({ applied: true }); await saving; });
      expect(c.editorMatch(), "m1's answer does not close m3's correction").toBe('m3');
      expect(heading(c)).toContain('Correcting');
    } finally { c.restore(); }
  });

  it('e) a landed decision on a correction (onClose) ends it; with no correction onClose changes nothing', async () => {
    const m1 = courtMatch('m1', 'completed', { modifiedAt: 1000 });
    const c = await mountCourt([m1, courtMatch('m2', 'scheduled')]);
    try {
      await tapCorrect(c, 'Aka m1');
      expect(heading(c)).toContain('Correcting');
      await act(async () => { probe.props.onClose(); });
      expect(heading(c)).not.toContain('Correcting');
      expect(c.utils.queryByRole('button', { name: /back to court/i })).toBeNull();
    } finally { c.restore(); }

    const c2 = await mountCourt([courtMatch('m1', 'completed', { modifiedAt: 1000 }), courtMatch('m2', 'running')]);
    try {
      expect(c2.editorMatch()).toBe('m2');
      await act(async () => { probe.props.onClose(); });
      expect(c2.editorMatch()).toBe('m2');
    } finally { c2.restore(); }
  });

  it('g) a match started from another screen mid-correction leaves the correction open and is listed beside it', async () => {
    const m1 = courtMatch('m1', 'completed', { modifiedAt: 1000 });
    const m2 = courtMatch('m2', 'scheduled');
    const c = await mountCourt([m1, m2]);
    try {
      await tapCorrect(c, 'Aka m1');
      expect(c.utils.queryByText(/another bout is running/i)).toBeNull();
      c.feed.current = [m1, { ...m2, status: 'running', modifiedAt: 3000 }];
      await c.refresh();
      expect(c.editorMatch()).toBe('m1');
      expect(heading(c)).toContain('Correcting');
      expect(c.showToast).not.toHaveBeenCalled();
      const block = c.utils.container.querySelector('.shiaijo-also-running');
      expect(block, 'the running match is listed above the correction').toBeTruthy();
      expect(block.textContent).toContain('Another bout is running on Shiaijo A');
      expect(block.textContent).toContain('Shiro m2 vs Aka m2');
    } finally { c.restore(); }
  });
});
