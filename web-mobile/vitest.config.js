import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'url';
import path from 'path';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Three vitest projects run under `vitest run` / `npm test`:
//
//  unit  , pure-logic suite with the fake React global stub.
//           Fast: components are never mounted; logic helpers are called directly.
//  node  , the unit files that touch no page, picked by the file-name suffix
//           `<name>.node.test.<ext>`: plain Node, no jsdom, only the console guard.
//           A file is in exactly one of unit and node (unit excludes the suffix).
//  render, render-smoke suite with REAL React 19 + jsdom. Components are
//           mounted via @testing-library/react so a missing window.* reference
//           throws a ReferenceError that the unit suite cannot see.
//
// JSX transformation: @vitejs/plugin-react handles `.jsx` files with the
// classic React.createElement runtime so the global `React` (stub or real,
// depending on the project) is what the compiled output calls. We intentionally
// do NOT also set an `esbuild` block here, vitest 4 uses oxc for transforms
// and warns "Both esbuild and oxc options were set. oxc options will be used
// and esbuild options will be ignored." if both are present.

// Use a factory so each project gets its own plugin instance, sharing a
// single instance across projects can cause cross-project state leakage in
// plugins that cache state per build (e.g. configResolved caches).
// A test that opens two tabs of one device imports api_client.jsx twice, under
// its own query each (`api_client.jsx?tab=a`). The query makes a second module
// instance of the entry only; the outbox modules it imports (`api_client_*`)
// would be shared, so both "tabs" would hold one queue. This carries the
// importer's `tab` query onto those imports, so each tab gets the whole outbox
// family as a browser tab would. Every other import stays shared, as before.
const TAB_QUERY = /[?&]tab=([^&#]+)/;
const tabInstances = () => ({
  name: 'bc-tab-instances',
  enforce: 'pre',
  async resolveId(source, importer, options) {
    const tab = importer && TAB_QUERY.exec(importer)?.[1];
    if (!tab || !/(^|\/)api_client_\w+\.jsx$/.test(source)) return null;
    const resolved = await this.resolve(source, importer, { ...options, skipSelf: true });
    if (!resolved || TAB_QUERY.test(resolved.id)) return resolved;
    return { ...resolved, id: `${resolved.id}?tab=${tab}` };
  },
});
// The one spelling of "a node file": the unit project excludes it and the node
// project includes it, so a file is in exactly one of them wherever it sits.
const NODE_TEST_GLOB = '**/*.node.test.*';
const makePlugins = () => [tabInstances(), react({ include: /\.(js|jsx)$/, jsxRuntime: 'classic' })];

// The pool is vmThreads, set on each project below: an inline project does not
// inherit the root `test` block. Isolation and file order stay at vitest's own
// settings (isolate on, the built-in sequencer). Measured 2026-11-20 on vitest
// 5.1.0, 577 files, 10 cores, warm cache, `nice -n 10` as the gate runs it,
// medians of three interleaved runs:
//  - vmThreads: 35.3 s against forks' 63.8 s (45 % faster, past the 10 % a
//    pool change must gain), green on every run and under three shuffled file
//    orders. Most of the gain is less work, not less waiting: user CPU 301 s
//    against 517 s, sys 10 s against 68 s (no process and no fresh jsdom per
//    file). Peak memory summed over the whole process tree: 5.0 GB against
//    forks' 4.2 GB. Its one process holds every worker's heap (about 5 GB),
//    where one fork peaks at 0.5 GB, so compare the sums, not the processes.
//  - Under vmThreads `window`, `performance` and `location` are getters on the
//    jsdom global, so a test that assigns `global.window = ...` (or the other
//    two) throws "has only a getter". Swap a global with vi.stubGlobal, undone
//    by vi.unstubAllGlobals().
//  - threads, measured before those rewrites against forks' 65.3 s: green,
//    61.8 s (5 %), under the 10 %.
//  - isolate: false: red, with a different failure set every run (tests leak
//    module and window.* state into the next file in the worker).
//  - a sequencer putting the permutation shards first on a cold cache: 66.7 s
//    against 66.0 s; the shards already end well inside the wall.
//  - fewer workers (maxWorkers: 4, under forks): 108.6 s. The cores are
//    saturated.
// CI reads this pool too (no --pool flag in validate.yaml). Measured on CI
// (ubuntu-latest, 4 cores, 16 GB), two runs of one commit, the whole suite in
// one job: vmThreads 187 s and 178 s, peak summed RSS 6.2 and 6.5 GB (40 % of
// the runner); forks 227 s and 352 s; threads 193 s and 215 s. Under vmThreads,
// 2 to 4 jobs gave 84 to 50 s for the slowest job's test step; CI runs it as
// four jobs (--shard=N/4 in validate.yaml, 65 s and 50 s). Re-measure before
// changing any of it.
const POOL = 'vmThreads';
export default defineConfig({
  test: {
    projects: [
      {
        plugins: makePlugins(),
        test: {
          pool: POOL,
          name: 'unit',
          environment: 'jsdom',
          globals: true,
          setupFiles: ['./vitest.setup.js'],
          exclude: [
            '**/node_modules/**',
            '**/dist/**',
            'js/__tests__/render/**',
            NODE_TEST_GLOB,
          ],
        },
      },
      {
        plugins: makePlugins(),
        test: {
          pool: POOL,
          name: 'node',
          environment: 'node',
          globals: true,
          // Only the console guard: no fake React, no window.* side-effect
          // imports, no storage shim. A file that needs any of them is not a
          // node file and stays in the jsdom unit project.
          setupFiles: ['./vitest.setup.node.js'],
          include: [NODE_TEST_GLOB],
        },
      },
      {
        plugins: makePlugins(),
        test: {
          pool: POOL,
          name: 'render',
          environment: 'jsdom',
          globals: true,
          // Real React 19, deliberately does NOT include vitest.setup.js
          // (the fake-React stub). Function bodies execute; missing window.*
          // references throw ReferenceErrors.
          setupFiles: ['./vitest.setup.render.js'],
          include: ['js/__tests__/render/**/*.render.test.jsx'],
        },
      },
    ],
  },
});
