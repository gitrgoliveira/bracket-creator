// Shared test utilities for vitest suites.
import { vi } from 'vitest';

export function makeNotifMock({ permission = 'default', requestResult = 'granted', requestThrows = false } = {}) {
  let _perm = permission;
  const mock = vi.fn();
  Object.defineProperty(mock, 'permission', {
    get: () => _perm,
    set: (v) => { _perm = v; },
    configurable: true,
  });
  mock.requestPermission = vi.fn().mockImplementation(async () => {
    if (requestThrows) throw new Error('blocked');
    if (requestResult === 'granted' || requestResult === 'denied') _perm = requestResult;
    return requestResult;
  });
  return mock;
}

export function makeLocalStorageMock(initial = {}) {
  const store = { ...initial };
  return {
    getItem: vi.fn((k) => (k in store ? store[k] : null)),
    setItem: vi.fn((k, v) => { store[k] = String(v); }),
    removeItem: vi.fn((k) => { delete store[k]; }),
    clear: () => { Object.keys(store).forEach(k => delete store[k]); },
    get _store() { return store; },
  };
}

export function makeThrowingLocalStorageMock() {
  return {
    getItem: vi.fn(() => null),
    setItem: vi.fn(() => { throw new DOMException('QuotaExceeded', 'QuotaExceededError'); }),
    removeItem: vi.fn(() => {}),
    clear: () => {},
  };
}

// Both setItem and removeItem throw. Simulates a fully locked/corrupted storage profile.
export function makeFullyLockedLocalStorageMock(initial = {}) {
  const store = { ...initial };
  return {
    getItem: vi.fn((k) => (k in store ? store[k] : null)),
    setItem: vi.fn(() => { throw new DOMException('QuotaExceeded', 'QuotaExceededError'); }),
    removeItem: vi.fn(() => { throw new DOMException('QuotaExceeded', 'QuotaExceededError'); }),
    clear: () => {},
  };
}

// stubVSchedGlobals: the window.* helpers VSchedItem (viewer_match.jsx) reads
// at render time, stubbed the way its mounting suites need them. boutMiddle
// stubs to "vs" because the real primitive (bracket.jsx) returns "vs" for
// decision-less fixtures. Pass per-suite extras to route more keys through
// the same save/restore. Returns restore(): puts back (or deletes) whatever
// each key held before.
export function stubVSchedGlobals(extra = {}) {
  const stubs = {
    ipponsFromScore: vi.fn(() => []),
    matchScoreStr: vi.fn(() => ''),
    boutMiddle: vi.fn(() => 'vs'),
    queueLabelCompact: null,
    ...extra,
  };
  global.window = global.window || {};
  const saved = {};
  for (const [k, v] of Object.entries(stubs)) {
    saved[k] = Object.prototype.hasOwnProperty.call(global.window, k)
      ? { had: true, val: global.window[k] } : { had: false };
    global.window[k] = v;
  }
  return () => {
    for (const k of Object.keys(saved)) {
      if (saved[k].had) global.window[k] = saved[k].val;
      else delete global.window[k];
    }
  };
}
