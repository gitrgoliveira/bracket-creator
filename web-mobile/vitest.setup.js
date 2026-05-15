import { vi } from 'vitest';

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
