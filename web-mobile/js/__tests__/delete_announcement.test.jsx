import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

// Taking an announcement down is asked for its OUTCOME: the banner is gone. A
// 404 says it already is (it expired, another console withdrew it, or the
// identical call that replaced it was withdrawn first), so it is not a failure
// for either caller: the court console's withdrawal of its own call, and the
// organiser's Dismiss, which refreshes the list after it.
describe('deleteAnnouncement', () => {
  const realFetch = global.fetch;
  afterEach(() => { global.fetch = realFetch; });

  it('resolves when the announcement is already gone (404)', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 404, json: async () => ({ error: 'Announcement not found' }) });
    await expect(API.deleteAnnouncement('gone', 'pw')).resolves.toBeUndefined();
  });

  it('still throws on any other failure', async () => {
    global.fetch = vi.fn().mockResolvedValue({ ok: false, status: 401, json: async () => ({ error: 'Invalid password' }) });
    await expect(API.deleteAnnouncement('x', 'bad')).rejects.toThrow('Invalid password');
  });
});
