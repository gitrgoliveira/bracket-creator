// Driving the real interface.
//
// Every selector here is one a human clicks. They are gathered in this file so
// a UI change breaks one place rather than thirty recipes.
// One owner: lib/api.mjs declares the password the seeder and the UI share.
// Imported AND re-exported, not `export ... from`: that form re-exports
// without binding the name locally, so authAdmin below could not see it.
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import { PASSWORD } from './api.mjs';

export { PASSWORD };

// One tab runs each shiaijo (court_control.jsx, FR9): the court console claims
// its shiaijo from the browser tab it is in, keeping the tab id in
// sessionStorage and the tablet id in localStorage. Every capture context is a
// fresh browser, and closing one sends no pagehide (Playwright's
// context.close() fires none), so a second capture of the same family's
// console would open a shiaijo another device runs and draw it view
// only. The capture is the court's own tablet instead: every signed-in
// context starts as the SAME tab of the SAME tablet, so each console reads as
// the one that runs its shiaijo (a tab that reopens claims what it held), as a
// docs screenshot of the console should show. The keys are read from their one
// source, so a renamed key fails here instead of drifting.
const COURT_CONTROL_SOURCE = fileURLToPath(new URL('../../../web-mobile/js/court_control.jsx', import.meta.url));
const courtKey = (name) => {
  const m = new RegExp(`export const ${name} = '([^']+)';`).exec(fs.readFileSync(COURT_CONTROL_SOURCE, 'utf8'));
  if (!m) throw new Error(`ui: no ${name} found in ${COURT_CONTROL_SOURCE}`);
  return m[1];
};
export const CAPTURE_COURT_TAB = {
  tabKey: courtKey('COURT_TAB_KEY'),
  deviceKey: courtKey('COURT_DEVICE_KEY'),
  // Lowercase UUID v4s, the shape court_control.jsx keeps (it mints a new one
  // over anything else).
  tabId: 'c0a7c0a7-0000-4000-8000-00000000000a',
  deviceId: 'c0a7c0a7-0000-4000-8000-00000000000d',
};

// Every capture context's locale and time zone, fixed so that a date, a time
// field or a 12/24-hour clock the page formats through the browser reads the
// same on every machine rather than following the host's (a time field read
// "10:23 AM" under one). British English, as the docs are written. The
// runner opens its contexts with these, and so does withAdminPage below.
export const CONTEXT_DEFAULTS = { locale: 'en-GB', timezoneId: 'Europe/London' };

// Sign a context in as the operator, without the login form. The SPA renders
// its admin surfaces when these two localStorage keys are present, so setting
// them before the first navigation is all a capture needs. Driving the form
// instead put a login screen in frame 0 of every video and a form round trip
// in front of every admin screenshot, and left the public captures clearing
// keys that a fresh context never had: a public capture is simply one that
// never calls this. The runner calls it for a recipe declaring `auth: 'admin'`;
// a seed calls it on a context of its own, usually through withAdminPage.
// `origin` limits the sign-in to one origin, for a page that frames a second
// origin which must stay signed out (the hero stage, recipes/hero.mjs): an
// init script runs in every frame of the context, not only the top one.
export async function authAdmin(context, { origin } = {}) {
  await context.addInitScript(({ pw, only, court }) => {
    // An init script also runs on the context's initial about:blank document,
    // whose opaque origin has no localStorage: touching it throws a
    // SecurityError. Measured: the runner's pageerror listener is attached
    // after that document exists, so the throw went unseen by timing alone.
    // Skip it there and set the keys on the app's origin, the only one that
    // reads them.
    if (location.protocol === 'about:') return;
    if (only && location.origin !== only) return;
    localStorage.setItem('bc_authed', 'true');
    localStorage.setItem('bc_password', pw);
    // The court's own tablet and tab (CAPTURE_COURT_TAB above).
    localStorage.setItem(court.deviceKey, court.deviceId);
    sessionStorage.setItem(court.tabKey, court.tabId);
  }, { pw: PASSWORD, only: origin || null, court: CAPTURE_COURT_TAB });
}

// Wait until a court console (a page, or a frameLocator holding one) shows
// that it runs its shiaijo: the green bar, not the "Checking who runs" line it
// shows while its claim is out. A capture of the console waits for it, so the
// still never catches the bar between the two.
export async function courtConsoleRuns(scope, { timeout = 15000 } = {}) {
  await scope.locator('[data-testid="court-control"][data-state="holds"]').waitFor({ state: 'visible', timeout });
}

// A signed-in page for a seed to drive, on a context of its own that is closed
// when fn returns or throws. Pass a viewport when the driven surface lays out
// by width; the editors do.
export async function withAdminPage(browser, viewport, fn) {
  const context = await browser.newContext({ ...CONTEXT_DEFAULTS, ...(viewport ? { viewport } : {}) });
  await authAdmin(context);
  try {
    return await fn(await context.newPage());
  } finally {
    await context.close();
  }
}

// Take the focus off whatever the driving left focused, in the page and in
// every frame it holds, so no control draws a focus ring nobody looking at
// the screen would see. The runner calls it before every still; a clip,
// which no still step reaches, calls it itself.
export async function blurEveryFrame(page) {
  for (const frame of page.frames()) {
    await frame.evaluate(() => document.activeElement && document.activeElement.blur());
  }
}

// Press a control inside a frame the page has SCALED with a CSS transform (the
// hero stage's frames). A Playwright pointer click misplaces itself there: it
// reported success while the app never saw it (the stage's console at 0.36),
// or was refused as landing on another element (the phone at 0.85). A click
// event dispatched on the control itself goes through the app's own handler
// with no hit test, as a keyboard press would, and leaves no focus ring. Its
// `detail` is 0, which the app's tap guard never reads as a bounce
// (tap_guard.jsx), so taps sent this way need no pacing. Text goes in with
// `fill`, which needs no hit test either.
export const pressInScaledFrame = (locator) => locator.dispatchEvent('click');

// NOTE: this file deliberately stops at auth and the page-wide helpers above.
// Driving a score editor lives in lib/editor.mjs or the recipe that needs it,
// because the individual, team and
// kachinuki editors differ in how they score, finish and name a fighter, and
// the shared helpers that once lived here were wrong for two of the three: they
// scoped ippons to the individual board's side wrappers, and located the
// correction-reason box as the last text field, which on a kachinuki row is a
// typeable fighter name. They were exported and imported by nothing. The two
// things every editor does share live in lib/editor.mjs: the dialog selector,
// the two-tap Finish, and the individual board's ippon and commit buttons.
// The kachinuki "End match" (editors.mjs) and the realtime-update clip's paced
// double tap (videos.mjs) stay local on purpose: the first arms a different
// button with a different label, and the second happens on camera, where the
// gap between the taps is chosen for the viewer.

export const settle = (page, ms = 350) => page.waitForTimeout(ms);
