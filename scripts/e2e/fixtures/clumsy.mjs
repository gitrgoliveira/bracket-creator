// The distracted, clumsy court operator (mp-yqxn.2's PERSONA AUDIT), as
// helpers a journey calls around its intended actions:
//
//   V1 tapNeighbour     the tap lands on the control next to the intended one
//   V2 doubleTap        two taps inside the app's bounce window
//      retapWhileSaving a second tap while the button still reads "Saving…"
//   V3 interrupt        reload / back / tab hidden / offline, then return
//   V4 hastyConfirm     a confirm dialog answered by its loudest button
//      hastyShiaijoConfirm  the same for the shiaijo page's own confirms
//      loudestButton    the ranking both use, for any other panel
//
// Every helper performs a real input on a real control and RETURNS what
// happened (which element was hit, how far apart the taps were, what the
// dialog said), so the journey can record it as an audit row. None of them
// asserts the app's behaviour: whether the outcome is correct is the journey's
// call, and judgement columns are formed by eye, never asserted.
//
// Taps go through page.touchscreen at the element's centre: the browser's own
// hit-testing decides what is tapped, as it does for a thumb. Every project in
// playwright.config.mjs has a touchscreen.
import { errors } from '@playwright/test';
import { dwell, TAP_BOUNCE_MS } from './pace.mjs';

// The longest gap that still counts as a double tap: the app's bounce window
// less a margin for the time between the first tap's start and the click the
// app times from.
const DOUBLE_TAP_MARGIN_MS = 100;
export const DOUBLE_TAP_LIMIT_MS = TAP_BOUNCE_MS - DOUBLE_TAP_MARGIN_MS;

// Controls a thumb can land on.
const INTERACTIVE = 'button, [role="button"], a[href], input:not([type="hidden"]), select, textarea, [role="checkbox"], [role="tab"], [role="switch"]';

// How far a mis-tap reaches: a neighbour further than this is not "the one
// next to it" but somewhere else on the screen. About one fingertip.
const REACH_PX = 96;

async function centreOf(locator) {
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (!box) throw new Error(`clumsy: ${locator} has no box (not rendered)`);
  return { x: box.x + box.width / 2, y: box.y + box.height / 2, box };
}

// V1. Tap the control ADJACENT to `locator` in `direction` ('left' | 'right'
// | 'up' | 'down') instead of it. The neighbour is found by layout, among
// every visible control on the page (not only DOM siblings: the other side's
// ippon button is in another subtree), as the nearest one that sits in that
// direction and overlaps the target's row (left/right) or column (up/down).
// It must be the topmost element at its own centre, so a control covered by
// an overlay is never picked. Throws when nothing is within reach: the
// journey asked for a neighbour that does not exist.
//
// Returns { intended, hit, gapPx }.
export async function tapNeighbour(locator, direction) {
  const page = locator.page();
  await locator.scrollIntoViewIfNeeded();
  const found = await locator.evaluate((src, { direction, sel, reach }) => {
    // Defined in here because it runs in the page. A short description of
    // an element, for the audit row.
    const describe = (el) => {
      const text = (el.getAttribute('aria-label') || el.innerText || el.value || '').trim().replace(/\s+/g, ' ');
      return { tag: el.tagName.toLowerCase(), label: text.slice(0, 80), testid: el.getAttribute('data-testid') || null,
        className: typeof el.className === 'string' ? el.className : null,
        disabled: !!el.disabled || el.getAttribute('aria-disabled') === 'true' };
    };
    const a = src.getBoundingClientRect();
    const horizontal = direction === 'left' || direction === 'right';
    let best = null;
    for (const el of document.querySelectorAll(sel)) {
      if (el === src || el.contains(src) || src.contains(el)) continue;
      const r = el.getBoundingClientRect();
      if (r.width < 1 || r.height < 1) continue;
      if (r.bottom <= 0 || r.right <= 0 || r.top >= innerHeight || r.left >= innerWidth) continue;
      const cx = r.left + r.width / 2;
      const cy = r.top + r.height / 2;
      const top = document.elementFromPoint(cx, cy);
      if (!top || !(top === el || el.contains(top))) continue;
      // Overlap on the perpendicular axis, and strictly beyond the target on
      // the requested one.
      const overlaps = horizontal ? (r.top < a.bottom && r.bottom > a.top) : (r.left < a.right && r.right > a.left);
      if (!overlaps) continue;
      const gap = { right: r.left - a.right, left: a.left - r.right, down: r.top - a.bottom, up: a.top - r.bottom }[direction];
      if (gap < -1 || gap > reach) continue;
      const drift = horizontal ? Math.abs(cy - (a.top + a.height / 2)) : Math.abs(cx - (a.left + a.width / 2));
      if (!best || gap < best.gap || (gap === best.gap && drift < best.drift)) best = { el, gap, drift, cx, cy };
    }
    if (!best) return null;
    return { intended: describe(src), hit: describe(best.el), gapPx: Math.round(best.gap), x: best.cx, y: best.cy };
  }, { direction, sel: INTERACTIVE, reach: REACH_PX });
  if (!found) throw new Error(`clumsy.tapNeighbour: no control within ${REACH_PX}px ${direction} of ${locator}`);
  await page.touchscreen.tap(found.x, found.y);
  const { x, y, ...row } = found;
  return row;
}

// V2. Two taps on the same spot inside the app's bounce window
// (DOUBLE_TAP_LIMIT_MS, derived from tap_guard.jsx's TAP_BOUNCE_MS), the
// impatient thumb. Returns { gapMs, withinWindow }, gapMs being the time from
// the start of the first tap to the start of the second, and withinWindow
// always true: a machine too slow to land the taps inside the window THROWS,
// because the row would not have tested what it says. The first tap's effects
// cannot be taken back, so the taps are not retried; the caller's test fails
// and is run again. Callers may assert on the outcome unconditionally.
export async function doubleTap(locator) {
  const page = locator.page();
  const { x, y } = await centreOf(locator);
  const t0 = Date.now();
  await page.touchscreen.tap(x, y);
  const gapMs = Date.now() - t0;
  await page.touchscreen.tap(x, y);
  if (gapMs >= DOUBLE_TAP_LIMIT_MS) {
    throw new Error(`clumsy.doubleTap: the two taps were ${gapMs} ms apart (limit ${DOUBLE_TAP_LIMIT_MS} ms, the app's bounce window is ${TAP_BOUNCE_MS} ms): this machine was too slow, so the row did not test the bounce guard`);
  }
  return { gapMs, withinWindow: true };
}

// V2 (second form). Tap `locator`, then tap the same spot again while it reads
// "Saving…". A local server answers in milliseconds, far too fast for any
// thumb, so the write is held for `holdWritesMs` first (every non-GET /api
// request), which is what a venue's wifi does anyway. Returns
// { savingSeen, retapped, labelAfter }, both flags always true: a button that
// never shows "Saving…" within the hold THROWS (the second tap was not made, so
// a check built on this helper would pass without testing anything). Any other
// failure propagates as it is.
export async function retapWhileSaving(locator, { holdWritesMs = 1200, savingText = /Saving…/ } = {}) {
  const page = locator.page();
  const { x, y } = await centreOf(locator);
  const handle = await locator.elementHandle();
  const hold = async (route) => {
    if (route.request().method() === 'GET') return route.continue();
    await new Promise((r) => setTimeout(r, holdWritesMs));
    // While the write was held the page may have navigated or closed (a
    // Finish + Start Next re-renders, a journey moves on), which cancels the
    // request: continuing a route whose request is gone throws, and there is
    // nothing left to let through.
    return route.continue().catch(() => {});
  };
  await page.route('**/api/**', hold);
  try {
    await page.touchscreen.tap(x, y);
    try {
      await page.waitForFunction(
        ([el, src, flags]) => new RegExp(src, flags).test(el.textContent || ''),
        [handle, savingText.source, savingText.flags],
        { timeout: holdWritesMs, polling: 'raf' },
      );
    } catch (err) {
      if (err instanceof errors.TimeoutError) {
        throw new Error(`clumsy.retapWhileSaving: ${locator} never read ${savingText} within ${holdWritesMs} ms of the tap (writes were held for that long), so there was no second tap to make`);
      }
      throw err;
    }
    const savingSeen = true;
    await page.touchscreen.tap(x, y);
    // Let the held write go through before the route is removed.
    await page.waitForFunction(
      ([el, src, flags]) => !el.isConnected || !new RegExp(src, flags).test(el.textContent || ''),
      [handle, savingText.source, savingText.flags],
      { timeout: holdWritesMs + 15000 },
    );
    const labelAfter = await handle.evaluate((el) => (el.isConnected ? el.textContent.trim() : null));
    return { savingSeen, retapped: savingSeen, labelAfter };
  } finally {
    await page.unroute('**/api/**', hold);
    await handle.dispose();
  }
}

// V3. Interrupt the operator mid-action, then bring them back.
//
//   reload   the page reloads (the iPad restored a discarded tab)
//   back     the browser Back button, then Forward to return
//   hidden   the tab goes to the background and comes back (a tab switch,
//            the iPad sleeping). Headless Chromium never changes visibility
//            by itself (measured: neither bringToFront on another page nor
//            CDP Page.setWebLifecycleState moves document.visibilityState),
//            so document.hidden and document.visibilityState are overridden
//            on the page and a real `visibilitychange` event is dispatched
//            for each edge. The app's own listeners (admin.jsx, app.jsx)
//            read document.hidden, so they run exactly as on a device; their
//            resume path reconnects the event stream, and `resumed` reports
//            whether that reconnect request was seen.
//   offline  the network drops (context.setOffline), then returns
//
// `during(page)` runs while the operator is away (offline: the taps made
// without a network). Returns a record of what was observed.
export async function interrupt(page, kind, { awayMs = 600, during } = {}) {
  const before = page.url();
  const away = async () => {
    if (during) await during(page);
    // The time away IS the scenario (the operator looked at the shiaijo), not
    // a wait for the app.
    await page.waitForTimeout(awayMs);
  };
  switch (kind) {
    case 'reload': {
      await page.reload();
      return { kind, before, after: page.url() };
    }
    case 'back': {
      // Whether Back went anywhere is read from the URL, not from goBack()'s
      // response: the SPA routes with history.pushState (router.jsx), and a
      // same-document step answers null although it did go back.
      await page.goBack();
      const landedOn = page.url();
      const wentBack = landedOn !== before;
      await away();
      if (wentBack) await page.goForward();
      return { kind, before, landedOn, hadHistory: wentBack, after: page.url() };
    }
    case 'hidden': {
      const setHidden = (hidden) => page.evaluate((h) => {
        if (h) {
          Object.defineProperty(document, 'hidden', { configurable: true, get: () => true });
          Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' });
        } else {
          delete document.hidden;
          delete document.visibilityState;
        }
        document.dispatchEvent(new Event('visibilitychange'));
      }, hidden);
      await setHidden(true);
      await away();
      const reconnect = page.waitForRequest((r) => r.url().includes('/api/events'), { timeout: 5000 })
        .then(() => true, () => false);
      await setHidden(false);
      return { kind, before, after: page.url(), resumed: await reconnect, method: 'document.hidden override + dispatched visibilitychange' };
    }
    case 'offline': {
      const context = page.context();
      await context.setOffline(true);
      try {
        await away();
      } finally {
        await context.setOffline(false);
      }
      return { kind, before, after: page.url() };
    }
    default:
      throw new Error(`clumsy.interrupt: unknown kind "${kind}"`);
  }
}

// V4's ranking: the most prominent visible button inside `scope` (a Locator),
// the one an operator who does not read taps. Prominence is read from the
// rendered classes: danger over primary over a plain button over a ghost one,
// the larger button breaking a tie. Returns { button, label, prominence,
// otherLabels }, the last being the other visible buttons' labels. Throws when
// `scope` shows no button.
const PROMINENCE = ['ghost', 'plain', 'primary', 'danger'];
const prominenceRank = (cls) => (/\bbtn--danger\b/.test(cls) ? 3 : /\bbtn--primary\b/.test(cls) ? 2 : /\bbtn--ghost\b/.test(cls) ? 0 : 1);

export async function loudestButton(scope) {
  const buttons = scope.locator('button');
  let best = null;
  const labels = [];
  for (let i = 0; i < await buttons.count(); i += 1) {
    const b = buttons.nth(i);
    if (!(await b.isVisible())) continue;
    const cls = (await b.getAttribute('class')) || '';
    const box = await b.boundingBox();
    const label = (await b.innerText()).trim();
    labels.push(label);
    const score = [prominenceRank(cls), box ? box.width * box.height : 0];
    if (!best || score[0] > best.score[0] || (score[0] === best.score[0] && score[1] > best.score[1])) {
      best = { b, label, score };
    }
  }
  if (!best) throw new Error(`clumsy.loudestButton: no visible button in ${scope}`);
  return {
    button: best.b,
    label: best.label,
    prominence: PROMINENCE[best.score[0]],
    otherLabels: labels.filter((l) => l !== best.label),
  };
}

// Wait until the dialog `handle` was pinned to has gone (hidden or detached),
// then let the handle go. A locator would re-resolve, so a second dialog the
// answer opens would be waited on instead of the one that was answered.
async function waitAnswered(handle) {
  try {
    await handle.waitForElementState('hidden', { timeout: 15_000 });
  } finally {
    await handle.dispose();
  }
}

// V4. Answer the open confirm dialog (confirmDialog in ui.jsx) the way an
// operator who does not read does: tap its loudest button (loudestButton).
// Returns { label, prominence, otherLabels, title, message }, so the audit row
// can say whether the loud button was the safe one.
export async function hastyConfirm(page) {
  const dialog = page.locator('.modal[role="dialog"]').filter({ has: page.locator('.modal__foot') }).last();
  await dialog.waitFor({ state: 'visible' });
  const shown = Date.now();
  const handle = await dialog.elementHandle();
  const loud = await loudestButton(dialog.locator('.modal__foot'));
  const title = (await dialog.getAttribute('aria-label')) || '';
  const message = ((await dialog.locator('.dialog-msg').allInnerTexts())[0] || '').trim();
  const { x, y } = await centreOf(loud.button);
  // A confirm ignores every tap inside the bounce window of its opening, so
  // "hasty" here is a hurried operator, never a tap before the guard lets one in.
  await dwell(page, shown);
  await page.touchscreen.tap(x, y);
  await waitAnswered(handle);
  const { button, ...row } = loud;
  return { ...row, title, message };
}

// V4 for the shiaijo page's own confirms (`.shiaijo-move-confirm` in
// admin_shiaijo.jsx: Send back to queue, Move to Shiaijo X). They are not
// confirmDialog, so hastyConfirm cannot see them; the same rule answers them.
// Returns the same fields as hastyConfirm.
export async function hastyShiaijoConfirm(page) {
  const dialog = page.locator('.shiaijo-move-confirm[role="dialog"]').last();
  await dialog.waitFor({ state: 'visible' });
  const shown = Date.now();
  const handle = await dialog.elementHandle();
  const loud = await loudestButton(dialog.locator('.shiaijo-move-confirm__actions'));
  const title = ((await dialog.locator('.shiaijo-move-confirm__title').allInnerTexts())[0] || '').trim();
  const message = ((await dialog.locator('.shiaijo-move-confirm__body').allInnerTexts())[0] || '').trim();
  // The confirm ignores a tap inside the bounce window of its opening (fixtures/pace.mjs).
  await dwell(page, shown);
  await loud.button.tap();
  await waitAnswered(handle);
  const { button, ...row } = loud;
  return { ...row, title, message };
}

