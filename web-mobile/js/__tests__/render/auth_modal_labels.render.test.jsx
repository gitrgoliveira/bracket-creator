// bc-lbla: the sign-in dialog's Password field is named by its visible label.
// Its input already carried id="admin-password" but the label had no htmlFor,
// so the field's accessible name fell back to the "••••••••" placeholder.
//
// AuthModal is not exported, so the real App is mounted on / with a probe in
// place of the home page, whose button is the Admin control (the harness of
// auth_modal_opened_tap.render.test.jsx). A keyboard-style click (detail 0)
// opens it, which the opened-tap guard never swallows.
import React from 'react';
import { act, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp } from '../helpers/mount_app.js';

function ProbeViewerHome(props) {
  return <button type="button" onClick={props.onAdminClick}>Open admin</button>;
}

const STUBBED_GLOBALS = {
  ViewerHome: ProbeViewerHome,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'London Cup', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => null),
    subscribeToEvents: vi.fn(() => () => {}),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

beforeAll(async () => {
  ({ unmount } = await mountApp({ path: '/', globals: STUBBED_GLOBALS }));
});

afterAll(() => { unmount(); });

describe('the sign-in dialog', () => {
  it('names the password field by its visible label', async () => {
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Open admin' })); });
    const input = screen.getByLabelText('Password');
    expect(input.id).toBe('admin-password');
    expect(input.type).toBe('password');
  });
});
