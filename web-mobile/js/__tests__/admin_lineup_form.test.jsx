// AdminLineup (competition-admin → Lineups) form behaviour.
//
// Regression guard: a team with NO member metadata used to render a fixed
// <select> limited to roster members, with the Save button disabled when the
// roster was empty: so for teams formed by grouping individuals (no metadata)
// the operator could not enter ANY lineup. The form now uses LineupNameInput
// (a typeable autocomplete combobox) so any name can be typed, and Save is
// no longer gated on the roster being non-empty.
//
// Because the test runtime (makeReactive) does NOT recurse into child
// component bodies, LineupNameInput appears in the tree as a component
// node with type === LineupNameInput function. Assertions check props on
// those nodes rather than the inner <input>/<datalist> DOM elements.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

const realReact = global.React;

function childrenOf(node) {
  const c = node.children;
  if (c !== undefined && c !== null && !(Array.isArray(c) && c.length === 0)) return c;
  return node.props?.children;
}

// Recurse through both element nodes AND nested arrays (e.g. positions.map()).
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

// Find component nodes (where type is a function) by function name.
function findComponents(tree, name) {
  const out = [];
  walk(tree, n => {
    if (n && typeof n.type === 'function' && n.type.name === name) out.push(n);
  });
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

describe('AdminLineup form (competition-admin Lineups)', () => {
  let runtime;
  let AdminLineup;
  let origAPI;
  let origCompMatches;
  let origAdminLineupHelpers;

  const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };

  beforeEach(async () => {
    origAPI = global.window.API;
    origCompMatches = global.window.compMatches;
    origAdminLineupHelpers = global.window.AdminLineupHelpers;
    global.window.compMatches = () => [];
    global.window.API = {
      fetchTeamLineup: vi.fn().mockResolvedValue(null), // 404 → fresh form
      putTeamLineup: vi.fn().mockResolvedValue({}),
    };
    // mergeRosterWithAssigned must be available for the suggestions computation
    // in AdminLineup (it reads window.AdminLineupHelpers only in admin_schedule_lineup;
    // admin_lineup.jsx calls mergeRosterWithAssigned directly from its own scope).
    // But window.AdminLineupHelpers is used by admin_schedule_lineup: set it up
    // here in case the module re-exports trigger it.
    global.window.AdminLineupHelpers = {
      positionsForSize: (n) => Array.from({ length: n }, (_, i) => ({
        key: String(i + 1), label: String(i + 1),
      })),
      rosterFor: (team) => {
        if (!team) return [];
        if (Array.isArray(team.metadata) && team.metadata.length > 0) return team.metadata;
        if (Array.isArray(team.Metadata) && team.Metadata.length > 0) return team.Metadata;
        return [];
      },
      mergeRosterWithAssigned: (base, lineup) => {
        const arr = Array.isArray(base) ? base : [];
        const positions = lineup && lineup.positions ? lineup.positions : null;
        if (!positions) return arr;
        const seen = new Set(arr.map(n => String(n).trim().toLowerCase()));
        const extras = [];
        for (const raw of Object.values(positions)) {
          const name = String(raw == null ? '' : raw).trim();
          if (!name) continue;
          const key = name.toLowerCase();
          if (seen.has(key)) continue;
          seen.add(key);
          extras.push(name);
        }
        return extras.length ? [...arr, ...extras] : arr;
      },
      teamIdOf: (team) => team?.id || team?.ID || team?.name || team?.Name || '',
    };
    // useClickOutside is needed by LineupNameInput (called via window.useClickOutside).
    // ui.jsx is loaded by vitest.setup.js, which sets window.useClickOutside.
    // Nothing extra needed here.

    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();
    ({ AdminLineup } = await import('../admin_lineup.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    global.window.API = origAPI;
    global.window.compMatches = origCompMatches;
    global.window.AdminLineupHelpers = origAdminLineupHelpers;
    vi.resetModules();
  });

  async function mountFor(team) {
    runtime.mount(AdminLineup, {
      comp: COMP, team, round: 0, password: 'pw', showToast: vi.fn(), onClose: vi.fn(),
    });
    // Let the mount effect's fetchTeamLineup promise resolve and flip loading.
    await Promise.resolve();
    await Promise.resolve();
    return runtime.currentTree();
  }

  it('renders pickers for a team WITHOUT metadata', async () => {
    const tree = await mountFor({ id: 'uuid-grouped', name: 'Grouped Team' });

    // LineupNameInput component nodes: one per position (teamSize 3).
    const pickers = findComponents(tree, 'LineupNameInput');
    expect(pickers.length).toBe(3);

    // No host <select> with a data-testid starting "lineup-position-".
    const selects = findHosts(tree, 'select').filter(
      s => String(s.props?.['data-testid'] || '').startsWith('lineup-position-'));
    expect(selects.length).toBe(0);
  });

  it('does NOT disable Save when the team has no roster', async () => {
    const tree = await mountFor({ id: 'uuid-grouped', name: 'Grouped Team' });
    const btn = saveButton(tree);
    expect(btn).toBeTruthy();
    expect(btn.props.disabled).toBeFalsy();
    // And the hint invites typing names directly.
    expect(collectText(tree)).toContain('type each competitor');
  });

  it('offers the registered roster as suggestions when metadata exists', async () => {
    const tree = await mountFor({
      id: 'uuid-meta', name: 'Tora', metadata: ['Tanaka', 'Sato', 'Yamada'],
    });
    // Each LineupNameInput receives the roster as a prop.
    const pickers = findComponents(tree, 'LineupNameInput');
    expect(pickers.length).toBeGreaterThan(0);
    const roster = pickers[0].props.roster;
    expect(roster).toContain('Tanaka');
    expect(roster).toContain('Sato');
    expect(roster).toContain('Yamada');
    // Save is enabled here too.
    expect(saveButton(tree).props.disabled).toBeFalsy();
  });

  it('trims leading/trailing whitespace before saving (onSelect + Save)', async () => {
    const tree = await mountFor({ id: 'uuid-x', name: 'Grouped' });
    // Drive the first LineupNameInput's onSelect with a padded name.
    const pickers = findComponents(tree, 'LineupNameInput');
    expect(pickers.length).toBeGreaterThan(0);
    pickers[0].props.onSelect('  Padded  ');
    const tree2 = runtime.currentTree();
    saveButton(tree2).props.onClick();
    await Promise.resolve();
    expect(global.window.API.putTeamLineup).toHaveBeenCalled();
    // putTeamLineup(compId, teamId, round, positionsOut, password)
    const positionsOut = global.window.API.putTeamLineup.mock.calls.at(-1)[3];
    expect(positionsOut['1']).toBe('Padded');
  });

  it('drops a whitespace-only position instead of persisting blanks', async () => {
    const tree = await mountFor({ id: 'uuid-x', name: 'Grouped' });
    const pickers = findComponents(tree, 'LineupNameInput');
    expect(pickers.length).toBeGreaterThan(0);
    // onSelect with whitespace-only string: save() trims to "" and drops it.
    pickers[0].props.onSelect('   ');
    const tree2 = runtime.currentTree();
    saveButton(tree2).props.onClick();
    await Promise.resolve();
    const positionsOut = global.window.API.putTeamLineup.mock.calls.at(-1)[3];
    expect(positionsOut['1']).toBeUndefined();
  });
});
