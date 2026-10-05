// The at-court lineup panel (MatchLineupSideEditor) shows the lineup the team
// fields at the match, the way the score sheet does (resolveMatchLineup: the
// match's own lineup, else the one the team carries from its previous match or
// round; the server owns the rule), says where that lineup was saved, and never
// writes a side the operator did not change. Same harness as
// match_lineup_side_editor_member_ids.test.jsx.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { collectText, expandNamed } from './helpers/vdom.js';

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
const saveButton = (tree) =>
  findHosts(tree, 'button').find(b => /Save lineup/.test(collectText(b)));
// This runtime never calls a child component, so the two the editor hands its
// source line and its read problem to are expanded by name (both are hook-free).
const allText = (tree) => collectText(tree, expandNamed('LineupSourceLine', 'LineupProblem'));

describe('MatchLineupSideEditor shows the lineup in force, where it came from, and writes only a changed side', () => {
  let runtime, MatchLineupSideEditor;
  let origAPI, origHelpers, origCompMatches;

  const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };
  const TEAM = { id: 'uuid-grouped', name: 'Grouped Team', number: 'T5' };
  const MATCH = {
    id: 'Pool D-1', compId: 'comp-1', phase: 'pool', poolName: 'Pool D',
    sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'other', name: 'Other' }, status: 'scheduled',
  };
  // The team's earlier match, which the lineup in force is carried from.
  const EARLIER = {
    id: 'Pool D-0', compId: 'comp-1', phase: 'pool', poolName: 'Pool D',
    sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'third', name: 'Third' }, status: 'completed',
  };
  const SQUAD = [
    { id: 'mem-1', index: 1, name: 'Aoki' },
    { id: 'mem-2', index: 2, name: 'Sato' },
    { id: 'mem-3', index: 3, name: 'Ito' },
    { id: 'mem-4', index: 4, name: 'Mori' },
  ];
  const NAMES = {
    positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' },
    memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' },
  };
  // What the server answers, by where the lineup was saved.
  const CARRIED = { teamId: 'uuid-grouped', matchId: 'Pool D-0', round: 0, ...NAMES, sourceMatchId: 'Pool D-0', saved: true };
  const OWN = { teamId: 'uuid-grouped', matchId: 'Pool D-1', round: 0, ...NAMES, sourceMatchId: 'Pool D-1', saved: true };
  const STARTING = { teamId: 'uuid-grouped', round: 0, ...NAMES, sourceRound: 0, saved: true };

  beforeEach(async () => {
    origAPI = global.window.API;
    origHelpers = global.window.AdminLineupHelpers;
    origCompMatches = global.window.compMatches;

    global.window.compMatches = () => [];
    global.window.AdminLineupHelpers = {
      positionsForSize: (n) => Array.from({ length: n }, (_, i) => ({ key: String(i + 1), label: String(i + 1) })),
      rosterFor: () => [],
      mergeRosterWithAssigned: (base) => (Array.isArray(base) ? base : []),
      teamIdOf: (t) => t?.id || t?.name || '',
      resolveMemberIdsForPositions: vi.fn().mockResolvedValue({ memberIds: {}, squad: [], failures: [] }),
      memberIdentityWarning: () => '',
    };
    global.window.API = {
      fetchLineupInForce: vi.fn().mockResolvedValue(CARRIED),
      fetchSquads: vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD }),
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
    global.window.compMatches = origCompMatches;
    vi.resetModules();
  });

  const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };

  async function mount(match = MATCH, allMatches = [EARLIER, match]) {
    runtime.mount(MatchLineupSideEditor, {
      comp: COMP, team: TEAM, match, allMatches, password: 'pw', showToast: vi.fn(),
    });
    await flush();
    return runtime.currentTree();
  }
  const pickerValues = (tree) => findComponents(tree, 'LineupNameInput').map(p => p.props.value);

  it('shows the names the team carries from its earlier match, and names that match', async () => {
    const tree = await mount();
    expect(global.window.API.fetchLineupInForce).toHaveBeenCalledWith('comp-1', 'uuid-grouped', 'Pool D-1');
    expect(pickerValues(tree)).toEqual(['Aoki', 'Sato', 'Ito']);
    expect(allText(tree)).toContain('Same as Pool D · Match 1');
    expect(allText(tree)).not.toContain('Lineup for this match');
  });

  it('names a carried-from match the list no longer holds by its id', async () => {
    const tree = await mount(MATCH, [MATCH]);
    expect(allText(tree)).toContain('Same as Pool D-0');
  });

  it('marks the team\'s starting lineup as such', async () => {
    global.window.API.fetchLineupInForce = vi.fn().mockResolvedValue(STARTING);
    const tree = await mount();
    expect(pickerValues(tree)).toEqual(['Aoki', 'Sato', 'Ito']);
    expect(allText(tree)).toContain('Starting lineup');
  });

  it('names the round of a later Lineups-page lineup', async () => {
    global.window.API.fetchLineupInForce = vi.fn().mockResolvedValue({ ...STARTING, round: 1, sourceRound: 1 });
    const tree = await mount();
    expect(allText(tree)).toContain('From the Lineups page (Round 2)');
  });

  it('marks a match\'s own lineup', async () => {
    global.window.API.fetchLineupInForce = vi.fn().mockResolvedValue({
      ...OWN, positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' },
    });
    const tree = await mount();
    expect(pickerValues(tree)).toEqual(['Mori', '', '']);
    expect(allText(tree)).toContain('Lineup for this match');
    expect(allText(tree)).not.toContain('Same as');
  });

  it('says so when no lineup is saved at all', async () => {
    global.window.API.fetchLineupInForce = vi.fn().mockResolvedValue(null);
    const tree = await mount();
    expect(pickerValues(tree)).toEqual(['', '', '']);
    expect(allText(tree)).toContain('No lineup saved yet');
  });

  it('disables Save on an untouched carried side, and a direct click writes nothing', async () => {
    const tree = await mount();
    const save = saveButton(tree);
    expect(save.props.disabled).toBe(true);
    expect(save.props.title).toBe('No changes to save');
    save.props.onClick();
    await flush();
    expect(global.window.API.putMatchLineup).not.toHaveBeenCalled();
  });

  it('after one edit Save enables and the write carries every carried position plus the edit', async () => {
    let tree = await mount();
    findComponents(tree, 'LineupNameInput')[1].props.onSelect('Mori', SQUAD[3]);
    tree = runtime.currentTree();
    const save = saveButton(tree);
    expect(save.props.disabled).toBe(false);
    save.props.onClick();
    await flush();

    expect(global.window.API.putMatchLineup).toHaveBeenCalledTimes(1);
    const call = global.window.API.putMatchLineup.mock.calls[0];
    expect(call[3]).toEqual({ 1: 'Aoki', 2: 'Mori', 3: 'Ito' });
    expect(call[5]).toEqual({ 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' });
  });

  it('an edit put back to what was loaded is not a change', async () => {
    let tree = await mount();
    findComponents(tree, 'LineupNameInput')[1].props.onSelect('Mori', SQUAD[3]);
    findComponents(runtime.currentTree(), 'LineupNameInput')[1].props.onSelect('Sato', SQUAD[1]);
    tree = runtime.currentTree();
    expect(saveButton(tree).props.disabled).toBe(true);
  });

  it('after a confirmed save the side is the baseline again: Save disables and the label turns to the match\'s own', async () => {
    global.window.API.putMatchLineup = vi.fn().mockResolvedValue({
      positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    });
    let tree = await mount();
    findComponents(tree, 'LineupNameInput')[1].props.onSelect('Mori', SQUAD[3]);
    saveButton(runtime.currentTree()).props.onClick();
    await flush();
    tree = runtime.currentTree();
    expect(saveButton(tree).props.disabled).toBe(true);
    expect(allText(tree)).toContain('Lineup for this match');
    expect(allText(tree)).not.toContain('Same as');
  });

  it('a queued (offline) write keeps the side dirty', async () => {
    global.window.API.putMatchLineup = vi.fn().mockResolvedValue({ queued: true });
    let tree = await mount();
    findComponents(tree, 'LineupNameInput')[1].props.onSelect('Mori', SQUAD[3]);
    saveButton(runtime.currentTree()).props.onClick();
    await flush();
    tree = runtime.currentTree();
    expect(saveButton(tree).props.disabled).toBe(false);
  });

  it('a deliberate edit that empties a match\'s own side is dirty and still saves', async () => {
    global.window.API.fetchLineupInForce = vi.fn().mockResolvedValue({
      ...OWN, positions: { 1: 'Aoki' }, memberIds: { 1: 'mem-1' },
    });
    let tree = await mount();
    findComponents(tree, 'LineupNameInput')[0].props.onSelect('');
    tree = runtime.currentTree();
    expect(saveButton(tree).props.disabled).toBe(false);
    saveButton(tree).props.onClick();
    await flush();
    expect(global.window.API.putMatchLineup.mock.calls[0][3]).toEqual({});
  });

  it('a failed read shows "Failed to load lineup" rather than an empty lineup', async () => {
    global.window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error(''));
    const tree = await mount();
    expect(allText(tree)).toContain('Failed to load lineup');
    expect(pickerValues(tree)).toEqual(['', '', '']);
  });

  it('a read of a missing competition shows the server\'s message', async () => {
    global.window.API.fetchLineupInForce = vi.fn().mockRejectedValue(new Error('competition not found'));
    const tree = await mount();
    expect(allText(tree)).toContain('competition not found');
  });

  it('renaming a member on a carried side does not make the side look edited', async () => {
    global.window.API.renameTeamMember = vi.fn().mockResolvedValue({ id: 'mem-1', index: 1, name: 'Aoki Jr' });
    let tree = await mount();
    const rename = findHosts(tree, 'button').find(b => b.props?.['aria-label'] === 'Rename 1 player');
    expect(rename).toBeTruthy();
    rename.props.onClick();
    tree = runtime.currentTree();
    findHosts(tree, 'input').find(i => i.props?.['aria-label'] === 'Rename 1 player')
      .props.onChange({ target: { value: 'Aoki Jr' } });
    tree = runtime.currentTree();
    findHosts(tree, 'button').find(b => /^Save$/.test(collectText(b).trim())).props.onClick();
    await flush();

    tree = runtime.currentTree();
    expect(global.window.API.renameTeamMember).toHaveBeenCalledWith('comp-1', 'uuid-grouped', 'mem-1', 'Aoki Jr', 'pw');
    expect(pickerValues(tree)[0]).toBe('Aoki Jr');
    expect(saveButton(tree).props.disabled).toBe(true);
    expect(allText(tree)).toContain('Same as Pool D · Match 1');
  });
});
