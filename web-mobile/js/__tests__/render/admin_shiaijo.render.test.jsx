import React from 'react';
import { render, act, fireEvent, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { setupShiaijoConsole, probe, makeMinimalTournament, renderPage, mountCourt, courtMatch } from '../helpers/shiaijo_console.js';

setupShiaijoConsole();

describe('AdminShiaijoPage render-smoke', () => {
  it('renders without throwing for an unknown court (empty state)', () => {
    expect(() => renderPage(makeMinimalTournament(), 'Z')).not.toThrow();
  });

  it('renders without throwing for a known court with no matches (empty queue)', () => {
    window.tournamentMatches = () => [];
    window.filterMatchesByCourt = () => [];
    expect(() => renderPage(makeMinimalTournament())).not.toThrow();
  });

  it('renders without throwing with an individual match in "Up Next"', () => {
    const upNextMatch = {
      id: 'm1', compId: 'c1', status: 'scheduled',
      phase: 'pool', poolName: 'Pool 1', court: 'A',
      sideA: { id: 'p1', name: 'Yamada' },
      sideB: { id: 'p2', name: 'Tanaka' },
    };
    window.tournamentMatches = () => [upNextMatch];
    window.filterMatchesByCourt = (matches) => matches;
    expect(() => renderPage(makeMinimalTournament())).not.toThrow();
  });

  // mp-jnvl: once the court has nothing running, the context strip anchors to a
  // finished bout. Two things must hold: it names the bout whose RESULT was
  // written last (not the one scheduled last: a court interleaving pools runs
  // out of schedule order routinely), and the heading admits the bout is over
  // instead of reading like the bout now being fought.
  const completedPoolBout = (over) => ({
    compId: 'c1', compName: 'Cup', status: 'completed', phase: 'pool', court: 'A',
    sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
    ...over,
  });

  it('anchors the context strip to the bout written last, not the one scheduled last', () => {
    // Played first, but holds the later slot: the tail-of-list pick this replaces.
    const playedFirst = completedPoolBout({ id: 'm-p2', poolName: 'Pool 2', scheduledAt: '09:05', modifiedAt: 1000 });
    const playedLast = completedPoolBout({ id: 'm-p1', poolName: 'Pool 1', scheduledAt: '09:00', modifiedAt: 2000 });
    window.tournamentMatches = () => [playedLast, playedFirst];
    window.filterMatchesByCourt = (matches) => matches;
    const { container } = renderPage(makeMinimalTournament());
    const heading = container.querySelector('.shiaijo-context__toggle').textContent;
    expect(heading).toContain('Pool 1');
    expect(heading).not.toContain('Pool 2');
    expect(heading).toContain('Just played');
  });

  // The panel can land on a finished bout two ways, and they are different
  // facts: it FELL BACK there (nothing running), or the operator opened that
  // result to correct it. "Just played" would be wrong for the second.
  it('says "Correcting", not "Just played", when the operator opened a finished bout to fix it', () => {
    const finished = completedPoolBout({ id: 'm-p1', poolName: 'Pool 1', scheduledAt: '09:00', modifiedAt: 2000 });
    window.tournamentMatches = () => [finished];
    window.filterMatchesByCourt = (matches) => matches;
    const { container } = renderPage(makeMinimalTournament());
    expect(container.querySelector('.shiaijo-context__toggle').textContent).toContain('Just played');

    const correct = [...container.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Correct');
    expect(correct).toBeTruthy();
    act(() => { correct.click(); });

    const heading = container.querySelector('.shiaijo-context__toggle').textContent;
    expect(heading).toContain('Correcting');
    expect(heading).not.toContain('Just played');
    expect(heading).toContain('Pool 1');
  });

  // Before the court's first bout the strip anchors to a bout NOT YET FOUGHT.
  // The bracket highlight there means "play this next", so the heading says so
  // rather than the meaningless "Context" it used to lead with (operator ruling
  // 2026-09-19).
  it('says "Up next" before the court has played anything', () => {
    const upcoming = {
      id: 'm-k1', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'bracket',
      court: 'A', scheduledAt: '09:00', round: 'Quarterfinals',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
    };
    window.tournamentMatches = () => [upcoming];
    window.filterMatchesByCourt = (matches) => matches;
    const { container } = renderPage(makeMinimalTournament());
    const heading = container.querySelector('.shiaijo-context__toggle').textContent;
    expect(heading).toContain('Up next');
    expect(heading).not.toContain('Context');
    expect(heading).toContain('Quarterfinals');
  });

  // With nothing to qualify, the lead names the panel's CONTENT: a knockout
  // panel renders a bracket fragment, so it says Bracket, the counterpart of
  // Standings for a pool. "Context" named nothing and is gone.
  it('names the panel content, never "Context", while a knockout bout is running', () => {
    const running = {
      id: 'm-k2', compId: 'c1', compName: 'Cup', status: 'running', phase: 'bracket',
      court: 'A', scheduledAt: '09:05', round: 'Semifinals',
      sideA: { id: 'p3', name: 'Sato' }, sideB: { id: 'p4', name: 'Kato' },
    };
    window.tournamentMatches = () => [running];
    window.filterMatchesByCourt = (matches) => matches;
    const { container } = renderPage(makeMinimalTournament());
    const heading = container.querySelector('.shiaijo-context__toggle').textContent;
    expect(heading).toContain('Bracket');
    expect(heading).not.toContain('Context');
    expect(heading).not.toContain('Up next');
  });

  it('keeps the live heading while a bout is running, even with a finished bout behind it', () => {
    const finished = completedPoolBout({ id: 'm-p1', poolName: 'Pool 1', scheduledAt: '09:00', modifiedAt: 2000 });
    const running = {
      id: 'm-p3', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool',
      poolName: 'Pool 3', court: 'A', scheduledAt: '09:10',
      sideA: { id: 'p5', name: 'Sato' }, sideB: { id: 'p6', name: 'Kato' },
    };
    window.tournamentMatches = () => [running, finished];
    window.filterMatchesByCourt = (matches) => matches;
    const { container } = renderPage(makeMinimalTournament());
    const heading = container.querySelector('.shiaijo-context__toggle').textContent;
    expect(heading).toContain('Pool 3');
    expect(heading).toContain('Standings');
    expect(heading).not.toContain('Just played');
  });

  it('renders without throwing with a running team match', () => {
    const runningTeamMatch = {
      id: 'm2', compId: 'c1', status: 'running',
      phase: 'pool', poolName: 'Pool 1', court: 'A',
      compKind: 'team', teamSize: 5,
      sideA: { id: 'team-A', name: 'Team A' },
      sideB: { id: 'team-B', name: 'Team B' },
    };
    window.tournamentMatches = () => [runningTeamMatch];
    window.filterMatchesByCourt = (matches) => matches;
    expect(() => renderPage(makeMinimalTournament())).not.toThrow();
  });

  // mp-y3nk: a scheduled knockout final whose sides are still "Winner of rX-mY"
  // feeders must appear in the queue as a non-actionable "Later" row (so a court
  // whose only remaining bout is a downstream final is not shown as empty/done),
  // and must NEVER become the startable Up Next card.
  it('shows a pending placeholder final as a non-actionable "Later" row, never as Up Next', () => {
    const completedFeeder = {
      id: 'r2-m0', compId: 'c1', compName: 'Cup', status: 'completed',
      phase: 'bracket', matchNumber: 1, court: 'A',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
      winner: { id: 'p1', name: 'Yamada' },
    };
    const pendingFinal = {
      id: 'r3-m0', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 3, court: 'A',
      sideA: { id: '', name: 'Winner of r2-m0' },
      sideB: { id: '', name: 'Winner of r2-m1' },
    };
    window.tournamentMatches = () => [completedFeeder, pendingFinal];
    window.filterMatchesByCourt = (matches) => matches;

    const { getByText, queryByText } = renderPage(makeMinimalTournament());

    // The placeholder text renders (the "Later" row is the visible signal).
    expect(getByText('Winner of r2-m0')).toBeTruthy();
    expect(getByText('Winner of r2-m1')).toBeTruthy();
    // It is flagged as waiting, not actionable.
    expect(getByText('Waiting')).toBeTruthy();
    // Crucially: it never surfaces a Start button (no real Up Next exists here).
    expect(queryByText('Start match')).toBeNull();
  });

  // bc-bzlb (operator decision 2026-10-11): the 3rd-place match has no match
  // number, so the console names it with the owner's words, the ones the Scores
  // list and the refusal dialogs use, rather than with nothing.
  it('names the 3rd-place match on the Up next card', () => {
    const bronze = {
      id: 'm-bronze', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 0, court: 'A', scheduledAt: '10:55',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
    };
    window.tournamentMatches = () => [bronze];
    window.filterMatchesByCourt = (matches) => matches;
    const { container } = renderPage(makeMinimalTournament());
    expect(container.querySelector('.shiaijo-upnext__time').textContent).toBe('10:55, Cup, the 3rd-place match');
  });

  // bc-bzlb: before the semi-finals, the 3rd-place match's sides are EMPTY (no
  // "Winner of" slot), so it used to be missing from the queue altogether. It is
  // listed under Later, its sides named after the semi-finals that fill them.
  it('lists the 3rd-place match under Later with "Loser of M<n>" before the semi-finals', async () => {
    // The real slot-text owner, imported without leaving bracket.jsx's window
    // globals behind (other tests here rely on bracketSlotLabeller being absent).
    const before = new Map(Object.keys(window).map((k) => [k, window[k]]));
    const { bronzeSlotLabels } = await import('../../bracket.jsx');
    for (const k of Object.keys(window)) {
      if (!before.has(k)) delete window[k];
      else if (window[k] !== before.get(k)) window[k] = before.get(k);
    }
    const rounds = [
      [
        { id: 'm-r1-0', status: 'scheduled', matchNumber: 1, sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' } },
        { id: 'm-r1-1', status: 'scheduled', matchNumber: 2, sideA: { id: 'c', name: 'Carol' }, sideB: { id: 'd', name: 'Dan' } },
      ],
      [
        { id: 'm-r2-0', status: 'scheduled', matchNumber: 3, sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' } },
      ],
    ];
    const bronze = {
      id: 'm-bronze', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 0, court: 'A',
      sideA: { id: '', name: '' }, sideB: { id: '', name: '' },
    };
    window.bronzeSlotLabels = bronzeSlotLabels;
    window.tournamentMatches = () => [bronze];
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      await act(async () => {
        utils = renderPage(makeMinimalTournament({ competitions: [{ id: 'c1', name: 'Cup', bracket: { rounds } }] }));
      });
      const later = utils.container.querySelector('.shiaijo-pending');
      expect(later).toBeTruthy();
      expect(within(later).getByText('Loser of M1')).toBeTruthy();
      expect(within(later).getByText('Loser of M2')).toBeTruthy();
      expect(later.textContent).toContain('the 3rd-place match');
      expect(utils.queryByRole('button', { name: /run now/i })).toBeNull();
      expect(utils.queryByText('Start match')).toBeNull();
    } finally {
      delete window.bronzeSlotLabels;
    }
  });

  // The bronze labels are read through the bracket module's window global, as
  // its sibling slot labeller is: guarded the same way, so a console without
  // the bracket module still lists the 3rd-place match (its sides unnamed).
  it('lists the 3rd-place match under Later without the bracket module loaded', async () => {
    const rounds = [
      [
        { id: 'm-r1-0', status: 'scheduled', matchNumber: 1, sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' } },
        { id: 'm-r1-1', status: 'scheduled', matchNumber: 2, sideA: { id: 'c', name: 'Carol' }, sideB: { id: 'd', name: 'Dan' } },
      ],
      [{ id: 'm-r2-0', status: 'scheduled', matchNumber: 3, sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' } }],
    ];
    const bronze = {
      id: 'm-bronze', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 0, court: 'A',
      sideA: { id: '', name: '' }, sideB: { id: '', name: '' },
    };
    expect(window.bronzeSlotLabels).toBeUndefined();
    window.tournamentMatches = () => [bronze];
    window.filterMatchesByCourt = (matches) => matches;
    let utils;
    await act(async () => {
      utils = renderPage(makeMinimalTournament({ competitions: [{ id: 'c1', name: 'Cup', bracket: { rounds } }] }));
    });
    const later = utils.container.querySelector('.shiaijo-pending');
    expect(later).toBeTruthy();
    expect(later.textContent).toContain('the 3rd-place match');
  });

  // A court back online refetches its feed when the stream reopens, with no
  // manual Refresh. app.jsx reopens the stream on 'online' by closing it, which
  // fires no error, so the reopen reads as a bare 'open' (the stale court seen
  // in the browser: Start still offered for a match another device started).
  // The refetch is jittered (200-600 ms), hence the fake timers.
  it('refetches the court feed when the stream reopens after the device comes back online', async () => {
    vi.useFakeTimers();
    const fetchCourtMatches = vi.fn().mockResolvedValue([]);
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    let status = null;
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = (_cb, onStatus) => { status = onStatus; return () => {}; };
    window.tournamentMatches = () => [];
    window.filterMatchesByCourt = (matches) => matches;
    try {
      await act(async () => { renderPage(makeMinimalTournament()); });
      await act(async () => { status('open'); await vi.advanceTimersByTimeAsync(1000); });
      const afterConnect = fetchCourtMatches.mock.calls.length;
      // A bare reopen with nothing before it is not a reconnect.
      await act(async () => { status('open'); await vi.advanceTimersByTimeAsync(1000); });
      expect(fetchCourtMatches.mock.calls.length).toBe(afterConnect);
      await act(async () => {
        window.dispatchEvent(new Event('online'));
        status('open');
        await vi.advanceTimersByTimeAsync(1000);
      });
      expect(fetchCourtMatches.mock.calls.length).toBe(afterConnect + 1);
    } finally {
      vi.useRealTimers();
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });

  // Back from the background (an iPad locked or switched app), the court's
  // feed is refetched: admin.jsx's own resume refresh reloads the
  // competition, not this court's feed.
  it('refetches the court feed when the page becomes visible again', async () => {
    vi.useFakeTimers();
    const fetchCourtMatches = vi.fn().mockResolvedValue([]);
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = () => () => {};
    window.tournamentMatches = () => [];
    window.filterMatchesByCourt = (matches) => matches;
    const hidden = Object.getOwnPropertyDescriptor(Document.prototype, 'hidden');
    let isHidden = false;
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => isHidden });
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); await vi.advanceTimersByTimeAsync(1000); });
      const afterMount = fetchCourtMatches.mock.calls.length;
      isHidden = true;
      await act(async () => { document.dispatchEvent(new Event('visibilitychange')); await vi.advanceTimersByTimeAsync(1000); });
      expect(fetchCourtMatches.mock.calls.length).toBe(afterMount);
      isHidden = false;
      await act(async () => { document.dispatchEvent(new Event('visibilitychange')); await vi.advanceTimersByTimeAsync(1000); });
      expect(fetchCourtMatches.mock.calls.length).toBe(afterMount + 1);
      // Unmounted, it no longer listens.
      utils.unmount();
      await act(async () => { document.dispatchEvent(new Event('visibilitychange')); await vi.advanceTimersByTimeAsync(1000); });
      expect(fetchCourtMatches.mock.calls.length).toBe(afterMount + 1);
    } finally {
      vi.useRealTimers();
      delete document.hidden;
      if (hidden) Object.defineProperty(Document.prototype, 'hidden', hidden);
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });

  // mp-y3nk Phase 2: the manual "Refresh" button re-pulls the court feed on
  // demand, the operator's recovery when the queue looks stale after a dropped
  // connection. It must call fetchCourtMatches again beyond the mount fetch.
  it('Refresh button re-pulls the court feed on click', async () => {
    const fetchCourtMatches = vi.fn().mockResolvedValue([]);
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = () => () => {};
    window.tournamentMatches = () => [];
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      // act() wraps the async setCourtComps/setRefreshing updates the mount
      // fetch and the click trigger.
      await act(async () => { utils = renderPage(makeMinimalTournament()); });
      const callsAfterMount = fetchCourtMatches.mock.calls.length;
      expect(callsAfterMount).toBeGreaterThanOrEqual(1);
      await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); });
      expect(fetchCourtMatches.mock.calls.length).toBeGreaterThan(callsAfterMount);
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });

  it('the queue header bar and the rail fold the queue column, and the choice persists per device', async () => {
    const upNextMatch = {
      id: 'm1', compId: 'c1', status: 'scheduled',
      phase: 'pool', poolName: 'Pool 1', court: 'A',
      sideA: { id: 'p1', name: 'Yamada', number: 'I1' },
      sideB: { id: 'p2', name: 'Tanaka', number: 'I2' },
    };
    window.tournamentMatches = () => [upNextMatch];
    window.filterMatchesByCourt = (matches) => matches;
    localStorage.removeItem('bc_shiaijo_queue_open');
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    const grid = () => utils.container.querySelector('.shiaijo');
    expect(grid().classList.contains('shiaijo--queue-collapsed')).toBe(false);
    // Aka's number sits AFTER the name (outer side), Shiro's before it.
    const sides = [...utils.container.querySelectorAll('.shiaijo-sides__side .name')];
    const names = sides.map(n => n.textContent);
    expect(names).toEqual(['I2Tanaka', 'YamadaI1']);
    // NumberedName wraps the name span and the one chip it renders in a
    // .numbered-name span (layout-transparent by default): for Aka (the
    // second side) .numbered-name__text comes first and the
    // num-prefix--after chip last.
    const chip = sides[1].querySelector('.numbered-name');
    expect(chip.children[0].classList.contains('numbered-name__text')).toBe(true);
    expect(chip.lastElementChild.classList.contains('num-prefix--after')).toBe(true);
    expect(utils.queryByTestId('shiaijo-queue-show')).toBeNull();
    await act(async () => { utils.getByTestId('shiaijo-queue-hide').click(); });
    expect(grid().classList.contains('shiaijo--queue-collapsed')).toBe(true);
    // Folded, a rail stands in the column's place and is the way back.
    expect(utils.getByTestId('shiaijo-queue-show').textContent).toContain('Show queue');
    expect(localStorage.getItem('bc_shiaijo_queue_open')).toBe('0');
    // Collapsing hides the header "Hide" button that had focus (it stays
    // mounted; its .shiaijo__queue parent goes display: none) while the
    // rail mounts in its place, so focus must land on that new rail button.
    expect(document.activeElement).toBe(utils.getByTestId('shiaijo-queue-show'));
    await act(async () => { utils.getByTestId('shiaijo-queue-show').click(); });
    expect(grid().classList.contains('shiaijo--queue-collapsed')).toBe(false);
    expect(utils.queryByTestId('shiaijo-queue-show')).toBeNull();
    expect(localStorage.getItem('bc_shiaijo_queue_open')).toBe('1');
    // Expanding unmounts the rail that had focus, so it must land back on the
    // header "Hide" button, which becomes visible again (it was hidden, not
    // unmounted, while collapsed).
    expect(document.activeElement).toBe(utils.getByTestId('shiaijo-queue-hide'));
  });

  // bc-rnrm: a knockout match whose feeders are unresolved is listed as
  // waiting on them, and the console offers no way to set their winners by
  // hand ("Run now" is gone).
  it('a pending knockout match with unresolved feeders does not offer "Run now"', async () => {
    const rounds = [
      [
        { id: 'm-r2-0', status: 'scheduled', sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' } },
        { id: 'm-r2-1', status: 'scheduled', sideA: { id: 'c', name: 'Carol' }, sideB: { id: 'd', name: 'Dan' } },
      ],
      [
        { id: 'm-r1-0', status: 'scheduled', sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' } },
      ],
    ];
    const comp = { id: 'c1', name: 'Cup', bracket: { rounds } };
    const pendingFinal = {
      id: 'm-r1-0', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 3, court: 'A',
      sideA: { id: '', name: 'Winner of r2-m0' }, sideB: { id: '', name: 'Winner of r2-m1' },
    };
    window.tournamentMatches = () => [pendingFinal];
    window.filterMatchesByCourt = (matches) => matches;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament({ competitions: [comp] })); });
    expect(utils.getByText(/once the earlier matches that feed it are scored/i)).toBeTruthy();
    expect(utils.queryByRole('button', { name: /run now/i })).toBeNull();
    expect(utils.queryByText(/run now/i)).toBeNull();
  });

  // mp-gmcg regression: correcting a COMPLETED match then Reopening it (kachinuki:
  // completed -> running, keep the bout log) must never strand the operator. The
  // "Back to court" exit is deliberately withheld while running (it could strand a
  // result-less bout behind another running match), so "Send back to queue" must
  // take over as the exit. A prior `!correctingMatch` guard hid BOTH once reopened,
  // leaving the shiaijo panel with no in-panel exit - the exact dead-end this
  // feature exists to remove.
  it('a reopened correction keeps an in-panel exit (Send back to queue), never a dead-end', async () => {
    const completed = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'completed',
      phase: 'bracket', matchNumber: 1, court: 'A', compKind: 'team', teamSize: 5,
      sideA: { id: 'a', name: 'Team A' }, sideB: { id: 'b', name: 'Team B' },
      winner: { id: 'a', name: 'Team A' },
    };
    // Mutable court feed: the reopen happens server-side inside the (stubbed)
    // editor, so we model its effect by returning the SAME match as running on
    // the next refetch. tournamentMatches ignores its arg and reads this closure.
    let current = [completed];
    window.tournamentMatches = () => current;
    window.filterMatchesByCourt = (matches) => matches;
    // fetchCourtMatches returns a FRESH array each call so courtComps identity
    // changes and courtMatchesRaw re-derives from the mutated feed.
    const fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    const prevRevert = window.API.revertMatchToQueue;
    window.API.fetchCourtMatches = fetchCourtMatches;
    window.API.subscribeToEvents = () => () => {};
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue(true);
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); });
      // Correct the completed match -> correcting (completed): the exit is "Back to court".
      await act(async () => { utils.getByRole('button', { name: /^correct$/i }).click(); });
      expect(utils.getByRole('button', { name: /back to court/i })).toBeTruthy();
      expect(utils.queryByRole('button', { name: /send back to queue/i })).toBeNull();
      // Simulate the Reopen: the match comes back RUNNING on the next refetch.
      current = [{ ...completed, status: 'running', winner: undefined }];
      await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); });
      // Now reopened (running). Exactly one exit, and it is Send back to queue.
      expect(utils.getByRole('button', { name: /send back to queue/i })).toBeTruthy();
      expect(utils.queryByRole('button', { name: /back to court/i })).toBeNull();
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
      window.API.revertMatchToQueue = prevRevert;
    }
  });

  // The console's own confirms (Send back to queue, the court move) work like the
  // app's other dialogs from a keyboard: focus goes into the confirm as it opens,
  // Escape cancels one that is not busy, and focus goes back to the control that
  // opened it. Found in the browser: Escape did nothing and focus fell to the page.
  describe('the console confirms from a keyboard', () => {
    const running = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'running',
      phase: 'bracket', matchNumber: 1, court: 'A', compKind: 'team', teamSize: 5,
      sideA: { id: 'a', name: 'Team A' }, sideB: { id: 'b', name: 'Team B' },
    };
    const settle = () => act(async () => { await new Promise((r) => setTimeout(r, 5)); });
    let saved;
    beforeEach(() => {
      saved = { fetch: window.API.fetchCourtMatches, sub: window.API.subscribeToEvents, revert: window.API.revertMatchToQueue };
      window.tournamentMatches = () => [running];
      window.filterMatchesByCourt = (matches) => matches;
      window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
      window.API.subscribeToEvents = () => () => {};
    });
    afterEach(() => {
      window.API.fetchCourtMatches = saved.fetch;
      window.API.subscribeToEvents = saved.sub;
      window.API.revertMatchToQueue = saved.revert;
    });

    it('Send back to queue: focus goes in, Escape cancels and gives it back, and a confirm whose request is out stays', async () => {
      let answer;
      window.API.revertMatchToQueue = vi.fn().mockImplementation(() => new Promise((r) => { answer = r; }));
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); });
      const opener = utils.getByRole('button', { name: /^send back to queue$/i });
      opener.focus();
      await act(async () => { opener.click(); });
      await settle();
      const dialog = utils.getByRole('dialog', { name: /send back to queue\?/i });
      expect(dialog.contains(document.activeElement), 'focus is in the confirm').toBe(true);

      await act(async () => { fireEvent.keyDown(document.activeElement, { key: 'Escape' }); });
      await settle();
      expect(utils.queryByRole('dialog'), 'Escape cancels it').toBeNull();
      expect(document.activeElement, 'focus is back on the control that opened it').toBe(utils.getByRole('button', { name: /^send back to queue$/i }));
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();

      await act(async () => { utils.getByRole('button', { name: /^send back to queue$/i }).click(); });
      await settle();
      const again = utils.getByRole('dialog', { name: /send back to queue\?/i });
      await act(async () => { within(again).getByRole('button', { name: /^send back to queue$/i }).click(); });
      await act(async () => { fireEvent.keyDown(document.body, { key: 'Escape' }); });
      expect(utils.queryByRole('dialog'), 'a confirm whose request is out stays open').not.toBeNull();
      await act(async () => { answer(true); });
    });

    it('the court move: focus goes in, Escape cancels and gives it back', async () => {
      // A queued row after Up next carries the court control (as in the move
      // confirm test below; mountCourt seeds a court feed).
      const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled')]);
      try {
        const opener = c.utils.getAllByTestId('move-to-B')[0];
        opener.focus();
        await act(async () => { opener.click(); });
        await settle();
        const dialog = c.utils.getByRole('dialog', { name: /move to shiaijo b\?/i });
        expect(dialog.contains(document.activeElement), 'focus is in the confirm').toBe(true);
        await act(async () => { fireEvent.keyDown(document.activeElement, { key: 'Escape' }); });
        await settle();
        expect(c.utils.queryByRole('dialog'), 'Escape cancels it').toBeNull();
        expect(document.activeElement, 'focus is back on the control that opened it').toBe(c.utils.getAllByTestId('move-to-B')[0]);
      } finally { c.restore(); }
    });
  });

  // UAT (bc-tmfn): the operator corrects a finished match, taps Clear
  // withdrawal and reopen (the match is RUNNING again), scores the rest and
  // taps Finish + Start Next. The console kept the old match pinned in
  // CORRECTION mode and hid the match it had just started until "Back to
  // court". A reopened correction is the court's live bout, so it becomes the
  // live pick and the correction ends.
  it('a reopened correction becomes the live match, so Finish + Start Next moves on to the match it started', async () => {
    const side = (id, name) => ({ id, name });
    const m1 = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'completed', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:00', modifiedAt: 1000,
      sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), winner: side('p1', 'Yamada'),
    };
    const m2 = {
      id: 'm2', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:05', sideA: side('p3', 'Sato'), sideB: side('p4', 'Kato'),
    };
    let current = [m1, m2];
    window.tournamentMatches = () => current;
    window.filterMatchesByCourt = (matches) => matches;
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    const prevRevert = window.API.revertMatchToQueue;
    window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
    window.API.subscribeToEvents = () => () => {};
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue(true);
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); });
      const editorMatch = () => utils.getByTestId('score-editor').getAttribute('data-match');
      const heading = () => utils.container.querySelector('.shiaijo-context__toggle').textContent;
      const refresh = async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); };

      await act(async () => { utils.getByRole('button', { name: /^correct$/i }).click(); });
      expect(editorMatch()).toBe('m1');
      expect(heading()).toContain('Correcting');

      // Clear withdrawal and reopen: m1 is running again.
      current = [{ ...m1, status: 'running', winner: undefined }, m2];
      await refresh();
      expect(editorMatch()).toBe('m1');
      expect(heading()).not.toContain('Correcting');

      // Finish + Start Next: m1 is completed again and m2 is running.
      current = [{ ...m1, status: 'completed', modifiedAt: 3000 }, { ...m2, status: 'running' }];
      await refresh();
      expect(editorMatch(), 'the panel must follow the match Finish + Start Next started').toBe('m2');
      expect(utils.queryByRole('button', { name: /back to court/i })).toBeNull();
      expect(heading()).not.toContain('Correcting');
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
      window.API.revertMatchToQueue = prevRevert;
    }
  });

  // bc-cse #6: a reopen does not always land on "running". A match-level
  // fusensho whose decisionBy side is STILL barred by an earlier, unrelated
  // withdrawal reopens to "scheduled" instead (engine.reopenTargetStatus):
  // starting it straight into "running" would strand the operator behind
  // the eligibility gate. The correction must still end, or the panel stays
  // pinned to the now-cleared correction (and, with the console's refusal to
  // start a match during a correction, no Start would ever work) until a
  // reload.
  it('a correction cleared to "scheduled" (still-barred fusensho) ends, and nothing stays pinned to it', async () => {
    const side = (id, name) => ({ id, name });
    const m1 = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'completed', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:00', modifiedAt: 1000,
      sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), winner: side('p1', 'Yamada'),
      decision: 'fusensho', decisionBy: 'aka',
    };
    const m2 = {
      id: 'm2', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:05', sideA: side('p3', 'Sato'), sideB: side('p4', 'Kato'),
    };
    let current = [m1, m2];
    window.tournamentMatches = () => current;
    window.filterMatchesByCourt = (matches) => matches;
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    const prevRevert = window.API.revertMatchToQueue;
    window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
    window.API.subscribeToEvents = () => () => {};
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue(true);
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); });
      const editorMatch = () => { const el = utils.queryByTestId('score-editor'); return el ? el.getAttribute('data-match') : null; };
      const heading = () => utils.container.querySelector('.shiaijo-context__toggle').textContent;
      const refresh = async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); };

      await act(async () => { utils.getByRole('button', { name: /^correct$/i }).click(); });
      expect(editorMatch()).toBe('m1');
      expect(heading()).toContain('Correcting');

      // Clear the withdrawal and reopen: the barred fusensho side is still
      // barred elsewhere, so the server sends m1 to "scheduled", not
      // "running" (m2 is only queued, on the same court).
      current = [{ ...m1, status: 'scheduled', decision: '', decisionBy: '', winner: undefined }, m2];
      await refresh();

      // The correction must end: nothing stays pinned to m1 and the panel
      // is the court's ordinary view (m2 is only queued, so no editor).
      expect(editorMatch(), 'the panel must release m1').toBeNull();
      expect(heading()).not.toContain('Correcting');
      expect(utils.queryByRole('button', { name: /back to court/i })).toBeNull();
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
      window.API.revertMatchToQueue = prevRevert;
    }
  });

  // UAT (bc-tmfn): a Start refused for an ineligible competitor
  // ("kiken-voluntary at Pool A-2") stayed on screen after the withdrawal was
  // cleared and eligibility restored, and after that match started it sat
  // under the NEXT match. It must go once it may no longer apply (an
  // eligibility change in its competition, or its match leaving scheduled), and
  // stay while it still does. A change of Up next by itself no longer drops it:
  // 'a stored Start refusal is bound to the refused match', below, pins that.
  it('a refused Start is dropped when eligibility changes or its match leaves scheduled, and kept while it still applies', async () => {
    const side = (id, name) => ({ id, name });
    const m1 = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:00', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    const m2 = {
      id: 'm2', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:05', sideA: side('p3', 'Sato'), sideB: side('p4', 'Kato'),
    };
    let current = [m1, m2];
    window.tournamentMatches = () => current;
    window.filterMatchesByCourt = (matches) => matches;
    let emit = () => {};
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
    window.API.subscribeToEvents = (cb) => { emit = cb; return () => {}; };
    const onEditScore = vi.fn().mockRejectedValue(new Error('kiken-voluntary at Pool A-2'));
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
      const refusal = () => utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error');
      // The Up next card's own Start (an Upcoming row carries one too).
      const start = async () => {
        const card = utils.container.querySelector('.shiaijo-upnext__card');
        const btn = [...card.querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));
        await act(async () => { btn.click(); });
      };
      const refresh = async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); };
      const send = async (event) => { await act(async () => { emit(event); }); };

      // A Start refused for an UPCOMING row's match is not shown under Up
      // next, which is a different match (the toast names it at the time).
      await act(async () => { utils.container.querySelector('.shiaijo-row__pick').click(); });
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(refusal()).toBeNull();

      await start();
      expect(refusal()?.textContent).toBe('kiken-voluntary at Pool A-2');

      // Still applies: a refetch with the same Up next, an unrelated event, and
      // an eligibility change in ANOTHER competition all leave it.
      await refresh();
      await send({ type: 'match_updated', data: { competitionId: 'c1', matchId: 'm9' } });
      await send({ type: 'competitor_status_updated', data: { competitionId: 'other', status: {} } });
      expect(refusal()?.textContent).toBe('kiken-voluntary at Pool A-2');

      // The restore in this competition clears it.
      await send({ type: 'competitor_status_updated', data: { competitionId: 'c1', status: { eligible: true } } });
      expect(refusal()).toBeNull();

      // Refused again, then m1 starts elsewhere: the refusal was about m1, which
      // has left scheduled, so it must not sit under the match Up next is now.
      await start();
      expect(refusal()).not.toBeNull();
      current = [{ ...m1, status: 'running' }, m2];
      await refresh();
      expect(utils.container.querySelector('.shiaijo-upnext__card').textContent).toContain('Sato');
      expect(refusal()).toBeNull();

      // Nor does it come back if m1 returns to the top of the queue.
      current = [m1, m2];
      await refresh();
      expect(utils.container.querySelector('.shiaijo-upnext__card').textContent).toContain('Yamada');
      expect(refusal()).toBeNull();
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });

  // A Start refused for a match picked from further down the queue (here its
  // competitor is fighting on another court) is kept keyed to that match, and
  // belongs to it: it stays on that match's row, and is on the card once the
  // match reaches Up next, for as long as the match is still scheduled here. It
  // used to be dropped by any change of Up next, which also took away a notice
  // whose cause still held. The cost is accepted: a cause that is gone is not
  // noticed until the operator taps Start again, which asks the server again.
  it('a refused Start for a match further down the queue is still there when it becomes Up next', async () => {
    const side = (id, name) => ({ id, name });
    const m1 = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:00', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    const m2 = {
      id: 'm2', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:05', sideA: side('p3', 'Sato'), sideB: side('p4', 'Kato'),
    };
    let current = [m1, m2];
    window.tournamentMatches = () => current;
    window.filterMatchesByCourt = (matches) => matches;
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
    window.API.subscribeToEvents = () => () => {};
    const onEditScore = vi.fn().mockRejectedValue(new Error('Sato is fighting on shiaijo B'));
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
      const refusal = () => utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error');
      const refresh = async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); };

      // Picked from the queue below Up next and refused: not shown under Up
      // next, which is m1.
      await act(async () => { utils.container.querySelector('.shiaijo-row__pick').click(); });
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(onEditScore.mock.calls[0][1]).toBe('m2');
      expect(refusal()).toBeNull();

      // m1 starts, so m2 is now Up next. The refusal is about m2, which is still
      // scheduled here, so it is on the card with it.
      current = [{ ...m1, status: 'running' }, m2];
      await refresh();
      expect(utils.container.querySelector('.shiaijo-upnext__card').textContent).toContain('Sato');
      expect(refusal()?.textContent).toBe('Sato is fighting on shiaijo B');
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });

  // The advance's own start (here the one after a decision) refused for the
  // next match: the refusal stays on the card once a refetch makes that match
  // Up next. The refetch is the very event a change of Up next used to drop it
  // on (an effect since removed: the refusal is bound to its match now).
  it('a refused advance start for the next match stays on Up next once that match becomes Up next', async () => {
    const side = (id, name) => ({ id, name });
    const mRun = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    // Up next before the decision. It involves the withdrawing Yamada, so the
    // advance passes over it to m3.
    const m2 = {
      id: 'm2', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:05', sideA: side('p1', 'Yamada'), sideB: side('p5', 'Endo'),
    };
    const m3 = {
      id: 'm3', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:10', sideA: side('p4', 'Doi'), sideB: side('p6', 'Fujii'),
    };
    let current = [mRun, m2, m3];
    window.tournamentMatches = () => current;
    window.filterMatchesByCourt = (matches) => matches;
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
    window.API.subscribeToEvents = () => () => {};
    const onEditScore = vi.fn().mockRejectedValue(new Error('Doi is fighting on shiaijo B'));
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
      expect(probe.props.match?.id).toBe('m-run');
      await act(async () => {
        await probe.props.onAfterDecision({
          id: 'm-run', sideA: 'Yamada', sideB: 'Tanaka', sideAId: 'p1', sideBId: 'p2', winner: 'Tanaka', winnerId: 'p2',
          status: 'completed', decision: 'kiken-voluntary', decisionBy: 'aka',
        });
      });
      // The advance's start of m3 is refused. Up next is still m2, so the
      // refusal is not on the card yet.
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(onEditScore.mock.calls[0][1]).toBe('m3');
      const upNextCard = () => utils.container.querySelector('.shiaijo-upnext__card');
      expect(upNextCard().querySelector('.shiaijo-upnext__error')).toBeNull();

      // The refetch: m2 carries the barred stamp the server writes, so Up next
      // moves onto m3, the match the refused start was for.
      current = [{ ...mRun, status: 'completed' }, { ...m2, ineligibleSides: { a: 'kiken-voluntary' } }, m3];
      await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); });
      expect(upNextCard().textContent).toContain('Doi');
      expect(upNextCard().querySelector('.shiaijo-upnext__error')?.textContent).toBe('Doi is fighting on shiaijo B');
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });

  // A refused Start for a queue row's match (not Up next) shows on that row,
  // so the operator sees why it did not start where they tapped it.
  it('a refused Start shows on the queue row of the match it refused, not on Up next', async () => {
    const side = (id, name) => ({ id, name });
    const m1 = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:00', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    const m2 = {
      id: 'm2', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:05', sideA: side('p3', 'Sato'), sideB: side('p4', 'Kato'),
    };
    window.tournamentMatches = () => [m1, m2];
    window.filterMatchesByCourt = (matches) => matches;
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
    window.API.subscribeToEvents = () => () => {};
    const onEditScore = vi.fn().mockRejectedValue(new Error('Kato is fighting on shiaijo B'));
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
      const rowOf = (name) => [...utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(name));
      await act(async () => { rowOf('Sato').querySelector('.shiaijo-row__pick').click(); });
      expect(onEditScore.mock.calls[0][1]).toBe('m2');
      expect(rowOf('Sato').querySelector('.shiaijo-upnext__error')?.textContent).toBe('Kato is fighting on shiaijo B');
      expect(utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error')).toBeNull();
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });

  // Guard: verify that a missing window scope reference causes a render failure.
  // This is the exact class of bug that PR #271 introduced undetected:
  // requestMoveCourt was used in ShiaijoQueueGroup but not passed as a prop,
  // causing a ReferenceError that the fake-React stub suite never caught.
  it('GUARD: a component referencing an undefined prop throws a ReferenceError', () => {
    // A minimal component whose body throws the ReferenceError an undefined
    // name throws. Simulates the requestMoveCourt bug class. Real React
    // invokes the function body; the stub suite's createElement never does.
    const BrokenComponent = () => {
      throw new ReferenceError("requestMoveCourt is not defined");
    };
    // React 18 calls console.error internally when a component throws (before
    // re-throwing the error). Suppress those expected calls with a local spy so
    // the suite's fail-on-console.error guard does not trip on this intentional
    // throw. The local spy replaces the beforeEach spy for this test's duration;
    // afterEach checks the beforeEach spy (which has 0 calls) and passes.
    const localError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const localWarn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    try {
      expect(() => render(<BrokenComponent />)).toThrow(ReferenceError);
    } finally {
      localError.mockRestore();
      localWarn.mockRestore();
    }
  });
});
