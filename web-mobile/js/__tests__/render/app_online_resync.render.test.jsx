// A page that lost its event stream reloads once the stream reopens. The hub
// replays the events the page missed, but not one it received whose load then
// failed because the device could not fetch: nothing retried that load, so
// the page stayed behind until the next event came. A participant's score
// sheet then still showed a representative bout another device had removed,
// so the server's refusal ("Check the scores and finish again") pointed at a
// sheet that had not changed.
//
// Mounts the real App on / with a stub API and a props probe in place of the
// home page, as app_failed_reload_keeps_tournament does.
import React from 'react';
import { act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { mountApp, settle } from '../helpers/mount_app.js';

function ProbeViewerHome(props) {
  return <div data-testid="viewer-home">{props.tournament && props.tournament.name}</div>;
}

const sse = { emit: null, status: null };

const STUBBED_GLOBALS = {
  ViewerHome: ProbeViewerHome,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'London Cup', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => null),
    subscribeToEvents: vi.fn((onEvent, onStatus) => { sse.emit = onEvent; sse.status = onStatus; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

const shown = () => screen.getByTestId('viewer-home').textContent;
// The reload is jittered; with Math.random at 0 it runs at once.
const status = async (s) => {
  const random = vi.spyOn(Math, 'random').mockReturnValue(0);
  try {
    await act(async () => { sse.status(s); });
    await settle();
  } finally {
    random.mockRestore();
  }
};
// The server's tournament changes while the page is not looking.
let edition = 1;
const serverChanges = () => {
  edition += 1;
  const name = `London Cup ${edition}`;
  window.API.fetchTournament.mockImplementation(async () => ({ name, courts: ['A'] }));
  return name;
};

let unmount;

beforeAll(async () => {
  ({ unmount } = await mountApp({ path: '/', globals: STUBBED_GLOBALS }));
  await status('open');
});

afterAll(() => { unmount(); });

afterEach(() => { window.API.reconnectEvents.mockClear(); });

describe('reloading after the event stream was lost', () => {
  it('a stream that opens without having been lost does not reload', async () => {
    serverChanges();
    const loads = window.API.fetchTournament.mock.calls.length;
    await status('open');
    expect(window.API.fetchTournament.mock.calls.length).toBe(loads);
    expect(window.API.reconnectEvents).not.toHaveBeenCalled();
  });

  it('a stream that reopens after it was lost reloads, even when a load during the outage failed', async () => {
    const name = serverChanges();
    await status('error');

    // An event reached the page during the outage; the load it started failed.
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      window.API.fetchTournament.mockRejectedValueOnce(new TypeError('Failed to fetch'));
      await act(async () => { sse.emit({ type: 'resync_required', seq: 0 }); });
      await settle();
    } finally {
      logged.mockRestore();
    }
    expect(shown()).not.toBe(name);

    await status('open');
    expect(shown()).toBe(name);
    expect(window.API.reconnectEvents, 'the reopen itself asks for no second reconnect').not.toHaveBeenCalled();
  });

  it('coming back online reopens the stream, and the reopen reloads', async () => {
    const name = serverChanges();
    // The stream outlived the outage, so it never reported a loss.
    await act(async () => { window.dispatchEvent(new Event('online')); });
    await settle();
    expect(window.API.reconnectEvents).toHaveBeenCalledTimes(1);
    expect(shown()).not.toBe(name);

    await status('open');
    expect(shown()).toBe(name);
  });
});

describe('coming back to the tab', () => {
  const resume = async () => {
    const visible = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
    try {
      await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
      await settle();
    } finally {
      visible.mockRestore();
    }
  };

  it('a tab whose stream held refreshes at once', async () => {
    const name = serverChanges();
    await resume();
    expect(window.API.reconnectEvents).toHaveBeenCalledTimes(1);
    expect(shown()).toBe(name);
  });

  it('a tab whose stream was lost loads once, when the stream reopens', async () => {
    const name = serverChanges();
    await status('error');
    const before = window.API.fetchTournament.mock.calls.length;

    await resume();
    expect(window.API.reconnectEvents).toHaveBeenCalledTimes(1);
    expect(window.API.fetchTournament.mock.calls.length, 'nothing loads before the stream is back').toBe(before);

    await status('open');
    expect(shown()).toBe(name);
    expect(window.API.fetchTournament.mock.calls.length, 'one load for the resume and the reopen together').toBe(before + 1);
  });
});
