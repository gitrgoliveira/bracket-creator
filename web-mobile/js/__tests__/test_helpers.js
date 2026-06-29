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
