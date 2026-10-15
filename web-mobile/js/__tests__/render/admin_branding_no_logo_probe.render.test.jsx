// bc-lgnf: Edit tournament details learns whether a custom logo is set from the
// tournament it already holds (theme.hasLogo), so it sends no request that can
// answer 404 for "no logo". It used to probe HEAD /api/branding/logo, which the
// server answers 404 on purpose while none is uploaded, and the browser logged
// a console error on every load.
import React from 'react';
import { render, screen, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const API = {
  uploadBrandingLogo: vi.fn().mockResolvedValue({ logoPath: 'logo.png' }),
  deleteBrandingLogo: vi.fn().mockResolvedValue({}),
};

let restoreGlobals;
let BrandingManager;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({ API, confirmDialog: vi.fn().mockResolvedValue(true) });
  ({ BrandingManager } = await import('../../admin_branding.jsx'));
});
afterAll(() => restoreGlobals());

let fetchSpy;
beforeEach(() => {
  fetchSpy = vi.fn().mockResolvedValue({ ok: false, status: 404 });
  vi.stubGlobal('fetch', fetchSpy);
});
afterEach(() => vi.unstubAllGlobals());

const logoRequests = () => fetchSpy.mock.calls.filter(([url]) => String(url).includes('/api/branding/logo'));

const mount = async (theme) => {
  await act(async () => {
    render(<BrandingManager tournament={{ name: 'Cup', theme }} password="pw" showToast={() => {}} onThemeChange={() => {}} />);
  });
};

describe('BrandingManager and the custom logo', () => {
  it('with no logo: sends no logo request, offers Upload logo', async () => {
    await mount({});
    expect(logoRequests()).toHaveLength(0);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Upload logo' })).toBeTruthy();
    expect(screen.queryByText('Current logo')).toBeNull();
  });

  it('with no theme at all: sends no request either', async () => {
    await mount(undefined);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(screen.queryByText('Current logo')).toBeNull();
  });

  it('with a logo set: shows it without asking the server', async () => {
    await mount({ hasLogo: true });
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(screen.getByText('Current logo')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Replace logo' })).toBeTruthy();
  });

  it('uploading then removing a logo flips the preview with no probe', async () => {
    await mount({});
    const input = screen.getByLabelText(/Upload logo/);
    const file = new File([new Uint8Array([1, 2, 3])], 'logo.png', { type: 'image/png' });
    await act(async () => { fireEvent.change(input, { target: { files: [file] } }); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Upload logo' })); });
    expect(API.uploadBrandingLogo).toHaveBeenCalledTimes(1);
    expect(screen.getByText('Current logo')).toBeTruthy();

    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Remove' })); });
    expect(API.deleteBrandingLogo).toHaveBeenCalledWith('pw');
    expect(screen.queryByText('Current logo')).toBeNull();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('follows the tournament when another organiser changes the logo', async () => {
    let view;
    await act(async () => {
      view = render(<BrandingManager tournament={{ name: 'Cup', theme: {} }} password="pw" showToast={() => {}} onThemeChange={() => {}} />);
    });
    expect(screen.queryByText('Current logo')).toBeNull();
    await act(async () => {
      view.rerender(<BrandingManager tournament={{ name: 'Cup', theme: { hasLogo: true } }} password="pw" showToast={() => {}} onThemeChange={() => {}} />);
    });
    expect(screen.getByText('Current logo')).toBeTruthy();
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
