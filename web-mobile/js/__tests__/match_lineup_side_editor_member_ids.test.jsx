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
  const TEAM = { id: 'uuid-grouped', name: 'Grouped Team', number: 'T5' };
  const MATCH = { id: 'match-1', compId: 'comp-1', sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'other', name: 'Other' }, status: 'scheduled' };
  // A 7-slot squad: 3 named members (index 1-3) and 4 blank reserve slots
  // (index 4-7, bc-dnst's SquadReserveSlots), the shape the row's list must
  // now show every one of (CLAUDE.md "Team Lineups & Kachinuki").
  const SQUAD_7 = Array.from({ length: 7 }, (_, i) => ({
    id: `mem-${i + 1}`,
    index: i + 1,
    name: i < 3 ? `Fighter ${i + 1}` : '',
  }));

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
      // The sixth argument is the panel's own memberIds map (bc-dnst): the
      // resolver attaches a typed name to the member the position already
      // holds before falling back to the slot seeded for it.
      .toHaveBeenCalledWith('comp-1', 'uuid-grouped', { 1: 'Sato' }, squadForTeam, 'pw', expect.any(Object));
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

  // bc-dnst: the "Enter lineup" panel offers every squad slot, blank ones
  // included, by number (squadMemberLabel), same as the score sheet's row
  // list -- not just already-named members.
  it('shows every squad slot, blank ones included, as a labelled entry in a position\'s list', async () => {
    global.window.API.fetchSquads = vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD_7 });

    const tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    const senpoRoster = pickers[0].props.roster;

    expect(senpoRoster.length).toBe(7);
    expect(senpoRoster.every(e => e.label)).toBe(true);
    expect(senpoRoster[0]).toMatchObject({ id: 'mem-1', name: 'Fighter 1', label: 'T5.1' });
    expect(senpoRoster[6]).toMatchObject({ id: 'mem-7', name: '', label: 'T5.7' });
  });

  it('a member picked at Senpo is not offered at Jiho', async () => {
    global.window.API.fetchSquads = vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD_7 });

    let tree = await mount();
    let pickers = findComponents(tree, 'LineupNameInput');
    // Pick SQUAD_7[0] (mem-1) at position "1" (Senpo): onSelect's second
    // argument is the whole squad-member entry, exactly what LineupNameInput
    // hands back for an object roster entry.
    pickers[0].props.onSelect('Fighter 1', SQUAD_7[0]);
    tree = runtime.currentTree();
    pickers = findComponents(tree, 'LineupNameInput');

    const jihoRoster = pickers[1].props.roster; // position "2"
    expect(jihoRoster.some(e => e && e.id === 'mem-1')).toBe(false);
    // Still offered at its OWN position, so re-opening Senpo's own picker
    // does not hide the member it currently holds.
    const senpoRoster = pickers[0].props.roster;
    expect(senpoRoster.some(e => e && e.id === 'mem-1')).toBe(true);
  });

  it('picking an entry writes its id in memberIds, without a resolver round trip', async () => {
    global.window.API.fetchSquads = vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD_7 });

    let tree = await mount();
    const pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Fighter 1', SQUAD_7[0]);
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();

    // The picked id is already known, so this position never goes through
    // resolveMemberIdsForPositions at all.
    expect(global.window.AdminLineupHelpers.resolveMemberIdsForPositions).not.toHaveBeenCalled();
    expect(global.window.API.putMatchLineup).toHaveBeenCalled();
    const call = global.window.API.putMatchLineup.mock.calls.at(-1);
    expect(call[3]).toEqual({ 1: 'Fighter 1' });
    expect(call[5]).toEqual({ 1: 'mem-1' });
  });

  // bc-cse: "Copy from previous match" can carry a source position that has
  // an id but no name yet (a fighter fielded by number, never typed). The
  // copy must still write that position (present, with an empty string) and
  // its id, and must not send it through the resolver at all: an empty name
  // never enters positionsForResolver, the id already known.
  it('copying a source position with an id and an empty name writes it directly, skipping the resolver', async () => {
    const MATCH_PREV = {
      id: 'match-0', compId: 'comp-1',
      sideA: { id: 'uuid-grouped', name: 'Grouped Team' }, sideB: { id: 'other', name: 'Other' },
      status: 'completed',
    };
    global.window.API.fetchMatchLineup = vi.fn((_compId, _teamId, matchId) => (
      matchId === 'match-0'
        ? Promise.resolve({ positions: { '1': '' }, memberIds: { '1': 'mem-1' } })
        : Promise.resolve(null)
    ));

    runtime.mount(MatchLineupSideEditor, {
      comp: COMP, team: TEAM, match: MATCH, allMatches: [MATCH, MATCH_PREV], password: 'pw', showToast: vi.fn(),
    });
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    let tree = runtime.currentTree();

    const copyBtn = findHosts(tree, 'button').find(b => /Copy from previous match/.test(collectText(b)));
    expect(copyBtn).toBeTruthy();
    copyBtn.props.onClick();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();

    expect(global.window.AdminLineupHelpers.resolveMemberIdsForPositions).not.toHaveBeenCalled();
    expect(global.window.API.putMatchLineup).toHaveBeenCalled();
    const call = global.window.API.putMatchLineup.mock.calls.at(-1);
    // positionsOut(3): the position is PRESENT with an empty string, not
    // omitted, because its id makes it a real placement (bc-dnst).
    expect(call[3]).toEqual({ '1': '' });
    // memberIdsOut(5): the copied id rides along unresolved.
    expect(call[5]).toEqual({ '1': 'mem-1' });
  });

  // bc-cse: typing a DIFFERENT name over a previously PICKED entry (its id
  // already known in the panel's own memberIds) must still resolve that
  // position through the shared resolver, passing the panel's memberIds
  // (still holding the picked id) as the sixth argument -- so the resolver
  // renames the picked member rather than minting a fresh, number-less one.
  it('typing a different name over a picked entry resolves through the panel\'s memberIds, which still holds the pick', async () => {
    global.window.API.fetchSquads = vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD_7 });
    global.window.AdminLineupHelpers.resolveMemberIdsForPositions =
      vi.fn().mockResolvedValue({ memberIds: { '1': 'mem-1' }, squad: SQUAD_7, failures: [] });

    let tree = await mount();
    let pickers = findComponents(tree, 'LineupNameInput');
    // Pick mem-1 (Fighter 1) into position "1" first.
    pickers[0].props.onSelect('Fighter 1', SQUAD_7[0]);
    tree = runtime.currentTree();
    pickers = findComponents(tree, 'LineupNameInput');
    // Then type a DIFFERENT name over it, with no entry (a free-typed override).
    pickers[0].props.onSelect('Yamada');
    tree = runtime.currentTree();

    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();

    expect(global.window.AdminLineupHelpers.resolveMemberIdsForPositions).toHaveBeenCalledWith(
      'comp-1', 'uuid-grouped', { '1': 'Yamada' }, SQUAD_7, 'pw',
      expect.objectContaining({ '1': 'mem-1' }),
    );
  });

  // bc-cse: clearing a picked entry (the roster's clear affordance: an
  // empty name, no entry) must remove its id from memberIds AND omit that
  // position from the write entirely -- it goes back to vacant, not to an
  // empty-string placement.
  it('clearing a picked entry removes its id and omits the position from the write', async () => {
    global.window.API.fetchSquads = vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD_7 });

    let tree = await mount();
    let pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Fighter 1', SQUAD_7[0]);
    tree = runtime.currentTree();
    pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('');
    tree = runtime.currentTree();

    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();

    expect(global.window.AdminLineupHelpers.resolveMemberIdsForPositions).not.toHaveBeenCalled();
    expect(global.window.API.putMatchLineup).toHaveBeenCalled();
    const call = global.window.API.putMatchLineup.mock.calls.at(-1);
    expect(call[3]).toEqual({});
    expect(call[5]).toBeUndefined();
  });

  // bc-dnst (operator decision 2026-09-15): a member's name can be corrected
  // from this panel through an explicit Rename door under the position, the
  // same rename the Lineups page offers; typing into the picker over a named
  // member stays a substitution. A blank pick gets no Rename: it is named by
  // typing into the box.
  it('Rename under a named pick renames that member and the next save writes the new name with the same id', async () => {
    global.window.API.fetchSquads = vi.fn().mockResolvedValue({ 'uuid-grouped': SQUAD_7 });
    global.window.API.renameTeamMember = vi.fn().mockResolvedValue({ id: 'mem-1', index: 1, name: 'Fighter One' });

    let tree = await mount();
    let pickers = findComponents(tree, 'LineupNameInput');
    pickers[0].props.onSelect('Fighter 1', SQUAD_7[0]);
    pickers = findComponents(runtime.currentTree(), 'LineupNameInput');
    pickers[1].props.onSelect('', SQUAD_7[4]);
    tree = runtime.currentTree();

    const renameButtons = findHosts(tree, 'button').filter(b => /^Rename \d player$/.test(b.props?.['aria-label'] || ''));
    expect(renameButtons.map(b => b.props['aria-label'])).toEqual(['Rename 1 player']);

    renameButtons[0].props.onClick();
    tree = runtime.currentTree();
    const input = findHosts(tree, 'input').find(i => i.props?.['aria-label'] === 'Rename 1 player');
    expect(input).toBeTruthy();
    expect(input.props.value).toBe('Fighter 1');
    input.props.onChange({ target: { value: 'Fighter One' } });
    tree = runtime.currentTree();
    findHosts(tree, 'button').find(b => /^Save$/.test(collectText(b).trim())).props.onClick();
    await Promise.resolve();
    await Promise.resolve();

    expect(global.window.API.renameTeamMember).toHaveBeenCalledWith('comp-1', 'uuid-grouped', 'mem-1', 'Fighter One', 'pw');
    tree = runtime.currentTree();
    expect(findComponents(tree, 'LineupNameInput')[0].props.value).toBe('Fighter One');

    saveButton(tree).props.onClick();
    await Promise.resolve();
    await Promise.resolve();
    expect(global.window.AdminLineupHelpers.resolveMemberIdsForPositions).not.toHaveBeenCalled();
    const call = global.window.API.putMatchLineup.mock.calls.at(-1);
    expect(call[3]).toEqual({ 1: 'Fighter One', 2: '' });
    expect(call[5]).toEqual({ 1: 'mem-1', 2: 'mem-5' });
  });
});
