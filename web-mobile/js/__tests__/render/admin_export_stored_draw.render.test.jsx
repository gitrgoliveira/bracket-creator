// "Download blank template" always downloads from the server's export door
// (GET /api/competitions/:id/export, engine.ExportCompetitionXlsx, the source of
// the printed PDFs) with the organiser's password: the stored draw once there is
// one, and before the draw the blank layout the draw would print, its places
// empty. The app never posts the roster anywhere to draw a template of its own,
// which could seat competitors elsewhere than the draw does.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { API } from '../../api_client.jsx';
import { AdminExport, PRE_DRAW_NOTICE } from '../../admin_schedule_export.jsx';
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

// The card's last full-width button, whatever its label says while busy: the
// results button before it shows only once a result is recorded.
const templateButton = (container) => Array.from(container.querySelectorAll('button.btn--full')).at(-1);

async function clickBlankTemplate(c) {
  const { container } = render(<AdminExport c={c} t={{ id: 't1' }} password="pw" />);
  await act(async () => { fireEvent.click(templateButton(container)); });
  return container;
}

// A failed download is said on the tab, beside the button, never in an alert.
const templateError = (container) => container.querySelector('[data-testid="export-template-error"]')?.textContent ?? null;

const xlsxAnswer = () => Promise.resolve({
  ok: true,
  status: 200,
  headers: new Headers(),
  blob: () => Promise.resolve(new Blob(['xlsx'])),
  json: () => Promise.resolve({}),
});

// Before the draw the tab says only a blank template exists, and what sizes it;
// a drawn competition's template is its draw, and the notice goes.
describe('AdminExport pre-draw notice', () => {
  const notice = (c) => {
    const { container } = render(<AdminExport c={c} t={{ id: 't1' }} password="pw" />);
    return container.querySelector('[data-testid="export-pre-draw-notice"]');
  };

  it('says only a blank template can be downloaded until the draw is generated', () => {
    expect(notice(competition('setup')).textContent).toBe(PRE_DRAW_NOTICE);
  });

  it('is gone once the competition is drawn', () => {
    expect(notice(competition('draw-ready'))).toBeNull();
  });
});

// "Download results" shows only once a result is recorded (a match running or
// completed, a knockout bye resolved at the draw excluded); the blank template is
// always offered.
describe('AdminExport results button', () => {
  const buttons = (props) => {
    const { container } = render(<AdminExport c={competition('pools')} t={{ id: 't1' }} password="pw" {...props} />);
    return Array.from(container.querySelectorAll('button')).map((b) => b.textContent);
  };

  it('is hidden before any result is recorded, the blank template still offered', () => {
    const labels = buttons({ poolMatches: [{ status: 'scheduled' }] });
    expect(labels).toContain('Download blank template (.xlsx)');
    expect(labels).not.toContain('Download results (.xlsx)');
  });

  it('shows once a pool match is running or completed', () => {
    expect(buttons({ poolMatches: [{ status: 'running' }] })).toContain('Download results (.xlsx)');
  });

  it('ignores a knockout bye the draw resolved', () => {
    const bye = { status: 'completed', sideA: { id: 'a', name: 'Alice' }, sideB: { id: '', name: '' } };
    expect(buttons({ bracket: { rounds: [[bye]] } })).not.toContain('Download results (.xlsx)');
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

  it('downloads from the same door before the draw, never posting the roster to /create', async () => {
    global.fetch = vi.fn(xlsxAnswer);
    await clickBlankTemplate(competition('setup'));
    expect(global.fetch).toHaveBeenCalledTimes(1);
    const [url, init] = global.fetch.mock.calls[0];
    expect(url).toBe('/api/competitions/c1/export');
    expect(init.method).toBe('GET');
    expect(init.headers['X-Tournament-Password']).toBe('pw');
  });

  it('asks the server even with nobody registered, which sizes the layout from the expected number', async () => {
    global.fetch = vi.fn(xlsxAnswer);
    const c = competition('setup');
    c.config.players = [];
    await clickBlankTemplate(c);
    expect(global.fetch).toHaveBeenCalledTimes(1);
    expect(global.fetch.mock.calls[0][0]).toBe('/api/competitions/c1/export');
  });

  it('says the server sentence when a pre-draw layout cannot be sized', async () => {
    global.fetch = vi.fn(() => Promise.resolve({
      ok: false,
      status: 422,
      headers: new Headers(),
      json: () => Promise.resolve({ error: 'no draw yet, and fewer than 2 places to print: set the expected number of participants' }),
    }));
    const container = await clickBlankTemplate(competition('setup'));
    expect(templateError(container)).toBe('Export failed: no draw yet, and fewer than 2 places to print: set the expected number of participants');
    expect(window.alert).not.toHaveBeenCalled();
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
    const container = await clickBlankTemplate(competition('pools'));
    expect(templateError(container)).toBe('Export failed: the stored bracket no longer matches the settings');
  });

  it('says the not-sent sentence, unwrapped, when the request never leaves the device', async () => {
    global.fetch = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')));
    const container = await clickBlankTemplate(competition('draw-ready'));
    expect(templateError(container)).toBe(NOT_SENT_NO_CONNECTION);
  });

  it('keeps refusing Swiss without asking the server', async () => {
    global.fetch = vi.fn(xlsxAnswer);
    const c = competition('pools');
    c.config.format = 'swiss';
    const container = await clickBlankTemplate(c);
    expect(global.fetch).not.toHaveBeenCalled();
    expect(templateError(container)).toMatch(/Swiss competitions can't be exported/);
  });
});
