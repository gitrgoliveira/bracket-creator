// MatchLineupSideEditor (per-match lineup, used by MatchLineupPanel / shiaijo
// "Enter lineup") must not persist leading/trailing or whitespace-only names
// now that positions are free-text (LineupNameInput). Copilot review on PR #319:
// the picker's onSelect stored the raw value into `values`, so save() could PUT
// untrimmed names. We trim at the selection boundary AND in save() (matching
// AdminLineup); this test pins the saved positions are trimmed.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

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

describe('MatchLineupSideEditor trims names before saving', () => {
  let runtime, MatchLineupSideEditor;
  let origAPI, origHelpers, origResolveRound, origCompMatches;

  const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };
  const TEAM = { id: 'uuid-grouped', name: 'Grouped Team' }; // no metadata
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
    };
    global.window.API = {
      fetchMatchLineup: vi.fn().mockResolvedValue(null),
      fetchTeamLineup: vi.fn().mockResolvedValue(null),
      putMatchLineup: vi.fn().mockResolvedValue({ positions: {}, lockedAt: null }),
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
    return runtime.currentTree();
  }

  it('trims leading/trailing whitespace from a selected name before PUT', async () => {
    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    expect(pickers.length).toBe(3);
    pickers[0].props.onSelect('  Padded Name  ');
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    expect(global.window.API.putMatchLineup).toHaveBeenCalled();
    // putMatchLineup(compId, teamId, matchId, positionsOut, ...)
    const positionsOut = global.window.API.putMatchLineup.mock.calls.at(-1)[3];
    expect(positionsOut['1']).toBe('Padded Name');
  });

  it('drops a whitespace-only name rather than persisting blanks', async () => {
    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('   ');
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    const positionsOut = global.window.API.putMatchLineup.mock.calls.at(-1)[3];
    expect(positionsOut['1']).toBeUndefined();
  });
});
