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
});
