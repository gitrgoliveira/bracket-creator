import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { isAnnouncementActive, filterActiveAnnouncements } from '../app.jsx';
import { AnnouncementBanner, AnnouncementCard } from '../viewer.jsx';

// Helper: recursively search a React element tree (mock objects) for all
// elements matching a predicate, returning them as a flat list.
// Handles the case where children is a nested array (as produced by list.map()
// spread into React.createElement's rest param: children becomes [[...items]]).
function findAll(node, predicate) {
  if (node === null || node === undefined) return [];
  if (Array.isArray(node)) {
    return node.flatMap(n => findAll(n, predicate));
  }
  if (typeof node !== 'object') return [];
  const results = [];
  if (predicate(node)) results.push(node);
  // children is either [] (no children), [[...]] (array from map spread),
  // or [child1, child2, ...] (multiple JSX children).
  const children = Array.isArray(node.children) ? node.children : (node.children != null ? [node.children] : []);
  for (const child of children) {
    results.push(...findAll(child, predicate));
  }
  return results;
}

function findByClass(tree, cls) {
  return findAll(tree, n => n.props && n.props.className && n.props.className.split(' ').includes(cls));
}

function findByType(tree, type) {
  return findAll(tree, n => n.type === type);
}

// isAnnouncementActive encapsulates the gate that decides whether an
// announcement fetched at mount (or received via SSE) should be shown.
// Three cases: active → show, dismissed → hide, expired → hide.

const future = new Date(Date.now() + 60_000).toISOString();
const past = new Date(Date.now() - 60_000).toISOString();

describe('isAnnouncementActive', () => {
  it('returns true for a non-dismissed, non-expired announcement', () => {
    const ann = { sentAt: 'ts1', expiresAt: future, message: 'Hello' };
    expect(isAnnouncementActive(ann, null, new Date())).toBe(true);
  });

  it('returns false when the announcement has been dismissed (sessionStorage key set)', () => {
    const ann = { sentAt: 'ts1', expiresAt: future, message: 'Hello' };
    expect(isAnnouncementActive(ann, 'true', new Date())).toBe(false);
  });

  it('returns false when the announcement has expired', () => {
    const ann = { sentAt: 'ts1', expiresAt: past, message: 'Hello' };
    expect(isAnnouncementActive(ann, null, new Date())).toBe(false);
  });

  it('returns false for null announcement', () => {
    expect(isAnnouncementActive(null, null, new Date())).toBe(false);
  });

  it('treats an announcement expiring exactly now as inactive', () => {
    const now = new Date();
    const ann = { sentAt: 'ts1', expiresAt: now.toISOString() };
    expect(isAnnouncementActive(ann, null, now)).toBe(false);
  });
});

describe('filterActiveAnnouncements', () => {
  const now = new Date();

  beforeEach(() => {
    sessionStorage.clear();
  });

  afterEach(() => {
    sessionStorage.clear();
  });

  it('returns active announcements unchanged', () => {
    const anns = [
      { id: 'a1', expiresAt: future, message: 'A' },
      { id: 'a2', expiresAt: future, message: 'B' },
    ];
    expect(filterActiveAnnouncements(anns, now)).toEqual(anns);
  });

  it('filters out expired announcements', () => {
    const anns = [
      { id: 'a1', expiresAt: future, message: 'keep' },
      { id: 'a2', expiresAt: past, message: 'drop' },
    ];
    const result = filterActiveAnnouncements(anns, now);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('a1');
  });

  it('filters out announcements dismissed via sessionStorage key', () => {
    sessionStorage.setItem('bc_dismissed_announcement_a1', 'true');
    const anns = [
      { id: 'a1', expiresAt: future, message: 'dismissed' },
      { id: 'a2', expiresAt: future, message: 'visible' },
    ];
    const result = filterActiveAnnouncements(anns, now);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('a2');
  });

  it('uses the ID-based sessionStorage key pattern', () => {
    sessionStorage.setItem('bc_dismissed_announcement_xyz', 'true');
    const ann = { id: 'xyz', expiresAt: future, message: 'x' };
    expect(filterActiveAnnouncements([ann], now)).toHaveLength(0);
  });

  it('returns empty array when all are expired or dismissed', () => {
    sessionStorage.setItem('bc_dismissed_announcement_a2', 'true');
    const anns = [
      { id: 'a1', expiresAt: past, message: 'expired' },
      { id: 'a2', expiresAt: future, message: 'dismissed' },
    ];
    expect(filterActiveAnnouncements(anns, now)).toHaveLength(0);
  });

  it('returns empty array for empty input', () => {
    expect(filterActiveAnnouncements([], now)).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Overlay render tests — verify the new stacked-cards structure via the
// global React mock (plain-object tree). Real hooks don't run in this
// environment; we verify structure and wire up the timer effect manually.
// ---------------------------------------------------------------------------

describe('AnnouncementBanner overlay structure', () => {
  const future2 = new Date(Date.now() + 120_000).toISOString();

  it('renders null when announcements list is empty', () => {
    const result = AnnouncementBanner({ announcements: [], onDismiss: vi.fn() });
    expect(result).toBeNull();
  });

  it('renders an .announcement-overlay container wrapping all cards', () => {
    const anns = [
      { id: 'x1', expiresAt: future2, message: 'First' },
      { id: 'x2', expiresAt: future2, message: 'Second' },
      { id: 'x3', expiresAt: future2, message: 'Third' },
    ];
    const tree = AnnouncementBanner({ announcements: anns, onDismiss: vi.fn() });
    expect(tree).not.toBeNull();
    // Top-level must be the overlay container
    expect(tree.props.className).toBe('announcement-overlay');
  });

  it('renders one AnnouncementCard child per announcement (not a single rotating slot)', () => {
    const anns = [
      { id: 'x1', expiresAt: future2, message: 'A' },
      { id: 'x2', expiresAt: future2, message: 'B' },
      { id: 'x3', expiresAt: future2, message: 'C' },
    ];
    const tree = AnnouncementBanner({ announcements: anns, onDismiss: vi.fn() });
    // The overlay's children should be AnnouncementCard elements (one per item)
    const cards = findByType(tree, AnnouncementCard);
    expect(cards).toHaveLength(3);
  });

  it('passes each card its own ann prop with the correct id', () => {
    const anns = [
      { id: 'alpha', expiresAt: future2, message: 'Msg A' },
      { id: 'beta',  expiresAt: future2, message: 'Msg B' },
    ];
    const tree = AnnouncementBanner({ announcements: anns, onDismiss: vi.fn() });
    const cards = findByType(tree, AnnouncementCard);
    const ids = cards.map(c => c.props.ann.id);
    expect(ids).toEqual(['alpha', 'beta']);
  });

  it('does NOT contain an N/M rotation counter anywhere in the tree', () => {
    const anns = [
      { id: 'y1', expiresAt: future2, message: 'One' },
      { id: 'y2', expiresAt: future2, message: 'Two' },
    ];
    const tree = AnnouncementBanner({ announcements: anns, onDismiss: vi.fn() });
    // Look for any element with className containing "count"
    const countEls = findByClass(tree, 'announcement-banner__count');
    expect(countEls).toHaveLength(0);
    // Also stringify the whole tree and confirm no "N/M"-style fraction text
    const treeStr = JSON.stringify(tree);
    expect(treeStr).not.toMatch(/\d\/\d/);
  });
});

describe('AnnouncementCard dismiss and per-card timer', () => {
  const future2 = new Date(Date.now() + 120_000).toISOString();

  it('clicking the dismiss button calls onDismiss with the card id', () => {
    const onDismiss = vi.fn();
    const ann = { id: 'card-1', expiresAt: future2, message: 'Hello' };
    const tree = AnnouncementCard({ ann, onDismiss });

    // Find the dismiss button in the tree
    const buttons = findAll(tree, n => n.props && n.props['aria-label'] === 'Dismiss announcement');
    expect(buttons).toHaveLength(1);
    // Trigger the onClick
    buttons[0].props.onClick();
    expect(onDismiss).toHaveBeenCalledWith('card-1');
  });

  it('per-card expiry effect calls onDismiss with this card id when expired', () => {
    // viewer.jsx captures `useEffect` via destructuring at module scope:
    //   const { useEffect } = React;
    // The destructured binding points to the SAME vi.fn() object as
    // React.useEffect, so mockImplementationOnce on React.useEffect affects
    // the call inside AnnouncementCard as well.
    const capturedCallbacks = [];
    React.useEffect.mockImplementation((cb) => { capturedCallbacks.push(cb); });

    const onDismiss = vi.fn();
    // Use a past expiresAt so the first updateTimer tick immediately dismisses.
    const ann = { id: 'expire-me', expiresAt: past, message: 'Gone' };
    AnnouncementCard({ ann, onDismiss });

    // Restore to no-op before any assertions
    React.useEffect.mockImplementation(() => {});

    expect(capturedCallbacks.length).toBeGreaterThanOrEqual(1);
    // Run each captured callback; the expired card must trigger onDismiss.
    // The timer effect creates a real setInterval and returns a clearInterval
    // cleanup — capture and run every cleanup so no live interval leaks past
    // this test (a leaked interval can hang Vitest or fire stray callbacks).
    let dismissed = false;
    const cleanups = [];
    for (const cb of capturedCallbacks) {
      cleanups.push(cb());
      if (onDismiss.mock.calls.length > 0) { dismissed = true; break; }
    }
    cleanups.forEach(fn => { if (typeof fn === 'function') fn(); });
    expect(dismissed).toBe(true);
    expect(onDismiss).toHaveBeenCalledWith('expire-me');
  });

  it('a card with a future expiresAt does NOT immediately call onDismiss', () => {
    const capturedCallbacks = [];
    React.useEffect.mockImplementation((cb) => { capturedCallbacks.push(cb); });

    const onDismiss = vi.fn();
    const ann = { id: 'still-live', expiresAt: future2, message: 'Live' };
    AnnouncementCard({ ann, onDismiss });

    React.useEffect.mockImplementation(() => {});

    // Run all captured callbacks (the timer effect sets up the interval but
    // the first tick should NOT dismiss since diff > 0). A future card's
    // interval never self-clears, so run the returned cleanup to clear it —
    // otherwise the live setInterval leaks past this test.
    for (const cb of capturedCallbacks) {
      const cleanup = cb();
      if (typeof cleanup === 'function') cleanup();
    }
    // onDismiss must NOT have been called immediately (card is not expired)
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it('two cards with different ids fire onDismiss independently', () => {
    const capturedForCard1 = [];
    const capturedForCard2 = [];

    const onDismiss1 = vi.fn();
    React.useEffect.mockImplementation((cb) => { capturedForCard1.push(cb); });
    AnnouncementCard({ ann: { id: 'c1', expiresAt: past, message: 'C1' }, onDismiss: onDismiss1 });

    const onDismiss2 = vi.fn();
    React.useEffect.mockImplementation((cb) => { capturedForCard2.push(cb); });
    AnnouncementCard({ ann: { id: 'c2', expiresAt: past, message: 'C2' }, onDismiss: onDismiss2 });

    // Restore before assertions
    React.useEffect.mockImplementation(() => {});

    // Run each effect and immediately run its returned cleanup so no live
    // interval leaks past the test.
    for (const cb of capturedForCard1) { const c = cb(); if (typeof c === 'function') c(); }
    for (const cb of capturedForCard2) { const c = cb(); if (typeof c === 'function') c(); }

    // Each callback fires for its own card id only
    expect(onDismiss1).toHaveBeenCalledWith('c1');
    expect(onDismiss2).toHaveBeenCalledWith('c2');
    // And they are independent — c1's dismiss did not fire c2's and vice versa
    expect(onDismiss1).not.toHaveBeenCalledWith('c2');
    expect(onDismiss2).not.toHaveBeenCalledWith('c1');
  });
});
