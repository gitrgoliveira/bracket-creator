// bc-sseq FR4: the court console reads its feed again once per burst, and
// not at all for an event the feed it applied already holds (the answer's
// X-Event-Seq, read_seq.jsx). Its refresh asks a gate (createRefetchGate,
// patch.jsx), 300 to 600 ms after the event; every read of the feed (the
// first one, the operator's Refresh) tells the gate the seq it applied.
import { act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { setupShiaijoConsole, makeMinimalTournament, renderPage } from '../helpers/shiaijo_console.js';
import { noteReadSeq } from '../../read_seq.jsx';

setupShiaijoConsole();

const withSeq = (value, seq) => noteReadSeq(value, { headers: { get: (k) => (k === 'X-Event-Seq' ? String(seq) : null) } });

describe('the court console reads its feed once per burst (bc-sseq FR4)', () => {
  it('skips events its read holds, and five quick taps cause one refetch', async () => {
    vi.useFakeTimers();
    const side = (id, name) => ({ id, name });
    const running = (ipponsA) => ({
      id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: '09:00',
      sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), ipponsA, ipponsB: [], modifiedAt: 100 + ipponsA.length,
    });
    const head = { seq: 20 };
    let emit = () => {};
    const prev = {
      fetch: window.API.fetchCourtMatches, sub: window.API.subscribeToEvents,
      tm: window.tournamentMatches, fbc: window.filterMatchesByCourt,
    };
    const fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve(
      withSeq([{ id: 'c1', name: 'Cup', status: 'pools', poolMatches: [running([])] }], head.seq),
    ));
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = (cb) => { emit = cb; return () => {}; };
    window.tournamentMatches = (t) => (t.competitions || [])
      .flatMap((c) => (c.poolMatches || []).map((m) => ({ ...m, compId: c.id, compName: c.name })));
    window.filterMatchesByCourt = (matches) => matches;
    const tap = (seq, ipponsA) => act(async () => {
      emit({ type: 'match_updated', seq, data: { competitionId: 'c1', matchId: 'm1', result: running(ipponsA) } });
    });
    const pastWindow = async () => {
      await act(async () => { vi.advanceTimersByTime(700); });
      await act(async () => { await Promise.resolve(); });
    };
    try {
      await act(async () => { renderPage(makeMinimalTournament(), 'A'); });
      await act(async () => { await Promise.resolve(); });
      const first = fetchCourtMatches.mock.calls.length;
      expect(first, 'the first read').toBeGreaterThan(0);

      // The stream's first events were sent before that read began.
      await tap(19, ['M']);
      await tap(20, ['M', 'K']);
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'nothing for events the first read holds').toBe(first);

      head.seq = 25;
      await tap(21, ['M']);
      await tap(22, ['M', 'K']);
      await tap(23, ['M', 'K', 'D']);
      await tap(24, ['M', 'K', 'D', 'T']);
      await tap(25, ['M', 'K', 'D', 'T', 'H']);
      await act(async () => { vi.advanceTimersByTime(299); });
      expect(fetchCourtMatches.mock.calls.length, 'nothing before the held push wait').toBe(first);
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'one refetch for the burst of five').toBe(first + 1);
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'and no second one').toBe(first + 1);

      // The operator's Refresh reads the feed outside the events; its answer
      // (named 30) covers the events the stream delivers after it.
      head.seq = 30;
      const refresh = document.querySelector('button.shiaijo-refresh');
      expect(refresh, 'the console offers a Refresh').toBeTruthy();
      await act(async () => { refresh.click(); });
      await act(async () => { await Promise.resolve(); });
      const afterRefresh = fetchCourtMatches.mock.calls.length;
      expect(afterRefresh, 'the Refresh read').toBe(first + 2);
      for (const seq of [26, 27, 28, 29, 30]) await tap(seq, ['M']);
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'events the Refresh already held').toBe(afterRefresh);
    } finally {
      window.API.fetchCourtMatches = prev.fetch;
      window.API.subscribeToEvents = prev.sub;
      window.tournamentMatches = prev.tm;
      window.filterMatchesByCourt = prev.fbc;
      vi.useRealTimers();
    }
  });

  it('a stream that reopens after an error restarts the gate, so a restarted server\'s seqs refetch', async () => {
    vi.useFakeTimers();
    const side = (id, name) => ({ id, name });
    const running = (ipponsA) => ({
      id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: '09:00',
      sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), ipponsA, ipponsB: [], modifiedAt: 100 + ipponsA.length,
    });
    const head = { seq: 20 };
    let emit = () => {};
    let status = () => {};
    const prev = {
      fetch: window.API.fetchCourtMatches, sub: window.API.subscribeToEvents,
      tm: window.tournamentMatches, fbc: window.filterMatchesByCourt,
    };
    const fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve(
      withSeq([{ id: 'c1', name: 'Cup', status: 'pools', poolMatches: [running([])] }], head.seq),
    ));
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = (cb, onStatus) => { emit = cb; status = onStatus; return () => {}; };
    window.tournamentMatches = (t) => (t.competitions || [])
      .flatMap((c) => (c.poolMatches || []).map((m) => ({ ...m, compId: c.id, compName: c.name })));
    window.filterMatchesByCourt = (matches) => matches;
    const pastWindow = async () => {
      await act(async () => { vi.advanceTimersByTime(700); });
      await act(async () => { await Promise.resolve(); });
    };
    try {
      await act(async () => { renderPage(makeMinimalTournament(), 'A'); });
      await act(async () => { await Promise.resolve(); });
      const first = fetchCourtMatches.mock.calls.length;

      // The server restarted: its head seq starts again at 0.
      head.seq = 0;
      await act(async () => { status('error'); });
      await act(async () => { status('open'); });
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'the feed is read again').toBe(first + 1);
      head.seq = 1;
      await act(async () => {
        emit({ type: 'match_updated', seq: 1, data: { competitionId: 'c1', matchId: 'm1', result: running(['M']) } });
      });
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'an event the old 20 would have called covered').toBe(first + 2);
    } finally {
      window.API.fetchCourtMatches = prev.fetch;
      window.API.subscribeToEvents = prev.sub;
      window.tournamentMatches = prev.tm;
      window.filterMatchesByCourt = prev.fbc;
      vi.useRealTimers();
    }
  });

  it('an answer to a feed read sent before a resync is not adopted (the gate forgot it)', async () => {
    vi.useFakeTimers();
    const side = (id, name) => ({ id, name });
    const match = {
      id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: '09:00',
      sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), ipponsA: [], ipponsB: [], modifiedAt: 100,
    };
    const feed = (seq) => withSeq([{ id: 'c1', name: 'Cup', status: 'pools', poolMatches: [match] }], seq);
    let emit = () => {};
    let manual = false;
    const pending = [];
    const prev = {
      fetch: window.API.fetchCourtMatches, sub: window.API.subscribeToEvents,
      tm: window.tournamentMatches, fbc: window.filterMatchesByCourt,
    };
    const fetchCourtMatches = vi.fn().mockImplementation(() => (
      manual ? new Promise((resolve) => pending.push(resolve)) : Promise.resolve(feed(20))));
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = (cb) => { emit = cb; return () => {}; };
    window.tournamentMatches = (t) => (t.competitions || [])
      .flatMap((c) => (c.poolMatches || []).map((m) => ({ ...m, compId: c.id, compName: c.name })));
    window.filterMatchesByCourt = (matches) => matches;
    const send = (event) => act(async () => { emit(event); });
    const pastWindow = async () => {
      await act(async () => { vi.advanceTimersByTime(700); });
      await act(async () => { await Promise.resolve(); });
    };
    const answer = async (index, value) => {
      await act(async () => { pending[index](value); });
      await act(async () => { await Promise.resolve(); });
    };
    try {
      await act(async () => { renderPage(makeMinimalTournament(), 'A'); });
      await act(async () => { await Promise.resolve(); });
      const first = fetchCourtMatches.mock.calls.length;

      // A read is out when the server says the stream restarted.
      manual = true;
      await send({ type: 'match_updated', seq: 21, data: { competitionId: 'c1', matchId: 'm1', result: match } });
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'the event asked for its read').toBe(first + 1);
      await send({ type: 'resync_required' });
      // The old stream's answer lands: it names a seq of the stream that ended.
      await answer(0, feed(90));
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'the read the resync asked for').toBe(first + 2);
      // An event of the new stream arrives while that read is out, above
      // the seq the read answers with: the old answer must not have hidden it.
      await send({ type: 'match_updated', seq: 50, data: { competitionId: 'c1', matchId: 'm1', result: match } });
      await answer(1, feed(40));
      await pastWindow();
      expect(fetchCourtMatches.mock.calls.length, 'the new stream\'s event is fetched').toBe(first + 3);
    } finally {
      window.API.fetchCourtMatches = prev.fetch;
      window.API.subscribeToEvents = prev.sub;
      window.tournamentMatches = prev.tm;
      window.filterMatchesByCourt = prev.fbc;
      vi.useRealTimers();
    }
  });

  it('an answer older than a read the page already shows is not shown', async () => {
    vi.useFakeTimers();
    const side = (id, name) => ({ id, name });
    const match = (name) => ({
      id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: '09:00',
      sideA: side('p1', name), sideB: side('p2', 'Tanaka'), ipponsA: [], ipponsB: [], modifiedAt: 100,
    });
    // The competition's name is what the console shows of a feed, so it tells the answers apart.
    const feed = (name, seq) => withSeq([{ id: 'c1', name, status: 'pools', poolMatches: [match('Yamada')] }], seq);
    let emit = () => {};
    let manual = false;
    const pending = [];
    const prev = {
      fetch: window.API.fetchCourtMatches, sub: window.API.subscribeToEvents,
      tm: window.tournamentMatches, fbc: window.filterMatchesByCourt,
    };
    const fetchCourtMatches = vi.fn().mockImplementation(() => (
      manual ? new Promise((resolve) => pending.push(resolve)) : Promise.resolve(feed('Alpha Cup', 20))));
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = (cb) => { emit = cb; return () => {}; };
    window.tournamentMatches = (t) => (t.competitions || [])
      .flatMap((c) => (c.poolMatches || []).map((m) => ({ ...m, compId: c.id, compName: c.name })));
    window.filterMatchesByCourt = (matches) => matches;
    const pastWindow = async () => {
      await act(async () => { vi.advanceTimersByTime(700); });
      await act(async () => { await Promise.resolve(); });
    };
    const answer = async (index, value) => {
      await act(async () => { pending[index](value); });
      await act(async () => { await Promise.resolve(); });
    };
    try {
      await act(async () => { renderPage(makeMinimalTournament(), 'A'); });
      await act(async () => { await Promise.resolve(); });
      expect(document.body.textContent).toContain('Alpha Cup');

      // An event sends a read that stays out; then the operator's Refresh
      // reads the feed, and its newer answer is shown first.
      manual = true;
      await act(async () => {
        emit({ type: 'match_updated', seq: 21, data: { competitionId: 'c1', matchId: 'm1', result: match('Yamada') } });
      });
      await pastWindow();
      expect(pending, 'the event sent its read').toHaveLength(1);
      const refresh = document.querySelector('button.shiaijo-refresh');
      await act(async () => { refresh.click(); });
      expect(pending, 'the Refresh sent its own').toHaveLength(2);
      await answer(1, feed('Kimura Cup', 30));
      expect(document.body.textContent).toContain('Kimura Cup');

      // The older read lands late: it must not put the older feed back.
      await answer(0, feed('Suzuki Cup', 25));
      await pastWindow();
      expect(document.body.textContent).toContain('Kimura Cup');
      expect(document.body.textContent).not.toContain('Suzuki Cup');
    } finally {
      window.API.fetchCourtMatches = prev.fetch;
      window.API.subscribeToEvents = prev.sub;
      window.tournamentMatches = prev.tm;
      window.filterMatchesByCourt = prev.fbc;
      vi.useRealTimers();
    }
  });
});
