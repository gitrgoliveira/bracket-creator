import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'url';
import path from 'path';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Two vitest projects run under `vitest run` / `npm test`:
//
//  unit  , pure-logic suite with the fake React global stub (~1572 tests).
//           Fast: components are never mounted; logic helpers are called directly.
//  render, render-smoke suite with REAL React 18 + jsdom. Components are
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
const makePlugins = () => [react({ include: /\.(js|jsx)$/, jsxRuntime: 'classic' })];
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
          ],
        },
      },
      {
        plugins: makePlugins(),
        resolve: sharedResolve,
        test: {
          name: 'render',
          environment: 'jsdom',
          globals: true,
          // Real React 18, deliberately does NOT include vitest.setup.js
          // (the fake-React stub). Function bodies execute; missing window.*
          // references throw ReferenceErrors.
          setupFiles: ['./vitest.setup.render.js'],
          include: ['js/__tests__/render/**/*.render.test.jsx'],
        },
      },
    ],
  },
});
