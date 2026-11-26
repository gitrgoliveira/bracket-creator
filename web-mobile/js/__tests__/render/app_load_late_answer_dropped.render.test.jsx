// bc-sseq FR4: a list read made OUTSIDE the list gate (load: the first load, a
// tab resume, a reload) is held to the gate's late-answer rule. A slow one that
// lands after the gate applied a newer answer is dropped: shown, it would put
// older data back while the gate's applied seq stayed above it, and every event
// up to that seq would then read as covered, so nothing healed the screen.
//
// Mounts the real App on / with a props probe in place of the home page. A tab
// resume starts a load whose answer is held back; an event above it sends the
// gate's read, which answers first; then the held answer lands.
import React from 'react';
import { act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp, settle } from '../helpers/mount_app.js';
import { HELD_PUSH_WAIT_MS } from '../../patch.jsx';
import { noteReadSeq } from '../../read_seq.jsx';

const withSeq = (value, seq) => noteReadSeq(value, { headers: { get: (k) => (k === 'X-Event-Seq' ? String(seq) : null) } });

const sse = { emit: null };
// The tournament reads the stub answers, in order: a read answers {name, seq}
// once its `held` promise (when it has one) is released.
const script = [];
let compSeq = 20;

const STUBBED_GLOBALS = {
  ViewerHome: (props) => <div data-testid="viewer-home">{props.tournament && props.tournament.name}</div>,
  API: {
    fetchTournament: vi.fn(async () => {
      const next = script.shift() || { name: 'Edition 1', seq: 20 };
      if (next.held) await next.held;
      compSeq = next.seq;
      return withSeq({ name: next.name, courts: ['A'] }, next.seq);
    }),
    fetchCompetitions: vi.fn(async () => withSeq([], compSeq)),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => null),
    subscribeToEvents: vi.fn((onEvent) => { sse.emit = onEvent; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

const shown = () => screen.getByTestId('viewer-home').textContent;
const pastWindow = () => act(async () => { await new Promise((r) => setTimeout(r, HELD_PUSH_WAIT_MS + 40)); });

let unmount;
let random;

beforeAll(async () => {
  random = vi.spyOn(Math, 'random').mockReturnValue(0);
  ({ unmount } = await mountApp({ path: '/', globals: STUBBED_GLOBALS }));
  await pastWindow();
});

afterAll(() => {
  random.mockRestore();
  unmount();
});

describe('a list read made outside the list gate lands after a newer gate answer (bc-sseq FR4)', () => {
  it('is dropped, so older data never goes back over the newer answer', async () => {
    expect(shown()).toBe('Edition 1');

    // A tab resume reads the list outside the gate; its answer is held back.
    let release;
    script.push({ name: 'Old edition', seq: 25, held: new Promise((r) => { release = r; }) });
    const visible = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
    try {
      await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
    } finally {
      visible.mockRestore();
    }
    await settle();

    // An event above it sends the gate's read, which answers first.
    script.push({ name: 'New edition', seq: 40 });
    await act(async () => { sse.emit({ type: 'tournament_updated', seq: 40, data: null }); });
    await pastWindow();
    expect(shown(), 'the gate showed the newer answer').toBe('New edition');

    // The held answer, read at seq 25, lands now.
    await act(async () => { release(); });
    await settle();
    expect(shown(), 'the older answer never goes back over it').toBe('New edition');

    // And the applied seq was not left claiming the older data: an event the
    // newer answer covers needs no read, one above it asks for one.
    const reads = window.API.fetchTournament.mock.calls.length;
    await act(async () => { sse.emit({ type: 'tournament_updated', seq: 35, data: null }); });
    await pastWindow();
    expect(window.API.fetchTournament.mock.calls.length, 'an event the applied answer covers reads nothing').toBe(reads);
    script.push({ name: 'Newest edition', seq: 60 });
    await act(async () => { sse.emit({ type: 'tournament_updated', seq: 50, data: null }); });
    await pastWindow();
    expect(window.API.fetchTournament.mock.calls.length, 'an event above the applied seq reads again').toBe(reads + 1);
    expect(shown()).toBe('Newest edition');
  });
});
