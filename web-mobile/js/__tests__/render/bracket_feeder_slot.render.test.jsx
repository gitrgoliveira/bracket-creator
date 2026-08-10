import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll } from 'vitest';

// The PUBLIC bracket (viewer Bracket tab) and the admin bracket both render
// through BracketTree → MatchCard → PlayerLine. An unresolved feeder side
// arrives from the server as the wire value "Winner of r<depth>-m<idx>", which
// is (a) an internal index a spectator cannot use and (b) NOT the number the
// same bracket prints on its cards. These tests pin that no raw SLOT VALUE
// reaches the DOM, and that the human label names a card that is actually on
// screen. Note the regex targets the "Winner of r<N>-m<N>" slot shape, not
// match ids in general: those legitimately appear in data-match-id and in the
// aria-label fallback, and are not what leaks to a reader.

let BracketTree, MatchCard;

beforeAll(async () => {
  // BracketTree measures card geometry to place its connectors; jsdom has no
  // ResizeObserver, so stub the observe/disconnect surface it uses. The layout
  // maths is covered by bracket_display_model.test.jsx; here only the TEXT matters.
  if (typeof globalThis.ResizeObserver === 'undefined') {
    globalThis.ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
  await import('../../bracket.jsx');
  BracketTree = window.BracketTree;
  MatchCard = window.MatchCard;
});

// 5 entrants: the roster that produces exactly this placeholder. Shape verified
// against the engine's bracket.json (same fixture as bracket_display_model.test.jsx),
// with sides in the normalized {id, name} form the viewer receives.
// Card numbers: m-r1-3 = M1, m-r1-0 = M2, m-r2-1 = M3, m-r3-0 = M4.
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

describe('public bracket: an unresolved feeder slot reads as a match number', () => {
  it('never renders a raw r<N>-m<N> id anywhere in the tree', () => {
    const { container } = render(<BracketTree rounds={fivePlayerRounds()} />);
    // The whole rendered subtree, text + attributes (aria-labels included).
    expect(container.innerHTML).not.toMatch(/r\d+-m\d+/);
    expect(container.textContent).not.toContain('Winner of r');
  });

  it('names the feeder after the card the bracket itself prints', () => {
    render(<BracketTree rounds={fivePlayerRounds()} />);
    // "Winner of r3-m3" is fed by m-r1-3, the card labelled M1 — NOT "M3", which
    // is the trap in reading the id as a match number.
    expect(screen.getByText('Winner of M1')).toBeTruthy();
    expect(screen.getByText('M1')).toBeTruthy(); // the card it points at is on screen
  });

  it('resolves a slot that points through a phantom bye match', () => {
    render(<BracketTree rounds={fivePlayerRounds()} />);
    // The final's sideA slot says "r2-m0" (a dead bye match); the real feeder is
    // m-r1-0 = M2. Its sideB slot "r2-m1" → m-r2-1 = M3.
    expect(screen.getByText('Winner of M2')).toBeTruthy();
    expect(screen.getByText('Winner of M3')).toBeTruthy();
  });

  it('leaves real competitor names untouched', () => {
    render(<BracketTree rounds={fivePlayerRounds()} />);
    ['Alice', 'Bob', 'Dave', 'Eve'].forEach((n) => expect(screen.getAllByText(n).length).toBeGreaterThan(0));
  });
});

describe('MatchCard without a bracket context still cannot leak the id', () => {
  it('degrades a feeder slot to TBD when no labeller is supplied', () => {
    const { container } = render(
      <MatchCard
        match={{ id: 'm-r3-0', court: 'A', status: 'scheduled', sideA: side('Winner of r2-m0'), sideB: side('Yuki Tanaka') }}
        variant="1"
      />
    );
    expect(container.innerHTML).not.toMatch(/r\d+-m\d+/);
    expect(screen.getByText('TBD')).toBeTruthy();
    expect(screen.getByText('Yuki Tanaka')).toBeTruthy();
  });

  it('keeps pool-origin placeholders verbatim: they are self-describing', () => {
    render(
      <MatchCard
        match={{ id: 'm-r1-0', court: 'A', status: 'scheduled', sideA: side('Pool A-1st'), sideB: side('Pool B-2nd') }}
        variant="1"
      />
    );
    expect(screen.getByText('Pool A-1st')).toBeTruthy();
    expect(screen.getByText('Pool B-2nd')).toBeTruthy();
  });
});
