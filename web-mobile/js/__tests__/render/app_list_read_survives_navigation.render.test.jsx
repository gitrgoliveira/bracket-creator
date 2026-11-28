// bc-sseq FR4: the tournament list does not depend on the competition shown,
// so a list read that was out (or waiting for its window) when the viewer went
// from a competition back to the lobby is not lost: the page that takes over
// asks for it again, instead of keeping a stale list until the next event.
//
// Mounts the real App on /competition/c1 (app.jsx renders itself into #root
// when it is imported) with a list read that never answers, leaves for the
// lobby, and counts the list reads.
import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp, propsProbe } from '../helpers/mount_app.js';
import { HELD_PUSH_WAIT_MS } from '../../refetch_gate.jsx';
import { noteReadSeq } from '../../read_seq.jsx';

const page = propsProbe(() => <div data-testid="viewer-competition" />);

const withSeq = (value, seq) => noteReadSeq(value, { headers: { get: (k) => (k === 'X-Event-Seq' ? String(seq) : null) } });

const sse = { emit: null };
let hangNextList = false;

const STUBBED_GLOBALS = {
  ViewerCompetition: page.Component,
  ViewerHome: () => <div data-testid="viewer-home" />,
  API: {
    fetchTournament: vi.fn(() => (hangNextList ? new Promise(() => {}) : Promise.resolve(withSeq({ name: 'T', courts: ['A'] }, 20)))),
    fetchCompetitions: vi.fn(async () => withSeq([{ id: 'c1', name: 'Open', date: '2026-11-01' }], 20)),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => withSeq({
      config: { id: 'c1', name: 'Open', kind: 'individual', format: 'league', status: 'pools', courts: ['A'], date: '2026-11-01' },
      pools: [], poolMatches: [], standings: {}, bracket: null,
    }, 20)),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

const loads = () => window.API.fetchTournament.mock.calls.length;
const pastWindow = () => act(async () => { await new Promise((r) => setTimeout(r, HELD_PUSH_WAIT_MS + 40)); });

let unmount;
let random;

beforeAll(async () => {
  random = vi.spyOn(Math, 'random').mockReturnValue(0);
  ({ unmount } = await mountApp({ path: '/competition/c1', globals: STUBBED_GLOBALS }));
  await pastWindow();
});

afterAll(() => {
  random.mockRestore();
  unmount();
});

describe('a list read owed when the viewer leaves a competition (bc-sseq FR4)', () => {
  it('is asked for again by the lobby, not forgotten', async () => {
    // An event above the list's reads sends a list fetch that never answers.
    hangNextList = true;
    const before = loads();
    await act(async () => { sse.emit({ type: 'tournament_updated', seq: 30, data: null }); });
    await pastWindow();
    expect(loads(), 'the list read is out').toBe(before + 1);

    // The viewer goes back to the lobby while it is out.
    hangNextList = false;
    window.history.pushState(null, '', '/');
    await act(async () => { window.dispatchEvent(new PopStateEvent('popstate')); });
    await pastWindow();
    expect(loads(), 'the lobby asks for the list read that was out').toBeGreaterThan(before + 1);
  });
});
