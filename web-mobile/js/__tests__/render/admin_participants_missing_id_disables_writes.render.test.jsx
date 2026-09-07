import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent } from '@testing-library/react';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-pnum (item 4): a check-in or replace write for an
// id-less row can never land (checkinApiPid returns "" for it): PUT
// .../participants//checkin 404s "participant not found" about a row on
// screen, and PUT .../participants/ (empty id segment) matches no route at
// all. Both controls are disabled client-side instead, with a hint mirroring
// helper.MissingParticipantIDsMessage's remedy sentence.

installParticipantsHarness();

describe('AdminParticipants disables writes for an id-less row (bc-pnum)', () => {
  let savedAPI;

  beforeEach(() => {
    savedAPI = window.API;
  });

  afterEach(() => {
    window.API = savedAPI;
  });

  it('disables the check-in checkbox for an id-less row and sends no request on click', async () => {
    const toggleCheckIn = vi.fn().mockResolvedValue({});
    window.API = { toggleCheckIn };
    const { container } = await mountParticipants(makeParticipantsCompetition({
      checkInEnabled: true,
      players: [{ id: '', name: 'Bob', dojo: 'Dojo Bob', checkedIn: false }],
    }));

    const checkbox = container.querySelector('input[type="checkbox"]');
    expect(checkbox).toBeTruthy();
    expect(checkbox.disabled).toBe(true);
    expect(checkbox.getAttribute('title')).toContain('No id on file');

    // testing-library's fireEvent.click dispatches a synthetic (untrusted)
    // MouseEvent, which jsdom does NOT suppress for a disabled checkbox
    // (confirmed: only the native .click() method respects `disabled` for
    // checkbox inputs in jsdom, unlike <button disabled> below, which DOES
    // suppress a dispatched click). Call the native method directly so this
    // assertion reflects what a real click on a real disabled checkbox does.
    await act(async () => { checkbox.click(); });
    expect(toggleCheckIn).not.toHaveBeenCalled();
  });

  it('leaves the check-in checkbox enabled for a stamped row', async () => {
    const toggleCheckIn = vi.fn().mockResolvedValue({});
    window.API = { toggleCheckIn };
    const { container } = await mountParticipants(makeParticipantsCompetition({
      checkInEnabled: true,
      players: [{ id: 'uuid-alice', name: 'Alice', dojo: 'Dojo Alice', checkedIn: false }],
    }));

    const checkbox = container.querySelector('input[type="checkbox"]');
    expect(checkbox.disabled).toBe(false);
    fireEvent.click(checkbox);
    expect(toggleCheckIn).toHaveBeenCalledTimes(1);
  });

  it('disables the Edit modal Save button for an id-less row and sends no request on click', async () => {
    const replaceParticipant = vi.fn().mockResolvedValue({});
    window.API = { replaceParticipant };
    window.promptAdminPassword = vi.fn().mockResolvedValue('admin-pw');
    const { container, getByText } = await mountParticipants(makeParticipantsCompetition({
      players: [{ id: '', name: 'Bob', dojo: 'Dojo Bob', checkedIn: false }],
    }));

    const editButton = container.querySelector('button[aria-label="Edit Bob"]');
    expect(editButton).toBeTruthy();
    fireEvent.click(editButton);

    const saveButton = getByText('Save');
    expect(saveButton.disabled).toBe(true);
    expect(saveButton.getAttribute('title')).toContain('No id on file');
    expect(getByText('No id on file. Save the roster once and the ids are assigned.')).toBeTruthy();

    fireEvent.click(saveButton);
    expect(replaceParticipant).not.toHaveBeenCalled();
  });

  // bc-pnum: a hover title alone is
  // unreachable on a tablet or by keyboard/screen-reader. The disabled
  // reason must ride in the aria-label AND render inline on the row.
  it('states the reason in the checkbox aria-label and shows it inline on the row', async () => {
    const toggleCheckIn = vi.fn().mockResolvedValue({});
    window.API = { toggleCheckIn };
    const { container } = await mountParticipants(makeParticipantsCompetition({
      checkInEnabled: true,
      players: [{ id: '', name: 'Bob', dojo: 'Dojo Bob', checkedIn: false }],
    }));

    const checkbox = container.querySelector('input[type="checkbox"]');
    expect(checkbox.getAttribute('aria-label')).toContain('No id on file');
    const inlineHint = container.querySelector('.seed-row__noid');
    expect(inlineHint?.textContent).toBe(' · No id on file. Save the roster once and the ids are assigned.');
  });

  it('does not fold a reason into the aria-label, nor render an inline hint, for a stamped row', async () => {
    const toggleCheckIn = vi.fn().mockResolvedValue({});
    window.API = { toggleCheckIn };
    const { container } = await mountParticipants(makeParticipantsCompetition({
      checkInEnabled: true,
      players: [{ id: 'uuid-alice', name: 'Alice', dojo: 'Dojo Alice', checkedIn: false }],
    }));

    const checkbox = container.querySelector('input[type="checkbox"]');
    expect(checkbox.getAttribute('aria-label')).not.toContain('No id on file');
    expect(container.querySelector('.seed-row__noid')).toBeNull();
  });
});
