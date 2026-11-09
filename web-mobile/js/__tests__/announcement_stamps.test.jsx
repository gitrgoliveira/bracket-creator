import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

// The server requires an id and a made-at on a court call, and a made-at on a
// clear (modifiedAt, in the server's clock frame). Without them the doors
// answer 400.
describe('announcement doors carry what the server requires', () => {
  const realFetch = global.fetch;
  afterEach(() => { global.fetch = realFetch; });

  const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  const ok = (body) => ({ ok: true, status: 200, json: async () => body });

  it('sendAnnouncement sends a lowercase UUID v4 id and a positive modifiedAt', async () => {
    global.fetch = vi.fn().mockResolvedValue(ok({ id: 'x', message: 'Hi' }));
    await API.sendAnnouncement('Hi', 5, 'pw');
    const [url, init] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/tournament/announce');
    const body = JSON.parse(init.body);
    expect(body.id).toMatch(UUID_V4);
    expect(body.modifiedAt).toBeGreaterThan(0);
    expect(body).toMatchObject({ message: 'Hi', durationMinutes: 5 });
  });

  it('sendAnnouncement mints a new id for each call', async () => {
    global.fetch = vi.fn().mockResolvedValue(ok({}));
    await API.sendAnnouncement('Hi', 5, 'pw');
    await API.sendAnnouncement('Hi', 5, 'pw');
    const ids = global.fetch.mock.calls.map(([, init]) => JSON.parse(init.body).id);
    expect(ids[0]).not.toBe(ids[1]);
  });

  it('sendAnnouncement throws the server sentence when the clock is refused', async () => {
    global.fetch = vi.fn().mockResolvedValue(ok({ applied: false, reason: 'clock_skew', message: 'Nothing was changed: clock' }));
    await expect(API.sendAnnouncement('Hi', 5, 'pw')).rejects.toThrow('Nothing was changed: clock');
  });

  it('clearAnnouncements sends a JSON body with a positive modifiedAt', async () => {
    global.fetch = vi.fn().mockResolvedValue(ok([]));
    await API.clearAnnouncements('pw');
    const [url, init] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/announcements');
    expect(init.method).toBe('DELETE');
    expect(init.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(init.body).modifiedAt).toBeGreaterThan(0);
  });

  it('clearAnnouncements throws the server sentence when the clock is refused', async () => {
    global.fetch = vi.fn().mockResolvedValue(ok({ applied: false, reason: 'clock_skew', message: 'Nothing was changed: clock' }));
    await expect(API.clearAnnouncements('pw')).rejects.toThrow('Nothing was changed: clock');
  });
});
