import { describe, it, expect } from 'vitest';
import { elbowXFor, connectorPath, connectorTargetY } from '../bracket.jsx';

// bc-pnum: in an uneven effective-round bracket a feeder can skip a column
// (a bye auto-advance elides the hidden column between it and its parent).
// The elbow's vertical run must sit in the gap immediately BEFORE the
// parent's column, not at the feeder/parent midpoint: the midpoint rule
// landed the vertical run inside the skipped column's card, so the skipping
// feeder read as feeding the skipped match instead of its real parent.
//
// A bracket generated now never skips: DisplayRound is the distance from the
// final, so every feeder sits one column before its parent (pinned by
// TestBracketDisplayMetadata_Feeders, internal/engine). The draw below was
// generated while rounds were classified by slot level, and a bracket stored
// then keeps those rounds, so this routing is still reachable.
//
// Numbers below are measured on the real render (tree-relative px) for a
// 10-entrant, 2-shiaijo draw: M1 (round 1) and M5 (the quarterfinal) both
// feed M7 (the semifinal); M1's winner skips the quarterfinal column
// entirely (the hidden bye slot), M5's does not.
//   M1: right=230, anchor y=99
//   M5: left=286, right=516, top=166, anchor y=234.75
//   M7: left=572, anchor y=166.875
//   column gap: 56 (adjacent elbows sit at fRight+28 == mLeft-28)
// These are pure-function unit tests, so they exercise elbowXFor/connectorPath
// directly and can't pin how BracketConnectorsMeta actually CALLS them (a
// mutation that restores the old `(fRight + mLeft) / 2` at the out.push
// call site, leaving these helpers defined but uncalled for that value,
// keeps the tests below green). The merge property -- that a skipping
// feeder's connector shares its vertical x with a column-adjacent sibling's,
// as produced by the real component -- is pinned by the render-project test
// bracket_connector_elbow.render.test.jsx instead.
describe('connector elbow: routes through the gap before the PARENT column', () => {
  const gap = 56;

  it('elbowXFor centres the elbow in the gap before the parent (mLeft - gap/2)', () => {
    expect(elbowXFor(572, gap)).toBe(544);
  });

  it('produces the exact M1->M7 path, crossing the skipped column at its own height', () => {
    const elbowX = elbowXFor(572, gap);
    const d = connectorPath({ fRight: 230, fMidY: 99, mLeft: 572, mMidY: 166.875, elbowX });
    expect(d).toBe('M 230 99 L 544 99 L 544 166.875 L 572 166.875');
  });

  it('produces the exact M5->M7 sibling path, merging with M1 at the same x', () => {
    const elbowX = elbowXFor(572, gap);
    const d = connectorPath({ fRight: 516, fMidY: 234.75, mLeft: 572, mMidY: 166.875, elbowX });
    expect(d).toBe('M 516 234.75 L 544 234.75 L 544 166.875 L 572 166.875');
  });

  it("the skipping feeder's vertical run (x=544) lies OUTSIDE the skipped card's x-range [286, 516]", () => {
    const elbowX = elbowXFor(572, gap);
    const [skippedLeft, skippedRight] = [286, 516];
    expect(elbowX < skippedLeft || elbowX > skippedRight).toBe(true);
    // Concretely: it's past the skipped card's right edge, in the gap before M7.
    expect(elbowX).toBeGreaterThan(skippedRight);
  });

  it('an adjacent pair (no skip) keeps its elbow at fRight+28 == mLeft-28', () => {
    // e.g. a first-round winner feeding directly into an adjacent quarterfinal:
    // fRight=230 (round-1 column), mLeft=286 (round-2 column), gap=56.
    const elbowX = elbowXFor(286, gap);
    expect(elbowX).toBe(258);
    expect(elbowX).toBe(230 + 28);
    expect(elbowX).toBe(286 - 28);
  });
});

// bc-tmfn (operator ruling, option A): a competitor who skips a round gets no
// placeholder card, so a card can be fed by ONE match, its other side a
// competitor who comes straight in. That connector ends on the row the match
// fills, at the row's centre: feeders[0] fills sideA, the Aka row
// (.bc-side--a); feeders[1] fills sideB, the Shiro row (.bc-side--b). A card
// fed by TWO matches keeps the single join at its anchor, the seam between the
// rows. Pure: the row centre comes from a callback, called only when needed.
// That BracketConnectorsMeta actually routes through this helper is pinned on
// a real mount in render/bracket_structural_bye.render.test.jsx.
describe('connectorTargetY: where a feeder connector ends on its parent card', () => {
  // A card whose seam (anchor) is at 150, Aka row centre 135, Shiro row centre 165.
  const cardAnchorY = 150;
  const ROW = { a: 135, b: 165 };
  const recorder = () => {
    const calls = [];
    const rowMidY = (side) => { calls.push(side); return ROW[side]; };
    return { calls, rowMidY };
  };

  it("one feeder in sideB (Shiro): ends at the Shiro row's centre", () => {
    const { calls, rowMidY } = recorder();
    expect(connectorTargetY({ feeders: ['', 'm-r1-3'], fid: 'm-r1-3', cardAnchorY, rowMidY })).toBe(165);
    expect(calls).toEqual(['b']);
  });

  it("one feeder in sideA (Aka): ends at the Aka row's centre", () => {
    const { calls, rowMidY } = recorder();
    expect(connectorTargetY({ feeders: ['m-r1-3', ''], fid: 'm-r1-3', cardAnchorY, rowMidY })).toBe(135);
    expect(calls).toEqual(['a']);
  });

  it('two feeders: both connectors join at the card anchor, and no row is measured', () => {
    const { calls, rowMidY } = recorder();
    const feeders = ['m-r1-0', 'm-r2-1'];
    expect(connectorTargetY({ feeders, fid: 'm-r1-0', cardAnchorY, rowMidY })).toBe(150);
    expect(connectorTargetY({ feeders, fid: 'm-r2-1', cardAnchorY, rowMidY })).toBe(150);
    expect(calls).toEqual([]);
  });

  it('falls back to the anchor when the fed row cannot be measured', () => {
    expect(connectorTargetY({ feeders: ['', 'm-r1-3'], fid: 'm-r1-3', cardAnchorY, rowMidY: () => null })).toBe(150);
  });

  it('falls back to the anchor for a feeder that is not the lone one on the card', () => {
    // Defensive: the effect only asks about ids in the card's own feeders.
    const { calls, rowMidY } = recorder();
    expect(connectorTargetY({ feeders: ['', 'm-r1-3'], fid: 'm-other', cardAnchorY, rowMidY })).toBe(150);
    expect(connectorTargetY({ feeders: undefined, fid: 'm-r1-3', cardAnchorY, rowMidY })).toBe(150);
    expect(calls).toEqual([]);
  });
});
