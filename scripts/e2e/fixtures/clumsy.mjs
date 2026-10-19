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
// call, and judgement columns are formed by eye, never asserted. They throw
// only when the variant itself could not be performed, so the row would not
// have tested what it says: tapNeighbour finds no neighbour in reach,
// doubleTap's taps land too far apart, retapWhileSaving never sees "Saving…",
// and a helper that needs a dialog or a button finds none.
//
// Taps go through page.touchscreen at the element's centre: the browser's own
// hit-testing decides what is tapped, as it does for a thumb. Every project in
// playwright.config.mjs has a touchscreen.
import { errors } from '@playwright/test';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';
import { dwell, TAP_BOUNCE_MS } from './pace.mjs';

// The longest gap that still counts as a double tap: the app's bounce window
// less a margin for the time between the first tap's start and the click the
// app times from.
const DOUBLE_TAP_MARGIN_MS = 100;
export const DOUBLE_TAP_LIMIT_MS = TAP_BOUNCE_MS - DOUBLE_TAP_MARGIN_MS;

// The longest doubleTap waits in the page for a tap's click. A click trails
// its touch by many tasks on a loaded machine, so the wait ends on what the
// page saw (the clicks, a control disabled) and this bound only stops a tap
// that never makes one from waiting for the test's timeout.
const SETTLE_MS = 2000;

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
// impatient thumb. Returns { gapMs, clicks, withinWindow }, gapMs being the
// time IN THE PAGE between the two taps (Date.now() in capturing listeners,
// the clock the app's guard reads): between the two clicks they produce, or,
// when the first tap disabled the control so the second produces no click (a
// browser dispatches none on a disabled button; the bounce then never reaches
// the app at all), between the two touchstarts. Never the host-side round trip
// of the first tap, which also counts the protocol's own latency, and the
// taps are sent so that none sits between them. clicks is how
// many clicks the page saw (2, or 1 for a control the first tap disabled), and
// withinWindow always true: a machine too slow to land the taps inside the
// window THROWS, because the row would not have tested what it says, and so do
// taps the page did not see as two touches. The first tap's effects cannot be
// taken back, so the taps are not retried; the caller's test fails with the
// message above (playwright.config.mjs sets retries to 0). Callers may assert
// on the outcome unconditionally.
//
// Both taps must reach the same place (stage 6 review 8, m11): two clicks
// count only when each landed on what was at the point as its touch began, and a lone click
// only when the control under the point was DISABLED after the first touch
// (watched from that touch on, since the first click, and so the disable it
// causes, can come after the second touch has ended). A
// control the first tap REPLACED takes no click when its replacement lands
// between the second touch's start and end, so the bounce never reached the
// place-keyed guard (acceptTap) the row claims to test: that throws. A row
// whose first tap replaces the control in place (Add or Remove representative
// bout, Record bout, Remove this bout, Reopen match) passes { afterRender:
// true }: the second tap is sent once the first tap's render has run (as a
// thumb's second tap, some 100 ms later, lands), so it reaches the replacement.
export async function doubleTap(locator, { afterRender = false } = {}) {
  const page = locator.page();
  const { x, y } = await centreOf(locator);
  // The page records each click (with whether its control holds the point)
  // and each touch, and whether the control under the point was disabled at
  // any moment from the first touch on (the first tap may disable it, which is
  // why the second touch makes no click). That is watched, not sampled: a
  // click can trail both touches by many tasks on a loaded machine, so the
  // disable it causes can come after any fixed read (stage 6 review 11, D).
  // The listeners and the observer are removed in the finally below whatever
  // the page saw, so none outlives this call.
  await page.evaluate(({ px, py, settleMs }) => {
    window.__clumsyClicks = [];
    window.__clumsyTouches = [];
    window.__clumsyMovedTo = null;
    const touchTargets = [];
    window.__clumsySecondDisabled = null;
    const controlAt = () => {
      const el = document.elementFromPoint(px, py);
      return el && el.closest('button, [role="button"], input, select, textarea');
    };
    const isDisabled = (control) => !!control && (control.disabled === true || control.getAttribute('aria-disabled') === 'true');
    const disabledAt = () => isDisabled(controlAt());
    // The control (or element) a node belongs to.
    const placeOf = (node) => (node && node.closest ? (node.closest('button, [role="button"], input, select, textarea') || node) : null);
    // What was at the point as each touch began: a click must land there, the
    // place the thumb touched (a control the first tap replaced included: the
    // second touch then begins on its replacement).
    const touched = [];
    const atTouch = (el, i) => {
      const t = touched[i];
      return !!el && !!t && (el === t || el.contains(t) || t.contains(el));
    };
    // Or what holds the point as the click comes (a render between the touch
    // and its click put a replacement there).
    const holdsPoint = (el) => {
      if (!el || !el.isConnected) return false;
      const r = el.getBoundingClientRect();
      return px >= r.left && px <= r.right && py >= r.top && py <= r.bottom;
    };
    // Sticky from the first touch on: the control the first touch began on
    // (or whatever holds the point) became disabled, or that control left
    // the document (the first tap replaced it). Each is a fact once seen,
    // whenever the click that caused it came.
    let watched = null;
    let replaced = false;
    const wakers = new Set();
    const wake = () => { for (const w of [...wakers]) w(); };
    // The control the second touch met was disabled: the one the first
    // touch pressed, the one at the point, or the browser's own target for
    // the second touch, which can differ from the point's (it adjusts a
    // touch towards a nearby control: a first tap that moved the pressed
    // control away, as a toggle whose label grows does, leaves the second
    // touch on a neighbour the point itself is not on; stage 6 review 11).
    let secondTarget = null;
    const noteDisabled = () => {
      if (!window.__clumsySecondDisabled && (isDisabled(watched) || disabledAt() || isDisabled(secondTarget))) {
        window.__clumsySecondDisabled = true;
      }
    };
    const observer = new MutationObserver(() => {
      if (!watched) return;
      if (!watched.isConnected) replaced = true;
      noteDisabled();
      wake();
    });
    const listeners = [];
    for (const [type, seen] of [['click', window.__clumsyClicks], ['touchstart', window.__clumsyTouches]]) {
      const rec = (e) => {
        if (type === 'click') {
          const el = placeOf(e.target);
          seen.push({ at: Date.now(), atPoint: atTouch(el, seen.length) || holdsPoint(el),
            desc: el ? `${el.tagName} "${(el.textContent || '').trim().slice(0, 40)}"` : 'none' });
        } else {
          const place = placeOf(document.elementFromPoint(px, py));
          touched.push(place);
          // The browser's own target for the touch, which can differ from
          // the point's (it adjusts a touch towards a nearby control): the
          // second touch reaching ANOTHER control means the first tap moved
          // the one it pressed away from the point.
          const target = placeOf(e.target);
          if (seen.length === 1) {
            secondTarget = target;
            const first = touchTargets[0];
            window.__clumsyMovedTo = target && first && target !== first && !first.contains(target) && !target.contains(first) && first.isConnected
              ? `${target.tagName.toLowerCase()}${target.disabled ? ' (disabled)' : ''} "${(target.textContent || '').trim().slice(0, 30)}"` : null;
          }
          touchTargets.push(target);
          seen.push(Date.now());
          if (seen.length === 1) {
            watched = place;
            observer.observe(document.documentElement, {
              subtree: true, childList: true, attributes: true, attributeFilter: ['disabled', 'aria-disabled'],
            });
          }
          if (seen.length === 2) noteDisabled();
        }
        wake();
      };
      document.addEventListener(type, rec, true);
      listeners.push([type, rec]);
    }
    // What the page saw of the taps, in order, for a throw to say: each
    // input event with its target, whether the page cancelled it (read as it
    // leaves the page), and where the tapped control then stood.
    const t0 = Date.now();
    const short = (el) => (el && el.tagName ? `${el.tagName.toLowerCase()}${el.disabled ? '[disabled]' : ''}"${(el.textContent || '').trim().slice(0, 16)}"` : '-');
    window.__clumsyTrace = [];
    for (const type of ['touchstart', 'touchend', 'touchcancel', 'pointerdown', 'pointerup', 'pointercancel', 'mousedown', 'mouseup', 'click']) {
      const rec = (e) => {
        if (window.__clumsyTrace.length >= 40) return;
        const atPt = placeOf(document.elementFromPoint(px, py));
        window.__clumsyTrace.push(`${Date.now() - t0}ms ${type}->${short(placeOf(e.target))}${e.defaultPrevented ? '(prevented)' : ''} pt:${short(atPt)}`);
      };
      window.addEventListener(type, rec, false);
      listeners.push([type, rec, window]);
    }
    const painted = () => new Promise((r) => requestAnimationFrame(() => setTimeout(r, 0)));
    // Waits, at most settleMs, until `done()` holds; re-checked on every
    // touch, click and observed change, and on a short poll for the bound.
    const until = (done) => new Promise((resolve) => {
      const deadline = Date.now() + settleMs;
      let timer = null;
      const check = () => {
        const ok = done();
        if (!ok && Date.now() < deadline) return;
        wakers.delete(check);
        clearTimeout(timer);
        resolve(ok);
      };
      const poll = () => { check(); if (wakers.has(check)) timer = setTimeout(poll, 10); };
      wakers.add(check);
      poll();
    });
    // Resolves once the first tap's click has run and the render it caused
    // has painted. A first tap that makes no click within the bound rejects,
    // so the helper fails with its sentence rather than waiting for the
    // test's timeout (stage 6 review 9, F8). Nothing waits between the click
    // and the paint, which would widen the gap the second tap is timed by.
    window.__clumsyAfterFirst = () => until(() => window.__clumsyClicks.length > 0)
      .then((clicked) => (clicked ? painted() : Promise.reject(new Error('no click'))));
    // Resolves once the taps' clicks have all come: two clicks, or one click
    // on a control that became disabled (then a frame, for a second click
    // still on its way), or at the bound. A lone click on a REPLACED control
    // does not end the wait early: the second click can still trail onto the
    // replacement, and a lone click there is a throw anyway.
    window.__clumsySettled = () => until(() => window.__clumsyTouches.length >= 2 && (window.__clumsyClicks.length >= 2
      || (window.__clumsyClicks.length === 1 && window.__clumsySecondDisabled === true)))
      .then(() => { noteDisabled(); return window.__clumsyClicks.length < 2 ? painted() : null; })
      .then(() => ({ replaced, moved: !!watched && watched.isConnected && !holdsPoint(watched) }));
    window.__clumsyRemove = () => {
      for (const [type, rec, target] of listeners) (target || document).removeEventListener(type, rec, !target);
      observer.disconnect();
      wakers.clear();
      window.__clumsyRemove = null;
      window.__clumsyAfterFirst = null;
      window.__clumsySettled = null;
    };
  }, { px: x, py: y, settleMs: SETTLE_MS });
  let seen;
  try {
    // The four touch events go back to back on ONE CDP session, so the second
    // tap is queued in the page right behind the first rather than after a
    // host round trip (two awaited touchscreen.tap calls measured over 500 ms
    // in the page on a loaded machine). One session delivers them in order.
    // With afterRender, the second pair follows the first tap's render, asked
    // of the page on the same session.
    const cdp = await page.context().newCDPSession(page);
    try {
      const tap = () => [
        cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] }),
        cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] }),
      ];
      if (afterRender) {
        await Promise.all(tap());
        const first = await cdp.send('Runtime.evaluate', { expression: 'window.__clumsyAfterFirst && window.__clumsyAfterFirst()', awaitPromise: true });
        if (first && first.exceptionDetails) {
          throw new Error('clumsy.doubleTap: the first tap produced no click in the page, so the row did not test the bounce guard');
        }
        await Promise.all(tap());
      } else {
        await Promise.all([...tap(), ...tap()]);
      }
    } finally {
      await cdp.detach().catch(() => {});
    }
    // Every click the taps make has come (or the bound has passed), judged in
    // the page, never by a host-side wait: the clicks can trail the touches.
    seen = await page.evaluate(async () => {
      const { replaced, moved } = await window.__clumsySettled();
      return { clicks: window.__clumsyClicks, touches: window.__clumsyTouches, secondDisabled: window.__clumsySecondDisabled, replaced, moved, movedTo: window.__clumsyMovedTo, trace: window.__clumsyTrace };
    });
  } finally {
    await page.evaluate(() => { if (typeof window.__clumsyRemove === 'function') window.__clumsyRemove(); }).catch(() => {});
  }
  const { clicks, touches, secondDisabled, replaced, moved, movedTo, trace } = seen;
  const saw = ` (the page saw: ${(trace || []).join(' | ')})`;
  if (!Array.isArray(touches) || touches.length !== 2) {
    throw new Error(`clumsy.doubleTap: the page saw ${Array.isArray(touches) ? touches.length : 'no'} touch(es), not 2, so the row did not test the bounce guard${saw}`);
  }
  if (!Array.isArray(clicks) || clicks.length === 0 || clicks.length > 2) {
    throw new Error(`clumsy.doubleTap: the two taps produced ${Array.isArray(clicks) ? clicks.length : 'no'} click(s) in the page, so the row did not test the bounce guard${saw}`);
  }
  // Two clicks: each on what was at the tapped point as its touch began, or
  // holds it as the click comes, so the bounce reached the place the first
  // tap did (its control, or the one that replaced it there).
  if (clicks.length === 2 && !clicks.every((c) => c.atPoint)) {
    throw new Error(`clumsy.doubleTap: a click of the two taps did not land at the tapped point (${x},${y}: ${clicks.map((c) => c.desc).join(' | ')}), so the taps did not reach the same place and the row did not test the bounce guard${saw}`);
  }
  // One click is a double tap only when the second touch landed on a
  // disabled control (the first tap disabled it, or moved it and left a
  // disabled one there); a control replaced, or a tap lost, is not.
  if (clicks.length === 1 && secondDisabled !== true) {
    throw new Error(`clumsy.doubleTap: the two taps produced one click while the control the second touch met was never disabled (${replaced ? 'the first tap replaced it' : moved || movedTo ? `the first tap moved it off the point${movedTo ? `, so the second touch landed on ${movedTo}` : ''}` : 'a tap lost'}), so the bounce never reached it and the row did not test the bounce guard; a row whose first tap replaces the control in place passes { afterRender: true }${saw}`);
  }
  const gapMs = clicks.length === 2 ? clicks[1].at - clicks[0].at : touches[1] - touches[0];
  if (gapMs >= DOUBLE_TAP_LIMIT_MS) {
    throw new Error(`clumsy.doubleTap: the two taps were ${gapMs} ms apart (limit ${DOUBLE_TAP_LIMIT_MS} ms, the app's bounce window is ${TAP_BOUNCE_MS} ms): this machine was too slow, so the row did not test the bounce guard${saw}`);
  }
  return { gapMs, clicks: clicks.length, withinWindow: true, secondDisabled: clicks.length === 1, afterRender };
}

// V2 (second form). Tap `locator`, then tap the same spot again while the
// write the first tap caused is still unanswered. A local server answers in
// milliseconds, far too fast for any thumb, so the write is held for
// `holdWritesMs` first (every non-GET /api request), which is what a venue's
// wifi does anyway. "While saving" is whichever comes first: the button reads
// "Saving…" (a form that waits for its answer, such as a Finish), or the
// write is out and held (a write that goes through the outbox, which never
// blocks the screen, so a button such as Record bout or Save lineup shows no
// "Saving…" at all; bc-evts). Returns { savingSeen, writeOut, retapped,
// labelAfter, writesSent }: `savingSeen` says the label was read, `writeOut`
// that a write was in flight at the retap, and `retapped` is always true. A
// tap that neither shows "Saving…" nor sends a write within the hold THROWS
// (the second tap was not made, so a check built on this helper would pass
// without testing anything). Any other failure propagates as it is.
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
  // Every write the two taps cause is counted and waited for: a second write
  // from the bounced tap is held on its own clock and can land after the first
  // one's answer has cleared "Saving…", so the caller reads the page only once
  // none is in flight, whatever it then finds.
  const inFlight = new Set();
  let writesSent = 0;
  const started = (req) => {
    if (req.method() === 'GET' || !new URL(req.url()).pathname.startsWith('/api/')) return;
    inFlight.add(req);
    writesSent += 1;
  };
  const ended = (req) => inFlight.delete(req);
  page.on('request', started);
  page.on('requestfinished', ended);
  page.on('requestfailed', ended);
  let writeOutSeen;
  const writeOut = new Promise((resolve) => { writeOutSeen = resolve; });
  const onWriteOut = (req) => {
    if (req.method() !== 'GET' && new URL(req.url()).pathname.startsWith('/api/')) writeOutSeen('write');
  };
  page.on('request', onWriteOut);
  try {
    await page.touchscreen.tap(x, y);
    let first;
    try {
      const label = page.waitForFunction(
        ([el, src, flags]) => new RegExp(src, flags).test(el.textContent || ''),
        [handle, savingText.source, savingText.flags],
        { timeout: holdWritesMs, polling: 'raf' },
      ).then(() => 'label');
      first = await Promise.race([label, writeOut]);
      label.catch(() => {});
    } catch (err) {
      if (err instanceof errors.TimeoutError) {
        throw new Error(`clumsy.retapWhileSaving: ${locator} never read ${savingText} and sent no write within ${holdWritesMs} ms of the tap (writes were held for that long), so there was no second tap to make`);
      }
      throw err;
    }
    const savingSeen = first === 'label'
      || await handle.evaluate((el, [src, flags]) => new RegExp(src, flags).test(el.textContent || ''), [savingText.source, savingText.flags]).catch(() => false);
    const writeOutAtRetap = inFlight.size > 0;
    await page.touchscreen.tap(x, y);
    // Let the held write go through before the route is removed.
    await page.waitForFunction(
      ([el, src, flags]) => !el.isConnected || !new RegExp(src, flags).test(el.textContent || ''),
      [handle, savingText.source, savingText.flags],
      { timeout: holdWritesMs + EXPECT_TIMEOUT_MS },
    );
    const drainBy = Date.now() + holdWritesMs + EXPECT_TIMEOUT_MS;
    while (inFlight.size > 0) {
      if (Date.now() > drainBy) throw new Error(`clumsy.retapWhileSaving: ${inFlight.size} write(s) still unanswered after the retap`);
      await new Promise((r) => setTimeout(r, 50));
    }
    const labelAfter = await handle.evaluate((el) => (el.isConnected ? el.textContent.trim() : null));
    return { savingSeen, writeOut: writeOutAtRetap, retapped: true, labelAfter, writesSent };
  } finally {
    page.off('request', onWriteOut);
    page.off('request', started);
    page.off('requestfinished', ended);
    page.off('requestfailed', ended);
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
    await handle.waitForElementState('hidden', { timeout: EXPECT_TIMEOUT_MS });
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

