// Per-step screenshots: output/<journey>/<nn>-<step>.png, numbered in the
// order they were taken so the folder reads as the walkthrough. They are for
// a human to judge (and for the PR body), never compared by a test.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const OUTPUT = path.resolve(fileURLToPath(new URL('../output', import.meta.url)));

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
    await page.screenshot({ path: file, fullPage, animations: 'disabled', caret: 'hide' });
    return file;
  };
}
