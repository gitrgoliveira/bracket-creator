// Per-step screenshots: output/<journey>/<nn>-<step>.png, numbered in the
// order they were taken so the folder reads as the walkthrough. They are for
// a human to judge (and for the PR body), never compared by a test.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const OUTPUT = path.resolve(fileURLToPath(new URL('../output', import.meta.url)));

// page -> the CDP session that re-applies touch emulation after a fullPage shot.
const touchSessions = new WeakMap();

// Empty the journey's folder and return its camera. Call once per spec file
// (in beforeAll), so a folder never mixes two runs.
export function journeyShots(journey) {
  const dir = path.join(OUTPUT, journey);
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  let n = 0;
  return async (page, step, { fullPage = false } = {}) => {
    n += 1;
    const file = path.join(dir, `${String(n).padStart(2, '0')}-${step}.png`);
    const coarse = fullPage && await page.evaluate(() => matchMedia('(pointer: coarse)').matches);
    await page.screenshot({ path: file, fullPage, animations: 'disabled', caret: 'hide' });
    // A fullPage shot resizes the viewport and, restoring it, drops the touch
    // emulation: measured, (pointer: coarse) reads false afterwards, so every
    // tap-floor rule switches off for the rest of the test. Turning touch
    // emulation back on restores it in place, no reload needed (measured).
    // The CDP session is kept for the page's life: detaching it clears the
    // emulation it set (measured), so a detach would undo the restore.
    if (coarse) {
      let cdp = touchSessions.get(page);
      if (!cdp) {
        cdp = await page.context().newCDPSession(page);
        touchSessions.set(page, cdp);
      }
      await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 });
    }
    return file;
  };
}
