// bc-sseq FR9: the court console's mark under the top bar in every state, its
// Take control and the one question it asks, the disabled action areas, and a
// console handler that finds the control gone when it acts.
import { act, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { setupShiaijoConsole, mountCourt, courtMatch, probe } from '../helpers/shiaijo_console.js';
import { courtControlBar, lastActivePhrase, refusedNotHeld } from '../../admin_shiaijo_control.jsx';
import { CLAIM_DEADLINE_MS } from '../../court_control.jsx';

setupShiaijoConsole();

const answer = (over = {}) => ({
  court: 'A', holderKey: '', device: '', youHold: false, youWereDisplaced: false, tabWasDisplaced: false,
  sameDevice: false, since: 0, lastActiveAt: 0, ...over,
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
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    await settle(200);
    try {
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A.');
      expect(bar(c).className).toContain('shiaijo-control--holds');
      expect(bar(c).querySelector('.shiaijo-control__dot')).toBeTruthy();
      expect(within(bar(c)).queryByRole('button', { name: 'Take control' })).toBeNull();
      expect(queueScope(c).disabled).toBe(false);
      expect(probe.props.readOnly).toBe(false);
      expect(c.utils.container.querySelector('fieldset.shiaijo-control-scope--editor')).toBeNull();
    } finally { c.restore(); c.utils.unmount(); }
  });

  // The bar sticks under the top bar over the inline team sheet, whose pinned header and
  // scroll margin read its height (styles.css, team_editor_pinned_bars.render.test.jsx).
  // jsdom lays nothing out, so the height is stubbed.
  it('publishes its height as --court-control-h while mounted and takes it back when the console goes', async () => {
    const realRect = Element.prototype.getBoundingClientRect;
    Element.prototype.getBoundingClientRect = function rect() {
      const h = this.classList && this.classList.contains('shiaijo-control') ? 46 : 0;
      return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: h, width: 0, height: h, toJSON() {} };
    };
    const root = document.documentElement;
    let c;
    try {
      c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
      await settle(200);
      expect(root.style.getPropertyValue('--court-control-h')).toBe('46px');
      c.utils.unmount();
      expect(root.style.getPropertyValue('--court-control-h')).toBe('');
    } finally {
      Element.prototype.getBoundingClientRect = realRect;
      root.style.removeProperty('--court-control-h');
      if (c) c.restore();
    }
  });

  it('another device: a grey bar naming its kind and when it was last active, the queue view only, and Take control asks once', async () => {
    reply = (url, init, body) => (body && body.take ? { body: HOLDS } : { body: answer({ holderKey: 'bbbb', device: 'ipad', lastActiveAt: twoMinAgo() }) });
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    await settle(200);
    try {
      expect(bar(c).textContent).toContain('Shiaijo A is run from another device (an iPad, last active 2 min ago).');
      expect(bar(c).className).toContain('shiaijo-control--other');
      expect(queueScope(c).disabled).toBe(true);
      expect(probe.props.readOnly).toBe(true);

      await act(async () => { within(bar(c)).getByRole('button', { name: 'Take control' }).click(); });
      const ask = c.utils.getByTestId('take-control-ask');
      expect(ask.textContent).toContain('The other device will stop running shiaijo A. Take control?');
      expect(takes()).toHaveLength(0);

      await act(async () => { within(ask).getByRole('button', { name: 'Take control' }).click(); });
      await settle();
      expect(takes()).toHaveLength(1);
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A.');
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('Cancel takes nothing', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'windows' }) });
    const c = await mount();
    try {
      expect(bar(c).textContent).toContain('Shiaijo A is run from another device (a Windows PC).');
      await act(async () => { within(bar(c)).getByRole('button', { name: 'Take control' }).click(); });
      await act(async () => { within(c.utils.getByTestId('take-control-ask')).getByRole('button', { name: 'Cancel' }).click(); });
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(takes()).toHaveLength(0);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('another tab on this tablet: a grey bar, and its own question', async () => {
    reply = () => ({ body: answer({ holderKey: 'cccc', device: 'ipad', sameDevice: true, lastActiveAt: twoMinAgo() }) });
    const c = await mount();
    try {
      expect(bar(c).querySelector('.shiaijo-control__text').textContent)
        .toBe("Shiaijo A is run from another tab on this tablet (last active 2 min ago). Results this tablet has queued still count as the court's.");
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
      expect(bar(c).querySelector('.shiaijo-control__text').textContent)
        .toBe('Shiaijo A is now run from another device.');
      expect(bar(c).textContent).not.toContain('count like');
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
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A.');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('free after another tab on this tablet lost it: says another tab ran it earlier, and Take control takes it with no question', async () => {
    reply = (url, init, body) => (body && body.take ? { body: HOLDS } : { body: answer({ tabWasDisplaced: true }) });
    const c = await mount();
    try {
      expect(bar(c).textContent).toContain('Nobody runs shiaijo A now. Another tab on this tablet ran it earlier; tap Take control to run it here.');
      expect(bar(c).className).toContain('shiaijo-control--warn');
      expect(bar(c).getAttribute('data-state')).toBe('free-displaced');
      expect(queueScope(c).disabled).toBe(true);
      await act(async () => { within(bar(c)).getByRole('button', { name: 'Take control' }).click(); });
      await settle();
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(takes()).toHaveLength(1);
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A.');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('401 refusal: says sign-in no longer works and to sign in again, while still allowing writes', async () => {
    reply = () => ({ status: 401, body: { error: 'Unauthorized' } });
    sessionStorage.clear();
    await settle(10);
    const c = await mount();
    try {
      expect(bar(c).textContent).toBe('Your sign-in no longer works for shiaijo A. Sign in again.');
      expect(bar(c).className).toContain('shiaijo-control--warn');
      expect(bar(c).getAttribute('data-state')).toBe('unknown');
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('500 refusal: says it could not check who runs the shiaijo and to reload', async () => {
    reply = () => ({ status: 500, body: { error: 'boom' } });
    sessionStorage.clear();
    await settle(10);
    const c = await mount();
    try {
      expect(bar(c).textContent).toBe('Could not check who runs shiaijo A: this console cannot tell. Reload the page.');
      expect(bar(c).className).toContain('shiaijo-control--warn');
      expect(bar(c).getAttribute('data-state')).toBe('unknown');
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('404: says the shiaijo is no longer in the tournament, offers no Take control, and still allows writes', async () => {
    reply = () => ({ status: 404, body: { error: 'not found' } });
    await settle(10);
    const c = await mount();
    try {
      expect(bar(c).textContent).toBe('Shiaijo A is no longer in this tournament. Go back to the schedule and pick one that is.');
      expect(bar(c).textContent).not.toContain('Checking who runs');
      expect(bar(c).className).toContain('shiaijo-control--warn');
      expect(bar(c).getAttribute('data-state')).toBe('not-listed');
      expect(within(bar(c)).queryByRole('button', { name: 'Take control' })).toBeNull();
      expect(queueScope(c).disabled).toBe(false);
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
      expect(bar(c).textContent).toBe('Not connected: this tablet runs shiaijo A once it connects, unless another device already does.');
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
      expect(bar(c).textContent).toBe('This tablet runs shiaijo A.');
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

  it('a hanging Take control ends after CLAIM_DEADLINE_MS so Cancel works again', async () => {
    vi.useFakeTimers();
    let c = null;
    try {
      reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad' }) });
      global.fetch = vi.fn((url, init = {}) => {
        const body = init.body ? JSON.parse(init.body) : null;
        calls.push({ url: String(url), method: init.method || 'GET', body });
        if (body && body.take) return new Promise(() => {});
        const r = reply(String(url), init, body);
        return Promise.resolve({ ok: (r.status || 200) < 400, status: r.status || 200, json: async () => r.body });
      });

      c = await mountCourt([courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')]);
      await act(async () => { await vi.advanceTimersByTimeAsync(200); });
      expect(bar(c).getAttribute('data-state')).toBe('other');

      await act(async () => { within(bar(c)).getByRole('button', { name: 'Take control' }).click(); });
      const ask = c.utils.getByTestId('take-control-ask');
      await act(async () => { within(ask).getByRole('button', { name: 'Take control' }).click(); });
      expect(within(ask).getByRole('button', { name: 'Cancel' }).disabled).toBe(true);
      expect(ask.textContent).toContain('Taking control…');

      await act(async () => { await vi.advanceTimersByTimeAsync(CLAIM_DEADLINE_MS); });
      expect(c.showToast).toHaveBeenCalledWith(expect.stringContaining('No answer in time'), 'error');
      expect(within(ask).getByRole('button', { name: 'Cancel' }).disabled).toBe(false);
      expect(within(ask).getByRole('button', { name: 'Take control' })).toBeTruthy();

      await act(async () => { within(ask).getByRole('button', { name: 'Cancel' }).click(); });
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
    } finally {
      if (c) { c.restore(); c.utils.unmount(); }
      await act(async () => { await vi.advanceTimersByTimeAsync(10); });
      vi.useRealTimers();
      await settle(10);
    }
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
    const view = { state: 'other', holderDevice: 'other', holderLastActiveAt: 0, sameDevice: false, youWereDisplaced: false, tabWasDisplaced: false };
    expect(courtControlBar(view, 'B').text).toBe('Shiaijo B is run from another device.');
  });

  it('tabWasDisplaced alone does not raise the tablet-displacement warning when held by another device', () => {
    const view = { state: 'other', holderDevice: 'ipad', holderLastActiveAt: 0, sameDevice: false, youWereDisplaced: false, tabWasDisplaced: true };
    const barView = courtControlBar(view, 'A');
    expect(barView.tone).toBe('other');
    expect(barView.text).toBe('Shiaijo A is run from another device (an iPad).');
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
