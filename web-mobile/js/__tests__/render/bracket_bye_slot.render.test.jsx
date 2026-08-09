import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';

// A non-power-of-two knockout contains structural byes: an entrant who advances
// a round with no opponent. BracketTreeMeta draws that as a standalone
// .bc-bye-slot placeholder in the upstream column. A NAMED slot used to render
// the competitor's name and nothing else, so a spectator saw an unexplained grey
// box sitting beside the match cards. These tests pin that the slot always
// carries the visible BYE marker, and that the marker is printed exactly once in
// either case (named or nameless).
//
// Render project (not the unit one): the marker is DOM the viewer reads off the
// bracket, so it has to be asserted on a real mount. The unit suite's fake-React
// stub never produces the slot markup.

let BracketTree;

beforeAll(async () => {
  // BracketTree measures card geometry for its connectors; jsdom has no
  // ResizeObserver. Only the slot TEXT matters here.
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

// 5 entrants, the smallest roster that produces a structural bye. Same fixture
// shape as bracket_display_model.test.jsx / bracket_feeder_slot.render.test.jsx:
// Carol is seeded straight into the semi-final, so m-r2-1's sideA feeder is ""
// and buildDisplayModel inserts a named bye slot for her in the column before.
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

const byeSlots = (container) => Array.from(container.querySelectorAll('.bc-bye-slot'));
const slotNamed = (container, name) =>
  byeSlots(container).find((s) => s.querySelector('.bc-bye-slot__name')?.textContent === name);

describe('public bracket: a named bye slot is legible as an unopposed advance', () => {
  // Alice, Bob and Carol are each seeded past a round in this bracket, so the
  // fixture draws three named placeholders.
  it('renders the competitor name AND a visible BYE marker in the same slot', () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    const slot = slotNamed(container, 'Carol');
    expect(slot).toBeTruthy();
    const tags = slot.querySelectorAll('.bc-bye-slot__tag');
    expect(tags).toHaveLength(1); // marker printed exactly once
    expect(tags[0].textContent).toBe('BYE');
    // What a spectator actually reads off the box, in order.
    expect(slot.textContent).toBe('CarolBYE');
  });

  it('marks every named slot, as visible text and not only in the aria-label', () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    const slots = byeSlots(container);
    expect(slots).toHaveLength(3);
    expect(slots.map((s) => s.textContent).sort()).toEqual(['AliceBYE', 'BobBYE', 'CarolBYE']);
    expect(screen.getAllByText('BYE')).toHaveLength(3);
  });

  it('prints the marker once, not twice, when the slot has no name', () => {
    // A bye whose feeding side carries no player: the placeholder is nameless.
    const rounds = fivePlayerRounds();
    rounds[1][1].sideA = side('');
    const { container } = render(<BracketTree rounds={rounds} />);
    const nameless = byeSlots(container).filter((s) => !s.querySelector('.bc-bye-slot__name'));
    expect(nameless).toHaveLength(1);
    expect(nameless[0].querySelectorAll('.bc-bye-slot__tag')).toHaveLength(1);
    expect(nameless[0].textContent).toBe('BYE');
  });
});
