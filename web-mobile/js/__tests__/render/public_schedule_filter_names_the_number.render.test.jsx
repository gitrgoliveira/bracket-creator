// bc-nsrc: the PUBLIC schedule filter (PlayerMultiFilter) searches competitor
// numbers, and says so -- to a reader and to a screen reader alike.
//
// The predicate itself is owned by competitor_search.jsx and pinned there.
// What this file pins is the COPY around it, which drifted on its own: the
// admin Scores page filter was reworded with the ruling ("player, team, dojo
// or number") while this one kept promising "player, tag, team, or dojo",
// and its <input> had no accessible name at all -- the visible placeholder
// is a sibling <span>, not the input's own placeholder, so assistive tech was
// handed an unlabelled text box. The free-text option ("Match ... in any
// name, tag, or dojo") is the third string, and it is the one a reader sees
// mid-search.
//
// Wording is not pinned exactly; what must survive is that all three name
// the number and the input has a name.
import React from 'react';
import { render, act, fireEvent, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';

const TOURNAMENT = {
  competitions: [
    {
      id: 'c1', name: 'Kyu',
      players: [
        { id: 'p1', name: 'Ken Saito', dojo: 'Nara', number: 'K1' },
        { id: 'p2', name: 'Kenji Mori', dojo: 'Osaka', number: 'K12' },
      ],
    },
  ],
};

let PlayerMultiFilter;

beforeAll(async () => {
  const mod = await import('../../viewer_schedule.jsx');
  PlayerMultiFilter = mod.PlayerMultiFilter;
});
afterAll(() => vi.restoreAllMocks());
beforeEach(() => cleanup());

async function mount() {
  await act(async () => {
    render(
      <PlayerMultiFilter
        tournament={TOURNAMENT}
        picked={[]}
        setPicked={vi.fn()}
        dojoText=""
        setDojoText={vi.fn()}
      />,
    );
  });
  return document.querySelector('input.pmf__input');
}

const namesTheNumber = (s) => {
  expect(s).toMatch(/number/i);
  expect(s).toMatch(/player|name/i);
  expect(s).toMatch(/dojo/i);
};

describe('the public schedule filter says it searches numbers', () => {
  it('the visible placeholder', async () => {
    await mount();
    namesTheNumber(document.querySelector('.pmf__placeholder').textContent);
  });

  it('the accessible name of the box', async () => {
    const box = await mount();
    namesTheNumber(box.getAttribute('aria-label') || '');
  });

  it('the free-text option offered while typing', async () => {
    const box = await mount();
    await act(async () => {
      fireEvent.focus(box);
      fireEvent.change(box, { target: { value: 'K12' } });
    });
    const option = document.querySelector('.pmf__option--text');
    expect(option, 'the free-text option is offered').not.toBeNull();
    namesTheNumber(option.textContent);
    // And the rule behind the copy is the owner's: K12 lists K12 alone.
    expect(screen.queryByText(/Kenji Mori/)).not.toBeNull();
    expect(screen.queryByText(/Ken Saito/)).toBeNull();
  });
});
