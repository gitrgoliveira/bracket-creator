// The double-tap helper itself (fixtures/clumsy.mjs), on a page of its own,
// no server needed. With afterRender the second tap waits for the first tap's
// render; a first tap that makes no click must fail at once with the
// helper's sentence, never wait for the test's timeout (stage 6 review 9, F8).
import { test, expect } from '@playwright/test';
import { doubleTap } from '../fixtures/clumsy.mjs';

const page = (script = '') => `<!doctype html><meta name="viewport" content="width=device-width">
<button id="b" style="position:absolute;left:40px;top:40px;width:240px;height:96px">Add</button>
<script>${script}</script>`;

test.describe('clumsy.doubleTap', () => {
  test.setTimeout(30_000);

  test('a first tap that makes no click fails at once, with the helper\'s sentence', async ({ page: p }) => {
    // The control swallows its touches: no click is made.
    await p.setContent(page("document.getElementById('b').addEventListener('touchstart', (e) => e.preventDefault(), { passive: false });"));
    const started = Date.now();
    await expect(doubleTap(p.locator('#b'), { afterRender: true })).rejects.toThrow(/the first tap produced no click/);
    expect(Date.now() - started).toBeLessThan(10_000);
  });

  test('two taps on a control that stays are two clicks', async ({ page: p }) => {
    await p.setContent(page("window.n = 0; document.getElementById('b').addEventListener('click', () => { window.n++; });"));
    const r = await doubleTap(p.locator('#b'), { afterRender: true });
    expect(r.clicks).toBe(2);
    expect(await p.evaluate(() => window.n)).toBe(2);
  });
});
