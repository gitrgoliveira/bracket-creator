import React from 'react';
import { render, screen, act } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';

// bc-tmfn (operator ruling). A non-power-of-two knockout contains structural
// byes: a competitor who skips a round because no match feeds their side. The
// bracket shows them the way the EKC reference sheets and the Excel tree page
// print them: ONLY in the card of the match they first fight. No placeholder
// card in the column before, no "BYE" tag, and no connector into that side.
// Where that leaves a card fed by ONE match, the connector from that match
// ends on the row it fills (option A), not at the seam between the two rows.
//
// These replace the pins of the old .bc-bye-slot placeholder, which drew each
// such competitor a second time, in a dashed box tagged BYE, one column early.
//
// Render project (not the unit one): what is and is not drawn, and where the
// SVG connector actually ends, only exist on a real mount.

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

// 5 entrants, the smallest roster with a structural bye, as the engine
// persists it (internal/engine/testdata/bracket_match_numbers.json, the
// 5-entrant case, with names instead of Player01..05). One first-round bout,
// Dave v Eve (m-r1-3). Alice and Bob come straight into one semifinal
// (m-r1-0, fed by nothing); Carol comes straight into the other (m-r2-1) as
// sideA, against the winner of m-r1-3 as sideB.
const side = (name) => ({ id: name ? `p-${name}` : '', name });
const fivePlayerRounds = () => [
  [
    { id: 'm-r1-0', court: 'A', status: 'scheduled', sideA: side('Alice'), sideB: side('Bob'), displayRound: 2, feeders: ['', ''] },
    { id: 'm-r1-1', court: 'A', sideA: side(''), sideB: side(''), hidden: true },
    { id: 'm-r1-2', court: 'A', sideA: side('Carol'), sideB: side(''), hidden: true },
    { id: 'm-r1-3', court: 'A', status: 'scheduled', sideA: side('Dave'), sideB: side('Eve'), displayRound: 3, feeders: ['', ''] },
  ],
  [
    { id: 'm-r2-0', court: 'A', sideA: side('Winner of r3-m0'), sideB: side(''), hidden: true },
    { id: 'm-r2-1', court: 'A', status: 'scheduled', sideA: side('Carol'), sideB: side('Winner of r3-m3'), displayRound: 2, feeders: ['', 'm-r1-3'] },
  ],
  [
    { id: 'm-r3-0', court: 'A', status: 'scheduled', sideA: side('Winner of r2-m0'), sideB: side('Winner of r2-m1'), displayRound: 1, feeders: ['m-r1-0', 'm-r2-1'] },
  ],
];

// The same draw with Carol's semifinal mirrored: the winner of m-r1-3 is
// sideA (the Aka row) and Carol, coming straight in, is sideB.
const mirroredCarolSF = () => {
  const rounds = fivePlayerRounds();
  rounds[1][1] = { ...rounds[1][1], sideA: side('Winner of r3-m3'), sideB: side('Carol'), feeders: ['m-r1-3', ''] };
  return rounds;
};

const columnCardIds = (container) => Array.from(container.querySelectorAll('.bc-round')).map((col) => ({
  wraps: col.querySelectorAll('.bc-match-wrap').length,
  ids: Array.from(col.querySelectorAll('[data-match-id]')).map((el) => el.getAttribute('data-match-id')),
}));

const occurrences = (text, needle) => text.split(needle).length - 1;

describe('bracket: a competitor who skips a round appears only in the card of their first match', () => {
  it('draws no placeholder card and no BYE tag', () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    expect(container.querySelectorAll('.bc-bye-slot')).toHaveLength(0);
    expect(screen.queryAllByText('BYE')).toHaveLength(0);
    expect(container.textContent).not.toContain('BYE');
  });

  it('holds only the one first-round bout in the first column', () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    const cols = columnCardIds(container);
    expect(cols.map((c) => c.ids)).toEqual([['m-r1-3'], ['m-r1-0', 'm-r2-1'], ['m-r3-0']]);
    // Every box in a column is a match card: nothing else is drawn.
    expect(cols.map((c) => c.wraps)).toEqual([1, 2, 1]);
  });

  it('shows Alice, Bob and Carol once each, in their semifinal cards', () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    const home = { Alice: 'm-r1-0', Bob: 'm-r1-0', Carol: 'm-r2-1' };
    for (const [name, matchId] of Object.entries(home)) {
      // Once in the whole tree: the placeholder used to print them twice.
      expect(occurrences(container.textContent, name), name).toBe(1);
      const els = screen.getAllByText(name);
      expect(els, name).toHaveLength(1);
      expect(els[0].closest('[data-match-id]').getAttribute('data-match-id'), name).toBe(matchId);
    }
  });

  it('draws a connector only between matches: none into a skipped side', async () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    await act(async () => {
      window.dispatchEvent(new Event('resize'));
    });
    // m-r1-3 → m-r2-1, and the final's two. The old placeholders added three
    // more (two into Alice/Bob's card, one into Carol's row).
    expect(container.querySelectorAll('.bc-connectors path')).toHaveLength(3);
  });
});

// Card geometry for the connector pins (tree-relative px; the .bc-tree root
// keeps jsdom's zero rect, so tree-relative == page). Columns are 230px wide
// with a 56px gap, as on the real render (bracket_connector_elbow.render.test.jsx).
// Each card is 80px tall: a 20px meta header, then the Aka row (.bc-side--a,
// top+20..top+50, centre top+35) and the Shiro row (.bc-side--b, top+50..top+80,
// centre top+65). The anchor, the seam between the rows, is at top+50. Tops
// are what computeMetaTops gives this draw for 80px cards: Alice/Bob stacked
// first (0), Dave/Eve next (80 + 16 gap = 96), Carol's semifinal level with
// Dave/Eve, its one feeder (96), and the final centred on the two semifinal
// anchors ((50 + 146) / 2 - 50 = 48).
const GEOM = {
  'm-r1-3': { left: 0, right: 230, top: 96 },
  'm-r1-0': { left: 286, right: 516, top: 0 },
  'm-r2-1': { left: 286, right: 516, top: 96 },
  'm-r3-0': { left: 572, right: 802, top: 48 },
};

function installGeometryMock() {
  const original = Element.prototype.getBoundingClientRect;
  Element.prototype.getBoundingClientRect = function mockGetBoundingClientRect() {
    const card = this.closest && this.closest('[data-match-id]');
    const g = card && GEOM[card.getAttribute('data-match-id')];
    if (!g) return original.call(this);
    let top, bottom;
    if (this.classList.contains('bc-side--a')) { top = g.top + 20; bottom = g.top + 50; }
    else if (this.classList.contains('bc-side--b')) { top = g.top + 50; bottom = g.top + 80; }
    else if (this === card) { top = g.top; bottom = g.top + 80; }
    else return original.call(this);
    return {
      left: g.left, right: g.right, top, bottom,
      width: g.right - g.left, height: bottom - top,
      x: g.left, y: top,
      toJSON() { return this; },
    };
  };
  return () => { Element.prototype.getBoundingClientRect = original; };
}

const connectorPaths = async (rounds) => {
  const { container } = render(<BracketTree rounds={rounds} />);
  await act(async () => {
    window.dispatchEvent(new Event('resize'));
  });
  return Array.from(container.querySelectorAll('.bc-connectors path')).map((p) => p.getAttribute('d'));
};

describe('bracket connectors: a card fed by one match takes it on the fed row (option A)', () => {
  let restoreGeometry;
  beforeAll(() => { restoreGeometry = installGeometryMock(); });
  afterAll(() => { restoreGeometry(); });

  // The final's two connectors, unchanged by this rule: both join at its
  // anchor (48 + 50 = 98), from each semifinal's own anchor, turning at the
  // elbow in the gap before the final (572 - 28 = 544).
  const FINAL_FROM_ALICE_BOB = 'M 516 50 L 544 50 L 544 98 L 572 98';
  const FINAL_FROM_CAROL_SF = 'M 516 146 L 544 146 L 544 98 L 572 98';

  it("ends Dave/Eve's connector at the centre of Carol's card's Shiro row when it fills sideB", async () => {
    const paths = await connectorPaths(fivePlayerRounds());
    expect(paths).toHaveLength(3);
    // From m-r1-3's anchor (96 + 50 = 146) to the Shiro row centre of m-r2-1
    // (96 + 65 = 161), NOT its anchor (146), which would be a straight line.
    expect(paths).toContain('M 230 146 L 258 146 L 258 161 L 286 161');
    expect(paths).toContain(FINAL_FROM_ALICE_BOB);
    expect(paths).toContain(FINAL_FROM_CAROL_SF);
  });

  it("ends it at the Aka row's centre when the fed side is sideA", async () => {
    const paths = await connectorPaths(mirroredCarolSF());
    expect(paths).toHaveLength(3);
    // Aka row centre of m-r2-1: 96 + 35 = 131.
    expect(paths).toContain('M 230 146 L 258 146 L 258 131 L 286 131');
    expect(paths).toContain(FINAL_FROM_ALICE_BOB);
    expect(paths).toContain(FINAL_FROM_CAROL_SF);
  });
});
