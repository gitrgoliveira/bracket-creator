// bc-sseq FR9: the court console's mark under the top bar in every state, its
// Take control and the one question it asks, the disabled action areas, and a
// console handler that finds the control gone when it acts.
import { act, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { setupShiaijoConsole, mountCourt, courtMatch } from '../helpers/shiaijo_console.js';
import { courtControlBar, lastActivePhrase, refusedNotHeld } from '../../admin_shiaijo_control.jsx';

setupShiaijoConsole();

const answer = (over = {}) => ({
  court: 'A', holderKey: '', device: '', youHold: false, youWereDisplaced: false, sameDevice: false,
  since: 0, lastActiveAt: 0, ...over,
});
const HOLDS = answer({ holderKey: 'aaaa', device: 'ipad', youHold: true, sameDevice: true, since: 5 });
const twoMinAgo = () => Date.now() - 2 * 60_000 - 5_000;

let reply;
let calls;
const realFetch = global.fetch;
beforeEach(() => {
  calls = [];
  reply = () => ({ body: HOLDS });
  global.fetch = vi.fn(async (url, init = {}) => {
    const body = init.body ? JSON.parse(init.body) : null;
    calls.push({ url: String(url), method: init.method || 'GET', body });
    const r = reply(String(url), init, body);
    if (r instanceof Error) throw r;
    return { ok: (r.status || 200) < 400, status: r.status || 200, json: async () => r.body };
  });
});
afterEach(() => { global.fetch = realFetch; });

// The first claim waits for the tab id check (court_control.jsx TAB_CHECK_MS).
const settle = (ms = 0) => act(async () => { await new Promise((r) => setTimeout(r, ms)); });
const bar = (c) => c.utils.getByTestId('court-control');
const queueScope = (c) => c.utils.container.querySelector('fieldset.shiaijo-control-scope--queue');
const takes = () => calls.filter((x) => x.method === 'PUT' && x.body && x.body.take === true);
const mount = async () => {
  const c = await mountCourt([courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')]);
  await settle(200);
  return c;
};

describe('the mark under the top bar', () => {
  it('holds: a green bar saying this tablet runs the shiaijo, and the queue can act', async () => {
    const c = await mount();
    try {
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A');
      expect(bar(c).className).toContain('shiaijo-control--holds');
      expect(bar(c).querySelector('.shiaijo-control__dot')).toBeTruthy();
      expect(within(bar(c)).queryByRole('button', { name: 'Take control' })).toBeNull();
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('another device: a grey bar naming its kind and when it was last active, the queue view only, and Take control asks once', async () => {
    reply = (url, init, body) => (body && body.take ? { body: HOLDS } : { body: answer({ holderKey: 'bbbb', device: 'ipad', lastActiveAt: twoMinAgo() }) });
    const c = await mount();
    try {
      expect(bar(c).textContent).toContain('Shiaijo A is run from another device (an iPad, last active 2 min ago)');
      expect(bar(c).className).toContain('shiaijo-control--other');
      expect(queueScope(c).disabled).toBe(true);

      await act(async () => { within(bar(c)).getByRole('button', { name: 'Take control' }).click(); });
      const ask = c.utils.getByTestId('take-control-ask');
      expect(ask.textContent).toContain('The other device will stop running shiaijo A. Take control?');
      expect(takes()).toHaveLength(0);

      await act(async () => { within(ask).getByRole('button', { name: 'Take control' }).click(); });
      await settle();
      expect(takes()).toHaveLength(1);
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A');
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('Cancel takes nothing', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'windows' }) });
    const c = await mount();
    try {
      expect(bar(c).textContent).toContain('Shiaijo A is run from another device (a Windows PC)');
      await act(async () => { within(bar(c)).getByRole('button', { name: 'Take control' }).click(); });
      await act(async () => { within(c.utils.getByTestId('take-control-ask')).getByRole('button', { name: 'Cancel' }).click(); });
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(takes()).toHaveLength(0);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('another tab on this tablet: a grey bar saying the tablet keeps the court\'s standing, and its own question', async () => {
    reply = () => ({ body: answer({ holderKey: 'cccc', device: 'ipad', sameDevice: true, lastActiveAt: twoMinAgo() }) });
    const c = await mount();
    try {
      expect(bar(c).textContent).toContain(
        'Shiaijo A is run from another tab on this tablet (last active 2 min ago). Results this tablet has queued still count as the court\'s.');
      expect(bar(c).className).toContain('shiaijo-control--other');
      await act(async () => { within(bar(c)).getByRole('button', { name: 'Take control' }).click(); });
      expect(c.utils.getByTestId('take-control-ask').textContent)
        .toContain('The other tab on this tablet will stop running shiaijo A. Take control?');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('taken by another device: an amber bar, with Take control', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad', youWereDisplaced: true }) });
    const c = await mount();
    try {
      expect(bar(c).textContent).toContain(
        'Shiaijo A is now run from another device. Results this tablet still has queued count like any other device\'s.');
      expect(bar(c).className).toContain('shiaijo-control--warn');
      expect(within(bar(c)).getByRole('button', { name: 'Take control' })).toBeTruthy();
      expect(queueScope(c).disabled).toBe(true);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('free but lost earlier: an amber bar, and Take control takes it with no question', async () => {
    reply = (url, init, body) => (body && body.take ? { body: HOLDS } : { body: answer({ youWereDisplaced: true }) });
    const c = await mount();
    try {
      expect(bar(c).textContent).toContain('Nobody runs shiaijo A now. This tablet lost the control earlier; tap Take control to run it.');
      expect(bar(c).className).toContain('shiaijo-control--warn');
      expect(queueScope(c).disabled).toBe(true);
      await act(async () => { within(bar(c)).getByRole('button', { name: 'Take control' }).click(); });
      await settle();
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(takes()).toHaveLength(1);
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('no connection: "Checking" at first, then the amber not-connected line, and the console still acts', async () => {
    reply = () => new TypeError('Failed to fetch');
    // The last test's console gives the shiaijo back first (its release runs
    // on a 0 ms timer), so this one opens with nothing known.
    await settle(10);
    const c = await mount();
    try {
      expect(bar(c).textContent).toBe('Checking who runs shiaijo A…');
      await act(async () => { await new Promise((r) => setTimeout(r, 1600)); });
      expect(bar(c).textContent).toBe('Not connected: this tablet runs shiaijo A once it connects, unless another device already does');
      expect(bar(c).className).toContain('shiaijo-control--warn');
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });
});

describe('a console handler checks the control when it acts', () => {
  it('a Send back confirm left open when another device took the shiaijo sends nothing and says why', async () => {
    const running = courtMatch('m1', 'running');
    const c = await mountCourt([running, courtMatch('m2', 'scheduled')]);
    await settle(200);
    try {
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A');
      await act(async () => { c.utils.getByRole('button', { name: 'Send back to queue' }).click(); });
      const dialog = c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
      expect(dialog).toBeTruthy();
      // The next claim says another device took it (a reconnect claims again).
      reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad', youWereDisplaced: true }) });
      await act(async () => { window.dispatchEvent(new Event('online')); });
      await settle();
      expect(bar(c).className).toContain('shiaijo-control--warn');
      await act(async () => { within(dialog).getByRole('button', { name: 'Send back to queue' }).click(); });
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]')).toBeNull();
      expect(c.showToast).toHaveBeenCalledWith(expect.stringContaining('this tablet does not run this shiaijo'), 'error');
      // The Send back button itself is greyed out now.
      expect(c.utils.getByRole('button', { name: 'Send back to queue' }).disabled).toBe(true);
    } finally { c.restore(); c.utils.unmount(); }
  });
});

describe('the words and the check', () => {
  it('words the last activity', () => {
    const now = 100_000_000;
    expect(lastActivePhrase(0, now)).toBe('');
    expect(lastActivePhrase(now - 20_000, now)).toBe('last active just now');
    expect(lastActivePhrase(now - 5 * 60_000, now)).toBe('last active 5 min ago');
    expect(lastActivePhrase(now - 3 * 3_600_000, now)).toBe('last active 3 h ago');
  });

  it('names no device for one the server reads as other', () => {
    const view = { state: 'other', holderDevice: 'other', holderLastActiveAt: 0, sameDevice: false, youWereDisplaced: false };
    expect(courtControlBar(view, 'B').text).toBe('Shiaijo B is run from another device');
  });

  it('refusedNotHeld refuses only a tab that may not write, quietly when asked', () => {
    const tell = vi.fn();
    expect(refusedNotHeld(null)).toBe(false);
    expect(refusedNotHeld({ current: { canWrite: true, tell } })).toBe(false);
    expect(refusedNotHeld({ current: { canWrite: false, tell } }, { quiet: true })).toBe(true);
    expect(tell).not.toHaveBeenCalled();
    expect(refusedNotHeld({ current: { canWrite: false, tell } })).toBe(true);
    expect(tell).toHaveBeenCalledTimes(1);
  });
});
