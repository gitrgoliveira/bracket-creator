import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { installSettingsHarness, makeSettingsCompetition } from './settings_mount_harness.jsx';

installSettingsHarness();

function renderSettings(initialComp, onUpdate) {
  const AdminComp = window.AdminCompetition;
  const t = {
    name: 'Spring Taikai',
    courts: ['A', 'B'],
    competitions: [initialComp, { id: 'c2', name: 'Yudansha' }],
  };

  const Wrapper = ({ comp }) => (
    <AdminComp
      tournament={t}
      competition={comp}
      pools={[]}
      poolMatches={[]}
      standings={[]}
      bracket={null}
      section="settings"
      onSection={() => {}}
      onBack={() => {}}
      onOpenCompetition={() => {}}
      onUpdate={onUpdate}
      onRefreshCompetition={() => {}}
      onMoveCourt={() => {}}
      onEditScore={() => {}}
      onLogout={() => {}}
      onViewerMode={() => {}}
      tweaks={{}}
      password=""
      showToast={() => {}}
    />
  );

  let rendered;
  act(() => {
    rendered = render(<Wrapper comp={initialComp} />);
  });

  return {
    ...rendered,
    updateComp: async (newComp) => {
      await act(async () => {
        rendered.rerender(<Wrapper comp={newComp} />);
      });
    },
  };
}

describe('Settings configVersion pinning, conflict alert, and reload', () => {
  it('1. an SSE update after edit leaves the pinned version: Save sends the OLD version, 409 config_changed shows alert naming the change', async () => {
    const onUpdate = vi.fn();
    const initialComp = makeSettingsCompetition({
      configVersion: 'v-initial',
      startTime: '09:00',
    });

    const { container, updateComp } = renderSettings(initialComp, onUpdate);

    // Operator edits Start time to 15:00
    const startInput = container.querySelector('#settings-start-time');
    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    // SSE update arrives AFTER edit with a new version and start time
    const sseComp = {
      ...initialComp,
      configVersion: 'v-server-new',
      startTime: '10:30',
    };
    await updateComp(sseComp);

    // Save should send the pinned OLD version ('v-initial')
    const configChangedErr = new Error('This competition changed, reload it.');
    configChangedErr.code = 'config_changed';
    configChangedErr.configVersion = 'v-server-new';
    configChangedErr.competition = sseComp;
    onUpdate.mockRejectedValueOnce(configChangedErr);

    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Save changes'
    );
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onUpdate.mock.calls[0][0].configVersion).toBe('v-initial');

    // The alert appears naming the operator's change
    const alert = container.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert.textContent).toContain('You had changed: Start time to 15:00.');
    expect(alert.textContent).toContain('Not saved: this competition changed somewhere else');
  });

  it('2. an SSE update before any edit is absorbed: then an edit and Save send the NEW version and succeed', async () => {
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v-final' });
    const initialComp = makeSettingsCompetition({
      configVersion: 'v-old',
      startTime: '09:00',
    });

    const { container, updateComp } = renderSettings(initialComp, onUpdate);

    // SSE update arrives BEFORE any edit
    const sseComp = {
      ...initialComp,
      configVersion: 'v-fresh',
      startTime: '10:00',
    };
    await updateComp(sseComp);

    // Input was updated by SSE to 10:00
    const startInput = container.querySelector('#settings-start-time');
    expect(startInput.value).toBe('10:00');

    // Now operator edits Start time to 14:00
    await act(async () => {
      fireEvent.change(startInput, { target: { value: '14:00' } });
    });

    // Save sends the NEW version 'v-fresh'
    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Save changes'
    );
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(onUpdate.mock.calls[0][0].configVersion).toBe('v-fresh');
    expect(onUpdate.mock.calls[0][0].startTime).toBe('14:00');
  });

  it('3. a 409 shows the alert, disables Save, and onUpdate is called exactly once', async () => {
    const onUpdate = vi.fn();
    const initialComp = makeSettingsCompetition({
      configVersion: 'v-base',
      startTime: '09:00',
    });

    const { container } = renderSettings(initialComp, onUpdate);

    const startInput = container.querySelector('#settings-start-time');
    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    const configChangedErr = new Error('This competition changed, reload it.');
    configChangedErr.code = 'config_changed';
    configChangedErr.configVersion = 'v-server-curr';
    configChangedErr.competition = { ...initialComp, configVersion: 'v-server-curr', startTime: '12:00' };
    onUpdate.mockRejectedValueOnce(configChangedErr);

    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Save changes'
    );
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    expect(onUpdate).toHaveBeenCalledTimes(1);

    // Alert is displayed
    const alert = container.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();

    // Save buttons are disabled while conflict is shown
    const allSaveBtns = Array.from(container.querySelectorAll('button')).filter(
      (b) => b.textContent.trim() === 'Save changes'
    );
    expect(allSaveBtns.length).toBeGreaterThan(0);
    for (const btn of allSaveBtns) {
      expect(btn.disabled).toBe(true);
    }
  });

  it('4. Reload loads the current values, enables Save, and a following edit + Save sends the conflict configVersion', async () => {
    const onUpdate = vi.fn();
    const initialComp = makeSettingsCompetition({
      configVersion: 'v-base',
      startTime: '09:00',
    });

    const { container, updateComp } = renderSettings(initialComp, onUpdate);

    const startInput = container.querySelector('#settings-start-time');
    await act(async () => {
      fireEvent.change(startInput, { target: { value: '15:00' } });
    });

    const serverComp = {
      ...initialComp,
      configVersion: 'v-from-conflict',
      startTime: '12:00',
    };
    const configChangedErr = new Error('This competition changed, reload it.');
    configChangedErr.code = 'config_changed';
    configChangedErr.configVersion = 'v-from-conflict';
    configChangedErr.competition = serverComp;
    onUpdate.mockRejectedValueOnce(configChangedErr);

    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Save changes'
    );
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    expect(onUpdate).toHaveBeenCalledTimes(1);

    // In the real app, AdminApp.updateCompetition merges e.competition into its state on config_changed,
    // so Settings receives the conflict's competition as prop `c` before Reload is tapped:
    await updateComp(serverComp);

    // Click Reload button in the alert
    const alert = container.querySelector('[role="alert"]');
    const reloadBtn = Array.from(alert.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Reload'
    );
    expect(reloadBtn).toBeDefined();

    await act(async () => {
      fireEvent.click(reloadBtn);
    });

    // Alert is cleared
    expect(container.querySelector('[role="alert"]')).toBeNull();

    // Start time input updated to the conflict competition's value
    expect(container.querySelector('#settings-start-time').value).toBe('12:00');

    // Reload leaves nothing unsaved, so Save waits for a change, as on a fresh
    // page: the conflict no longer holds it.
    const activeSaveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Save changes'
    );
    expect(activeSaveBtn.disabled).toBe(true);

    // Operator edits again, and Save is enabled
    await act(async () => {
      fireEvent.change(container.querySelector('#settings-start-time'), { target: { value: '16:00' } });
    });
    expect(activeSaveBtn.disabled).toBe(false);

    // Save succeeds and sends the conflict's configVersion ('v-from-conflict')
    onUpdate.mockResolvedValueOnce({ configVersion: 'v-after-save' });
    await act(async () => {
      fireEvent.click(activeSaveBtn);
    });

    expect(onUpdate).toHaveBeenCalledTimes(2);
    expect(onUpdate.mock.calls[1][0].configVersion).toBe('v-from-conflict');
    expect(onUpdate.mock.calls[1][0].startTime).toBe('16:00');
  });
});
