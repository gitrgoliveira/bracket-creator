import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

// JSX transformation: @vitejs/plugin-react handles `.jsx` files with the
// classic React.createElement runtime so the global `React` stub in
// vitest.setup.js is what the compiled output calls. We intentionally do
// NOT also set an `esbuild` block here — vitest 4 uses oxc for transforms
// and warns "Both esbuild and oxc options were set. oxc options will be
// used and esbuild options will be ignored." if both are present.
export default defineConfig({
  plugins: [react({
    include: /\.(js|jsx)$/,
    jsxRuntime: 'classic',
  })],
  resolve: {
    alias: {
      // qr.jsx is imported as ./qr.js in admin_shell.jsx because esbuild's
      // non-bundling transform preserves import specifiers verbatim and the
      // compiled output in dist/ is qr.js. Vitest needs to resolve the .js
      // specifier back to the .jsx source.
      './qr.js': path.resolve(__dirname, 'js/qr.jsx'),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.js'],
  },
});
