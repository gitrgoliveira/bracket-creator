// Review of PR #483 (app_version.jsx): the automatic reload onto a new build
// fired whenever no score editor was open and the outbox was idle, so a list
// half pasted into the participants box (or any form not yet saved) was lost
// with no prompt. A form holding typed input registers into the one registry
// (registerUnsavedInput, through useUnsavedInput) while it does, so the page
// shows the banner instead and reloads only once the input is saved or cleared.
import React from 'react';
import { render, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import * as V from '../../app_version.jsx';
import { installParticipantsHarness } from './admin_participants_mount_harness.jsx';

installParticipantsHarness();

let reload;

beforeEach(() => {
  document.head.innerHTML = '<meta name="app-build" content="build-a">';
  V.resetVersionForTests();
  reload = vi.fn();
  V.setReloader(reload);
});

afterEach(() => {
  document.head.innerHTML = '';
  V.resetVersionForTests();
});

function Host() {
  const c = { id: 'c1', name: 'Autumn Cup', status: 'setup', format: 'mixed', kind: 'individual',
    poolSize: 4, poolWinners: 2, checkInEnabled: false, withZekkenName: false, numberPrefix: 'K',
    players: [], rosterVersion: 'v1' };
  return (
    <window.AdminParticipants c={c} tournament={{ name: 'Spring Taikai', courts: ['A'] }}
      onUpdate={async () => []} password="" showToast={() => {}} onSection={() => {}} />
  );
}

describe('a form holding unsaved input holds the automatic reload', () => {
  it('a list typed into the participants box shows the banner instead of reloading', async () => {
    let r;
    await act(async () => { r = render(<Host />); });
    const box = r.container.querySelector('textarea');
    await act(async () => { fireEvent.change(box, { target: { value: 'Alice, D1\nBob, D2' } }); });

    act(() => { V.noteServerBuild('build-b'); });
    expect(reload).not.toHaveBeenCalled();
    expect(V.versionState()).toBe('stale');

    // Cleared, nothing is held: the page reloads onto the new build.
    await act(async () => { fireEvent.change(box, { target: { value: '' } }); });
    expect(reload).toHaveBeenCalledTimes(1);
    r.unmount();
  });

  it('an empty participants box does not hold the reload', async () => {
    let r;
    await act(async () => { r = render(<Host />); });
    act(() => { V.noteServerBuild('build-b'); });
    expect(reload).toHaveBeenCalledTimes(1);
    r.unmount();
  });

  it('useUnsavedInput registers only while it holds input, and unmounting lets go', async () => {
    function Form({ text }) { V.useUnsavedInput(text.trim() !== ''); return null; }
    let r;
    await act(async () => { r = render(<Form text="half typed" />); });
    act(() => { V.noteServerBuild('build-b'); });
    expect(reload).not.toHaveBeenCalled();
    await act(async () => { r.unmount(); });
    expect(reload).toHaveBeenCalledTimes(1);
  });
});
