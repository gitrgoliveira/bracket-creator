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
// The bar's words: the bold word that opens it, then the sentence (and the
// detail a phone leaves out).
const barText = (c) => bar(c).querySelector('.shiaijo-control__text').textContent;
const glyph = (c) => bar(c).querySelector('.shiaijo-control__glyph').getAttribute('data-icon');
const takeButton = (c) => within(bar(c)).getByRole('button', { name: 'Take control' });
const queueScope = (c) => c.utils.container.querySelector('fieldset.shiaijo-control-scope--queue');
const takes = () => calls.filter((x) => x.method === 'PUT' && x.body && x.body.take === true);
const mount = async () => {
  const c = await mountCourt([courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')]);
  await settle(200);
  return c;
};

describe('the mark under the top bar', () => {
  it('holds: a green bar opening with a check and "Running", saying this device runs the shiaijo, and the queue can act', async () => {
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    await settle(200);
    try {
      expect(barText(c)).toBe('Running This device runs shiaijo A.');
      expect(bar(c).querySelector('.shiaijo-control__lead').textContent).toBe('Running');
      expect(glyph(c)).toBe('circle-check');
      expect(bar(c).className).toContain('shiaijo-control--holds');
      expect(c.utils.queryByTestId('view-only-reason')).toBeNull();
      expect(c.utils.queryByTestId('court-control-lost')).toBeNull();
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

  it('another device: a slate "View only" bar naming its kind and when it was last active, the queue view only, and Take control asks once', async () => {
    reply = (url, init, body) => (body && body.take ? { body: HOLDS } : { body: answer({ holderKey: 'bbbb', device: 'ipad', lastActiveAt: twoMinAgo() }) });
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    await settle(200);
    try {
      expect(barText(c)).toBe('View only An iPad runs shiaijo A. Last active 2 min ago.');
      expect(bar(c).querySelector('.shiaijo-control__more').textContent).toBe(' Last active 2 min ago.');
      expect(glyph(c)).toBe('eye');
      expect(bar(c).className).toContain('shiaijo-control--view');
      // A secondary button here (another device simply runs it), full size.
      expect(takeButton(c).className).not.toContain('btn--primary');
      expect(takeButton(c).className).not.toContain('btn--sm');
      expect(queueScope(c).disabled).toBe(true);
      expect(probe.props.readOnly).toBe(true);

      await act(async () => { takeButton(c).click(); });
      const ask = c.utils.getByTestId('take-control-ask');
      // The title asks; the body states who runs it, the match it has running
      // and what the take does, with no second question.
      expect(ask.querySelector('h3').textContent).toBe('Take control of shiaijo A?');
      expect(ask.querySelector('.shiaijo-move-confirm__body').textContent)
        .toBe('An iPad runs it (last active 2 min ago) and has Cup running. That device becomes view only; results it already saved are kept.');
      expect(ask.textContent).not.toContain('Take control?');
      expect(takes()).toHaveLength(0);

      await act(async () => { within(ask).getByRole('button', { name: 'Take control' }).click(); });
      await settle();
      expect(takes()).toHaveLength(1);
      expect(barText(c)).toBe('Running This device runs shiaijo A.');
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(queueScope(c).disabled).toBe(false);
      // Taking it is not losing it: no loss note.
      expect(c.utils.queryByTestId('court-control-lost')).toBeNull();
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('Cancel takes nothing', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'windows' }) });
    const c = await mount();
    try {
      expect(barText(c)).toBe('View only A Windows PC runs shiaijo A.');
      await act(async () => { takeButton(c).click(); });
      await act(async () => { within(c.utils.getByTestId('take-control-ask')).getByRole('button', { name: 'Cancel' }).click(); });
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(takes()).toHaveLength(0);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('another tab on this device: a slate "View only" bar, and its own statement', async () => {
    reply = () => ({ body: answer({ holderKey: 'cccc', device: 'ipad', sameDevice: true, lastActiveAt: twoMinAgo() }) });
    const c = await mount();
    try {
      expect(barText(c))
        .toBe('View only Another tab on this device runs shiaijo A. Last active 2 min ago. Results queued on this device still count.');
      expect(bar(c).className).toContain('shiaijo-control--view');
      expect(glyph(c)).toBe('eye');
      await act(async () => { takeButton(c).click(); });
      expect(c.utils.getByTestId('take-control-ask').querySelector('.shiaijo-move-confirm__body').textContent)
        .toBe('Another tab on this device runs it (last active 2 min ago). That tab becomes view only; results queued on this device still count.');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('taken by another device: a saturated amber "Control lost" bar, with Take control as the primary button', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad', youWereDisplaced: true }) });
    const c = await mount();
    try {
      expect(barText(c)).toBe('Control lost An iPad took shiaijo A from this device.');
      expect(bar(c).className).toContain('shiaijo-control--lost');
      expect(glyph(c)).toBe('triangle-alert');
      expect(takeButton(c).className).toContain('btn--primary');
      // Opened already lost (a reload): no loss note, the bar says it.
      expect(c.utils.queryByTestId('court-control-lost')).toBeNull();
      expect(queueScope(c).disabled).toBe(true);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('free but lost earlier: a "Control lost" bar, and Take control takes it with no question', async () => {
    reply = (url, init, body) => (body && body.take ? { body: HOLDS } : { body: answer({ youWereDisplaced: true }) });
    const c = await mount();
    try {
      expect(barText(c)).toBe('Control lost Nobody runs shiaijo A now; this device lost it earlier.');
      expect(bar(c).className).toContain('shiaijo-control--lost');
      expect(takeButton(c).className).toContain('btn--primary');
      expect(queueScope(c).disabled).toBe(true);
      expect(c.utils.getAllByTestId('view-only-reason')[0].textContent)
        .toBe('View only: nobody runs shiaijo A. Tap Take control at the top to run it here.');
      await act(async () => { takeButton(c).click(); });
      await settle();
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(takes()).toHaveLength(1);
      expect(barText(c)).toBe('Running This device runs shiaijo A.');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('free after another tab on this device lost it: says another tab ran it last, and Take control takes it with no question', async () => {
    reply = (url, init, body) => (body && body.take ? { body: HOLDS } : { body: answer({ tabWasDisplaced: true }) });
    const c = await mount();
    try {
      expect(barText(c)).toBe('Control lost Nobody runs shiaijo A now; another tab on this device ran it last.');
      expect(bar(c).className).toContain('shiaijo-control--lost');
      expect(bar(c).getAttribute('data-state')).toBe('free-displaced');
      expect(queueScope(c).disabled).toBe(true);
      await act(async () => { takeButton(c).click(); });
      await settle();
      expect(c.utils.queryByTestId('take-control-ask')).toBeNull();
      expect(takes()).toHaveLength(1);
      expect(barText(c)).toBe('Running This device runs shiaijo A.');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('401 refusal: says sign-in no longer works and to sign in again, while still allowing writes', async () => {
    reply = () => ({ status: 401, body: { error: 'Unauthorized' } });
    sessionStorage.clear();
    await settle(10);
    const c = await mount();
    try {
      expect(barText(c)).toBe('Sign in again Your sign-in no longer works for shiaijo A.');
      expect(bar(c).className).toContain('shiaijo-control--error');
      expect(glyph(c)).toBe('alert-circle');
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
      expect(barText(c)).toBe('Check failed Could not check who runs shiaijo A. Reload the page.');
      expect(bar(c).className).toContain('shiaijo-control--error');
      expect(bar(c).getAttribute('data-state')).toBe('unknown');
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('404: says the shiaijo is no longer in the tournament, offers no Take control, and still allows writes', async () => {
    reply = () => ({ status: 404, body: { error: 'not found' } });
    await settle(10);
    const c = await mount();
    try {
      expect(barText(c)).toBe('Not found Shiaijo A is no longer in this tournament. Go back to the schedule and pick one that is.');
      expect(barText(c)).not.toContain('Finding out who runs');
      expect(bar(c).className).toContain('shiaijo-control--error');
      expect(bar(c).getAttribute('data-state')).toBe('not-listed');
      expect(within(bar(c)).queryByRole('button', { name: 'Take control' })).toBeNull();
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('no connection: "Checking" at first, then the neutral "Not connected" line, and the console still acts', async () => {
    reply = () => new TypeError('Failed to fetch');
    // The last test's console gives the shiaijo back first (its release runs
    // on a 0 ms timer), so this one opens with nothing known.
    await settle(10);
    const c = await mount();
    try {
      expect(barText(c)).toBe('Checking Finding out who runs shiaijo A…');
      expect(glyph(c)).toBe('hourglass');
      expect(bar(c).className).toContain('shiaijo-control--pending');
      await act(async () => { await new Promise((r) => setTimeout(r, 1600)); });
      expect(barText(c)).toBe('Not connected Scoring here still works. This device runs shiaijo A once it connects, unless another device already does.');
      expect(glyph(c)).toBe('wifi-off');
      // The same neutral treatment as Checking: an unconnected console still scores.
      expect(bar(c).className).toContain('shiaijo-control--pending');
      expect(queueScope(c).disabled).toBe(false);
    } finally { c.restore(); c.utils.unmount(); }
  });
});

describe('telling the operator what view only means', () => {
  // A take that arrives while the console is open: the next claim (a reconnect
  // claims again) answers that another device or tab runs it now.
  const takenWith = async (next) => {
    reply = () => ({ body: next });
    await act(async () => { window.dispatchEvent(new Event('online')); });
    await settle();
  };

  it('the release grace: names the device that closed it and that this console takes it soon, with Take control', async () => {
    reply = (url, init, body) => (body && body.take ? { body: HOLDS }
      : { body: answer({ holderKey: 'bbbb', device: 'mac', lastActiveAt: Date.now() - 3000, releasedAt: Date.now() - 2000 }) });
    const c = await mount();
    try {
      expect(barText(c)).toBe('View only Shiaijo A was closed on a Mac just now. This console takes it within about 10 seconds unless that Mac comes back.');
      expect(bar(c).className).toContain('shiaijo-control--view');
      expect(queueScope(c).disabled).toBe(true);
      await act(async () => { takeButton(c).click(); });
      expect(c.utils.getByTestId('take-control-ask').querySelector('.shiaijo-move-confirm__body').textContent)
        .toBe('A Mac closed it just now and may come back. Taking it now makes that device view only; results it already saved are kept.');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('a grace with no device word says "another device"', () => {
    const view = { state: 'other', holderDevice: 'device', holderLastActiveAt: 0, releasedAt: 5, sameDevice: false, youWereDisplaced: false };
    expect(courtControlBar(view, 'C').text)
      .toBe('Shiaijo C was closed on another device just now. This console takes it within about 10 seconds unless that device comes back.');
  });

  it('losing the shiaijo to another device while open: a one-time static note, announced, that Dismiss takes away', async () => {
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    await settle(200);
    try {
      expect(barText(c)).toBe('Running This device runs shiaijo A.');
      await takenWith(answer({ holderKey: 'bbbb', device: 'ipad', youWereDisplaced: true }));
      const note = c.utils.getByTestId('court-control-lost');
      expect(note.getAttribute('role')).toBe('alert');
      expect(note.textContent).toContain('Shiaijo A was taken by an iPad. This screen is now view only; entries you had not saved stay on screen.');
      // Inside the sticky bar, so it is on screen wherever the operator works.
      expect(bar(c).contains(note)).toBe(true);
      expect(barText(c)).toBe('Control lost An iPad took shiaijo A from this device.');
      await act(async () => { within(note).getByRole('button', { name: 'Dismiss' }).click(); });
      expect(c.utils.queryByTestId('court-control-lost')).toBeNull();
      // Told once: another answer saying the same does not bring it back.
      await takenWith(answer({ holderKey: 'bbbb', device: 'ipad', youWereDisplaced: true }));
      expect(c.utils.queryByTestId('court-control-lost')).toBeNull();
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('losing it to another tab on this device: the same note, though the bar stays "View only"', async () => {
    const c = await mount();
    try {
      await takenWith(answer({ holderKey: 'cccc', device: 'ipad', sameDevice: true }));
      expect(bar(c).className).toContain('shiaijo-control--view');
      expect(c.utils.getByTestId('court-control-lost').textContent)
        .toContain('Shiaijo A was taken by another tab on this device. This screen is now view only; entries you had not saved stay on screen.');
      // Taking it back clears the note.
      reply = () => ({ body: HOLDS });
      await act(async () => { takeButton(c).click(); });
      await act(async () => { within(c.utils.getByTestId('take-control-ask')).getByRole('button', { name: 'Take control' }).click(); });
      await settle();
      expect(barText(c)).toBe('Running This device runs shiaijo A.');
      expect(c.utils.queryByTestId('court-control-lost')).toBeNull();
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('beside each disabled group, the reason: the queue, the completed rows and the score editor', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad' }) });
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'completed')]);
    await settle(200);
    try {
      const reasons = c.utils.getAllByTestId('view-only-reason');
      expect(reasons.length).toBe(3);
      for (const r of reasons) expect(r.textContent).toBe('View only: an iPad runs shiaijo A.');
      // Each sits just before its group.
      expect(queueScope(c).previousElementSibling.getAttribute('data-testid')).toBe('view-only-reason');
      expect(reasons[0].querySelector('[data-icon="eye"]')).toBeTruthy();
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('the idle panel says view only and who runs the shiaijo, not "Ready when you are"', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'android-tablet' }) });
    const c = await mount();
    try {
      const idle = c.utils.container.querySelector('.shiaijo__placeholder');
      expect(idle.querySelector('h3').textContent).toBe('View only');
      expect(idle.querySelector('p').textContent).toBe('An Android tablet runs shiaijo A. The match it starts shows here.');
      expect(idle.textContent).not.toContain('Ready when you are');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('while it runs the shiaijo the idle panel keeps its own words', async () => {
    const c = await mount();
    try {
      const idle = c.utils.container.querySelector('.shiaijo__placeholder');
      expect(idle.querySelector('h3').textContent).toBe('Ready when you are');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('the confirm names the running match by its place in the pool', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad' }) });
    const c = await mountCourt([courtMatch('m1', 'running', { poolPosition: 1, poolCount: 3 }), courtMatch('m2', 'scheduled')]);
    await settle(200);
    try {
      await act(async () => { takeButton(c).click(); });
      expect(c.utils.getByTestId('take-control-ask').querySelector('.shiaijo-move-confirm__body').textContent)
        .toBe('An iPad runs it and has Cup, Match 1 of 3 running. That device becomes view only; results it already saved are kept.');
    } finally { c.restore(); c.utils.unmount(); }
  });

  it('offline, Take control is greyed out and the bar says it needs the connection', async () => {
    reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad' }) });
    const c = await mount();
    const desc = Object.getOwnPropertyDescriptor(window.navigator, 'onLine');
    try {
      Object.defineProperty(window.navigator, 'onLine', { configurable: true, get: () => false });
      await act(async () => { window.dispatchEvent(new Event('offline')); });
      expect(barText(c)).toBe('View only An iPad runs shiaijo A. Taking control needs a connection.');
      expect(takeButton(c).disabled).toBe(true);
    } finally {
      if (desc) Object.defineProperty(window.navigator, 'onLine', desc);
      else delete window.navigator.onLine;
      c.restore(); c.utils.unmount();
    }
  });
});

describe('a console handler checks the control when it acts', () => {
  it('a Send back confirm left open when another device took the shiaijo sends nothing and says why', async () => {
    const running = courtMatch('m1', 'running');
    const c = await mountCourt([running, courtMatch('m2', 'scheduled')]);
    await settle(200);
    try {
      expect(barText(c)).toBe('Running This device runs shiaijo A.');
      await act(async () => { c.utils.getByRole('button', { name: 'Send back to queue' }).click(); });
      const dialog = c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
      expect(dialog).toBeTruthy();
      // The next claim says another device took it (a reconnect claims again).
      reply = () => ({ body: answer({ holderKey: 'bbbb', device: 'ipad', youWereDisplaced: true }) });
      await act(async () => { window.dispatchEvent(new Event('online')); });
      await settle();
      expect(bar(c).className).toContain('shiaijo-control--lost');
      await act(async () => { within(dialog).getByRole('button', { name: 'Send back to queue' }).click(); });
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]')).toBeNull();
      expect(c.showToast).toHaveBeenCalledWith(expect.stringContaining('this device does not run this shiaijo'), 'error');
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

      await act(async () => { takeButton(c).click(); });
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
    const view = { state: 'other', holderDevice: 'device', holderLastActiveAt: 0, sameDevice: false, youWereDisplaced: false, tabWasDisplaced: false };
    expect(courtControlBar(view, 'B').text).toBe('Another device runs shiaijo B.');
  });

  it('tabWasDisplaced alone does not raise the device-displacement warning when held by another device', () => {
    const view = { state: 'other', holderDevice: 'ipad', holderLastActiveAt: 0, sameDevice: false, youWereDisplaced: false, tabWasDisplaced: true };
    const barView = courtControlBar(view, 'A');
    expect(barView.tone).toBe('view');
    expect(barView.text).toBe('An iPad runs shiaijo A.');
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
