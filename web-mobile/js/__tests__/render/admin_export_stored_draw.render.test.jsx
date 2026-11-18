// Once a competition has a draw, "Download blank template" prints THAT draw:
// it downloads the stored-draw workbook (GET /api/competitions/:id/export,
// engine.ExportCompetitionXlsx, the source of the printed PDFs) with the
// organiser's password, and never posts the roster to /create, which would
// draw the competition again in registration order and could seat its
// competitors elsewhere. Before the draw there is nothing stored, and /create
// still draws the template.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../../api_client.jsx';
import { AdminExport, templateFromStoredDraw } from '../../admin_schedule_export.jsx';
import { NOT_SENT_NO_CONNECTION } from '../../write_result.jsx';

const competition = (status) => ({
  id: 'c1',
  name: 'Open Men',
  status,
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
});

// The card's second full-width button, whatever its label says while busy.
const templateButton = (container) => container.querySelectorAll('button.btn--full')[1];

async function clickBlankTemplate(c) {
  const { container } = render(<AdminExport c={c} t={{ id: 't1' }} password="pw" />);
  await act(async () => { fireEvent.click(templateButton(container)); });
}

const xlsxAnswer = () => Promise.resolve({
  ok: true,
  status: 200,
  headers: new Headers(),
  blob: () => Promise.resolve(new Blob(['xlsx'])),
  json: () => Promise.resolve({}),
});

describe('templateFromStoredDraw', () => {
  it('reads every status but setup as drawn', () => {
    for (const s of ['draw-ready', 'pools', 'knockout', 'completed', 'invalid']) {
      expect(templateFromStoredDraw(s), s).toBe(true);
    }
    for (const s of ['setup', '', undefined, null]) {
      expect(templateFromStoredDraw(s), String(s)).toBe(false);
    }
  });
});

describe('AdminExport public viewer link', () => {
  it("is the app's own public page for the competition", () => {
    const { container } = render(<AdminExport c={competition('pools')} t={{ name: 'Cup' }} password="pw" />);
    const link = container.querySelector('input[readonly]').value;
    expect(link).toBe(`${window.location.origin}/competition/c1`);
  });
});

describe('AdminExport blank template source', () => {
  const originalFetch = global.fetch;
  const originalAPI = window.API;
  const originalCreate = window.URL.createObjectURL;
  const originalRevoke = window.URL.revokeObjectURL;
  beforeEach(() => {
    window.API = API;
    window.URL.createObjectURL = vi.fn(() => 'blob:template');
    window.URL.revokeObjectURL = vi.fn();
    vi.spyOn(window, 'alert').mockImplementation(() => {});
  });
  afterEach(() => {
    global.fetch = originalFetch;
    window.API = originalAPI;
    window.URL.createObjectURL = originalCreate;
    window.URL.revokeObjectURL = originalRevoke;
    vi.restoreAllMocks();
  });

  for (const status of ['draw-ready', 'pools', 'knockout', 'completed']) {
    it(`downloads the stored draw, with the password, for a ${status} competition`, async () => {
      global.fetch = vi.fn(xlsxAnswer);
      await clickBlankTemplate(competition(status));
      expect(global.fetch).toHaveBeenCalledTimes(1);
      const [url, init] = global.fetch.mock.calls[0];
      expect(url).toBe('/api/competitions/c1/export');
      expect(init.method).toBe('GET');
      expect(init.headers['X-Tournament-Password']).toBe('pw');
      expect(window.URL.createObjectURL).toHaveBeenCalledTimes(1);
      expect(window.alert).not.toHaveBeenCalled();
    });
  }

  it('posts the roster to /create before the draw', async () => {
    global.fetch = vi.fn(xlsxAnswer);
    await clickBlankTemplate(competition('setup'));
    expect(global.fetch).toHaveBeenCalledTimes(1);
    const [url, init] = global.fetch.mock.calls[0];
    expect(url).toBe('/create');
    expect(init.method).toBe('POST');
  });

  it('sends one request for a double tap and holds the button while the download is out', async () => {
    let answer;
    global.fetch = vi.fn(() => new Promise((resolve) => { answer = resolve; }));
    const { container } = render(<AdminExport c={competition('pools')} t={{ id: 't1' }} password="pw" />);
    const button = () => templateButton(container);
    await act(async () => { fireEvent.click(button()); });
    await act(async () => { fireEvent.click(button()); });
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(button().disabled).toBe(true);
    expect(button().textContent).toContain('Preparing');
    await act(async () => { answer(await xlsxAnswer()); });
    expect(button().disabled).toBe(false);
    expect(button().textContent).toContain('Download blank template');
    expect(window.URL.createObjectURL).toHaveBeenCalledTimes(1);
  });

  it('says the stored export refusal in its "Export failed" wording', async () => {
    global.fetch = vi.fn(() => Promise.resolve({
      ok: false,
      status: 422,
      headers: new Headers(),
      json: () => Promise.resolve({ error: 'the stored bracket no longer matches the settings' }),
    }));
    await clickBlankTemplate(competition('pools'));
    expect(window.alert).toHaveBeenCalledWith('Export failed: the stored bracket no longer matches the settings');
  });

  it('alerts the not-sent sentence, unwrapped, when the request never leaves the device', async () => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    await clickBlankTemplate(competition('draw-ready'));
    expect(window.alert).toHaveBeenCalledWith(NOT_SENT_NO_CONNECTION);
  });

  it('keeps refusing Swiss without asking the server', async () => {
    global.fetch = vi.fn(xlsxAnswer);
    const c = competition('pools');
    c.config.format = 'swiss';
    await clickBlankTemplate(c);
    expect(global.fetch).not.toHaveBeenCalled();
    expect(window.alert.mock.calls[0][0]).toMatch(/Swiss competitions can't be exported/);
  });
});
