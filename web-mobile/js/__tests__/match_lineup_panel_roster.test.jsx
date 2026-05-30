// mp-bkg regression guard: MatchLineupPanel must hydrate team rosters
// from the /participants endpoint when comp.players is empty.
//
// Root cause: the competition list endpoint (/api/viewer/competitions)
// returns players: [] to keep the payload small. MatchLineupPanel was
// resolving team objects from comp.players, so the roster dropdowns
// were always empty ("— Select —" only) in the score-editor data path.
//
// This test exercises the REAL data path:
//   - comp.players = [] (as returned by the competition list endpoint)
//   - window.API.listParticipants returns teams WITH metadata
//   - After the mount effect resolves, side editors must render roster
//     options (non-empty select).
//
// The test uses makeReactive() to run useState/useEffect for real so the
// async hydration effect is exercised rather than mocked away.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

const realReact = global.React;

// Walk the render tree and collect all nodes matching predicate.
// Does NOT recurse into component nodes (functions) — only host elements.
function findAll(node, predicate) {
  if (!node || typeof node !== 'object') return [];
  const found = [];
  if (predicate(node)) found.push(node);
  const kids = [].concat(node.children || node.props?.children || []);
  for (const k of kids) {
    found.push(...findAll(k, predicate));
  }
  return found;
}

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

// Find component nodes (where type is a function) by display name or identity.
function findComponents(node, namePredicate) {
  if (!node || typeof node !== 'object') return [];
  const found = [];
  if (typeof node.type === 'function' && namePredicate(node.type.name || '')) {
    found.push(node);
  }
  // Walk into props.children even for component nodes (but don't call them).
  const kids = [].concat(node.children || node.props?.children || []);
  for (const k of kids) {
    found.push(...findComponents(k, namePredicate));
  }
  return found;
}

describe('MatchLineupPanel roster hydration (mp-bkg)', () => {
  let runtime;
  let MatchLineupPanel;
  let origAPI;
  let origAdminLineupHelpers;
  let origCompMatches;
  let origNormalizePlayer;

  const COMP_ID = 'comp-team-1';

  // Two teams with member rosters, as returned by GET /participants.
  // Server returns PascalCase; listParticipants returns raw server JSON.
  const PARTICIPANTS_RAW = [
    { ID: 'team-a', Name: 'Red Dojo', Dojo: 'Red Dojo', Metadata: ['Aka Ichi', 'Aka Ni', 'Aka San', 'Aka Shi', 'Aka Go'] },
    { ID: 'team-b', Name: 'Blue Dojo', Dojo: 'Blue Dojo', Metadata: ['Shiro Ichi', 'Shiro Ni', 'Shiro San', 'Shiro Shi', 'Shiro Go'] },
  ];

  const MATCH = {
    id: 'match-1',
    compId: COMP_ID,
    sideA: { id: 'team-a', name: 'Red Dojo' },
    sideB: { id: 'team-b', name: 'Blue Dojo' },
    round: 'Round 1',
    status: 'scheduled',
  };

  // comp.players = [] simulates the score-editor data path where the
  // competition list endpoint omits the roster.
  const TOURNAMENT = {
    competitions: [
      {
        id: COMP_ID,
        name: 'Team Event',
        kind: 'team',
        teamSize: 5,
        players: [],  // THIS IS THE BUG: competition list returns empty players
      },
    ],
  };

  beforeEach(async () => {
    origAPI = global.window.API;
    origAdminLineupHelpers = global.window.AdminLineupHelpers;
    origCompMatches = global.window.compMatches;
    origNormalizePlayer = global.window.normalizePlayer;

    // Stub normalizePlayer: convert PascalCase to camelCase for the fields
    // we care about (name, metadata). Mirrors api_serializers.jsx.
    global.window.normalizePlayer = (p) => {
      if (!p) return p;
      if (p.name !== undefined) return p; // already camelCase
      return {
        id: p.ID || p.id || '',
        name: p.Name || '',
        dojo: p.Dojo || '',
        metadata: p.Metadata || [],
        danGrade: (p.Metadata && p.Metadata[0]) || '',
        seed: p.Seed || 0,
        tag: p.Tag || '',
        displayName: p.DisplayName || '',
        number: p.Number || '',
      };
    };

    // Stub AdminLineupHelpers so MatchLineupSideEditor can resolve roster.
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

    // The key stub: listParticipants returns teams with rosters.
    global.window.API = {
      listParticipants: vi.fn().mockResolvedValue(PARTICIPANTS_RAW),
      // MatchLineupSideEditor calls these — stub to avoid unhandled rejections.
      fetchMatchLineup: vi.fn().mockResolvedValue(null),
      fetchTeamLineup: vi.fn().mockResolvedValue(null),
    };

    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();

    // Import AFTER setting up React and window globals.
    // Also import api_serializers so window.normalizePlayer is populated
    // from the module itself (our stub above is for safety).
    ({ MatchLineupPanel } = await import('../admin_schedule.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    global.window.API = origAPI;
    global.window.AdminLineupHelpers = origAdminLineupHelpers;
    global.window.compMatches = origCompMatches;
    global.window.normalizePlayer = origNormalizePlayer;
    vi.resetModules();
  });

  it('calls listParticipants when comp.players is empty', async () => {
    runtime.mount(MatchLineupPanel, {
      match: MATCH,
      tournament: TOURNAMENT,
      password: 'pw',
      showToast: vi.fn(),
      onClose: vi.fn(),
    });

    // Let the async useEffect IIFE resolve.
    await Promise.resolve();
    await Promise.resolve();

    expect(global.window.API.listParticipants).toHaveBeenCalledWith(COMP_ID, 'pw');
  });

  it('hydrates team metadata from participants so roster dropdowns are non-empty', async () => {
    runtime.mount(MatchLineupPanel, {
      match: MATCH,
      tournament: TOURNAMENT,
      password: 'pw',
      showToast: vi.fn(),
      onClose: vi.fn(),
    });

    // Flush the async listParticipants fetch.
    await Promise.resolve();
    await Promise.resolve();

    const tree = runtime.currentTree();

    // After hydration the "No roster found" message must NOT appear in the
    // host-element tree (it is inside the MatchLineupSideEditor fallback).
    const allText = collectText(tree);
    expect(allText).not.toContain('No roster found');

    // MatchLineupSideEditor nodes are rendered as component nodes
    // (not called recursively by createElement). Verify the panel DID render
    // side editors (not just the "Team not found in roster." fallback divs),
    // and that the team props passed to them carry metadata.
    const sideEditorNodes = findComponents(tree, n => n === 'MatchLineupSideEditor');
    expect(sideEditorNodes.length).toBeGreaterThan(0);

    // Each side editor must receive a team prop that has metadata.
    for (const node of sideEditorNodes) {
      const team = node.props?.team;
      expect(team).toBeTruthy();
      const roster = (team.metadata || team.Metadata || []);
      expect(roster.length).toBeGreaterThan(0);
    }
  });

  it('shows a loading indicator (null hydrated state) before the fetch resolves', () => {
    // Replace listParticipants with a never-resolving promise to freeze
    // the component in the loading state.
    global.window.API.listParticipants = vi.fn(() => new Promise(() => {}));

    const tree = runtime.mount(MatchLineupPanel, {
      match: MATCH,
      tournament: TOURNAMENT,
      password: 'pw',
      showToast: vi.fn(),
      onClose: vi.fn(),
    });

    // While loading: "Loading roster…" should appear.
    const text = collectText(tree);
    expect(text).toContain('Loading roster');
  });

  it('does NOT call listParticipants for non-team competitions', async () => {
    const nonTeamTournament = {
      competitions: [
        { id: COMP_ID, name: 'Individuals', kind: 'individual', teamSize: 0, players: [] },
      ],
    };

    runtime.mount(MatchLineupPanel, {
      match: { ...MATCH, compId: COMP_ID },
      tournament: nonTeamTournament,
      password: 'pw',
      showToast: vi.fn(),
      onClose: vi.fn(),
    });

    await Promise.resolve();
    await Promise.resolve();

    // Non-team comps return null (the component renders nothing), so
    // listParticipants should not have been called.
    expect(global.window.API.listParticipants).not.toHaveBeenCalled();
  });

  it('REGRESSION: comp.players=[] + listParticipants with metadata → roster non-empty', async () => {
    // This is the load-bearing test that exercises the exact bug path:
    //   - competition list returns players: [] (triggering the bug)
    //   - participants endpoint returns teams with Metadata
    //   - panel must resolve the roster from the participants response
    //
    // The previous code read comp.players only, so roster was always [].
    // The fix fetches from listParticipants and normalises the response.
    expect(TOURNAMENT.competitions[0].players).toHaveLength(0); // confirms empty comp.players

    runtime.mount(MatchLineupPanel, {
      match: MATCH,
      tournament: TOURNAMENT,
      password: 'pw',
      showToast: vi.fn(),
      onClose: vi.fn(),
    });

    // Before fetch resolves: loading state.
    const treeBefore = runtime.currentTree();
    expect(collectText(treeBefore)).toContain('Loading roster');

    // Resolve the participants fetch.
    await Promise.resolve();
    await Promise.resolve();

    const treeAfter = runtime.currentTree();
    const allText = collectText(treeAfter);

    // The panel must not show the "No roster found" message (which
    // appeared when roster was [] — the pre-fix behaviour).
    expect(allText).not.toContain('No roster found');

    // MatchLineupSideEditor nodes must be rendered (not the fallback
    // "Team not found in roster." divs) and each must carry metadata.
    const sideEditorNodes = findComponents(treeAfter, n => n === 'MatchLineupSideEditor');
    expect(sideEditorNodes.length).toBeGreaterThan(0);

    for (const node of sideEditorNodes) {
      const team = node.props?.team;
      expect(team).toBeTruthy();
      // The fix: team must now have metadata from the participants fetch.
      const roster = (team.metadata || team.Metadata || []);
      expect(roster.length).toBeGreaterThan(0);
    }
  });
});
