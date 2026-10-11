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
import { TAP_DWELL_MS } from '../../screenshots/lib/editor.mjs';

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
