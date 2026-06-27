// AdminLineup (competition-admin → Lineups) form behaviour.
//
// Regression guard: a team with NO member metadata used to render a fixed
// <select> limited to roster members, with the Save button disabled when the
// roster was empty — so for teams formed by grouping individuals (no metadata)
// the operator could not enter ANY lineup. The form now uses a free-text
// combobox (<input list> + <datalist>) so any name can be typed, and Save is
// no longer gated on the roster being non-empty.

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

const positionInputs = (tree) =>
  findHosts(tree, 'input').filter(n => String(n.props?.['data-testid'] || '').startsWith('lineup-position-'));

const saveButton = (tree) =>
  findHosts(tree, 'button').find(b => /Save lineup/.test(collectText(b)));

describe('AdminLineup form (competition-admin Lineups)', () => {
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
      putTeamLineup: vi.fn().mockResolvedValue({ lockedAt: null }),
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

  async function mountFor(team) {
    runtime.mount(AdminLineup, {
      comp: COMP, team, round: 0, password: 'pw', showToast: vi.fn(), onClose: vi.fn(),
    });
    // Let the mount effect's fetchTeamLineup promise resolve and flip loading.
    await Promise.resolve();
    await Promise.resolve();
    return runtime.currentTree();
  }

  it('renders free-text comboboxes (not fixed selects) for a team WITHOUT metadata', async () => {
    const tree = await mountFor({ id: 'uuid-grouped', name: 'Grouped Team' });

    // One input per position (teamSize 3), each a text combobox bound to a datalist.
    const inputs = positionInputs(tree);
    expect(inputs.length).toBe(3);
    for (const inp of inputs) {
      expect(inp.props.type).toBe('text');
      expect(inp.props.list).toBeTruthy();
    }
    // No <select> position controls remain.
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

  it('offers the registered roster as datalist suggestions when metadata exists', async () => {
    const tree = await mountFor({
      id: 'uuid-meta', name: 'Tora', metadata: ['Tanaka', 'Sato', 'Yamada'],
    });
    const optionValues = findHosts(tree, 'datalist')
      .flatMap(dl => findHosts(dl, 'option'))
      .map(o => o.props?.value);
    expect(optionValues).toContain('Tanaka');
    expect(optionValues).toContain('Sato');
    expect(optionValues).toContain('Yamada');
    // Save is enabled here too.
    expect(saveButton(tree).props.disabled).toBeFalsy();
  });

  it('sanitizes the datalist id when teamId falls back to a name with spaces/punctuation', async () => {
    // No id on the team → teamIdOf falls back to the (messy) name.
    const tree = await mountFor({ name: 'Team Bravo! (A)' });
    const inputs = positionInputs(tree);
    expect(inputs.length).toBeGreaterThan(0);
    for (const inp of inputs) {
      // A valid HTML id: no whitespace or punctuation that would break list binding.
      expect(inp.props.list).toMatch(/^[A-Za-z0-9_-]+$/);
    }
  });

  it('trims leading/trailing whitespace before saving (Save without blur)', async () => {
    let tree = await mountFor({ id: 'uuid-x', name: 'Grouped' });
    const inp = positionInputs(tree).find(n => n.props['data-testid'] === 'lineup-position-1');
    inp.props.onChange({ target: { value: '  Padded Name  ' } });
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    expect(global.window.API.putTeamLineup).toHaveBeenCalled();
    // putTeamLineup(compId, teamId, round, positionsOut, password)
    const positionsOut = global.window.API.putTeamLineup.mock.calls.at(-1)[3];
    expect(positionsOut['1']).toBe('Padded Name');
  });

  it('drops a whitespace-only position instead of persisting blanks', async () => {
    let tree = await mountFor({ id: 'uuid-x', name: 'Grouped' });
    const inp = positionInputs(tree).find(n => n.props['data-testid'] === 'lineup-position-1');
    inp.props.onChange({ target: { value: '   ' } });
    tree = runtime.currentTree();
    saveButton(tree).props.onClick();
    await Promise.resolve();
    const positionsOut = global.window.API.putTeamLineup.mock.calls.at(-1)[3];
    expect(positionsOut['1']).toBeUndefined();
  });
});
