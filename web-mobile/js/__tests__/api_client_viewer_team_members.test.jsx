// bc-dnst: the viewer payload's team-member key was renamed squads ->
// teamMembers on the wire, while the CLIENT property stays `squads` (a code
// name, per the operator's vocabulary ruling). normalizeViewerCompItem is the
// one place that bridges the two, and it had no test at all.
//
// The seam is invisible to every other suite: the render tests build their
// fixtures with the already-normalized client key, so reverting the bridge to
// `item.squads` left the whole JS suite and the whole Go suite green while the
// court display, the streaming overlay and the court feed silently lost every
// team's members. Bout rows then fall back to "#N" and to frozen pre-rename
// names, which is the exact failure resolveBoutSideDisplayName exists to fix.
//
// Pinned through the public fetchers rather than the unexported normalizer, so
// the test cannot drift from what a caller actually receives.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../api_client.jsx';

const MEMBERS = { 'team-1': [{ id: 'm1', index: 1, name: 'Sato' }] };

function jsonOnce(body) {
  return vi.fn(() => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) }));
}

// One competition item in the shape the aggregate and the court feed send.
const item = (extra) => ({
  config: { id: 'c1', name: 'Team Cup', kind: 'team', teamSize: 3, players: [] },
  poolMatches: [],
  bracket: null,
  ...extra,
});

describe('the viewer payload carries team members under teamMembers', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('maps the WIRE key teamMembers onto the client property squads', async () => {
    global.fetch = jsonOnce([item({ teamMembers: MEMBERS })]);
    const comps = await API.fetchCompetitions();
    expect(comps).toHaveLength(1);
    expect(comps[0].squads).toEqual(MEMBERS);
  });

  it('does the same for the court feed, the operator console\'s own source', async () => {
    global.fetch = jsonOnce({ competitions: [item({ teamMembers: MEMBERS })] });
    const comps = await API.fetchCourtMatches('A');
    expect(comps).toHaveLength(1);
    expect(comps[0].squads).toEqual(MEMBERS);
  });

  it('reads NOTHING from a payload still using the old wire key', async () => {
    // The rename shipped with no compatibility shim, deliberately: client and
    // server ride in one binary. This asserts that on purpose, so the absence
    // of a shim is a decision on the record rather than an untested gap.
    global.fetch = jsonOnce([item({ squads: MEMBERS })]);
    const comps = await API.fetchCompetitions();
    expect(comps[0].squads).toBeUndefined();
  });
});
