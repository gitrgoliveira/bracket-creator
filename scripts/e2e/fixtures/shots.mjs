// Per-step screenshots: <journey folder>/<nn>-<step>.png, numbered in the
// order they were taken so the folder reads as the walkthrough. They are for
// a human to judge (and for the PR body), never compared by a test.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { isCoarse, restoreCoarse } from './pointer.mjs';

// scripts/e2e, the directory playwright.config.mjs sits in.
const E2E_DIR = path.resolve(fileURLToPath(new URL('..', import.meta.url)));

// Where every journey's folder lives. With E2E_RESULTS_DIR set (a second run
// at the same time, see README.md) it is inside that run's own results folder,
// resolved the way Playwright resolves outputDir (relative to the config's
// directory), so two runs never empty each other's folders; Playwright empties
// that folder once, when the run starts, before any test writes. Unset, it is
// output/.
const JOURNEYS_ROOT = process.env.E2E_RESULTS_DIR
  ? path.resolve(E2E_DIR, process.env.E2E_RESULTS_DIR, 'journeys')
  : path.join(E2E_DIR, 'output');

// The one owner of a journey's output folder: its screenshots and its
// audit.json (fixtures/audit.mjs). `journey` may name a subfolder
// ('knockout-mixed-individual/J1').
// A name that is empty, absolute or climbs out of the root is refused: this
// path is what journeyShots empties.
export function journeyDir(journey) {
  const dir = path.resolve(JOURNEYS_ROOT, String(journey));
  const inside = path.relative(JOURNEYS_ROOT, dir);
  if (!journey || path.isAbsolute(journey) || !inside || inside.startsWith('..') || path.isAbsolute(inside)) {
    throw new Error(`journeyDir: "${journey}" is not a folder inside ${JOURNEYS_ROOT}`);
  }
  return dir;
}

// Empty the journey's folder and return its camera. Call once per spec file
// (in beforeAll), so a folder never mixes two runs.
export function journeyShots(journey) {
  const dir = journeyDir(journey);
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  let n = 0;
  return async (page, step, { fullPage = false } = {}) => {
    n += 1;
    const file = path.join(dir, `${String(n).padStart(2, '0')}-${step}.png`);
    const coarse = fullPage && await isCoarse(page);
    await page.screenshot({ path: file, fullPage, animations: 'disabled', caret: 'hide' });
    // A fullPage shot resizes the viewport and, restoring it, drops the touch
    // emulation: measured, (pointer: coarse) reads false afterwards, so every
    // tap-floor rule switches off for the rest of the test. Put it back in
    // place (fixtures/pointer.mjs).
    if (coarse) await restoreCoarse(page);
    return file;
  };
}
