// normalizeViewerCompItem (api_client.jsx) hoists rosterAvailable from the
// aggregate item onto the flattened competition, exactly like dataIssues,
// "or it is silently dropped" (its own comment). rosterFullyLoaded
// (viewer_watchlist_core.jsx) then reads an ABSENT flag as loaded, so an
// older payload never goes quieter than it already was.
//
// viewer_watchlist_unified.test.jsx already pins rosterFullyLoaded's own
// true/false/absent logic against hand-built objects, but never touches
// api_client.jsx. This file is the one that goes through the real
// fetch -> normalizeViewerCompItem hoist, so a dropped hoist line still
// reddens something even though that other suite stays green (verified by
// mutation: deleting the hoist line failed test (a) here while leaving
// viewer_watchlist_unified.test.jsx untouched). If the hoist breaks, the
// watchlist's amber "not in the roster" bug returns.
//
// Pinned through the public fetcher rather than the unexported normalizer,
// so the test cannot drift from what a caller actually receives.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';
import { rosterFullyLoaded } from '../viewer_watchlist_core.jsx';

function jsonOnce(body) {
  return vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) }));
}

// One competition item in the shape the aggregate sends.
const item = (extra) => ({
  config: { id: 'c1', name: 'Autumn Cup', kind: 'individual', players: [] },
  poolMatches: [],
  bracket: null,
  ...extra,
});

describe('the viewer payload carries rosterAvailable through to rosterFullyLoaded', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('hoists an explicit rosterAvailable: false and reads it as not fully loaded', async () => {
    global.fetch = jsonOnce([item({ rosterAvailable: false })]);
    const comps = await API.fetchCompetitions();
    expect(comps).toHaveLength(1);
    expect(comps[0].rosterAvailable).toBe(false);
    expect(rosterFullyLoaded(comps)).toBe(false);
  });

  it('treats an absent rosterAvailable key, an older payload, as fully loaded', async () => {
    global.fetch = jsonOnce([item()]);
    const comps = await API.fetchCompetitions();
    expect(comps).toHaveLength(1);
    expect(comps[0].rosterAvailable).toBeUndefined();
    expect(rosterFullyLoaded(comps)).toBe(true);
  });
});
