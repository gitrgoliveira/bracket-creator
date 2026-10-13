// Delete competition asks for the password again (operator ruling 2026-10-12).
// Driven through the real Settings screen and the real DialogHost: the dialog
// names the competition, takes the password in a password input, and a wrong
// one shows the server's sentence INSIDE the dialog and leaves the operator
// signed in; the right one deletes and leaves the screen.
import React from 'react';
import { render, act, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { DialogHost, confirmDialog, promptDialog } from '../../ui.jsx';
import { installSettingsHarness, makeSettingsCompetition } from './settings_mount_harness.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

const stubs = installSettingsHarness({ confirmDialog, promptDialog });

const SENTENCE = 'Wrong password. The competition was not deleted.';
const noop = () => {};

const wrongPassword = () => Object.assign(new Error(SENTENCE), { code: 'confirm_password_wrong' });

async function mountWithDialogHost({ comp, onBack, showToast }) {
  const t = { name: 'Spring Taikai', courts: ['A', 'B'], competitions: [comp] };
  let result;
  await act(async () => {
    result = render(
      <div>
        <window.AdminCompetition
          tournament={t} competition={comp} pools={[]} poolMatches={[]} standings={[]} bracket={null}
          section="settings" onSection={noop} onBack={onBack} onOpenCompetition={noop} onUpdate={noop}
          onRefreshCompetition={noop} onMoveCourt={noop} onEditScore={noop} onLogout={noop}
          onViewerMode={noop} tweaks={{}} password="pw" showToast={showToast}
        />
        <DialogHost />
      </div>
    );
  });
  return result;
}

const button = (container, text) =>
  Array.from(container.querySelectorAll('button')).find((b) => b.textContent.trim() === text);
const dialog = () => document.querySelector('[role="dialog"]');
const type = (value) => act(async () => {
  fireEvent.change(dialog().querySelector('input'), { target: { value } });
});

describe('Delete competition asks for the password again', () => {
  let onBack; let showToast; let container;

  beforeEach(async () => {
    stubs.API.deleteCompetition = vi.fn();
    onBack = vi.fn();
    showToast = vi.fn();
    ({ container } = await mountWithDialogHost({
      comp: makeSettingsCompetition({ id: 'c1', name: 'Autumn Cup' }), onBack, showToast,
    }));
  });
  afterEach(() => { cleanup(); });

  it('opens a password dialog that names the competition, with Cancel and Delete', async () => {
    await pointerTap(button(container, 'Delete competition'));
    const d = dialog();
    expect(d).not.toBeNull();
    expect(d.textContent).toContain('Enter the admin password to delete "Autumn Cup".');
    expect(d.querySelector('input').type).toBe('password');
    expect(button(d, 'Cancel')).toBeDefined();
    expect(button(d, 'Delete')).toBeDefined();
    expect(stubs.API.deleteCompetition).not.toHaveBeenCalled();
  });

  it('a wrong password shows the sentence in the dialog, keeps it open and deletes nothing', async () => {
    stubs.API.deleteCompetition.mockRejectedValueOnce(wrongPassword());
    await pointerTap(button(container, 'Delete competition'));
    await type('nope');
    await keyboardClick(button(dialog(), 'Delete'));

    expect(stubs.API.deleteCompetition).toHaveBeenCalledWith('c1', 'pw', 'nope');
    expect(dialog()).not.toBeNull();
    expect(dialog().querySelector('[role="alert"]').textContent).toBe(SENTENCE);
    expect(onBack).not.toHaveBeenCalled();
    expect(showToast).not.toHaveBeenCalled();
  });

  it('the right password after a wrong one deletes and leaves the screen', async () => {
    stubs.API.deleteCompetition.mockRejectedValueOnce(wrongPassword());
    stubs.API.deleteCompetition.mockResolvedValueOnce(true);
    await pointerTap(button(container, 'Delete competition'));
    await type('nope');
    await keyboardClick(button(dialog(), 'Delete'));
    await type('right');
    await keyboardClick(button(dialog(), 'Delete'));

    expect(stubs.API.deleteCompetition).toHaveBeenLastCalledWith('c1', 'pw', 'right');
    expect(dialog()).toBeNull();
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it('Cancel closes the dialog and sends nothing', async () => {
    await pointerTap(button(container, 'Delete competition'));
    await type('whatever');
    await keyboardClick(button(dialog(), 'Cancel'));
    expect(dialog()).toBeNull();
    expect(stubs.API.deleteCompetition).not.toHaveBeenCalled();
    expect(onBack).not.toHaveBeenCalled();
  });

  it('any other refusal closes the dialog and is toasted', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    stubs.API.deleteCompetition.mockRejectedValueOnce(new Error('competition is in progress; mark it invalid before deleting'));
    await pointerTap(button(container, 'Delete competition'));
    await type('right');
    await keyboardClick(button(dialog(), 'Delete'));
    expect(dialog()).toBeNull();
    expect(showToast).toHaveBeenCalledWith('competition is in progress; mark it invalid before deleting', 'error');
    expect(onBack).not.toHaveBeenCalled();
    expect(logged).toHaveBeenCalledTimes(1);
    logged.mockRestore();
  });

  it('an empty password is not sent', async () => {
    await pointerTap(button(container, 'Delete competition'));
    await keyboardClick(button(dialog(), 'Delete'));
    expect(stubs.API.deleteCompetition).not.toHaveBeenCalled();
    expect(dialog()).not.toBeNull();
    expect(dialog().querySelector('[role="alert"]').textContent).toBe('Enter the password.');
  });
});
