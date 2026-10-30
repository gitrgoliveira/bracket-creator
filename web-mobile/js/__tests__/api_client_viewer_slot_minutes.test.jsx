// normalizeViewerCompItem (api_client.jsx) hoists the server's slotMinutes and
// knockoutSlotMinutes, siblings of config on the wire, onto the flattened
// competition. Dropped there, courtPaceLookup finds no slot and projects no
// time anywhere. Pinned through the public fetcher and the real lookup.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';
import { courtPaceLookup, paceKey } from '../court_pace.jsx';

function jsonOnce(body) {
  return vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) }));
}

describe('the viewer payload carries the slot minutes through to courtPaceLookup', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('hoists slotMinutes and knockoutSlotMinutes and projects a waiting match', async () => {
    global.fetch = jsonOnce([{
      config: { id: 'c1', name: 'Autumn Cup', kind: 'individual', date: '29-10-2026', players: [] },
      slotMinutes: 5,
      knockoutSlotMinutes: 7,
      poolMatches: [
        { id: 'p1', court: 'A', status: 'running', scheduledAt: '10:00' },
        { id: 'p2', court: 'A', status: 'scheduled', scheduledAt: '10:05' },
      ],
      bracket: null,
    }]);
    const comps = await API.fetchCompetitions();
    expect(comps[0].slotMinutes).toBe(5);
    expect(comps[0].knockoutSlotMinutes).toBe(7);
    const lookup = courtPaceLookup(comps, { now: 10 * 60 + 8, today: '29-10-2026' });
    expect(lookup.byMatch.has(paceKey('c1', 'p2'))).toBe(true);
  });
});
