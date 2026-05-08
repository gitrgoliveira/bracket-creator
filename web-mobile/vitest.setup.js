import { vi } from 'vitest';

// Mock React since it's used as a global in the browser
global.React = {
  createElement: (type, props, ...children) => ({ type, props, children }),
  useState: (val) => [val, vi.fn()],
  useEffect: vi.fn(),
  useMemo: (fn) => fn(),
  useRef: (val) => ({ current: val }),
  useLayoutEffect: vi.fn(),
};

// Mock other browser globals if needed
global.alert = vi.fn();
global.confirm = vi.fn(() => true);
global.prompt = vi.fn(() => 'mocked');
