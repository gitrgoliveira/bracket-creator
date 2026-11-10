// The blank-template download (POST /create) is a direct fetch: when the
// request never leaves the device, the operator reads the one "not sent"
// sentence (write_result.jsx NOT_SENT_NO_CONNECTION) as it is, never the
// browser's "Failed to fetch" inside "Export failed: ...". Any other failure
// keeps its own wording.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { API } from '../../api_client.jsx';
import { AdminExport } from '../../admin_schedule_export.jsx';
import { NOT_SENT_NO_CONNECTION } from '../../write_result.jsx';

const competition = {
  id: 'c1',
  name: 'Open Men',
  config: {
    format: 'pools',
    poolSize: 3,
    poolWinners: 2,
    courts: ['A'],
    players: [
      { name: 'Alice', dojo: 'DA' },
      { name: 'Bob', dojo: 'DB' },
      { name: 'Charlie', dojo: 'DC' },
    ],
  },
};

async function clickBlankTemplate() {
  const { container } = render(<AdminExport c={competition} t={{ id: 't1' }} password="pw" />);
  const button = Array.from(container.querySelectorAll('button')).find(
    (b) => b.textContent.includes('Download blank template')
  );
  await act(async () => { fireEvent.click(button); });
}

async function clickResults() {
  const { container } = render(<AdminExport c={competition} t={{ id: 't1' }} password="pw" />);
  const button = Array.from(container.querySelectorAll('button')).find(
    (b) => /results/i.test(b.textContent)
  );
  await act(async () => { fireEvent.click(button); });
}

describe('AdminExport results download', () => {
  const originalFetch = global.fetch;
  const originalAPI = window.API;
  afterEach(() => {
    global.fetch = originalFetch;
    window.API = originalAPI;
    vi.restoreAllMocks();
  });

  it('shows the not-sent sentence, unwrapped, when the request never leaves the device', async () => {
    window.API = API;
    const alert = vi.spyOn(window, 'alert').mockImplementation(() => {});
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    await clickResults();
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(alert).toHaveBeenCalledWith(NOT_SENT_NO_CONNECTION);
  });
});

describe('AdminExport blank template download', () => {
  const originalFetch = global.fetch;
  afterEach(() => {
    global.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('alerts the not-sent sentence, unwrapped, when the request never leaves the device', async () => {
    const alert = vi.spyOn(window, 'alert').mockImplementation(() => {});
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    await clickBlankTemplate();
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(alert).toHaveBeenCalledWith(NOT_SENT_NO_CONNECTION);
  });

  it('keeps a TypeError that is not the lost request (the body failing after the answer) in "Export failed"', async () => {
    const alert = vi.spyOn(window, 'alert').mockImplementation(() => {});
    global.fetch = vi.fn(() => Promise.resolve({
      ok: true,
      blob: () => Promise.reject(new TypeError('network error')),
    }));
    await clickBlankTemplate();
    expect(alert).toHaveBeenCalledWith('Export failed: network error');
  });

  it('keeps an answered refusal in its own "Export failed" wording', async () => {
    const alert = vi.spyOn(window, 'alert').mockImplementation(() => {});
    global.fetch = vi.fn(() => Promise.resolve({
      ok: false,
      text: () => Promise.resolve(JSON.stringify({ error: 'bad roster' })),
    }));
    await clickBlankTemplate();
    expect(alert).toHaveBeenCalledWith('Export failed: bad roster');
  });
});
