// The pace of a deliberate tap.
//
// The app ignores a pointer tap that lands within TAP_BOUNCE_MS (400 ms,
// web-mobile/js/tap_guard.jsx) of one of two things: the layer it just opened
// (a confirm, the sign-in dialog, an edit dialog), and the tap that armed a
// two-tap commit (Finish, End match, Save result). That is how a bouncing
// thumb is told from a second tap, and no person's second tap is ever that
// close. Playwright's is, so a journey that taps into a layer the moment it
// shows, or taps an armed button the moment it arms, has the tap swallowed and
// then fails somewhere later with nothing pointing back here.
//
// The dwell below is therefore the operator's own pace, not a wait for the app:
// nothing in the page says the window has passed, so it is timed from the
// moment the journey saw the layer or the arm. TAP_DWELL_MS is the screenshot
// harness's value, kept above the app's window, so both harnesses pace alike.
import { TAP_DWELL_MS } from '../../screenshots/lib/editor.mjs';

export { TAP_DWELL_MS };

// Wait out what is left of the dwell since `since` (a Date.now() reading taken
// when the layer or the arm was seen).
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
