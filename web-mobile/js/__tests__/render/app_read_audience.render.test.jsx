// bc-hnam: the competitions a page holds are keyed by who they were read as.
//
// An organiser signed in on an admin route reads with the password and sees
// every name; the public page reads without it. On one laptop, moving from
// the admin route to the public page must never show the organiser's read on
// the public page: the competitions are dropped and read again as the public,
// and nothing of the first read is shown meanwhile.
import React from 'react';
import { act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp, settle } from '../helpers/mount_app.js';
import { readHeaders } from '../../api_client_requests.jsx';

const names = (props) => ((props.tournament && props.tournament.competitions) || []).map((c) => c.shown).join(',');

function ProbeAdminApp(props) {
  return (
    <div>
      <div data-testid="admin-comps">{names(props)}</div>
      <button type="button" onClick={props.onViewerMode}>Public viewer</button>
    </div>
  );
}

function ProbeViewerHome(props) {
  return <div data-testid="viewer-comps">{names(props)}</div>;
}

// What a read returns depends on the credential it was sent with, as the
// server's masking does.
const STUBBED_GLOBALS = {
  AdminApp: ProbeAdminApp,
  ViewerHome: ProbeViewerHome,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'London Cup', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => [{ id: 'juniors', shown: readHeaders()['X-Tournament-Password'] ? 'Aiko Sato' : 'J1' }]),
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
  window.localStorage.setItem('bc_authed', 'true');
  window.localStorage.setItem('bc_password', 'pw');
  ({ unmount } = await mountApp({ path: '/admin', globals: STUBBED_GLOBALS }));
});

afterAll(() => {
  unmount();
  window.localStorage.clear();
});

describe('the competitions held are keyed by who they were read as', () => {
  it('an admin route reads with the password', () => {
    expect(screen.getByTestId('admin-comps').textContent).toBe('Aiko Sato');
  });

  it('the public page on the same laptop reads again without it, and never shows the first read', async () => {
    const reads = window.API.fetchCompetitions.mock.calls.length;
    await act(async () => { screen.getByText('Public viewer').click(); });
    // Before the new read lands, the organiser's read is not on the page.
    expect(document.body.textContent).not.toContain('Aiko Sato');
    await settle();
    expect(window.API.fetchCompetitions.mock.calls.length).toBe(reads + 1);
    expect(screen.getByTestId('viewer-comps').textContent).toBe('J1');
    expect(document.body.textContent).not.toContain('Aiko Sato');
  });
});
