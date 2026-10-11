// The `test` every journey imports: Playwright's own, plus a server per spec
// file and an operator page whose pointer is verified coarse.
import { test as base, expect } from '@playwright/test';
import { start } from '../../screenshots/lib/server.mjs';
import { isCoarse, restoreCoarse } from './pointer.mjs';

export { isCoarse };

export const test = base.extend({
  // One mobile-app server per SPEC FILE, on a free port and a throwaway
  // TOURNAMENT_DATA_DIR, started and stopped by scripts/screenshots'
  // server.mjs (SIGTERM then SIGKILL by PID). Playwright hands a worker one
  // file after another, so a worker-scoped server would greet the second file
  // with the first file's tournament; this fixture keeps at most one server,
  // and `serverForFile(file)` stops it and starts a fresh one when the file
  // changes. Tests in a file share theirs, so a spec that seeds once and walks
  // several tests declares test.describe.configure({ mode: 'serial' }). A
  // beforeAll hook (worker scope) asks for it with its own testInfo.file.
  serverForFile: [async ({}, use) => {
    let current = null; // { file, server }
    const forFile = async (file) => {
      if (current && current.file === file) return current.server;
      if (current) {
        const old = current;
        current = null;
        await old.server.stop();
      }
      let server;
      try {
        server = await start('mobile');
      } catch (err) {
        throw new Error(`${err.message}\n(make e2e builds the binary before it runs the suite)`);
      }
      current = { file, server };
      return server;
    };
    try {
      await use(forFile);
    } finally {
      if (current) await current.server.stop();
    }
  }, { scope: 'worker' }],

  // The server of the file this test is in.
  server: async ({ serverForFile }, use, testInfo) => {
    await use(await serverForFile(testInfo.file));
  },

  // Every journey navigates by path; the host is the file's server.
  baseURL: async ({ server }, use) => {
    await use(server.base);
  },

  // The operator project's page must really have a coarse pointer. hasTouch +
  // isMobile (fixtures/devices.mjs) is what provides it today, measured. If
  // that ever stops holding, turn on CDP touch emulation in place
  // (restoreCoarse, which fails loudly if even that does not do it).
  page: async ({ page }, use, testInfo) => {
    if (testInfo.project.name === 'operator' && !(await isCoarse(page))) {
      await restoreCoarse(page);
      testInfo.annotations.push({ type: 'coarse-pointer', description: 'CDP Emulation.setTouchEmulationEnabled' });
    }
    await use(page);
  },
});

export { expect };
