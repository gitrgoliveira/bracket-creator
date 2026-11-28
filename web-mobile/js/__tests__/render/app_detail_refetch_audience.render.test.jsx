// bc-hnam: a competition detail read that is still out when the page's read
// audience changes is dropped, never shown. The page's refetches (an SSE
// event, a resync) are sent as one audience; if the phone takes a reveal key
// or the organiser signs out while one is out, its answer was read with the
// other audience's names and must not reach the page.
import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, propsProbe } from '../helpers/mount_app.js';
import { HELD_PUSH_WAIT_MS } from '../../refetch_gate.jsx';

// settle: past the shortest delay the page's refetch gates draw (the start of
// their window, HELD_PUSH_WAIT_MS, createRefetchGate in refetch_gate.jsx).
const settle = () => act(async () => { await new Promise((r) => setTimeout(r, HELD_PUSH_WAIT_MS + 20)); });
import { rememberRevealKey } from '../../reveal_key.jsx';
import { noteReadSeq } from '../../read_seq.jsx';

// atSeq: a read's value as the fetcher returns it, carrying its answer's seq.
const atSeq = (value, seq) => noteReadSeq(value, { headers: { get: (k) => (k === 'X-Event-Seq' ? String(seq) : null) } });

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
    details.push(atSeq(detail(100), 1));
    ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
    expect(page.props.poolMatches[0].modifiedAt).toBe(100);

    // An event sends a refetch that stays out.
    let answerOld;
    details.push(new Promise((resolve) => { answerOld = resolve; }));
    const random = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => { sse.emit({ type: 'schedule_updated', seq: 4 }); });
      await settle();
    } finally {
      random.mockRestore();
    }

    // The phone takes a reveal key: a new audience, read again as that one.
    details.push(atSeq(detail(200), 2));
    await act(async () => { rememberRevealKey('c1', 'a'.repeat(64)); });
    await settle();
    expect(page.props.poolMatches[0].modifiedAt).toBe(200);

    // The old audience's answer lands now, and is not shown.
    await act(async () => { answerOld(atSeq(detail(999), 6)); });
    await settle();
    expect(page.props.poolMatches[0].modifiedAt).toBe(200);

    // Nor does the page count it as read: the answer named seq 6, but the
    // events up to it were never shown, so the next one still refetches.
    details.push(atSeq(detail(300), 5));
    const reads = STUBBED_GLOBALS.API.fetchCompetitionDetails.mock.calls.length;
    const again = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => { sse.emit({ type: 'schedule_updated', seq: 5 }); });
      await settle();
    } finally {
      again.mockRestore();
    }
    expect(STUBBED_GLOBALS.API.fetchCompetitionDetails.mock.calls.length).toBe(reads + 1);
    expect(page.props.poolMatches[0].modifiedAt).toBe(300);
  });
});
