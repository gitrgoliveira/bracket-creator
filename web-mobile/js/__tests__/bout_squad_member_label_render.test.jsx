// bc-pnum: extend the squad member label to the PUBLIC surfaces (operator
// ruling: "the label must be visible everywhere, together with the name").
//
// This pins the RENDERED output of BoutSubRow (match_scoreboard.jsx), the
// shared component every public team-match surface (viewer card, TV display,
// streaming overlay) renders bout rows through. The pure resolution chain
// (resolveBoutSideMemberId / resolveSquadMember / squadMemberLabel /
// resolveBoutSideSquadLabel) is already pinned in bout_side_squad_member.test.jsx
// and lineup_resolver-level tests; this file exercises the same chain as an
// operator would actually see it, composed inside the real row component,
// mirroring kachinuki_scoreboard.test.jsx's mount pattern for the same
// component.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findInTree, collectText } from './helpers/vdom.js';

const realReact = global.React;

async function setupSuite() {
  const runtime = makeReactive();
  global.React = runtime.React;
  global.window = global.window || {};
  global.window.isHikiwake = vi.fn((t) => t === 'hikiwake');
  vi.resetModules();
  const mod = await import('../match_scoreboard.jsx');
  return { runtime, mod };
}

function teardownSuite(runtime) {
  runtime.unmount();
  global.React = realReact;
  delete global.window.isHikiwake;
  vi.restoreAllMocks();
  vi.resetModules();
}

describe('BoutSubRow: squad member label on public surfaces', () => {
  let runtime, BoutSubRow;

  beforeEach(async () => {
    let mod;
    ({ runtime, mod } = await setupSuite());
    BoutSubRow = mod.BoutSubRow;
  });
  afterEach(() => teardownSuite(runtime));

  const shiroLabel = (tree) => findInTree(tree, n => n?.props?.['data-testid'] === 'sub-member-label-b');
  const akaLabel = (tree) => findInTree(tree, n => n?.props?.['data-testid'] === 'sub-member-label-a');
  const shiroName = (tree) => collectText(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name'));
  const akaName = (tree) => collectText(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name'));

  it('resolves by member id: shows that member\'s label beside the name', () => {
    const squadB = [{ id: 'mem-1', index: 1, name: 'Suzuki' }];
    const sub = { position: 1, sideB: 'Suzuki', sideBMemberId: 'mem-1', ipponsB: ['M'], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: null, lineupB: null, teamSize: 5,
      squadB, numberB: 'T10',
    });
    expect(collectText(shiroLabel(tree))).toBe('T10.1');
    expect(shiroName(tree)).toBe('Suzuki');
  });

  it('resolves by name only (no member id on the row): still shows the label', () => {
    // No sideBMemberId anywhere, and the lineup carries no memberIds map at
    // all (a lineup saved before squads existed) -- only the NAME resolves.
    const squadB = [{ id: 'mem-1', index: 2, name: 'Tanaka' }];
    const lineupB = { positions: { senpo: 'Tanaka' } };
    const sub = { position: 1, ipponsB: [], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: null, lineupB, teamSize: 5,
      squadB, numberB: 'T10',
    });
    expect(shiroName(tree)).toBe('Tanaka');
    expect(collectText(shiroLabel(tree))).toBe('T10.2');
  });

  it('a fighter matching no squad member: shows the bare name, no label, no stray separator', () => {
    const squadB = [{ id: 'mem-1', index: 1, name: 'Suzuki' }];
    const sub = { position: 1, sideB: 'Guest Fighter', ipponsB: [], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: null, lineupB: null, teamSize: 5,
      squadB, numberB: 'T10',
    });
    expect(shiroLabel(tree)).toBeNull();
    expect(shiroName(tree)).toBe('Guest Fighter');
  });

  it('a team with no competitor number: shows bare names even for a resolved squad member', () => {
    const squadB = [{ id: 'mem-1', index: 1, name: 'Suzuki' }];
    const sub = { position: 1, sideB: 'Suzuki', sideBMemberId: 'mem-1', ipponsB: [], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: null, lineupB: null, teamSize: 5,
      squadB, // numberB omitted -> defaults to ""
    });
    expect(shiroLabel(tree)).toBeNull();
    expect(shiroName(tree)).toBe('Suzuki');
  });

  it('a blank-named squad member (an unfilled position) never produces a label for a fighter', () => {
    // squads.yaml's wire shape: a member with a BLANK name is a normal
    // unfilled position, not an absence. It must never be mistaken for a
    // match against a fighter whose own name happens to be empty/absent --
    // here the fighter has no recorded name and no lineup pick at all, so
    // BoutSubRow falls back to the bout number ("#3"), which the blank-named
    // slot (and the other, unrelated real member) must not match.
    const squadB = [{ id: 'mem-1', index: 1, name: '' }, { id: 'mem-2', index: 2, name: 'Tanaka' }];
    const sub = { position: 3, ipponsB: [], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 2, lineupA: null, lineupB: null, teamSize: 5,
      squadB, numberB: 'T10',
    });
    expect(shiroLabel(tree)).toBeNull();
    expect(shiroName(tree)).toBe('#3');
  });

  it('threads the same resolution to the Aka side independently', () => {
    const squadA = [{ id: 'mem-9', index: 5, name: 'Ito' }];
    const sub = { position: 1, sideA: 'Ito', sideAMemberId: 'mem-9', ipponsA: ['K'], ipponsB: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: null, lineupB: null, teamSize: 5,
      squadA, numberA: 'T3',
    });
    expect(collectText(akaLabel(tree))).toBe('T3.5');
    expect(akaName(tree)).toBe('Ito');
    // The untouched Shiro side gets no label at all (no squadB/numberB passed).
    expect(shiroLabel(tree)).toBeNull();
  });

  it('a caller that passes no squad props at all renders exactly as before (no label, no crash)', () => {
    const sub = { position: 1, sideA: 'Aka Player', sideB: 'Shiro Player', ipponsB: ['M'], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    expect(shiroLabel(tree)).toBeNull();
    expect(akaLabel(tree)).toBeNull();
    expect(shiroName(tree)).toBe('Shiro Player');
    expect(akaName(tree)).toBe('Aka Player');
  });
});
