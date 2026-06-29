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

// Stub browser dialog APIs that jsdom leaves undefined. Components that call
// alert/confirm/prompt directly (rather than via window.confirmDialog) would
// otherwise throw in the render suite.
global.alert = vi.fn();
global.confirm = vi.fn(() => true);
global.prompt = vi.fn(() => null);

// admin_helpers.jsx sets window constants (MAX_RANK, MAX_COURTS, MAX_TEAM_SIZE,
// etc.) that are evaluated at module load time by several components.
// Load it now so those globals are available when render tests import components.
await import('./js/admin_helpers.jsx');

// viewer_utils.jsx publishes window.poolLabel / window.leagueAwareLabel, which
// the admin scoring/shiaijo/pools surfaces call at render time (in production
// viewer.js evaluates this module before the admin bundles load). Import it so
// mounted admin components have those globals, mirroring the admin_helpers load.
await import('./js/viewer_utils.jsx');

// ui.jsx publishes window.EmptyState (and other shared UI primitives) that
// consumer components alias at module-eval time. Load it so render tests see
// the real component, mirroring the browser's index.html load order.
await import('./js/ui.jsx');

// Fail tests that produce unexpected console.warn or console.error, matches
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
