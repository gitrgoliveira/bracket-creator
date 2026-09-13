import { describe, it, expect } from 'vitest';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-prow: the ordering card carries no check-in control (the registration
// desk owns check-in), and a roster row's text is two units, name and
// dojo, so the row can be one line where it fits and wrap as units
// where it does not. jsdom cannot measure the wrapping; this pins the
// structure the CSS relies on and the absence of the removed controls.

installParticipantsHarness();

describe('AdminParticipants ordering card (bc-prow)', () => {
  it('renders no check-in control even when the competition tracks check-in', async () => {
    const { container, queryByText, getByText } = await mountParticipants(makeParticipantsCompetition({
      checkInEnabled: true,
      players: [
        { id: 'p-1', name: 'Alice', dojo: 'Dojo Alice', checkedIn: true },
        { id: 'p-2', name: 'Bob', dojo: 'Dojo Alice', checkedIn: false },
      ],
    }));

    expect(container.querySelector('input[type="checkbox"]')).toBeNull();
    expect(queryByText('Check in all')).toBeNull();
    expect(queryByText('Show unchecked')).toBeNull();
    expect(queryByText(/Mark all from/)).toBeNull();
    expect(queryByText(/checked in/)).toBeNull();
    expect(container.querySelector('.seed-row.is-checked-in')).toBeNull();
    expect(getByText('Ordering & seeding').className).toBe('card__title');
    expect(queryByText('Check-in & Seeding')).toBeNull();
  });

  it('lists competitors in number order once the draw has numbered them, unnumbered last', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      status: 'draw-ready',
      players: [
        { id: 'p-9', name: 'Nine', dojo: 'D', number: 'K9' },
        { id: 'p-x', name: 'Left out', dojo: 'D' },
        { id: 'p-10', name: 'Ten', dojo: 'D', number: 'K10' },
        { id: 'p-1', name: 'One', dojo: 'D', number: 'K1' },
      ],
    }));

    const names = [...container.querySelectorAll('.seed-row__name')].map((n) => n.textContent.replace(/^K\d+/, ''));
    // Integer order, not string order ("K10" would sort before "K9" as text).
    expect(names).toEqual(['One', 'Nine', 'Ten', 'Left out']);
  });

  it('keeps the edit pencil while the draw is pending and drops it once the competition has started', async () => {
    // The single-competitor PUT accepts setup and draw-ready (and cascades a
    // draw-ready rename into the draw) but 409s after the start, so the
    // pencil follows the server (operator ruling bc-prow).
    const drawReady = await mountParticipants(makeParticipantsCompetition({
      status: 'draw-ready',
      players: [{ id: 'p-1', name: 'Alice', dojo: 'D', number: 'K1' }],
    }));
    expect(drawReady.container.querySelector('button[aria-label="Edit Alice"]')).toBeTruthy();
    drawReady.unmount();

    const started = await mountParticipants(makeParticipantsCompetition({
      status: 'pools',
      players: [{ id: 'p-1', name: 'Alice', dojo: 'D', number: 'K1' }],
    }));
    expect(started.container.querySelector('button[aria-label="Edit Alice"]')).toBeNull();
  });

  it('keeps roster order before the draw, when no competitor has a number', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [
        { id: 'p-2', name: 'Second', dojo: 'D' },
        { id: 'p-1', name: 'First', dojo: 'D' },
      ],
    }));

    expect([...container.querySelectorAll('.seed-row__name')].map((n) => n.textContent)).toEqual(['Second', 'First']);
  });

  it('lays a row out as name unit then dojo + id unit inside one line', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [{ id: 'abcdef1234567890', name: 'Alice', dojo: 'Dojo Alice', number: 'K1', source: 'manual' }],
    }));

    const row = container.querySelector('.seed-row');
    const line = row.querySelector('.seed-row__main > .seed-row__line');
    expect(line).toBeTruthy();
    expect([...line.children].map((el) => el.className)).toEqual(['seed-row__who', 'seed-row__where']);

    const who = line.querySelector('.seed-row__who');
    expect(who.querySelector('.seed-row__name .num-prefix').textContent).toBe('K1');
    expect(who.querySelector('.seed-row__name').textContent).toBe('K1Alice');
    expect(who.querySelector('.tag-badge').textContent).toBe('manual');

    const where = line.querySelector('.seed-row__where');
    expect(where.textContent).toBe('Dojo Alice');
    // Operator ruling (bc-prow, reversing bc-pnum 1e): the participant id is
    // not shown on the row, not even truncated.
    expect(row.textContent).not.toContain('abcdef12');

    const actions = row.querySelector('.seed-row__actions');
    expect(actions.querySelector('button[aria-label="Move up"]')).toBeTruthy();
    expect(actions.querySelector('button[aria-label="Move down"]')).toBeTruthy();
    expect(actions.querySelector('button[aria-label="Edit Alice"]')).toBeTruthy();
  });
});
