// The pace of a deliberate tap.
//
// The app ignores a pointer tap that lands within TAP_BOUNCE_MS (400 ms,
// web-mobile/js/tap_guard.jsx) of one of three things: the layer it just
// opened (a confirm, the sign-in dialog, an edit dialog), the tap that armed a
// two-tap commit (Finish, End match, Save result), and the previous ippon or
// foul tap on the same side of the same bout (acceptTap: the guard is per
// side, so a tap on the other side counts at once). That is how a bouncing
// thumb is told from a second tap, and no person's second tap is ever that
// close. Playwright's is, so a journey that taps into a layer the moment it
// shows, taps an armed button the moment it arms, or strikes twice for one
// side back to back, has the tap swallowed and then fails somewhere later with
// nothing pointing back here.
//
// The dwell below is therefore the operator's own pace, not a wait for the app:
// nothing in the page says the window has passed, so it is timed from the
// moment the journey saw the layer or the arm, or made the tap. TAP_DWELL_MS
// is the screenshot harness's value, kept above the app's window, so both
// harnesses pace alike.
import fs from 'node:fs';
import { fileURLToPath } from 'node:url';
import { TAP_DWELL_MS } from '../../screenshots/lib/editor.mjs';
import { EXPECT_TIMEOUT_MS } from './devices.mjs';

// The app's own window, read from its one source (tap_guard.jsx is JSX, so it
// cannot be imported here). Failing to find it is an error: a harness that
// guessed would drift from the app unnoticed.
export const TAP_BOUNCE_MS = (() => {
  const file = fileURLToPath(new URL('../../../web-mobile/js/tap_guard.jsx', import.meta.url));
  const m = /export const TAP_BOUNCE_MS = (\d+);/.exec(fs.readFileSync(file, 'utf8'));
  if (!m) throw new Error(`pace: no TAP_BOUNCE_MS found in ${file}`);
  return Number(m[1]);
})();
if (TAP_DWELL_MS <= TAP_BOUNCE_MS) {
  throw new Error(`pace: TAP_DWELL_MS (${TAP_DWELL_MS}) must exceed the app's TAP_BOUNCE_MS (${TAP_BOUNCE_MS})`);
}

// Wait out what is left of the dwell since `since` (a Date.now() reading taken
// when the layer or the arm was seen, or the tap made).
export async function dwell(page, since) {
  const left = TAP_DWELL_MS - (Date.now() - since);
  if (left > 0) await page.waitForTimeout(left);
}

// Wait until `locator` is visible, then out the dwell: for a layer or an armed
// button the journey is about to tap into.
export async function settled(locator) {
  await locator.waitFor({ state: 'visible' });
  await dwell(locator.page(), Date.now());
}

// The first tap of a two-tap commit (Finish, End match, Save result): tap
// `button`, wait for `armed` (the same control in its armed state) to show, and
// out the dwell, so the second tap, which the caller makes on the returned
// `armed`, is a deliberate one. The one owner of that arm-settle-tap sequence.
export async function armTwoTap(button, armed) {
  await button.tap();
  await settled(armed);
  return armed;
}

// Settle before a read that no single state names. A journey that taps the
// control NEXT to the intended one (or taps twice) cannot say what it will see:
// the outcome varies by design and is what the audit row records, so there is
// no state to wait for. Two real frames later the page has drawn whatever the
// tap changed locally (a mark, a panel, a dialog, a list), which is what the
// read that follows is of. A change that needs the server's answer is not
// covered; wait for that answer (scoring.mjs nextMatchWrite) or use
// untilStable.
export function rendered(page) {
  return page.evaluate(() => new Promise((done) => {
    requestAnimationFrame(() => requestAnimationFrame(() => done()));
  }));
}

// Settle before a read that follows the server's data: poll `read()` until two
// reads `quietMs` apart agree, then return that value. For a screen that
// refetches after an action (a reload, Back and Forward, a write's echo) whose
// end state the journey cannot name, such as a probe of what the app does. The
// answer is the last read, so the caller need not read again.
export async function untilStable(read, { quietMs = 400, timeout = EXPECT_TIMEOUT_MS } = {}) {
  const deadline = Date.now() + timeout;
  let last = JSON.stringify(await read());
  for (;;) {
    await new Promise((done) => setTimeout(done, quietMs));
    const now = JSON.stringify(await read());
    if (now === last) return JSON.parse(now === undefined ? 'null' : now);
    if (Date.now() > deadline) throw new Error(`untilStable: the read kept changing for ${timeout} ms (last: ${now})`);
    last = now;
  }
}

// page -> Map(side key -> when the last tap on that side finished).
const sideTaps = new WeakMap();

// Tap `locator`, a scoring button on one side of a bout, after the dwell since
// the previous tap this page made on the same `side` (any key naming one side
// of one bout: 'shiro', 'bout 2 aka'). Taps on other sides are not held up.
// The tap's time is taken once it returns, which is no earlier than the app's
// own reading of it.
export async function tapOnSide(page, side, locator) {
  let taps = sideTaps.get(page);
  if (!taps) {
    taps = new Map();
    sideTaps.set(page, taps);
  }
  if (taps.has(side)) await dwell(page, taps.get(side));
  await locator.tap();
  taps.set(side, Date.now());
}
