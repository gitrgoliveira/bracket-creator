// bc-sseq FR4: the public competition page fetches once per burst. Every
// event that used to schedule its own refetch asks a gate (createRefetchGate,
// patch.jsx), which sends one fetch per burst and none for an event the answer
// it applied already holds (the answer's X-Event-Seq, read_seq.jsx).
//
// Mounts the real App on /competition/c1 (app.jsx renders itself into #root
// when it is imported) with a stub API whose reads carry a seq, and a props
// probe in place of the competition page. The figure "five quick taps cause
// one detail fetch" in docs/architecture/network-architecture.md section 7
// and infrastructure-architecture.md section 5 is this test's count.
import React from 'react';
import { act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { mountApp, propsProbe } from '../helpers/mount_app.js';
import { HELD_PUSH_WAIT_MS } from '../../patch.jsx';
import { noteReadSeq } from '../../read_seq.jsx';

const page = propsProbe(() => <div data-testid="viewer-competition" />);

// withSeq: a read's value as the fetcher returns it, carrying its answer's seq.
const withSeq = (value, seq) => noteReadSeq(value, { headers: { get: (k) => (k === 'X-Event-Seq' ? String(seq) : null) } });

const head = { detail: 20, tournament: 20, competitions: 22 };
const running = (ipponsA) => ({ id: 'Pool A-0', status: 'running', court: 'A', modifiedAt: 100 + ipponsA.length, ipponsA, ipponsB: [] });
const detail = () => withSeq({
  config: { id: 'c1', name: 'Open', kind: 'individual', format: 'league', status: 'pools', courts: ['A'], date: '2026-11-01' },
  pools: [], poolMatches: [running([])], standings: {}, bracket: null,
}, head.detail);

const sse = { emit: null, status: null };

const STUBBED_GLOBALS = {
  ViewerCompetition: page.Component,
  ViewerHome: () => <div data-testid="viewer-home" />,
  API: {
    fetchTournament: vi.fn(async () => withSeq({ name: 'T', courts: ['A'] }, head.tournament)),
    fetchCompetitions: vi.fn(async () => withSeq([{ id: 'c1', name: 'Open', date: '2026-11-01' }], head.competitions)),
    fetchAuthConfig: vi.fn(async () => ({ mode: 'file', resetEnabled: true })),
    fetchAnnouncements: vi.fn(async () => []),
    fetchCompetitionDetails: vi.fn(async () => detail()),
    subscribeToEvents: vi.fn((onEvent, onStatus) => { sse.emit = onEvent; sse.status = onStatus; return () => {}; }),
    reconnectEvents: vi.fn(),
    resumeAfterAuth: vi.fn(),
  },
};

const details = () => window.API.fetchCompetitionDetails.mock.calls.length;
const loads = () => window.API.fetchTournament.mock.calls.length;

// emit sends events inside one act, as a burst arrives; window waits past the
// gate's delay (Math.random is 0, so it is HELD_PUSH_WAIT_MS exactly).
const emit = (...events) => act(async () => { events.forEach((e) => sse.emit(e)); });
const pastWindow = () => act(async () => { await new Promise((r) => setTimeout(r, HELD_PUSH_WAIT_MS + 40)); });
const tap = (seq, ipponsA) => ({ type: 'match_updated', seq, data: { competitionId: 'c1', result: running(ipponsA) } });

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

describe('the public competition page fetches once per burst (bc-sseq FR4)', () => {
  it('an event the first read already holds fetches nothing, and its push still shows', async () => {
    expect(page.props, 'the competition page rendered').toBeTruthy();
    const before = details();
    // The stream's first events were sent before the page's read began: the
    // read, merged into the page (keep(prev, data)), already holds them.
    await emit(tap(19, ['M']), tap(20, ['M', 'K']));
    await pastWindow();
    expect(details(), 'no refetch for events at or below the read').toBe(before);
    expect(page.props.poolMatches[0].ipponsA, 'the push itself is applied').toEqual(['M', 'K']);
  });

  it('the list is covered only up to the lower of its two reads', async () => {
    const before = loads();
    // The tournament read named 20, the competitions read 22: an event at 21
    // is in the competitions but maybe not in the tournament.
    await emit({ type: 'tournament_updated', seq: 21, data: null });
    await pastWindow();
    expect(loads(), 'one load for an event above the lower seq').toBe(before + 1);
  });

  it('five quick taps cause one detail fetch', async () => {
    head.detail = 26;
    const before = details();
    await emit(tap(22, ['M']), tap(23, ['M', 'K']), tap(24, ['M', 'K', 'D']), tap(25, ['M', 'K', 'D', 'T']), tap(26, ['M', 'K', 'D', 'T', 'H']));
    expect(details(), 'nothing is sent before the held push wait').toBe(before);
    await pastWindow();
    expect(details(), 'one fetch for the burst of five').toBe(before + 1);
    await pastWindow();
    expect(details(), 'and no second one: its answer named the last tap').toBe(before + 1);
  });

  it('an event the refetch answered already fetches nothing', async () => {
    // The answer above named 26; an event at 27 is above it, so it
    // refetches, and that read, named 29, covers 28 and 29 when the stream
    // delivers them after it.
    head.detail = 29;
    const before = details();
    await emit(tap(27, ['M']));
    await pastWindow();
    expect(details(), 'an event above the answer').toBe(before + 1);
    await emit(tap(28, ['M', 'K']), tap(29, ['M', 'K', 'D']));
    await pastWindow();
    expect(details(), 'events the answer named').toBe(before + 1);
  });

  it('resync_required forgets every seq and refetches whatever they say', async () => {
    const before = details();
    const beforeLoads = loads();
    head.detail = 3;
    head.tournament = 3;
    head.competitions = 3;
    await emit({ type: 'resync_required', seq: 3 });
    await pastWindow();
    expect(details(), 'the detail is read again').toBe(before + 1);
    expect(loads(), 'the tournament is read again').toBe(beforeLoads + 1);
    // A new stream: its events above the new reads refetch.
    await emit(tap(4, ['M']));
    await pastWindow();
    expect(details(), 'the new stream counts from its own reads').toBe(before + 2);
  });

  it('a stream that reopens after an error restarts the gates: a restarted server\'s low seqs still refetch', async () => {
    // The server restarted: its head seq starts again, and the page, which
    // held no id'd event, gets no resync_required.
    head.detail = 1;
    head.tournament = 1;
    head.competitions = 1;
    await pastWindow();
    const before = details();
    const beforeLoads = loads();
    await act(async () => { sse.status('error'); });
    await act(async () => { sse.status('open'); });
    await pastWindow();
    expect(details(), 'the detail is read again').toBe(before + 1);
    expect(loads(), 'the tournament is read again').toBe(beforeLoads + 1);
  });

  it('a list refetch that fails is asked for once more, after the same wait', async () => {
    head.tournament = 50;
    head.competitions = 50;
    await pastWindow();
    const before = loads();
    window.API.fetchCompetitions.mockRejectedValueOnce(new Error('offline'));
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    await emit({ type: 'tournament_updated', seq: 50, data: null });
    await pastWindow();
    expect(loads(), 'the failed read').toBe(before + 1);
    await pastWindow();
    expect(loads(), 'the gate asked once more, and it answered').toBe(before + 2);
    await pastWindow();
    expect(loads(), 'and no third one: the answer named the event').toBe(before + 2);
    error.mockRestore();
  });

  it('a reconnect followed by resync_required reads the list once', async () => {
    head.tournament = 60;
    head.competitions = 60;
    await pastWindow();
    const beforeLoads = loads();
    await act(async () => { sse.status('error'); });
    await act(async () => { sse.status('open'); });
    // The reopened stream restarted the gate; resync_required restarts it
    // again, and both ask for the same one read.
    await emit({ type: 'resync_required', seq: 3 });
    await pastWindow();
    expect(loads(), 'one list read for the two').toBe(beforeLoads + 1);
  });

  it('a restart seen away from the competition drops the seq the page kept for it', async () => {
    // The page reads the detail at 40 and leaves for the lobby, which keeps
    // 40 for a gate made later.
    head.detail = 40;
    await emit(tap(39, ['M']));
    await pastWindow();
    window.history.pushState(null, '', '/');
    await act(async () => { window.dispatchEvent(new PopStateEvent('popstate')); });
    await pastWindow();
    // The server restarts while the page is on the lobby: its seqs begin again.
    head.detail = 3;
    head.tournament = 3;
    head.competitions = 3;
    await emit({ type: 'resync_required', seq: 3 });
    await pastWindow();
    window.history.pushState(null, '', '/competition/c1');
    await act(async () => { window.dispatchEvent(new PopStateEvent('popstate')); });
    await pastWindow();
    const before = details();
    await emit(tap(4, ['M', 'K']));
    await pastWindow();
    expect(details(), 'an event of the new stream is above the read the page holds').toBe(before + 1);
  });
});
