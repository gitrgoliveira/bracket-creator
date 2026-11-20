// The public match card's meta line is one phrase, its parts separated by
// commas (operator decision R1, 2026-11-18). A part with nothing to say is
// left out, so the line never carries a dangling comma: a knockout match with
// no round read "Shiaijo A, , 09:40" and, with no time either, "Shiaijo A,".
//
// bracket.jsx is imported first: it publishes the window.* globals the card's
// scoreboard reaches bracket.jsx through.
import React from 'react';
import { render, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterEach } from 'vitest';

let MatchDetailCard;

beforeAll(async () => {
  await import('../../bracket.jsx');
  ({ MatchDetailCard } = await import('../../viewer_match.jsx'));
});

afterEach(() => cleanup());

const match = (fields) => ({
  id: 'm1', compId: 'c1', court: 'A', phase: 'bracket', status: 'scheduled',
  sideA: { id: 'p-aka', name: 'Aoki Taro' }, sideB: { id: 'p-shiro', name: 'Endo Goro' },
  ipponsA: [], ipponsB: [],
  ...fields,
});

const metaOf = (m) => {
  const { container } = render(<MatchDetailCard match={m} />);
  // The row's gap spaces the parts on screen, so read them one by one.
  return [...container.querySelector('.match-detail-card__meta').children]
    .map((part) => part.textContent.replace(/\s+/g, ' ').trim()).join(' ');
};

describe('MatchDetailCard meta line', () => {
  it('joins every part with a comma', () => {
    expect(metaOf(match({ compName: 'Open', round: 'QF', scheduledAt: '09:40' }))).toBe('Open, Shiaijo A, QF, 09:40');
  });

  it('leaves out a missing round without a dangling comma', () => {
    expect(metaOf(match({ compName: 'Open', scheduledAt: '09:40' }))).toBe('Open, Shiaijo A, 09:40');
  });

  it('ends on the court when there is no round and no time', () => {
    expect(metaOf(match({}))).toBe('Shiaijo A');
  });
});
