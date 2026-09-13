import { describe, it, expect } from 'vitest';
import { act, fireEvent } from '@testing-library/react';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-prow: the paste box is an INPUT for a new list and mounts empty, so its
// card's sub-line describes the BOX (empty, or how many lines are typed) and
// the SAVED roster's size is read off the Ordering & seeding card. Before
// this the box's sub-line counted its own lines while calling them "entries",
// so a competition with a full saved roster opened on "0 entries" and the
// saved size appeared nowhere on the page.

installParticipantsHarness();

function cardSub(container, title) {
  const head = [...container.querySelectorAll('.card__title')].find((el) => el.textContent === title);
  expect(head, title).toBeTruthy();
  return head.parentElement.querySelector('.card__sub').textContent;
}

describe('AdminParticipants card sub-lines (bc-prow)', () => {
  it('reads the saved roster size off the ordering card and the box as empty', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [
        { id: 'p-1', name: 'Alice', dojo: 'Dojo Alice', seed: 1 },
        { id: 'p-2', name: 'Bob', dojo: 'Dojo Bob' },
      ],
    }));

    expect(cardSub(container, 'Ordering & seeding')).toBe('2 participants · 1 seeded');
    expect(cardSub(container, 'Participant list')).toContain('Empty · One per line');
    // The box counts nothing but itself: the saved roster is not "0" here.
    expect(cardSub(container, 'Participant list')).not.toContain('0 ');
  });

  it('counts the typed lines once the box holds a list, and says "line" for one', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition());
    const box = container.querySelector('textarea');

    await act(async () => { fireEvent.change(box, { target: { value: 'Carol, Dojo Carol' } }); });
    expect(cardSub(container, 'Participant list')).toContain('1 line · One per line');

    await act(async () => { fireEvent.change(box, { target: { value: 'Carol, Dojo Carol\nDave, Dojo Dave' } }); });
    expect(cardSub(container, 'Participant list')).toContain('2 lines · One per line');
  });

  it('says "team" for a team competition and never "1 participants"', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      kind: 'team',
      players: [{ id: 'p-1', name: 'Tora A', dojo: 'Tora Dojo London' }],
    }));

    expect(cardSub(container, 'Ordering & seeding')).toBe('1 team · 0 seeded');
  });
});
