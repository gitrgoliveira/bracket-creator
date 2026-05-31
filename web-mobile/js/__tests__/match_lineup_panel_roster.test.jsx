// mp-bkg regression guard: MatchLineupPanel must resolve a team's roster
// from comp.players even when the match side is keyed by team NAME while
// the participant record is keyed by UUID.
//
// Root cause (found via browser verification, not unit tests):
//   - comp.players (loaded via /api/viewer/competitions) ALREADY carries
//     each team's metadata roster, with id = a UUID.
//   - The match's sideA/sideB normalize to { id, name } where `id` falls
//     back to the team NAME when the backend has no UUID for that slot
//     (api_serializers.normalizeMatch, playerMap empty).
//   - The old resolver did `players.find(p => (p.id || p.name) === sideId)`.
//     Because p.id is a truthy UUID, `||` short-circuited and compared the
//     UUID against the name-keyed sideId — NEVER equal. So the roster
//     silently failed to resolve and every dropdown showed "No roster found".
//
// The fix matches on EITHER id OR name. This test mirrors the real data
// shape (UUID participant id + name-keyed match side) that the previous
// per-mount-hydration tests bypassed by injecting matching ids.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

const realReact = global.React;

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

// Find component nodes (where type is a function) by name. Does NOT call them.
function findComponents(node, namePredicate) {
  if (!node || typeof node !== 'object') return [];
  const found = [];
  if (typeof node.type === 'function' && namePredicate(node.type.name || '')) {
    found.push(node);
  }
  const kids = [].concat(node.children || node.props?.children || []);
  for (const k of kids) {
    found.push(...findComponents(k, namePredicate));
  }
  return found;
}

describe('MatchLineupPanel roster resolution (mp-bkg)', () => {
  let runtime;
  let MatchLineupPanel;
  let origAPI;
  let origAdminLineupHelpers;
  let origCompMatches;

  const COMP_ID = 'comp-team-1';

  // Teams as they appear in comp.players (camelCase, UUID ids, with roster).
  const PLAYERS = [
    { id: 'uuid-aaaa-1111', name: 'Red Dojo', dojo: 'Red Dojo', metadata: ['Aka Ichi', 'Aka Ni', 'Aka San', 'Aka Shi', 'Aka Go'] },
    { id: 'uuid-bbbb-2222', name: 'Blue Dojo', dojo: 'Blue Dojo', metadata: ['Shiro Ichi', 'Shiro Ni', 'Shiro San', 'Shiro Shi', 'Shiro Go'] },
  ];

  // The match side is keyed by NAME (id === name) — the real normalized
  // shape when api_serializers has no UUID for the slot. THIS is what broke
  // the old `(p.id || p.name) === sideId` resolver.
  const MATCH = {
    id: 'match-1',
    compId: COMP_ID,
    sideA: { id: 'Red Dojo', name: 'Red Dojo' },
    sideB: { id: 'Blue Dojo', name: 'Blue Dojo' },
    round: 'Round 1',
    status: 'scheduled',
  };

  const TOURNAMENT = {
    competitions: [
      { id: COMP_ID, name: 'Team Event', kind: 'team', teamSize: 5, players: PLAYERS },
    ],
  };

  beforeEach(async () => {
    origAPI = global.window.API;
    origAdminLineupHelpers = global.window.AdminLineupHelpers;
    origCompMatches = global.window.compMatches;

    global.window.AdminLineupHelpers = {
      positionsForSize: (n) => Array.from({ length: n }, (_, i) => ({
        key: ['senpo', 'jiho', 'chuken', 'fukusho', 'taisho'][i] || String(i + 1),
        label: ['Senpo', 'Jiho', 'Chuken', 'Fukusho', 'Taisho'][i] || String(i + 1),
      })),
      rosterFor: (team) => {
        if (!team) return [];
        if (Array.isArray(team.metadata) && team.metadata.length > 0) return team.metadata;
        if (Array.isArray(team.Metadata) && team.Metadata.length > 0) return team.Metadata;
        return [];
      },
      teamIdOf: (team) => team?.id || team?.ID || team?.name || team?.Name || '',
      canRevise: () => false,
    };

    global.window.compMatches = () => [];

    // MatchLineupSideEditor fetches its saved lineup on mount — stub to null.
    global.window.API = {
      fetchMatchLineup: vi.fn().mockResolvedValue(null),
      fetchTeamLineup: vi.fn().mockResolvedValue(null),
    };

    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();
    ({ MatchLineupPanel } = await import('../admin_schedule.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    global.window.API = origAPI;
    global.window.AdminLineupHelpers = origAdminLineupHelpers;
    global.window.compMatches = origCompMatches;
    vi.resetModules();
  });

  it('REGRESSION: name-keyed match side resolves to the UUID-keyed participant roster', async () => {
    runtime.mount(MatchLineupPanel, {
      match: MATCH,
      tournament: TOURNAMENT,
      password: 'pw',
      showToast: vi.fn(),
      onClose: vi.fn(),
    });
    // Let MatchLineupSideEditor's mount effects settle.
    await Promise.resolve();
    await Promise.resolve();

    const tree = runtime.currentTree();
    const allText = collectText(tree);

    // The pre-fix failure surfaced as "No roster found" / "Team not found".
    expect(allText).not.toContain('No roster found');
    expect(allText).not.toContain('Team not found in roster');

    // Both side editors must render and receive a team prop carrying the roster.
    const sideEditors = findComponents(tree, n => n === 'MatchLineupSideEditor');
    expect(sideEditors.length).toBe(2);
    for (const node of sideEditors) {
      const team = node.props?.team;
      expect(team).toBeTruthy();
      expect((team.metadata || team.Metadata || []).length).toBeGreaterThan(0);
    }
  });

  it('does not render side editors for a non-team competition', async () => {
    const nonTeam = {
      competitions: [
        { id: COMP_ID, name: 'Individuals', kind: 'individual', teamSize: 0, players: PLAYERS },
      ],
    };
    runtime.mount(MatchLineupPanel, {
      match: MATCH,
      tournament: nonTeam,
      password: 'pw',
      showToast: vi.fn(),
      onClose: vi.fn(),
    });
    await Promise.resolve();
    const tree = runtime.currentTree();
    // isTeamComp === false → the panel renders null.
    const sideEditors = findComponents(tree, n => n === 'MatchLineupSideEditor');
    expect(sideEditors.length).toBe(0);
  });

  it('falls back to the match-side object when no participant matches', async () => {
    const matchUnknown = {
      ...MATCH,
      sideA: { id: 'Ghost Dojo', name: 'Ghost Dojo' },
      sideB: { id: 'Blue Dojo', name: 'Blue Dojo' },
    };
    runtime.mount(MatchLineupPanel, {
      match: matchUnknown,
      tournament: TOURNAMENT,
      password: 'pw',
      showToast: vi.fn(),
      onClose: vi.fn(),
    });
    await Promise.resolve();
    await Promise.resolve();

    const tree = runtime.currentTree();
    const sideEditors = findComponents(tree, n => n === 'MatchLineupSideEditor');
    // sideA falls back to the bare {id,name} object (no roster) — still a
    // side editor node, but its team carries no metadata; sideB resolves.
    const teams = sideEditors.map(n => n.props?.team).filter(Boolean);
    const blue = teams.find(t => (t.name || t.Name) === 'Blue Dojo');
    expect(blue).toBeTruthy();
    expect((blue.metadata || []).length).toBeGreaterThan(0);
  });
});
