import { describe, it, expect } from 'vitest';
import { isAnnouncementActive } from '../app.jsx';

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
