import { vi, beforeEach, afterEach } from 'vitest';

// Fail tests that produce unexpected console.warn or console.error.
// Tests that intentionally trigger warnings mock console themselves
// (vi.spyOn(console, 'warn').mockImplementation(...)), which replaces
// the spy installed here — so the afterEach check only fires for
// genuinely unexpected output.
let warnSpy, errorSpy;

beforeEach(() => {
  warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
  errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => {
  const warns = warnSpy.mock?.calls ?? [];
  const errors = errorSpy.mock?.calls ?? [];
  warnSpy.mockRestore();
  errorSpy.mockRestore();
  if (warns.length > 0) {
    throw new Error(`Unexpected console.warn (${warns.length} call(s)):\n${warns.map((a) => a.join(' ')).join('\n')}`);
  }
  if (errors.length > 0) {
    throw new Error(`Unexpected console.error (${errors.length} call(s)):\n${errors.map((a) => a.join(' ')).join('\n')}`);
  }
});

// Mock React since it's used as a global in the browser
global.React = {
  createElement: (type, props, ...children) => ({ type, props, children }),
  useState: (val) => [val, vi.fn()],
  useEffect: vi.fn(),
  useMemo: (fn) => fn(),
  useRef: (val) => ({ current: val }),
  useLayoutEffect: vi.fn(),
  memo: (c) => c,
};

// Mock other browser globals if needed
global.alert = vi.fn();
global.confirm = vi.fn(() => true);
global.prompt = vi.fn(() => 'mocked');

// Load admin_helpers.jsx for its side effects so the `window.MAX_RANK`,
// `window.MAX_COURTS`, `window.MIN_YEAR`, etc. globals are populated for
// tests that import sibling files (admin_pools.jsx, admin_competition.jsx,
// admin_setup.jsx) that read these constants via `window.X`. In the
// browser, index.html loads admin_helpers.js before its consumers — these
// tests don't go through that load order, so without this setup the
// consumer modules see `undefined` and predicates like `next > MAX_RANK`
// silently pass invalid input.
import './js/admin_helpers.jsx';
