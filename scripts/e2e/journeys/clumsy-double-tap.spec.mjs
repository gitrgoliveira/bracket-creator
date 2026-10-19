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

  // Stage 6 review 12, m3: a second tap whose click never came while the
  // control was ENABLED is a lost tap, even if the control is disabled later
  // (here 250 ms on, as an answer from the server would): the bounce never
  // reached the guard.
  test('a control disabled only after the second click would have come is not a double tap', async ({ page: p }) => {
    await p.setContent(page(`{ const b = document.getElementById('b'); let ends = 0;
      b.addEventListener('touchend', (e) => { ends++; if (ends === 2) e.preventDefault(); }, { passive: false });
      b.addEventListener('click', () => { setTimeout(() => { b.disabled = true; }, 250); }); }`));
    await expect(doubleTap(p.locator('#b'))).rejects.toThrow(/never disabled/);
  });

  // Stage 6 review 12, m4: a disabled NEIGHBOUR under the point (the first
  // tap moved the control away) is not the guarded control: the bounce
  // never reached it.
  test('a disabled neighbour the second touch lands on is not the control reached', async ({ page: p }) => {
    await p.setContent(page(`{ const b = document.getElementById('b');
      const n = document.createElement('button'); n.id = 'n'; n.disabled = true; n.textContent = 'H';
      n.style.cssText = 'position:absolute;left:40px;top:40px;width:240px;height:96px;display:none';
      document.body.appendChild(n);
      b.addEventListener('click', () => { b.style.top = '300px'; n.style.display = 'block'; }); }`));
    await expect(doubleTap(p.locator('#b'))).rejects.toThrow(/never disabled|did not land/);
  });

  // Without afterRender, on a slowed page (the CPU throttled, as on a loaded
  // machine, where a click trails both touches by many tasks): a control
  // its first click disables is one click, never a throw, and a control
  // that stays enabled is still two (stage 6 review 11, D).
  for (const [name, disables, clicks] of [['a control its first click disables is one click', true, 1], ['a control that stays enabled is two clicks', false, 2]]) {
    test(`slowed: ${name}`, async ({ page: p }) => {
      const cdp = await p.context().newCDPSession(p);
      try {
        for (let i = 0; i < 5; i++) {
          await cdp.send('Emulation.setCPUThrottlingRate', { rate: 1 });
          // In a block: setContent keeps the window, so a second top-level
          // `const b` would fail to declare and the script would not run.
          await p.setContent(page(`window.n = 0; { const b = document.getElementById('b'); b.addEventListener('click', () => { window.n++; ${disables ? 'queueMicrotask(() => { b.disabled = true; });' : ''} }); }`));
          await cdp.send('Emulation.setCPUThrottlingRate', { rate: 10 });
          const r = await doubleTap(p.locator('#b'));
          expect(r.clicks).toBe(clicks);
          expect(await p.evaluate(() => window.n)).toBe(clicks);
        }
      } finally {
        await cdp.send('Emulation.setCPUThrottlingRate', { rate: 1 }).catch(() => {});
        await cdp.detach().catch(() => {});
      }
    });
  }
});
