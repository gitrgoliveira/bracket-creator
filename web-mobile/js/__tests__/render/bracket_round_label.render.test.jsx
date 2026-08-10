import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';

// mp-u37s: pin the bracket COLUMN header as the DOM actually renders it.
//
// bracket_round_label_agreement.test.jsx proves the ROW side of the agreement
// (compMatches → m.round) against the shared bracketRoundLabel primitive, but
// its column half is computed by a test-local helper that calls
// bracketRoundLabel(col[0], ci, len) itself. That mirrors BracketTreeMeta's
// source line rather than reading it, so it stays green even if the component
// stops calling the primitive (or prints a literal). This file closes that gap:
// it MOUNTS BracketTree and reads .bc-round-label out of the rendered tree, so
// the header text is asserted against the DOM a spectator sees.
//
// Also pinned here: the column a card is drawn UNDER carries the same name as
// that card's own bracketRoundLabel row label. The column side comes from the
// DOM; the row side comes from the real primitive. Nothing in this file
// re-spells the roundIdx → name rule.

let BracketTree, bracketRoundLabel, roundLabel;

beforeAll(async () => {
  // BracketTree measures card geometry to place its connectors; jsdom has no
  // ResizeObserver. The layout maths is covered by bracket_display_model.test.jsx;
  // here only the header TEXT and the card-to-column grouping matter.
  if (typeof globalThis.ResizeObserver === 'undefined') {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
  const mod = await import('../../bracket.jsx');
  BracketTree = window.BracketTree;
  bracketRoundLabel = mod.bracketRoundLabel;
  roundLabel = mod.roundLabel;
});

// The 5-entrant individual knockout as the engine persists it: 3 backend rounds
// with a bye-collapsed round, so a match's effective round (displayRound) can
// name a DIFFERENT round from its raw slot in bracket.rounds. Same shape as
// bracket_round_label_agreement.test.jsx and bracket_display_model.test.jsx,
// with sides in the normalized {id, name} form the viewer receives (as in
// bracket_feeder_slot.render.test.jsx).
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

// Every rendered column: its header text and the ids of the match cards drawn
// under it, straight out of the DOM. Bye slots carry no data-match-id (they are
// a .bc-bye-slot div, not a MatchCard button), so only real matches appear.
const domColumns = (container) => Array.from(container.querySelectorAll('.bc-round')).map((col) => {
  const header = col.querySelector('.bc-round-label');
  return {
    label: header ? header.textContent : null,
    matchIds: Array.from(col.querySelectorAll('[data-match-id]')).map((el) => el.getAttribute('data-match-id')),
  };
});

// matchId → the raw (roundIndex, totalRounds) pair it occupies on the wire.
const rawPositions = (rounds) => {
  const out = {};
  rounds.forEach((round, ri) => round.forEach((m) => { out[m.id] = { m, ri, total: rounds.length }; }));
  return out;
};

describe('bracket column headers as rendered (mp-u37s)', () => {
  it('prints exactly Quarterfinals / Semifinals / Final for the 5-entrant draw', () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    expect(domColumns(container).map((c) => c.label)).toEqual(['Quarterfinals', 'Semifinals', 'Final']);
  });

  it('draws each card under a column whose header matches that card own round label', () => {
    const rounds = fivePlayerRounds();
    const { container } = render(<BracketTree rounds={rounds} />);
    const positions = rawPositions(rounds);
    const columns = domColumns(container);

    const compared = [];
    columns.forEach(({ label, matchIds }) => {
      matchIds.forEach((id) => {
        const pos = positions[id];
        expect(pos, `card ${id} is on screen but not in the fixture`).toBeTruthy();
        // Column header: read from the DOM. Row label: the real primitive.
        expect(label).toBe(bracketRoundLabel(pos.m, pos.ri, pos.total));
        compared.push(id);
      });
    });
    // Guard against a vacuous pass: all four real matches must have been drawn.
    expect(compared.sort()).toEqual(['m-r1-0', 'm-r1-3', 'm-r2-1', 'm-r3-0']);
  });

  it('headers the bye-collapsed match Semifinals, not the raw Quarterfinals', () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    const column = domColumns(container).find((c) => c.matchIds.includes('m-r1-0'));
    expect(column).toBeTruthy();
    // THE regression: m-r1-0 sits in backend round 0 of 3, so the raw rule calls
    // it a quarterfinal, but its winner plays the final. The header a spectator
    // reads above the card must be the effective round.
    expect(roundLabel(0, 3)).toBe('Quarterfinals'); // what the raw rule would print
    expect(column.label).toBe('Semifinals');
  });
});
