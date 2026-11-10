// An announcement that reached the server after its time had run out (a send
// held offline for longer than it was to show) was never shown. The composer has
// already said it was sent, so it says afterwards that it did not go out. The
// court console's call is pinned beside this, in court_call_expired.render.test.jsx.
import React from 'react';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const API = {
  fetchAnnouncements: vi.fn().mockResolvedValue([]),
  sendAnnouncement: vi.fn(),
};

let restoreGlobals;
let AnnouncementComposer;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({ API });
  await import('../../admin_announcement.jsx');
  AnnouncementComposer = window.AnnouncementComposer;
});

afterAll(() => restoreGlobals());

async function sendWith(stored) {
  API.sendAnnouncement.mockResolvedValue({ pending: true, settled: Promise.resolve(stored), id: 'x' });
  const showToast = vi.fn();
  await act(async () => { render(<AnnouncementComposer password="pw" showToast={showToast} />); });
  fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Court A is open' } });
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Send announcement' })); });
  return showToast;
}

describe('the announcement composer after the server answers', () => {
  it('says the announcement did not go out when its time had run out on arrival', async () => {
    const showToast = await sendWith({ id: 'x', expired: true });
    await waitFor(() => expect(showToast).toHaveBeenCalledWith(expect.stringMatching(/^That announcement did not go out/), 'error'));
  });

  it('says nothing more when the announcement was shown', async () => {
    const showToast = await sendWith({ id: 'x' });
    await act(async () => {});
    expect(showToast).toHaveBeenCalledTimes(1);
    expect(showToast).toHaveBeenCalledWith(expect.stringMatching(/broadcast|saved/), 'success');
  });
});
