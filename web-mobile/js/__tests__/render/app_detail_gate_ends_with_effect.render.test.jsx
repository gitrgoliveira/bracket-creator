// bc-sseq: a competition page's refetch gate ends with the page's effect. A
// detail read still out when the viewer opens another competition must not be
// shown (it would replace the new competition's detail whole), and its gate
// must not send another read for the competition left.
import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, afterAll } from 'vitest';
import { mountApp, propsProbe } from '../helpers/mount_app.js';
import { HELD_PUSH_WAIT_MS } from '../../refetch_gate.jsx';

// settle: past the shortest delay the page's refetch gates draw (the start of
// their window, HELD_PUSH_WAIT_MS, createRefetchGate in refetch_gate.jsx).
const settle = () => act(async () => { await new Promise((r) => setTimeout(r, HELD_PUSH_WAIT_MS + 20)); });

const page = propsProbe(() => <div data-testid="viewer-competition" />);

const detail = (id, modifiedAt) => ({
  config: { id, name: id, kind: 'individual', format: 'mixed', status: 'pools', courts: ['A'] },
  pools: [],
  poolMatches: [{ id: 'Pool A-0', status: 'running', court: 'A', modifiedAt, ipponsA: [], ipponsB: [] }],
  standings: {},
  bracket: null,
});

const sse = { emit: null };
const answers = {};

const STUBBED_GLOBALS = {
  ViewerCompetition: page.Component,
  API: {
    fetchTournament: vi.fn(async () => ({ name: 'T', mode: 'file', courts: ['A'] })),
    fetchCompetitions: vi.fn(async () => []),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async (id) => answers[id].shift()),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

let unmount;

afterAll(() => { unmount(); window.localStorage.clear(); });

describe('a detail read out while another competition is opened is dropped (bc-sseq)', () => {
  it('the answer of the competition left never reaches the page, nor does it ask again', async () => {
    answers.c1 = [detail('c1', 100)];
    answers.c2 = [detail('c2', 200)];
    ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
    expect(page.props.poolMatches[0].modifiedAt).toBe(100);

    // An event for c1 sends a refetch that stays out; a second event, seen
    // while it is out, would have the gate ask once more when it landed.
    let answerOld;
    answers.c1.push(new Promise((resolve) => { answerOld = resolve; }));
    const random = vi.spyOn(Math, 'random').mockReturnValue(0);
    try {
      await act(async () => { sse.emit({ type: 'schedule_updated', seq: 1 }); });
      await settle();
      await act(async () => { sse.emit({ type: 'schedule_updated', seq: 2 }); });

      // The viewer opens c2.
      window.history.pushState(null, '', '/competition/c2');
      await act(async () => { window.dispatchEvent(new PopStateEvent('popstate')); });
      await settle();
      expect(page.props.poolMatches[0].modifiedAt).toBe(200);

      // c1's answer lands now: not shown, and c1 is not read again.
      const reads = STUBBED_GLOBALS.API.fetchCompetitionDetails.mock.calls.filter(([id]) => id === 'c1').length;
      answers.c1.push(detail('c1', 301));
      await act(async () => { answerOld(detail('c1', 999)); });
      await settle();
      expect(page.props.poolMatches[0].modifiedAt).toBe(200);
      expect(STUBBED_GLOBALS.API.fetchCompetitionDetails.mock.calls.filter(([id]) => id === 'c1')).toHaveLength(reads);
    } finally {
      random.mockRestore();
    }
  });
});
