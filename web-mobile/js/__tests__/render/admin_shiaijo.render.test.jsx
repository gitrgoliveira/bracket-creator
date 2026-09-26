import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED } from '../../write_result.jsx';
// Window globals required by admin_shiaijo.jsx.
// MODULE-EVAL-TIME entries (e.g. `const AdminTopbar = window.AdminTopbar;`)
// must be set before the dynamic import, or the module captures undefined.
//
// The write_result predicates are deliberately NOT here: admin_shiaijo.jsx
// imports them directly from the leaf, so the component always gets the real
// implementations and a window entry for them would be dead weight suggesting
// otherwise. The queued-vs-superseded split the offline-resolve test below
// depends on is therefore the real one either way.

// bc-cse: captures the LATEST ScoreEditorModal props so a test can drive
// onSubmitAndNext/onAfterDecision directly (the chained-navigation tests
// below), while the rendered markup stays exactly what the pre-existing
// tests already query (data-testid="score-editor" data-match=...).
const probe = { props: null };

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import; set before dynamic import below
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  // Carries the id of the match the panel is scoring, so a test can tell
  // WHICH match the console selected (captured at import, so it cannot be
  // swapped per test).
  ScoreEditorModal: (props) => { probe.props = props; return <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />; },
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  // hasBothSides / isPendingBracketMatch are the REAL implementations published
  // on window by vitest.setup.render.js (import of admin_helpers.jsx). We
  // deliberately do NOT stub them: the shiaijo queue's split between actionable
  // rows and pending placeholder finals (mp-y3nk) depends on their real logic.
  // LAZY: only called in event handlers or guarded effects
  filterMatchesByCourt: (matches, _court) => matches,
  tournamentMatches: () => [],
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
    reinstateCompetitor: vi.fn().mockResolvedValue({}),
  },
  startPatch: vi.fn(),
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  compMatches: () => [],
  // LAZY: the requeue confirm counts what it discards (ui.jsx's pluralize).
  pluralize: (count, singular, plural) => (count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`),
};

let restoreGlobals;
let AdminShiaijoPage;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});

afterAll(() => restoreGlobals());

// Reset per-test window overrides so each test starts from a known baseline.
afterEach(() => {
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
});

function makeMinimalTournament(overrides = {}) {
  return {
    name: 'Test Tournament',
    courts: ['A', 'B'],
    competitions: [],
    ...overrides,
  };
}

function renderPage(tournament, court = 'A', props = {}) {
  return render(
    <AdminShiaijoPage
      tournament={tournament}
      court={court}
      onBack={vi.fn()}
      onEditScore={props.onEditScore || vi.fn()}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      password=""
      showToast={vi.fn()}
      tweaks={{}}
      onSwitchCourt={vi.fn()}
    />
  );
}

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

  // mp-y3nk Phase 3: "Run now" on a pending final opens the resolve-feeders
  // modal; recording each feeder's winner calls overrideBracketWinner (which
  // server-side propagates to resolve the final) and then refetches the court.
  it('Run now → recording feeder winners calls overrideBracketWinner per feeder', async () => {
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
    const overrideBracketWinner = vi.fn().mockResolvedValue(true);
    const prevOverride = window.API.overrideBracketWinner;
    window.API.overrideBracketWinner = overrideBracketWinner;
    window.tournamentMatches = () => [pendingFinal];
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament({ competitions: [comp] })); });
      await act(async () => { utils.getByRole('button', { name: /run now/i }).click(); });
      // Pick a winner for each feeder, then confirm.
      await act(async () => { utils.getByRole('button', { name: 'Alice' }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Carol' }).click(); });
      await act(async () => { utils.getByRole('button', { name: /record & make startable/i }).click(); });
      expect(overrideBracketWinner).toHaveBeenCalledTimes(2);
      expect(overrideBracketWinner).toHaveBeenCalledWith('c1', 'm-r2-0', 'Alice', expect.anything());
      expect(overrideBracketWinner).toHaveBeenCalledWith('c1', 'm-r2-1', 'Carol', expect.anything());
    } finally {
      window.API.overrideBracketWinner = prevOverride;
    }
  });

  // mp-y3nk offline console: when the override write is only QUEUED (offline),
  // resolving feeders must still optimistically advance the LOCAL bracket so the
  // final becomes a startable Up Next immediately (not stuck in "Later"). This is
  // the "run the competition offline" path.
  it('offline resolve (queued write) advances the local bracket so the final is startable', async () => {
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
    // OFFLINE: override returns a queued discriminator instead of confirming.
    const overrideBracketWinner = vi.fn().mockResolvedValue({ queued: true });
    const prevOverride = window.API.overrideBracketWinner;
    window.API.overrideBracketWinner = overrideBracketWinner;
    // tournamentMatches re-derives queue rows from each comp's bracket.rounds, so
    // an optimistic advance to the bracket surfaces the resolved final.
    window.tournamentMatches = (t) => {
      const c = (t.competitions || []).find((x) => x.id === 'c1');
      const r = c && c.bracket && c.bracket.rounds;
      const out = [];
      if (r) {
        r[0].forEach((m) => out.push({ ...m, compId: 'c1', compName: 'Cup', phase: 'bracket', court: 'A' }));
        r[1].forEach((m, i) => out.push({ ...m, id: 'm-r1-' + i, compId: 'c1', compName: 'Cup', phase: 'bracket', matchNumber: 3, court: 'A' }));
      }
      return out.length ? out : [pendingFinal];
    };
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament({ competitions: [comp] })); });
      await act(async () => { utils.getByRole('button', { name: /run now/i }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Alice' }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Carol' }).click(); });
      await act(async () => { utils.getByRole('button', { name: /record & make startable/i }).click(); });
      // The final now shows resolved competitors (no placeholder) and is startable
      // even though the override only queued (offline).
      expect(utils.queryByText(/Winner of r2/)).toBeNull(); // no placeholder left anywhere
      expect(utils.getAllByText('Alice').length).toBeGreaterThan(0);
      expect(utils.getAllByText('Carol').length).toBeGreaterThan(0);
      expect(utils.getByRole('button', { name: /start match/i })).toBeTruthy();
    } finally {
      window.API.overrideBracketWinner = prevOverride;
    }
  });

  // bc-kcdg: a feeder assertion refused with 409 downstream_knockout_played
  // (the final built on this feeder's PRIOR result already played) must offer
  // the same confirm+retry experience as a score correction, via the shared
  // attemptScoreWrite loop -- not surface the raw refusal as an opaque error.
  it('Run now → a downstream refusal on a feeder prompts confirm, and confirming retries with forceDownstreamReopen', async () => {
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
    const refusal = new Error('Eve already played match m-r1-0, asserting this winner would displace them.');
    refusal.downstreamKnockoutPlayed = { matchId: 'm-r2-0', blockingMatchId: 'm-r1-0', displaced: 'Eve' };
    // First call for the m-r2-0 feeder is refused; every call after (its forced
    // retry, and the untouched m-r2-1 feeder) succeeds.
    const overrideBracketWinner = vi.fn()
      .mockRejectedValueOnce(refusal)
      .mockResolvedValue({ applied: true });
    const prevOverride = window.API.overrideBracketWinner;
    const prevConfirm = window.confirmDialog;
    window.API.overrideBracketWinner = overrideBracketWinner;
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    window.tournamentMatches = () => [pendingFinal];
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament({ competitions: [comp] })); });
      await act(async () => { utils.getByRole('button', { name: /run now/i }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Alice' }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Carol' }).click(); });
      await act(async () => { utils.getByRole('button', { name: /record & make startable/i }).click(); });

      // The dialog names the blocking match and the displaced competitor, not a
      // generic prompt -- this IS the operator-facing copy the confirm dialog
      // shows (write_result.jsx's downstreamKnockoutPlayedConfirm).
      expect(window.confirmDialog).toHaveBeenCalledTimes(1);
      const dialogArg = window.confirmDialog.mock.calls[0][0];
      expect(dialogArg.message).toContain('m-r1-0');
      expect(dialogArg.message).toContain('Eve');
      expect(dialogArg.confirmLabel).toBe('Apply correction and reopen');

      // The refused feeder is retried with the force flag; the untouched
      // feeder is never offered it.
      expect(overrideBracketWinner).toHaveBeenCalledWith('c1', 'm-r2-0', 'Alice', expect.anything());
      expect(overrideBracketWinner).toHaveBeenCalledWith('c1', 'm-r2-0', 'Alice', expect.anything(), true);
      expect(overrideBracketWinner).toHaveBeenCalledWith('c1', 'm-r2-1', 'Carol', expect.anything());
      expect(overrideBracketWinner).toHaveBeenCalledTimes(3);
    } finally {
      window.API.overrideBracketWinner = prevOverride;
      window.confirmDialog = prevConfirm;
    }
  });

  // bc-kcdg: declining the confirm dialog must leave BOTH the feeder assertion
  // and the later match it would have reopened unwritten, and tell the
  // operator so in the modal's own inline error area (not a raw thrown message).
  it('Run now → declining the downstream-reopen prompt records nothing and shows the cancellation notice', async () => {
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
    const refusal = new Error('Eve already played match m-r1-0, asserting this winner would displace them.');
    refusal.downstreamKnockoutPlayed = { matchId: 'm-r2-0', blockingMatchId: 'm-r1-0', displaced: 'Eve' };
    const overrideBracketWinner = vi.fn().mockRejectedValue(refusal);
    const prevOverride = window.API.overrideBracketWinner;
    const prevConfirm = window.confirmDialog;
    window.API.overrideBracketWinner = overrideBracketWinner;
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    window.tournamentMatches = () => [pendingFinal];
    window.filterMatchesByCourt = (matches) => matches;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament({ competitions: [comp] })); });
      await act(async () => { utils.getByRole('button', { name: /run now/i }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Alice' }).click(); });
      await act(async () => { utils.getByRole('button', { name: 'Carol' }).click(); });
      await act(async () => { utils.getByRole('button', { name: /record & make startable/i }).click(); });

      expect(utils.getByText(DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED)).toBeTruthy();
      // The loop aborts on the first (declined) feeder: the untouched second
      // feeder is never even attempted, and no retry with the force flag ran.
      expect(overrideBracketWinner).toHaveBeenCalledTimes(1);
      // The modal stays open (declining is not the same as onClose/onResolved).
      expect(utils.getByRole('button', { name: /record & make startable/i })).toBeTruthy();
    } finally {
      window.API.overrideBracketWinner = prevOverride;
      window.confirmDialog = prevConfirm;
    }
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
  // pinned to the now-cleared correction and hides whatever the court's
  // normal view would show -- here, the OTHER match already running on this
  // same court -- until a reload.
  it('a correction cleared to "scheduled" (still-barred fusensho) releases the panel back to the running bout', async () => {
    const side = (id, name) => ({ id, name });
    const m1 = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'completed', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:00', modifiedAt: 1000,
      sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), winner: side('p1', 'Yamada'),
      decision: 'fusensho', decisionBy: 'aka',
    };
    const m2 = {
      id: 'm2', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
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

      // Clear the withdrawal and reopen: the barred fusensho side is still
      // barred elsewhere, so the server sends m1 to "scheduled", not
      // "running" (m2 keeps running throughout, on the same court).
      current = [{ ...m1, status: 'scheduled', decision: '', decisionBy: '', winner: undefined }, m2];
      await refresh();

      // The correction must end: the panel falls back to the court's
      // normal view -- the running bout (m2) -- instead of staying pinned
      // to m1.
      expect(editorMatch(), "the panel must release m1 and follow the court's running bout").toBe('m2');
      expect(heading()).not.toContain('Correcting');
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
  // eligibility change in its competition, or ANY change of Up next), and
  // stay while it still does.
  it('a refused Start is dropped when eligibility changes or Up next moves on, and kept while it still applies', async () => {
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
      const refusal = () => utils.container.querySelector('.shiaijo-upnext__error');
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

      // Refused again, then m1 starts elsewhere: Up next is now m2, and the
      // refusal must not sit under it.
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
  // competitor is fighting on another court) is kept keyed to that match. It
  // used to come back when that match later reached Up next, although its
  // cause was gone by then and nothing else clears it: a finished match sends
  // no competitor_status_updated. Any change of Up next drops it.
  it('a refused Start for a match further down the queue does not reappear when it becomes Up next', async () => {
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
      const refusal = () => utils.container.querySelector('.shiaijo-upnext__error');
      const refresh = async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); };

      // Picked from the queue below Up next and refused: not shown under Up
      // next, which is m1.
      await act(async () => { utils.container.querySelector('.shiaijo-row__pick').click(); });
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(onEditScore.mock.calls[0][1]).toBe('m2');
      expect(refusal()).toBeNull();

      // m1 starts, so m2 is now Up next. The old refusal must not come back
      // with it.
      current = [{ ...m1, status: 'running' }, m2];
      await refresh();
      expect(utils.container.querySelector('.shiaijo-upnext__card').textContent).toContain('Sato');
      expect(refusal()).toBeNull();
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
    // A minimal component that references an undefined variable. Simulates
    // the requestMoveCourt bug class. Real React invokes the function body;
    // the stub suite's createElement never does.
    const BrokenComponent = () => {
      // eslint-disable-next-line no-undef
      return <div>{undefinedVariable}</div>;
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

// bc-cse: a scheduled match a competitor is barred from (withdrew earlier)
// cannot be fought. No auto-pick may offer it (Up Next, Finish + Start Next);
// its queue row shows why and a one-tap way to resolve it instead of a Start
// button the server would just refuse.
describe('a barred match is skipped by every auto-pick (bc-cse)', () => {
  const side = (id, name) => ({ id, name });
  // Withdrawn on side B (Yama, kiken-voluntary): the default win goes to Umi.
  const barredMatch = (over = {}) => ({
    id: 'm-barred', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
    court: 'A', scheduledAt: '09:05', sideA: side('u', 'Umi'), sideB: side('y', 'Yama'),
    ineligibleSides: { b: 'kiken-voluntary' },
    ...over,
  });
  const openMatch = {
    id: 'm-open', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
    court: 'A', scheduledAt: '09:10', sideA: side('s', 'Sato'), sideB: side('k', 'Kato'),
  };

  it('Up Next skips it, its queue row offers the default win instead of Start, and no Reinstate for a non-reinstateable withdrawal', async () => {
    window.tournamentMatches = () => [barredMatch(), openMatch];
    window.filterMatchesByCourt = (m) => m;
    const recordDecision = vi.fn().mockResolvedValue({ applied: true });
    const prevRD = window.API.recordDecision;
    window.API.recordDecision = recordDecision;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); });

      // Up Next skips the barred match and offers the next fightable one.
      const card = utils.container.querySelector('.shiaijo-upnext__card');
      expect(card.textContent).toContain('Sato');
      expect(card.textContent).not.toContain('Umi');
      expect(card.textContent).not.toContain('Yama');

      // The barred match's own row: the note, one action, no Start, no Reinstate.
      const rows = [...utils.container.querySelectorAll('.shiaijo-qrow')];
      const row = rows.find((r) => r.textContent.includes('Yama'));
      expect(row).toBeTruthy();
      expect(row.textContent).toContain('Yama withdrew: record the default win.');
      const startBtn = [...row.querySelectorAll('button')].find((b) => /^start match$/i.test(b.textContent));
      expect(startBtn).toBeUndefined();
      expect(utils.queryByTestId('barred-match-reinstate')).toBeNull();
      const awardBtn = utils.getByTestId('barred-match-default-win');
      expect(awardBtn.textContent).toBe('Record default win for Umi');

      await act(async () => { awardBtn.click(); });
      expect(recordDecision).toHaveBeenCalledWith('c1', 'm-barred', {
        decision: 'fusensho', decisionBy: 'shiro', decisionReason: 'auto: Yama withdrawn',
      }, '');
    } finally {
      window.API.recordDecision = prevRD;
    }
  });

  it('Finish + Start Next skips it when advancing to the next match on the same court', async () => {
    const running = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    window.tournamentMatches = () => [running, barredMatch(), openMatch];
    window.filterMatchesByCourt = (m) => m;
    const onEditScore = vi.fn().mockResolvedValue({ status: 'ok' });
    await act(async () => { renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    expect(probe.props.match?.id).toBe('m-run');
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed', winner: side('p1', 'Yamada') }); });
    // The finishing write, then the start-next write -- skipping m-barred.
    expect(onEditScore).toHaveBeenCalledTimes(2);
    expect(onEditScore.mock.calls[0][0]).toBe('c1');
    expect(onEditScore.mock.calls[0][1]).toBe('m-run');
    expect(onEditScore.mock.calls[1][0]).toBe('c1');
    expect(onEditScore.mock.calls[1][1]).toBe('m-open');
  });

  it('offers Reinstate for a reinstateable (kiken-injury) withdrawal', async () => {
    window.tournamentMatches = () => [barredMatch({ ineligibleSides: { b: 'kiken-injury' } }), openMatch];
    window.filterMatchesByCourt = (m) => m;
    const reinstateCompetitor = vi.fn().mockResolvedValue({});
    const prevR = window.API.reinstateCompetitor;
    window.API.reinstateCompetitor = reinstateCompetitor;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); });
      const reinstateBtn = utils.getByTestId('barred-match-reinstate');
      expect(reinstateBtn.textContent).toBe('Reinstate Yama');
      await act(async () => { reinstateBtn.click(); });
      expect(reinstateCompetitor).toHaveBeenCalledWith('c1', 'y', '');
    } finally {
      window.API.reinstateCompetitor = prevR;
    }
  });

  // bc-cse: both sides barred on a POOL match (barredMatch's own phase) can
  // be recorded as drawn -- neither single-sided default-win action fits,
  // but the pool/league draw action does.
  it('both sides barred: the note, and the draw action (no single-sided action)', async () => {
    window.tournamentMatches = () => [barredMatch({ id: 'Pool A-2', ineligibleSides: { a: 'fusenpai', b: 'kiken-voluntary' } })];
    window.filterMatchesByCourt = (m) => m;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    const notice = utils.getByTestId('barred-match-notice');
    expect(notice.textContent).toContain('Both withdrew earlier: neither can fight this match.');
    expect(utils.queryByTestId('barred-match-default-win')).toBeNull();
    expect(utils.queryByTestId('barred-match-reinstate')).toBeNull();
    expect(utils.getByTestId('barred-match-record-drawn').textContent).toBe('Record as drawn (neither can fight)');
  });

  // bc-cse: when EVERY scheduled match on the court is barred there is no
  // Up Next card at all (upNext skips a barred match on purpose), so
  // telling the operator to use it points at something not on screen. The
  // placeholder must name the real remedy instead.
  it('placeholder names the real remedy when every scheduled match is barred (no Up Next card)', async () => {
    window.tournamentMatches = () => [barredMatch()];
    window.filterMatchesByCourt = (m) => m;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    expect(utils.container.querySelector('.shiaijo-upnext__card')).toBeNull();
    expect(utils.container.textContent).toContain(
      'Every scheduled match on this court is barred. Resolve a withdrawal in the queue to bring one back.'
    );
    expect(utils.container.textContent).not.toContain('Start the next match from the Up Next card');
  });

  // bc-cse: the ordinary placeholder still applies when there is nothing
  // scheduled for a reason OTHER than "everything is barred" -- here, a
  // pending placeholder final with no real Up Next yet (same fixture shape
  // as the "Later" row test above).
  it('placeholder keeps the ordinary copy when nothing is scheduled for an unrelated reason', async () => {
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
    window.filterMatchesByCourt = (m) => m;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    expect(utils.container.textContent).toContain('Start the next match from the Up Next card');
    expect(utils.container.textContent).not.toContain('Every scheduled match on this court is barred');
  });

  // bc-cse: a barred queue row offers only its own one-tap resolution --
  // Call to court and Lineup, both meaningless for a match the server would
  // refuse to start, are hidden.
  it('a barred queue row hides Call to court and Lineup', async () => {
    const teamBarred = {
      id: 'm-team-barred', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:05', compKind: 'team', teamSize: 3,
      sideA: { id: 'team-a', name: 'Team A' }, sideB: { id: 'team-b', name: 'Team B' },
      ineligibleSides: { b: 'kiken-voluntary' },
    };
    window.tournamentMatches = () => [teamBarred, openMatch];
    window.filterMatchesByCourt = (m) => m;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    const rows = [...utils.container.querySelectorAll('.shiaijo-qrow')];
    const row = rows.find((r) => r.textContent.includes('Team A'));
    expect(row).toBeTruthy();
    const buttonText = [...row.querySelectorAll('button')].map((b) => b.textContent);
    expect(buttonText).not.toContain('Lineup');
    expect(buttonText.some((t) => /call to court/i.test(t))).toBe(false);
  });
});

// Shared by the bc-kpnl and bc-sbq blocks below: a mutable court feed, a
// Refresh that re-reads it, and the editor probe's current match.
async function mountCourt(initial) {
  const feed = { current: initial };
  window.tournamentMatches = () => feed.current;
  window.filterMatchesByCourt = (matches) => matches;
  const prev = {
    fetch: window.API.fetchCourtMatches,
    sub: window.API.subscribeToEvents,
    revert: window.API.revertMatchToQueue,
  };
  window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
  window.API.subscribeToEvents = () => () => {};
  window.API.revertMatchToQueue = vi.fn().mockResolvedValue(true);
  const showToast = vi.fn();
  let utils;
  await act(async () => {
    utils = render(
      <AdminShiaijoPage tournament={makeMinimalTournament()} court="A" onBack={vi.fn()} onEditScore={vi.fn()}
        onMoveCourt={vi.fn()} onLogout={vi.fn()} onViewerMode={vi.fn()} password="" showToast={showToast}
        tweaks={{}} onSwitchCourt={vi.fn()} />
    );
  });
  return {
    utils,
    feed,
    showToast,
    editorMatch: () => { const el = utils.queryByTestId('score-editor'); return el ? el.getAttribute('data-match') : null; },
    refresh: async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); },
    restore: () => {
      window.API.fetchCourtMatches = prev.fetch;
      window.API.subscribeToEvents = prev.sub;
      window.API.revertMatchToQueue = prev.revert;
    },
  };
}

const courtSide = (id, name) => ({ id, name });
const courtMatch = (id, status, over = {}) => ({
  id, compId: 'c1', compName: 'Cup', status, phase: 'pool', poolName: 'Pool A', court: 'A',
  scheduledAt: `09:0${id.slice(-1)}`,
  sideA: courtSide(`${id}-a`, `Aka ${id}`), sideB: courtSide(`${id}-b`, `Shiro ${id}`),
  ...over,
});

// bc-kpnl: a kiken completes the match, so the console's running[0] moved on,
// the editor (keyed on the match) unmounted and the Remaining matches panel,
// which only the editor holds, went with it after about a second. The editor
// now reports the kiken (onWithdrawal) and the console pins that match until
// the panel is closed (the editor's onClose) or the operator leaves it.
describe('the kiken-decided match stays on the console until its panel is closed (bc-kpnl)', () => {
  it('stays through the refetch that completes it, then releases on the panel close', async () => {
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    try {
      expect(c.editorMatch()).toBe('m1');
      await act(async () => { probe.props.onWithdrawal({ id: 'm1-a', name: 'Aka m1' }); });
      // The kiken completed m1 and another device already started m2.
      c.feed.current = [courtMatch('m1', 'completed', { decision: 'kiken-voluntary', decisionBy: 'aka' }), courtMatch('m2', 'running')];
      await c.refresh();
      expect(c.editorMatch(), 'the panel must stay on the kiken-decided match').toBe('m1');
      // The panel's close releases the pin: the court's live bout returns.
      await act(async () => { probe.props.onClose(); });
      expect(c.editorMatch()).toBe('m2');
    } finally { c.restore(); }
  });

  it("holds the editor open when the kiken was the court's last bout", async () => {
    const c = await mountCourt([courtMatch('m1', 'running')]);
    try {
      await act(async () => { probe.props.onWithdrawal({ id: 'm1-a', name: 'Aka m1' }); });
      c.feed.current = [courtMatch('m1', 'completed', { decision: 'kiken-voluntary', decisionBy: 'aka' })];
      await c.refresh();
      expect(c.editorMatch(), 'allDone must not unmount the pinned editor').toBe('m1');
      expect(c.utils.queryByText(/complete on Shiaijo A/i)).toBeNull();
      // Back to court is the console's own exit from the pin.
      await act(async () => { c.utils.getByRole('button', { name: /back to court/i }).click(); });
      expect(c.editorMatch()).toBeNull();
      expect(c.utils.getByText(/complete on Shiaijo A/i)).toBeTruthy();
    } finally { c.restore(); }
  });

  // The pin is for a kiken-COMPLETED match. Reopened (Clear withdrawal and
  // reopen) it is the court's live bout, so it becomes the pick and the pin
  // ends: Finish + Start Next then moves on to the match it started, as it
  // does after a reopened correction (bc-tmfn). Without the release the
  // panel stayed pinned on the old match and hid the one just started.
  it('releases once the match is reopened, so Finish + Start Next moves on', async () => {
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    try {
      await act(async () => { probe.props.onWithdrawal({ id: 'm1-a', name: 'Aka m1' }); });
      // The feed has not caught up with the kiken yet: the pin must hold.
      expect(c.editorMatch()).toBe('m1');
      c.feed.current = [courtMatch('m1', 'completed', { decision: 'kiken-voluntary', decisionBy: 'aka' }), courtMatch('m2', 'scheduled')];
      await c.refresh();
      expect(c.editorMatch()).toBe('m1');
      // Clear withdrawal and reopen: m1 is running again.
      c.feed.current = [courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')];
      await c.refresh();
      expect(c.editorMatch()).toBe('m1');
      expect(c.utils.queryByRole('button', { name: /back to court/i })).toBeNull();
      // Finish + Start Next: m1 completed again and m2 running.
      c.feed.current = [courtMatch('m1', 'completed'), courtMatch('m2', 'running')];
      await c.refresh();
      expect(c.editorMatch(), 'the panel must follow the match Finish + Start Next started').toBe('m2');
    } finally { c.restore(); }
  });

  // Review finding: the kiken is recorded for the wrong side, cleared and
  // reopened (the pin lets go), then recorded for the right side on the SAME
  // match while the feed still shows it running. The second pin must hold.
  it('holds a second kiken pin on the same match after the first was released', async () => {
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    try {
      await act(async () => { probe.props.onWithdrawal({ id: 'm1-b', name: 'Shiro m1' }); });
      c.feed.current = [courtMatch('m1', 'completed', { decision: 'kiken-voluntary', decisionBy: 'shiro' }), courtMatch('m2', 'scheduled')];
      await c.refresh();
      c.feed.current = [courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')];
      await c.refresh();
      expect(c.editorMatch()).toBe('m1');
      // The correct side withdraws; the feed has not caught up yet.
      await act(async () => { probe.props.onWithdrawal({ id: 'm1-a', name: 'Aka m1' }); });
      c.feed.current = [courtMatch('m1', 'completed', { decision: 'kiken-voluntary', decisionBy: 'aka' }), courtMatch('m2', 'running')];
      await c.refresh();
      expect(c.editorMatch(), 'the second pin must hold the panel').toBe('m1');
    } finally { c.restore(); }
  });

  it('every other onClose is still a no-op on the console', async () => {
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    try {
      await act(async () => { probe.props.onClose(); });
      expect(c.editorMatch()).toBe('m1');
    } finally { c.restore(); }
  });
});

// bc-sbq: Send back to queue clears the whole bout log. The confirm read what
// would be lost from the court feed alone, which lags the editor, so it said
// "nothing will be lost" with a point on the board; and on a reopened team
// encounter it wiped every fought bout. The confirm now reads the editor's
// board too, and the button is not offered once any team bout has a result.
describe('Send back to queue says what it discards and never discards fought bouts (bc-sbq)', () => {
  const tapSendBack = async (c) => {
    await act(async () => { c.utils.getByRole('button', { name: /send back to queue/i }).click(); });
    return c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
  };

  it('names the point on the board even while the court feed still shows none', async () => {
    const c = await mountCourt([courtMatch('m1', 'running')]);
    try {
      await act(async () => {
        probe.props.onBoardChange({ compId: 'c1', matchId: 'm1', points: 1, fouls: 1, overtime: false, draw: false, bouts: 0 });
      });
      const dialog = await tapSendBack(c);
      expect(dialog.textContent).toContain('will be discarded: 1 point and 1 foul.');
      expect(dialog.textContent).not.toContain('nothing will be lost');
    } finally { c.restore(); }
  });

  it('names the engi flags on the board', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { compEngi: true })]);
    try {
      await act(async () => {
        probe.props.onBoardChange({ compId: 'c1', matchId: 'm1', points: 0, fouls: 0, flags: 3, overtime: false, draw: false, bouts: 0 });
      });
      const dialog = await tapSendBack(c);
      expect(dialog.textContent).toContain('will be discarded: 3 flags.');
    } finally { c.restore(); }
  });

  it("ignores a board reported for a different match", async () => {
    const c = await mountCourt([courtMatch('m1', 'running')]);
    try {
      await act(async () => {
        probe.props.onBoardChange({ compId: 'c1', matchId: 'm9', points: 2, fouls: 0, overtime: false, draw: false, bouts: 0 });
      });
      const dialog = await tapSendBack(c);
      expect(dialog.textContent).toContain('No score has been entered, so nothing will be lost.');
    } finally { c.restore(); }
  });

  it('is not offered on a team match whose feed carries a fought bout', async () => {
    const reopened = courtMatch('m1', 'running', {
      compKind: 'team', teamSize: 3, reopenPending: true,
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' },
        { position: 2, sideA: 'A2', sideB: 'B2', ipponsA: [], ipponsB: [] },
      ],
    });
    const c = await mountCourt([reopened]);
    try {
      expect(c.editorMatch()).toBe('m1');
      expect(c.utils.queryByRole('button', { name: /send back to queue/i })).toBeNull();
    } finally { c.restore(); }
  });

  it("is withdrawn as soon as the sheet reports a fought bout the feed has not caught up with", async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { compKind: 'team', teamSize: 3, subResults: [] })]);
    try {
      expect(c.utils.getByRole('button', { name: /send back to queue/i })).toBeTruthy();
      await act(async () => {
        probe.props.onBoardChange({ compId: 'c1', matchId: 'm1', points: 0, fouls: 0, overtime: false, draw: false, bouts: 1 });
      });
      expect(c.utils.queryByRole('button', { name: /send back to queue/i })).toBeNull();
    } finally { c.restore(); }
  });

  // Review finding: a mark taken back on a kachinuki bout stays in the feed
  // until the clear has saved and the court has refetched, so counting the
  // feed would hide the requeue meanwhile. The sheet's own count decides
  // once it has reported.
  it('offers the button again once the sheet reports the bout cleared, whatever the feed still holds', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', {
      compKind: 'team', teamSize: 3,
      subResults: [{ position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [] }],
    })]);
    try {
      expect(c.utils.queryByRole('button', { name: /send back to queue/i })).toBeNull();
      await act(async () => {
        probe.props.onBoardChange({ compId: 'c1', matchId: 'm1', points: 0, fouls: 0, overtime: false, draw: false, bouts: 0 });
      });
      expect(c.utils.getByRole('button', { name: /send back to queue/i })).toBeTruthy();
    } finally { c.restore(); }
  });

  it('keeps the button on a team match whose autosaved rows carry no result', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', {
      compKind: 'team', teamSize: 2,
      subResults: [{ position: 1, sideA: 'A1', sideB: 'B1' }, { position: 2, sideA: 'A2', sideB: 'B2' }],
    })]);
    try {
      const dialog = await tapSendBack(c);
      expect(dialog.textContent).toContain('nothing will be lost');
    } finally { c.restore(); }
  });

  it("pickMatch does not silently defer a bout whose point is only on the editor's board", async () => {
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')]);
    try {
      await act(async () => {
        probe.props.onBoardChange({ compId: 'c1', matchId: 'm1', points: 1, fouls: 0, overtime: false, draw: false, bouts: 0 });
      });
      const startNext = c.utils.getAllByRole('button').find((b) => /^start/i.test(b.textContent.trim()));
      expect(startNext, 'the Up Next card offers Start for m2').toBeTruthy();
      await act(async () => { startNext.click(); });
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(c.showToast).toHaveBeenCalledWith('Finish or correct the bout in progress first', 'error');
    } finally { c.restore(); }
  });
});
