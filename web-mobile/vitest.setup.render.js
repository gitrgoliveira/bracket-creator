import React from 'react';
import { createRoot } from 'react-dom/client';
import '@testing-library/jest-dom';
import { vi, beforeEach, afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';

// Provide real React 18 as the window/global that component files destructure:
//   const { useState, useEffect, ... } = React;   ← evaluated at module load
// This must be set before any component module is dynamically imported.
global.React = React;
global.ReactDOM = { createRoot };

// admin_helpers.jsx sets window constants (MAX_RANK, MAX_COURTS, MAX_TEAM_SIZE,
// etc.) that are evaluated at module load time by several components.
// Load it now so those globals are available when render tests import components.
await import('./js/admin_helpers.jsx');

// Fail tests that produce unexpected console.warn or console.error — matches
// the invariant enforced by the unit suite (vitest.setup.js).
// Tests that intentionally trigger warnings (e.g. the GUARD test) must spy on
// console.error themselves with a local mock, which replaces this spy for that
// test so afterEach only sees the genuinely unexpected calls.
let warnSpy, errorSpy;

beforeEach(() => {
  warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
  errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
});

afterEach(() => {
  // Unmount BEFORE restoring spies so act()/effect warnings emitted during
  // React Testing Library teardown are captured and counted.
  cleanup();
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
