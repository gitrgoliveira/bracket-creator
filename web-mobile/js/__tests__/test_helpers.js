// Shared test utilities for vitest suites.
import { vi } from 'vitest';

export function makeLocalStorageMock(initial = {}) {
  const store = { ...initial };
  return {
    getItem: (k) => (k in store ? store[k] : null),
    setItem: vi.fn((k, v) => { store[k] = String(v); }),
    removeItem: (k) => { delete store[k]; },
    clear: () => { Object.keys(store).forEach(k => delete store[k]); },
    get _store() { return store; },
  };
}

export function makeThrowingLocalStorageMock() {
  return {
    getItem: () => null,
    setItem: vi.fn(() => { throw new DOMException('QuotaExceeded', 'QuotaExceededError'); }),
    removeItem: () => {},
    clear: () => {},
  };
}
