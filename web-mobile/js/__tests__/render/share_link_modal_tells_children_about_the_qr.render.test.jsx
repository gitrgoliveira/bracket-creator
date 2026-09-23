// ShareLinkModal (ui.jsx) drops its QR for a URL too long to encode, and a
// caller's note under it may need to know: the registration sheet's said
// "Participants can scan this QR code or open the link" whether or not a code
// was there. The modal now accepts `children` as a function of showQR, so the
// caller words its note for the sheet actually rendered without measuring the
// URL a second time. This pins the contract from the caller's side: the
// argument agrees with the canvas, in both directions, and a plain node child
// still renders.
import React from 'react';
import { render, act, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

// Well inside the QR budget, and well outside it (QR_MAX_BYTES is a few
// hundred bytes; 4000 is over any version-40 byte-mode capacity).
const SHORT = 'http://192.168.1.20:8080/register';
const LONG = 'http://192.168.1.20:8080/register?t=' + 'x'.repeat(4000);

let ShareLinkModal;

beforeAll(async () => {
  // qr.jsx publishes window.qrFits / window.renderQR; ui.jsx (loaded by the
  // render setup) reads them at render time, as in the browser.
  await import('../../qr.jsx');
  ShareLinkModal = window.ShareLinkModal;
});
beforeEach(() => cleanup());

async function mount(url, children) {
  await act(async () => {
    render(<ShareLinkModal title="Share" url={url} onClose={vi.fn()}>{children}</ShareLinkModal>);
  });
}

describe('ShareLinkModal tells a function child whether the QR is shown', () => {
  it('a URL that fits: showQR is true and the canvas is there', async () => {
    const seen = vi.fn(() => null);
    await mount(SHORT, seen);
    expect(seen).toHaveBeenCalledWith(true);
    expect(document.querySelector('canvas.share-link__qr')).not.toBeNull();
  });

  it('a URL that does not fit: showQR is false and there is no canvas', async () => {
    const seen = vi.fn(() => null);
    await mount(LONG, seen);
    expect(seen).toHaveBeenCalledWith(false);
    expect(document.querySelector('canvas.share-link__qr')).toBeNull();
  });

  it('a plain node child still renders', async () => {
    await mount(SHORT, <p className="share-link__note">note</p>);
    expect(document.querySelector('.share-link__note').textContent).toBe('note');
  });
});
