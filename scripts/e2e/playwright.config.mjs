// Local e2e journeys for the tournament app. Run through `make e2e`, which
// builds bin/bracket-creator first; see README.md.
import { defineConfig } from '@playwright/test';
import { EXPECT_TIMEOUT_MS, OPERATOR_DEVICE, PUBLIC_DEVICE } from './fixtures/devices.mjs';

// Chromium refuses to run its sandbox as root, so the sandbox is dropped there
// and only there (the same rule as scripts/screenshots/run.mjs).
const asRoot = typeof process.getuid === 'function' && process.getuid() === 0;

export default defineConfig({
  testDir: './journeys',
  // Playwright wipes outputDir at the start of every run. Two runs at once in
  // one checkout must not share it, or the second run's wipe deletes the first
  // run's traces mid-test (ENOENT on context close): give each concurrent run
  // its own E2E_RESULTS_DIR. It is read from the environment, not derived from
  // the pid, because every worker process re-reads this file and must resolve
  // the same directory. The journeys' own folders (per-step screenshots and
  // audit.json, fixtures/shots.mjs) follow it: <E2E_RESULTS_DIR>/journeys/
  // <journey>/ when it is set, so a second run never empties the first run's
  // folders either, and output/<journey>/, beside output/test-results/, when
  // it is not.
  outputDir: process.env.E2E_RESULTS_DIR || './output/test-results',
  // A journey drives the create wizard, the roster and the draw through the
  // interface before it reaches the step it is about, so a test is slow by
  // design. Each spec file gets its own server (fixtures/test.mjs), so files
  // can run in parallel, and a worker that runs several files starts a fresh
  // one for each; tests inside a file run in order.
  timeout: 240_000,
  expect: { timeout: EXPECT_TIMEOUT_MS },
  fullyParallel: false,
  forbidOnly: true,
  retries: 0,
  reporter: 'list',
  use: {
    browserName: 'chromium',
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    launchOptions: { args: asRoot ? ['--no-sandbox'] : [] },
  },
  projects: [
    // The court table. Every journey that is about the operator runs here.
    {
      name: 'operator',
      use: { ...OPERATOR_DEVICE },
      testIgnore: /\.public\.spec\.mjs$/,
    },
    // A spectator's phone. A journey that is about a public surface alone is
    // named *.public.spec.mjs and runs here. An operator journey that checks
    // a public surface on the side opens its own PUBLIC_DEVICE context.
    {
      name: 'public',
      use: { ...PUBLIC_DEVICE },
      testMatch: /\.public\.spec\.mjs$/,
    },
  ],
});
