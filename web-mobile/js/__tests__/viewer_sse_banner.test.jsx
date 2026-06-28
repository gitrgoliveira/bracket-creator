// Tests that ViewerHome renders the SSE offline banner when sseConnected=false
// and suppresses it when sseConnected=true (default). ViewerHome is tested via
// direct function call (same pattern as AnnouncementBanner in app_announcement.test.jsx)
// with window globals set up before the dynamic import so the module-level
// `const x = window.x` bindings capture real stubs.
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';

// Helpers ----------------------------------------------------------------

function findAll(node, predicate) {
  if (node == null) return [];
  if (Array.isArray(node)) return node.flatMap(n => findAll(n, predicate));
  if (typeof node !== 'object') return [];
  const results = [];
  if (predicate(node)) results.push(node);
  const kids = Array.isArray(node.children) ? node.children : node.children != null ? [node.children] : [];
  for (const k of kids) results.push(...findAll(k, predicate));
  if (node.props?.children) {
    const pc = node.props.children;
    for (const k of [].concat(pc)) results.push(...findAll(k, predicate));
  }
  return results;
}

function findByClass(tree, cls) {
  return findAll(tree, n => {
    const cn = n.props?.className || '';
    return cn.split(' ').includes(cls);
  });
}

// Minimal tournament fixture : empty competitions so useMemo filters are no-ops.
const TOURNAMENT = { name: 'Test Tournament', date: '10-06-2026', venue: 'Budokan', competitions: [] };
const NOOP = () => {};

// -----------------------------------------------------------------------

describe('ViewerHome SSE offline banner', () => {
  let ViewerHome;
  let originals = {};

  beforeAll(async () => {
    // Stash and replace window globals that viewer.jsx captures at module load.
    // These must be set BEFORE the dynamic import so the const-bindings pick them up.
    const globals = {
      formatDate:               (d) => d || 'Date TBA',
      formatViewerHeaderEyebrow:(date, venue) => `${date} · ${venue}`,
      formatLabel:              (l) => l,
      hasBothSides:             () => false,
      compareDmy:               () => 0,
      StatusBadge:              () => null,
      pluralize:                (n, s) => `${n} ${s}`,
      queueLabelCompact:        null,
    };
    for (const [k, v] of Object.entries(globals)) {
      originals[k] = global.window[k];
      global.window[k] = v;
    }

    vi.resetModules();
    const mod = await import('../viewer.jsx');
    ViewerHome = mod.ViewerHome;
  });

  afterAll(() => {
    for (const [k, v] of Object.entries(originals)) {
      if (v === undefined) delete global.window[k];
      else global.window[k] = v;
    }
    vi.resetModules();
  });

  it('renders the SSE offline banner when sseConnected=false', () => {
    const tree = ViewerHome({ tournament: TOURNAMENT, sseConnected: false,
      onSelectCompetition: NOOP, onAdminClick: NOOP,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP });
    const banners = findByClass(tree, 'sse-offline-banner');
    expect(banners.length).toBeGreaterThan(0);
  });

  it('does NOT render the SSE offline banner when sseConnected=true', () => {
    const tree = ViewerHome({ tournament: TOURNAMENT, sseConnected: true,
      onSelectCompetition: NOOP, onAdminClick: NOOP,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP });
    const banners = findByClass(tree, 'sse-offline-banner');
    expect(banners).toHaveLength(0);
  });

  it('does NOT render the SSE offline banner when sseConnected is omitted (default true)', () => {
    const tree = ViewerHome({ tournament: TOURNAMENT,
      onSelectCompetition: NOOP, onAdminClick: NOOP,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP });
    const banners = findByClass(tree, 'sse-offline-banner');
    expect(banners).toHaveLength(0);
  });

  it('offline banner carries role=status and aria-live=polite', () => {
    const tree = ViewerHome({ tournament: TOURNAMENT, sseConnected: false,
      onSelectCompetition: NOOP, onAdminClick: NOOP,
      onOpenSchedule: NOOP, onRegister: NOOP, onOpenResults: NOOP });
    const [banner] = findByClass(tree, 'sse-offline-banner');
    expect(banner.props.role).toBe('status');
    expect(banner.props['aria-live']).toBe('polite');
  });
});
