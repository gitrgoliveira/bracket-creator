// bc-sseq FR4: the admin competition page fetches once per burst. Its
// per-event detail refetch and its tournament refresh each ask a gate
// (createRefetchGate, patch.jsx), which sends one fetch per burst and none for
// an event the answer it applied already holds (X-Event-Seq, read_seq.jsx).
// Every read of the detail made outside the events (the first one, the data
// issue banner's Reload) tells the gate the seq it applied.
import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { HELD_PUSH_WAIT_MS } from '../../patch.jsx';
import { noteReadSeq } from '../../read_seq.jsx';

const probe = { props: null };
const sse = { emit: null };

const withSeq = (value, seq) => noteReadSeq(value, { headers: { get: (k) => (k === 'X-Event-Seq' ? String(seq) : null) } });

const head = { detail: 20, competitions: 20 };
const config = { id: 'c1', name: 'Autumn Cup', status: 'pools', format: 'league', kind: 'individual', courts: ['A'], players: [] };
const running = (ipponsA) => ({ id: 'Pool A-0', status: 'running', court: 'A', modifiedAt: 100 + ipponsA.length, ipponsA, ipponsB: [] });
const detail = () => withSeq({ config: { ...config }, pools: [], poolMatches: [running([])], standings: {}, bracket: null }, head.detail);

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import by admin.jsx.
  AdminCompetition: (props) => { probe.props = props; return <div data-testid="competition" />; },
  confirmDialog: vi.fn(),
  API: {
    subscribeToEvents: vi.fn((onEvent, onStatus) => { sse.emit = onEvent; sse.status = onStatus; return () => {}; }),
    fetchCompetitions: vi.fn(async () => withSeq([{ ...config }], head.competitions)),
    fetchTournament: vi.fn(async () => withSeq({ name: 'Cup', courts: ['A'] }, head.competitions)),
    fetchCompetitionDetails: vi.fn(async () => detail()),
  },
};

const details = () => window.API.fetchCompetitionDetails.mock.calls.length;
const lists = () => window.API.fetchCompetitions.mock.calls.length;
const emit = (...events) => act(async () => { events.forEach((e) => sse.emit(e)); });
// Math.random is 0: each gate sends at HELD_PUSH_WAIT_MS exactly.
const pastWindow = () => act(async () => { await new Promise((r) => setTimeout(r, HELD_PUSH_WAIT_MS + 40)); });
const tap = (seq, ipponsA) => ({ type: 'match_updated', seq, data: { competitionId: 'c1', result: running(ipponsA) } });

let restore;
let random;
let AdminApp;

beforeAll(async () => {
  restore = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin.jsx');
  AdminApp = window.AdminApp;
  random = vi.spyOn(Math, 'random').mockReturnValue(0);
});
afterAll(() => {
  random.mockRestore();
  restore();
});

// mount opens the competition page; its first detail read answers at `seq`.
// Each test mounts its own: the render cleanup unmounts it after the test.
const adminElement = (id) => (
  <AdminApp
    tournament={{ name: 'Cup', courts: ['A'], competitions: [{ ...config }] }}
    onUpdate={vi.fn()} onLogout={vi.fn()} onViewerMode={vi.fn()} onPasswordChange={vi.fn()}
    tweaks={{}} password="pw" view={{ kind: 'competition', id, section: 'overview' }} setView={vi.fn()}
    showToast={vi.fn()} authConfig={{}}
  />
);

async function mount(seq) {
  head.detail = seq;
  head.competitions = seq;
  let view;
  await act(async () => { view = render(adminElement('c1')); });
  await pastWindow();
  return view;
}

describe('the admin competition page fetches once per burst (bc-sseq FR4)', () => {
  it('an event the first read already holds refetches no detail', async () => {
    const before = details();
    await mount(20);
    expect(probe.props, 'the competition page rendered').toBeTruthy();
    expect(details(), 'the first read').toBe(before + 1);
    await emit(tap(19, ['M']), tap(20, ['M', 'K']));
    await pastWindow();
    expect(details(), 'no refetch for events at or below the first read').toBe(before + 1);
  });

  it('five quick taps cause one detail fetch and one tournament refresh', async () => {
    await mount(20);
    head.detail = 25;
    head.competitions = 25;
    const before = details();
    const beforeLists = lists();
    await emit(tap(21, ['M']), tap(22, ['M', 'K']), tap(23, ['M', 'K', 'D']), tap(24, ['M', 'K', 'D', 'T']), tap(25, ['M', 'K', 'D', 'T', 'H']));
    expect(details(), 'nothing is sent before the held push wait').toBe(before);
    await pastWindow();
    expect(details(), 'one detail fetch for the burst of five').toBe(before + 1);
    expect(lists(), 'one tournament refresh for the burst of five').toBe(beforeLists + 1);
    await pastWindow();
    expect(details(), 'and no second one').toBe(before + 1);
  });

  it('a Reload outside the events tells the gate what it read', async () => {
    await mount(20);
    head.detail = 30;
    const before = details();
    let answer;
    await act(async () => { answer = await probe.props.onRefreshCompetition(); });
    expect(answer).toEqual({ ok: true });
    expect(details(), 'the Reload read').toBe(before + 1);
    // The stream delivers 21 to 30 after the Reload's answer named 30.
    await emit(...[21, 22, 23, 24, 25, 26, 27, 28, 29, 30].map((seq) => tap(seq, ['M'])));
    await pastWindow();
    expect(details(), 'events the Reload already held').toBe(before + 1);
    await emit(tap(31, ['M', 'K']));
    await pastWindow();
    expect(details(), 'an event above it').toBe(before + 2);
  });

  it('a stream that reopens after an error restarts both gates, so a restarted server\'s seqs refetch', async () => {
    await mount(20);
    // The server restarted: its head seq starts again, and the page, which
    // held no id'd event, gets no resync_required.
    head.detail = 0;
    head.competitions = 0;
    const before = details();
    const beforeLists = lists();
    await act(async () => { sse.status('error'); });
    await act(async () => { sse.status('open'); });
    await pastWindow();
    expect(details(), 'the detail is read again').toBe(before + 1);
    expect(lists(), 'the competitions are read again').toBe(beforeLists + 1);
    head.detail = 1;
    head.competitions = 1;
    await emit(tap(1, ['M']));
    await pastWindow();
    expect(details(), 'an event the old 20 would have called covered').toBe(before + 2);
  });

  // The settings gate applied a high seq from its own read, then the server restarted.
  async function settingsReadAt30() {
    await mount(20);
    head.competitions = 30;
    await emit({ type: 'tournament_updated', seq: 30, data: null });
    await pastWindow();
    head.competitions = 0;
    return window.API.fetchTournament.mock.calls.length;
  }

  it('a stream that reopens after an error reads the settings again', async () => {
    const before = await settingsReadAt30();
    await act(async () => { sse.status('error'); });
    await act(async () => { sse.status('open'); });
    await pastWindow();
    expect(window.API.fetchTournament.mock.calls.length, 'the settings are read again').toBe(before + 1);
  });

  it('resync_required reads the settings again, so a restarted server\'s tournament_updated refetches', async () => {
    const before = await settingsReadAt30();
    await emit({ type: 'resync_required', seq: 0 });
    await pastWindow();
    expect(window.API.fetchTournament.mock.calls.length, 'the settings are read again').toBe(before + 1);
    head.competitions = 1;
    await emit({ type: 'tournament_updated', seq: 1, data: null });
    await pastWindow();
    expect(window.API.fetchTournament.mock.calls.length, 'a seq the old 30 would have called covered').toBe(before + 2);
  });

  it('a competition opened again starts from the list seq the page already held', async () => {
    const first = await mount(20);
    head.detail = 25;
    head.competitions = 25;
    await emit(tap(25, ['M']));
    await pastWindow();
    // Another competition is opened and this one again: the page is the same
    // and holds the list read, so the new list gate starts at 25.
    await act(async () => { first.rerender(adminElement('c2')); });
    await pastWindow();
    await act(async () => { first.rerender(adminElement('c1')); });
    await pastWindow();
    const beforeLists = lists();
    await emit(tap(24, ['M', 'K']));
    await pastWindow();
    expect(lists(), 'no tournament refresh for an event the page holds').toBe(beforeLists);
  });

  it('tournament_updated reads the tournament whatever the seqs say', async () => {
    await mount(20);
    // A competitions refresh named 40 covers the events up to it, but says
    // nothing of the tournament's own settings.
    head.competitions = 40;
    await emit(tap(21, ['M']));
    await pastWindow();
    const before = window.API.fetchTournament.mock.calls.length;
    await emit({ type: 'tournament_updated', seq: 22, data: null });
    await pastWindow();
    expect(window.API.fetchTournament.mock.calls.length, 'the settings are read').toBe(before + 1);
  });

  it('a settings change is shown even when the list was read at a higher seq than the settings answer', async () => {
    const onUpdate = vi.fn();
    await act(async () => {
      render(<AdminApp
        tournament={{ name: 'Cup', courts: ['A'], competitions: [{ ...config }] }}
        onUpdate={onUpdate} onLogout={vi.fn()} onViewerMode={vi.fn()} onPasswordChange={vi.fn()}
        tweaks={{}} password="pw" view={{ kind: 'competition', id: 'c1', section: 'overview' }} setView={vi.fn()}
        showToast={vi.fn()} authConfig={{}}
      />);
    });
    await pastWindow();
    // The list is read at 40 and shown. The settings answer names a lower seq
    // (a read begun earlier): the competitions read's seq must not cover it.
    head.competitions = 40;
    await emit(tap(21, ['M']));
    await pastWindow();
    window.API.fetchTournament.mockImplementationOnce(async () => withSeq({ name: 'Renamed Cup', courts: ['A', 'B'] }, 10));
    onUpdate.mockClear();
    await emit({ type: 'tournament_updated', seq: 22, data: null });
    await pastWindow();
    expect(onUpdate.mock.calls.some(([next]) => next && next.name === 'Renamed Cup'), 'the new settings are shown').toBe(true);
  });

  it('a tournament_updated event counts as seen: an answer built before it is read once more', async () => {
    await mount(20);
    const beforeLists = lists();
    // The competitions answer joins a build that began before event 50 and
    // names 40: the list is behind the event, whatever the settings read says.
    head.competitions = 40;
    await emit({ type: 'tournament_updated', seq: 50, data: null });
    await pastWindow();
    expect(lists(), 'the first read').toBe(beforeLists + 1);
    head.competitions = 50;
    await pastWindow();
    expect(lists(), 'and one more, since it named less than the event').toBe(beforeLists + 2);
    await pastWindow();
    expect(lists(), 'and no third: that answer holds the event').toBe(beforeLists + 2);
  });
});
