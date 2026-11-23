// bc-hnam: a self-run tournament shows every name to every audience, so the
// page keeps nothing per audience there. A change that is a new audience in an
// officiated tournament (the phone takes a reveal key, the route moves between
// admin and public) reads nothing again and never shows the spinner, exactly
// as before names could be hidden (app_detail_refetch_audience covers the
// officiated case).
import React from 'react';
import { act, screen } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, settle, propsProbe } from '../helpers/mount_app.js';
import { rememberRevealKey } from '../../reveal_key.jsx';

const page = propsProbe(() => <div data-testid="viewer-competition" />);

const detail = {
  config: { id: 'c1', name: 'Open', kind: 'individual', format: 'mixed', status: 'pools', courts: ['A'], publicNames: 'hidden' },
  pools: [],
  poolMatches: [{ id: 'Pool A-0', status: 'running', court: 'A', modifiedAt: 100, ipponsA: [], ipponsB: [] }],
  standings: {},
  bracket: null,
};

const STUBBED_GLOBALS = {
  ViewerCompetition: page.Component,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'T', mode: 'self-run', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => [{ id: 'c1', name: 'Open', publicNames: 'hidden' }]),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => detail),
    subscribeToEvents: vi.fn(() => () => {}),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

afterAll(() => { unmount(); window.localStorage.clear(); });

describe('a self-run tournament keeps nothing per audience (bc-hnam)', () => {
  it('taking a reveal key reads nothing again and shows no spinner', async () => {
    ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
    expect(screen.getByTestId('viewer-competition')).toBeTruthy();
    const competitionReads = STUBBED_GLOBALS.API.fetchCompetitions.mock.calls.length;
    const detailReads = STUBBED_GLOBALS.API.fetchCompetitionDetails.mock.calls.length;

    await act(async () => { rememberRevealKey('c1', 'a'.repeat(64)); });
    expect(screen.queryByText('Loading...')).toBeNull();
    await settle();

    expect(screen.getByTestId('viewer-competition')).toBeTruthy();
    expect(STUBBED_GLOBALS.API.fetchCompetitions.mock.calls.length).toBe(competitionReads);
    expect(STUBBED_GLOBALS.API.fetchCompetitionDetails.mock.calls.length).toBe(detailReads);
  });
});
