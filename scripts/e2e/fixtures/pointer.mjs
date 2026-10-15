// The operator's coarse pointer: reading it, putting it back, and measuring a
// tap target under it.
//
// The operator's iPad matches (pointer: coarse), and the tap-floor rules in
// styles.css key on it, so a journey judged at the wrong pointer measures the
// wrong targets.
import { expect } from '@playwright/test';

// Does this page match (pointer: coarse)?
export const isCoarse = (page) => page.evaluate(() => matchMedia('(pointer: coarse)').matches);

// page -> the CDP session that holds its touch emulation.
const touchSessions = new WeakMap();

// Turn CDP touch emulation on for `page`, in place, and fail loudly if the
// page still does not match (pointer: coarse). Measured: it takes effect at
// once, on the current document (about:blank before the first navigation
// included), and holds across later navigations, so no reload is needed. The
// session is kept for the page's life: detaching it clears the emulation it
// set (measured), so a detach would undo the restore.
export async function restoreCoarse(page) {
  let cdp = touchSessions.get(page);
  if (!cdp) {
    cdp = await page.context().newCDPSession(page);
    touchSessions.set(page, cdp);
  }
  await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 });
  if (!(await isCoarse(page))) {
    throw new Error('page is not (pointer: coarse) even with CDP touch emulation');
  }
}

// Is the page still (pointer: coarse)? If an interruption or a fullPage
// screenshot lost it, every tap-floor rule in styles.css is off for the rest of
// the test, so put it back in place (restoreCoarse, no reload, so the bout is
// not interrupted a second time) and say whether it had been lost, so an audit
// row can name the pointer a measurement was made at. The one owner of "if not
// coarse, restore": the page fixture, the second devices, the screenshot
// camera and the kachinuki interruptions all ask it.
export async function ensureCoarse(page) {
  if (await isCoarse(page)) return { coarseLost: false };
  await restoreCoarse(page);
  return { coarseLost: true, restoredBy: 'CDP Emulation.setTouchEmulationEnabled (in place)' };
}

// A control's rendered size, for the 44px coarse-pointer floor. The page must
// still match (pointer: coarse) at the moment of measuring, or the 44px floor
// rules are off and the size reads low, so that is asserted first.
export async function tapSize(locator) {
  expect(await isCoarse(locator.page()), 'tap targets are measured under (pointer: coarse)').toBe(true);
  const box = await locator.boundingBox();
  if (!box) return null;
  return { w: Math.round(box.width), h: Math.round(box.height), meets44: box.width >= 44 && box.height >= 44 };
}
