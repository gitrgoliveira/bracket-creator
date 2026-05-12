import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react({
    include: /\.(js|jsx)$/,
    jsxRuntime: 'classic',
  })],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.js'],
  },
  esbuild: {
    loader: 'jsx',
    include: /js\/.*\.js$/,
  },
});
