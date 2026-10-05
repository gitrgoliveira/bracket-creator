// The at-court lineup panel (MatchLineupSideEditor) resolves a side the way the
// score sheet does (resolveMatchLineup: the match's own lineup, else the
// nearest saved round), so it shows the same inherited names, and it never
// writes a side the operator did not change. Same harness as
// match_lineup_side_editor_member_ids.test.jsx.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { collectText } from './helpers/vdom.js';

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
const allText = (tree) => collectText(tree);

describe('MatchLineupSideEditor shows the round default the sheet shows, and writes only a changed side', () => {
  let runtime, MatchLineupSideEditor;
  let origAPI, origHelpers, origResolveRound, origCompMatches;

  const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };
  const TEAM = { id: 'uuid-grouped', name: 'Grouped Team', number: 'T5' };
  const MATCH = {
    id: 'match-1', compId: 'comp-1', round: 1,
    sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'other', name: 'Other' }, status: 'scheduled',
  };
  const SQUAD = [
    { id: 'mem-1', index: 1, name: 'Aoki' },
    { id: 'mem-2', index: 2, name: 'Sato' },
    { id: 'mem-3', index: 3, name: 'Ito' },
    { id: 'mem-4', index: 4, name: 'Mori' },
  ];
  // Saved for round 0 only; the match asks for round 1.
  const ROUND_ZERO = {
    teamId: 'uuid-grouped', round: 0,
    positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' },
    memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' },
  };

  beforeEach(async () => {
    origAPI = global.window.API;
    origHelpers = global.window.AdminLineupHelpers;
    origResolveRound = global.window.resolveRoundIndex;
    origCompMatches = global.window.compMatches;

    global.window.resolveRoundIndex = (m) => m.round;
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
      fetchMatchLineup: vi.fn().mockResolvedValue(null),
      // Only a best-effort read finds the round-0 lineup; an exact read of
      // round 1 answers nothing saved (null), as the real client does.
      fetchTeamLineup: vi.fn((_c, _t, _round, opts) => Promise.resolve(opts && opts.fallback ? ROUND_ZERO : null)),
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
    global.window.resolveRoundIndex = origResolveRound;
    global.window.compMatches = origCompMatches;
    vi.resetModules();
  });

  const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };

  async function mount(match = MATCH) {
    runtime.mount(MatchLineupSideEditor, {
      comp: COMP, team: TEAM, match, allMatches: [match], password: 'pw', showToast: vi.fn(),
    });
    await flush();
    return runtime.currentTree();
  }
  const pickerValues = (tree) => findComponents(tree, 'LineupNameInput').map(p => p.props.value);

  it('shows the nearest saved round\'s names, marked as inherited from that round', async () => {
    const tree = await mount();
    expect(global.window.API.fetchTeamLineup).toHaveBeenCalledWith('comp-1', 'uuid-grouped', 1, { fallback: true });
    expect(pickerValues(tree)).toEqual(['Aoki', 'Sato', 'Ito']);
    expect(allText(tree)).toContain('Inheriting Round 1 lineup');
    expect(allText(tree)).not.toContain('Override for this match');
  });

  it('keeps the plain inherited wording when the lineup carries no round', async () => {
    global.window.API.fetchTeamLineup = vi.fn().mockResolvedValue({ positions: { 1: 'Aoki' } });
    const tree = await mount();
    expect(allText(tree)).toContain('Inheriting round default');
  });

  it('marks a match\'s own lineup as an override', async () => {
    global.window.API.fetchMatchLineup = vi.fn().mockResolvedValue({
      matchId: 'match-1', positions: { 1: 'Mori' }, memberIds: { 1: 'mem-4' },
    });
    const tree = await mount();
    expect(pickerValues(tree)).toEqual(['Mori', '', '']);
    expect(allText(tree)).toContain('Override for this match');
  });

  it('disables Save on an untouched inherited side, and a direct click writes nothing', async () => {
    const tree = await mount();
    const save = saveButton(tree);
    expect(save.props.disabled).toBe(true);
    expect(save.props.title).toBe('No changes to save');
    save.props.onClick();
    await flush();
    expect(global.window.API.putMatchLineup).not.toHaveBeenCalled();
  });

  it('after one edit Save enables and the write carries every inherited position plus the edit', async () => {
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

  it('after a confirmed save the side is the baseline again: Save disables and the label turns to override', async () => {
    global.window.API.putMatchLineup = vi.fn().mockResolvedValue({
      positions: { 1: 'Aoki', 2: 'Mori', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-4', 3: 'mem-3' },
    });
    let tree = await mount();
    findComponents(tree, 'LineupNameInput')[1].props.onSelect('Mori', SQUAD[3]);
    saveButton(runtime.currentTree()).props.onClick();
    await flush();
    tree = runtime.currentTree();
    expect(saveButton(tree).props.disabled).toBe(true);
    expect(allText(tree)).toContain('Override for this match');
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

  it('a deliberate edit that empties an overridden side is dirty and still saves', async () => {
    global.window.API.fetchMatchLineup = vi.fn().mockResolvedValue({
      matchId: 'match-1', positions: { 1: 'Aoki' }, memberIds: { 1: 'mem-1' },
    });
    let tree = await mount();
    findComponents(tree, 'LineupNameInput')[0].props.onSelect('');
    tree = runtime.currentTree();
    expect(saveButton(tree).props.disabled).toBe(false);
    saveButton(tree).props.onClick();
    await flush();
    expect(global.window.API.putMatchLineup.mock.calls[0][3]).toEqual({});
  });

  it('a failed read shows "Failed to load lineup" rather than the round default', async () => {
    global.window.API.fetchMatchLineup = vi.fn().mockRejectedValue(new Error(''));
    const tree = await mount();
    expect(allText(tree)).toContain('Failed to load lineup');
    expect(pickerValues(tree)).toEqual(['', '', '']);
    expect(global.window.API.fetchTeamLineup).not.toHaveBeenCalled();
  });

  it('a read of a missing competition shows the server\'s message', async () => {
    global.window.API.fetchTeamLineup = vi.fn().mockRejectedValue(new Error('competition not found'));
    const tree = await mount();
    expect(allText(tree)).toContain('competition not found');
  });

  it('renaming a member on an inherited side does not make the side look edited', async () => {
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
    expect(allText(tree)).toContain('Inheriting Round 1 lineup');
  });
});
