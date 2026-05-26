import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { isAnnouncementActive, filterActiveAnnouncements } from '../app.jsx';

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
