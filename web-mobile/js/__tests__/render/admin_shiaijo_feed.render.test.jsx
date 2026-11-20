import { act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { setupShiaijoConsole, probe, makeMinimalTournament, renderPage } from '../helpers/shiaijo_console.js';

setupShiaijoConsole();

// The console used to refetch its whole court feed for every event, and a
// running match broadcasts every saved point. A running match's score update
// changes only its own row, so the pushed result is shown at once, a push
// older than the row it would replace is ignored, and the refetch that still
// follows (for what a push cannot carry) happens once per burst.
describe('the court console shows a pushed running score at once', () => {
  it('applies the push to its row, ignores a stale one, and refetches once per burst', async () => {
    vi.useFakeTimers();
    const side = (id, name) => ({ id, name });
    const running = (o = {}) => ({
      id: 'm1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: '09:00',
      sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), ipponsA: [], ipponsB: [], modifiedAt: 100, ...o,
    });
    const comp = { id: 'c1', name: 'Cup', status: 'pools', poolMatches: [running()] };
    let emit = () => {};
    const prev = {
      fetch: window.API.fetchCourtMatches, sub: window.API.subscribeToEvents,
      tm: window.tournamentMatches, fbc: window.filterMatchesByCourt,
    };
    const fetchCourtMatches = vi.fn().mockResolvedValue([comp]);
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = (cb) => { emit = cb; return () => {}; };
    window.tournamentMatches = (t) => (t.competitions || [])
      .flatMap((c) => (c.poolMatches || []).map((m) => ({ ...m, compId: c.id, compName: c.name })));
    window.filterMatchesByCourt = (matches) => matches;
    try {
      await act(async () => { renderPage(makeMinimalTournament(), 'A'); });
      await act(async () => { await Promise.resolve(); });
      expect(probe.props.match?.id).toBe('m1');
      const fetchesBefore = fetchCourtMatches.mock.calls.length;
      const push = (o) => act(async () => {
        emit({ type: 'match_updated', data: { competitionId: 'c1', matchId: 'm1', result: running(o) } });
      });

      await push({ ipponsA: ['M'], modifiedAt: 200 });
      expect(probe.props.match.ipponsA, 'shown at once, before any refetch').toEqual(['M']);
      await push({ ipponsA: ['M', 'K'], modifiedAt: 300 });
      await push({ ipponsA: [], modifiedAt: 250 });
      expect(probe.props.match.ipponsA, 'a push older than the row is not applied').toEqual(['M', 'K']);
      expect(fetchCourtMatches.mock.calls.length, 'nothing refetched yet').toBe(fetchesBefore);

      await act(async () => { vi.advanceTimersByTime(700); });
      await act(async () => { await Promise.resolve(); });
      expect(fetchCourtMatches.mock.calls.length, 'one refetch for the burst of three').toBe(fetchesBefore + 1);
      // That refetch answers with the row as it stood before the pushes (it
      // read the data before the writes committed): the newer row stays.
      expect(probe.props.match.ipponsA, 'a refetch older than the row does not put the old score back').toEqual(['M', 'K']);

      // A refetch as new as the row, or newer, replaces it as before.
      fetchCourtMatches.mockResolvedValue([{ ...comp, poolMatches: [running({ ipponsA: ['M', 'K'], ipponsB: ['D'], modifiedAt: 400 })] }]);
      await act(async () => { emit({ type: 'schedule_updated', data: {} }); });
      await act(async () => { vi.advanceTimersByTime(700); });
      await act(async () => { await Promise.resolve(); });
      expect(probe.props.match.ipponsB, 'a newer refetch is taken').toEqual(['D']);
    } finally {
      window.API.fetchCourtMatches = prev.fetch;
      window.API.subscribeToEvents = prev.sub;
      window.tournamentMatches = prev.tm;
      window.filterMatchesByCourt = prev.fbc;
      vi.useRealTimers();
    }
  });
});

// The console's pool panel refetches the competition on poolMatchesSignature,
// the one owner of "did the pool results change". A correction to a team pool
// encounter that keeps its winner and its status moves only the sub-bouts (IV
// and PW), which the old id/status/ippons key never saw.
describe('the console pool panel follows a correction that keeps the winner', () => {
  it('fetches the competition detail again when a sub-bout of a finished encounter changes', async () => {
    vi.useFakeTimers();
    const side = (id, name) => ({ id, name });
    const bout = (winner, ipponsA) => ({ winner, ipponsA, ipponsB: [], decision: '' });
    const encounter = (subResults) => ({
      id: 'Pool A-0', status: 'completed', phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: '09:00',
      sideA: side('t1', 'Alpha'), sideB: side('t2', 'Bravo'), winner: 'Alpha', modifiedAt: 100, subResults,
    });
    const comp = (m) => ({ id: 'c1', name: 'Cup', status: 'pools', poolMatches: [m] });
    let emit = () => {};
    const prev = {
      fetch: window.API.fetchCourtMatches, sub: window.API.subscribeToEvents,
      tm: window.tournamentMatches, fbc: window.filterMatchesByCourt,
    };
    const fetchCourtMatches = vi.fn().mockResolvedValue([comp(encounter([bout('Alpha', ['M']), bout('Alpha', ['K'])]))]);
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = (cb) => { emit = cb; return () => {}; };
    window.tournamentMatches = (t) => (t.competitions || [])
      .flatMap((c) => (c.poolMatches || []).map((m) => ({ ...m, compId: c.id, compName: c.name })));
    window.filterMatchesByCourt = (matches) => matches;
    const detail = window.API.fetchCompetitionDetails;
    try {
      await act(async () => { renderPage(makeMinimalTournament(), 'A'); });
      await act(async () => { await Promise.resolve(); });
      const before = detail.mock.calls.length;
      expect(before, 'the panel asked for the competition once it opened').toBeGreaterThan(0);

      // Same winner, same status: one bout corrected from a win to a draw.
      fetchCourtMatches.mockResolvedValue([comp({
        ...encounter([bout('Alpha', ['M', 'K']), { winner: '', ipponsA: [], ipponsB: [], decision: 'hikiwake' }]),
        modifiedAt: 200,
      })]);
      await act(async () => { emit({ type: 'schedule_updated', data: {} }); });
      await act(async () => { vi.advanceTimersByTime(700); });
      await act(async () => { await Promise.resolve(); });
      expect(detail.mock.calls.length, 'the corrected bout is read again').toBe(before + 1);
    } finally {
      window.API.fetchCourtMatches = prev.fetch;
      window.API.subscribeToEvents = prev.sub;
      window.tournamentMatches = prev.tm;
      window.filterMatchesByCourt = prev.fbc;
      vi.useRealTimers();
    }
  });
});
