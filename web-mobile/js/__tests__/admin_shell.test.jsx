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

// Recursively gather string/number leaves from the React-stub vnode tree
// ({type, props, children}) so we can assert on rendered text without a
// real DOM.
function collectText(node) {
  if (node == null || node === false || node === true) return '';
  if (typeof node === 'string') return node;
  if (typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children !== undefined) return collectText(node.children);
  return '';
}

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

  // Copilot review (PR #195): with no date/startTime and empty courts the
  // meta line must not render a dangling " · " separator.
  it('omits the meta separator entirely when courts and date/time are empty', () => {
    const c = { id: 'x', name: 'X', format: 'playoffs', status: 'setup', courts: null };
    expect(collectText(CompCard({ c, onOpen: noop, onStart: noop }))).not.toContain('·');
  });

  it('does not lead the court list with a separator when nothing precedes it', () => {
    const c = { id: 'x', name: 'X', format: 'playoffs', status: 'setup', courts: ['A', 'B'] };
    const text = collectText(CompCard({ c, onOpen: noop, onStart: noop }));
    expect(text).toContain('A, B');
    expect(text).not.toContain('·');
  });

  it('separates date from the court list with " · " when both are present', () => {
    const c = { id: 'x', name: 'X', format: 'playoffs', status: 'setup', date: '2026-06-01', courts: ['A'] };
    expect(collectText(CompCard({ c, onOpen: noop, onStart: noop }))).toContain('·');
  });
});
