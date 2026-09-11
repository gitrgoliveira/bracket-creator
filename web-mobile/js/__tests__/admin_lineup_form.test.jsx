// AdminLineup (competition-admin → Lineups) form behaviour (bc-tmid pass 3).
//
// The round-scoped lineup editor now resolves a position to a squad MEMBER
// (id + name), not a bare typed string. Per the operator's ruling there are
// exactly three operations: SELECT an existing squad member into a
// position, ADD a new name in a position (minting the member's id in that
// one step), and RENAME a member (keeping its id). The squad itself is
// read from GET /api/competitions/:id/squads, never from team.metadata.
//
// Because the test runtime (makeReactive) does NOT recurse into child
// component bodies, host elements (<select>, <option>, <input>, <button>)
// DO appear fully in the tree with their real props: events are simulated
// by calling the relevant prop function directly (onChange/onClick) rather
// than through a DOM.

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

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

const mainSaveButton = (tree) =>
  findHosts(tree, 'button').find(b => /^Save lineup$/.test(collectText(b).trim()));

const positionSelect = (tree, key) =>
  findHosts(tree, 'select').find(s => s.props?.['data-testid'] === `lineup-position-${key}`);

const squadRow = (tree, memberId) =>
  findHosts(tree, 'div').find(d => d.props?.['data-testid'] === `squad-member-${memberId}`);

const buttonNamed = (node, text) =>
  findHosts(node, 'button').find(b => collectText(b).trim() === text);

async function flush(times = 8) {
  for (let i = 0; i < times; i++) await Promise.resolve();
}

describe('AdminLineup form (competition-admin Lineups, bc-tmid pass 3)', () => {
  let runtime;
  let AdminLineup;
  let origAPI;
  let origCompMatches;

  const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };

  beforeEach(async () => {
    origAPI = global.window.API;
    origCompMatches = global.window.compMatches;
    global.window.compMatches = () => [];
    global.window.API = {
      fetchTeamLineup: vi.fn().mockResolvedValue(null), // 404 → fresh form
      fetchSquads: vi.fn().mockResolvedValue({}),
      addTeamMember: vi.fn(),
      renameTeamMember: vi.fn().mockResolvedValue(true),
      putTeamLineup: vi.fn().mockResolvedValue({}),
    };

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
    vi.resetModules();
  });

  async function mountFor(team, opts = {}) {
    if (opts.lineup !== undefined) {
      global.window.API.fetchTeamLineup.mockResolvedValue(opts.lineup);
    }
    if (opts.squads !== undefined) {
      global.window.API.fetchSquads.mockResolvedValue(opts.squads);
    }
    runtime.mount(AdminLineup, {
      comp: COMP, team, round: 0, password: 'pw', showToast: vi.fn(), onClose: vi.fn(),
    });
    await flush();
    return runtime.currentTree();
  }

  it('renders one <select> position picker per team-size slot, sourced from the squad', async () => {
    const tree = await mountFor({ id: 'team-1', name: 'Tora A', number: 'T10' }, {
      squads: { 'team-1': [{ id: 'sq-sato', index: 1, name: 'Sato' }] },
    });
    const selects = ['1', '2', '3'].map(k => positionSelect(tree, k));
    expect(selects.every(Boolean)).toBe(true);

    const optionTexts = collectText(selects[0]);
    expect(optionTexts).toContain('Sato');
    expect(optionTexts).toContain('T10.1');
  });

  it('reads the squad from GET .../squads rather than from team.metadata', async () => {
    // metadata carries a name the squad endpoint does NOT: proves the
    // picker's options come from the squad store, not team.metadata.
    const tree = await mountFor(
      { id: 'team-1', name: 'Tora A', number: 'T10', metadata: ['LegacyOnlyName'] },
      { squads: { 'team-1': [{ id: 'sq-sato', index: 1, name: 'Sato' }] } },
    );
    const select1 = positionSelect(tree, '1');
    const optionTexts = collectText(select1);
    expect(optionTexts).toContain('Sato');
    expect(optionTexts).not.toContain('LegacyOnlyName');
    expect(global.window.API.fetchSquads).toHaveBeenCalledWith('comp-1', 'pw');
  });

  it('operation 1 (SELECT): choosing an existing squad member sets its name AND id for that position', async () => {
    const tree = await mountFor({ id: 'team-1', name: 'Tora A', number: 'T10' }, {
      squads: { 'team-1': [{ id: 'sq-sato', index: 1, name: 'Sato' }] },
    });
    positionSelect(tree, '1').props.onChange({ target: { value: 'sq-sato' } });

    const tree2 = runtime.currentTree();
    mainSaveButton(tree2).props.onClick();
    await flush();

    expect(global.window.API.putTeamLineup).toHaveBeenCalled();
    const call = global.window.API.putTeamLineup.mock.calls.at(-1);
    // putTeamLineup(compId, teamId, round, positionsOut, password, memberIdsOut)
    expect(call[3]['1']).toBe('Sato');
    expect(call[5]['1']).toBe('sq-sato');
  });

  it('operation 2 (ADD): typing a new name in a position mints a member and stores its id there', async () => {
    global.window.API.addTeamMember.mockResolvedValue({ id: 'sq-new', index: 1, name: 'Ito' });
    const tree = await mountFor({ id: 'team-1', name: 'Tora A', number: 'T10' }, { squads: {} });

    // Choosing "+ Add new member…" switches that position into add-mode.
    positionSelect(tree, '1').props.onChange({ target: { value: '__add__' } });
    let tree2 = runtime.currentTree();
    const nameField = findHosts(tree2, 'input').find(i => i.props?.['aria-label'] === 'New member name for 1');
    expect(nameField).toBeTruthy();
    nameField.props.onChange({ target: { value: 'Ito' } });

    tree2 = runtime.currentTree();
    const addBtn = buttonNamed(tree2, 'Add');
    expect(addBtn).toBeTruthy();
    addBtn.props.onClick();
    await flush();

    expect(global.window.API.addTeamMember).toHaveBeenCalledWith('comp-1', 'team-1', 'Ito', 'pw');

    // The minted member is now selected in that same position.
    const tree3 = runtime.currentTree();
    expect(positionSelect(tree3, '1').props.value).toBe('sq-new');

    mainSaveButton(tree3).props.onClick();
    await flush();
    const call = global.window.API.putTeamLineup.mock.calls.at(-1);
    expect(call[3]['1']).toBe('Ito');
    expect(call[5]['1']).toBe('sq-new');
  });

  it('operation 3 (RENAME): renaming a squad member keeps its id in the position that references it', async () => {
    const tree = await mountFor({ id: 'team-1', name: 'Tora A', number: 'T10' }, {
      squads: { 'team-1': [{ id: 'sq-sato', index: 1, name: 'Sato' }] },
    });
    // Select the member into position 1 first.
    positionSelect(tree, '1').props.onChange({ target: { value: 'sq-sato' } });

    let tree2 = runtime.currentTree();
    const row = squadRow(tree2, 'sq-sato');
    expect(row).toBeTruthy();
    buttonNamed(row, 'Rename').props.onClick();

    tree2 = runtime.currentTree();
    const rowAfterRenameStart = squadRow(tree2, 'sq-sato');
    const renameField = findHosts(rowAfterRenameStart, 'input')[0];
    expect(renameField).toBeTruthy();
    renameField.props.onChange({ target: { value: 'Sato-Renamed' } });

    tree2 = runtime.currentTree();
    const rowMidRename = squadRow(tree2, 'sq-sato');
    buttonNamed(rowMidRename, 'Save').props.onClick();
    await flush();

    expect(global.window.API.renameTeamMember).toHaveBeenCalledWith('comp-1', 'team-1', 'sq-sato', 'Sato-Renamed', 'pw');

    const tree3 = runtime.currentTree();
    // The position's select value (the id) is unchanged...
    expect(positionSelect(tree3, '1').props.value).toBe('sq-sato');
    // ...but the squad row now shows the new name.
    expect(collectText(squadRow(tree3, 'sq-sato'))).toContain('Sato-Renamed');

    mainSaveButton(tree3).props.onClick();
    await flush();
    const call = global.window.API.putTeamLineup.mock.calls.at(-1);
    // Save persists the NEW name against the SAME (unchanged) id.
    expect(call[3]['1']).toBe('Sato-Renamed');
    expect(call[5]['1']).toBe('sq-sato');
  });

  it('there is no member-removal control anywhere in the form', async () => {
    const tree = await mountFor({ id: 'team-1', name: 'Tora A', number: 'T10' }, {
      squads: { 'team-1': [{ id: 'sq-sato', index: 1, name: 'Sato' }] },
    });
    const allText = collectText(tree);
    expect(allText).not.toMatch(/remove/i);
    expect(allText).not.toMatch(/delete member/i);
  });

  it('Save omits memberIds entirely when no position resolved to a squad member', async () => {
    const tree = await mountFor({ id: 'team-1', name: 'Tora A', number: 'T10' }, { squads: {} });
    mainSaveButton(tree).props.onClick();
    await flush();
    const call = global.window.API.putTeamLineup.mock.calls.at(-1);
    // putTeamLineup(compId, teamId, round, positionsOut, password, memberIdsOut)
    expect(call[5]).toBeUndefined();
  });

  // bc-cse gap closure: make a silent failure visible, without ever blocking.
  const memberWarning = (tree) =>
    findHosts(tree, 'div').find(d => d.props?.['data-testid'] === 'lineup-member-warning');

  it('shows the squad-unavailable warning after a save that still succeeded, when the squad failed to load', async () => {
    global.window.API.fetchSquads.mockRejectedValue(new Error('network error'));
    const tree = await mountFor({ id: 'team-1', name: 'Tora A', number: 'T10' });

    mainSaveButton(tree).props.onClick();
    await flush();

    const tree2 = runtime.currentTree();
    // The save itself still succeeded (never blocked by the squad failure).
    expect(global.window.API.putTeamLineup).toHaveBeenCalled();
    const warning = memberWarning(tree2);
    expect(warning).toBeTruthy();
    const text = collectText(warning);
    expect(text).toContain('Lineup saved');
    expect(text).toContain('squad list could not be loaded');
    expect(text).toContain('Scores will still record normally');
  });

  it('shows no member-identity warning after an ordinary successful save (squad loaded fine)', async () => {
    const tree = await mountFor({ id: 'team-1', name: 'Tora A', number: 'T10' }, {
      squads: { 'team-1': [{ id: 'sq-sato', index: 1, name: 'Sato' }] },
    });
    positionSelect(tree, '1').props.onChange({ target: { value: 'sq-sato' } });
    const tree2 = runtime.currentTree();
    mainSaveButton(tree2).props.onClick();
    await flush();
    const tree3 = runtime.currentTree();
    expect(memberWarning(tree3)).toBeFalsy();
  });
});
