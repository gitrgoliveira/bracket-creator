import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';

// bc-pnum: BracketConnectorsMeta must actually CALL elbowXFor/connectorPath
// with the gap-before-parent value, not just define them. A pure-function
// unit test on elbowXFor/connectorPath in isolation (bracket_connector_elbow.test.jsx)
// cannot pin that: a mutation that restores the old `(fRight + mLeft) / 2`
// directly at the out.push call site -- leaving the helpers defined but
// unused for that value -- keeps such a test green. This render test mounts
// the real component and reads the SVG `d` attributes it actually produces.

let BracketTree;

beforeAll(async () => {
  // BracketTree measures card geometry for its connectors; jsdom has no
  // ResizeObserver.
  if (typeof globalThis.ResizeObserver === 'undefined') {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
  await import('../../bracket.jsx');
  BracketTree = window.BracketTree;
});

// Column geometry mirrors the real render measured in bc-pnum: .bc-round is
// 230px wide (CSS min-width) with a 56px gap between columns (.bc-tree's
// flex gap), so each column starts 286px after the previous one. m-r1-0 /
// m-r2-0 / m-r3-0 below stand in for that render's M1 / M5 / M7.
const GEOM = {
  'm-r1-0': { left: 0, right: 230, anchorY: 99 },
  'm-r2-0': { left: 286, right: 516, anchorY: 234.75 },
  'm-r3-0': { left: 572, right: 802, anchorY: 166.875 },
};

// jsdom has no layout engine, so every getBoundingClientRect() is zero by
// default. This monkey-patches it to return the fixed geometry above for any
// element inside a card carrying `data-match-id` (the .bc-match button and
// its .bc-side children), and falls through to jsdom's real (zero) rect for
// everything else -- in particular the .bc-tree root itself, keeping
// tree-relative math simple (treeRect.top === treeRect.left === 0). Both
// .bc-side slots are pinned to the SAME y so anchorY()'s
// (first.top + last.bottom) / 2 lands exactly on the fixture's anchor
// regardless of which side (sideA/sideB) is being measured.
function installGeometryMock() {
  const original = Element.prototype.getBoundingClientRect;
  Element.prototype.getBoundingClientRect = function mockGetBoundingClientRect() {
    const matchEl = this.closest && this.closest('[data-match-id]');
    const g = matchEl && GEOM[matchEl.getAttribute('data-match-id')];
    if (!g) return original.call(this);
    const isSide = this.classList && (this.classList.contains('bc-side--a') || this.classList.contains('bc-side--b'));
    const top = isSide ? g.anchorY : g.anchorY - 20;
    const bottom = isSide ? g.anchorY : g.anchorY + 20;
    return {
      left: g.left, right: g.right, top, bottom,
      width: g.right - g.left, height: bottom - top,
      x: g.left, y: top,
      toJSON() { return this; },
    };
  };
  return () => { Element.prototype.getBoundingClientRect = original; };
}

const side = (name) => ({ id: `p-${name}`, name });

// A genuine two-column skip: m-r1-0 (displayRound 3, the earliest column)
// feeds the final (m-r3-0, displayRound 1) directly, skipping the entire
// column m-r2-0 (displayRound 2) occupies. Three effective-round columns,
// one match each -- the minimal fixture that reproduces the skip.
const roundsFixture = () => [
  [{ id: 'm-r1-0', court: 'A', status: 'scheduled', sideA: side('Alice'), sideB: side('Bob'), displayRound: 3, feeders: [] }],
  [{ id: 'm-r2-0', court: 'A', status: 'scheduled', sideA: side('Carol'), sideB: side('Dave'), displayRound: 2, feeders: [] }],
  [{ id: 'm-r3-0', court: 'A', status: 'scheduled', sideA: side('TBD1'), sideB: side('TBD2'), displayRound: 1, feeders: ['m-r1-0', 'm-r2-0'] }],
];

const elbowXOf = (d) => d.match(/^M [\d.]+ [\d.]+ L ([\d.]+) /)[1];

describe("BracketTree: a skipping feeder's connector crosses the hidden column at its own height", () => {
  let restoreGeometry;

  beforeAll(() => {
    restoreGeometry = installGeometryMock();
  });

  afterAll(() => {
    restoreGeometry();
  });

  it('routes both connectors into m-r3-0 through the gap before its own column, sharing one vertical x', async () => {
    const { container } = render(<BracketTree rounds={roundsFixture()} />);
    await act(async () => {
      window.dispatchEvent(new Event('resize'));
    });

    const paths = Array.from(container.querySelectorAll('.bc-connectors path')).map((p) => p.getAttribute('d'));
    expect(paths).toHaveLength(2);
    // m-r1-0 skips the m-r2-0 column entirely; its horizontal run must cross
    // that hidden column at its OWN height (y=99), not cut across m-r2-0's card.
    expect(paths).toContain('M 230 99 L 544 99 L 544 166.875 L 572 166.875');
    // m-r2-0 is column-adjacent to m-r3-0 and merges with the skipping feeder
    // at the same elbow x.
    expect(paths).toContain('M 516 234.75 L 544 234.75 L 544 166.875 L 572 166.875');
    // The merge property itself: both connectors into the SAME parent turn at
    // the SAME x, which a pure-function unit test on elbowXFor alone cannot
    // pin (see bracket_connector_elbow.test.jsx).
    expect(new Set(paths.map(elbowXOf)).size).toBe(1);
  });
});
