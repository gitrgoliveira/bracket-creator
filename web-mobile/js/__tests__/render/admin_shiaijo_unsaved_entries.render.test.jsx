import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { setupShiaijoConsole, probe, chrome, mountCourt, courtMatch } from '../helpers/shiaijo_console.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';

setupShiaijoConsole();

// Operator decision R1 (2026-10-17, stage 5 review 8): every console action
// that unmounts the score editor or puts another match in it asks first while
// the editor holds unsaved entries (the editor publishes them through
// `hostUnsavedRef`; the probe stands in for it here). Keep entries leaves
// everything as it was; Discard entries goes on. Send back to queue says it in
// its own confirm. With nothing unsaved nothing asks.
describe('the console asks before it drops the editor\'s unsaved entries (R1)', () => {
  const WARN = /^Your unsaved entries will be discarded\. /;
  const warnings = () => window.confirmDialog.mock.calls.filter(([o]) => WARN.test((o && o.message) || ''));
  const tapCorrect = async (c, rowText) => {
    const row = [...c.utils.container.querySelectorAll('.shiaijo-qrow--complete')].find((r) => r.textContent.includes(rowText));
    const btn = [...row.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Correct');
    await act(async () => { btn.click(); });
  };
  const holdUnsaved = (yes) => { probe.props.hostUnsavedRef.current = () => yes; };
  const heading = (c) => c.utils.container.querySelector('.shiaijo-context__toggle').textContent;
  let prevConfirm;
  beforeEach(() => { prevConfirm = window.confirmDialog; });
  afterEach(() => { window.confirmDialog = prevConfirm; });

  it('Back to court: Keep entries stays in the correction, Discard entries leaves it', async () => {
    const c = await mountCourt([courtMatch('m1', 'completed', { modifiedAt: 1000 }), courtMatch('m2', 'scheduled')]);
    try {
      await tapCorrect(c, 'Aka m1');
      holdUnsaved(true);
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await act(async () => { c.utils.getByRole('button', { name: /back to court/i }).click(); });
      expect(warnings().map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Leave this match anyway?']);
      expect(heading(c)).toContain('Correcting');
      expect(c.editorMatch()).toBe('m1');
      window.confirmDialog = vi.fn().mockResolvedValue(true);
      await act(async () => { c.utils.getByRole('button', { name: /back to court/i }).click(); });
      expect(heading(c)).not.toContain('Correcting');
    } finally { c.restore(); }
  });

  it('Back to court asks nothing with nothing unsaved', async () => {
    const c = await mountCourt([courtMatch('m1', 'completed', { modifiedAt: 1000 }), courtMatch('m2', 'scheduled')]);
    try {
      await tapCorrect(c, 'Aka m1');
      holdUnsaved(false);
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await act(async () => { c.utils.getByRole('button', { name: /back to court/i }).click(); });
      expect(warnings()).toHaveLength(0);
      expect(heading(c)).not.toContain('Correcting');
    } finally { c.restore(); }
  });

  it('Correct on another finished match asks first; Keep entries keeps the editor on its match', async () => {
    const c = await mountCourt([courtMatch('m1', 'completed', { modifiedAt: 1000 }), courtMatch('m3', 'completed', { modifiedAt: 900 })]);
    try {
      await tapCorrect(c, 'Aka m1');
      holdUnsaved(true);
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await tapCorrect(c, 'Aka m3');
      expect(warnings()).toHaveLength(1);
      expect(c.editorMatch()).toBe('m1');
      window.confirmDialog = vi.fn().mockResolvedValue(true);
      await tapCorrect(c, 'Aka m3');
      expect(c.editorMatch()).toBe('m3');
    } finally { c.restore(); }
  });

  it('a pick of another bout asks first; Keep entries sends nothing back', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 }), courtMatch('m2', 'scheduled')]);
    try {
      expect(c.editorMatch()).toBe('m1');
      holdUnsaved(true);
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      const card = c.utils.container.querySelector('.shiaijo-upnext__card');
      const start = [...card.querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));
      await act(async () => { start.click(); });
      expect(warnings()).toHaveLength(1);
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(c.editorMatch()).toBe('m1');
    } finally { c.restore(); }
  });

  it('Send back to queue says the unsaved entries are discarded, and only then', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 })]);
    try {
      holdUnsaved(true);
      await act(async () => { c.utils.getByRole('button', { name: /send back to queue/i }).click(); });
      let dialog = c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
      expect(dialog.textContent).toContain('Your unsaved entries will be discarded.');
      expect(dialog.textContent).not.toContain('Any score entered is kept');
      await act(async () => { [...dialog.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Cancel').click(); });
      holdUnsaved(false);
      await act(async () => { c.utils.getByRole('button', { name: /send back to queue/i }).click(); });
      dialog = c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
      expect(dialog.textContent).not.toContain('Your unsaved entries will be discarded.');
      expect(dialog.textContent).toContain('The score already saved is kept');
    } finally { c.restore(); }
  });

  // Stage 5 review 9, m1: the confirm says the entries are discarded, so the
  // editor drops them at once (its published discard), as Discard entries
  // does everywhere else, never only once the feed shows the match queued.
  it('Send back to queue over unsaved entries tells the editor to drop them, and only then', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 })]);
    const sendBack = async () => {
      await act(async () => { c.utils.getByRole('button', { name: /send back to queue/i }).click(); });
      const dialog = c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
      await act(async () => { dialog.querySelector('.btn--primary').click(); });
    };
    try {
      const discard = vi.fn();
      const probeFn = () => true;
      probeFn.discard = discard;
      probe.props.hostUnsavedRef.current = probeFn;
      await sendBack();
      expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
      expect(discard).toHaveBeenCalledTimes(1);
      const quiet = vi.fn();
      const clean = () => false;
      clean.discard = quiet;
      probe.props.hostUnsavedRef.current = clean;
      await sendBack();
      expect(quiet).not.toHaveBeenCalled();
    } finally { c.restore(); }
  });


  // Stage 5 review 11, minor 3: the confirm's wording is decided when it
  // opens. Entries made while a confirm WITHOUT the warning is open (a scoring
  // key reaching the console once focus fell to the body) were never named in
  // it, so confirming must show the warning and wait for a second answer
  // instead of dropping them silently.
  it('Send back to queue: entries made while a confirm without the warning is open are asked about, not dropped', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 })]);
    try {
      const discard = vi.fn();
      const probeFn = () => true;
      probeFn.discard = discard;
      const clean = () => false;
      clean.discard = vi.fn();
      probe.props.hostUnsavedRef.current = clean;
      await act(async () => { c.utils.getByRole('button', { name: /send back to queue/i }).click(); });
      let dialog = c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
      expect(dialog.textContent).not.toContain('Your unsaved entries will be discarded.');
      probe.props.hostUnsavedRef.current = probeFn;
      await act(async () => { dialog.querySelector('.btn--primary').click(); });
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(discard).not.toHaveBeenCalled();
      dialog = c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
      expect(dialog.textContent).toContain('Your unsaved entries will be discarded.');
      await act(async () => { dialog.querySelector('.btn--primary').click(); });
      expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
      expect(discard).toHaveBeenCalledTimes(1);
    } finally { c.restore(); }
  });


  // A confirm that turns warned (entries made while it was open) is one
  // confirm: the bounce of the tap that re-rendered it must not send the match
  // or drop the entries, and a later deliberate tap confirms once. Pointer
  // taps (detail 1) are the ones tap_guard reads as bounces; the clock is a
  // spy on Date.now, which tap_guard reads.
  it('Send back to queue: the bounce of the tap that turned the confirm warned does nothing; a later tap confirms once', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 })]);
    let now = Date.now() + 1000;
    const spy = vi.spyOn(Date, 'now').mockImplementation(() => now);
    try {
      const discard = vi.fn();
      const probeFn = () => true; probeFn.discard = discard;
      const clean = () => false; clean.discard = vi.fn();
      probe.props.hostUnsavedRef.current = clean;
      await act(async () => { c.utils.getByRole('button', { name: /send back to queue/i }).click(); });
      probe.props.hostUnsavedRef.current = probeFn;
      // The tap that opened the confirm stamped the clock; the first Confirm is a
      // separate tap, past the bounce window of that one.
      now += TAP_BOUNCE_MS;
      const confirmBtn = () => c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"] .btn--primary');
      await act(async () => { fireEvent.click(confirmBtn(), { detail: 1 }); });
      expect(c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]').textContent).toContain('Your unsaved entries will be discarded.');
      now += 30;
      await act(async () => { fireEvent.click(confirmBtn(), { detail: 1 }); });
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(discard).not.toHaveBeenCalled();
      now += TAP_BOUNCE_MS;
      await act(async () => { fireEvent.click(confirmBtn(), { detail: 1 }); });
      expect(discard).toHaveBeenCalledTimes(1);
      expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
    } finally { spy.mockRestore(); c.restore(); }
  });


  it('the confirm body that gains the unsaved-entries warning is announced (aria-live)', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 })]);
    try {
      const discard = vi.fn();
      const probeFn = () => true; probeFn.discard = discard;
      const clean = () => false; clean.discard = vi.fn();
      probe.props.hostUnsavedRef.current = clean;
      await act(async () => { c.utils.getByRole('button', { name: /send back to queue/i }).click(); });
      const body = () => c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"] .shiaijo-move-confirm__body');
      probe.props.hostUnsavedRef.current = probeFn;
      await act(async () => { c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"] .btn--primary').click(); });
      expect(body().textContent).toContain('Your unsaved entries will be discarded.');
      expect(body().getAttribute('aria-live')).toBe('polite');
    } finally { c.restore(); }
  });

  // Stage 5 review 9, M1: every console exit that drops the editor asks the
  // same question: the court switcher, the Dashboard crumb, Sign out and
  // Public viewer. With nothing unsaved each goes in the same tick.
  it('the court switcher asks first; Keep entries stays on this court', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 })]);
    try {
      const sel = c.utils.container.querySelector('.shiaijo-title-select__native');
      holdUnsaved(true);
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      await act(async () => { fireEvent.change(sel, { target: { value: 'B' } }); });
      expect(warnings().map(([o]) => o.message)).toEqual(['Your unsaved entries will be discarded. Leave this match anyway?']);
      expect(c.exits.onSwitchCourt).not.toHaveBeenCalled();
      expect(sel.value).toBe('A');
      window.confirmDialog = vi.fn().mockResolvedValue(true);
      await act(async () => { fireEvent.change(sel, { target: { value: 'B' } }); });
      expect(c.exits.onSwitchCourt).toHaveBeenCalledWith('B');
      holdUnsaved(false);
      window.confirmDialog = vi.fn().mockResolvedValue(false);
      fireEvent.change(sel, { target: { value: 'B' } });
      expect(c.exits.onSwitchCourt).toHaveBeenCalledTimes(2);
      expect(warnings()).toHaveLength(0);
    } finally { c.restore(); }
  });

  it('the Dashboard crumb, Sign out and Public viewer ask first', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 })]);
    try {
      const exits = [
        ['onBack', () => chrome.crumbs.items[0].onClick()],
        ['onLogout', () => chrome.topbar.onLogout()],
        ['onViewerMode', () => chrome.topbar.onViewerMode()],
      ];
      for (const [name, take] of exits) {
        holdUnsaved(true);
        window.confirmDialog = vi.fn().mockResolvedValue(false);
        await act(async () => { take(); });
        expect(warnings(), name).toHaveLength(1);
        expect(c.exits[name], name).not.toHaveBeenCalled();
        window.confirmDialog = vi.fn().mockResolvedValue(true);
        await act(async () => { take(); });
        expect(c.exits[name], name).toHaveBeenCalledTimes(1);
        holdUnsaved(false);
        window.confirmDialog = vi.fn().mockResolvedValue(false);
        take();
        expect(c.exits[name], name).toHaveBeenCalledTimes(2);
        expect(warnings(), name).toHaveLength(0);
      }
    } finally { c.restore(); }
  });

  // Stage 5 review 9, m3: the guards are judged again once the operator has
  // answered, on the court as it is then, never on the copy from the tap.
  it('a pick over unsaved entries is judged on the court as it is after the ask', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { modifiedAt: 1000 }), courtMatch('m2', 'scheduled')]);
    try {
      holdUnsaved(true);
      let answer;
      window.confirmDialog = vi.fn(() => new Promise((res) => { answer = res; }));
      const card = c.utils.container.querySelector('.shiaijo-upnext__card');
      const start = [...card.querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));
      await act(async () => { start.click(); });
      expect(warnings()).toHaveLength(1);
      // Another device finishes the running bout while the question is open.
      c.feed.current = [courtMatch('m1', 'completed', { modifiedAt: 2000 }), courtMatch('m2', 'scheduled')];
      await c.refresh();
      await act(async () => { answer(true); });
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    } finally { c.restore(); }
  });

  it('a Correct over unsaved entries is refused when a bout started while it asked', async () => {
    const c = await mountCourt([courtMatch('m1', 'completed', { modifiedAt: 1000 }), courtMatch('m3', 'completed', { modifiedAt: 900 }), courtMatch('m2', 'scheduled')]);
    try {
      await tapCorrect(c, 'Aka m1');
      const discard = vi.fn();
      const unsaved = () => true;
      unsaved.discard = discard;
      probe.props.hostUnsavedRef.current = unsaved;
      let answer;
      window.confirmDialog = vi.fn(() => new Promise((res) => { answer = res; }));
      await tapCorrect(c, 'Aka m3');
      expect(warnings()).toHaveLength(1);
      c.feed.current = [courtMatch('m1', 'completed', { modifiedAt: 1000 }), courtMatch('m3', 'completed', { modifiedAt: 900 }), courtMatch('m2', 'running', { modifiedAt: 3000 })];
      await c.refresh();
      await act(async () => { answer(true); });
      expect(c.editorMatch()).not.toBe('m3');
      // Stage 5 review 10, minor 2: Discard entries dropped them, though the
      // Correct was then refused and the editor stayed.
      expect(discard).toHaveBeenCalledTimes(1);
    } finally { c.restore(); }
  });
});
