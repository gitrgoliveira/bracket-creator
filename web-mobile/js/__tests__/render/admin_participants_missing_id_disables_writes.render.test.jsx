import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent } from '@testing-library/react';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-pnum (item 4): a replace write for an id-less row can never land: PUT
// .../participants/ (empty id segment) matches no route at all. The control
// is disabled client-side instead, with a hint mirroring
// helper.MissingParticipantIDsMessage's remedy sentence. (The check-in
// checkbox this file also covered left the page with bc-prow; the
// registration desk's own render test pins that control now.)

installParticipantsHarness();

describe('AdminParticipants disables writes for an id-less row (bc-pnum)', () => {
  let savedAPI;

  beforeEach(() => {
    savedAPI = window.API;
  });

  afterEach(() => {
    window.API = savedAPI;
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
});
