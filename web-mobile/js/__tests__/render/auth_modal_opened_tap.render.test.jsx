// bc-cfbd: the admin sign-in modal, opened by a tap on the viewer's Admin
// control, survives the bounce of that tap. The second click of a double tap
// lands on the fresh backdrop one render later, and the backdrop's onClick is
// the modal's dismiss, which closed it at once; landing on Sign in it submitted
// an empty password. tap_guard.jsx's useOpenedTapGuard swallows every pointer
// click in the layer for TAP_BOUNCE_MS after it opens; a deliberate tap after
// the window acts as before, and keyboard activation (detail 0) is never
// swallowed.
//
// AuthModal is not exported, so the real App is mounted on / with a probe in
// place of the home page, whose button is the Admin control. App mounts once
// per file, with real timers (mountApp settles on one); each test then fakes
// them and leaves the modal closed.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them and these tests could never go red.
import React from 'react';
import { act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { mountApp } from '../helpers/mount_app.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

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

beforeEach(() => { vi.useFakeTimers(); });

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });
const backdrop = () => document.querySelector('.modal-backdrop');
const signIn = () => screen.getByRole('button', { name: 'Sign in' });
const EMPTY_PASSWORD = 'Enter a password.';

afterEach(async () => {
  if (backdrop()) {
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(backdrop());
  }
  vi.useRealTimers();
});

async function openSignIn() {
  expect(backdrop()).toBeNull();
  await pointerTap(screen.getByRole('button', { name: 'Open admin' }));
  expect(backdrop()).not.toBeNull();
}

describe('the sign-in modal opened by a tap', () => {
  it('survives the bounce landing on its backdrop', async () => {
    await openSignIn();
    await wait(30);
    await pointerTap(backdrop());
    expect(backdrop()).not.toBeNull();
  });

  it('survives the bounce landing on Sign in without submitting an empty password', async () => {
    await openSignIn();
    await wait(30);
    await pointerTap(signIn());
    expect(backdrop()).not.toBeNull();
    expect(screen.queryByText(EMPTY_PASSWORD)).toBeNull();
  });

  it('is dismissed by a deliberate tap on the backdrop after the window', async () => {
    await openSignIn();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(backdrop());
    expect(backdrop()).toBeNull();
  });

  it('submits at once from the keyboard (detail 0 is never a bounce)', async () => {
    await openSignIn();
    await wait(30);
    await keyboardClick(signIn());
    expect(screen.getByText(EMPTY_PASSWORD)).toBeTruthy();
  });
});
