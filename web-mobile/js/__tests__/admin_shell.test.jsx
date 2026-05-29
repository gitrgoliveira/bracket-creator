import { describe, it, expect, beforeAll } from 'vitest';

// Regression: CompCard crashed the entire admin console with
//   TypeError: Cannot read properties of null (reading 'join')
// when rendering a competition whose `courts` (or `players`) field was
// null. The admin create form always sends courts, so this only bit
// API/import-created competitions — but it tripped the error boundary
// and took down the whole dashboard rather than degrading gracefully.
// Fix: CompCard defaults null courts/players to [] before .join/.length.
//
// admin_shell.jsx is a global-script module (window.* pattern, like the
// other admin_*.jsx files). It captures its sibling helpers from window
// at module-eval time, so the stubs below must be installed BEFORE the
// dynamic import. compMatchStats / sideName come from admin_helpers.jsx,
// which vitest.setup.js already loads for side effects.
let CompCard;

beforeAll(async () => {
  window.pluralize = (n, s, p) => `${n} ${n === 1 ? s : (p || `${s}s`)}`;
  window.formatLabelShort = (f) => f;
  window.formatDate = (d) => d;
  window.competitionKindLabel = () => 'Individual';
  // Stub component: CompCard references <StatusBadge> via the global. The
  // React stub's createElement never invokes it, so a no-op suffices.
  window.StatusBadge = function StatusBadge() { return null; };
  await import('../admin_shell.jsx');
  CompCard = window.CompCard;
});

describe('CompCard', () => {
  const noop = () => {};

  it('renders a competition with null courts/players without throwing', () => {
    const c = {
      id: 'x', name: 'X', format: 'playoffs', status: 'setup',
      courts: null, players: null,
    };
    expect(() => CompCard({ c, onOpen: noop, onStart: noop })).not.toThrow();
  });

  it('renders a competition with missing courts/players without throwing', () => {
    const c = { id: 'y', name: 'Y', format: 'playoffs', status: 'setup' };
    expect(() => CompCard({ c, onOpen: noop, onStart: noop })).not.toThrow();
  });

  it('still renders a fully-populated competition', () => {
    const c = {
      id: 'z', name: 'Z', format: 'playoffs', status: 'pools',
      courts: ['A', 'B'], players: [{ id: 'p1' }, { id: 'p2' }],
    };
    expect(() => CompCard({ c, onOpen: noop, onStart: noop })).not.toThrow();
  });
});
