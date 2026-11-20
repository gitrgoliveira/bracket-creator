// The blank-template download (GET /api/competitions/:id/export) is a direct fetch: when the
// request never leaves the device, the operator reads the one "not sent"
// sentence (write_result.jsx NOT_SENT_NO_CONNECTION) as it is, never the
// browser's "Failed to fetch" inside "Export failed: ...". Any other failure
// keeps its own wording. The template's failure is said on the tab beside its
// button; the results download, which has a toast host, falls back to an alert.
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
  return container.querySelector('[data-testid="export-template-error"]')?.textContent ?? null;
}

async function clickResults() {
  // The results button shows once a result is recorded.
  const { container } = render(<AdminExport c={competition} t={{ id: 't1' }} password="pw" poolMatches={[{ status: 'completed' }]} />);
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

  it('says the not-sent sentence, unwrapped, when the request never leaves the device', async () => {
    const alert = vi.spyOn(window, 'alert').mockImplementation(() => {});
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    expect(await clickBlankTemplate()).toBe(NOT_SENT_NO_CONNECTION);
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(alert).not.toHaveBeenCalled();
  });

  it('keeps an answered refusal in its own "Export failed" wording', async () => {
    const alert = vi.spyOn(window, 'alert').mockImplementation(() => {});
    global.fetch = vi.fn(() => Promise.resolve({
      ok: false,
      json: () => Promise.resolve({ error: 'bad roster' }),
    }));
    expect(await clickBlankTemplate()).toBe('Export failed: bad roster');
    expect(alert).not.toHaveBeenCalled();
  });
});
