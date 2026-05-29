import { describe, it, expect } from 'vitest';
import { isSendAnnouncementDisabled, sendAnnouncementLabel } from '../admin_announcement.jsx';

// Pure helpers for the announcement-broadcast button. Moved here from
// admin_setup.test.jsx alongside the AnnouncementComposer extraction (mp-djc).

describe('isSendAnnouncementDisabled', () => {
  it('disabled when message is empty', () => {
    expect(isSendAnnouncementDisabled('', false)).toBe(true);
  });

  it('disabled when message is whitespace-only (trimming guard)', () => {
    expect(isSendAnnouncementDisabled('   ', false)).toBe(true);
  });

  it('disabled when in-flight (prevents double-send)', () => {
    expect(isSendAnnouncementDisabled('Hello', true)).toBe(true);
  });

  it('enabled when message is non-empty and not in-flight', () => {
    expect(isSendAnnouncementDisabled('Hello', false)).toBe(false);
  });
});

describe('sendAnnouncementLabel', () => {
  it('returns "Send announcement" when idle', () => {
    expect(sendAnnouncementLabel(false)).toBe('Send announcement');
  });

  it('returns "Sending..." during in-flight request', () => {
    expect(sendAnnouncementLabel(true)).toBe('Sending...');
  });
});
