import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

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

// Globals this suite stubs so admin_shell.jsx (a window.* global-script
// module) resolves its sibling helpers at import time. We snapshot the
// originals and restore them in afterAll so these stubs can't leak into
// other suites and cause order-dependent flakes.
const STUBBED_GLOBALS = {
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || `${s}s`)}`,
  formatLabelShort: (f) => f,
  formatDate: (d) => d,
  competitionKindLabel: () => 'Individual',
  // Stub component: CompCard references <StatusBadge> via the global. The
  // React stub's createElement never invokes it, so a no-op suffices.
  StatusBadge: function StatusBadge() { return null; },
};
const originalGlobals = {};

beforeAll(async () => {
  for (const [key, stub] of Object.entries(STUBBED_GLOBALS)) {
    originalGlobals[key] = { had: key in window, value: window[key] };
    window[key] = stub;
  }
  await import('../admin_shell.jsx');
  CompCard = window.CompCard;
});

afterAll(() => {
  for (const [key, orig] of Object.entries(originalGlobals)) {
    if (orig.had) window[key] = orig.value;
    else delete window[key];
  }
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

// Walk the React-stub vnode tree and collect all nodes matching pred.
function findAll(node, pred) {
  if (!node || typeof node !== 'object') return [];
  const acc = Array.isArray(node) ? [] : (pred(node) ? [node] : []);
  const kids = Array.isArray(node) ? node : (node.children ?? []);
  for (const k of kids) acc.push(...findAll(k, pred));
  return acc;
}

// ExportPdfModal — busy-gating, API call, filename construction.
// The component is exported to window by admin_shell.jsx so tests can
// exercise it directly without mounting a full AdminDashboard.
describe('ExportPdfModal', () => {
  // Snapshot and restore affected globals so this suite can't leak.
  const saved = {};
  const STUBS = ['API', 'useEscapeToClose'];

  beforeAll(() => {
    for (const k of STUBS) { saved[k] = { had: k in window, value: window[k] }; }
    // URL.createObjectURL / revokeObjectURL are not implemented in jsdom.
    saved._createObjectURL = window.URL?.createObjectURL;
    saved._revokeObjectURL = window.URL?.revokeObjectURL;

    window.useEscapeToClose = vi.fn();
    window.URL.createObjectURL = vi.fn(() => 'blob:mock');
    window.URL.revokeObjectURL = vi.fn();
    window.API = {
      exportPDFs: vi.fn().mockResolvedValue(new Blob([], { type: 'application/zip' })),
    };
  });

  afterAll(() => {
    for (const k of STUBS) {
      if (saved[k].had) window[k] = saved[k].value;
      else delete window[k];
    }
    // Mirror the window-global restore logic: delete the property when the
    // original was undefined (jsdom typically doesn't implement these) so the
    // stubs can't leak into later suites and cause order-dependent flakes.
    if (saved._createObjectURL !== undefined) {
      window.URL.createObjectURL = saved._createObjectURL;
    } else {
      delete window.URL.createObjectURL;
    }
    if (saved._revokeObjectURL !== undefined) {
      window.URL.revokeObjectURL = saved._revokeObjectURL;
    } else {
      delete window.URL.revokeObjectURL;
    }
  });

  it('renders without throwing', () => {
    expect(() => window.ExportPdfModal({
      tournament: { name: 'Kanto Open' }, password: 'pw',
      onClose: vi.fn(), showToast: vi.fn(),
    })).not.toThrow();
  });

  it('renders one Download button per PDF_EXPORT_TYPES entry (6 total)', () => {
    const vnode = window.ExportPdfModal({ onClose: vi.fn(), showToast: vi.fn() });
    // Each type row renders a single "Download" button; busyType="" (mocked
    // useState always returns initial value) so no "Generating…" label.
    const btns = findAll(vnode, n => n.type === 'button' && n.children?.[0] === 'Download');
    expect(btns).toHaveLength(6);
  });

  it('calls API.exportPDFs with the correct type and password', async () => {
    const exportPDFs = vi.fn().mockResolvedValue(new Blob([], { type: 'application/zip' }));
    window.API = { exportPDFs };
    const password = 'secret';
    const vnode = window.ExportPdfModal({
      tournament: { name: 'Test' }, password,
      onClose: vi.fn(), showToast: vi.fn(),
    });
    // PDF_EXPORT_TYPES[0] is "all" — the first Download button.
    const btn = findAll(vnode, n => n.type === 'button' && n.children?.[0] === 'Download')[0];
    await btn.props.onClick();
    expect(exportPDFs).toHaveBeenCalledWith('all', password);
  });

  it('sets a stable download filename from tournament name and type', async () => {
    const exportPDFs = vi.fn().mockResolvedValue(new Blob([], { type: 'application/zip' }));
    window.API = { exportPDFs };
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation(() => {});
    const vnode = window.ExportPdfModal({
      tournament: { name: 'Kanto Open 2026' }, password: 'pw',
      onClose: vi.fn(), showToast: vi.fn(),
    });
    const btn = findAll(vnode, n => n.type === 'button' && n.children?.[0] === 'Download')[0];
    await btn.props.onClick();
    const anchor = appendSpy.mock.calls[0]?.[0];
    expect(anchor?.download).toBe('Kanto_Open_2026_all_pdfs.zip');
    appendSpy.mockRestore();
  });

  it('shows error toast when API call fails', async () => {
    const exportPDFs = vi.fn().mockRejectedValue(new Error('LibreOffice unavailable'));
    window.API = { exportPDFs };
    const showToast = vi.fn();
    const vnode = window.ExportPdfModal({ onClose: vi.fn(), showToast });
    const btn = findAll(vnode, n => n.type === 'button' && n.children?.[0] === 'Download')[0];
    await btn.props.onClick();
    expect(showToast).toHaveBeenCalledWith(
      expect.stringContaining('LibreOffice unavailable'), 'error'
    );
  });
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
