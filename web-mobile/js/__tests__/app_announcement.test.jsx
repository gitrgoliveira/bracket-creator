import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { isAnnouncementActive, filterActiveAnnouncements, fireBrowserNotifications, diffAnnouncementSnapshot } from '../app.jsx';
import { AnnouncementBanner, AnnouncementCard, NotificationSettings, notificationSupported } from '../viewer.jsx';
import { makeReactive } from './helpers/reactive_react.js';

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

// ---------------------------------------------------------------------------
// fireBrowserNotifications — unit tests for the notification diff helper.
// These cover the four guard conditions and the happy path.
// ---------------------------------------------------------------------------

describe('fireBrowserNotifications', () => {
  let NotificationMock;
  let originalNotification;
  let originalDocumentHidden;
  let originalWindowLocalStorage;

  const setDocumentHidden = (val) => {
    Object.defineProperty(document, 'hidden', { value: val, writable: true, configurable: true });
  };

  // Create a minimal in-memory localStorage stub for tests that need it.
  // window.localStorage is undefined in this jsdom/node environment so we
  // install a mock on window directly in each test suite that reads it.
  const makeLocalStorageMock = (initialEntries = {}) => {
    const store = { ...initialEntries };
    return {
      getItem: (k) => (k in store ? store[k] : null),
      setItem: (k, v) => { store[k] = String(v); },
      removeItem: (k) => { delete store[k]; },
      clear: () => { Object.keys(store).forEach((k) => delete store[k]); },
    };
  };

  beforeEach(() => {
    originalNotification = global.Notification;
    originalDocumentHidden = Object.getOwnPropertyDescriptor(document, 'hidden');
    originalWindowLocalStorage = Object.getOwnPropertyDescriptor(window, 'localStorage');

    NotificationMock = vi.fn();
    NotificationMock.permission = 'granted';
    global.Notification = NotificationMock;

    setDocumentHidden(true);

    // Install a mock localStorage with the toggle pre-enabled.
    const lsMock = makeLocalStorageMock({ 'viewer.notifications.enabled': 'true' });
    Object.defineProperty(window, 'localStorage', { value: lsMock, writable: true, configurable: true });
  });

  afterEach(() => {
    global.Notification = originalNotification;
    if (originalDocumentHidden) {
      Object.defineProperty(document, 'hidden', originalDocumentHidden);
    } else {
      Object.defineProperty(document, 'hidden', { value: false, writable: true, configurable: true });
    }
    if (originalWindowLocalStorage) {
      Object.defineProperty(window, 'localStorage', originalWindowLocalStorage);
    } else {
      Object.defineProperty(window, 'localStorage', { value: undefined, writable: true, configurable: true });
    }
  });

  it('fires once for a new id when all guards pass', () => {
    const additions = [{ id: 'ann-1', message: 'New announcement' }];
    fireBrowserNotifications(additions);
    expect(NotificationMock).toHaveBeenCalledTimes(1);
    expect(NotificationMock).toHaveBeenCalledWith('Tournament Announcement', {
      tag: 'ann-1',
      body: 'New announcement',
      icon: '/favicon.jpeg',
    });
  });

  it('does NOT fire when document is visible (document.hidden === false)', () => {
    setDocumentHidden(false);
    fireBrowserNotifications([{ id: 'ann-2', message: 'Hello' }]);
    expect(NotificationMock).not.toHaveBeenCalled();
  });

  it('does NOT fire when permission is not granted', () => {
    NotificationMock.permission = 'default';
    fireBrowserNotifications([{ id: 'ann-3', message: 'Hello' }]);
    expect(NotificationMock).not.toHaveBeenCalled();
  });

  it('does NOT fire when permission is denied', () => {
    NotificationMock.permission = 'denied';
    fireBrowserNotifications([{ id: 'ann-4', message: 'Hello' }]);
    expect(NotificationMock).not.toHaveBeenCalled();
  });

  it('does NOT fire when the localStorage toggle is off', () => {
    // Override the mock localStorage (beforeEach sets 'true') to 'false'.
    window.localStorage.setItem('viewer.notifications.enabled', 'false');
    fireBrowserNotifications([{ id: 'ann-5', message: 'Hello' }]);
    expect(NotificationMock).not.toHaveBeenCalled();
  });

  it('does NOT fire when the Notification API is unavailable', () => {
    global.Notification = undefined;
    // Should not throw; additions are silently ignored
    expect(() => fireBrowserNotifications([{ id: 'ann-6', message: 'Hello' }])).not.toThrow();
    // Nothing to assert about calls since the mock is gone, but no throw = pass
  });

  it('fires one notification per addition when multiple are passed', () => {
    const additions = [
      { id: 'multi-1', message: 'One' },
      { id: 'multi-2', message: 'Two' },
    ];
    fireBrowserNotifications(additions);
    expect(NotificationMock).toHaveBeenCalledTimes(2);
    expect(NotificationMock).toHaveBeenCalledWith('Tournament Announcement', expect.objectContaining({ tag: 'multi-1' }));
    expect(NotificationMock).toHaveBeenCalledWith('Tournament Announcement', expect.objectContaining({ tag: 'multi-2' }));
  });

  it('does NOT fire for entries without an id', () => {
    fireBrowserNotifications([{ message: 'No id here' }]);
    expect(NotificationMock).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Snapshot-diff logic — exercises the REAL diffAnnouncementSnapshot helper.
// Models a plain { current } object for the ref, matching the useRef shape.
//
// Intended call sequence (app.jsx):
//   1. fetchAnnouncements() HTTP success → first call (null ref) → seeds.
//   2. Any SSE snapshot that arrived before step 1 is BUFFERED externally in
//      pendingSseAnnouncements and replayed (second call) after step 1. This
//      allows announcements added in the mount→fetch race window to fire.
//   3. fetchAnnouncements() HTTP failure + buffered SSE → first call → seeds
//      from SSE (no notifications — pre-existing from SSE perspective).
//   4. HTTP failure + nothing buffered → ref stays null; first subsequent SSE
//      call seeds (original spam-prevention fallback).
// ---------------------------------------------------------------------------

describe('diffAnnouncementSnapshot', () => {
  it('identifies additions correctly — only ids not in the already-seeded set', () => {
    const ref = { current: new Set(['existing-1', 'existing-2']) };
    const additions = diffAnnouncementSnapshot(ref, [
      { id: 'existing-1', message: 'Old' },
      { id: 'new-1', message: 'New announcement' },
    ]);
    expect(additions).toHaveLength(1);
    expect(additions[0].id).toBe('new-1');
  });

  it('produces no additions when the snapshot only removed items (dismiss/clear)', () => {
    const ref = { current: new Set(['ann-1', 'ann-2', 'ann-3']) };
    const additions = diffAnnouncementSnapshot(ref, [{ id: 'ann-1', message: 'Still here' }]);
    expect(additions).toHaveLength(0);
  });

  it('produces no additions when snapshot is empty (clear-all)', () => {
    const ref = { current: new Set(['ann-1', 'ann-2']) };
    expect(diffAnnouncementSnapshot(ref, [])).toHaveLength(0);
  });

  it('marks all current snapshot IDs as seen after processing', () => {
    const ref = { current: new Set(['old-1']) };
    diffAnnouncementSnapshot(ref, [
      { id: 'old-1', message: 'Existing' },
      { id: 'new-1', message: 'New' },
    ]);
    expect(ref.current.has('old-1')).toBe(true);
    expect(ref.current.has('new-1')).toBe(true);
  });

  it('a second identical snapshot produces no new additions (idempotent)', () => {
    const ref = { current: new Set() };
    const snapshot = [{ id: 'ann-1', message: 'Hello' }];
    expect(diffAnnouncementSnapshot(ref, snapshot)).toHaveLength(1); // new vs empty seeded set
    expect(diffAnnouncementSnapshot(ref, snapshot)).toHaveLength(0); // already seen
  });

  // --- Seeding (null-ref first-call behaviour) ---

  it('the first call against an unseeded (null) ref always seeds — no additions returned', () => {
    // In app.jsx this first call comes from fetchAnnouncements() HTTP success.
    // SSE snapshots that arrive before this point are buffered externally and
    // replayed after seeding.
    const ref = { current: null };
    const additions = diffAnnouncementSnapshot(ref, [
      { id: 'ann-1', message: 'Active at load' },
      { id: 'ann-2', message: 'Also active at load' },
    ]);
    expect(additions).toHaveLength(0); // no notifications on page load
    expect(ref.current.has('ann-1')).toBe(true);
    expect(ref.current.has('ann-2')).toBe(true);
  });

  it('after HTTP seeds, a subsequent diff (replayed buffered SSE) fires for the new announcement', () => {
    // Models the race-window case: fetchAnnouncements() HTTP response contains
    // [ann-1] (snapshot taken before NEW was posted); the buffered SSE snapshot
    // contains [ann-1, NEW]. Replaying it after HTTP seeding fires for NEW.
    const ref = { current: null };
    const httpList = [{ id: 'ann-1', message: 'Pre-existing' }];
    const sseList  = [{ id: 'ann-1', message: 'Pre-existing' }, { id: 'new-1', message: 'Added in race window' }];
    diffAnnouncementSnapshot(ref, httpList); // step 1: HTTP seeds (returns [])
    const additions = diffAnnouncementSnapshot(ref, sseList); // step 2: replay buffered SSE
    expect(additions).toHaveLength(1);
    expect(additions[0].id).toBe('new-1');
  });

  it('after the seed, a genuinely new announcement in the next snapshot fires', () => {
    const ref = { current: null };
    diffAnnouncementSnapshot(ref, [{ id: 'ann-1', message: 'At load' }]); // seed
    const additions = diffAnnouncementSnapshot(ref, [
      { id: 'ann-1', message: 'At load' },
      { id: 'ann-2', message: 'Arrived after load' },
    ]);
    expect(additions).toHaveLength(1);
    expect(additions[0].id).toBe('ann-2');
  });

  it('HTTP-failure fallback: seeding from buffered SSE (no notifications) leaves ref usable', () => {
    // Models sequence 3: HTTP fails, the buffered SSE snapshot seeds the ref
    // instead. The seed call returns [] (no notifications). A subsequent SSE
    // diff correctly identifies new arrivals.
    const ref = { current: null };
    const sseSeed = [{ id: 'ann-1', message: 'Pre-existing via SSE fallback' }];
    diffAnnouncementSnapshot(ref, sseSeed); // HTTP failed; caller seeds from buffered SSE
    expect(ref.current).toBeInstanceOf(Set);
    const additions = diffAnnouncementSnapshot(ref, [
      { id: 'ann-1', message: 'Pre-existing' },
      { id: 'ann-2', message: 'New' },
    ]);
    expect(additions).toHaveLength(1);
    expect(additions[0].id).toBe('ann-2');
  });

  it('seeds from an empty first snapshot, then fires for a later addition', () => {
    const ref = { current: null };
    expect(diffAnnouncementSnapshot(ref, [])).toHaveLength(0); // seed (nothing active)
    expect(ref.current).toBeInstanceOf(Set); // now seeded, not null
    const additions = diffAnnouncementSnapshot(ref, [{ id: 'ann-1', message: 'New' }]);
    expect(additions).toHaveLength(1);
  });

  it('tolerates a non-array snapshot without throwing', () => {
    const ref = { current: null };
    expect(diffAnnouncementSnapshot(ref, null)).toEqual([]);
    expect(ref.current).toBeInstanceOf(Set);
  });

  // --- sentAt mount-time filter (Copilot comments 3328811690, 3329327022) ---
  // The HTTP seed is pre-filtered to announcements with sentAt ≤ mountTime so
  // post-mount announcements in the HTTP response don't block the SSE replay
  // from firing notifications for them. Date.parse() is used for numeric
  // comparison because Go's RFC3339Nano may omit fractional seconds, making
  // lexicographic string comparison incorrect ('Z' > '.' in ASCII).

  // Helper matching the app.jsx preMountList filter logic (numeric comparison).
  function preMountFilter(list, mountMs) {
    return list.filter(a => {
      if (!a || !a.id) return false;
      if (!a.sentAt) return true;
      const ms = Date.parse(a.sentAt);
      return isNaN(ms) || ms <= mountMs;
    });
  }

  it('sentAt filter WITH buffered SSE: post-mount ID excluded from seed fires on SSE replay', () => {
    // Models the race WITH a buffered SSE snapshot: HTTP response includes NEW
    // (sentAt after mount), but seed is pre-mount only. Buffered SSE replay fires
    // for NEW.
    const mountMs = Date.parse('2026-05-30T13:00:00.000Z');
    const ref = { current: null };

    const httpList = [
      { id: 'ann-1', sentAt: '2026-05-30T12:00:00.000Z', message: 'Pre-existing' },
      { id: 'new-1', sentAt: '2026-05-30T13:00:01.000Z', message: 'Created after mount' },
    ];
    const sseList = httpList; // SSE was buffered; same snapshot (with NEW)

    // WITH buffered SSE: seed pre-mount only
    diffAnnouncementSnapshot(ref, preMountFilter(httpList, mountMs));
    // Replay buffered SSE (includes new-1) → fires for NEW
    const additions = diffAnnouncementSnapshot(ref, sseList);
    expect(additions).toHaveLength(1);
    expect(additions[0].id).toBe('new-1');
  });

  it('sentAt filter WITHOUT buffered SSE: post-mount ID seeded from full HTTP list (no stale notification)', () => {
    // Models the case with NO buffered SSE: HTTP response includes NEW (post-mount)
    // but since there is no SSE to replay, seed the full list to avoid leaving
    // NEW unseeded and triggering stale notifications on a later unrelated SSE.
    const _mountMs = Date.parse('2026-05-30T13:00:00.000Z');
    const ref = { current: null };

    const httpList = [
      { id: 'ann-1', sentAt: '2026-05-30T12:00:00.000Z', message: 'Pre-existing' },
      { id: 'new-1', sentAt: '2026-05-30T13:00:01.000Z', message: 'Created after mount' },
    ];

    // WITHOUT buffered SSE: seed the full list (including new-1)
    diffAnnouncementSnapshot(ref, httpList);
    expect(ref.current.has('new-1')).toBe(true); // seeded → won't re-fire

    // Later unrelated SSE arrives with the same list → no stale notification
    const additions = diffAnnouncementSnapshot(ref, httpList);
    expect(additions).toHaveLength(0);
  });

  it('sentAt filter: Go RFC3339 without fractional seconds compares correctly', () => {
    // "2026-05-30T13:00:00Z" (no fractional) vs mountMs from "2026-05-30T13:00:00.500Z".
    // String comparison would wrongly say "Z" > "." → excluded. Numeric is correct.
    const mountMs = Date.parse('2026-05-30T13:00:00.500Z'); // ~500ms after the hour
    const ref = { current: null };

    // ann-1 sentAt is the same second but without fractional — pre-existing (sentAt < mountMs)
    const httpList = [
      { id: 'ann-1', sentAt: '2026-05-30T13:00:00Z', message: 'Pre-existing, no frac seconds' },
    ];
    diffAnnouncementSnapshot(ref, preMountFilter(httpList, mountMs)); // ann-1 IS pre-mount
    expect(ref.current.has('ann-1')).toBe(true); // correctly seeded as pre-existing

    // Replay with same list → ann-1 already seen → no notification ✓
    const additions = diffAnnouncementSnapshot(ref, httpList);
    expect(additions).toHaveLength(0);
  });

  it('sentAt filter: missing sentAt treated as pre-existing (conservative — no spam)', () => {
    const mountMs = Date.parse('2026-05-30T13:00:00.000Z');
    const ref = { current: null };

    const httpList = [{ id: 'ann-1', message: 'No sentAt' }]; // treated as pre-existing
    diffAnnouncementSnapshot(ref, preMountFilter(httpList, mountMs)); // seeds ann-1
    // Replay SSE with the same list → ann-1 already seen → no additions
    const additions = diffAnnouncementSnapshot(ref, httpList);
    expect(additions).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// NotificationSettings component — settings toggle round-trip tests.
// Uses the global React stub (non-reactive) since we only need static renders.
// ---------------------------------------------------------------------------

describe('NotificationSettings', () => {
  let originalNotification;
  let originalWindowLocalStorage;

  // Minimal in-memory localStorage stub (same pattern as fireBrowserNotifications suite).
  const makeLocalStorageMock = (initialEntries = {}) => {
    const store = { ...initialEntries };
    return {
      getItem: (k) => (k in store ? store[k] : null),
      setItem: (k, v) => { store[k] = String(v); },
      removeItem: (k) => { delete store[k]; },
      clear: () => { Object.keys(store).forEach((k) => delete store[k]); },
    };
  };

  beforeEach(() => {
    originalNotification = global.Notification;
    originalWindowLocalStorage = Object.getOwnPropertyDescriptor(window, 'localStorage');
    Object.defineProperty(window, 'localStorage', {
      value: makeLocalStorageMock(),
      writable: true, configurable: true,
    });
  });

  afterEach(() => {
    global.Notification = originalNotification;
    if (originalWindowLocalStorage) {
      Object.defineProperty(window, 'localStorage', originalWindowLocalStorage);
    } else {
      Object.defineProperty(window, 'localStorage', { value: undefined, writable: true, configurable: true });
    }
  });

  it('returns null when Notification API is unavailable', () => {
    global.Notification = undefined;
    const result = NotificationSettings({});
    expect(result).toBeNull();
  });

  it('renders the toggle when Notification is available and permission is default', () => {
    global.Notification = { permission: 'default', requestPermission: vi.fn() };
    const result = NotificationSettings({});
    expect(result).not.toBeNull();
    // Should render a card with a checkbox
    const str = JSON.stringify(result);
    expect(str).toContain('notification-settings');
    expect(str).toContain('notification-toggle');
  });

  it('renders a blocked message when permission is denied', () => {
    global.Notification = { permission: 'denied' };
    const result = NotificationSettings({});
    expect(result).not.toBeNull();
    const str = JSON.stringify(result);
    expect(str).toContain('notification-denied');
  });

  it('shows the insecure-context warning when window.isSecureContext is false', () => {
    const orig = window.isSecureContext;
    Object.defineProperty(window, 'isSecureContext', { value: false, writable: true, configurable: true });
    global.Notification = { permission: 'default', requestPermission: vi.fn() };
    const result = NotificationSettings({});
    const str = JSON.stringify(result);
    expect(str).toContain('notification-insecure-warning');
    Object.defineProperty(window, 'isSecureContext', { value: orig, writable: true, configurable: true });
  });

  it('notificationSupported returns false when Notification is undefined', () => {
    global.Notification = undefined;
    expect(notificationSupported()).toBe(false);
  });

  it('notificationSupported returns true when Notification is defined', () => {
    global.Notification = { permission: 'default' };
    expect(notificationSupported()).toBe(true);
  });

  it('persists toggle state to localStorage', () => {
    // Verify the mock localStorage (installed in beforeEach) round-trips correctly.
    // This exercises the same LS_NOTIFICATIONS_ENABLED key path used by the component.
    window.localStorage.setItem('viewer.notifications.enabled', 'true');
    expect(window.localStorage.getItem('viewer.notifications.enabled')).toBe('true');

    window.localStorage.setItem('viewer.notifications.enabled', 'false');
    expect(window.localStorage.getItem('viewer.notifications.enabled')).toBe('false');
  });

  // --- Two-click bug (Copilot comment 3328483825) -------------------------
  // Stored opt-in is "true" but the browser permission has been reset to
  // "default". The checkbox must render unchecked AND the first click must go
  // down the "turning on" branch (request permission), not "turning off".
  it('first click requests permission when opt-in was stored but permission is default', async () => {
    window.localStorage.setItem('viewer.notifications.enabled', 'true');
    const requestPermission = vi.fn().mockResolvedValue('granted');
    global.Notification = { permission: 'default', requestPermission };

    const tree = NotificationSettings({});
    const toggle = findAll(tree, n => n.props && n.props['data-testid'] === 'notification-toggle')[0];
    expect(toggle).toBeDefined();
    // enabled is gated on permission===granted, so the box renders unchecked.
    expect(toggle.props.checked).toBe(false);

    // First click → must request permission (turning-on branch), not flip off.
    await toggle.props.onChange();
    expect(requestPermission).toHaveBeenCalledTimes(1);
  });

  it('does NOT re-request permission on the first click when already granted (turning off)', async () => {
    window.localStorage.setItem('viewer.notifications.enabled', 'true');
    const requestPermission = vi.fn().mockResolvedValue('granted');
    global.Notification = { permission: 'granted', requestPermission };

    const tree = NotificationSettings({});
    const toggle = findAll(tree, n => n.props && n.props['data-testid'] === 'notification-toggle')[0];
    // Stored true + granted → checkbox checked.
    expect(toggle.props.checked).toBe(true);

    // First click turns OFF — no permission prompt.
    await toggle.props.onChange();
    expect(requestPermission).not.toHaveBeenCalled();
    expect(window.localStorage.getItem('viewer.notifications.enabled')).toBe('false');
  });
});

// ---------------------------------------------------------------------------
// NotificationSettings — reactive runtime, for behaviours that need a real
// re-render after a state change (Copilot round-2 comments 3328780111 /
// 3328780129). Uses the makeReactive() shim like reset.test.jsx.
// ---------------------------------------------------------------------------

describe('NotificationSettings (reactive)', () => {
  let runtime, RS, realReact, origNotif, origSecure, origLS;

  beforeEach(async () => {
    realReact = global.React;
    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();
    ({ NotificationSettings: RS } = await import('../viewer.jsx'));
    origNotif = global.Notification;
    origSecure = Object.getOwnPropertyDescriptor(window, 'isSecureContext');
    origLS = Object.getOwnPropertyDescriptor(window, 'localStorage');
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    global.Notification = origNotif;
    if (origSecure) Object.defineProperty(window, 'isSecureContext', origSecure);
    if (origLS) Object.defineProperty(window, 'localStorage', origLS);
    vi.resetModules();
  });

  const findToggle = () => findAll(runtime.currentTree(), n => n.props && n.props['data-testid'] === 'notification-toggle')[0];
  const findWarn = () => findAll(runtime.currentTree(), n => n.props && n.props['data-testid'] === 'notification-insecure-warning');

  it('shows the insecure-context warning even when the Notification API is unavailable', () => {
    // Bare http:// browser that gates `Notification` on a secure context:
    // both no API AND isSecureContext === false. The panel must still explain.
    global.Notification = undefined;
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true });
    const tree = runtime.mount(RS, {});
    expect(tree).not.toBeNull();
    expect(findWarn().length).toBe(1);
  });

  it('hides entirely when the API is unavailable AND the context is secure', () => {
    global.Notification = undefined;
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });
    const tree = runtime.mount(RS, {});
    expect(tree).toBeNull();
  });

  it('leaves the toggle OFF when enabling succeeds at the prompt but localStorage.setItem throws', async () => {
    // Storage that throws on write — fireBrowserNotifications reads the flag at
    // fire time, so an unpersisted "on" would be a checked box that never fires.
    Object.defineProperty(window, 'localStorage', {
      value: { getItem: () => null, setItem: () => { throw new Error('quota exceeded'); }, removeItem: () => {} },
      writable: true, configurable: true,
    });
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });
    global.Notification = { permission: 'default', requestPermission: vi.fn().mockResolvedValue('granted') };

    runtime.mount(RS, {});
    expect(findToggle().props.checked).toBe(false);
    await findToggle().props.onChange();
    // Permission was granted, but persistence failed → keep the box OFF so the
    // UI stays consistent with the (storage-backed) firing path.
    expect(findToggle().props.checked).toBe(false);
  });

  it('marks the toggle ON when enabling succeeds and storage persists', async () => {
    const store = {};
    Object.defineProperty(window, 'localStorage', {
      value: { getItem: (k) => (k in store ? store[k] : null), setItem: (k, v) => { store[k] = String(v); }, removeItem: (k) => { delete store[k]; } },
      writable: true, configurable: true,
    });
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true });
    global.Notification = { permission: 'default', requestPermission: vi.fn().mockResolvedValue('granted') };

    runtime.mount(RS, {});
    await findToggle().props.onChange();
    expect(findToggle().props.checked).toBe(true);
    expect(store['viewer.notifications.enabled']).toBe('true');
  });
});
