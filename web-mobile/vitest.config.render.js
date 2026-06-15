import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'url';
import path from 'path';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react({
    include: /\.(js|jsx)$/,
    jsxRuntime: 'classic',
  })],
  resolve: {
    alias: {
      './qr.js': path.resolve(__dirname, 'js/qr.jsx'),
    },
  },
  test: {
    name: 'render',
    environment: 'jsdom',
    globals: true,
    // Uses real React — deliberately does NOT include vitest.setup.js
    // (the fake-React stub). Components are actually mounted; function
    // bodies execute; missing window.* references throw ReferenceErrors.
    setupFiles: ['./vitest.setup.render.js'],
    include: ['js/__tests__/render/**/*.render.test.jsx'],
  },
});
