import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent } from '@testing-library/react';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-pnum: the check-in checkbox's onChange used to send window.checkinPid(p)
// -- id when present, else the "name|dojo" composite -- to
// window.API.toggleCheckIn. Name and dojo are operator-editable after the
// draw, so that composite is not a safe wire identifier. The checkbox must
// send the id ONLY (window.checkinApiPid); an id-less row has no safe wire
// identifier at all and the write is left to the server to refuse.

installParticipantsHarness();

describe('AdminParticipants check-in checkbox sends the id-only wire pid (bc-pnum)', () => {
  let savedAPI;

  beforeEach(() => {
    savedAPI = window.API;
  });

  afterEach(() => {
    window.API = savedAPI;
  });

  it('sends the real id, not window.checkinPid\'s composite, for a stamped row', async () => {
    const toggleCheckIn = vi.fn().mockResolvedValue({});
    window.API = { toggleCheckIn };
    const { container } = await mountParticipants(makeParticipantsCompetition({
      checkInEnabled: true,
      players: [
        { id: 'uuid-alice', name: 'Alice', dojo: 'Dojo Alice', checkedIn: false },
      ],
    }));

    const checkbox = container.querySelector('input[type="checkbox"]');
    expect(checkbox).toBeTruthy();
    await fireEvent.click(checkbox);

    expect(toggleCheckIn).toHaveBeenCalledTimes(1);
    // Second positional arg is the pid; must be the real id, never
    // "Alice|Dojo Alice".
    expect(toggleCheckIn.mock.calls[0][1]).toBe('uuid-alice');
  });

  it('sends "" (never the name|dojo composite) for an id-less legacy row', async () => {
    const toggleCheckIn = vi.fn().mockResolvedValue({});
    window.API = { toggleCheckIn };
    const { container } = await mountParticipants(makeParticipantsCompetition({
      checkInEnabled: true,
      players: [
        { id: '', name: 'Bob', dojo: 'Dojo Bob', checkedIn: false },
      ],
    }));

    const checkbox = container.querySelector('input[type="checkbox"]');
    expect(checkbox).toBeTruthy();
    await fireEvent.click(checkbox);

    expect(toggleCheckIn).toHaveBeenCalledTimes(1);
    expect(toggleCheckIn.mock.calls[0][1]).toBe('');
    expect(toggleCheckIn.mock.calls[0][1]).not.toBe('Bob|Dojo Bob');
  });
});
