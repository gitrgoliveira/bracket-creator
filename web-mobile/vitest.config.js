import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'url';
import path from 'path';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Three vitest projects run under `vitest run` / `npm test`:
//
//  unit  , pure-logic suite with the fake React global stub (~1572 tests).
//           Fast: components are never mounted; logic helpers are called directly.
//  node  , the unit files that touch no page, picked by the file-name suffix
//           `<name>.node.test.js(x)`: plain Node, no jsdom, only the console guard.
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
const makePlugins = () => [tabInstances(), react({ include: /\.(js|jsx)$/, jsxRuntime: 'classic' })];
const sharedResolve = {
  alias: {
    // qr.jsx is imported as ./qr.js in admin_shell.jsx because esbuild's
    // non-bundling transform preserves import specifiers verbatim and the
    // compiled output in dist/ is qr.js. Vitest needs to resolve the .js
    // specifier back to the .jsx source.
    './qr.js': path.resolve(__dirname, 'js/qr.jsx'),
  },
};

export default defineConfig({
  test: {
    projects: [
      {
        plugins: makePlugins(),
        resolve: sharedResolve,
        test: {
          name: 'unit',
          environment: 'jsdom',
          globals: true,
          setupFiles: ['./vitest.setup.js'],
          exclude: [
            '**/node_modules/**',
            '**/dist/**',
            'js/__tests__/render/**',
            '**/*.node.test.*',
          ],
        },
      },
      {
        plugins: makePlugins(),
        resolve: sharedResolve,
        test: {
          name: 'node',
          environment: 'node',
          globals: true,
          // Only the console guard: no fake React, no window.* side-effect
          // imports, no storage shim. A file that needs any of them is not a
          // node file and stays in the jsdom unit project.
          setupFiles: ['./vitest.setup.node.js'],
          include: ['js/__tests__/**/*.node.test.{js,jsx}'],
        },
      },
      {
        plugins: makePlugins(),
        resolve: sharedResolve,
        test: {
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
