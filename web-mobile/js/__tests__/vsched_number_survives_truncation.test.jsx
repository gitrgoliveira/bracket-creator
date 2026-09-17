// bc-dnst: the competitor number must survive truncation on the viewer
// schedule row, on BOTH sides.
//
// The number moved to the OUTER side of the name (operator ruling
// 2026-09-14), which for Aka means last. `.vsched-item__side .n` is a
// nowrap-ellipsis box (styles.css), so while the row rendered withNumber's
// flat STRING the ellipsis ate whatever sat last -- Aka's number. Measured in
// a real browser at 360px, every Aka name tested lost its number outright;
// only a short name at 402px survived. Shiro was never affected, its number
// being first.
//
// The fix hands the cell NumberedName in `clip` mode, whose CSS puts the
// ellipsis on the NAME child alone so the number chip is never what gets cut.
// jsdom cannot measure truncation, and this file's harness renders one level
// deep, so the pin here is the thing a regression would actually undo: the
// cell's child must be the NumberedName COMPONENT carrying `clip`, not a
// pre-joined string. Reverting either side to withNumber makes that child a
// string and reddens these tests.
//
// numberedParts is pinned directly as well, since it is the single source both
// renderers read and a drift there is what produced the bug.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findAll, hasClass } from './helpers/vdom.js';

const realReact = global.React;

const SHIRO = { id: 'p-shiro', name: 'KOBAYASHI HIROSHI', number: 'K1' };
const AKA = { id: 'p-aka', name: 'YAMAMOTO TAKESHIRO', number: 'K2' };

describe('VSchedItem: the competitor number is a chip, not trailing text', () => {
  let runtime, VSchedItem, numberedParts;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    global.window.matchScoreStr = vi.fn(() => '');
    global.window.boutMiddle = vi.fn(() => 'vs');
    global.window.queueLabelCompact = null;
    vi.resetModules();
    ({ numberedParts } = await import('../match_scoreboard.jsx'));
    ({ VSchedItem } = await import('../viewer_match.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete global.window.matchScoreStr;
    delete global.window.boutMiddle;
    delete global.window.queueLabelCompact;
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // The name cells, in DOM order: Shiro first, then Aka.
  const nameCells = () => {
    const m = { id: 'm1', status: 'scheduled', court: 'A', sideA: AKA, sideB: SHIRO };
    const tree = runtime.mount(VSchedItem, { m, tweaks: {} });
    return findAll(tree, n => hasClass(n, 'n')).map(n => n.props && n.props.children);
  };

  it('hands each side a NumberedName in clip mode, never a joined string', () => {
    const [shiroChild, akaChild] = nameCells();
    for (const child of [shiroChild, akaChild]) {
      // A string here is the regression: it means the number was joined onto
      // the name before it reached the ellipsising cell.
      expect(typeof child).not.toBe('string');
      expect(child && child.props && child.props.clip).toBe(true);
    }
  });

  it('passes the number as its own prop, on the correct side of each name', () => {
    const [shiroChild, akaChild] = nameCells();
    expect(shiroChild.props.side).toBe('shiro');
    expect(shiroChild.props.number).toBe('K1');
    expect(shiroChild.props.name).toBe('KOBAYASHI HIROSHI');
    expect(akaChild.props.side).toBe('aka');
    expect(akaChild.props.number).toBe('K2');
    expect(akaChild.props.name).toBe('YAMAMOTO TAKESHIRO');
    // The name prop must stay free of the number, or the chip renders it twice.
    expect(akaChild.props.name).not.toContain('K2');
  });

  it('numberedParts splits name from number for every side shape', () => {
    expect(numberedParts(AKA)).toEqual({ name: 'YAMAMOTO TAKESHIRO', number: 'K2' });
    // Zekken mode prefers displayName, matching withNumber.
    expect(numberedParts({ name: 'TANAKA ICHIRO', displayName: 'TANAKA', number: 'K3' }, true))
      .toEqual({ name: 'TANAKA', number: 'K3' });
    // A side with no number yields an empty number, so NumberedName renders no chip.
    expect(numberedParts({ name: 'SATO' })).toEqual({ name: 'SATO', number: '' });
    // Unresolved shapes degrade exactly as withNumber did.
    expect(numberedParts(null)).toEqual({ name: 'TBD', number: '' });
    expect(numberedParts('Winner of M1')).toEqual({ name: 'Winner of M1', number: '' });
  });
});
