// bc-rvfx: Aka's member label used to be clipped away by the name cell's own
// ellipsis.
//
// A bout row's name cell (.msb-name) is a single-line text run with
// `overflow: hidden; text-overflow: ellipsis; white-space: nowrap`. The member
// label sits on the OUTER side of the name (operator ruling 2026-09-14,
// bc-dnst): BEFORE it on Shiro, AFTER it on Aka. An ellipsis truncates the END
// of a run, so with both children inside one run a long name ate AKA's label
// while Shiro's leading one always survived -- the two sides degraded
// differently from the same fixture.
//
// The fix mirrors the competitor-number clip fix: the cell becomes a flex row
// (.msb-name--labelled) whose NAME child (.msb-name__text) is the only
// shrinkable part, so the label survives on both sides and the name ellipsises
// instead.
//
// WHAT THIS FILE CAN AND CANNOT PIN: jsdom does no layout, so it cannot
// measure a clip. It pins the STRUCTURE the fix depends on -- the label is a
// SIBLING of the ellipsised text node, never inside it, and the cell carries
// the class that makes it a flex row. The pixels are verified by screenshot.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findInTree } from './helpers/vdom.js';

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

const hasClass = (n, cls) =>
  typeof n?.props?.className === 'string' && n.props.className.split(/\s+/).includes(cls);

describe('BoutSubRow: the member label is never inside the ellipsised text run', () => {
  let runtime, BoutSubRow;

  beforeEach(async () => {
    let mod;
    ({ runtime, mod } = await setupSuite());
    BoutSubRow = mod.BoutSubRow;
  });
  afterEach(() => teardownSuite(runtime));

  // Both sides field a squad member, so BOTH rows carry a label and the two
  // can be compared against each other.
  function bothSidesLabelled() {
    const squadA = [{ id: 'mem-a', index: 3, name: 'Yamamoto-Nakashima' }];
    const squadB = [{ id: 'mem-b', index: 1, name: 'Suzuki' }];
    const sub = {
      position: 1,
      sideA: 'Yamamoto-Nakashima', sideAMemberId: 'mem-a',
      sideB: 'Suzuki', sideBMemberId: 'mem-b',
      ipponsA: ['M'], ipponsB: [],
    };
    return runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: null, lineupB: null, teamSize: 5,
      squadA, squadB, numberA: 'T11', numberB: 'T10',
    });
  }

  it('puts the Aka name in its own shrinkable child, with the label outside it', () => {
    const tree = bothSidesLabelled();

    const akaText = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    expect(akaText).toBeTruthy();
    // The name is the only shrinkable child, so it is what ellipsises.
    expect(hasClass(akaText, 'msb-name__text')).toBe(true);

    // The label must NOT be a descendant of that text node: inside it, the
    // cell's ellipsis would truncate the label away with the name.
    const labelInsideText = findInTree(akaText, n => n?.props?.['data-testid'] === 'sub-member-label-a');
    expect(labelInsideText).toBeNull();

    // It is present as a sibling, in the row.
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-member-label-a')).toBeTruthy();
  });

  it('marks BOTH name cells as labelled rows, so neither side clips its label', () => {
    const tree = bothSidesLabelled();

    const akaCell = findInTree(tree, n => hasClass(n, 'msb-name') && hasClass(n, 'msb-name--aka'));
    const shiroCell = findInTree(tree, n => hasClass(n, 'msb-name') && !hasClass(n, 'msb-name--aka'));

    expect(akaCell).toBeTruthy();
    expect(shiroCell).toBeTruthy();
    // --labelled is what makes the cell a flex row rather than one text run.
    expect(hasClass(akaCell, 'msb-name--labelled')).toBe(true);
    expect(hasClass(shiroCell, 'msb-name--labelled')).toBe(true);
  });

  it('wraps the Shiro name the same way, so the two sides degrade alike', () => {
    const tree = bothSidesLabelled();

    const shiroText = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    expect(shiroText).toBeTruthy();
    expect(hasClass(shiroText, 'msb-name__text')).toBe(true);
    expect(findInTree(shiroText, n => n?.props?.['data-testid'] === 'sub-member-label-b')).toBeNull();
  });
});
