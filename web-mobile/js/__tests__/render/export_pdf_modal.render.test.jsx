import React from 'react';
import { render, act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';

// mp-yuy8: the print booklet (POST /api/print/:type) now SKIPS competitions
// it cannot export (Swiss, or a stored bracket that no longer matches the
// competition's current settings) instead of aborting the whole export, and
// reports them via the X-Skipped-Competitions header (partial success) or,
// when every competition was skipped, a 422 whose body carries the same
// list. Before this change ExportPdfModal surfaced NONE of that: a partial
// export just toasted "PDFs generated. Download started." with no mention of
// what was left out, and an all-skipped 422 read as an opaque failure.
//
// Mounted with REAL React so ExportPdfModal's own state (the `skipped` list
// set from window.API.exportPDFs's resolved/thrown value) actually updates
// across a render, which the fake-React unit-test stub (vitest.setup.js)
// cannot exercise: its useState always returns the initial value.

const noop = () => {};

const STUBBED_GLOBALS = {
  useEscapeToClose: () => {},
};

const originals = {};
let ExportPdfModal;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_shell.jsx');
  ExportPdfModal = window.ExportPdfModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

let savedURL;
beforeEach(() => {
  savedURL = { createObjectURL: window.URL.createObjectURL, revokeObjectURL: window.URL.revokeObjectURL };
  window.URL.createObjectURL = vi.fn(() => 'blob:mock');
  window.URL.revokeObjectURL = vi.fn();
  // appendChild(a); a.click() would otherwise attempt a real jsdom navigation
  // for the synthetic download anchor; the modal's own code path is what is
  // under test here, not the browser's download mechanics.
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
});

afterEach(() => {
  window.URL.createObjectURL = savedURL.createObjectURL;
  window.URL.revokeObjectURL = savedURL.revokeObjectURL;
  vi.restoreAllMocks();
});

const comps = [
  { id: 'c-swiss', name: 'U12 Swiss' },
  { id: 'c-mismatch', name: 'Senior Knockout' },
];

function mount({ exportPDFs, showToast = vi.fn(), compsProp = comps } = {}) {
  window.API = { exportPDFs };
  let result;
  act(() => {
    result = render(
      <ExportPdfModal
        tournament={{ name: 'Kanto Open' }}
        password="pw"
        comps={compsProp}
        showToast={showToast}
        onClose={noop}
      />
    );
  });
  return { ...result, showToast };
}

// Clicks the "all" row's Download button (PDF_EXPORT_TYPES[0], the first
// button in DOM order); every test here only exercises that one row.
function clickDownload(container) {
  const btn = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Download');
  fireEvent.click(btn);
}

describe('ExportPdfModal: skipped-competitions warning', () => {
  it('shows no banner and only the success toast when nothing was skipped', async () => {
    const exportPDFs = vi.fn().mockResolvedValue({ blob: new Blob([]), skipped: [] });
    const { container, showToast } = mount({ exportPDFs });

    clickDownload(container);

    await waitFor(() => expect(showToast).toHaveBeenCalledWith('PDFs generated. Download started.'));
    expect(container.querySelector('[data-testid="export-pdf-skipped-banner"]')).toBeNull();
  });

  it('shows a persistent warning naming each skipped competition by its resolved display name, after a partial success', async () => {
    const exportPDFs = vi.fn().mockResolvedValue({
      blob: new Blob([]),
      skipped: [
        { id: 'c-swiss', reason: 'Swiss competitions have no static bracket to export' },
        { id: 'unknown-comp-id', reason: 'stored bracket does not match current settings' },
      ],
    });
    const { container, showToast } = mount({ exportPDFs });

    clickDownload(container);

    // The success toast still fires (a partial export IS a success for the
    // competitions that DID export) alongside the persistent banner -- the
    // toast is not replaced, only supplemented.
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('PDFs generated. Download started.'));

    const banner = container.querySelector('[data-testid="export-pdf-skipped-banner"]');
    expect(banner).not.toBeNull();
    // Known id resolves to its display name from `comps`, not the raw id.
    expect(banner.textContent).toContain('U12 Swiss');
    expect(banner.textContent).toContain('Swiss competitions have no static bracket to export');
    // Unknown id (not present in `comps`) falls back to the raw id rather
    // than silently dropping the entry.
    expect(banner.textContent).toContain('unknown-comp-id');
    expect(banner.textContent).toContain('stored bracket does not match current settings');
  });

  it('shows the persistent warning after a 422 all-skipped failure, alongside the error toast', async () => {
    const err = new Error('no competitions could be exported; every competition was skipped');
    err.skipped = [{ id: 'c-mismatch', reason: 'stored bracket does not match current settings' }];
    const exportPDFs = vi.fn().mockRejectedValue(err);
    const { container, showToast } = mount({ exportPDFs });

    clickDownload(container);

    await waitFor(() => expect(showToast).toHaveBeenCalledWith(
      expect.stringContaining('every competition was skipped'), 'error'
    ));

    const banner = container.querySelector('[data-testid="export-pdf-skipped-banner"]');
    expect(banner).not.toBeNull();
    expect(banner.textContent).toContain('Senior Knockout');
    expect(banner.textContent).toContain('stored bracket does not match current settings');
  });

  it('shows no banner after a failure that carries no skipped list (e.g. 503 LibreOffice absent)', async () => {
    const exportPDFs = vi.fn().mockRejectedValue(new Error('LibreOffice unavailable'));
    const { container, showToast } = mount({ exportPDFs });

    clickDownload(container);

    await waitFor(() => expect(showToast).toHaveBeenCalledWith(
      expect.stringContaining('LibreOffice unavailable'), 'error'
    ));
    expect(container.querySelector('[data-testid="export-pdf-skipped-banner"]')).toBeNull();
  });
});
