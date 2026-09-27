// The `test` every journey imports: Playwright's own, plus a server per
// worker and an operator page whose pointer is verified coarse.
import { test as base, expect } from '@playwright/test';
import { start } from '../../screenshots/lib/server.mjs';

// Does this page match (pointer: coarse)? The operator's iPad does, and the
// tap-floor rules in styles.css key on it, so a journey judged at the wrong
// pointer measures the wrong targets.
export const isCoarse = (page) => page.evaluate(() => matchMedia('(pointer: coarse)').matches);

export const test = base.extend({
  // One mobile-app server per WORKER, on a free port and a throwaway
  // TOURNAMENT_DATA_DIR, started and stopped by scripts/screenshots'
  // server.mjs (SIGTERM then SIGKILL by PID). Tests in a worker share it, so a
  // spec that seeds once and walks several tests declares
  // test.describe.configure({ mode: 'serial' }).
  server: [async ({}, use) => {
    let server;
    try {
      server = await start('mobile');
    } catch (err) {
      throw new Error(`${err.message}\n(make e2e builds the binary before it runs the suite)`);
    }
    try {
      await use(server);
    } finally {
      await server.stop();
    }
  }, { scope: 'worker' }],

  // Every journey navigates by path; the host is the worker's server.
  baseURL: async ({ server }, use) => {
    await use(server.base);
  },

  // The operator project's page must really have a coarse pointer. hasTouch +
  // isMobile (fixtures/devices.mjs) is what provides it today, measured. If
  // that ever stops holding, turn on CDP touch emulation, which flips the
  // media query on the next document, and fail loudly if even that does not.
  page: async ({ page, context }, use, testInfo) => {
    if (testInfo.project.name === 'operator' && !(await isCoarse(page))) {
      const cdp = await context.newCDPSession(page);
      await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 });
      await page.reload();
      if (!(await isCoarse(page))) {
        throw new Error('operator page is not (pointer: coarse) even with CDP touch emulation');
      }
      testInfo.annotations.push({ type: 'coarse-pointer', description: 'CDP Emulation.setTouchEmulationEnabled' });
    }
    await use(page);
  },
});

export { expect };
