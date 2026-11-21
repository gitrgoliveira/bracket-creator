import { vi, beforeEach, afterEach } from 'vitest';

// Fail tests that produce unexpected console.warn or console.error.
// Tests that intentionally trigger warnings mock console themselves
// (vi.spyOn(console, 'warn').mockImplementation(...)), which replaces
// the spy installed here, so the afterEach check only fires for
// genuinely unexpected output.
// Shared by the unit and node projects' setup files (one owner of the guard).
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
