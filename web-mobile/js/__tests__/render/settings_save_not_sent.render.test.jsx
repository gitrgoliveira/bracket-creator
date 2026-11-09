// The Competition Settings save is a write that stays direct: when the request
// never leaves the device, the page says the one "not sent" sentence
// (write_result.jsx NOT_SENT_NO_CONNECTION) as it is, never the browser's
// "Failed to fetch" and never inside another sentence. onUpdate calls the real
// API door, the way admin.jsx's updateCompetition does (it toasts the message
// and throws the same error on), so the error under test is the one
// _directFetch makes.
import { act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../../api_client.jsx';
import { NOT_SENT_NO_CONNECTION } from '../../write_result.jsx';
import { installSettingsHarness, makeSettingsCompetition, mountSettings } from './settings_mount_harness.jsx';

installSettingsHarness();

describe('Competition Settings save with no connection', () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; });

  it('shows the not-sent sentence unwrapped beside Save', async () => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    const onUpdate = vi.fn((next) => API.updateCompetition('c1', next, ''));
    const { container } = await mountSettings(makeSettingsCompetition({ configVersion: 'v1' }), onUpdate);

    await act(async () => {
      fireEvent.change(container.querySelector('#settings-start-time'), { target: { value: '15:00' } });
    });
    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === 'Save changes'
    );
    await act(async () => { fireEvent.click(saveBtn); });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(global.fetch).toHaveBeenCalledTimes(1);
    const text = container.textContent;
    expect(text).toContain(`⚠ ${NOT_SENT_NO_CONNECTION}`);
    expect(text).not.toContain('Failed to fetch');
    expect(text).not.toContain('Save failed');
  });
});
