// Tests for API.exportPDFs (mp-yuy8). The print booklet endpoint
// (POST /api/print/:type) now skips competitions it cannot export instead of
// aborting the whole booklet, and reports them via the X-Skipped-Competitions
// response header (wire format "<id>: <reason> | <id>: <reason>", see
// internal/mobileapp/handlers_print.go's skippedCompetitionsHeaderValue) or,
// when EVERY competition was skipped, a 422 JSON body carrying the same list
// under `skipped`. exportPDFs must surface both: a resolved `{ blob, skipped }`
// pair on success, and an Error carrying a `.skipped` array on failure -- ONE
// contract, since the caller (ExportPdfModal) has to render the same warning
// banner from either path.
//
// The delimiter is " | ", not "; ": the server's own sentinel reason
// strings (e.g. engine.ErrSwissExportUnsupported) contain semicolons, so a
// "; " delimiter would tear one entry's reason into two and silently drop
// the tail. The tests below use that exact sentinel text, not a synthetic
// reason -- a synthetic fixture with no semicolon in it is exactly what let
// this bug ship the first time.

import { describe, it, expect, vi } from 'vitest';
import { API } from '../api_client.jsx';

function mockFetchBlob(status, { headers = {}, blob = new Blob([]), jsonBody = {} } = {}) {
  return vi.fn(() =>
    Promise.resolve({
      ok: status >= 200 && status < 300,
      status,
      headers: { get: (name) => headers[name] ?? null },
      blob: () => Promise.resolve(blob),
      json: () => Promise.resolve(jsonBody),
    })
  );
}

describe('API.exportPDFs', () => {
  let originalFetch;
  beforeEach(() => { originalFetch = global.fetch; });
  afterEach(() => { global.fetch = originalFetch; });

  it('returns { blob, skipped: [] } when the header is absent (nothing skipped)', async () => {
    const blob = new Blob(['pdf-bytes']);
    global.fetch = mockFetchBlob(200, { blob });
    const result = await API.exportPDFs('all', 'pw');
    expect(result.blob).toBe(blob);
    expect(result.skipped).toEqual([]);
  });

  it('parses the X-Skipped-Competitions header into [{id, reason}, ...]', async () => {
    global.fetch = mockFetchBlob(200, {
      headers: {
        'X-Skipped-Competitions':
          'c-swiss: Swiss competitions have no static bracket to export; use the live standings view instead' +
          ' | c-mismatch: stored bracket does not match current settings',
      },
    });
    const result = await API.exportPDFs('all', 'pw');
    expect(result.skipped).toEqual([
      {
        id: 'c-swiss',
        reason: 'Swiss competitions have no static bracket to export; use the live standings view instead',
      },
      { id: 'c-mismatch', reason: 'stored bracket does not match current settings' },
    ]);
  });

  it('parses a single skipped entry using the REAL Swiss sentinel text, which itself ' +
    'contains a "; " -- the exact byte sequence that broke the old "; "-delimited parse ' +
    '(only the first half of the reason survived, the rest silently dropped)', async () => {
    const realSwissReason =
      'not yet implemented: Swiss competitions have no static bracket to export; ' +
      'use the live standings view instead';
    global.fetch = mockFetchBlob(200, {
      headers: { 'X-Skipped-Competitions': `c1: ${realSwissReason}` },
    });
    const result = await API.exportPDFs('all', 'pw');
    expect(result.skipped).toEqual([{ id: 'c1', reason: realSwissReason }]);
    expect(result.skipped).toHaveLength(1);
  });

  it('parses a single skipped entry (no " | " separator present)', async () => {
    global.fetch = mockFetchBlob(200, {
      headers: { 'X-Skipped-Competitions': 'c1: some reason' },
    });
    const result = await API.exportPDFs('all', 'pw');
    expect(result.skipped).toEqual([{ id: 'c1', reason: 'some reason' }]);
  });

  it('attaches the 422 all-skipped body\'s `skipped` array to the thrown Error', async () => {
    global.fetch = mockFetchBlob(422, {
      jsonBody: {
        error: 'no competitions could be exported; every competition was skipped',
        skipped: [
          { id: 'c-swiss', name: 'U12 Swiss', reason: 'Swiss competitions have no static bracket to export' },
        ],
      },
    });
    const err = await API.exportPDFs('all', 'pw').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e
    );
    expect(err.message).toBe('no competitions could be exported; every competition was skipped');
    expect(err.skipped).toEqual([
      { id: 'c-swiss', name: 'U12 Swiss', reason: 'Swiss competitions have no static bracket to export' },
    ]);
  });

  it('gives a failure with no `skipped` field in the body an empty array, not undefined', async () => {
    global.fetch = mockFetchBlob(503, {
      jsonBody: { error: 'PDF generation requires LibreOffice.' },
    });
    const err = await API.exportPDFs('all', 'pw').then(
      () => { throw new Error('expected a rejection'); },
      (e) => e
    );
    expect(err.message).toBe('PDF generation requires LibreOffice.');
    expect(err.skipped).toEqual([]);
  });
});
