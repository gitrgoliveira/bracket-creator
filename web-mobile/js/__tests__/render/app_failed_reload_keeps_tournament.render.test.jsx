// A failed background reload never shows the create-tournament screen over a
// tournament already on screen. App's load() runs on every server event, so a
// network blip or a 5xx mid-event used to drop the page to "Welcome to Bracket
// Creator", as though no tournament existed. A failed fetch says nothing about
// that: the reload keeps the last good data, and only the server's own "no
// tournament" answer (fetchTournament resolving null) opens the setup screen.
//
// Mounts the real App on / (app.jsx renders itself into #root when it is
// imported) with a stub API and a props probe in place of the home page, then
// makes the reloads fail.
import React from 'react';
import { act, screen } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, settle } from '../helpers/mount_app.js';

const home = { props: null };
function ProbeViewerHome(props) {
  home.props = props;
  return <div data-testid="viewer-home">{props.tournament && props.tournament.name}</div>;
}

const sse = { emit: null };

const STUBBED_GLOBALS = {
  ViewerHome: ProbeViewerHome,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'London Cup', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => null),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

// A server event that reloads the tournament at once (resync_required calls
// load() with no jitter).
const reload = async () => {
  await act(async () => { sse.emit({ type: 'resync_required', seq: 0 }); });
  await settle();
};

let unmount;

afterAll(() => { unmount(); });

describe('a failed reload keeps the tournament on screen', () => {
  it('only the server\'s "no tournament" answer opens the setup screen', async () => {
    ({ unmount } = await mountApp({ path: '/', globals: STUBBED_GLOBALS }));
    expect(screen.getByTestId('viewer-home')).toHaveTextContent('London Cup');

    // load() logs each failure; that log is expected here.
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      // The tournament fetch fails (a network blip, or a 5xx, which
      // fetchTournament throws on).
      window.API.fetchTournament.mockRejectedValueOnce(new Error('Failed to fetch tournament (Status 503)'));
      await reload();
      expect(window.API.fetchTournament).toHaveBeenCalledTimes(2);
      expect(screen.queryByText('Welcome to Bracket Creator')).toBeNull();
      expect(screen.getByTestId('viewer-home')).toHaveTextContent('London Cup');
      expect(home.props.tournament.name).toBe('London Cup');

      // The tournament answers but its competitions do not.
      window.API.fetchCompetitions.mockRejectedValueOnce(new TypeError('Failed to fetch'));
      await reload();
      expect(window.API.fetchTournament).toHaveBeenCalledTimes(3);
      expect(screen.queryByText('Welcome to Bracket Creator')).toBeNull();
      expect(screen.getByTestId('viewer-home')).toHaveTextContent('London Cup');

      expect(logged.mock.calls.map((c) => c[0])).toEqual(['Failed to load tournament', 'Failed to load tournament']);
    } finally {
      logged.mockRestore();
    }

    // The server says there is no tournament: now the setup screen opens.
    window.API.fetchTournament.mockResolvedValueOnce(null);
    await reload();
    expect(window.API.fetchTournament).toHaveBeenCalledTimes(4);
    expect(screen.getByText('Welcome to Bracket Creator')).toBeInTheDocument();
    expect(screen.queryByTestId('viewer-home')).toBeNull();
  });
});
