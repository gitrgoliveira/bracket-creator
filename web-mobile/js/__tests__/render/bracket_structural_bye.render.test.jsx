import React from 'react';
import { render, screen, act } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';

// bc-tmfn (operator ruling). A non-power-of-two knockout contains structural
// byes: a competitor who skips a round because no match feeds their side. The
// bracket shows them the way the EKC reference sheets and the Excel tree page
// print them: ONLY in the card of the match they first fight. No placeholder
// card in the column before, no "BYE" tag, and no connector into that side.
// Where that leaves a card fed by ONE match, the connector from that match
// ends on the row it fills (option A), not at the seam between the two rows,
// and the card is placed with that row level with the match, so the line runs
// straight.
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
// centre top+65). The anchor, the seam between the rows, is at top+50.
//
// A card's TOP is read from where the tree itself placed it (its wrapper's
// style.top, set from computeMetaTops once the cards are measured), so these
// pins follow the real placement rather than a hand-copied one: a line only
// runs straight here if the tree put the card where the line needs it.
const COLUMN_X = {
  'm-r1-3': { left: 0, right: 230 },
  'm-r1-0': { left: 286, right: 516 },
  'm-r2-1': { left: 286, right: 516 },
  'm-r3-0': { left: 572, right: 802 },
};

function installGeometryMock() {
  const original = Element.prototype.getBoundingClientRect;
  Element.prototype.getBoundingClientRect = function mockGetBoundingClientRect() {
    const card = this.closest && this.closest('[data-match-id]');
    const x = card && COLUMN_X[card.getAttribute('data-match-id')];
    if (!x) return original.call(this);
    const cardTop = parseFloat(card.closest('.bc-match-wrap')?.style.top) || 0;
    let top, bottom;
    if (this.classList.contains('bc-side--a')) { top = cardTop + 20; bottom = cardTop + 50; }
    else if (this.classList.contains('bc-side--b')) { top = cardTop + 50; bottom = cardTop + 80; }
    else if (this === card) { top = cardTop; bottom = cardTop + 80; }
    else return original.call(this);
    return {
      left: x.left, right: x.right, top, bottom,
      width: x.right - x.left, height: bottom - top,
      x: x.left, y: top,
      toJSON() { return this; },
    };
  };
  return () => { Element.prototype.getBoundingClientRect = original; };
}

// Two passes: the first measures the cards and places them, the second draws
// the connectors from the placed cards (in a browser the tree's own
// ResizeObserver triggers the second; jsdom has none).
const connectorPaths = async (rounds) => {
  const { container } = render(<BracketTree rounds={rounds} />);
  for (let pass = 0; pass < 2; pass++) {
    await act(async () => {
      window.dispatchEvent(new Event('resize'));
    });
  }
  return Array.from(container.querySelectorAll('.bc-connectors path')).map((p) => p.getAttribute('d'));
};

describe('bracket connectors: a card fed by one match takes it on the fed row, in a straight line', () => {
  let restoreGeometry;
  beforeAll(() => { restoreGeometry = installGeometryMock(); });
  afterAll(() => { restoreGeometry(); });

  it("runs Dave/Eve's connector straight into the centre of Carol's card's Shiro row when it fills sideB", async () => {
    const paths = await connectorPaths(fivePlayerRounds());
    expect(paths).toHaveLength(3);
    // Alice/Bob is stacked first (top 0). Carol's card rises 15px against its
    // feeder (its Shiro row centre, 65, sits 15 below its seam, 50), so Dave/Eve
    // is stacked 15 lower than the 96 it would take (111, anchor 161) and
    // Carol's card lands at 96, keeping its 16px gap below Alice/Bob. Its Shiro
    // row centre, 96 + 65 = 161, is level with Dave/Eve's anchor: no bend.
    expect(paths).toContain('M 230 161 L 258 161 L 258 161 L 286 161');
    // The final's two connectors join at its anchor, centred on the two
    // semifinal anchors ((50 + 146) / 2 = 98).
    expect(paths).toContain('M 516 50 L 544 50 L 544 98 L 572 98');
    expect(paths).toContain('M 516 146 L 544 146 L 544 98 L 572 98');
  });

  it("runs it straight into the Aka row's centre when the fed side is sideA", async () => {
    const paths = await connectorPaths(mirroredCarolSF());
    expect(paths).toHaveLength(3);
    // Dave/Eve is stacked at 96 (anchor 146). Carol's card drops 15px against
    // it (its Aka row centre, 35, sits 15 above its seam), to 111, so its Aka
    // row centre, 111 + 35 = 146, is level with Dave/Eve's anchor.
    expect(paths).toContain('M 230 146 L 258 146 L 258 146 L 286 146');
    // The final centres on the semifinal anchors ((50 + 161) / 2 = 105.5).
    expect(paths).toContain('M 516 50 L 544 50 L 544 105.5 L 572 105.5');
    expect(paths).toContain('M 516 161 L 544 161 L 544 105.5 L 572 105.5');
  });

  // The connector SVG spans the cards themselves: the rightmost card's right
  // edge (the final, 802) and the lowest card's bottom (Dave/Eve, 111 + 80).
  // Sized from the tree's own scroll size instead, it would include itself and
  // hold a tree whose columns narrow to fit at its first, wider width.
  it('sizes the connector SVG to the cards it joins', async () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    for (let pass = 0; pass < 2; pass++) {
      await act(async () => {
        window.dispatchEvent(new Event('resize'));
      });
    }
    const svg = container.querySelector('.bc-connectors');
    expect(svg.getAttribute('width')).toBe('802');
    expect(svg.getAttribute('height')).toBe('191');
  });
});
