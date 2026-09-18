// bc-dnst: useTeamLineups (match_scoreboard.jsx) resolves squadA/squadB
// from the passed competition's squads map inside an effect. A member
// renamed while the board is up arrives as a fresher competition item, and
// the resolved squads must follow it, or every bout row keeps the spelling
// the first run captured (seen on the TV board in a browser: a forced
// re-run showed the new name, a fresh load did not). Pins that a changed
// squads map re-runs the effect, keyed on the map's content.
import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let useTeamLineups;
let origAPI;

const COMP = (squadName) => ({
  id: 'comp1',
  players: [{ id: 'team-a', name: 'Team A' }, { id: 'team-b', name: 'Team B' }],
  squads: { 'team-a': [{ id: 'm-a1', index: 1, name: squadName }], 'team-b': [] },
});
const MATCH = { id: 'Pool A-0', compId: 'comp1', sideA: { id: 'team-a', name: 'Team A' }, sideB: { id: 'team-b', name: 'Team B' } };

let latest;
function Probe({ competition }) {
  latest = useTeamLineups(MATCH, competition, 0);
  return null;
}

beforeEach(async () => {
  origAPI = window.API;
  window.API = {
    fetchMatchLineup: vi.fn().mockResolvedValue(null),
    fetchTeamLineup: vi.fn().mockResolvedValue(null),
    fetchCompetitionDetails: vi.fn().mockResolvedValue({}),
  };
  ({ useTeamLineups } = await import('../../match_scoreboard.jsx'));
});
afterEach(() => { window.API = origAPI; });

describe('useTeamLineups follows a changed squads map (bc-dnst)', () => {
  it('re-resolves squadA when the competition item arrives with a renamed member', async () => {
    let utils;
    await act(async () => { utils = render(<Probe competition={COMP('Satoh')} />); });
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(latest.squadA.map(m => m.name)).toEqual(['Satoh']);

    await act(async () => { utils.rerender(<Probe competition={COMP('Sato')} />); });
    await act(async () => { await Promise.resolve(); await Promise.resolve(); });
    expect(latest.squadA.map(m => m.name)).toEqual(['Sato']);
  });
});
