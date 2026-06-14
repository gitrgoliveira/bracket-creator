import React from 'react';
import { createRoot } from 'react-dom/client';
import '@testing-library/jest-dom';
import { afterEach } from 'vitest';
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

// Clean up mounted components after each test.
afterEach(() => {
  cleanup();
});
