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

const probe = { props: null, editProps: null };
const sse = { emit: null };

const withSeq = (value, seq) => noteReadSeq(value, { headers: { get: (k) => (k === 'X-Event-Seq' ? String(seq) : null) } });

const head = { detail: 20, competitions: 20 };
const config = { id: 'c1', name: 'Autumn Cup', status: 'pools', format: 'league', kind: 'individual', courts: ['A'], players: [] };
const running = (ipponsA) => ({ id: 'Pool A-0', status: 'running', court: 'A', modifiedAt: 100 + ipponsA.length, ipponsA, ipponsB: [] });
const detail = () => withSeq({ config: { ...config }, pools: [], poolMatches: [running([])], standings: {}, bracket: null }, head.detail);

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import by admin.jsx.
  AdminCompetition: (props) => { probe.props = props; return <div data-testid="competition" />; },
  AdminEditTournament: (props) => { probe.editProps = props; return <div data-testid="edit-tournament" />; },
  confirmDialog: vi.fn(),
  API: {
    subscribeToEvents: vi.fn((onEvent, onStatus) => { sse.emit = onEvent; sse.status = onStatus; return () => {}; }),
    fetchCompetitions: vi.fn(async () => withSeq([{ ...config }], head.competitions)),
    fetchTournament: vi.fn(async () => withSeq({ name: 'Cup', courts: ['A'] }, head.competitions)),
    fetchCompetitionDetails: vi.fn(async () => detail()),
    recordScore: vi.fn(async () => ({ applied: true })),
    updateTournament: vi.fn(),
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

  it('the refresh after a score write tells the list gate what it read, so the write\'s own event refetches no list', async () => {
    await mount(20);
    head.competitions = 30;
    await act(async () => {
      await probe.props.onEditScore('c1', 'Pool A-0', { id: 'Pool A-0', status: 'running', court: 'A', ipponsA: ['M'], ipponsB: [] }, running([]));
    });
    expect(window.API.recordScore).toHaveBeenCalled();
    const beforeLists = lists();
    // The write's own match_updated arrives after the refresh that read it.
    await emit(tap(30, ['M']));
    await pastWindow();
    expect(lists(), 'the refresh already held the event').toBe(beforeLists);
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

  it('a late out-of-gate detail read does not overwrite a newer gate answer', async () => {
    await mount(20);
    let resolveLate;
    window.API.fetchCompetitionDetails.mockImplementationOnce(() => new Promise((r) => { resolveLate = r; }));
    const reloadPromise = probe.props.onRefreshCompetition();
    window.API.fetchCompetitionDetails.mockImplementationOnce(async () => withSeq({
      config: { ...config, name: 'Newer Cup' }, pools: [], poolMatches: [running(['M'])], standings: {}, bracket: null,
    }, 30));
    await emit(tap(30, ['M']));
    await pastWindow();
    expect(probe.props.competition.name).toBe('Newer Cup');

    let answer;
    await act(async () => {
      resolveLate(withSeq({
        config: { ...config, name: 'Stale Cup' }, pools: [], poolMatches: [running([])], standings: {}, bracket: null,
      }, 25));
      answer = await reloadPromise;
    });
    expect(answer).toEqual({ ok: true });
    expect(probe.props.competition.name).toBe('Newer Cup');
  });

  it('a late out-of-gate list read does not overwrite a newer gate answer', async () => {
    const onUpdate = vi.fn();
    head.detail = 20;
    head.competitions = 20;
    await act(async () => {
      render(<AdminApp
        tournament={{ name: 'Cup', courts: ['A'], competitions: [{ ...config }] }}
        onUpdate={onUpdate} onLogout={vi.fn()} onViewerMode={vi.fn()} onPasswordChange={vi.fn()}
        tweaks={{}} password="pw" view={{ kind: 'competition', id: 'c1', section: 'overview' }} setView={vi.fn()}
        showToast={vi.fn()} authConfig={{}}
      />);
    });
    await pastWindow();

    let resolveLate;
    window.API.fetchCompetitions.mockImplementationOnce(() => new Promise((r) => { resolveLate = r; }));
    const editPromise = probe.props.onEditScore(
      'c1', 'Pool A-0', { id: 'Pool A-0', status: 'running', court: 'A', ipponsA: ['M'], ipponsB: [] }, running([])
    );
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(resolveLate).toBeTypeOf('function');

    window.API.fetchCompetitions.mockImplementationOnce(async () => withSeq([{ ...config, name: 'Newer List Cup' }], 30));
    await emit(tap(30, ['M']));
    await pastWindow();
    expect(onUpdate.mock.calls.at(-1)[0].competitions[0].name).toBe('Newer List Cup');

    onUpdate.mockClear();
    await act(async () => {
      resolveLate(withSeq([{ ...config, name: 'Stale List Cup' }], 25));
      await editPromise;
    });
    expect(onUpdate).not.toHaveBeenCalled();

    window.API.fetchCompetitions.mockImplementationOnce(async () => withSeq([{ ...config, name: 'Newest List Cup' }], 31));
    await act(async () => {
      await probe.props.onEditScore(
        'c1', 'Pool A-0', { id: 'Pool A-0', status: 'running', court: 'A', ipponsA: ['M', 'K'], ipponsB: [] }, running(['M'])
      );
    });
    expect(onUpdate.mock.calls.at(-1)[0].competitions[0].name).toBe('Newest List Cup');
  });

  function SettingsHarness({ initialTournament, view, onUpdate }) {
    const [tournament, setTournament] = React.useState(initialTournament);
    return (
      <AdminApp
        tournament={tournament}
        onUpdate={(next) => { if (onUpdate) onUpdate(next); setTournament(next); }}
        onLogout={vi.fn()} onViewerMode={vi.fn()} onPasswordChange={vi.fn()}
        tweaks={{}} password="pw" view={view} setView={vi.fn()}
        showToast={vi.fn()} authConfig={{ mode: 'file' }}
      />
    );
  }

  async function openCompetitionWithPendingSave({ initialName = 'Cup', optimisticName = 'Optimistic Cup', initialSeq = 20 } = {}) {
    head.detail = initialSeq;
    head.competitions = initialSeq;
    let rejectSettled;
    const settled = new Promise((_resolve, reject) => { rejectSettled = reject; });
    window.API.updateTournament.mockImplementationOnce(async () => ({ pending: true, settled }));
    const onUpdate = vi.fn();
    const initialTournament = { name: initialName, courts: ['A'], competitions: [{ ...config }] };
    let view;
    await act(async () => {
      view = render(<SettingsHarness initialTournament={initialTournament} view={{ kind: 'editTournament' }} onUpdate={onUpdate} />);
    });
    expect(probe.editProps, 'edit tournament screen rendered').toBeTruthy();
    await act(async () => {
      await probe.editProps.onSave({ name: optimisticName, courts: ['A'] });
    });
    await act(async () => {
      view.rerender(<SettingsHarness initialTournament={initialTournament} view={{ kind: 'competition', id: 'c1', section: 'overview' }} onUpdate={onUpdate} />);
    });
    await pastWindow();
    expect(probe.props, 'competition screen rendered').toBeTruthy();
    expect(probe.props.tournament.name).toBe(optimisticName);
    return { onUpdate, rejectSettled };
  }

  it('a stale settings fetch after a refused save does not overwrite a newer settings gate answer', async () => {
    const { onUpdate, rejectSettled } = await openCompetitionWithPendingSave();
    head.detail = 30;
    head.competitions = 30;
    window.API.fetchTournament.mockImplementationOnce(async () => withSeq({ name: 'Newer Settings Cup', courts: ['A', 'B'] }, 30));
    await emit({ type: 'tournament_updated', seq: 30, data: null });
    await pastWindow();
    expect(probe.props.tournament.name).toBe('Newer Settings Cup');

    window.API.fetchTournament.mockImplementationOnce(async () => withSeq({ name: 'Stale Settings Cup', courts: ['A'] }, 25));
    onUpdate.mockClear();
    const beforeFetches = window.API.fetchTournament.mock.calls.length;
    await act(async () => {
      rejectSettled(new Error('invalid venue'));
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(window.API.fetchTournament.mock.calls.length, 'takeCurrentCopy fetched the stored settings').toBe(beforeFetches + 1);
    expect(onUpdate, 'stale fetched settings are dropped').not.toHaveBeenCalled();
    expect(probe.props.tournament.name, 'displayed settings stay at the newer ones').toBe('Newer Settings Cup');
    expect(probe.props.tournament.courts).toEqual(['A', 'B']);
  });

  it('a fresh copy restored after a refused save is shown and covers tournament_updated up to its seq', async () => {
    const { rejectSettled } = await openCompetitionWithPendingSave();
    head.detail = 30;
    head.competitions = 30;
    window.API.fetchTournament.mockImplementationOnce(async () => withSeq({ name: 'Settings At 30', courts: ['A'] }, 30));
    await emit({ type: 'tournament_updated', seq: 30, data: null });
    await pastWindow();
    expect(probe.props.tournament.name).toBe('Settings At 30');

    const conflict = Object.assign(new Error('Tournament changed'), {
      code: 'tournament_changed',
      tournament: withSeq({ name: 'Restored At 32', courts: ['A', 'B'] }, 32),
    });
    await act(async () => {
      rejectSettled(conflict);
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(probe.props.tournament.name).toBe('Restored At 32');
    expect(probe.props.tournament.courts).toEqual(['A', 'B']);

    const beforeFetches = window.API.fetchTournament.mock.calls.length;
    head.detail = 32;
    head.competitions = 32;
    await emit({ type: 'tournament_updated', seq: 31, data: null }, { type: 'tournament_updated', seq: 32, data: null });
    await pastWindow();
    expect(window.API.fetchTournament.mock.calls.length, 'no refetch for tournament_updated up to the restored seq').toBe(beforeFetches);
    expect(probe.props.tournament.name, 'restored settings stay shown').toBe('Restored At 32');

    head.detail = 33;
    head.competitions = 33;
    await emit({ type: 'tournament_updated', seq: 33, data: null });
    await pastWindow();
    expect(window.API.fetchTournament.mock.calls.length, 'an event above the restored seq still refetches').toBe(beforeFetches + 1);
  });

  it('restored settings survive a list gate apply in the same tick', async () => {
    const { onUpdate, rejectSettled } = await openCompetitionWithPendingSave({ initialName: 'Cup', optimisticName: 'Optimistic Cup', initialSeq: 20 });
    head.detail = 21;
    let resolveList;
    window.API.fetchCompetitions.mockImplementationOnce(() => new Promise((r) => { resolveList = r; }));
    await emit(tap(21, ['M']));
    await pastWindow();
    expect(resolveList, 'list gate fetch is in flight').toBeTypeOf('function');

    onUpdate.mockClear();
    const conflict = Object.assign(new Error('Tournament changed'), {
      code: 'tournament_changed',
      tournament: withSeq({ name: 'Restored Cup', courts: ['A', 'B'] }, 25),
    });
    await act(async () => {
      rejectSettled(conflict);
      resolveList(withSeq([{ ...config, name: 'Updated Comp' }], 25));
    });
    expect(onUpdate.mock.calls.length, 'both takeCurrentCopy and listGate applied').toBe(2);
    expect(onUpdate.mock.calls[0][0].name).toBe('Restored Cup');
    expect(onUpdate.mock.calls[1][0].name).toBe('Restored Cup');
    expect(onUpdate.mock.calls[1][0].competitions[0].name).toBe('Updated Comp');
    expect(probe.props.tournament.name, 'restored settings survive the same-tick list apply').toBe('Restored Cup');
    expect(probe.props.tournament.courts, 'restored courts survive the same-tick list apply').toEqual(['A', 'B']);
    expect(probe.props.tournament.competitions[0].name, 'the list update also stood').toBe('Updated Comp');
  });
});
