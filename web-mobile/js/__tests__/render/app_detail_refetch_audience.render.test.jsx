// bc-hnam: a competition detail read that is still out when the page's read
// audience changes is dropped, never shown. The page's refetches (an SSE
// event, a resync) are sent as one audience; if the phone takes a reveal key
// or the organiser signs out while one is out, its answer was read with the
// other audience's names and must not reach the page.
import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, settle, propsProbe } from '../helpers/mount_app.js';
import { rememberRevealKey } from '../../reveal_key.jsx';

const page = propsProbe(() => <div data-testid="viewer-competition" />);

const detail = (modifiedAt) => ({
  config: { id: 'c1', name: 'Open', kind: 'individual', format: 'mixed', status: 'pools', courts: ['A'] },
  pools: [],
  poolMatches: [{ id: 'Pool A-0', status: 'running', court: 'A', modifiedAt, ipponsA: [], ipponsB: [] }],
  standings: {},
  bracket: null,
});

const sse = { emit: null };
const details = [];

const STUBBED_GLOBALS = {
  ViewerCompetition: page.Component,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'T', mode: 'file', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => details.shift()),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

afterAll(() => { unmount(); window.localStorage.clear(); });

describe('a detail read out while the read audience changes is dropped (bc-hnam)', () => {
  it('the answer of a refetch sent as the old audience never reaches the page', async () => {
    details.push(detail(100));
    ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
    expect(page.props.poolMatches[0].modifiedAt).toBe(100);

    // An event sends a refetch that stays out.
    let answerOld;
    details.push(new Promise((resolve) => { answerOld = resolve; }));
    const random = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => { sse.emit({ type: 'schedule_updated' }); });
      await settle();
    } finally {
      random.mockRestore();
    }

    // The phone takes a reveal key: a new audience, read again as that one.
    details.push(detail(200));
    await act(async () => { rememberRevealKey('c1', 'a'.repeat(64)); });
    await settle();
    expect(page.props.poolMatches[0].modifiedAt).toBe(200);

    // The old audience's answer lands now, and is not shown.
    await act(async () => { answerOld(detail(999)); });
    await settle();
    expect(page.props.poolMatches[0].modifiedAt).toBe(200);
  });
});
