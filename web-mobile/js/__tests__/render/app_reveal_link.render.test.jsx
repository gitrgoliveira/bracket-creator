// bc-hnam: a reveal link opened on a phone.
//
// /reveal/<competition>#<key>: the key is kept on the phone and leaves the
// address bar before anything reads, the competition is read with it, a
// per-person link follows the competitor the server says its key reveals, and the page
// opens the competition in place of the link.
//
// Mounts the real App with a stub API and a probe in place of the
// competition page, as app_online_resync does.
import React from 'react';
import { screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp } from '../helpers/mount_app.js';

const KEY = 'd'.repeat(64);

function ProbeViewerCompetition(props) {
  return <div data-testid="viewer-competition">{props.competition && props.competition.id}</div>;
}

const DETAIL = {
  namesHidden: true,
  revealedCompetitors: ['p2'],
  config: {
    id: 'juniors',
    name: 'Juniors',
    players: [
      { id: 'p1', name: 'J1', number: 'J1', dojo: 'Kyoto', nameHidden: true },
      { id: 'p2', name: 'Aiko Sato', number: 'J2', dojo: 'Osaka' },
    ],
  },
};

const STUBBED_GLOBALS = {
  ViewerCompetition: ProbeViewerCompetition,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'London Cup', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => DETAIL),
    subscribeToEvents: vi.fn(() => () => {}),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

beforeAll(async () => {
  window.localStorage.clear();
  ({ unmount } = await mountApp({ path: `/reveal/juniors#${KEY}`, globals: STUBBED_GLOBALS }));
});

afterAll(() => {
  unmount();
  window.localStorage.clear();
});

describe('opening a reveal link', () => {
  it('keeps the key on the phone and takes it off the address bar', () => {
    expect(window.localStorage.getItem('bc_reveal_keys')).toContain(KEY);
    expect(window.location.hash).toBe('');
    expect(window.location.href).not.toContain(KEY);
  });

  it('follows the competitor the per-person link reveals, and opens the competition in its place', () => {
    expect(screen.getByTestId('viewer-competition').textContent).toBe('juniors');
    expect(window.location.pathname).toBe('/competition/juniors');
    const followed = JSON.parse(window.localStorage.getItem('bc_watchlist') || '[]');
    expect(followed.map((e) => e.id)).toEqual(['p2']);
  });
});
