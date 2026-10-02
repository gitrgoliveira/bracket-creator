// mount_app.js: mount the real App into a fresh #root with a test file's
// window.* stubs, at a given path. Shared setup lifted out of three render
// tests (app_online_resync, app_failed_reload_keeps_tournament,
// app_competition_refetch_keeps_newer) that each hand-rolled the same
// install-stubs / create-#root / pushState / import-app.jsx / settle
// sequence, plus the matching teardown.
//
// app.jsx renders itself into #root as a side effect of being imported, and
// it is imported dynamically so each test file gets a fresh module instance;
// Vitest isolates modules per file, so one file calling mountApp more than
// once would re-import the SAME cached module and do nothing on the second
// call. Call it exactly once per test file.
//
// Usage:
//   let unmount;
//   afterAll(() => unmount());
//   ...
//   ({ unmount } = await mountApp({ path: '/', globals: STUBBED_GLOBALS }));
import { act } from '@testing-library/react';
import { installWindowStubs } from './stub_globals.js';

// Lets every pending fetch and zero-delay timer run inside act().
export const settle = () => act(async () => { await new Promise((r) => setTimeout(r, 20)); });

export async function mountApp({ path, globals }) {
  const restoreGlobals = installWindowStubs(globals);
  const root = document.createElement('div');
  root.id = 'root';
  document.body.appendChild(root);
  const startPath = window.location.pathname;
  window.history.pushState(null, '', path);
  await act(async () => { await import('../../app.jsx'); });
  await settle();
  return {
    unmount() {
      restoreGlobals();
      root.remove();
      window.history.pushState(null, '', startPath);
    },
  };
}
