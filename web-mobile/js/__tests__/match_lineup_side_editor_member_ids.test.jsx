// bc-pnum gap closure: MatchLineupSideEditor (admin_schedule_lineup.jsx,
// the match-scoped "Lineup for this match" panel reached from the scoring
// modal) resolves each occupied position's name to a squad member id
// before writing, via the shared resolveMemberIdsForPositions helper
// (admin_lineup.jsx, reached through window.AdminLineupHelpers). This
// mirrors match_lineup_side_editor_trim.test.jsx's mount setup.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
// bc-cse: the REAL composer, not a stub, so these tests exercise the exact
// wording the operator sees (mirrors admin_lineup.jsx's own window bridge).
import { memberIdentityWarning } from '../admin_lineup.jsx';

const realReact = global.React;

function childrenOf(node) {
  const c = node.children;
  if (c !== undefined && c !== null && !(Array.isArray(c) && c.length === 0)) return c;
  return node.props?.children;
}
function walk(node, visit) {
  if (node == null || node === false || node === true) return;
  if (Array.isArray(node)) { for (const n of node) walk(n, visit); return; }
  if (typeof node !== 'object') return;
  visit(node);
  walk(childrenOf(node), visit);
}
function findHosts(tree, typeName) {
  const out = [];
  walk(tree, n => { if (n && n.type === typeName) out.push(n); });
  return out;
}
function findComponents(tree, name) {
  const out = [];
  walk(tree, n => { if (n && typeof n.type === 'function' && n.type.name === name) out.push(n); });
  return out;
}
function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}
const saveButton = (tree) =>
  findHosts(tree, 'button').find(b => /Save lineup/.test(collectText(b)));
const memberWarning = (tree) =>
  findHosts(tree, 'div').find(d => d.props?.['data-testid'] === 'match-lineup-warning-uuid-grouped');
const errorBanner = (tree) =>
  findHosts(tree, 'div').find(d => collectText(d) && /Failed to (save|load) lineup/.test(collectText(d)));

describe('MatchLineupSideEditor resolves names to squad member ids (bc-pnum gap closure)', () => {
  let runtime, MatchLineupSideEditor;
  let origAPI, origHelpers, origResolveRound, origCompMatches;

  const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };
  const TEAM = { id: 'uuid-grouped', name: 'Grouped Team' };
  const MATCH = { id: 'match-1', compId: 'comp-1', sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'other', name: 'Other' }, status: 'scheduled' };

  beforeEach(async () => {
    origAPI = global.window.API;
    origHelpers = global.window.AdminLineupHelpers;
    origResolveRound = global.window.resolveRoundIndex;
    origCompMatches = global.window.compMatches;

    global.window.resolveRoundIndex = () => 0;
    global.window.compMatches = () => [];
    global.window.AdminLineupHelpers = {
      positionsForSize: (n) => Array.from({ length: n }, (_, i) => ({ key: String(i + 1), label: String(i + 1) })),
      rosterFor: () => [],
      mergeRosterWithAssigned: (base) => (Array.isArray(base) ? base : []),
      teamIdOf: (t) => t?.id || t?.name || '',
      resolveMemberIdsForPositions: vi.fn().mockResolvedValue({ memberIds: {}, squad: [], failures: [] }),
      memberIdentityWarning,
    };
    global.window.API = {
      fetchMatchLineup: vi.fn().mockResolvedValue(null),
      fetchTeamLineup: vi.fn().mockResolvedValue(null),
      fetchSquads: vi.fn().mockResolvedValue({}),
      putMatchLineup: vi.fn().mockResolvedValue({ positions: {} }),
    };

    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();
    ({ MatchLineupSideEditor } = await import('../admin_schedule_lineup.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    global.window.API = origAPI;
    global.window.AdminLineupHelpers = origHelpers;
    global.window.resolveRoundIndex = origResolveRound;
    global.window.compMatches = origCompMatches;
    vi.resetModules();
  });

  async function mount() {
    runtime.mount(MatchLineupSideEditor, {
      comp: COMP, team: TEAM, match: MATCH, allMatches: [MATCH], password: 'pw', showToast: vi.fn(),
    });
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve(); // let the squad-load effect settle too
    return runtime.currentTree();
  }

  it('loads the team squad and passes it, plus the positions, to the resolver on save', async () => {
    const squadForTeam = [{ id: 'mem-1', index: 0, name: 'Sato' }];
    global.window.API.fetchSquads = vi.fn().mockResolvedValue({ 'uuid-grouped': squadForTeam });

    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Sato');
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();

    expect(global.window.AdminLineupHelpers.resolveMemberIdsForPositions)
      .toHaveBeenCalledWith('comp-1', 'uuid-grouped', { 1: 'Sato' }, squadForTeam, 'pw');
  });

  it('sends the resolved memberIds to putMatchLineup', async () => {
    global.window.AdminLineupHelpers.resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
      memberIds: { 1: 'mem-1' },
      squad: [{ id: 'mem-1', index: 0, name: 'Sato' }],
    });

    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Sato');
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();

    expect(global.window.API.putMatchLineup).toHaveBeenCalled();
    // putMatchLineup(compId, teamId, matchId, positionsOut, password, memberIdsOut)
    const call = global.window.API.putMatchLineup.mock.calls.at(-1);
    expect(call[5]).toEqual({ 1: 'mem-1' });
  });

  it('omits memberIds entirely when the resolver has nothing to report', async () => {
    // Default beforeEach mock already resolves to an empty map.
    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Sato');
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();

    const call = global.window.API.putMatchLineup.mock.calls.at(-1);
    expect(call[5]).toBeUndefined();
  });

  it('a FAILING resolver (rejects outright) never blocks the save: the lineup still writes with names alone', async () => {
    // This is the one that matters most: the operator is never blocked by
    // a resolve/mint failure (offline venue wifi).
    global.window.AdminLineupHelpers.resolveMemberIdsForPositions = vi.fn().mockRejectedValue(new Error('offline'));

    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Sato');
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();

    expect(global.window.API.putMatchLineup).toHaveBeenCalled();
    const call = global.window.API.putMatchLineup.mock.calls.at(-1);
    expect(call[3]).toEqual({ 1: 'Sato' }); // positions still written
    expect(call[5]).toBeUndefined(); // no ids resolved, but the write proceeded
  });

  // bc-cse gap closure: making the swallowed failure visible, without ever
  // blocking the operator.
  it('shows the composed member-identity warning after a save whose resolver reported a failure', async () => {
    global.window.AdminLineupHelpers.resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
      memberIds: {},
      squad: [],
      failures: [{ position: '1', name: 'Sato', reason: 'sato normalises onto an existing member' }],
    });

    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Sato');
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    tree = runtime.currentTree();
    // The save itself still succeeded (never blocked): putMatchLineup ran.
    expect(global.window.API.putMatchLineup).toHaveBeenCalled();
    const warning = memberWarning(tree);
    expect(warning).toBeTruthy();
    const text = collectText(warning);
    expect(text).toContain('Lineup saved');
    expect(text).toContain('Sato');
    expect(text).toContain('Scores will still record normally');
    // Visually distinct from the error channel: a different class, and no
    // error banner rendered alongside it.
    expect(warning.props.className).toContain('alert--warn');
    expect(errorBanner(tree)).toBeFalsy();
  });

  it('shows the ONE root-cause sentence, not a per-position list, when the squad itself failed to load', async () => {
    global.window.API.fetchSquads = vi.fn().mockRejectedValue(new Error('network error'));
    // Even if a resolver failure ALSO carried a per-position reason, the
    // squad-unavailable sentence must win: every position looked "new" to
    // the resolver for the SAME root cause, so a per-position list would
    // just repeat it.
    global.window.AdminLineupHelpers.resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
      memberIds: {},
      squad: [],
      failures: [{ position: '1', name: 'Sato', reason: 'duplicate name' }],
    });

    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Sato');
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    tree = runtime.currentTree();
    expect(global.window.API.putMatchLineup).toHaveBeenCalled(); // never blocked
    const warning = memberWarning(tree);
    expect(warning).toBeTruthy();
    const text = collectText(warning);
    expect(text).toContain('squad list could not be loaded');
    expect(text).not.toContain('Sato'); // no per-position enumeration
  });
});
