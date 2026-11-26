// The court console's shiaijo control (FR9): one tab runs each shiaijo, and
// the court's standing belongs to the tablet that tab is on. The console shows
// it in a bar under its top bar (admin_shiaijo_control.jsx, data-testid
// "court-control", whose data-state is court_control.jsx's state: holds,
// other, free-displaced, or unknown while no claim has been answered). A
// console that does not run its shiaijo is view only for every write.
//
// What that means for a journey:
//   - Pages of ONE browser context share localStorage, so they are one tablet
//     (court_control.jsx keeps the tablet id there) and each page is a tab of
//     it. A journey that means two tablets opens them in two contexts.
//   - The first console to claim a free shiaijo runs it; a second one, in
//     another tab or on another tablet, watches until it taps Take control.
//     openShiaijo (fixtures/shiaijo.mjs) waits for the claim's answer and, by
//     default, takes the shiaijo when another console still holds it.
//   - Closing a tab gives the shiaijo back on `pagehide`. Playwright's
//     context.close() fires none (measured: the next tablet then read
//     "run from another device"), so a context closed at a test's end would
//     keep every shiaijo it ran. releaseCourtControl below leaves the page the
//     way a closing tab does, so the page sends its own release, and
//     fixtures/test.mjs calls it for every page before its context or the
//     page itself closes, so the next test, or the next context, opens a free
//     shiaijo.
import { expect } from '@playwright/test';
import { settled as layerSettled } from './pace.mjs';

export const COURT_CONTROL = '[data-testid="court-control"]';
export const TAKE_CONTROL_ASK = '[data-testid="take-control-ask"]';

export const courtControlBar = (page) => page.locator(COURT_CONTROL);

// The bar's state, or null when no console is on screen.
export async function controlState(page) {
  const bar = courtControlBar(page);
  if (!(await bar.count())) return null;
  return bar.getAttribute('data-state');
}

// Wait until the console's claim is answered (the bar leaves `unknown`), and
// return the state it settled on.
export async function awaitControl(page) {
  const bar = courtControlBar(page);
  await expect(bar, 'the court console shows who runs its shiaijo').toBeVisible();
  await expect(bar, 'the court console\'s claim is answered').not.toHaveAttribute('data-state', 'unknown');
  return bar.getAttribute('data-state');
}

// The grey bar a second tab of the tablet that runs the shiaijo shows.
export const SAME_TABLET_TEXT = (court) => new RegExp(`^Shiaijo ${court} is run from another tab on this tablet\\b.*\\.$`);
// The grey bar a console on another tablet shows.
export const OTHER_DEVICE_TEXT = (court) => new RegExp(`^Shiaijo ${court} is run from another device\\b`);
// The green bar of the tab that runs it.
export const HOLDS_TEXT = (court) => `This tablet runs shiaijo ${court}`;

// The bar's text, one line.
export const controlText = async (page) => (await courtControlBar(page).locator('.shiaijo-control__text').innerText()).replace(/\s+/g, ' ').trim();

// Tap Take control, answer its one question when it asks one (another tab or
// tablet stops running the shiaijo; a free shiaijo this tablet lost earlier is
// taken without one), and return once this tab runs it. Returns the question
// it asked, or null.
export async function takeControl(page) {
  const bar = courtControlBar(page);
  await bar.getByRole('button', { name: 'Take control' }).tap();
  const ask = page.locator(TAKE_CONTROL_ASK);
  let question = null;
  // The question opens at once when there is one; a free shiaijo goes straight to the take.
  const asked = await ask.waitFor({ state: 'visible', timeout: 2_000 }).then(() => true, () => false);
  if (asked) {
    // The layer takes the bounce of the tap that opened it (useOpenedTapGuard),
    // so the answer waits out the dwell, as a deliberate second tap does.
    await layerSettled(ask);
    question = (await ask.locator('.shiaijo-move-confirm__body').innerText()).trim();
    await ask.getByRole('button', { name: 'Take control' }).tap();
    await expect(ask).toHaveCount(0);
  }
  await expect(bar, 'this tab runs the shiaijo after Take control').toHaveAttribute('data-state', 'holds');
  return question;
}

// How long the server is asked, after the page left, whether the release
// landed. The release leaves at once, but as a `keepalive` request made while
// the page unloads, which Playwright does not report (measured: neither the
// page's nor the context's request events see it), so the server is asked.
const RELEASE_LANDED_MS = 5_000;
const RELEASE_POLL_MS = 100;

// Who runs `court`, as the server answers it (GET /api/courts/:court/control).
async function lookCourt(page, origin, court, password) {
  const res = await page.request.get(`${origin}/api/courts/${encodeURIComponent(court)}/control`, {
    headers: { 'X-Tournament-Password': password || '' },
  });
  return res.ok() ? res.json() : null;
}

// Give back the shiaijo a page's console runs, as closing the tab would, and
// wait until the server has it. Safe on any page: one with no console that
// holds sends nothing. Only for a page that is about to close: it leaves the
// app for about:blank, so its own pagehide handlers run as a closing tab's do
// (the release, and the outbox's base lease let go) and nothing is left on it
// to claim the shiaijo again (a console still on screen would: the release
// announces a free shiaijo, and a free shiaijo is claimed at once). Answers
// whether the shiaijo was given back; false when the page kept it, as it does
// while its tablet has results queued for that shiaijo (they need the
// control).
export async function releaseCourtControl(page) {
  if (!page || page.isClosed()) return false;
  const holds = await page.locator(`${COURT_CONTROL}[data-state="holds"]`).count().catch(() => 0);
  if (!holds) return false;
  const url = new URL(page.url());
  const court = decodeURIComponent((/^\/admin\/shiaijo\/([^/]+)/.exec(url.pathname) || [])[1] || '');
  if (!court) return false;
  const password = await page.evaluate(() => { try { return localStorage.getItem('bc_password'); } catch { return null; } }).catch(() => null);
  const before = await lookCourt(page, url.origin, court, password).catch(() => null);
  const key = before && before.holderKey;
  const left = await page.goto('about:blank').then(() => true, () => false);
  if (!left || !key) return false;
  const until = Date.now() + RELEASE_LANDED_MS;
  while (Date.now() < until) {
    const now = await lookCourt(page, url.origin, court, password).catch(() => null);
    if (now && now.holderKey !== key) return true;
    await new Promise((done) => { setTimeout(done, RELEASE_POLL_MS); });
  }
  return false;
}

// Every page of a context, before the context closes.
export async function releaseContextCourts(context) {
  for (const page of context.pages()) await releaseCourtControl(page);
}

// Patch a context so that closing it, or closing one of its pages, first gives
// back the shiaijo its consoles run. `page.close({ pagehide: false })` closes a
// page as a crash would, leaving the shiaijo held.
export function releaseOnClose(context) {
  if (context.__courtReleaseOnClose) return context;
  context.__courtReleaseOnClose = true;
  const patchPage = (page) => {
    if (page.__courtReleaseOnClose) return;
    page.__courtReleaseOnClose = true;
    const close = page.close.bind(page);
    page.close = async (options = {}) => {
      const { pagehide = true, ...rest } = options || {};
      if (pagehide) await releaseCourtControl(page).catch(() => false);
      return close(rest);
    };
  };
  for (const p of context.pages()) patchPage(p);
  context.on('page', patchPage);
  const closeContext = context.close.bind(context);
  context.close = async (...args) => {
    await releaseContextCourts(context).catch(() => {});
    return closeContext(...args);
  };
  return context;
}
