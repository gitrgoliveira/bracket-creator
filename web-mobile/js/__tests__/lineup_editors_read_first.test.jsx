// Both lineup editors (the at-court panel's MatchLineupSideEditor and the Lineups
// page's AdminLineup) write a lineup only after it was READ. Before, a read that
// failed left an empty form with Save live: on the starting lineup (always
// saveable) one tap PUT {} over round 0 and every match carrying it lost its
// names, and on a match one pick wrote a partial lineup of its own that later
// matches then carried.
//
// This runtime calls the handlers straight off the vnodes, so it checks that
// save() itself refuses, not only that the button is disabled. It cannot see
// inside a child component, so the two the editors hand their source line and
// their read problem to are called by name (both are hook-free).

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
const findHosts = (tree, type) => { const out = []; walk(tree, (n) => { if (n && n.type === type) out.push(n); }); return out; };
const findComponents = (tree, name) => {
  const out = [];
  walk(tree, (n) => { if (n && typeof n.type === 'function' && n.type.name === name) out.push(n); });
  return out;
};
const allText = (tree) => collectText(tree, expandNamed('LineupSourceLine', 'LineupProblem'));
// A hook-free child component's own output.
const rendered = (node) => node.type(node.props);

const SQUAD = [
  { id: 'mem-1', index: 1, name: 'Aoki' },
  { id: 'mem-2', index: 2, name: 'Sato' },
  { id: 'mem-3', index: 3, name: 'Ito' },
  { id: 'mem-4', index: 4, name: 'Mori' },
];
const NAMES = { positions: { 1: 'Aoki', 2: 'Sato', 3: 'Ito' }, memberIds: { 1: 'mem-1', 2: 'mem-2', 3: 'mem-3' } };
const OWN = { ...NAMES, sourceMatchId: 'Pool D-1', saved: true };
const CARRIED = { ...NAMES, sourceMatchId: 'Pool D-0', saved: true };
const STARTING = { ...NAMES, sourceRound: 0, saved: true };
const COMP = { id: 'comp-1', name: 'Team Event', kind: 'team', teamSize: 3 };
const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };

let runtime;
let saved;

function install(api) {
  saved = {
    API: window.API, helpers: window.AdminLineupHelpers, compMatches: window.compMatches,
    confirmDialog: window.confirmDialog,
  };
  window.compMatches = () => [];
  window.confirmDialog = vi.fn().mockResolvedValue(true);
  window.AdminLineupHelpers = {
    positionsForSize: (n) => Array.from({ length: n }, (_, i) => ({ key: String(i + 1), label: String(i + 1) })),
    rosterFor: () => [],
    mergeRosterWithAssigned: (base) => (Array.isArray(base) ? base : []),
    teamIdOf: (t) => t?.id || t?.name || '',
    resolveMemberIdsForPositions: vi.fn().mockResolvedValue({ memberIds: {}, squad: [], failures: [] }),
    memberIdentityWarning: () => '',
  };
  window.API = api;
  runtime = makeReactive();
  global.React = runtime.React;
  vi.resetModules();
}

function restore() {
  runtime.unmount();
  global.React = realReact;
  window.API = saved.API;
  window.AdminLineupHelpers = saved.helpers;
  window.compMatches = saved.compMatches;
  window.confirmDialog = saved.confirmDialog;
  vi.resetModules();
}

describe('MatchLineupSideEditor (the at-court panel)', () => {
  const TEAM = { id: 'uuid-grouped', name: 'Grouped Team', number: 'T5' };
  const MATCH = {
    id: 'Pool D-1', compId: 'comp-1', phase: 'pool', poolName: 'Pool D',
    sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'other', name: 'Other' }, status: 'scheduled',
  };
  let MatchLineupSideEditor;
  let api;

  beforeEach(async () => {
    api = {
      fetchLineupInForce: vi.fn().mockResolvedValue(CARRIED),
      fetchSquads: vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD }),
      putMatchLineup: vi.fn().mockResolvedValue({ positions: {} }),
      deleteMatchLineup: vi.fn().mockResolvedValue(true),
    };
    install(api);
    ({ MatchLineupSideEditor } = await import('../admin_schedule_lineup.jsx'));
  });
  afterEach(restore);

  async function mount() {
    runtime.mount(MatchLineupSideEditor, {
      comp: COMP, team: TEAM, match: MATCH, allMatches: [MATCH], password: 'pw', showToast: vi.fn(),
    });
    await flush();
    return runtime.currentTree();
  }
  const saveButton = (tree) => findHosts(tree, 'button').find((b) => /Save lineup/.test(collectText(b)));
  const pickers = (tree) => findComponents(tree, 'LineupNameInput');
  const tryAgain = (tree) => {
    const problem = findComponents(tree, 'LineupProblem')[0];
    return findHosts(rendered(problem), 'button').find((b) => collectText(b) === 'Try again');
  };

  it('after a failed read shows why, and Save, the boxes and a direct save all refuse', async () => {
    api.fetchLineupInForce.mockRejectedValue(new Error('competition not found'));
    let tree = await mount();
    expect(allText(tree)).toContain('competition not found');
    expect(saveButton(tree).props.disabled).toBe(true);
    expect(pickers(tree).every((p) => p.props.disabled)).toBe(true);

    // The boxes are off in the page, but the callback is still reachable: what is
    // typed there must not make anything saveable.
    pickers(tree)[0].props.onSelect('Mori', SQUAD[3]);
    tree = runtime.currentTree();
    expect(saveButton(tree).props.disabled).toBe(true);
    saveButton(tree).props.onClick();
    await flush();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
    // The problem is still there after the attempt.
    expect(allText(runtime.currentTree())).toContain('competition not found');
  });

  it('shows no source for a lineup that was not read', async () => {
    api.fetchLineupInForce.mockRejectedValue(new Error('down'));
    const tree = await mount();
    expect(allText(tree)).not.toContain('No lineup saved yet');
  });

  it('reads again on Try again, and then saves a change', async () => {
    api.fetchLineupInForce.mockRejectedValueOnce(new Error('down')).mockResolvedValue(CARRIED);
    let tree = await mount();
    tryAgain(tree).props.onClick();
    await flush();
    tree = runtime.currentTree();

    expect(api.fetchLineupInForce).toHaveBeenCalledTimes(2);
    expect(allText(tree)).not.toContain('down');
    expect(allText(tree)).toContain('Same as');
    expect(pickers(tree).some((p) => p.props.disabled)).toBe(false);
    pickers(tree)[1].props.onSelect('Mori', SQUAD[3]);
    expect(saveButton(runtime.currentTree()).props.disabled).toBe(false);
    saveButton(runtime.currentTree()).props.onClick();
    await flush();
    expect(api.putMatchLineup).toHaveBeenCalledTimes(1);
  });

  it('after a removal whose re-read fails, shows an empty form that cannot be saved and says so', async () => {
    api.fetchLineupInForce.mockResolvedValueOnce(OWN).mockRejectedValue(new Error('down'));
    let tree = await mount();
    expect(allText(tree)).toContain('Lineup for this match');
    const line = findComponents(tree, 'LineupSourceLine')[0];

    await line.props.form.dropOwnLineup();
    await flush();
    tree = runtime.currentTree();

    expect(api.deleteMatchLineup).toHaveBeenCalledWith('comp-1', 'uuid-grouped', 'Pool D-1', 'pw');
    expect(allText(tree)).toContain('Removed. The lineup this match now uses could not be read: try again.');
    expect(allText(tree)).not.toContain('Lineup for this match');
    expect(pickers(tree).map((p) => p.props.value)).toEqual(['', '', '']);
    expect(saveButton(tree).props.disabled).toBe(true);
    saveButton(tree).props.onClick();
    await flush();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });
});

describe('AdminLineup (the Lineups page)', () => {
  const TEAM = { id: 'team-1', name: 'Tora A', number: 'T10' };
  let AdminLineup;
  let api;

  beforeEach(async () => {
    api = {
      fetchTeamLineup: vi.fn().mockResolvedValue(STARTING),
      fetchLineupInForce: vi.fn().mockResolvedValue(CARRIED),
      fetchSquads: vi.fn().mockResolvedValue({ 'team-1': SQUAD }),
      putTeamLineup: vi.fn().mockResolvedValue({}),
      putMatchLineup: vi.fn().mockResolvedValue({}),
      deleteMatchLineup: vi.fn().mockResolvedValue(true),
    };
    install(api);
    ({ AdminLineup } = await import('../admin_lineup.jsx'));
  });
  afterEach(restore);

  async function mount(extra = {}) {
    runtime.mount(AdminLineup, {
      comp: COMP, team: TEAM, password: 'pw', showToast: vi.fn(), onClose: vi.fn(), ...extra,
    });
    await flush();
    return runtime.currentTree();
  }
  const saveButton = (tree) => findHosts(tree, 'button').find((b) => /^Save lineup$/.test(collectText(b).trim()));
  const select = (tree, key) => findHosts(tree, 'select').find((s) => s.props?.['data-testid'] === `lineup-position-${key}`);
  const tryAgain = (tree) => {
    const problem = findComponents(tree, 'LineupProblem')[0];
    return findHosts(rendered(problem), 'button').find((b) => collectText(b) === 'Try again');
  };

  it('a starting lineup that could not be read is never written: one tap on Save writes nothing', async () => {
    api.fetchTeamLineup.mockRejectedValue(new Error('Failed to load lineup'));
    const tree = await mount();
    expect(allText(tree)).toContain('Failed to load lineup');
    expect(saveButton(tree).props.disabled).toBe(true);
    expect(select(tree, '1').props.disabled).toBe(true);

    saveButton(tree).props.onClick();
    await flush();

    expect(api.putTeamLineup).not.toHaveBeenCalled();
    expect(allText(runtime.currentTree())).toContain('Failed to load lineup');
  });

  it('nor does a pick on it make it saveable', async () => {
    api.fetchTeamLineup.mockRejectedValue(new Error('down'));
    const tree = await mount();
    select(tree, '1').props.onChange({ target: { value: 'mem-4' } });
    const after = runtime.currentTree();
    expect(saveButton(after).props.disabled).toBe(true);
    saveButton(after).props.onClick();
    await flush();
    expect(api.putTeamLineup).not.toHaveBeenCalled();
  });

  it('a match whose lineup could not be read is never written either', async () => {
    api.fetchLineupInForce.mockRejectedValue(new Error('down'));
    const tree = await mount({ matchId: 'Pool D-1', matchLabel: 'Pool D · Match 2', allMatches: [] });
    select(tree, '2').props.onChange({ target: { value: 'mem-4' } });
    const after = runtime.currentTree();
    expect(saveButton(after).props.disabled).toBe(true);
    saveButton(after).props.onClick();
    await flush();
    expect(api.putMatchLineup).not.toHaveBeenCalled();
  });

  it('reads again on Try again, and then the form works', async () => {
    api.fetchTeamLineup.mockRejectedValueOnce(new Error('down')).mockResolvedValue(STARTING);
    let tree = await mount();
    tryAgain(tree).props.onClick();
    await flush();
    tree = runtime.currentTree();

    expect(api.fetchTeamLineup).toHaveBeenCalledTimes(2);
    expect(allText(tree)).not.toContain('down');
    expect(select(tree, '1').props.disabled).toBeFalsy();
    expect(select(tree, '1').props.value).toBe('mem-1');
    select(tree, '1').props.onChange({ target: { value: 'mem-4' } });
    expect(saveButton(runtime.currentTree()).props.disabled).toBe(false);
  });

  it('an untouched starting lineup that WAS read has nothing to save: a direct save writes nothing', async () => {
    const tree = await mount();
    expect(saveButton(tree).props.disabled).toBe(true);
    expect(saveButton(tree).props.title).toBe('No changes to save');
    saveButton(tree).props.onClick();
    await flush();
    expect(api.putTeamLineup).not.toHaveBeenCalled();
  });
});
